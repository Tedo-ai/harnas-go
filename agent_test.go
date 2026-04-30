package harnas

import (
	"path/filepath"
	"testing"
)

func TestAgentFromManifestChat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	mustWrite(t, path, `{
		"harnas_version":"0.1",
		"name":"agent-test",
		"provider":{"kind":"mock","max_tokens":128},
		"tools":[],
		"strategies":[]
	}`)

	agent, err := AgentFromManifest(path, ManifestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	response, err := agent.Chat("hello")
	if err != nil {
		t.Fatal(err)
	}
	if response.Text != "ok" {
		t.Fatalf("unexpected response: %#v", response)
	}
	if agent.Session.Log.Events()[0].Type != EventUserMessage {
		t.Fatalf("expected user message in log")
	}
}
