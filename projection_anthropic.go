package harnas

type AnthropicProjection struct {
	Model     string
	MaxTokens int
	System    string
	Registry  *Registry
}

func (p AnthropicProjection) Project(log *Log) (map[string]any, error) {
	messages := []map[string]any{}
	for _, event := range ApplyMutations(log) {
		text, _ := event.Payload["text"].(string)
		switch event.Type {
		case EventUserMessage, EventSummary:
			messages = append(messages, map[string]any{"role": "user", "content": text})
		case EventAssistantMessage:
			if text != "" {
				messages = append(messages, map[string]any{"role": "assistant", "content": text})
			}
		}
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
