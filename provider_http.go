package harnas

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	AnthropicEndpoint     = "https://api.anthropic.com/v1/messages"
	AnthropicAPIVersion   = "2023-06-01"
	OpenAIEndpoint        = "https://api.openai.com/v1/chat/completions"
	OllamaBaseURL         = "http://localhost:11434/v1"
	GeminiEndpointBase    = "https://generativelanguage.googleapis.com/v1beta/models"
	GeminiGenerateContent = "generateContent"
)

var DefaultProviderHTTPTimeout = 60 * time.Second

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type ProviderError struct {
	Message string
}

func (e ProviderError) Error() string {
	return e.Message
}

type HTTPError struct {
	Status int
	Body   any
}

func (e HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %v", e.Status, e.Body)
}

func (e HTTPError) HTTPStatus() int {
	return e.Status
}

// ProviderStreamError is an error delivered inside an otherwise-successful
// streaming HTTP response. Some providers (notably Anthropic) can return HTTP
// 200, begin an SSE stream, and then emit a typed error event. Keeping the
// provider type, message, request id, and equivalent HTTP status lets the retry
// policy and downstream diagnostics treat that event exactly like the
// corresponding non-streaming failure.
type ProviderStreamError struct {
	Provider  string
	Type      string
	Message   string
	RequestID string
	Status    int
}

func (e ProviderStreamError) Error() string {
	provider := e.Provider
	if provider == "" {
		provider = "provider"
	}
	errorType := e.Type
	if errorType == "" {
		errorType = "stream_error"
	}
	message := e.Message
	if message == "" {
		message = "stream returned an error event"
	}
	if e.RequestID != "" {
		return fmt.Sprintf("%s stream error %s (request_id=%s): %s", provider, errorType, e.RequestID, message)
	}
	return fmt.Sprintf("%s stream error %s: %s", provider, errorType, message)
}

func (e ProviderStreamError) HTTPStatus() int { return e.Status }

func (e ProviderStreamError) ProviderErrorClass() string {
	return "Harnas::Providers::StreamError"
}

// ProviderProtocolError means a successful streaming HTTP response did not
// satisfy the provider's lifecycle contract: malformed JSON, a missing start or
// terminal event, or an invalid event order. It is retryable because no durable
// assistant message or tool call is emitted until the stream validates.
type ProviderProtocolError struct {
	Provider string
	Message  string
}

func (e ProviderProtocolError) Error() string {
	provider := e.Provider
	if provider == "" {
		provider = "provider"
	}
	return fmt.Sprintf("%s stream protocol error: %s", provider, e.Message)
}

func (e ProviderProtocolError) ProviderErrorClass() string {
	return "Harnas::Providers::ProtocolError"
}

func (e ProviderProtocolError) ProviderRetryable() bool { return true }

// Kind identifies this provider in Observation and provider_error events.
func (p AnthropicProvider) Kind() string { return "anthropic" }

type AnthropicProvider struct {
	APIKey     string
	APIVersion string
	Endpoint   string
	Client     HTTPDoer
}

func NewAnthropicProvider(apiKey string) AnthropicProvider {
	return AnthropicProvider{APIKey: apiKey}
}

func (p AnthropicProvider) Call(request map[string]any) (map[string]any, error) {
	endpoint := p.Endpoint
	if endpoint == "" {
		endpoint = AnthropicEndpoint
	}
	apiVersion := p.APIVersion
	if apiVersion == "" {
		apiVersion = AnthropicAPIVersion
	}
	return postJSON(p.client(), endpoint, map[string]string{
		"x-api-key":         p.APIKey,
		"anthropic-version": apiVersion,
		"content-type":      "application/json",
		"accept":            "application/json",
	}, request)
}

// Kind identifies this provider in Observation and provider_error events.
func (p OpenAIProvider) Kind() string { return "openai" }

type OpenAIProvider struct {
	APIKey   string
	Endpoint string
	Client   HTTPDoer
	NoAuth   bool
}

func NewOpenAIProvider(apiKey string) OpenAIProvider {
	return OpenAIProvider{APIKey: apiKey}
}

func (p OpenAIProvider) Call(request map[string]any) (map[string]any, error) {
	endpoint := p.Endpoint
	if endpoint == "" {
		endpoint = OpenAIEndpoint
	}
	headers := map[string]string{
		"content-type": "application/json",
		"accept":       "application/json",
	}
	if !p.NoAuth {
		headers["authorization"] = "Bearer " + p.APIKey
	}
	return postJSON(p.client(), endpoint, headers, request)
}

// Kind identifies this provider in Observation and provider_error events.
func (p OllamaProvider) Kind() string { return "ollama" }

type OllamaProvider struct {
	BaseURL string
	Client  HTTPDoer
}

func NewOllamaProvider(baseURL string) OllamaProvider {
	return OllamaProvider{BaseURL: baseURL}
}

func (p OllamaProvider) Call(request map[string]any) (map[string]any, error) {
	return (OpenAIProvider{
		Endpoint: ollamaChatEndpoint(p.BaseURL),
		Client:   p.Client,
		NoAuth:   true,
	}).Call(request)
}

// Kind identifies this provider in Observation and provider_error events.
func (p GeminiProvider) Kind() string { return "gemini" }

type GeminiProvider struct {
	APIKey       string
	EndpointBase string
	Client       HTTPDoer
}

func NewGeminiProvider(apiKey string) GeminiProvider {
	return GeminiProvider{APIKey: apiKey}
}

func (p GeminiProvider) Call(request map[string]any) (map[string]any, error) {
	model, ok := request["model"].(string)
	if !ok || model == "" {
		return nil, ProviderError{Message: "Gemini request must include 'model'"}
	}
	endpointBase := p.EndpointBase
	if endpointBase == "" {
		endpointBase = GeminiEndpointBase
	}
	body := copyMap(request)
	delete(body, "model")
	return postJSON(p.client(), fmt.Sprintf("%s/%s:%s", endpointBase, model, GeminiGenerateContent), map[string]string{
		"x-goog-api-key": p.APIKey,
		"content-type":   "application/json",
		"accept":         "application/json",
	}, body)
}

func (p AnthropicProvider) client() HTTPDoer {
	if p.Client != nil {
		return p.Client
	}
	return &http.Client{Timeout: DefaultProviderHTTPTimeout}
}

func (p OpenAIProvider) client() HTTPDoer {
	if p.Client != nil {
		return p.Client
	}
	return &http.Client{Timeout: DefaultProviderHTTPTimeout}
}

func ollamaChatEndpoint(baseURL string) string {
	base := strings.TrimRight(baseURL, "/")
	if base == "" {
		base = OllamaBaseURL
	}
	if strings.HasSuffix(base, "/v1") {
		return base + "/chat/completions"
	}
	return base + "/v1/chat/completions"
}

func (p GeminiProvider) client() HTTPDoer {
	if p.Client != nil {
		return p.Client
	}
	return &http.Client{Timeout: DefaultProviderHTTPTimeout}
}

func postJSON(client HTTPDoer, endpoint string, headers map[string]string, body map[string]any) (map[string]any, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), DefaultProviderHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	parsed, err := parseJSONBody(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, HTTPError{Status: response.StatusCode, Body: parsed}
	}
	return parsed, nil
}

func parseJSONBody(body io.Reader) (map[string]any, error) {
	var parsed map[string]any
	decoder := json.NewDecoder(body)
	if err := decoder.Decode(&parsed); err != nil {
		return nil, ProviderError{Message: "invalid JSON response: " + err.Error()}
	}
	return parsed, nil
}

func copyMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

// providerName resolves a provider's identity for Observation and
// provider_error events. Providers self-identify via an optional
// Kind() string method (all built-ins implement it, including Ollama —
// previously reported "unknown" on the buffered path); the type switch
// remains as a fallback for legacy wrappers.
func providerName(provider Provider) string {
	if provider == nil {
		return "unknown"
	}
	if kinded, ok := provider.(interface{ Kind() string }); ok {
		if kind := kinded.Kind(); kind != "" {
			return kind
		}
	}
	switch provider.(type) {
	case AnthropicProvider, *AnthropicProvider:
		return "anthropic"
	case OpenAIProvider, *OpenAIProvider:
		return "openai"
	case GeminiProvider, *GeminiProvider:
		return "gemini"
	default:
		return "unknown"
	}
}
