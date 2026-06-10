package conformance

import (
	"fmt"
	"reflect"

	harnas "github.com/Tedo-ai/harnas-go"
)

type ProviderHTTPError struct {
	Status int
	Body   string
}

func (e ProviderHTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body)
}

func (e ProviderHTTPError) HTTPStatus() int {
	return e.Status
}

type ProviderMalformedFrameError struct {
	Message string
}

func (e ProviderMalformedFrameError) Error() string {
	return e.Message
}

func (e ProviderMalformedFrameError) ProviderErrorClass() string {
	return "Harnas::Providers::Error"
}

type ScriptedProvider struct {
	responses []map[string]any
}

type ScriptedStreamProvider struct {
	streams []any
}

func NewScriptedStreamProvider(streams []any) *ScriptedStreamProvider {
	return &ScriptedStreamProvider{streams: append([]any(nil), streams...)}
}

func (p *ScriptedStreamProvider) Call(request map[string]any, emit func(harnas.EventArgs)) error {
	if len(p.streams) == 0 {
		return fmt.Errorf("scripted stream provider exhausted")
	}
	stream := p.streams[0]
	p.streams = p.streams[1:]
	if entry, ok := stream.(map[string]any); ok {
		if expected, ok := entry["expect_request"]; ok {
			actual := normalizeValue(request)
			if !reflect.DeepEqual(actual, normalizeValue(expected)) {
				return fmt.Errorf("request does not match expected: %#v != %#v", actual, normalizeValue(expected))
			}
			stream = entry["response"]
		}
	}
	for _, event := range streamEvents(stream) {
		if errorSpec, ok := event["error"].(map[string]any); ok {
			emit(harnas.EventArgs{
				Type: harnas.EventAssistantTurnFailed,
				Payload: map[string]any{
					"turn_id": stringValue(errorSpec["turn_id"]),
					"error":   stringValue(errorSpec["message"]),
				},
			})
			return ProviderHTTPError{
				Status: int(floatValue(errorSpec["status"])),
				Body:   stringValue(errorSpec["body"]),
			}
		}
		if frameSpec, ok := event["malformed_frame"].(map[string]any); ok {
			message := stringValue(frameSpec["message"])
			emit(harnas.EventArgs{
				Type: harnas.EventAssistantTurnFailed,
				Payload: map[string]any{
					"turn_id": stringValue(frameSpec["turn_id"]),
					"error":   message,
				},
			})
			return ProviderMalformedFrameError{Message: message}
		}
		emit(harnas.EventArgs{
			Type:    harnas.EventType(stringValue(event["type"])),
			Payload: asMap(event["payload"]),
		})
	}
	return nil
}

func streamEvents(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, asMap(item))
	}
	return out
}

func asMap(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{}
}

func asSlice(value any) []any {
	if typed, ok := value.([]any); ok {
		return typed
	}
	return nil
}

func stringValue(value any) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	return ""
}

func floatValue(value any) float64 {
	if typed, ok := value.(float64); ok {
		return typed
	}
	return 0
}

func NewScriptedProvider(responses []map[string]any) *ScriptedProvider {
	return &ScriptedProvider{responses: append([]map[string]any(nil), responses...)}
}

func (p *ScriptedProvider) Call(request map[string]any) (map[string]any, error) {
	if len(p.responses) == 0 {
		return nil, fmt.Errorf("scripted provider exhausted")
	}
	response := p.responses[0]
	p.responses = p.responses[1:]
	if _, ok := response["expect_request"]; ok {
		expected := normalizeValue(response["expect_request"])
		actual := normalizeValue(request)
		if !reflect.DeepEqual(actual, expected) {
			return nil, fmt.Errorf("request does not match expected: %#v != %#v", actual, expected)
		}
		response = asMap(response["response"])
	}
	if errorSpec, ok := response["error"].(map[string]any); ok {
		return nil, ProviderHTTPError{
			Status: int(floatValue(errorSpec["status"])),
			Body:   stringValue(errorSpec["body"]),
		}
	}
	return response, nil
}

func normalizeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, item := range typed {
			out[key] = normalizeValue(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = normalizeValue(item)
		}
		return out
	case []map[string]any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = normalizeValue(item)
		}
		return out
	case int:
		return float64(typed)
	default:
		return value
	}
}
