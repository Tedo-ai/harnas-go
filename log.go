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
	storage     StorageAdapter
	storageErr  error
}

func NewLog() *Log {
	return &Log{events: []Event{}}
}

// BindStorage attaches a write-through durability sink: every Append becomes
// durable in the adapter (under the OCC fence) before the event is visible in
// memory. Prefer Session.BindStorage, which also persists the header and
// backfills existing events.
func (l *Log) BindStorage(adapter StorageAdapter) {
	l.storage = adapter
}

// StorageErr reports the latched write-through failure, if any. Once a
// write-through append fails, the Log refuses further appends until the latch
// is cleared; AgentLoop.Run surfaces the latch as a *StorageWriteError.
func (l *Log) StorageErr() error {
	return l.storageErr
}

// ClearStorageErr clears the write-through failure latch. The failed event
// was neither persisted nor appended in memory, so after the storage outage
// is resolved the turn can safely be retried. When in doubt, reload the
// session from the adapter with LoadSessionFromStorage instead.
func (l *Log) ClearStorageErr() {
	l.storageErr = nil
}

func (l *Log) Append(eventType EventType, payload map[string]any) Event {
	if l.storageErr != nil {
		return Event{}
	}
	seq := len(l.events)
	event := Event{
		ID:        eventID(seq, payload),
		Seq:       seq,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Type:      eventType,
		Payload:   payload,
	}
	if l.storage != nil {
		expected := seq
		row, err := l.storage.AppendEvent(EventDraft{
			ID:        event.ID,
			Timestamp: event.Timestamp,
			Type:      event.Type,
			Payload:   event.Payload,
		}, &expected)
		if err != nil {
			l.storageErr = err
			l.Observation.Emit("storage_write_failed", map[string]any{
				"seq":   float64(seq),
				"type":  string(eventType),
				"error": err.Error(),
			})
			return Event{}
		}
		event.ContentHash = row.ContentHash
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
