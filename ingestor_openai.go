package harnas

import "encoding/json"

type OpenAIIngestor struct{}

func (OpenAIIngestor) Ingest(response map[string]any) ([]EventArgs, error) {
	choice := firstMap(response["choices"])
	message := asMap(choice["message"])
	text, _ := message["content"].(string)
	stopReason := normalizeOpenAIStop(choice["finish_reason"])
	reasoning := openAIReasoningBlocks(message)
	hasCarrierData := openAIMessageHasCarrierData(message)
	payload := map[string]any{
		"text":        text,
		"stop_reason": stopReason,
		"usage":       normalizeOpenAIUsage(response["usage"]),
		"provider":    "openai",
		"model":       stringValue(response["model"]),
	}
	if hasCarrierData {
		payload["content"] = []any{map[string]any{
			"type": "text",
			"text": text,
			"provider_parts": []any{providerCarrier("openai.chat_completions", 0, "openai.message_content",
				map[string]any{"content": text}, []string{"payload.content[0]"})},
		}}
	}
	if len(reasoning) > 0 {
		payload["reasoning"] = reasoning
	}
	if hasCarrierData && len(message) > 0 {
		payload["provider_items"] = []any{providerCarrier("openai.chat_completions", 0, "openai.chat_message", message, []string{"payload.content[0]", "payload.reasoning[0]"})}
	}
	events := []EventArgs{{
		Type:    EventAssistantMessage,
		Payload: payload,
	}}
	for _, call := range asSlice(message["tool_calls"]) {
		toolCall := asMap(call)
		function := asMap(toolCall["function"])
		var args map[string]any
		_ = json.Unmarshal([]byte(stringValue(function["arguments"])), &args)
		if args == nil {
			args = map[string]any{}
		}
		events = append(events, EventArgs{
			Type: EventToolUse,
			Payload: map[string]any{
				"id":        toolCall["id"],
				"name":      function["name"],
				"arguments": args,
			},
		})
	}
	return events, nil
}

func openAIReasoningBlocks(message map[string]any) []any {
	blocks := []any{}
	if reasoning := stringValue(message["reasoning"]); reasoning != "" {
		blocks = append(blocks, map[string]any{"type": "text", "text": reasoning})
	}
	for _, raw := range asSlice(message["reasoning_details"]) {
		detail := asMap(raw)
		text := stringValue(detail["text"])
		if text == "" {
			text = stringValue(detail["reasoning"])
		}
		if text == "" {
			text = stringValue(detail["content"])
		}
		if text != "" {
			out := map[string]any{
				"type": "text",
				"text": text,
			}
			if openAIReasoningDetailHasCarrierData(detail) {
				out["provider_parts"] = []any{providerCarrier("openai.chat_completions", len(blocks), "openai.reasoning_detail",
					detail, []string{"payload.reasoning[0]"})}
			}
			blocks = append(blocks, out)
		}
	}
	return blocks
}

func openAIMessageHasCarrierData(message map[string]any) bool {
	for _, raw := range asSlice(message["reasoning_details"]) {
		if openAIReasoningDetailHasCarrierData(asMap(raw)) {
			return true
		}
	}
	return false
}

func openAIReasoningDetailHasCarrierData(detail map[string]any) bool {
	for key := range detail {
		if key != "type" && key != "text" && key != "reasoning" && key != "content" {
			return true
		}
	}
	return false
}

func normalizeOpenAIStop(value any) string {
	if value == "tool_calls" || value == "function_call" {
		return "tool_use"
	}
	if value == "stop" {
		return "end_turn"
	}
	return "other"
}

func normalizeOpenAIUsage(value any) map[string]any {
	return NormalizeUsage(value)
}
