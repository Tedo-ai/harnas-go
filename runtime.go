package harnas

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Runtime is a convenience wrapper around manifest loading plus optional
// Session resume/save.
type Runtime struct {
	Loaded *LoadedManifest
}

type RuntimeConfig struct {
	Manifest     map[string]any
	ManifestPath string
	Options      ManifestOptions
	SessionPath  string
	Resume       bool
	Metadata     map[string]any
}

func NewRuntime(config RuntimeConfig) (*Runtime, error) {
	if config.Manifest == nil && config.ManifestPath == "" {
		return nil, fmt.Errorf("RuntimeConfig requires either Manifest or ManifestPath")
	}
	if config.Manifest != nil && config.ManifestPath != "" {
		return nil, fmt.Errorf("RuntimeConfig accepts Manifest OR ManifestPath, not both")
	}
	if config.Options.AttachmentStore == nil {
		config.Options.AttachmentStore = NewFilesystemStore(DefaultAttachmentRoot(config.SessionPath))
	}
	var loaded *LoadedManifest
	var err error
	if config.Manifest != nil {
		manifest, decodeErr := ManifestFromMap(config.Manifest)
		if decodeErr != nil {
			return nil, decodeErr
		}
		loaded, err = BuildManifest(manifest, config.Options)
	} else {
		loaded, err = LoadManifest(config.ManifestPath, config.Options)
	}
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

func DefaultAttachmentRoot(sessionPath string) string {
	if sessionPath != "" {
		ext := filepath.Ext(sessionPath)
		if ext != "" {
			return strings.TrimSuffix(sessionPath, ext) + ".attachments"
		}
		return sessionPath + ".attachments"
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	return filepath.Join(home, ".harnas", "runs", "attachments")
}
