package harnas

import "testing"

// TestSQLStorageAdapterWorkspaceIsolation proves the same session_id in two
// workspaces does not collide, rows don't leak across tenants, and the OCC
// fence still bites within a workspace.
func TestSQLStorageAdapterWorkspaceIsolation(t *testing.T) {
	db := newSQLiteStorageDB(t)
	a := NewSQLStorageAdapter(db, "conv_1", SQLStorageOptions{Dialect: SQLStorageDialectSQLite, WorkspaceID: "ws_a"})
	b := NewSQLStorageAdapter(db, "conv_1", SQLStorageOptions{Dialect: SQLStorageDialectSQLite, WorkspaceID: "ws_b"})

	zero := 0
	if _, err := a.AppendEvent(EventDraft{ID: "evt_a", Type: EventUserMessage, Payload: map[string]any{"text": "from A"}}, &zero); err != nil {
		t.Fatalf("append to ws_a seq 0: %v", err)
	}
	// Same session_id, different workspace — must NOT conflict at seq 0.
	if _, err := b.AppendEvent(EventDraft{ID: "evt_b", Type: EventUserMessage, Payload: map[string]any{"text": "from B"}}, &zero); err != nil {
		t.Fatalf("append to ws_b at the same seq must not conflict across workspaces: %v", err)
	}

	aRows, err := a.EventsSince(nil)
	if err != nil {
		t.Fatal(err)
	}
	bRows, err := b.EventsSince(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(aRows) != 1 || len(bRows) != 1 {
		t.Fatalf("expected 1 event each, got a=%d b=%d (rows leaked across workspaces)", len(aRows), len(bRows))
	}
	if aRows[0].Payload["text"] != "from A" || bRows[0].Payload["text"] != "from B" {
		t.Fatalf("workspace rows leaked: a=%v b=%v", aRows[0].Payload["text"], bRows[0].Payload["text"])
	}

	// Within a workspace, the OCC fence still rejects a stale seq.
	if _, err := a.AppendEvent(EventDraft{ID: "evt_a2", Type: EventUserMessage, Payload: map[string]any{}}, &zero); err == nil {
		t.Fatal("expected OCC StorageConflictError re-appending seq 0 in ws_a")
	}
}

// TestSQLStorageAdapterDefaultWorkspaceBackwardCompatible confirms that with no
// WorkspaceID set, the adapter behaves as the prior session-only key.
func TestSQLStorageAdapterDefaultWorkspaceBackwardCompatible(t *testing.T) {
	db := newSQLiteStorageDB(t)
	a := NewSQLStorageAdapter(db, "conv_1", SQLStorageOptions{Dialect: SQLStorageDialectSQLite})

	zero := 0
	if _, err := a.AppendEvent(EventDraft{ID: "e0", Type: EventUserMessage, Payload: map[string]any{"text": "x"}}, &zero); err != nil {
		t.Fatal(err)
	}
	rows, err := a.EventsSince(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row with empty workspace, got %d", len(rows))
	}
}
