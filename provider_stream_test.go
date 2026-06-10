package harnas

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnthropicStreamProviderEmitsTextDeltas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("accept") != "text/event-stream" || r.Header.Get("x-api-key") != "sk-test" {
			t.Fatalf("unexpected headers")
		}
		var body map[string]any
		mustDecode(t, r, &body)
		if body["stream"] != true {
			t.Fatalf("expected stream request: %#v", body)
		}
		w.Header().Set("content-type", "text/event-stream")
		writeSSE(t, w, map[string]any{
			"type":  "content_block_delta",
			"delta": map[string]any{"type": "text_delta", "text": "he"},
		}, "\n\n")
		writeSSE(t, w, map[string]any{
			"type":  "content_block_delta",
			"delta": map[string]any{"type": "text_delta", "text": "llo"},
		}, "\n\n")
		writeSSE(t, w, map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn"},
			"usage": map[string]any{"input_tokens": 1, "output_tokens": 2},
		}, "\n\n")
	}))
	defer server.Close()

	var events []EventArgs
	err := (AnthropicStreamProvider{APIKey: "sk-test", Endpoint: server.URL}).Call(
		map[string]any{"model": "claude-test", "messages": []any{}},
		func(event EventArgs) { events = append(events, event) },
	)
	if err != nil {
		t.Fatal(err)
	}
	assertStreamText(t, events, "hello")
}

func TestAnthropicStreamProviderKeepsMessageStartUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		writeSSE(t, w, map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"usage": map[string]any{"input_tokens": 7, "output_tokens": 0},
			},
		}, "\n\n")
		writeSSE(t, w, map[string]any{
			"type":  "content_block_delta",
			"delta": map[string]any{"type": "text_delta", "text": "ok"},
		}, "\n\n")
		writeSSE(t, w, map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn"},
			"usage": map[string]any{"output_tokens": 3},
		}, "\n\n")
	}))
	defer server.Close()

	var events []EventArgs
	err := (AnthropicStreamProvider{APIKey: "sk-test", Endpoint: server.URL}).Call(
		map[string]any{"model": "claude-test", "messages": []any{}},
		func(event EventArgs) { events = append(events, event) },
	)
	if err != nil {
		t.Fatal(err)
	}
	done := events[len(events)-2]
	usage := asMap(done.Payload["usage"])
	if usage["input_tokens"] != float64(7) || usage["output_tokens"] != float64(3) {
		t.Fatalf("unexpected usage: %#v", usage)
	}
}

func TestOpenAIStreamProviderEmitsToolDeltas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("authorization") != "Bearer sk-test" {
			t.Fatalf("unexpected auth")
		}
		w.Header().Set("content-type", "text/event-stream")
		writeSSE(t, w, map[string]any{
			"choices": []any{map[string]any{"delta": map[string]any{
				"tool_calls": []any{map[string]any{
					"index": 0, "id": "call_1",
					"function": map[string]any{"name": "read_file", "arguments": "{\"path\""},
				}},
			}}},
		}, "\n")
		writeSSE(t, w, map[string]any{
			"choices": []any{map[string]any{"delta": map[string]any{
				"tool_calls": []any{map[string]any{
					"index":    0,
					"function": map[string]any{"arguments": ":\"a.txt\"}"},
				}},
			}}},
		}, "\n")
		writeSSE(t, w, map[string]any{
			"choices": []any{map[string]any{"finish_reason": "tool_calls"}},
		}, "\n")
	}))
	defer server.Close()

	var events []EventArgs
	err := (OpenAIStreamProvider{APIKey: "sk-test", Endpoint: server.URL}).Call(
		map[string]any{"model": "gpt-test", "messages": []any{}},
		func(event EventArgs) { events = append(events, event) },
	)
	if err != nil {
		t.Fatal(err)
	}
	last := events[len(events)-1]
	if last.Type != EventToolUse || last.Payload["id"] != "call_1" {
		t.Fatalf("expected final tool_use, got %#v", last)
	}
}

func TestGeminiStreamProviderAcceptsCRLFSSESeparator(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.String(), "/gemini-test:streamGenerateContent?alt=sse") {
			t.Fatalf("unexpected url: %s", r.URL.String())
		}
		w.Header().Set("content-type", "text/event-stream")
		writeSSE(t, w, map[string]any{
			"candidates": []any{map[string]any{
				"content":      map[string]any{"parts": []any{map[string]any{"text": "ok"}}},
				"finishReason": "STOP",
			}},
			"usageMetadata": map[string]any{"promptTokenCount": 1, "candidatesTokenCount": 1},
		}, "\r\n\r\n")
	}))
	defer server.Close()

	var events []EventArgs
	err := (GeminiStreamProvider{APIKey: "sk-test", EndpointBase: server.URL}).Call(
		map[string]any{"model": "gemini-test", "contents": []any{}},
		func(event EventArgs) { events = append(events, event) },
	)
	if err != nil {
		t.Fatal(err)
	}
	assertStreamText(t, events, "ok")
}

func TestStreamProviderFailureEmitsTurnFailed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"unavailable"}`))
	}))
	defer server.Close()

	var events []EventArgs
	err := (OpenAIStreamProvider{APIKey: "sk-test", Endpoint: server.URL}).Call(
		map[string]any{"model": "gpt-test", "messages": []any{}},
		func(event EventArgs) { events = append(events, event) },
	)
	if err == nil {
		t.Fatalf("expected error")
	}
	if events[len(events)-1].Type != EventAssistantTurnFailed {
		t.Fatalf("expected assistant_turn_failed, got %#v", events)
	}
}

func writeSSE(t *testing.T, w http.ResponseWriter, payload map[string]any, separator string) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("data: " + string(data) + separator))
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func assertStreamText(t *testing.T, events []EventArgs, expected string) {
	t.Helper()
	if len(events) < 3 {
		t.Fatalf("too few events: %#v", events)
	}
	last := events[len(events)-1]
	if last.Type != EventAssistantMessage || last.Payload["text"] != expected {
		t.Fatalf("expected final text %q, got %#v", expected, last)
	}
}
