package harnas

type Response struct {
	Text       string
	StopReason string
	Log        *Log
}

type Agent struct {
	Name    string
	Session *Session
	Loaded  *LoadedManifest
}

func AgentFromManifest(path string, options ManifestOptions) (*Agent, error) {
	loaded, err := LoadManifest(path, options)
	if err != nil {
		return nil, err
	}
	loaded.InstallStrategies()
	return &Agent{Name: loaded.Name, Session: loaded.Session, Loaded: loaded}, nil
}

func AgentFromSession(session *Session, path string, options ManifestOptions) (*Agent, error) {
	loaded, err := LoadManifest(path, options)
	if err != nil {
		return nil, err
	}
	loaded.Session = session
	loaded.InstallStrategies()
	return &Agent{Name: loaded.Name, Session: loaded.Session, Loaded: loaded}, nil
}

func (a *Agent) Chat(text string) (Response, error) {
	a.Session.Log.Append(EventUserMessage, map[string]any{"text": text})
	loop := a.Loaded.Loop()
	loop.Session = a.Session
	reason, err := loop.Run()
	if err != nil {
		return Response{}, err
	}
	message, _ := a.Session.Log.LastAssistantMessage()
	reply, _ := message.Payload["text"].(string)
	stopReason, _ := message.Payload["stop_reason"].(string)
	if stopReason == "" {
		stopReason = reason
	}
	return Response{Text: reply, StopReason: stopReason, Log: a.Session.Log}, nil
}
