package harnas

import "testing"

func TestDelegationProjections(t *testing.T) {
	parent := NewSession("ses_parent", NewLog(), nil)
	parent.Log.Append(EventUserMessage, map[string]any{"text": "delegate"})
	spawn := parent.Log.Append(EventAgentSpawn, map[string]any{
		"spawn_id":         "spn_1",
		"child_session_id": "ses_child",
		"task":             "audit",
	})
	parent.Log.Append(EventAgentStatus, map[string]any{
		"spawn_id":         "spn_1",
		"child_session_id": "ses_child",
		"status":           "running",
	})
	parent.Log.Append(EventAgentResult, map[string]any{
		"spawn_id":         "spn_1",
		"child_session_id": "ses_child",
		"status":           "succeeded",
		"result":           map[string]any{"text": "done"},
		"usage":            map[string]any{"prompt_tokens": 1, "completion_tokens": 2, "total_tokens": 3},
	})
	child := NewSession("ses_child", NewLog(), nil)
	child.ParentSessionID = "ses_parent"
	child.RootSessionID = "ses_parent"
	child.SpawnID = "spn_1"
	child.SpawnedByEventID = spawn.ID
	child.DelegationChain = []map[string]any{{"session_id": "ses_parent", "spawn_id": nil}}
	child.Log.Append(EventAssistantMessage, map[string]any{
		"text":        "child done",
		"stop_reason": "end_turn",
		"usage":       map[string]any{"input_tokens": 4, "output_tokens": 5},
	})
	store := SessionMap{"ses_parent": parent, "ses_child": child}

	open, err := OpenChildren("ses_parent", store)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 0 {
		t.Fatalf("expected no open children, got %#v", open)
	}
	tree, err := DelegationTree("ses_parent", store)
	if err != nil {
		t.Fatal(err)
	}
	children := tree["children"].([]map[string]any)
	if children[0]["status"] != "succeeded" {
		t.Fatalf("unexpected tree: %#v", tree)
	}
	usage, err := DescendantUsage("ses_parent", store)
	if err != nil {
		t.Fatal(err)
	}
	if usage["total_tokens"] != 12 {
		t.Fatalf("unexpected usage: %#v", usage)
	}
}
