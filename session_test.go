package harnas

import (
	"path/filepath"
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
