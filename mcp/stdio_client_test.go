package mcp

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestStdioClientHandshakeToolsAndCall(t *testing.T) {
	script := fakeMCPServer(t, `
import json, sys
for line in sys.stdin:
    req=json.loads(line)
    if req["method"] == "initialize":
        print(json.dumps({"jsonrpc":"2.0","id":req["id"],"result":{}}), flush=True)
    elif req["method"] == "notifications/initialized":
        pass
    elif req["method"] == "tools/list":
        print(json.dumps({"jsonrpc":"2.0","id":req["id"],"result":{"tools":[{"name":"fetch_story","description":"Fetch a story","inputSchema":{"type":"object"}}]}}), flush=True)
    elif req["method"] == "tools/call":
        print(json.dumps({"jsonrpc":"2.0","id":req["id"],"result":{"content":[{"type":"text","text":"stdio body"}]}}), flush=True)
`)
	client, err := NewStdioClient(pythonCommand(), []string{script}, "editorial-ai")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	tools, err := client.Tools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "editorial-ai.fetch_story" {
		t.Fatalf("unexpected tools: %#v", tools)
	}
	output, err := client.CallTool(context.Background(), "fetch_story", map[string]any{"uid": "abc"})
	if err != nil || output != "stdio body" {
		t.Fatalf("unexpected output: %q %v", output, err)
	}
}

func TestStdioClientFailures(t *testing.T) {
	t.Run("spawn", func(t *testing.T) {
		_, err := NewStdioClient("/definitely/not/harnas-mcp", nil, "bad")
		var startup StartupError
		if !errors.As(err, &startup) {
			t.Fatalf("expected StartupError, got %T: %v", err, err)
		}
	})
	t.Run("exit-mid-call", func(t *testing.T) {
		script := fakeMCPServer(t, `
import json, sys
for line in sys.stdin:
    req=json.loads(line)
    if req["method"] == "initialize":
        print(json.dumps({"jsonrpc":"2.0","id":req["id"],"result":{}}), flush=True)
    elif req["method"] == "tools/list":
        sys.exit(0)
`)
		client, err := NewStdioClient(pythonCommand(), []string{script}, "bad")
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		if err := client.InitializeSession(context.Background()); err != nil {
			t.Fatal(err)
		}
		_, err = client.listTools(context.Background())
		var transport TransportError
		if !errors.As(err, &transport) {
			t.Fatalf("expected TransportError, got %T: %v", err, err)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		script := fakeMCPServer(t, `
import json, sys, time
for line in sys.stdin:
    req=json.loads(line)
    if req["method"] == "initialize":
        print(json.dumps({"jsonrpc":"2.0","id":req["id"],"result":{}}), flush=True)
    elif req["method"] == "tools/list":
        time.sleep(1)
`)
		client, err := NewStdioClient(pythonCommand(), []string{script}, "slow", WithStdioTimeout(2*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		if err := client.InitializeSession(context.Background()); err != nil {
			t.Fatal(err)
		}
		client.timeout = 20 * time.Millisecond
		_, err = client.listTools(context.Background())
		var timeout TimeoutError
		if !errors.As(err, &timeout) {
			t.Fatalf("expected TimeoutError, got %T: %v", err, err)
		}
	})
}

func fakeMCPServer(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "server.py")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func pythonCommand() string {
	for _, name := range []string{"python3", "python"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	if runtime.GOOS == "windows" {
		return "python"
	}
	return "python3"
}
