package harnas

import "fmt"

// StorageWriteError is the typed terminal error AgentLoop.Run returns when a
// write-through Log append fails to become durable (storage outage or a
// StorageConflictError from a concurrent writer). The turn is aborted before
// the next loop step observes the event; the in-memory Log never contains the
// failed event, so the session remains resumable: reload it from the adapter
// with LoadSessionFromStorage (or clear the latch with Log.ClearStorageErr
// after the outage is resolved) and run again.
type StorageWriteError struct {
	Cause error
}

func (e *StorageWriteError) Error() string {
	return "storage write-through failed: " + e.Cause.Error()
}

func (e *StorageWriteError) Unwrap() error {
	return e.Cause
}

// BindStorage attaches a StorageAdapter to the Session as a write-through
// durability sink: the header is persisted immediately, any in-memory events
// not yet in storage are backfilled through the OCC fence, and every
// subsequent Log.Append becomes durable in the adapter before the event is
// visible in memory. Sessions without a binding keep the in-memory behavior
// verbatim. Forked sessions do not inherit the binding.
func (s *Session) BindStorage(adapter StorageAdapter) error {
	if adapter == nil {
		return fmt.Errorf("BindStorage requires a non-nil StorageAdapter")
	}
	header := SessionHeader{
		ID:               s.ID,
		Metadata:         s.Metadata,
		ParentSessionID:  s.ParentSessionID,
		RootSessionID:    s.RootSessionID,
		SpawnID:          s.SpawnID,
		SpawnedByEventID: s.SpawnedByEventID,
		DelegationChain:  s.DelegationChain,
	}
	if err := adapter.SaveHeader(header); err != nil {
		return fmt.Errorf("BindStorage: saving session header: %w", err)
	}
	rows, err := adapter.EventsSince(nil)
	if err != nil {
		return fmt.Errorf("BindStorage: reading existing events: %w", err)
	}
	events := s.Log.Events()
	if len(rows) > len(events) {
		return fmt.Errorf("BindStorage: storage has %d events but the log has %d; load the session from storage instead", len(rows), len(events))
	}
	for i, row := range rows {
		if row.Seq != events[i].Seq || row.Type != events[i].Type {
			return fmt.Errorf("BindStorage: storage event at seq %d does not match the in-memory log (storage %s, log %s)", row.Seq, row.Type, events[i].Type)
		}
	}
	for _, event := range events[len(rows):] {
		expected := event.Seq
		if _, err := adapter.AppendEvent(EventDraft{
			ID:        event.ID,
			Timestamp: event.Timestamp,
			Type:      event.Type,
			Payload:   event.Payload,
		}, &expected); err != nil {
			return fmt.Errorf("BindStorage: backfilling event seq %d: %w", event.Seq, err)
		}
	}
	s.Log.BindStorage(adapter)
	return nil
}

// LoadSessionFromStorage restores a Session from a StorageAdapter — the
// storage-backed sibling of LoadSession(path). Events are restored without
// re-emitting observations, the dense-seq invariant is enforced, and the
// returned Session keeps the adapter bound so subsequent appends write
// through.
func LoadSessionFromStorage(adapter StorageAdapter) (*Session, error) {
	if adapter == nil {
		return nil, fmt.Errorf("LoadSessionFromStorage requires a non-nil StorageAdapter")
	}
	header, err := adapter.LoadSession()
	if err != nil {
		return nil, err
	}
	if header == nil {
		return nil, fmt.Errorf("storage adapter has no session header")
	}
	rows, err := adapter.EventsSince(nil)
	if err != nil {
		return nil, err
	}
	log := NewLog()
	for i, row := range rows {
		if row.Seq != i {
			return nil, fmt.Errorf("invalid event seq in storage: got %d, want %d", row.Seq, i)
		}
		log.Restore(Event{
			ID:          row.ID,
			Seq:         row.Seq,
			Timestamp:   row.Timestamp,
			Type:        row.Type,
			Payload:     row.Payload,
			ContentHash: row.ContentHash,
		})
	}
	session := NewSession(header.ID, log, header.Metadata)
	session.ParentSessionID = header.ParentSessionID
	session.RootSessionID = header.RootSessionID
	session.SpawnID = header.SpawnID
	session.SpawnedByEventID = header.SpawnedByEventID
	if len(header.DelegationChain) > 0 {
		session.DelegationChain = cloneDelegationChain(header.DelegationChain)
	}
	log.BindStorage(adapter)
	return session, nil
}
