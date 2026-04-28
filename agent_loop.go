package harnas

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
	if l.StreamProvider != nil {
		err = l.StreamProvider.Call(request, func(event EventArgs) {
			l.Session.Log.Append(event.Type, event.Payload)
		})
		if err != nil {
			return "", err
		}
	} else {
		response, err := l.Provider.Call(request)
		if err != nil {
			return "", err
		}
		events, err := l.Ingestor.Ingest(response)
		if err != nil {
			return "", err
		}
		for _, event := range events {
			l.Session.Log.Append(event.Type, event.Payload)
		}
	}

	last, ok := l.Session.Log.LastAssistantMessage()
	if !ok {
		return "end_turn", nil
	}
	stopReason, _ := last.Payload["stop_reason"].(string)
	return stopReason, nil
}

func (l AgentLoop) dispatchPendingTools() []Event {
	if l.Runner == nil {
		return nil
	}
	pending := l.pendingToolUses()
	for _, toolUse := range pending {
		l.Runner.Run(toolUse, l.Session.Log)
	}
	return pending
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
