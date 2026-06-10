package harnas

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAnthropicProviderPostsMessagesRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "sk-test" {
			t.Fatalf("missing api key")
		}
		if r.Header.Get("anthropic-version") != AnthropicAPIVersion {
			t.Fatalf("missing api version")
		}
		var body map[string]any
		mustDecode(t, r, &body)
		if body["model"] != "claude-test" {
			t.Fatalf("unexpected body: %#v", body)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`))
	}))
	defer server.Close()

	provider := AnthropicProvider{APIKey: "sk-test", Endpoint: server.URL}
	response, err := provider.Call(map[string]any{"model": "claude-test", "messages": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	if response["stop_reason"] != "end_turn" {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestOpenAIProviderUsesBearerAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("authorization") != "Bearer sk-test" {
			t.Fatalf("missing bearer auth")
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	provider := OpenAIProvider{APIKey: "sk-test", Endpoint: server.URL}
	if _, err := provider.Call(map[string]any{"model": "gpt-test", "messages": []any{}}); err != nil {
		t.Fatal(err)
	}
}

func TestGeminiProviderMovesModelIntoURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/gemini-test:generateContent") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("x-goog-api-key") != "sk-test" {
			t.Fatalf("missing gemini api key")
		}
		var body map[string]any
		mustDecode(t, r, &body)
		if _, ok := body["model"]; ok {
			t.Fatalf("model should not be in Gemini body: %#v", body)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`))
	}))
	defer server.Close()

	provider := GeminiProvider{APIKey: "sk-test", EndpointBase: server.URL}
	if _, err := provider.Call(map[string]any{"model": "gemini-test", "contents": []any{}}); err != nil {
		t.Fatal(err)
	}
}

func TestProviderHTTPErrorCarriesStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"unavailable"}`))
	}))
	defer server.Close()

	provider := OpenAIProvider{APIKey: "sk-test", Endpoint: server.URL}
	_, err := provider.Call(map[string]any{"model": "gpt-test", "messages": []any{}})
	httpErr, ok := err.(HTTPError)
	if !ok {
		t.Fatalf("expected HTTPError, got %T", err)
	}
	if httpErr.HTTPStatus() != http.StatusServiceUnavailable {
		t.Fatalf("unexpected status: %d", httpErr.HTTPStatus())
	}
}

func TestProviderRejectsInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer server.Close()

	provider := OpenAIProvider{APIKey: "sk-test", Endpoint: server.URL}
	_, err := provider.Call(map[string]any{"model": "gpt-test", "messages": []any{}})
	if err == nil || !strings.Contains(err.Error(), "invalid JSON response") {
		t.Fatalf("expected invalid JSON error, got %v", err)
	}
}

type blockingHTTPDoer struct{}

func (blockingHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	<-req.Context().Done()
	return nil, req.Context().Err()
}

func TestProviderPostJSONUsesRequestTimeout(t *testing.T) {
	previous := DefaultProviderHTTPTimeout
	DefaultProviderHTTPTimeout = 20 * time.Millisecond
	defer func() { DefaultProviderHTTPTimeout = previous }()

	_, err := postJSON(blockingHTTPDoer{}, "https://example.com", nil, map[string]any{})
	if err == nil {
		t.Fatal("expected timeout")
	}
}

func mustDecode(t *testing.T, r *http.Request, target any) {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		t.Fatal(err)
	}
}
