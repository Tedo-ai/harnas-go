package harnas

type Projection interface {
	Project(log *Log) (map[string]any, error)
}

type Provider interface {
	Call(request map[string]any) (map[string]any, error)
}

type Ingestor interface {
	Ingest(response map[string]any) ([]EventArgs, error)
}

type EventArgs struct {
	Type    EventType
	Payload map[string]any
}

type StreamProvider interface {
	Call(request map[string]any, emit func(EventArgs)) error
}

type MockProvider struct {
	Text string
}

func (p MockProvider) Call(_ map[string]any) (map[string]any, error) {
	text := p.Text
	if text == "" {
		text = "ok"
	}
	return map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": text},
		},
		"stop_reason": "end_turn",
		"usage":       map[string]any{"input_tokens": float64(0), "output_tokens": float64(0)},
	}, nil
}
