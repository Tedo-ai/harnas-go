package harnas

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionDelegationMetadataRoundTrips(t *testing.T) {
	session := NewSession("ses_child", NewLog(), map[string]any{"label": "child"})
	session.ParentSessionID = "ses_parent"
	session.RootSessionID = "ses_root"
	session.SpawnID = "spn_1"
	session.SpawnedByEventID = "evt_2_abc"
	session.DelegationChain = []map[string]any{
		{"session_id": "ses_root", "spawn_id": nil},
		{"session_id": "ses_parent", "spawn_id": "spn_parent"},
	}
	session.Log.Append(EventUserMessage, map[string]any{"text": "task"})

	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := session.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ParentSessionID != "ses_parent" || loaded.RootSessionID != "ses_root" ||
		loaded.SpawnID != "spn_1" || loaded.SpawnedByEventID != "evt_2_abc" {
		t.Fatalf("delegation fields did not round-trip: %#v", loaded)
	}
	if got := loaded.DelegationChain[1]["spawn_id"]; got != "spn_parent" {
		t.Fatalf("delegation chain did not round-trip: %#v", loaded.DelegationChain)
	}
}

func TestLoadSessionRejectsCorruptEventRows(t *testing.T) {
	cases := map[string]string{
		"torn final line": `{"__session__":true,"id":"ses_test","metadata":{}}
{"seq":0,"id":"evt_0","timestamp":"2026-06-01T00:00:00Z","type":"user_message","payload":{"text":"a"}}
{"seq":1,"id":"evt_1","timestamp":"2026-06-01T00:00:01Z","type":"user_message","payload":{"text":"b"}`,
		"duplicate seq": `{"__session__":true,"id":"ses_test","metadata":{}}
{"seq":0,"id":"evt_0","timestamp":"2026-06-01T00:00:00Z","type":"user_message","payload":{"text":"a"}}
{"seq":0,"id":"evt_dup","timestamp":"2026-06-01T00:00:01Z","type":"user_message","payload":{"text":"b"}}
`,
		"gapped seq": `{"__session__":true,"id":"ses_test","metadata":{}}
{"seq":0,"id":"evt_0","timestamp":"2026-06-01T00:00:00Z","type":"user_message","payload":{"text":"a"}}
{"seq":2,"id":"evt_2","timestamp":"2026-06-01T00:00:01Z","type":"user_message","payload":{"text":"b"}}
`,
		"reordered seq": `{"__session__":true,"id":"ses_test","metadata":{}}
{"seq":1,"id":"evt_1","timestamp":"2026-06-01T00:00:00Z","type":"user_message","payload":{"text":"a"}}
{"seq":0,"id":"evt_0","timestamp":"2026-06-01T00:00:01Z","type":"user_message","payload":{"text":"b"}}
`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.jsonl")
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := LoadSession(path)
			if err == nil {
				t.Fatalf("expected load error")
			}
			if name != "torn final line" && !strings.Contains(err.Error(), "invalid event seq") {
				t.Fatalf("expected seq validation error, got %v", err)
			}
		})
	}
}
