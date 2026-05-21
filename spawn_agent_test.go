package harnas

import (
	"encoding/json"
	"testing"
)

func TestRunnerSpawnAgentAppendsSpawnReceipt(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(Tool{
		Name:        "spawn_agent",
		Handler:     "harnas.builtin.spawn_agent",
		Description: "spawn",
		InputSchema: map[string]any{},
	}); err != nil {
		t.Fatal(err)
	}
	log := NewLog()
	toolUse := log.Append(EventToolUse, map[string]any{
		"id":        "call_spawn",
		"name":      "spawn_agent",
		"arguments": map[string]any{"task": "Audit this", "label": "Explorer", "role": "explorer", "tools_deny": []any{"bash_session"}},
	})

	(&Runner{Registry: registry}).Run(toolUse, log)

	events := log.Events()
	if len(events) != 3 {
		t.Fatalf("expected tool_use, agent_spawn, tool_result; got %d", len(events))
	}
	if events[1].Type != EventAgentSpawn {
		t.Fatalf("expected agent_spawn, got %s", events[1].Type)
	}
	if events[1].Payload["task"] != "Audit this" {
		t.Fatalf("unexpected spawn payload: %#v", events[1].Payload)
	}
	if events[1].Payload["spawned_by_event_id"] != toolUse.ID {
		t.Fatalf("spawned_by_event_id mismatch: %#v", events[1].Payload)
	}
	var output map[string]any
	if err := json.Unmarshal([]byte(events[2].Payload["output"].(string)), &output); err != nil {
		t.Fatal(err)
	}
	if output["spawn_id"] != events[1].Payload["spawn_id"] {
		t.Fatalf("tool result did not echo spawn id: %#v", output)
	}
}
