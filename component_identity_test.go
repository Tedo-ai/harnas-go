package harnas

import "testing"

// Custom Projection/Provider implementations must self-identify in
// Observation events and durable error events instead of collapsing to
// "unknown" (issue #16). Built-ins keep their exact current names.

type customProjection struct{}

func (customProjection) Name() string { return "custom-proxy" }
func (customProjection) Project(_ *Log) (map[string]any, error) {
	return map[string]any{}, nil
}

type unnamedProjection struct{}

func (unnamedProjection) Project(_ *Log) (map[string]any, error) {
	return map[string]any{}, nil
}

type customProvider struct{}

func (customProvider) Kind() string { return "custom-router" }
func (customProvider) Call(_ map[string]any) (map[string]any, error) {
	return map[string]any{}, nil
}

func TestProjectionNameSelfIdentifies(t *testing.T) {
	cases := []struct {
		projection Projection
		want       string
	}{
		{AnthropicProjection{}, "anthropic"},
		{&AnthropicProjection{}, "anthropic"},
		{OpenAIProjection{}, "openai"},
		{GeminiProjection{}, "gemini"},
		{customProjection{}, "custom-proxy"},
		{unnamedProjection{}, "unknown"},
		{nil, "unknown"},
	}
	for _, tc := range cases {
		if got := projectionName(tc.projection); got != tc.want {
			t.Fatalf("projectionName(%T) = %q, want %q", tc.projection, got, tc.want)
		}
	}
}

func TestProviderNameSelfIdentifies(t *testing.T) {
	cases := []struct {
		provider Provider
		want     string
	}{
		{AnthropicProvider{}, "anthropic"},
		{OpenAIProvider{}, "openai"},
		{GeminiProvider{}, "gemini"},
		{OllamaProvider{}, "ollama"}, // previously "unknown" on the buffered path
		{customProvider{}, "custom-router"},
		{nil, "unknown"},
	}
	for _, tc := range cases {
		if got := providerName(tc.provider); got != tc.want {
			t.Fatalf("providerName(%T) = %q, want %q", tc.provider, got, tc.want)
		}
	}
}

func TestCustomProjectionNameReachesDurableRuntimeError(t *testing.T) {
	session := CreateSession(nil)
	loop := AgentLoop{Session: session, Projection: customProjection{}}
	loop.appendRuntimeError("capability_mismatch", "boom")
	events := session.Log.Events()
	last := events[len(events)-1]
	if last.Type != EventRuntimeError {
		t.Fatalf("expected runtime_error, got %s", last.Type)
	}
	if last.Payload["handler"] != "custom-proxy" {
		t.Fatalf("durable error handler = %v, want custom-proxy", last.Payload["handler"])
	}
}
