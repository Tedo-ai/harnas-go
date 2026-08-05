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
	events, err := l.AppendBatch([]EventArgs{{Type: eventType, Payload: payload}})
	if err != nil || len(events) == 0 {
		return Event{}
	}
	return events[0]
}

// AppendBatch persists and publishes a semantic event group all-or-none at
// one OCC watermark. Unlike Append, it returns write failures directly.
func (l *Log) AppendBatch(args []EventArgs) ([]Event, error) {
	if l.storageErr != nil {
		return nil, l.storageErr
	}
	if len(args) == 0 {
		return []Event{}, nil
	}
	nextSeq := len(l.events)
	events := make([]Event, 0, len(args))
	drafts := make([]EventDraft, 0, len(args))
	for index, arg := range args {
		seq := nextSeq + index
		event := Event{
			ID:        eventID(seq, arg.Payload),
			Seq:       seq,
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Type:      arg.Type,
			Payload:   arg.Payload,
		}
		events = append(events, event)
		drafts = append(drafts, EventDraft{
			ID: event.ID, Timestamp: event.Timestamp, Type: event.Type, Payload: event.Payload,
		})
	}
	if l.storage != nil {
		expected := nextSeq
		rows, err := l.storage.AppendEvents(drafts, &expected)
		if err != nil {
			l.storageErr = err
			l.Observation.Emit("storage_write_failed", map[string]any{
				"seq":   float64(nextSeq),
				"count": float64(len(args)),
				"error": err.Error(),
			})
			return nil, err
		}
		if len(rows) != len(events) {
			err := fmt.Errorf("storage batch returned %d rows for %d drafts", len(rows), len(events))
			l.storageErr = err
			return nil, err
		}
		for index, row := range rows {
			if row.Seq != nextSeq+index {
				err := fmt.Errorf("storage batch row %d has seq %d, want %d", index, row.Seq, nextSeq+index)
				l.storageErr = err
				return nil, err
			}
			events[index].ContentHash = row.ContentHash
		}
	}
	l.events = append(l.events, events...)
	for index, event := range events {
		l.Observation.Emit("event_appended", map[string]any{"event": event, "log_size": nextSeq + index + 1})
	}
	return events, nil
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
