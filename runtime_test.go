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
