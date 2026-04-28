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
