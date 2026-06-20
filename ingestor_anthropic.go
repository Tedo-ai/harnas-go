package harnas

type AnthropicIngestor struct{}

func (AnthropicIngestor) Ingest(response map[string]any) ([]EventArgs, error) {
	text := ""
	reasoning := []any{}
	events := []EventArgs{}
	content := asSlice(response["content"])
	hasCarrierData := false
	for _, block := range content {
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
				hasCarrierData = true
			}
			if hasCarrierData {
				out["provider_parts"] = []any{providerCarrier("anthropic.messages", 0, "anthropic.content_block", cloneMap(part), []string{"payload.reasoning[0]"})}
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
	if text != "" && hasCarrierData {
		payload["content"] = []any{map[string]any{
			"type": "text",
			"text": text,
			"provider_parts": []any{providerCarrier("anthropic.messages", 0, "anthropic.content_block",
				map[string]any{"type": "text", "text": text}, []string{"payload.content[0]"})},
		}}
	}
	if len(reasoning) > 0 {
		payload["reasoning"] = reasoning
	}
	carrierContent := anthropicCarrierContent(content)
	if hasCarrierData && len(carrierContent) > 0 {
		refs := []string{"payload.reasoning[0]"}
		if text != "" {
			refs = append(refs, "payload.content[0]")
		}
		payload["provider_items"] = []any{providerCarrier("anthropic.messages", 0, "anthropic.content", carrierContent, refs)}
	}

	result := []EventArgs{{
		Type:    EventAssistantMessage,
		Payload: payload,
	}}
	result = append(result, events...)
	return result, nil
}

func anthropicCarrierContent(content []any) []any {
	out := []any{}
	for _, raw := range content {
		part := asMap(raw)
		if part["type"] == "tool_use" {
			continue
		}
		out = append(out, raw)
	}
	return out
}

func asSlice(value any) []any {
	if items, ok := value.([]any); ok {
		return items
	}
	return nil
}
