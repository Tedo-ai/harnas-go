package harnas

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	AnthropicEndpoint     = "https://api.anthropic.com/v1/messages"
	AnthropicAPIVersion   = "2023-06-01"
	OpenAIEndpoint        = "https://api.openai.com/v1/chat/completions"
	OllamaBaseURL         = "http://localhost:11434/v1"
	GeminiEndpointBase    = "https://generativelanguage.googleapis.com/v1beta/models"
	GeminiGenerateContent = "generateContent"
)

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
	return http.DefaultClient
}

func (p OpenAIProvider) client() HTTPDoer {
	if p.Client != nil {
		return p.Client
	}
	return http.DefaultClient
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
	return http.DefaultClient
}

func postJSON(client HTTPDoer, endpoint string, headers map[string]string, body map[string]any) (map[string]any, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
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

func providerName(provider Provider) string {
	if provider == nil {
		return "unknown"
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
