package harnas

import (
	"errors"
	"testing"
)

// fakePQError mimics a driver's typed error carrying a SQLSTATE code, like
// lib/pq's *pq.Error, so the test exercises the consumer's intended detector
// shape (errors.As + Code == "23505") without importing a driver.
type fakePQError struct{ Code string }

func (e *fakePQError) Error() string { return "pq: error " + e.Code }

func TestSQLConflictDetectorOverridesDefault(t *testing.T) {
	a := &SQLStorageAdapter{
		conflictDetector: func(err error) bool {
			var pqErr *fakePQError
			return errors.As(err, &pqErr) && pqErr.Code == "23505"
		},
	}

	// The SQLSTATE detector catches the driver's typed unique-violation...
	if !a.isUniqueConflict(&fakePQError{Code: "23505"}) {
		t.Fatal("custom ConflictDetector did not catch SQLSTATE 23505")
	}
	// ...a different SQLSTATE is not a conflict...
	if a.isUniqueConflict(&fakePQError{Code: "23503"}) {
		t.Fatal("custom ConflictDetector matched a non-unique SQLSTATE")
	}
	// ...and the custom detector fully replaces the default string match, so a
	// message that *would* match "unique" is no longer a conflict.
	if a.isUniqueConflict(errors.New("violates unique constraint")) {
		t.Fatal("custom ConflictDetector should override the default string match")
	}
}

func TestSQLConflictDetectorDefaultStringMatch(t *testing.T) {
	a := &SQLStorageAdapter{} // no detector -> driver-agnostic default

	for _, msg := range []string{
		"UNIQUE constraint failed: harnas_events.session_id, harnas_events.seq",     // sqlite
		"pq: duplicate key value violates unique constraint \"harnas_events_pkey\"", // lib/pq
		"ERROR: duplicate key value violates unique constraint (SQLSTATE 23505)",    // pgx
	} {
		if !a.isUniqueConflict(errors.New(msg)) {
			t.Fatalf("default detector missed a unique-violation message: %q", msg)
		}
	}
	if a.isUniqueConflict(errors.New("connection refused")) {
		t.Fatal("default detector matched an unrelated error")
	}
}
