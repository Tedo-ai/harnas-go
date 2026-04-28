package harnas

import "fmt"

type AgentLoop struct {
	Session        *Session
	Projection     Projection
	Provider       Provider
	Ingestor       Ingestor
	StreamProvider StreamProvider
	Runner         *Runner
	MaxTurns       int
}

func (l AgentLoop) Run() (string, error) {
	maxTurns := l.MaxTurns
	if maxTurns == 0 {
		maxTurns = 10
	}
	reason := "max_turns_reached"
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
	}
	return reason, nil
}

func (l AgentLoop) runTurn() (string, error) {
	l.Session.Hooks.Invoke("pre_projection", map[string]any{"session": l.Session})
	request, err := l.Projection.Project(l.Session.Log)
	if err != nil {
		return "", err
	}
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
	for {
		if err := l.runOneProviderAttempt(request); err != nil {
			terminal := !retryableProviderError(err, attempt)
			l.appendProviderError(err, attempt, terminal)
			if terminal {
				return false
			}
			attempt++
			continue
		}
		return true
	}
}

func (l AgentLoop) runOneProviderAttempt(request map[string]any) error {
	if l.StreamProvider != nil {
		err := l.StreamProvider.Call(request, func(event EventArgs) {
			l.Session.Log.Append(event.Type, event.Payload)
		})
		if err != nil {
			return err
		}
	} else {
		response, err := l.Provider.Call(request)
		if err != nil {
			return err
		}
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

type statusError interface {
	error
	HTTPStatus() int
}

func retryableProviderError(err error, attempt int) bool {
	if attempt >= 3 {
		return false
	}
	status := providerStatus(err)
	switch status {
	case 408, 429, 500, 502, 503, 504:
		return true
	default:
		return false
	}
}

func (l AgentLoop) appendProviderError(err error, attempt int, terminal bool) {
	l.Session.Log.Append(EventProviderError, map[string]any{
		"provider":    "unknown",
		"status":      providerStatusPayload(err),
		"error_class": providerErrorClass(err),
		"message":     err.Error(),
		"attempt":     float64(attempt),
		"terminal":    terminal,
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
			})
		} else {
			l.Runner.Run(toolUse, l.Session.Log)
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
