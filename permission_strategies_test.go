package harnas

import "testing"

func TestAlwaysAllowReturnsAllowDecision(t *testing.T) {
	session := CreateSession(nil)
	AlwaysAllow{}.Install(session)
	decisions := session.Hooks.Invoke("pre_tool_use", map[string]any{
		"tool_use": Event{Type: EventToolUse, Payload: map[string]any{"name": "read_file"}},
	})
	if len(decisions) != 1 || decisions[0].(map[string]any)["allow"] != true {
		t.Fatalf("unexpected decisions: %#v", decisions)
	}
}

func TestHumanApprovalAllowsWhenPromptApproves(t *testing.T) {
	session := CreateSession(nil)
	HumanApproval{Prompt: func(Event) bool { return true }}.Install(session)
	decisions := session.Hooks.Invoke("pre_tool_use", map[string]any{
		"tool_use": Event{Type: EventToolUse, Payload: map[string]any{"name": "read_file"}},
	})
	if decisions[0].(map[string]any)["allow"] != true {
		t.Fatalf("unexpected decisions: %#v", decisions)
	}
}

func TestHumanApprovalDeniesWhenPromptDeclines(t *testing.T) {
	session := CreateSession(nil)
	HumanApproval{Prompt: func(Event) bool { return false }}.Install(session)
	decisions := session.Hooks.Invoke("pre_tool_use", map[string]any{
		"tool_use": Event{Type: EventToolUse, Payload: map[string]any{"name": "read_file"}},
	})
	decision := decisions[0].(map[string]any)
	if decision["allow"] != false || decision["reason"] != "human declined" {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestBuildStrategiesResolvesHumanApprovalPrompt(t *testing.T) {
	strategies, err := BuildStrategies([]StrategySpec{{
		Name:   "Permission::HumanApproval",
		Config: map[string]any{"prompt": "approve"},
	}}, map[string]ApprovalHandler{
		"approve": func(Event) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(strategies) != 1 {
		t.Fatalf("expected strategy")
	}
}
