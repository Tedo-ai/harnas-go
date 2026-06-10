package harnas

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNetworkSandboxRefusesMalformedURLs(t *testing.T) {
	session := CreateSession(nil)
	NetworkSandbox{Allow: []string{"api.github.com"}}.Install(session)

	decisions := session.Hooks.Invoke("pre_tool_use", map[string]any{
		"tool_use": Event{
			Type: EventToolUse,
			Payload: map[string]any{
				"name":      "fetch_url",
				"arguments": map[string]any{"url": "http://[::1"},
			},
		},
		"session": session,
	})

	decision := decisions[0].(map[string]any)
	if decision["allow"] != false {
		t.Fatalf("expected malformed URL to be refused, got %#v", decision)
	}
	if !strings.Contains(decision["reason"].(string), "unparseable URL") {
		t.Fatalf("expected unparseable URL reason, got %#v", decision)
	}
}

func TestWriteSandboxRefusesSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires additional privileges on some Windows runners")
	}
	dir := t.TempDir()
	allowed := filepath.Join(dir, "allowed")
	outside := filepath.Join(dir, "outside")
	if err := os.MkdirAll(allowed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(allowed, "escape")); err != nil {
		t.Fatal(err)
	}
	session := CreateSession(nil)
	WriteSandbox{Allow: []string{allowed}}.Install(session)

	decisions := session.Hooks.Invoke("pre_tool_use", map[string]any{
		"tool_use": Event{
			Type: EventToolUse,
			Payload: map[string]any{
				"name":      "write_file",
				"arguments": map[string]any{"path": filepath.Join(allowed, "escape", "pwned.txt")},
			},
		},
		"session": session,
	})

	decision := decisions[0].(map[string]any)
	if decision["allow"] != false {
		t.Fatalf("expected symlink escape to be refused, got %#v", decision)
	}
}
