package harnas

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

type Log struct {
	events      []Event
	Observation *Observation
}

func NewLog() *Log {
	return &Log{events: []Event{}}
}

func (l *Log) Append(eventType EventType, payload map[string]any) Event {
	seq := len(l.events)
	event := Event{
		ID:        eventID(seq, payload),
		Seq:       seq,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Type:      eventType,
		Payload:   payload,
	}
	l.events = append(l.events, event)
	l.Observation.Emit("event_appended", map[string]any{"event": event, "log_size": len(l.events)})
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

func eventID(seq int, payload map[string]any) string {
	data, err := json.Marshal(payload)
	if err != nil {
		data = []byte(fmt.Sprintf("%v", payload))
	}
	digest := sha256.Sum256(data)
	return fmt.Sprintf("evt_%d_%x", seq, digest[:6])
}
