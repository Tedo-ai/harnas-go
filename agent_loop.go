package harnas

import (
	"fmt"
	"time"
)

var sleep = time.Sleep

type AgentLoop struct {
	Session        *Session
	Projection     Projection
	Provider       Provider
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
	reason = "max_turns_reached"
	for range maxTurns {
		stopReason, err := l.runTurn()
		if err != nil {
			return "", err
		}
		if stopReason != "tool_use" {
			reason = "end_turn"
			break
		}
		if len(l.dispatchPendingTools()) == 0 {
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

func (l AgentLoop) terminalRuntimeError() bool {
	for _, event := range l.Session.Log.Events() {
		if event.Type == EventRuntimeError && event.Payload["terminal"] == true {
			return true
		}
	}
	return false
}

func (l AgentLoop) runTurn() (string, error) {
	l.Session.Hooks.Invoke("pre_projection", map[string]any{"session": l.Session})
	if l.terminalRuntimeError() {
		return "runtime_failed", nil
	}
	request, err := l.Projection.Project(l.Session.Log)
	if err != nil {
		if mismatch, ok := err.(CapabilityMismatchError); ok {
			l.appendRuntimeError("capability_mismatch", mismatch.Error())
			return "runtime_failed", nil
		}
		return "", err
	}
	l.Session.Hooks.Invoke("post_projection", map[string]any{"session": l.Session, "request": request})
	l.Session.Observation.Emit("projection_invoked", map[string]any{
		"projection": projectionName(l.Projection),
		"log_size":   len(l.Session.Log.Events()),
		"request":    request,
	})
	if ok := l.callProviderWithRetry(request); !ok {
		return "provider_failed", nil
	}

	last, ok := l.Session.Log.LastAssistantMessage()
	if !ok {
		return "end_turn", nil
	}
	stopReason, _ := last.Payload["stop_reason"].(string)
	return stopReason, nil
}

func (l AgentLoop) callProviderWithRetry(request map[string]any) bool {
	attempt := 1
	policy := DefaultRetryPolicy()
	if l.RetryPolicy != nil {
		policy = *l.RetryPolicy
	}
	for {
		if err := l.runOneProviderAttempt(request); err != nil {
			decision := policy.Decide(err, attempt)
			if !decision.Retry {
				l.appendProviderError(err, attempt, true)
				return false
			}
			l.appendProviderError(err, attempt, false)
			if decision.Delay > 0 {
				sleep(decision.Delay)
			}
			attempt++
			continue
		}
		return true
	}
}

func (l AgentLoop) runOneProviderAttempt(request map[string]any) error {
	started := time.Now()
	l.Session.Hooks.Invoke("pre_provider_call", map[string]any{"session": l.Session, "request": request})
	l.Session.Observation.Emit("provider_called", map[string]any{
		"provider": providerName(l.Provider),
		"request":  request,
	})
	if l.StreamProvider != nil {
		err := l.StreamProvider.Call(request, func(event EventArgs) {
			l.handleStreamEvent(event)
		})
		if err != nil {
			l.Session.Observation.Emit("provider_failed", map[string]any{
				"provider":    providerName(l.Provider),
				"duration_ms": float64(time.Since(started).Milliseconds()),
				"error":       err.Error(),
			})
			return err
		}
		l.Session.Hooks.Invoke("post_provider_call", map[string]any{
			"session":  l.Session,
			"request":  request,
			"response": nil,
		})
		l.Session.Observation.Emit("provider_responded", map[string]any{
			"provider":    providerName(l.Provider),
			"duration_ms": float64(time.Since(started).Milliseconds()),
			"response":    nil,
		})
	} else {
		response, err := l.Provider.Call(request)
		if err != nil {
			l.Session.Observation.Emit("provider_failed", map[string]any{
				"provider":    providerName(l.Provider),
				"duration_ms": float64(time.Since(started).Milliseconds()),
				"error":       err.Error(),
			})
			return err
		}
		l.Session.Hooks.Invoke("post_provider_call", map[string]any{
			"session":  l.Session,
			"request":  request,
			"response": response,
		})
		l.Session.Observation.Emit("provider_responded", map[string]any{
			"provider":    providerName(l.Provider),
			"duration_ms": float64(time.Since(started).Milliseconds()),
			"response":    response,
		})
		events, err := l.Ingestor.Ingest(response)
		if err != nil {
			return err
		}
		for _, event := range events {
			l.Session.Log.Append(event.Type, event.Payload)
		}
	}
	return nil
}

func (l AgentLoop) handleStreamEvent(args EventArgs) {
	if isStreamObservationEvent(args.Type) {
		event := Event{Seq: -1, ID: "stream", Type: args.Type, Payload: args.Payload}
		l.Session.Observation.Emit("stream_event", map[string]any{"event": event})
		if l.OnStreamEvent != nil && isDeltaEvent(args.Type) {
			l.OnStreamEvent(event)
		}
		return
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

func projectionName(projection Projection) string {
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

func (l AgentLoop) appendProviderError(err error, attempt int, terminal bool) {
	l.Session.Log.Append(EventProviderError, map[string]any{
		"provider":    providerName(l.Provider),
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
	if _, ok := err.(statusError); ok {
		return "Harnas::Providers::HTTPError"
	}
	return fmt.Sprintf("%T", err)
}

func (l AgentLoop) dispatchPendingTools() []Event {
	if l.Runner == nil {
		return nil
	}
	pending := l.pendingToolUses()
	for _, toolUse := range pending {
		decisions := l.Session.Hooks.Invoke("pre_tool_use", map[string]any{
			"session":  l.Session,
			"tool_use": toolUse,
		})
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
					"decision": "rejected",
					"reason":   reason,
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
	return pending
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
