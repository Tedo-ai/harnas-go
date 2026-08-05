package harnas

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	_ "modernc.org/sqlite"
)

func TestSQLStorageAdapterOCCLawFixture(t *testing.T) {
	db := newSQLiteStorageDB(t)
	adapter := NewSQLStorageAdapter(db, "ses_occ", SQLStorageOptions{Dialect: SQLStorageDialectSQLite})
	runStorageOCCLawFixture(t, adapter)
}

func TestSQLStorageAdapterLawsS1ThroughS8(t *testing.T) {
	db := newSQLiteStorageDB(t)
	adapter := NewSQLStorageAdapter(db, "ses_storage", SQLStorageOptions{Dialect: SQLStorageDialectSQLite})
	runStorageS1ThroughS8(t, adapter)
}

func TestBuiltInStorageAdaptersAppendBatchUnderOneOCCFence(t *testing.T) {
	file := NewFileStorageAdapter(filepath.Join(t.TempDir(), "session.jsonl"))
	if err := file.SaveHeader(SessionHeader{ID: "ses_batch", Metadata: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	sqlAdapter := NewSQLStorageAdapter(newSQLiteStorageDB(t), "ses_batch", SQLStorageOptions{Dialect: SQLStorageDialectSQLite})
	if err := sqlAdapter.SaveHeader(SessionHeader{ID: "ses_batch", Metadata: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	adapters := map[string]StorageAdapter{
		"memory": NewMemoryStorageAdapter(),
		"file":   file,
		"sql":    sqlAdapter,
	}
	for name, adapter := range adapters {
		t.Run(name, func(t *testing.T) {
			zero := 0
			rows, err := adapter.AppendEvents([]EventDraft{
				{ID: "evt_0", Timestamp: "2026-08-05T00:00:00Z", Type: EventAssistantMessage, Payload: map[string]any{"stop_reason": "max_tokens"}},
				{ID: "evt_1", Timestamp: "2026-08-05T00:00:01Z", Type: EventToolUse, Payload: map[string]any{"id": "call_1"}},
				{ID: "evt_2", Timestamp: "2026-08-05T00:00:02Z", Type: EventToolResult, Payload: map[string]any{"tool_use_id": "call_1", "error": "truncated"}},
			}, &zero)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 3 || rows[0].Seq != 0 || rows[2].Seq != 2 {
				t.Fatalf("batch rows = %#v", rows)
			}

			// The stale fence rejects the whole second batch, not merely its
			// first row.
			_, err = adapter.AppendEvents([]EventDraft{
				{ID: "evt_3", Type: EventAnnotation, Payload: map[string]any{}},
				{ID: "evt_4", Type: EventAnnotation, Payload: map[string]any{}},
			}, &zero)
			var conflict *StorageConflictError
			if !errors.As(err, &conflict) {
				t.Fatalf("expected batch conflict, got %v", err)
			}
			all, err := adapter.EventsSince(nil)
			if err != nil || len(all) != 3 {
				t.Fatalf("conflicted batch changed storage: rows=%#v err=%v", all, err)
			}
		})
	}
}

func TestFileAndSQLBatchRollbackOnInvalidSecondDraft(t *testing.T) {
	file := NewFileStorageAdapter(filepath.Join(t.TempDir(), "session.jsonl"))
	if err := file.SaveHeader(SessionHeader{ID: "ses_invalid_batch", Metadata: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	sqlAdapter := NewSQLStorageAdapter(newSQLiteStorageDB(t), "ses_invalid_batch", SQLStorageOptions{Dialect: SQLStorageDialectSQLite})
	if err := sqlAdapter.SaveHeader(SessionHeader{ID: "ses_invalid_batch", Metadata: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	for name, adapter := range map[string]StorageAdapter{"file": file, "sql": sqlAdapter} {
		t.Run(name, func(t *testing.T) {
			zero := 0
			_, err := adapter.AppendEvents([]EventDraft{
				{ID: "evt_good", Type: EventUserMessage, Payload: map[string]any{"text": "good"}},
				{ID: "evt_bad", Type: EventAnnotation, Payload: map[string]any{"invalid": make(chan int)}},
			}, &zero)
			if err == nil {
				t.Fatal("expected invalid batch to fail")
			}
			rows, rowsErr := adapter.EventsSince(nil)
			if rowsErr != nil || len(rows) != 0 {
				t.Fatalf("failed batch persisted a prefix: rows=%#v err=%v", rows, rowsErr)
			}
		})
	}
}

func TestSQLStorageAdapterRoundTripsJSONLRows(t *testing.T) {
	db := newSQLiteStorageDB(t)
	source := NewSQLStorageAdapter(db, "ses_storage", SQLStorageOptions{Dialect: SQLStorageDialectSQLite})
	runStorageS1ThroughS8(t, source)
	rows, err := source.EventsSince(nil)
	if err != nil {
		t.Fatal(err)
	}

	jsonlPath := filepath.Join(t.TempDir(), "session.jsonl")
	fileAdapter := NewFileStorageAdapter(jsonlPath)
	header, err := source.LoadSession()
	if err != nil {
		t.Fatal(err)
	}
	if header == nil {
		t.Fatal("missing source header")
	}
	if err := fileAdapter.SaveHeader(*header); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		_, err := fileAdapter.AppendEvent(EventDraft{
			ID:        row.ID,
			Timestamp: row.Timestamp,
			Type:      row.Type,
			Payload:   row.Payload,
		}, ptr(row.Seq))
		if err != nil {
			t.Fatal(err)
		}
	}

	targetDB := newSQLiteStorageDB(t)
	target := NewSQLStorageAdapter(targetDB, "ses_storage", SQLStorageOptions{Dialect: SQLStorageDialectSQLite})
	loadedHeader, err := fileAdapter.LoadSession()
	if err != nil {
		t.Fatal(err)
	}
	if err := target.SaveHeader(*loadedHeader); err != nil {
		t.Fatal(err)
	}
	jsonlRows, err := fileAdapter.EventsSince(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range jsonlRows {
		_, err := target.AppendEvent(EventDraft{
			ID:        row.ID,
			Timestamp: row.Timestamp,
			Type:      row.Type,
			Payload:   row.Payload,
		}, ptr(row.Seq))
		if err != nil {
			t.Fatal(err)
		}
	}
	targetRows, err := target.EventsSince(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(targetRows) != len(jsonlRows) {
		t.Fatalf("target row count = %d, want %d", len(targetRows), len(jsonlRows))
	}
	for i := range targetRows {
		if targetRows[i].Seq != jsonlRows[i].Seq || targetRows[i].ID != jsonlRows[i].ID || targetRows[i].Type != jsonlRows[i].Type {
			t.Fatalf("row envelope mismatch: %#v vs %#v", targetRows[i], jsonlRows[i])
		}
		if !reflect.DeepEqual(targetRows[i].Payload, jsonlRows[i].Payload) {
			t.Fatalf("row payload mismatch: %#v vs %#v", targetRows[i].Payload, jsonlRows[i].Payload)
		}
	}
}

func newSQLiteStorageDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "harnas.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := EnsureSQLStorageSchema(db, SQLStorageOptions{Dialect: SQLStorageDialectSQLite}); err != nil {
		t.Fatal(err)
	}
	return db
}

func runStorageOCCLawFixture(t *testing.T, adapter StorageAdapter) {
	t.Helper()
	root := harnasSpecRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "conformance", "storage-laws", "occ-conditional-append", "law.json"))
	if err != nil {
		t.Fatal(err)
	}
	var law struct {
		Operations []map[string]any `json:"operations"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&law); err != nil {
		t.Fatal(err)
	}
	for _, op := range law.Operations {
		switch op["op"] {
		case "append_event":
			draft := draftFromMap(t, op["draft"])
			expected := intPtrFromFloat(op["expected_next_seq"])
			row, err := adapter.AppendEvent(draft, expected)
			expect := op["expect"].(map[string]any)
			if ok := expect["ok"].(bool); ok {
				if err != nil {
					t.Fatalf("append failed: %v", err)
				}
				assertStorageRow(t, row, expect["row"].(map[string]any))
			} else {
				var conflict *StorageConflictError
				if !errors.As(err, &conflict) {
					t.Fatalf("expected storage conflict, got %v", err)
				}
				if conflict.Reason != expect["reason"] {
					t.Fatalf("reason mismatch: got %s want %s", conflict.Reason, expect["reason"])
				}
				if !storageNumberEqual(conflict.CurrentNextSeq, expect["current_next_seq"]) {
					t.Fatalf("current_next_seq mismatch: got %d want %v", conflict.CurrentNextSeq, expect["current_next_seq"])
				}
			}
		case "events_since":
			rows, err := adapter.EventsSince(intPtrFromFloat(op["cursor"]))
			if err != nil {
				t.Fatal(err)
			}
			expect := op["expect"].(map[string]any)
			expectedRows := expect["rows"].([]any)
			if len(rows) != len(expectedRows) {
				t.Fatalf("row count mismatch: got %d want %d", len(rows), len(expectedRows))
			}
			for i, expected := range expectedRows {
				assertStorageRow(t, rows[i], expected.(map[string]any))
			}
		default:
			t.Fatalf("unknown op %v", op["op"])
		}
	}
}

func runStorageS1ThroughS8(t *testing.T, adapter StorageAdapter) {
	t.Helper()
	header := SessionHeader{
		ID:       "ses_storage",
		Metadata: map[string]any{"label": "storage"},
	}
	if err := adapter.SaveHeader(header); err != nil {
		t.Fatal(err)
	}
	loadedHeader, err := adapter.LoadSession()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loadedHeader, &header) {
		t.Fatalf("header mismatch: %#v != %#v", loadedHeader, header)
	}
	row0, err := adapter.AppendEvent(EventDraft{
		ID:        "evt_0",
		Timestamp: "2026-06-16T10:00:00Z",
		Type:      EventUserMessage,
		Payload:   map[string]any{"content": []any{map[string]any{"type": "text", "text": "one"}}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if row0.Seq != 0 {
		t.Fatalf("seq = %d, want 0", row0.Seq)
	}
	row1, err := adapter.AppendEvent(EventDraft{
		ID:        "evt_1",
		Timestamp: "2026-06-16T10:00:01Z",
		Type:      EventAssistantMessage,
		Payload:   map[string]any{"content": []any{map[string]any{"type": "text", "text": "two"}}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if row1.Seq != 1 {
		t.Fatalf("seq = %d, want 1", row1.Seq)
	}
	rows, err := adapter.EventsSince(ptr(0))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "evt_1" {
		t.Fatalf("events_since returned %#v", rows)
	}
}
