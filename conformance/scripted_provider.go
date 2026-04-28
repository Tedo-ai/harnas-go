package conformance

import (
	"fmt"

	harnas "github.com/Tedo-ai/harnas-go"
)

type ScriptedProvider struct {
	responses []map[string]any
}

type ScriptedStreamProvider struct {
	streams [][]map[string]any
}

func NewScriptedStreamProvider(streams [][]map[string]any) *ScriptedStreamProvider {
	return &ScriptedStreamProvider{streams: append([][]map[string]any(nil), streams...)}
}

func (p *ScriptedStreamProvider) Call(_ map[string]any, emit func(harnas.EventArgs)) error {
	if len(p.streams) == 0 {
		return fmt.Errorf("scripted stream provider exhausted")
	}
	stream := p.streams[0]
	p.streams = p.streams[1:]
	for _, event := range stream {
		emit(harnas.EventArgs{
			Type:    harnas.EventType(stringValue(event["type"])),
			Payload: asMap(event["payload"]),
		})
	}
	return nil
}

func asMap(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{}
}

func stringValue(value any) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	return ""
}

func NewScriptedProvider(responses []map[string]any) *ScriptedProvider {
	return &ScriptedProvider{responses: append([]map[string]any(nil), responses...)}
}

func (p *ScriptedProvider) Call(_ map[string]any) (map[string]any, error) {
	if len(p.responses) == 0 {
		return nil, fmt.Errorf("scripted provider exhausted")
	}
	response := p.responses[0]
	p.responses = p.responses[1:]
	return response, nil
}
