package harnas

type AnthropicIngestor struct{}

func (AnthropicIngestor) Ingest(response map[string]any) ([]EventArgs, error) {
	text := ""
	events := []EventArgs{}
	for _, block := range asSlice(response["content"]) {
		part, ok := block.(map[string]any)
		if !ok {
			continue
		}
		switch part["type"] {
		case "text":
			chunk, _ := part["text"].(string)
			text += chunk
		case "tool_use":
			events = append(events, EventArgs{
				Type: EventToolUse,
				Payload: map[string]any{
					"id":        part["id"],
					"name":      part["name"],
					"arguments": asMap(part["input"]),
				},
			})
		}
	}

	stopReason, _ := response["stop_reason"].(string)
	if stopReason == "" {
		stopReason = "other"
	}

	result := []EventArgs{{
		Type: EventAssistantMessage,
		Payload: map[string]any{
			"text":        text,
			"stop_reason": stopReason,
			"usage":       normalizeUsage(response["usage"]),
		},
	}}
	result = append(result, events...)
	return result, nil
}

func asSlice(value any) []any {
	if items, ok := value.([]any); ok {
		return items
	}
	return nil
}

func normalizeUsage(value any) map[string]any {
	usage, ok := value.(map[string]any)
	if !ok {
		return map[string]any{"input_tokens": 0, "output_tokens": 0}
	}
	return map[string]any{
		"input_tokens":  usage["input_tokens"],
		"output_tokens": usage["output_tokens"],
	}
}
