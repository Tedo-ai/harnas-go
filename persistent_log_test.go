package harnas

import (
	"errors"
	"fmt"
	"testing"
)

type failingAdapter struct {
	*MemoryStorageAdapter
	failAfter int // number of successful appends before failures start
	appended  int
	err       error
}

func (a *failingAdapter) AppendEvent(draft EventDraft, expectedNextSeq *int) (EventRow, error) {
	if a.appended >= a.failAfter {
		return EventRow{}, a.err
	}
	a.appended++
	return a.MemoryStorageAdapter.AppendEvent(draft, expectedNextSeq)
}

func TestWriteThroughAppendPersistsBeforeMemory(t *testing.T) {
	adapter := NewMemoryStorageAdapter()
	session := CreateSession(map[string]any{"tenant": "t1"})
	if err := session.BindStorage(adapter); err != nil {
		t.Fatalf("BindStorage: %v", err)
	}
	event := session.Log.Append(EventUserMessage, map[string]any{"text": "hello"})
	if event.Seq != 0 {
		t.Fatalf("expected seq 0, got %d", event.Seq)
	}
	rows, err := adapter.EventsSince(nil)
	if err != nil {
		t.Fatalf("EventsSince: %v", err)
	}
	if len(rows) != 1 || rows[0].Type != EventUserMessage {
		t.Fatalf("expected 1 durable user_message row, got %#v", rows)
	}
	header, err := adapter.LoadSession()
	if err != nil || header == nil {
		t.Fatalf("expected persisted header, got %v / %v", header, err)
	}
	if header.ID != session.ID {
		t.Fatalf("header ID %q != session ID %q", header.ID, session.ID)
	}
}

func TestWriteThroughConflictLatchesAndSkipsMemory(t *testing.T) {
	adapter := NewMemoryStorageAdapter()
	session := CreateSession(nil)
	if err := session.BindStorage(adapter); err != nil {
		t.Fatalf("BindStorage: %v", err)
	}
	session.Log.Append(EventUserMessage, map[string]any{"text": "one"})

	// A concurrent writer advances storage out from under the log.
	if _, err := adapter.AppendEvent(EventDraft{ID: "evt_x", Type: EventAnnotation, Payload: map[string]any{}}, nil); err != nil {
		t.Fatalf("out-of-band append: %v", err)
	}

	event := session.Log.Append(EventUserMessage, map[string]any{"text": "two"})
	if event.ID != "" {
		t.Fatalf("expected zero event on conflicted append, got %#v", event)
	}
	var conflict *StorageConflictError
	if !errors.As(session.Log.StorageErr(), &conflict) {
		t.Fatalf("expected latched StorageConflictError, got %v", session.Log.StorageErr())
	}
	if got := len(session.Log.Events()); got != 1 {
		t.Fatalf("failed event must not reach memory; log has %d events", got)
	}

	// Latched log refuses further appends until cleared.
	if again := session.Log.Append(EventUserMessage, map[string]any{"text": "three"}); again.ID != "" {
		t.Fatalf("latched log must refuse appends, got %#v", again)
	}
	session.Log.ClearStorageErr()
	if session.Log.StorageErr() != nil {
		t.Fatal("ClearStorageErr should clear the latch")
	}
}

func TestRunReturnsStorageWriteErrorMidTurn(t *testing.T) {
	// Allow the user message through, fail on the assistant append mid-turn.
	adapter := &failingAdapter{MemoryStorageAdapter: NewMemoryStorageAdapter(), failAfter: 1, err: fmt.Errorf("storage down")}
	session := CreateSession(nil)
	if err := session.BindStorage(adapter); err != nil {
		t.Fatalf("BindStorage: %v", err)
	}
	session.Log.Append(EventUserMessage, map[string]any{"text": "hello"})

	loop := AgentLoop{
		Session:    session,
		Projection: AnthropicProjection{Model: "m", MaxTokens: 16},
		Provider:   MockProvider{Text: "hi"},
		Ingestor:   AnthropicIngestor{},
	}
	_, err := loop.Run()
	var writeErr *StorageWriteError
	if !errors.As(err, &writeErr) {
		t.Fatalf("expected *StorageWriteError from Run, got %v", err)
	}
	// Only the durable prefix is visible in memory: the assistant message
	// never landed, so a reload from storage replays a consistent transcript.
	if got := len(session.Log.Events()); got != 1 {
		t.Fatalf("expected only the durable user message in memory, got %d events", got)
	}
	// A latched session refuses to run again until the host intervenes.
	if _, err := loop.Run(); err == nil {
		t.Fatal("expected latched Run to fail fast")
	}
}

func TestLoadSessionFromStorageRoundTrip(t *testing.T) {
	adapter := NewMemoryStorageAdapter()
	original := CreateSession(map[string]any{"workspace": "w1"})
	original.ParentSessionID = "ses_parent"
	if err := original.BindStorage(adapter); err != nil {
		t.Fatalf("BindStorage: %v", err)
	}
	original.Log.Append(EventUserMessage, map[string]any{"text": "hello"})
	original.Log.Append(EventAssistantMessage, map[string]any{"text": "hi", "stop_reason": "end_turn"})

	restored, err := LoadSessionFromStorage(adapter)
	if err != nil {
		t.Fatalf("LoadSessionFromStorage: %v", err)
	}
	if restored.ID != original.ID {
		t.Fatalf("restored ID %q != original %q", restored.ID, original.ID)
	}
	if restored.Metadata["workspace"] != "w1" || restored.ParentSessionID != "ses_parent" {
		t.Fatalf("restored header fields wrong: %#v parent=%q", restored.Metadata, restored.ParentSessionID)
	}
	restoredEvents := restored.Log.Events()
	originalEvents := original.Log.Events()
	if len(restoredEvents) != len(originalEvents) {
		t.Fatalf("restored %d events, want %d", len(restoredEvents), len(originalEvents))
	}
	for i := range restoredEvents {
		if restoredEvents[i].Seq != originalEvents[i].Seq || restoredEvents[i].Type != originalEvents[i].Type {
			t.Fatalf("event %d mismatch: %#v vs %#v", i, restoredEvents[i], originalEvents[i])
		}
	}
	// The restored session keeps the binding: the next append continues the
	// dense seq under the OCC fence.
	next := restored.Log.Append(EventUserMessage, map[string]any{"text": "again"})
	if next.Seq != 2 {
		t.Fatalf("expected continued seq 2, got %d", next.Seq)
	}
	rows, _ := adapter.EventsSince(nil)
	if len(rows) != 3 {
		t.Fatalf("expected 3 durable rows after continued append, got %d", len(rows))
	}
}

func TestBindStorageBackfillsExistingEvents(t *testing.T) {
	session := CreateSession(nil)
	session.Log.Append(EventUserMessage, map[string]any{"text": "one"})
	session.Log.Append(EventAssistantMessage, map[string]any{"text": "two", "stop_reason": "end_turn"})

	adapter := NewMemoryStorageAdapter()
	if err := session.BindStorage(adapter); err != nil {
		t.Fatalf("BindStorage: %v", err)
	}
	rows, err := adapter.EventsSince(nil)
	if err != nil {
		t.Fatalf("EventsSince: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 backfilled rows, got %d", len(rows))
	}
	if rows[0].Type != EventUserMessage || rows[1].Type != EventAssistantMessage {
		t.Fatalf("backfill order wrong: %#v", rows)
	}
}

func TestBindStorageRejectsMismatchedStorage(t *testing.T) {
	adapter := NewMemoryStorageAdapter()
	if _, err := adapter.AppendEvent(EventDraft{ID: "evt_a", Type: EventAnnotation, Payload: map[string]any{}}, nil); err != nil {
		t.Fatal(err)
	}
	session := CreateSession(nil) // empty log, storage already has one event
	if err := session.BindStorage(adapter); err == nil {
		t.Fatal("expected BindStorage to reject storage ahead of the log")
	}
}

func TestWriteThroughAgainstSQLAdapter(t *testing.T) {
	db := newSQLiteStorageDB(t)
	session := CreateSession(map[string]any{"tenant": "acme"})
	adapter := NewSQLStorageAdapter(db, session.ID, SQLStorageOptions{Dialect: SQLStorageDialectSQLite})
	if err := session.BindStorage(adapter); err != nil {
		t.Fatalf("BindStorage: %v", err)
	}
	event := session.Log.Append(EventUserMessage, map[string]any{"text": "hello"})
	if event.ContentHash == "" {
		t.Fatal("SQL write-through should surface the stored content hash on the in-memory event")
	}
	restored, err := LoadSessionFromStorage(adapter)
	if err != nil {
		t.Fatalf("LoadSessionFromStorage over SQL: %v", err)
	}
	events := restored.Log.Events()
	if len(events) != 1 || events[0].ContentHash != event.ContentHash {
		t.Fatalf("restored SQL event should carry the same content hash: %#v", events)
	}
}

func TestUnboundLogBehaviorUnchanged(t *testing.T) {
	session := CreateSession(nil)
	event := session.Log.Append(EventUserMessage, map[string]any{"text": "hello"})
	if event.Seq != 0 || event.ContentHash != "" {
		t.Fatalf("unbound append changed shape: %#v", event)
	}
	if session.Log.StorageErr() != nil {
		t.Fatal("unbound log must never latch")
	}
}
