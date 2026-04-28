package harnas

type EventType string

const (
	EventUserMessage      EventType = "user_message"
	EventAssistantMessage EventType = "assistant_message"
)

type Event struct {
	Seq     int            `json:"seq"`
	Type    EventType      `json:"type"`
	Payload map[string]any `json:"payload"`
}
