package harnas

type AnthropicProjection struct {
	Model     string
	MaxTokens int
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
	return map[string]any{
		"model":      p.Model,
		"max_tokens": p.MaxTokens,
		"messages":   messages,
	}, nil
}
