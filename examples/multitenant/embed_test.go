package multitenant

import (
	"testing"

	harnas "github.com/Tedo-ai/harnas-go"
)

// TestRunTurnEndToEnd exercises the full embed: load a fresh Session from a
// StorageAdapter, build the registry, install the enforce gate, run a turn with
// MockProvider, persist the new events with the OCC fence, and confirm the
// session reloads with VerifySessionPortable passing.
func TestRunTurnEndToEnd(t *testing.T) {
	adapter := harnas.NewMemoryStorageAdapter()

	session, persisted, err := loadSession(adapter, "ws_1:conv_1", map[string]any{"workspace_id": "ws_1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.SaveHeader(harnas.SessionHeader{ID: session.ID, Metadata: session.Metadata}); err != nil {
		t.Fatal(err)
	}

	registry, err := buildRegistry(
		func(name string, args map[string]any) (string, error) { return "ok:" + name, nil },
		[]ToolSpec{{Name: "notes.create_note", Description: "create a note", InputSchema: map[string]any{"type": "object"}}},
	)
	if err != nil {
		t.Fatal(err)
	}

	installEnforceGate(session, func(toolName string) (bool, string) {
		return true, "" // allow-all for the demo; a real policy checks actor perms
	})

	if _, err := runTurn(session, registry, harnas.MockProvider{Text: "hello"}, "hi"); err != nil {
		t.Fatal(err)
	}

	if err := persistNewEvents(adapter, session, persisted); err != nil {
		t.Fatal(err)
	}

	// Portability gate: a session persisted via the adapter must round-trip.
	if err := harnas.VerifySessionPortable(session); err != nil {
		t.Fatalf("session not portable: %v", err)
	}

	rows, err := adapter.EventsSince(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("expected events persisted to the adapter")
	}
}
