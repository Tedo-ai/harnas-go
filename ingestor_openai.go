package harnas

import "encoding/json"

type OpenAIIngestor struct{}

func (OpenAIIngestor) Ingest(response map[string]any) ([]EventArgs, error) {
	choice := firstMap(response["choices"])
	message := asMap(choice["message"])
	text, _ := message["content"].(string)
	stopReason := normalizeOpenAIStop(choice["finish_reason"])
	payload := map[string]any{
		"text":        text,
		"stop_reason": stopReason,
		"usage":       normalizeOpenAIUsage(response["usage"]),
	}
	if reasoning := openAIReasoningBlocks(message); len(reasoning) > 0 {
		payload["reasoning"] = reasoning
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
			blocks = append(blocks, map[string]any{"type": "text", "text": text})
		}
	}
	return blocks
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
	usage := asMap(value)
	return map[string]any{
		"input_tokens":  usage["prompt_tokens"],
		"output_tokens": usage["completion_tokens"],
	}
}
