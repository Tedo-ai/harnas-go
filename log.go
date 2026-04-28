package harnas

type Log struct {
	events []Event
}

func NewLog() *Log {
	return &Log{events: []Event{}}
}

func (l *Log) Append(eventType EventType, payload map[string]any) Event {
	event := Event{Seq: len(l.events), Type: eventType, Payload: payload}
	l.events = append(l.events, event)
	return event
}

func (l *Log) Events() []Event {
	out := make([]Event, len(l.events))
	copy(out, l.events)
	return out
}

func (l *Log) LastAssistantMessage() (Event, bool) {
	for i := len(l.events) - 1; i >= 0; i-- {
		if l.events[i].Type == EventAssistantMessage {
			return l.events[i], true
		}
	}
	return Event{}, false
}

func (l *Log) Restore(event Event) {
	l.events = append(l.events, event)
}
