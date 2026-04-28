package harnas

import "fmt"

type GeminiIngestor struct {
	counter int
}

func (g *GeminiIngestor) Ingest(response map[string]any) ([]EventArgs, error) {
	candidate := firstMap(response["candidates"])
	content := asMap(candidate["content"])
	text := ""
	events := []EventArgs{}
	for _, raw := range asSlice(content["parts"]) {
		part := asMap(raw)
		if value, ok := part["text"].(string); ok {
			text += value
		}
		if call, ok := part["functionCall"]; ok {
			functionCall := asMap(call)
			name := stringValue(functionCall["name"])
			id := fmt.Sprintf("gemini.%s.%d", name, g.counter)
			g.counter++
			events = append(events, EventArgs{
				Type: EventToolUse,
				Payload: map[string]any{
					"id":        id,
					"name":      name,
					"arguments": asMap(functionCall["args"]),
				},
			})
		}
	}
	result := []EventArgs{{
		Type: EventAssistantMessage,
		Payload: map[string]any{
			"text":        text,
			"stop_reason": normalizeGeminiStop(candidate["finishReason"], len(events) > 0),
			"usage":       normalizeGeminiUsage(response["usageMetadata"]),
		},
	}}
	result = append(result, events...)
	return result, nil
}

func normalizeGeminiStop(value any, hasToolUse bool) string {
	if hasToolUse {
		return "tool_use"
	}
	if value == "STOP" {
		return "end_turn"
	}
	return "other"
}

func normalizeGeminiUsage(value any) map[string]any {
	usage := asMap(value)
	return map[string]any{
		"input_tokens":  usage["promptTokenCount"],
		"output_tokens": usage["candidatesTokenCount"],
	}
}
