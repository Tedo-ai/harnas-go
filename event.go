package harnas

type EventType string

const (
	EventUserMessage          EventType = "user_message"
	EventAssistantMessage     EventType = "assistant_message"
	EventToolUse              EventType = "tool_use"
	EventToolResult           EventType = "tool_result"
	EventCompact              EventType = "compact"
	EventAnnotation           EventType = "annotation"
	EventAssistantTurnStarted EventType = "assistant_turn_started"
	EventAssistantTextDelta   EventType = "assistant_text_delta"
	EventToolUseBegin         EventType = "tool_use_begin"
	EventToolUseArgumentDelta EventType = "tool_use_argument_delta"
	EventToolUseEnd           EventType = "tool_use_end"
	EventAssistantTurnDone    EventType = "assistant_turn_completed"
)

type Event struct {
	Seq     int            `json:"seq"`
	Type    EventType      `json:"type"`
	Payload map[string]any `json:"payload"`
}
