package harnas

import (
	"strings"
	"testing"
)

func approvalFixtureSession(t *testing.T) (*Session, *Runner) {
	t.Helper()
	session := CreateSession(nil)
	registry := NewRegistry()
	if err := registry.Register(Tool{
		Name:        "get_current_time",
		Description: "time",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Call: func(args map[string]any) (string, error) {
			return "12:00", nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{Registry: registry}
	return session, runner
}

func TestRunPausesWithAwaitingApproval(t *testing.T) {
	session, runner := approvalFixtureSession(t)
	RequireApproval{Names: []string{"get_current_time"}}.Install(session)
	session.Log.Append(EventUserMessage, map[string]any{"text": "time?"})
	loop := AgentLoop{
		Session:    session,
		Projection: AnthropicProjection{Model: "m", MaxTokens: 16, Registry: runner.Registry},
		Provider: MockToolProvider{ToolName: "get_current_time", ToolUseID: "toolu_t1"},
		Ingestor: AnthropicIngestor{},
		Runner:   runner,
	}
	reason, err := loop.Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if reason != "awaiting_approval" {
		t.Fatalf("expected awaiting_approval, got %q", reason)
	}
	events := session.Log.Events()
	last := events[len(events)-1]
	if last.Type != EventApprovalRequested {
		t.Fatalf("expected trailing approval_requested, got %s", last.Type)
	}
	if last.Payload["tool_use_id"] != "toolu_t1" || last.Payload["requested_by"] != "Permission::RequireApproval" {
		t.Fatalf("approval_requested payload wrong: %#v", last.Payload)
	}
	for _, event := range events {
		if event.Type == EventToolResult {
			t.Fatal("paused turn must not append a tool_result")
		}
	}
}

func TestApproveExecutesExactlyOnce(t *testing.T) {
	session, runner := approvalFixtureSession(t)
	session.Log.Append(EventToolUse, map[string]any{"id": "toolu_t1", "name": "get_current_time", "arguments": map[string]any{}})
	session.Log.Append(EventApprovalRequested, map[string]any{"tool_use_id": "toolu_t1"})

	if err := ApproveToolUse(session, runner, "toolu_t1", ApprovalResolution{ResolvedBy: "tester"}); err != nil {
		t.Fatalf("ApproveToolUse: %v", err)
	}
	events := session.Log.Events()
	resolved := events[len(events)-2]
	result := events[len(events)-1]
	if resolved.Type != EventApprovalResolved || resolved.Payload["decision"] != "approved" || resolved.Payload["resolved_by"] != "tester" {
		t.Fatalf("approval_resolved wrong: %#v", resolved.Payload)
	}
	if result.Type != EventToolResult || result.Payload["output"] != "12:00" || result.Payload["error"] != nil {
		t.Fatalf("tool_result wrong: %#v", result.Payload)
	}
	// exactly once: a second resolution must error
	if err := ApproveToolUse(session, runner, "toolu_t1", ApprovalResolution{}); err == nil || !strings.Contains(err.Error(), "exactly once") {
		t.Fatalf("expected exactly-once error, got %v", err)
	}
	if err := DenyToolUse(session, "toolu_t1", ApprovalResolution{Reason: "late"}); err == nil {
		t.Fatal("deny after resolution must error")
	}
}

func TestDenySynthesizesRejection(t *testing.T) {
	session, _ := approvalFixtureSession(t)
	session.Log.Append(EventToolUse, map[string]any{"id": "toolu_t1", "name": "get_current_time", "arguments": map[string]any{}})

	if err := DenyToolUse(session, "toolu_t1", ApprovalResolution{Reason: "operator said no", ResolvedBy: "tester"}); err != nil {
		t.Fatalf("DenyToolUse: %v", err)
	}
	events := session.Log.Events()
	result := events[len(events)-1]
	if result.Payload["error"] != "denied by approval: operator said no" {
		t.Fatalf("rejection error wrong: %#v", result.Payload)
	}
	envelope, _ := result.Payload["approval"].(map[string]any)
	if envelope["decision"] != "rejected" || envelope["rule_matched"] != "operator said no" {
		t.Fatalf("approval envelope wrong: %#v", envelope)
	}
}

func TestResolveUnknownToolUseErrors(t *testing.T) {
	session, runner := approvalFixtureSession(t)
	if err := ApproveToolUse(session, runner, "toolu_missing", ApprovalResolution{}); err == nil {
		t.Fatal("expected error for unknown tool_use id")
	}
}

// MockToolProvider returns a single tool_use response, then plain text.
type MockToolProvider struct {
	ToolName  string
	ToolUseID string
}

func (p MockToolProvider) Call(request map[string]any) (map[string]any, error) {
	messages, _ := request["messages"].([]map[string]any)
	_ = messages
	return map[string]any{
		"content": []any{
			map[string]any{"type": "tool_use", "id": p.ToolUseID, "name": p.ToolName, "input": map[string]any{}},
		},
		"stop_reason": "tool_use",
		"usage":       map[string]any{"input_tokens": float64(1), "output_tokens": float64(1)},
	}, nil
}
