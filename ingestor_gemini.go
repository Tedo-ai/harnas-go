package harnas

import "fmt"

type GeminiIngestor struct {
	counter int
}

func (g *GeminiIngestor) Ingest(response map[string]any) ([]EventArgs, error) {
	candidate := firstMap(response["candidates"])
	content := asMap(candidate["content"])
	text := ""
	reasoning := []any{}
	events := []EventArgs{}
	for _, raw := range asSlice(content["parts"]) {
		part := asMap(raw)
		if value, ok := part["text"].(string); ok {
			text += value
		}
		thought := stringValue(part["thought"])
		if thought == "" {
			thought = stringValue(part["thoughtSummary"])
		}
		if thought == "" {
			thought = stringValue(part["thought_summary"])
		}
		if thought != "" {
			reasoning = append(reasoning, map[string]any{"type": "text", "text": thought})
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
	contentBlocks := geminiContentBlocksWithCarriers(asSlice(content["parts"]))
	payload := map[string]any{
		"text":        text,
		"stop_reason": normalizeGeminiStop(candidate["finishReason"], len(events) > 0),
		"usage":       normalizeGeminiUsage(response["usageMetadata"]),
		"provider":    "gemini",
		"model":       stringValue(firstNonEmptyAny(response["modelVersion"], response["model"])),
	}
	if geminiBlocksHaveCarriers(contentBlocks) {
		payload["content"] = contentBlocks
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

func geminiBlocksHaveCarriers(blocks []any) bool {
	for _, raw := range blocks {
		if len(asSlice(asMap(raw)["provider_parts"])) > 0 {
			return true
		}
	}
	return false
}

func geminiContentBlocksWithCarriers(parts []any) []any {
	blocks := []any{}
	for _, raw := range parts {
		part := asMap(raw)
		text := stringValue(part["text"])
		if text == "" {
			continue
		}
		block := map[string]any{"type": "text", "text": text}
		if _, ok := part["thoughtSignature"]; ok || len(part) > 1 {
			block["provider_parts"] = []any{providerCarrier("gemini.generateContent", 0, "gemini.part", cloneMap(part), []string{"payload.content[0]"})}
		}
		blocks = append(blocks, block)
	}
	if len(blocks) == 0 {
		return []any{map[string]any{"type": "text", "text": ""}}
	}
	return blocks
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
	return NormalizeUsage(value)
}
