package harnas

type AnthropicIngestor struct{}

func (AnthropicIngestor) Ingest(response map[string]any) ([]EventArgs, error) {
	text := ""
	for _, block := range asSlice(response["content"]) {
		part, ok := block.(map[string]any)
		if !ok || part["type"] != "text" {
			continue
		}
		if chunk, ok := part["text"].(string); ok {
			text += chunk
		}
	}

	stopReason, _ := response["stop_reason"].(string)
	if stopReason == "" {
		stopReason = "other"
	}

	return []EventArgs{{
		Type: EventAssistantMessage,
		Payload: map[string]any{
			"text":        text,
			"stop_reason": stopReason,
			"usage":       normalizeUsage(response["usage"]),
		},
	}}, nil
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
