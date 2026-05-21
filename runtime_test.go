package harnas

import (
	"path/filepath"
	"testing"
)

func TestRuntimeBuildsAgentWithMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	mustWrite(t, path, `{
		"harnas_version":"0.1",
		"name":"runtime-test",
		"provider":{"kind":"mock","max_tokens":128},
		"tools":[],
		"strategies":[]
	}`)

	runtime, err := NewRuntime(RuntimeConfig{
		ManifestPath: path,
		Metadata:     map[string]any{"trace_id": "tr_1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := runtime.Agent().Chat("hi")
	if err != nil {
		t.Fatal(err)
	}
	if response.Text != "ok" {
		t.Fatalf("unexpected response: %#v", response)
	}
	if runtime.Session().Metadata["trace_id"] != "tr_1" {
		t.Fatalf("metadata not applied: %#v", runtime.Session().Metadata)
	}
}

func TestRuntimeBuildsFromInMemoryManifest(t *testing.T) {
	runtime, err := NewRuntime(RuntimeConfig{
		Manifest: map[string]any{
			"harnas_version": "0.1",
			"name":           "runtime-map-test",
			"provider":       map[string]any{"kind": "mock", "max_tokens": 128},
			"tools":          []any{},
			"strategies":     []any{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Session().Metadata["manifest_name"] != "runtime-map-test" {
		t.Fatalf("runtime did not build from in-memory manifest: %#v", runtime.Session().Metadata)
	}
}

func TestRuntimeRequiresExactlyOneManifestSource(t *testing.T) {
	if _, err := NewRuntime(RuntimeConfig{}); err == nil || err.Error() != "RuntimeConfig requires either Manifest or ManifestPath" {
		t.Fatalf("expected missing manifest source error, got %v", err)
	}
	if _, err := NewRuntime(RuntimeConfig{
		Manifest:     map[string]any{},
		ManifestPath: "manifest.json",
	}); err == nil || err.Error() != "RuntimeConfig accepts Manifest OR ManifestPath, not both" {
		t.Fatalf("expected conflicting manifest source error, got %v", err)
	}
}

func TestRuntimeResumesSavedSession(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "manifest.json")
	mustWrite(t, manifest, `{
		"harnas_version":"0.1",
		"name":"runtime-test",
		"provider":{"kind":"mock","max_tokens":128},
		"tools":[],
		"strategies":[]
	}`)

	session := CreateSession(nil)
	session.Log.Append(EventUserMessage, map[string]any{"text": "old"})
	sessionPath := filepath.Join(dir, "session.jsonl")
	if err := session.Save(sessionPath); err != nil {
		t.Fatal(err)
	}

	runtime, err := NewRuntime(RuntimeConfig{
		ManifestPath: manifest,
		SessionPath:  sessionPath,
		Resume:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Session().ID != session.ID {
		t.Fatalf("expected resumed session id %s, got %s", session.ID, runtime.Session().ID)
	}
	if runtime.Session().Log.Events()[0].Payload["text"] != "old" {
		t.Fatalf("session was not resumed")
	}
}

func TestDefaultAttachmentRootUsesSessionPath(t *testing.T) {
	got := DefaultAttachmentRoot(filepath.Join("tmp", "run.jsonl"))
	want := filepath.Join("tmp", "run.attachments")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
