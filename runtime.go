package harnas

// Runtime is a convenience wrapper around manifest loading plus optional
// Session resume/save.
type Runtime struct {
	Loaded *LoadedManifest
}

type RuntimeConfig struct {
	ManifestPath string
	Options      ManifestOptions
	SessionPath  string
	Resume       bool
	Metadata     map[string]any
}

func NewRuntime(config RuntimeConfig) (*Runtime, error) {
	loaded, err := LoadManifest(config.ManifestPath, config.Options)
	if err != nil {
		return nil, err
	}
	if config.Resume && config.SessionPath != "" {
		session, err := LoadSession(config.SessionPath)
		if err != nil {
			return nil, err
		}
		loaded.Session = session
	}
	for key, value := range config.Metadata {
		loaded.Session.Metadata[key] = value
	}
	loaded.InstallStrategies()
	return &Runtime{Loaded: loaded}, nil
}

func (r *Runtime) Session() *Session {
	return r.Loaded.Session
}

func (r *Runtime) Registry() *Registry {
	return r.Loaded.Registry
}

func (r *Runtime) Agent() *Agent {
	return &Agent{Name: r.Loaded.Name, Session: r.Loaded.Session, Loaded: r.Loaded}
}

func (r *Runtime) Loop() AgentLoop {
	return r.Loaded.Loop()
}

func (r *Runtime) Save(path string) error {
	return r.Loaded.Session.Save(path)
}
