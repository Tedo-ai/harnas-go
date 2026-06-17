package harnas

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestStorageAdapterOCCLawFixture(t *testing.T) {
	root := harnasSpecRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "conformance", "storage-laws", "occ-conditional-append", "law.json"))
	if err != nil {
		t.Fatal(err)
	}
	var law struct {
		Operations []map[string]any `json:"operations"`
	}
	if err := json.Unmarshal(data, &law); err != nil {
		t.Fatal(err)
	}
	adapter := NewMemoryStorageAdapter()
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
				if float64(conflict.CurrentNextSeq) != expect["current_next_seq"] {
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

func TestFileStorageAdapterLawsS1ThroughS8(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	adapter := NewFileStorageAdapter(path)
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
	reloaded := NewFileStorageAdapter(path)
	row1, err := reloaded.AppendEvent(EventDraft{
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
	rows, err := reloaded.EventsSince(ptr(0))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "evt_1" {
		t.Fatalf("events_since returned %#v", rows)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, []byte(`{"seq":`)...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileStorageAdapter(path).EventsSince(nil); err == nil {
		t.Fatalf("expected loud error on torn final line")
	}
}

func TestFileStorageAdapterPreservesJSONNumberPayloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	adapter := NewFileStorageAdapter(path)
	if err := adapter.SaveHeader(SessionHeader{ID: "ses_big", Metadata: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	_, err := adapter.AppendEvent(EventDraft{
		ID:        "evt_big",
		Timestamp: "2026-06-16T10:00:00Z",
		Type:      EventUserMessage,
		Payload:   map[string]any{"big": json.Number("12345678901234567890")},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := NewFileStorageAdapter(path).EventsSince(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := rows[0].Payload["big"]; got != json.Number("12345678901234567890") {
		t.Fatalf("big integer payload was not preserved: %#v", got)
	}
}

func draftFromMap(t *testing.T, value any) EventDraft {
	t.Helper()
	spec := value.(map[string]any)
	return EventDraft{
		ID:        spec["id"].(string),
		Timestamp: spec["timestamp"].(string),
		Type:      EventType(spec["type"].(string)),
		Payload:   spec["payload"].(map[string]any),
	}
}

func intPtrFromFloat(value any) *int {
	if value == nil {
		return nil
	}
	v := int(value.(float64))
	return &v
}

func ptr(v int) *int {
	return &v
}

func assertStorageRow(t *testing.T, row EventRow, expected map[string]any) {
	t.Helper()
	if float64(row.Seq) != expected["seq"] || row.ID != expected["id"] || row.Timestamp != expected["timestamp"] || string(row.Type) != expected["type"] {
		t.Fatalf("row envelope mismatch: %#v vs %#v", row, expected)
	}
	if !reflect.DeepEqual(row.Payload, expected["payload"]) {
		t.Fatalf("payload mismatch: %#v vs %#v", row.Payload, expected["payload"])
	}
}
