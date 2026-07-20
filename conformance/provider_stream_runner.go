package conformance

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"

	harnas "github.com/Tedo-ai/harnas-go"
)

const providerStreamSchemaVersion = "harnas.provider-streams.v1"

type ProviderStreamReport struct {
	Cases    int
	Profiles int
}

type providerStreamCorpus struct {
	SchemaVersion    string                          `json:"schema_version"`
	ChunkingProfiles map[string]providerChunkProfile `json:"chunking_profiles"`
	Cases            []providerStreamCase            `json:"cases"`
}

type providerChunkProfile struct {
	Sizes  []int `json:"sizes"`
	Repeat bool  `json:"repeat"`
}

type providerStreamCase struct {
	ID               string                 `json:"id"`
	Provider         string                 `json:"provider"`
	Request          map[string]any         `json:"request"`
	Response         providerStreamResponse `json:"response"`
	ChunkingProfiles []string               `json:"chunking_profiles"`
	Expected         providerStreamExpected `json:"expected"`
}

type providerStreamResponse struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

type providerStreamExpected struct {
	Outcome string           `json:"outcome"`
	Events  []map[string]any `json:"events"`
	Failure map[string]any   `json:"failure"`
}

func RunProviderStreamCorpus(specRoot string) (ProviderStreamReport, error) {
	path := filepath.Join(specRoot, "conformance", "provider-streams", "corpus.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return ProviderStreamReport{}, fmt.Errorf("read provider-stream corpus: %w", err)
	}
	var corpus providerStreamCorpus
	if err := json.Unmarshal(raw, &corpus); err != nil {
		return ProviderStreamReport{}, fmt.Errorf("parse provider-stream corpus: %w", err)
	}
	if corpus.SchemaVersion != providerStreamSchemaVersion {
		return ProviderStreamReport{}, fmt.Errorf("unsupported provider-stream schema %q", corpus.SchemaVersion)
	}
	report := ProviderStreamReport{Cases: len(corpus.Cases)}
	for _, fixture := range corpus.Cases {
		if len(fixture.ChunkingProfiles) == 0 {
			return report, fmt.Errorf("%s: no chunking profiles", fixture.ID)
		}
		for _, profileName := range fixture.ChunkingProfiles {
			profile, ok := corpus.ChunkingProfiles[profileName]
			if !ok {
				return report, fmt.Errorf("%s: unknown chunking profile %q", fixture.ID, profileName)
			}
			report.Profiles++
			if err := runProviderStreamCase(fixture, profileName, profile); err != nil {
				return report, fmt.Errorf("%s/%s: %w", fixture.ID, profileName, err)
			}
		}
	}
	return report, nil
}

func runProviderStreamCase(fixture providerStreamCase, profileName string, profile providerChunkProfile) error {
	client := providerFixtureClient{
		status:  fixture.Response.Status,
		headers: fixture.Response.Headers,
		chunks:  splitProviderBytes([]byte(fixture.Response.Body), profile),
	}
	events := make([]harnas.EventArgs, 0, len(fixture.Expected.Events))
	emit := func(event harnas.EventArgs) { events = append(events, event) }
	var err error
	switch fixture.Provider {
	case "anthropic":
		err = (harnas.AnthropicStreamProvider{
			APIKey: "conformance-key", Endpoint: "https://provider.invalid/anthropic", Client: client,
		}).Call(fixture.Request, emit)
	case "openai":
		err = (harnas.OpenAIStreamProvider{
			APIKey: "conformance-key", Endpoint: "https://provider.invalid/openai", Client: client,
		}).Call(fixture.Request, emit)
	case "gemini":
		err = (harnas.GeminiStreamProvider{
			APIKey: "conformance-key", EndpointBase: "https://provider.invalid/gemini", Client: client,
		}).Call(fixture.Request, emit)
	default:
		return fmt.Errorf("unsupported provider %q", fixture.Provider)
	}

	actualEvents, normalizeErr := normalizeProviderEvents(events)
	if normalizeErr != nil {
		return normalizeErr
	}
	if !reflect.DeepEqual(actualEvents, fixture.Expected.Events) {
		return fmt.Errorf("event artifact mismatch\nexpected: %#v\nactual:   %#v", fixture.Expected.Events, actualEvents)
	}
	switch fixture.Expected.Outcome {
	case "success":
		if err != nil {
			return fmt.Errorf("expected success, got %T: %v", err, err)
		}
	case "failure":
		if err == nil {
			return errors.New("expected failure, got success")
		}
		actualFailure := normalizeProviderFailure(fixture.Provider, err)
		if !reflect.DeepEqual(actualFailure, fixture.Expected.Failure) {
			return fmt.Errorf("failure artifact mismatch\nexpected: %#v\nactual:   %#v", fixture.Expected.Failure, actualFailure)
		}
		for _, event := range actualEvents {
			switch event["type"] {
			case harnas.EventAssistantMessage, harnas.EventToolUse, harnas.EventAssistantTurnDone:
				return fmt.Errorf("failed stream produced durable/completed event %q", event["type"])
			}
		}
	default:
		return fmt.Errorf("unsupported expected outcome %q", fixture.Expected.Outcome)
	}
	_ = profileName
	return nil
}

type providerFixtureClient struct {
	status  int
	headers map[string]string
	chunks  [][]byte
}

func (c providerFixtureClient) Do(*http.Request) (*http.Response, error) {
	headers := make(http.Header, len(c.headers))
	for key, value := range c.headers {
		headers.Set(key, value)
	}
	return &http.Response{
		StatusCode: c.status,
		Header:     headers,
		Body:       io.NopCloser(&providerChunkReader{chunks: c.chunks}),
	}, nil
}

type providerChunkReader struct {
	chunks [][]byte
	index  int
	offset int
}

func (r *providerChunkReader) Read(destination []byte) (int, error) {
	if r.index >= len(r.chunks) {
		return 0, io.EOF
	}
	chunk := r.chunks[r.index]
	n := copy(destination, chunk[r.offset:])
	r.offset += n
	if r.offset == len(chunk) {
		r.index++
		r.offset = 0
	}
	return n, nil
}

func splitProviderBytes(body []byte, profile providerChunkProfile) [][]byte {
	if len(profile.Sizes) == 0 {
		return [][]byte{bytes.Clone(body)}
	}
	chunks := make([][]byte, 0)
	offset := 0
	sizeIndex := 0
	for offset < len(body) {
		if sizeIndex >= len(profile.Sizes) {
			if !profile.Repeat {
				chunks = append(chunks, bytes.Clone(body[offset:]))
				break
			}
			sizeIndex = 0
		}
		size := profile.Sizes[sizeIndex]
		sizeIndex++
		end := offset + size
		if end > len(body) {
			end = len(body)
		}
		chunks = append(chunks, bytes.Clone(body[offset:end]))
		offset = end
	}
	if len(chunks) == 0 {
		return [][]byte{{}}
	}
	return chunks
}

func normalizeProviderEvents(events []harnas.EventArgs) ([]map[string]any, error) {
	normalized := make([]map[string]any, 0, len(events))
	for _, source := range events {
		raw, err := json.Marshal(source.Payload)
		if err != nil {
			return nil, fmt.Errorf("marshal provider event payload: %w", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("normalize provider event payload: %w", err)
		}
		if _, ok := payload["turn_id"]; ok {
			payload["turn_id"] = "<turn_id>"
		}
		if source.Type == harnas.EventAssistantTurnFailed {
			payload["error"] = "<provider_failure>"
		}
		normalized = append(normalized, map[string]any{"type": string(source.Type), "payload": payload})
	}
	return normalized, nil
}

func normalizeProviderFailure(provider string, err error) map[string]any {
	var streamError harnas.ProviderStreamError
	if errors.As(err, &streamError) {
		return map[string]any{
			"kind":                "provider_stream_error",
			"provider":            provider,
			"reason":              "provider_error_frame",
			"provider_error_type": streamError.Type,
			"request_id":          streamError.RequestID,
			"status":              float64(streamError.Status),
		}
	}
	var protocolError harnas.ProviderProtocolError
	if errors.As(err, &protocolError) {
		return map[string]any{
			"kind":     "provider_protocol_error",
			"provider": provider,
			"reason":   protocolError.Reason,
		}
	}
	var httpError harnas.HTTPError
	if errors.As(err, &httpError) {
		return map[string]any{
			"kind":     "http_error",
			"provider": provider,
			"reason":   "http_status",
			"status":   float64(httpError.Status),
		}
	}
	return map[string]any{
		"kind":     "network_error",
		"provider": provider,
		"reason":   "transport",
	}
}
