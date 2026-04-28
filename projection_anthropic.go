package harnas

type AnthropicProjection struct {
	Model     string
	MaxTokens int
	System    string
}

func (p AnthropicProjection) Project(log *Log) (map[string]any, error) {
	messages := []map[string]any{}
	for _, event := range log.Events() {
		text, _ := event.Payload["text"].(string)
		switch event.Type {
		case EventUserMessage:
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
	return request, nil
}

type OpenAIProjection struct {
	Model  string
	System string
}

func (p OpenAIProjection) Project(log *Log) (map[string]any, error) {
	messages := []map[string]any{}
	if p.System != "" {
		messages = append(messages, map[string]any{"role": "system", "content": p.System})
	}
	for _, event := range log.Events() {
		text, _ := event.Payload["text"].(string)
		switch event.Type {
		case EventUserMessage:
			messages = append(messages, map[string]any{"role": "user", "content": text})
		case EventAssistantMessage:
			messages = append(messages, map[string]any{"role": "assistant", "content": text})
		}
	}
	return map[string]any{
		"model":    p.Model,
		"messages": messages,
	}, nil
}

type GeminiProjection struct {
	Model  string
	System string
}

func (p GeminiProjection) Project(log *Log) (map[string]any, error) {
	contents := []map[string]any{}
	for _, event := range log.Events() {
		text, _ := event.Payload["text"].(string)
		switch event.Type {
		case EventUserMessage:
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
	return request, nil
}
