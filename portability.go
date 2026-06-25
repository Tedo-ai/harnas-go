package harnas

import (
	"fmt"
	"os"
)

// VerifySessionPortable checks that a Session satisfies the cross-implementation
// portability invariants the storage adapters maintain automatically:
//
//   - sequence numbers are dense and monotonic from 0,
//   - every event payload is canonical-JSON (harnas-jcs-v1) encodable, so a
//     per-event content hash is computable, and
//   - the Session round-trips losslessly through canonical JSONL.
//
// A Session that fails any check has been persisted or mutated in a way that
// breaks portability to other Harnas implementations. This is the symptom of
// hand-rolled persistence that bypasses the storage adapter — a footgun that no
// conformance fixture catches, because it is the *absence* of adapter use.
//
// Run it in CI on a representative Session produced by your integration, as an
// acceptance gate before relying on cross-implementation portability.
func VerifySessionPortable(s *Session) error {
	if s == nil {
		return fmt.Errorf("verify portable: nil session")
	}
	events := s.Log.Events()

	// 1 & 2. Dense, monotonic seqs from 0, and every event is content-hashable.
	// A non-dense seq means an append bypassed the Log; a hash failure means a
	// payload is not canonical-JSON encodable and cannot cross to another impl.
	origHashes := make([]string, len(events))
	for i, e := range events {
		if e.Seq != i {
			return fmt.Errorf("verify portable: non-dense seq at index %d: got %d, want %d (a gap means an append bypassed the Log)", i, e.Seq, i)
		}
		h, err := ContentHashForEventRow(eventToRow(e))
		if err != nil {
			return fmt.Errorf("verify portable: event seq %d (%s) is not portable: payload is not canonical-JSON encodable: %w", e.Seq, e.Type, err)
		}
		origHashes[i] = h
	}

	// 3. Lossless canonical-JSONL round-trip. LoadSession independently
	// re-validates dense seqs and structure, so a store that lost fidelity
	// fails here; matching content hashes prove every field round-tripped.
	tmp, err := os.CreateTemp("", "harnas-portable-*.jsonl")
	if err != nil {
		return fmt.Errorf("verify portable: temp file: %w", err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	if err := s.Save(tmpPath); err != nil {
		return fmt.Errorf("verify portable: session does not save to canonical JSONL: %w", err)
	}
	reloaded, err := LoadSession(tmpPath)
	if err != nil {
		return fmt.Errorf("verify portable: session does not reload from canonical JSONL (portability lost): %w", err)
	}

	back := reloaded.Log.Events()
	if len(events) != len(back) {
		return fmt.Errorf("verify portable: JSONL round-trip changed event count: %d -> %d", len(events), len(back))
	}
	for i, e := range back {
		h, err := ContentHashForEventRow(eventToRow(e))
		if err != nil {
			return fmt.Errorf("verify portable: reloaded event seq %d is not content-hashable: %w", e.Seq, err)
		}
		if h != origHashes[i] {
			return fmt.Errorf("verify portable: JSONL round-trip altered event seq %d (content hash changed) — persistence is lossy", e.Seq)
		}
	}
	return nil
}

func eventToRow(e Event) EventRow {
	return EventRow{
		Seq:       e.Seq,
		ID:        e.ID,
		Timestamp: e.Timestamp,
		Type:      e.Type,
		Payload:   e.Payload,
	}
}
