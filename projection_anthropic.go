package harnas

type AnthropicProjection struct {
	Model     string
	MaxTokens int
	System    string
	Registry  *Registry
}

func (p AnthropicProjection) Project(log *Log) (map[string]any, error) {
	groups := []map[string]any{}
	var current map[string]any
	for _, event := range ApplyMutations(log) {
		role, blocks := p.translate(event)
		if role == "" || len(blocks) == 0 {
			continue
		}
		if current != nil && current["role"] == role {
			current["blocks"] = append(asSlice(current["blocks"]), blocks...)
		} else {
			if current != nil {
				groups = append(groups, current)
			}
			current = map[string]any{"role": role, "blocks": blocks}
		}
	}
	if current != nil {
		groups = append(groups, current)
	}
	messages := []map[string]any{}
	for _, group := range groups {
		messages = append(messages, finalizeAnthropicGroup(stringValue(group["role"]), asSlice(group["blocks"])))
	}
	request := map[string]any{
		"model":      p.Model,
		"max_tokens": p.MaxTokens,
		"messages":   messages,
	}
	if p.System != "" {
		request["system"] = p.System
	}
	if p.Registry != nil && p.Registry.Size() > 0 {
		request["tools"] = anthropicToolDescriptors(p.Registry)
	}
	return request, nil
}

func (p AnthropicProjection) translate(event Event) (string, []any) {
	text, _ := event.Payload["text"].(string)
	switch event.Type {
	case EventUserMessage, EventSummary:
		return "user", []any{map[string]any{"type": "text", "text": text}}
	case EventAssistantMessage:
		blocks := anthropicReasoningBlocks(event)
		if text != "" {
			blocks = append(blocks, map[string]any{"type": "text", "text": text})
		}
		return "assistant", blocks
	case EventToolUse:
		return "assistant", []any{map[string]any{
			"type":  "tool_use",
			"id":    event.Payload["id"],
			"name":  event.Payload["name"],
			"input": asMap(event.Payload["arguments"]),
		}}
	case EventToolResult:
		block := map[string]any{
			"type":        "tool_result",
			"tool_use_id": event.Payload["tool_use_id"],
		}
		if errText := stringValue(event.Payload["error"]); errText != "" {
			block["content"] = errText
			block["is_error"] = true
		} else {
			block["content"] = stringValue(event.Payload["output"])
		}
		return "user", []any{block}
	default:
		return "", nil
	}
}

func finalizeAnthropicGroup(role string, blocks []any) map[string]any {
	if len(blocks) == 1 {
		if block := asMap(blocks[0]); block["type"] == "text" {
			return map[string]any{"role": role, "content": block["text"]}
		}
	}
	return map[string]any{"role": role, "content": blocks}
}

func anthropicReasoningBlocks(event Event) []any {
	blocks := []any{}
	for _, raw := range asSlice(event.Payload["reasoning"]) {
		block := asMap(raw)
		if block["type"] != "text" {
			continue
		}
		out := map[string]any{"type": "thinking", "thinking": stringValue(block["text"])}
		if signature := stringValue(block["signature"]); signature != "" {
			out["signature"] = signature
		}
		blocks = append(blocks, out)
	}
	return blocks
}

type OpenAIProjection struct {
	Model    string
	System   string
	Registry *Registry
}

func (p OpenAIProjection) Project(log *Log) (map[string]any, error) {
	messages := []map[string]any{}
	if p.System != "" {
		messages = append(messages, map[string]any{"role": "system", "content": p.System})
	}
	for _, event := range ApplyMutations(log) {
		text, _ := event.Payload["text"].(string)
		switch event.Type {
		case EventUserMessage, EventSummary:
			messages = append(messages, map[string]any{"role": "user", "content": text})
		case EventAssistantMessage:
			messages = append(messages, map[string]any{"role": "assistant", "content": text})
		}
	}
	request := map[string]any{
		"model":    p.Model,
		"messages": messages,
	}
	if p.Registry != nil && p.Registry.Size() > 0 {
		request["tools"] = openAIToolDescriptors(p.Registry)
	}
	return request, nil
}

type GeminiProjection struct {
	Model    string
	System   string
	Registry *Registry
}

func (p GeminiProjection) Project(log *Log) (map[string]any, error) {
	contents := []map[string]any{}
	for _, event := range ApplyMutations(log) {
		text, _ := event.Payload["text"].(string)
		switch event.Type {
		case EventUserMessage, EventSummary:
			contents = append(contents, map[string]any{
				"role":  "user",
				"parts": []map[string]any{{"text": text}},
			})
		case EventAssistantMessage:
			if text != "" {
				contents = append(contents, map[string]any{
					"role":  "model",
					"parts": []map[string]any{{"text": text}},
				})
			}
		}
	}
	request := map[string]any{
		"model":            p.Model,
		"contents":         contents,
		"generationConfig": map[string]any{"thinkingConfig": map[string]any{"thinkingBudget": float64(0)}},
	}
	if p.System != "" {
		request["systemInstruction"] = map[string]any{
			"parts": []map[string]any{{"text": p.System}},
		}
	}
	if p.Registry != nil && p.Registry.Size() > 0 {
		request["tools"] = geminiToolDescriptors(p.Registry)
	}
	return request, nil
}

func anthropicToolDescriptors(registry *Registry) []map[string]any {
	tools := []map[string]any{}
	for _, tool := range registry.Tools() {
		tools = append(tools, map[string]any{
			"name":         tool.Name,
			"description":  tool.Description,
			"input_schema": tool.InputSchema,
		})
	}
	return tools
}

func openAIToolDescriptors(registry *Registry) []map[string]any {
	tools := []map[string]any{}
	for _, tool := range registry.Tools() {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  tool.InputSchema,
			},
		})
	}
	return tools
}

func geminiToolDescriptors(registry *Registry) []map[string]any {
	declarations := []map[string]any{}
	for _, tool := range registry.Tools() {
		declarations = append(declarations, map[string]any{
			"name":        tool.Name,
			"description": tool.Description,
			"parameters":  tool.InputSchema,
		})
	}
	return []map[string]any{{"functionDeclarations": declarations}}
}
