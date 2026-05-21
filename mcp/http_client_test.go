package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPClientHandshakeToolsCallAndHeaders(t *testing.T) {
	var sawAuth bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer test" {
			sawAuth = true
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		switch request["method"] {
		case "initialize":
			writeJSON(t, w, map[string]any{"jsonrpc": "2.0", "id": request["id"], "result": map[string]any{}})
		case "notifications/initialized":
			writeJSON(t, w, map[string]any{"jsonrpc": "2.0", "result": map[string]any{}})
		case "tools/list":
			writeJSON(t, w, map[string]any{"jsonrpc": "2.0", "id": request["id"], "result": map[string]any{"tools": []any{map[string]any{
				"name": "fetch_story", "description": "Fetch a story", "inputSchema": map[string]any{"type": "object"},
			}}}})
		case "tools/call":
			writeJSON(t, w, map[string]any{"jsonrpc": "2.0", "id": request["id"], "result": map[string]any{
				"content": []any{map[string]any{"type": "text", "text": "story body"}},
			}})
		default:
			t.Fatalf("unexpected method: %v", request["method"])
		}
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "editorial-ai", WithHeaders(map[string]string{"Authorization": "Bearer test"}))
	tools, err := client.Tools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "editorial-ai.fetch_story" {
		t.Fatalf("unexpected tools: %#v", tools)
	}
	if !sawAuth {
		t.Fatal("expected custom header")
	}
	output, err := client.CallTool(context.Background(), "fetch_story", map[string]any{"uid": "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if output != "story body" {
		t.Fatalf("unexpected output: %q", output)
	}
	handler := client.ToolHandlers()["mcp_passthrough.editorial-ai"]
	output, err = handler(map[string]any{"uid": "abc"}, map[string]any{"mcp_tool_name": "fetch_story"})
	if err != nil || output != "story body" {
		t.Fatalf("unexpected handler result: %q %v", output, err)
	}
}

func TestHTTPClientTransportErrors(t *testing.T) {
	t.Run("non-200", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer server.Close()
		client := NewHTTPClient(server.URL, "bad")
		err := client.InitializeSession(context.Background())
		var transport TransportError
		if !errors.As(err, &transport) {
			t.Fatalf("expected TransportError, got %T: %v", err, err)
		}
	})
	t.Run("malformed-json", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("not-json"))
		}))
		defer server.Close()
		client := NewHTTPClient(server.URL, "bad")
		err := client.InitializeSession(context.Background())
		var transport TransportError
		if !errors.As(err, &transport) {
			t.Fatalf("expected TransportError, got %T: %v", err, err)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(100 * time.Millisecond)
			writeJSON(t, w, map[string]any{"jsonrpc": "2.0", "result": map[string]any{}})
		}))
		defer server.Close()
		client := NewHTTPClient(server.URL, "slow", WithHTTPTimeout(5*time.Millisecond))
		err := client.InitializeSession(context.Background())
		var timeout TimeoutError
		if !errors.As(err, &timeout) {
			t.Fatalf("expected TimeoutError, got %T: %v", err, err)
		}
	})
}

func TestHTTPClientDegradedStartup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()
	client := NewHTTPClient(server.URL, "bad")
	tools, err := client.Tools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 0 || !client.state.degraded {
		t.Fatalf("expected degraded empty tools")
	}
	if _, err := client.CallTool(context.Background(), "x", nil); err == nil {
		t.Fatal("expected degraded call error")
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatal(err)
	}
}
