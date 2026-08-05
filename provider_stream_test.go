package harnas

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
		writeAnthropicMessageStart(t, w, 1, "\n\n")
		writeAnthropicTextBlockStart(t, w, 0, "\n\n")
		writeSSE(t, w, map[string]any{
			"type": "content_block_delta", "index": 0,
			"delta": map[string]any{"type": "text_delta", "text": "he"},
		}, "\n\n")
		writeSSE(t, w, map[string]any{
			"type": "content_block_delta", "index": 0,
			"delta": map[string]any{"type": "text_delta", "text": "llo"},
		}, "\n\n")
		writeAnthropicBlockStop(t, w, 0, "\n\n")
		writeSSE(t, w, map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn"},
			"usage": map[string]any{"input_tokens": 1, "output_tokens": 2},
		}, "\n\n")
		writeAnthropicMessageStop(t, w, "\n\n")
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

func TestAnthropicStreamProviderAcceptsCompleteToolUseWithMaxTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		writeAnthropicMessageStart(t, w, 5, "\n\n")
		writeSSE(t, w, map[string]any{
			"type": "content_block_start", "index": 0,
			"content_block": map[string]any{"type": "tool_use", "id": "toolu_complete", "name": "write_file"},
		}, "\n\n")
		writeSSE(t, w, map[string]any{
			"type": "content_block_delta", "index": 0,
			"delta": map[string]any{"type": "input_json_delta", "partial_json": `{"path":"a.txt"}`},
		}, "\n\n")
		writeAnthropicBlockStop(t, w, 0, "\n\n")
		writeSSE(t, w, map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "max_tokens"},
			"usage": map[string]any{"output_tokens": 9},
		}, "\n\n")
		writeAnthropicMessageStop(t, w, "\n\n")
	}))
	defer server.Close()

	var events []EventArgs
	err := (AnthropicStreamProvider{APIKey: "sk-test", Endpoint: server.URL}).Call(
		map[string]any{"model": "claude-test", "messages": []any{}},
		func(event EventArgs) { events = append(events, event) },
	)
	if err != nil {
		t.Fatalf("complete tool stream must normalize successfully: %v", err)
	}
	var assistant, toolUse *EventArgs
	for index := range events {
		switch events[index].Type {
		case EventAssistantMessage:
			assistant = &events[index]
		case EventToolUse:
			toolUse = &events[index]
		}
	}
	if assistant == nil || assistant.Payload["stop_reason"] != "max_tokens" {
		t.Fatalf("assistant did not preserve max_tokens: %#v", assistant)
	}
	if toolUse == nil || toolUse.Payload["id"] != "toolu_complete" || toolUse.Payload["name"] != "write_file" {
		t.Fatalf("complete tool use was not consolidated: %#v", toolUse)
	}
	arguments := asMap(toolUse.Payload["arguments"])
	if arguments["path"] != "a.txt" {
		t.Fatalf("tool arguments = %#v", arguments)
	}
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
		writeAnthropicTextBlockStart(t, w, 0, "\n\n")
		writeSSE(t, w, map[string]any{
			"type": "content_block_delta", "index": 0,
			"delta": map[string]any{"type": "text_delta", "text": "ok"},
		}, "\n\n")
		writeAnthropicBlockStop(t, w, 0, "\n\n")
		writeSSE(t, w, map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn"},
			"usage": map[string]any{"output_tokens": 3},
		}, "\n\n")
		writeAnthropicMessageStop(t, w, "\n\n")
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

func TestAnthropicStreamProviderRejectsErrorEventInsideHTTP200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		writeSSE(t, w, map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    "overloaded_error",
				"message": "Overloaded",
			},
			"request_id": "req_stream_error",
		}, "\n\n")
	}))
	defer server.Close()

	var events []EventArgs
	err := (AnthropicStreamProvider{APIKey: "sk-test", Endpoint: server.URL}).Call(
		map[string]any{"model": "claude-test", "messages": []any{}},
		func(event EventArgs) { events = append(events, event) },
	)
	if err == nil {
		t.Fatal("expected the HTTP-200 error event to fail the provider call")
	}
	var streamErr ProviderStreamError
	if !errors.As(err, &streamErr) {
		t.Fatalf("expected ProviderStreamError, got %T: %v", err, err)
	}
	if streamErr.Type != "overloaded_error" || streamErr.Message != "Overloaded" ||
		streamErr.RequestID != "req_stream_error" || streamErr.Status != 529 {
		t.Fatalf("provider stream error lost fields: %#v", streamErr)
	}
	for _, event := range events {
		if event.Type == EventAssistantMessage {
			t.Fatalf("error stream produced a phantom assistant message: %#v", event)
		}
	}
	if events[len(events)-1].Type != EventAssistantTurnFailed {
		t.Fatalf("expected terminal assistant_turn_failed observation, got %#v", events)
	}
}

func TestAnthropicStreamProviderRejectsEmptyHTTP200Stream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var events []EventArgs
	err := (AnthropicStreamProvider{APIKey: "sk-test", Endpoint: server.URL}).Call(
		map[string]any{"model": "claude-test", "messages": []any{}},
		func(event EventArgs) { events = append(events, event) },
	)
	if err == nil {
		t.Fatal("expected an empty HTTP-200 stream to fail the provider call")
	}
	var protocolErr ProviderProtocolError
	if !errors.As(err, &protocolErr) {
		t.Fatalf("expected ProviderProtocolError, got %T: %v", err, err)
	}
	for _, event := range events {
		if event.Type == EventAssistantMessage {
			t.Fatalf("empty stream produced a phantom assistant message: %#v", event)
		}
	}
}

func TestAnthropicStreamProviderRejectsMalformedSSEJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {not-json}\n\n"))
	}))
	defer server.Close()

	var events []EventArgs
	err := (AnthropicStreamProvider{APIKey: "sk-test", Endpoint: server.URL}).Call(
		map[string]any{"model": "claude-test", "messages": []any{}},
		func(event EventArgs) { events = append(events, event) },
	)
	var protocolErr ProviderProtocolError
	if !errors.As(err, &protocolErr) {
		t.Fatalf("expected ProviderProtocolError, got %T: %v", err, err)
	}
	for _, event := range events {
		if event.Type == EventAssistantMessage {
			t.Fatalf("malformed stream produced a phantom assistant message: %#v", event)
		}
	}
}

func TestAnthropicStreamProviderRejectsTruncatedStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		writeAnthropicMessageStart(t, w, 7, "\n\n")
		writeAnthropicTextBlockStart(t, w, 0, "\n\n")
		writeSSE(t, w, map[string]any{
			"type": "content_block_delta", "index": 0,
			"delta": map[string]any{"type": "text_delta", "text": "partial"},
		}, "\n\n")
	}))
	defer server.Close()

	var events []EventArgs
	err := (AnthropicStreamProvider{APIKey: "sk-test", Endpoint: server.URL}).Call(
		map[string]any{"model": "claude-test", "messages": []any{}},
		func(event EventArgs) { events = append(events, event) },
	)
	var protocolErr ProviderProtocolError
	if !errors.As(err, &protocolErr) || !strings.Contains(protocolErr.Message, "message_stop") {
		t.Fatalf("expected missing-message_stop protocol error, got %T: %v", err, err)
	}
	for _, event := range events {
		if event.Type == EventAssistantMessage {
			t.Fatalf("truncated stream produced a phantom assistant message: %#v", event)
		}
	}
}

func TestAnthropicStreamProviderAllowsUnknownEventsWithinValidLifecycle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		writeAnthropicMessageStart(t, w, 1, "\n\n")
		writeSSE(t, w, map[string]any{"type": "future_event", "new_field": true}, "\n\n")
		writeAnthropicTextBlockStart(t, w, 0, "\n\n")
		writeSSE(t, w, map[string]any{
			"type": "content_block_delta", "index": 0,
			"delta": map[string]any{"type": "text_delta", "text": "ok"},
		}, "\n\n")
		writeAnthropicBlockStop(t, w, 0, "\n\n")
		writeSSE(t, w, map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn"},
			"usage": map[string]any{"output_tokens": 1},
		}, "\n\n")
		writeAnthropicMessageStop(t, w, "\n\n")
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
	assertStreamText(t, events, "ok")
}

func TestAgentLoopRetriesAnthropicHTTP200ErrorsWithoutPhantomAssistant(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.Header().Set("content-type", "text/event-stream")
		if attempts < 3 {
			writeSSE(t, w, map[string]any{
				"type": "error",
				"error": map[string]any{
					"type":    "overloaded_error",
					"message": "Overloaded",
				},
				"request_id": "req_retry",
			}, "\n\n")
			return
		}
		writeAnthropicMessageStart(t, w, 2, "\n\n")
		writeAnthropicTextBlockStart(t, w, 0, "\n\n")
		writeSSE(t, w, map[string]any{
			"type": "content_block_delta", "index": 0,
			"delta": map[string]any{"type": "text_delta", "text": "recovered"},
		}, "\n\n")
		writeAnthropicBlockStop(t, w, 0, "\n\n")
		writeSSE(t, w, map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn"},
			"usage": map[string]any{"output_tokens": 1},
		}, "\n\n")
		writeAnthropicMessageStop(t, w, "\n\n")
	}))
	defer server.Close()

	session := NewSession("ses_retry", NewLog(), nil)
	session.Log.Append(EventUserMessage, map[string]any{"text": "hello"})
	loop := AgentLoop{
		Session:        session,
		Projection:     AnthropicProjection{Model: "claude-test", MaxTokens: 32},
		StreamProvider: AnthropicStreamProvider{APIKey: "sk-test", Endpoint: server.URL},
		Ingestor:       AnthropicIngestor{},
		RetryPolicy: &RetryPolicy{
			MaxAttempts: 3,
			Backoff:     func(int) time.Duration { return 0 },
		},
		MaxTurns: 1,
	}
	reason, err := loop.Run()
	if err != nil {
		t.Fatal(err)
	}
	if reason != "end_turn" || attempts != 3 {
		t.Fatalf("unexpected recovery: reason=%q attempts=%d", reason, attempts)
	}
	assistantMessages := 0
	nonterminalProviderErrors := 0
	for _, event := range session.Log.Events() {
		switch event.Type {
		case EventAssistantMessage:
			assistantMessages++
			if event.Payload["text"] != "recovered" {
				t.Fatalf("unexpected assistant message: %#v", event)
			}
		case EventProviderError:
			if event.Payload["terminal"] == false {
				nonterminalProviderErrors++
			}
		}
	}
	if assistantMessages != 1 || nonterminalProviderErrors != 2 {
		t.Fatalf("unexpected retry log: assistant_messages=%d nonterminal_provider_errors=%d events=%#v",
			assistantMessages, nonterminalProviderErrors, session.Log.Events())
	}
}

func TestAgentLoopReturnsProviderFailedAfterAnthropicHTTP200RetriesExhausted(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.Header().Set("content-type", "text/event-stream")
		writeSSE(t, w, map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    "overloaded_error",
				"message": "Overloaded",
			},
			"request_id": "req_exhausted",
		}, "\n\n")
	}))
	defer server.Close()

	session := NewSession("ses_exhausted", NewLog(), nil)
	session.Log.Append(EventUserMessage, map[string]any{"text": "hello"})
	loop := AgentLoop{
		Session:        session,
		Projection:     AnthropicProjection{Model: "claude-test", MaxTokens: 32},
		StreamProvider: AnthropicStreamProvider{APIKey: "sk-test", Endpoint: server.URL},
		Ingestor:       AnthropicIngestor{},
		RetryPolicy: &RetryPolicy{
			MaxAttempts: 3,
			Backoff:     func(int) time.Duration { return 0 },
		},
		MaxTurns: 1,
	}
	reason, err := loop.Run()
	if err != nil {
		t.Fatal(err)
	}
	if reason != "provider_failed" || attempts != 3 {
		t.Fatalf("unexpected terminal failure: reason=%q attempts=%d", reason, attempts)
	}
	assistantMessages := 0
	terminalProviderErrors := 0
	for _, event := range session.Log.Events() {
		switch event.Type {
		case EventAssistantMessage:
			assistantMessages++
		case EventProviderError:
			if event.Payload["terminal"] == true {
				terminalProviderErrors++
			}
		}
	}
	if assistantMessages != 0 || terminalProviderErrors != 1 {
		t.Fatalf("unexpected terminal log: assistant_messages=%d terminal_provider_errors=%d events=%#v",
			assistantMessages, terminalProviderErrors, session.Log.Events())
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
		}, "\n\n")
		writeSSE(t, w, map[string]any{
			"choices": []any{map[string]any{"delta": map[string]any{
				"tool_calls": []any{map[string]any{
					"index":    0,
					"function": map[string]any{"arguments": ":\"a.txt\"}"},
				}},
			}}},
		}, "\n\n")
		writeSSE(t, w, map[string]any{
			"choices": []any{map[string]any{"finish_reason": "tool_calls"}},
		}, "\n\n")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
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

func writeAnthropicMessageStart(t *testing.T, w http.ResponseWriter, inputTokens int, separator string) {
	t.Helper()
	writeSSE(t, w, map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"usage": map[string]any{"input_tokens": inputTokens, "output_tokens": 0},
		},
	}, separator)
}

func writeAnthropicMessageStop(t *testing.T, w http.ResponseWriter, separator string) {
	t.Helper()
	writeSSE(t, w, map[string]any{"type": "message_stop"}, separator)
}

func writeAnthropicTextBlockStart(t *testing.T, w http.ResponseWriter, index int, separator string) {
	t.Helper()
	writeSSE(t, w, map[string]any{
		"type": "content_block_start", "index": index,
		"content_block": map[string]any{"type": "text", "text": ""},
	}, separator)
}

func writeAnthropicBlockStop(t *testing.T, w http.ResponseWriter, index int, separator string) {
	t.Helper()
	writeSSE(t, w, map[string]any{"type": "content_block_stop", "index": index}, separator)
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
