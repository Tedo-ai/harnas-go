package harnas

import (
	"fmt"
	"time"
)

var sleep = time.Sleep

const RunReasonIncompleteToolBatch = "incomplete_tool_batch"

type providerTurn struct {
	stopReason string
	reason     string
}

type ProviderResponseIntegrityError struct {
	Reason  string
	Message string
}

func (e *ProviderResponseIntegrityError) Error() string {
	return fmt.Sprintf("provider response integrity violation (%s): %s", e.Reason, e.Message)
}

func (e *ProviderResponseIntegrityError) ProviderRetryable() bool { return false }

type AgentLoop struct {
	Session        *Session
	Projection     Projection
	Provider       Provider
	ProviderKind   string
	Ingestor       Ingestor
	StreamProvider StreamProvider
	Runner         *Runner
	RetryPolicy    *RetryPolicy
	MaxTurns       int
	OnStreamEvent  func(Event)
}

func (l AgentLoop) Run() (reason string, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if _, ok := recovered.(TurnFailed); ok {
				reason = "runtime_failed"
				err = nil
				return
			}
			panic(recovered)
		}
	}()
	maxTurns := l.MaxTurns
	if maxTurns == 0 {
		maxTurns = 10
	}
	if serr := l.Session.Log.StorageErr(); serr != nil {
		return "", &StorageWriteError{Cause: serr}
	}
	if violations := AnalyzeDurableLog(l.Session.Log); len(violations) > 0 {
		return "", &TranscriptIntegrityError{Violations: violations}
	}
	if l.terminalRuntimeError() {
		return "runtime_failed", nil
	}
	if entryReason, blocked, entryErr := l.prepareRunEntry(); entryErr != nil {
		return "", entryErr
	} else if blocked {
		return entryReason, nil
	}
	reason = "max_turns_reached"
	for range maxTurns {
		turn, err := l.runTurn()
		if err != nil {
			return "", err
		}
		if turn.reason != "" {
			reason = turn.reason
			break
		}
		if turn.stopReason != "tool_use" {
			reason = "end_turn"
			break
		}
		pending, awaiting := l.dispatchPendingTools()
		if serr := l.Session.Log.StorageErr(); serr != nil {
			return "", &StorageWriteError{Cause: serr}
		}
		if awaiting {
			reason = "awaiting_approval"
			break
		}
		if len(l.pendingToolUses()) > 0 {
			_, prepareErr := PrepareProviderCall(l.Session)
			return "", prepareErr
		}
		if len(pending) == 0 {
			reason = "no_pending_tools"
			break
		}
		if l.terminalRuntimeError() {
			reason = "runtime_failed"
			break
		}
	}
	return reason, nil
}

func (l AgentLoop) prepareRunEntry() (string, bool, error) {
	pending := l.pendingToolUses()
	if len(pending) == 0 {
		return "", false, nil
	}
	pendingIDs := map[string]bool{}
	for _, toolUse := range pending {
		pendingIDs[stringValue(toolUse.Payload["id"])] = true
	}
	awaiting := map[string]bool{}
	resolvedWithoutResult := map[string]bool{}
	for _, event := range l.Session.Log.Events() {
		id := stringValue(event.Payload["tool_use_id"])
		if !pendingIDs[id] {
			continue
		}
		switch event.Type {
		case EventApprovalRequested:
			awaiting[id] = true
		case EventApprovalResolved:
			delete(awaiting, id)
			resolvedWithoutResult[id] = true
		case EventToolResult:
			delete(awaiting, id)
			delete(resolvedWithoutResult, id)
		}
	}
	if len(awaiting) > 0 {
		return "awaiting_approval", true, nil
	}
	if len(resolvedWithoutResult) > 0 {
		violations := make([]IntegrityViolation, 0, len(resolvedWithoutResult))
		for id := range resolvedWithoutResult {
			violations = append(violations, IntegrityViolation{
				Domain: IntegrityDomainDurableLog, Code: IntegrityResolvedApprovalOpen,
				EventSeq: -1, ToolUseID: id,
				Message: fmt.Sprintf("approval for tool_use %q was resolved but no result became durable", id),
			})
		}
		return "", false, &TranscriptIntegrityError{Violations: violations}
	}
	last, ok := l.Session.Log.LastAssistantMessage()
	if !ok || stringValue(last.Payload["stop_reason"]) != "tool_use" {
		_, err := PrepareProviderCall(l.Session)
		return "", false, err
	}
	_, nowAwaiting := l.dispatchPendingTools()
	if serr := l.Session.Log.StorageErr(); serr != nil {
		return "", false, &StorageWriteError{Cause: serr}
	}
	if nowAwaiting {
		return "awaiting_approval", true, nil
	}
	if len(l.pendingToolUses()) > 0 {
		_, err := PrepareProviderCall(l.Session)
		return "", false, err
	}
	if l.terminalRuntimeError() {
		return "runtime_failed", true, nil
	}
	return "", false, nil
}

func (l AgentLoop) closeProviderStep(events []EventArgs) (providerTurn, []EventArgs, error) {
	staged := make([]EventArgs, 0, len(events))
	toolUses := []EventArgs{}
	toolUseIDs := map[string]bool{}
	assistantCount := 0
	assistantSeen := false
	stopReason := ""
	for _, event := range events {
		switch event.Type {
		case EventAssistantMessage:
			assistantCount++
			assistantSeen = true
			stopReason = stringValue(event.Payload["stop_reason"])
		case EventToolUse:
			if !assistantSeen {
				return providerTurn{}, nil, &ProviderResponseIntegrityError{
					Reason: "tool_use_before_assistant", Message: "provider emitted tool_use before its assistant message",
				}
			}
			id := stringValue(event.Payload["id"])
			if id == "" {
				return providerTurn{}, nil, &ProviderResponseIntegrityError{
					Reason: IntegrityMissingToolUseID, Message: "provider emitted a tool_use without an id",
				}
			}
			if toolUseIDs[id] {
				return providerTurn{}, nil, &ProviderResponseIntegrityError{
					Reason: IntegrityDuplicateToolUseID, Message: fmt.Sprintf("provider emitted duplicate tool_use id %q", id),
				}
			}
			if stringValue(event.Payload["name"]) == "" {
				return providerTurn{}, nil, &ProviderResponseIntegrityError{
					Reason: "missing_tool_name", Message: fmt.Sprintf("provider tool_use %q has no name", id),
				}
			}
			toolUseIDs[id] = true
			toolUses = append(toolUses, event)
		case EventToolResult:
			return providerTurn{}, nil, &ProviderResponseIntegrityError{
				Reason: IntegrityOrphanToolResult, Message: "provider response unexpectedly contained a tool_result",
			}
		}
		staged = append(staged, event)
	}
	if assistantCount != 1 {
		return providerTurn{}, nil, &ProviderResponseIntegrityError{
			Reason:  "assistant_message_count",
			Message: fmt.Sprintf("provider step produced %d assistant messages, want exactly one", assistantCount),
		}
	}
	if stopReason == "" {
		return providerTurn{}, nil, &ProviderResponseIntegrityError{
			Reason: "missing_stop_reason", Message: "provider assistant message has no normalized stop_reason",
		}
	}
	if stopReason == "tool_use" && len(toolUses) == 0 {
		return providerTurn{}, nil, &ProviderResponseIntegrityError{
			Reason: "missing_tool_use", Message: "provider ended with tool_use but emitted no complete tool calls",
		}
	}
	if len(toolUses) == 0 || stopReason == "tool_use" {
		return providerTurn{stopReason: stopReason}, staged, nil
	}
	for _, toolUse := range toolUses {
		id := stringValue(toolUse.Payload["id"])
		name := stringValue(toolUse.Payload["name"])
		staged = append(staged, EventArgs{Type: EventToolResult, Payload: map[string]any{
			"tool_use_id": id,
			"output":      nil,
			"error": fmt.Sprintf(
				"tool %s did not complete: assistant turn ended with stop_reason %s before a tool result was recorded",
				name, stopReason,
			),
			"error_class": "IncompleteToolResult",
			"reason":      "incomplete_tool_result",
			"stop_reason": stopReason,
		}})
	}
	return providerTurn{stopReason: stopReason, reason: RunReasonIncompleteToolBatch}, staged, nil
}

func (l AgentLoop) terminalRuntimeError() bool {
	for _, event := range l.Session.Log.Events() {
		if event.Type == EventRuntimeError && event.Payload["terminal"] == true {
			return true
		}
	}
	return false
}

func (l AgentLoop) runTurn() (providerTurn, error) {
	l.Session.Hooks.Invoke("pre_projection", map[string]any{"session": l.Session})
	if l.terminalRuntimeError() {
		return providerTurn{reason: "runtime_failed"}, nil
	}
	prepared, err := PrepareProviderCall(l.Session)
	if err != nil {
		return providerTurn{}, err
	}
	request, err := prepared.Project(l.Projection)
	if err != nil {
		if mismatch, ok := err.(CapabilityMismatchError); ok {
			l.appendRuntimeError("capability_mismatch", mismatch.Error())
			return providerTurn{reason: "runtime_failed"}, nil
		}
		return providerTurn{}, err
	}
	l.Session.Hooks.Invoke("post_projection", map[string]any{"session": l.Session, "request": request})
	l.Session.Observation.Emit("projection_invoked", map[string]any{
		"projection": projectionName(l.Projection),
		"log_size":   len(l.Session.Log.Events()),
		"request":    request,
	})
	events, providerOK, err := l.callProviderWithRetry(prepared, request)
	if err != nil {
		return providerTurn{}, err
	}
	if serr := l.Session.Log.StorageErr(); serr != nil {
		return providerTurn{}, &StorageWriteError{Cause: serr}
	}
	if !providerOK {
		return providerTurn{reason: "provider_failed"}, nil
	}
	turn, staged, err := l.closeProviderStep(events)
	if err != nil {
		return providerTurn{}, err
	}
	if _, err := l.Session.Log.AppendBatch(staged); err != nil {
		return providerTurn{}, &StorageWriteError{Cause: err}
	}
	return turn, nil
}

func (l AgentLoop) callProviderWithRetry(prepared PreparedTranscript, request map[string]any) ([]EventArgs, bool, error) {
	attempt := 1
	policy := DefaultRetryPolicy()
	if l.RetryPolicy != nil {
		policy = *l.RetryPolicy
	}
	for {
		events, err := l.runOneProviderAttempt(prepared, request)
		if err != nil {
			if _, integrityFailure := err.(*TranscriptIntegrityError); integrityFailure {
				return nil, false, err
			}
			decision := policy.Decide(err, attempt)
			if !decision.Retry {
				l.appendProviderError(err, attempt, true)
				if serr := l.Session.Log.StorageErr(); serr != nil {
					return nil, false, &StorageWriteError{Cause: serr}
				}
				return nil, false, nil
			}
			l.appendProviderError(err, attempt, false)
			if serr := l.Session.Log.StorageErr(); serr != nil {
				return nil, false, &StorageWriteError{Cause: serr}
			}
			var prepareErr error
			prepared, prepareErr = PrepareProviderCall(l.Session)
			if prepareErr != nil {
				return nil, false, prepareErr
			}
			request, prepareErr = prepared.Project(l.Projection)
			if prepareErr != nil {
				return nil, false, prepareErr
			}
			if decision.Delay > 0 {
				sleep(decision.Delay)
			}
			attempt++
			continue
		}
		return events, true, nil
	}
}

func (l AgentLoop) runOneProviderAttempt(prepared PreparedTranscript, request map[string]any) ([]EventArgs, error) {
	started := time.Now()
	providerKind := l.providerKind()
	l.Session.Hooks.Invoke("pre_provider_call", map[string]any{"session": l.Session, "request": request})
	if !prepared.current(l.Session) {
		return nil, &TranscriptIntegrityError{Violations: []IntegrityViolation{{
			Domain: IntegrityDomainEffectiveTranscript, Code: IntegrityPreparedTranscriptStale,
			EventSeq: prepared.NextSeq(),
			Message:  "session changed after provider preparation",
		}}}
	}
	l.Session.Observation.Emit("provider_called", map[string]any{
		"provider": providerKind,
		"request":  request,
	})
	staged := []EventArgs{}
	if l.StreamProvider != nil {
		err := l.StreamProvider.Call(request, func(event EventArgs) {
			if isStreamObservationEvent(event.Type) {
				l.handleStreamEventWithIdentity(event, providerKind, stringValue(request["model"]))
				return
			}
			if event.Type == EventAssistantMessage {
				event.Payload = normalizeAssistantPayload(event.Payload, providerKind, stringValue(request["model"]))
			}
			staged = append(staged, event)
		})
		if err != nil {
			l.Session.Observation.Emit("provider_failed", map[string]any{
				"provider":    providerKind,
				"duration_ms": float64(time.Since(started).Milliseconds()),
				"error":       err.Error(),
			})
			return nil, err
		}
		l.Session.Hooks.Invoke("post_provider_call", map[string]any{
			"session":  l.Session,
			"request":  request,
			"response": nil,
		})
		l.Session.Observation.Emit("provider_responded", map[string]any{
			"provider":    providerKind,
			"duration_ms": float64(time.Since(started).Milliseconds()),
			"response":    nil,
		})
	} else {
		response, err := l.Provider.Call(request)
		if err != nil {
			l.Session.Observation.Emit("provider_failed", map[string]any{
				"provider":    providerKind,
				"duration_ms": float64(time.Since(started).Milliseconds()),
				"error":       err.Error(),
			})
			return nil, err
		}
		l.Session.Hooks.Invoke("post_provider_call", map[string]any{
			"session":  l.Session,
			"request":  request,
			"response": response,
		})
		l.Session.Observation.Emit("provider_responded", map[string]any{
			"provider":    providerKind,
			"duration_ms": float64(time.Since(started).Milliseconds()),
			"response":    response,
		})
		events, err := l.Ingestor.Ingest(response)
		if err != nil {
			return nil, err
		}
		for _, event := range events {
			if event.Type == EventAssistantMessage {
				event.Payload = normalizeAssistantPayload(event.Payload, providerKind, stringValue(firstNonEmptyAny(event.Payload["model"], request["model"])))
			}
			staged = append(staged, event)
		}
	}
	return staged, nil
}

func (l AgentLoop) providerKind() string {
	if l.ProviderKind != "" {
		return l.ProviderKind
	}
	return providerName(l.Provider)
}

func streamProviderName(provider StreamProvider) string {
	if provider == nil {
		return "unknown"
	}
	if kinded, ok := provider.(interface{ Kind() string }); ok {
		if kind := kinded.Kind(); kind != "" {
			return kind
		}
	}
	switch provider.(type) {
	case AnthropicStreamProvider, *AnthropicStreamProvider:
		return "anthropic"
	case OpenAIStreamProvider, *OpenAIStreamProvider:
		return "openai"
	case GeminiStreamProvider, *GeminiStreamProvider:
		return "gemini"
	case OllamaStreamProvider, *OllamaStreamProvider:
		return "ollama"
	default:
		return "unknown"
	}
}

func (l AgentLoop) handleStreamEvent(args EventArgs) {
	l.handleStreamEventWithIdentity(args, "", "")
}

func (l AgentLoop) handleStreamEventWithIdentity(args EventArgs, provider, model string) {
	if isStreamObservationEvent(args.Type) {
		event := Event{Seq: -1, ID: "stream", Type: args.Type, Payload: args.Payload}
		l.Session.Observation.Emit("stream_event", map[string]any{"event": event})
		if l.OnStreamEvent != nil && isDeltaEvent(args.Type) {
			l.OnStreamEvent(event)
		}
		return
	}
	if args.Type == EventAssistantMessage {
		args.Payload = normalizeAssistantPayload(args.Payload, stringValue(firstNonEmptyAny(args.Payload["provider"], provider)), stringValue(firstNonEmptyAny(args.Payload["model"], model)))
	}
	l.Session.Log.Append(args.Type, args.Payload)
}

func isStreamObservationEvent(eventType EventType) bool {
	switch eventType {
	case EventAssistantTurnStarted,
		EventAssistantTextDelta,
		EventToolUseBegin,
		EventToolUseArgumentDelta,
		EventToolUseEnd,
		EventAssistantTurnDone,
		EventAssistantTurnFailed:
		return true
	default:
		return false
	}
}

// projectionName resolves a projection's identity for Observation events and
// durable error events. Projections self-identify via an optional
// Name() string method (all built-ins implement it), so custom Projection
// implementations are never reduced to "unknown" in the audit record; the
// type switch remains as a fallback for legacy wrappers.
func projectionName(projection Projection) string {
	if projection == nil {
		return "unknown"
	}
	if named, ok := projection.(interface{ Name() string }); ok {
		if name := named.Name(); name != "" {
			return name
		}
	}
	switch projection.(type) {
	case AnthropicProjection, *AnthropicProjection:
		return "anthropic"
	case OpenAIProjection, *OpenAIProjection:
		return "openai"
	case GeminiProjection, *GeminiProjection:
		return "gemini"
	default:
		return "unknown"
	}
}

type statusError interface {
	error
	HTTPStatus() int
}

type providerClassError interface {
	error
	ProviderErrorClass() string
}

func (l AgentLoop) appendProviderError(err error, attempt int, terminal bool) {
	l.Session.Log.Append(EventProviderError, map[string]any{
		"provider":    l.providerKind(),
		"status":      providerStatusPayload(err),
		"error_class": providerErrorClass(err),
		"message":     err.Error(),
		"attempt":     float64(attempt),
		"terminal":    terminal,
	})
}

func (l AgentLoop) appendRuntimeError(reason, message string) {
	l.Session.Log.Append(EventRuntimeError, map[string]any{
		"source":      "projection",
		"handler":     projectionName(l.Projection),
		"error_class": "CapabilityMismatchError",
		"message":     message,
		"reason":      reason,
		"terminal":    true,
	})
}

func providerStatus(err error) int {
	if typed, ok := err.(statusError); ok {
		return typed.HTTPStatus()
	}
	return 0
}

func providerStatusPayload(err error) any {
	if status := providerStatus(err); status > 0 {
		return float64(status)
	}
	return nil
}

func providerErrorClass(err error) string {
	if typed, ok := err.(providerClassError); ok {
		return typed.ProviderErrorClass()
	}
	if _, ok := err.(statusError); ok {
		return "Harnas::Providers::HTTPError"
	}
	return fmt.Sprintf("%T", err)
}

func (l AgentLoop) dispatchPendingTools() ([]Event, bool) {
	pending := l.pendingToolUses()
	if l.Runner == nil {
		return pending, false
	}
	// First pass: compose every tool_use's pre_tool_use decision. Per
	// 07-permission R7 composition is Refuse > RequestApproval > Allow, and
	// per R8 any pending_approval verdict pauses the batch atomically: no
	// tool_use executes, only approval_requested events are appended.
	decisionsByIndex := make([][]any, len(pending))
	type approvalRequest struct {
		toolUse     Event
		reason      string
		requestedBy string
	}
	requests := []approvalRequest{}
	for i, toolUse := range pending {
		decisions := l.Session.Hooks.Invoke("pre_tool_use", map[string]any{
			"session":  l.Session,
			"tool_use": toolUse,
		})
		decisionsByIndex[i] = decisions
		if denied, _ := deniedByHook(decisions); denied {
			continue
		}
		if isPending, reason, requestedBy := pendingApprovalByHook(decisions); isPending {
			requests = append(requests, approvalRequest{toolUse: toolUse, reason: reason, requestedBy: requestedBy})
		}
	}
	if len(requests) > 0 {
		for _, request := range requests {
			payload := map[string]any{
				"tool_use_id":  request.toolUse.Payload["id"],
				"reason":       nil,
				"requested_by": nil,
			}
			if request.reason != "" {
				payload["reason"] = request.reason
			}
			if request.requestedBy != "" {
				payload["requested_by"] = request.requestedBy
			}
			l.Session.Log.Append(EventApprovalRequested, payload)
		}
		return pending, true
	}
	for i, toolUse := range pending {
		decisions := decisionsByIndex[i]
		denied, reason := deniedByHook(decisions)
		if denied {
			if reason == "" {
				reason = "no reason given"
			}
			l.Session.Log.Append(EventToolResult, map[string]any{
				"tool_use_id": toolUse.Payload["id"],
				"output":      nil,
				"error":       "denied by hook: " + reason,
				"approval": map[string]any{
					"decision":     "rejected",
					"rule_matched": reason,
					"applied_diff": nil,
				},
			})
		} else {
			l.Runner.ParentSession = l.Session
			l.Runner.Run(toolUseWithArgumentOverrides(toolUse, decisions), l.Session.Log)
		}
		l.Session.Hooks.Invoke("post_tool_use", map[string]any{
			"session":     l.Session,
			"tool_use":    toolUse,
			"tool_result": l.matchingToolResult(toolUse),
			"denied":      denied,
		})
	}
	return pending, false
}

func toolUseWithArgumentOverrides(toolUse Event, decisions []any) Event {
	for _, decision := range decisions {
		result, ok := decision.(map[string]any)
		if !ok {
			continue
		}
		arguments, ok := result["arguments"].(map[string]any)
		if !ok {
			continue
		}
		payload := map[string]any{}
		for key, value := range toolUse.Payload {
			payload[key] = value
		}
		payload["arguments"] = arguments
		return Event{Seq: toolUse.Seq, ID: toolUse.ID, Type: toolUse.Type, Payload: payload}
	}
	return toolUse
}

func deniedByHook(decisions []any) (bool, string) {
	for _, decision := range decisions {
		result, ok := decision.(map[string]any)
		if !ok {
			continue
		}
		allow, ok := result["allow"].(bool)
		if !ok || allow {
			continue
		}
		reason, _ := result["reason"].(string)
		return true, reason
	}
	return false, ""
}

func (l AgentLoop) matchingToolResult(toolUse Event) *Event {
	id, _ := toolUse.Payload["id"].(string)
	events := l.Session.Log.Events()
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event.Type == EventToolResult && event.Payload["tool_use_id"] == id {
			return &event
		}
	}
	return nil
}

func (l AgentLoop) pendingToolUses() []Event {
	fulfilled := map[string]bool{}
	for _, event := range l.Session.Log.Events() {
		if event.Type == EventToolResult {
			if id, ok := event.Payload["tool_use_id"].(string); ok {
				fulfilled[id] = true
			}
		}
	}
	pending := []Event{}
	for _, event := range l.Session.Log.Events() {
		if event.Type != EventToolUse {
			continue
		}
		id, _ := event.Payload["id"].(string)
		if !fulfilled[id] {
			pending = append(pending, event)
		}
	}
	return pending
}
