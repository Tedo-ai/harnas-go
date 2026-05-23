package harnas

type AnthropicIngestor struct{}

func (AnthropicIngestor) Ingest(response map[string]any) ([]EventArgs, error) {
	text := ""
	reasoning := []any{}
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
		case "thinking":
			out := map[string]any{"type": "text", "text": stringValue(part["thinking"])}
			if signature := stringValue(part["signature"]); signature != "" {
				out["signature"] = signature
			}
			reasoning = append(reasoning, out)
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

	payload := map[string]any{
		"text":        text,
		"stop_reason": stopReason,
		"usage":       NormalizeUsage(response["usage"]),
		"provider":    "anthropic",
		"model":       stringValue(response["model"]),
	}
	if len(reasoning) > 0 {
		payload["reasoning"] = reasoning
	}

	result := []EventArgs{{
		Type:    EventAssistantMessage,
		Payload: payload,
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
