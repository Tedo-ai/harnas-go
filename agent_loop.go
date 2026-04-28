package harnas

type AgentLoop struct {
	Session    *Session
	Projection Projection
	Provider   Provider
	Ingestor   Ingestor
	MaxTurns   int
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
		reason = "no_pending_tools"
	}
	return reason, nil
}

func (l AgentLoop) runTurn() (string, error) {
	l.Session.Hooks.Invoke("pre_projection", map[string]any{"session": l.Session})
	request, err := l.Projection.Project(l.Session.Log)
	if err != nil {
		return "", err
	}
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
	last, ok := l.Session.Log.LastAssistantMessage()
	if !ok {
		return "end_turn", nil
	}
	stopReason, _ := last.Payload["stop_reason"].(string)
	return stopReason, nil
}
