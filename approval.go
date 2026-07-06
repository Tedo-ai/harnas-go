package harnas

import "fmt"

// RequestApproval is the canonical Action producing the third pre_tool_use
// decision shape (07-permission R7): hold the tool_use un-executed for async
// approval. Composition per tool_use is Refuse > RequestApproval > Allow.
func RequestApproval(reason string) map[string]any {
	return map[string]any{"pending_approval": true, "reason": reason}
}

// RequireApproval is the canonical permission strategy that holds listed
// tools for async approval (07-permission). Unlisted tools are allowed.
type RequireApproval struct {
	Names        []string
	ReasonFormat string
}

func (s RequireApproval) Install(session *Session) {
	session.Hooks.On("pre_tool_use", func(ctx map[string]any) any {
		toolUse, _ := ctx["tool_use"].(Event)
		name, _ := toolUse.Payload["name"].(string)
		if !containsString(s.Names, name) {
			return map[string]any{"allow": true}
		}
		format := s.ReasonFormat
		if format == "" {
			format = "tool $NAME requires approval"
		}
		return map[string]any{
			"pending_approval": true,
			"reason":           replaceName(format, name),
			"requested_by":     "Permission::RequireApproval",
		}
	})
}

// ApprovalResolution carries host-side resolver identity and reason for
// ApproveToolUse / DenyToolUse.
type ApprovalResolution struct {
	Reason     string
	ResolvedBy string
}

// ApproveToolUse resolves a pending approval (07-permission R9): appends an
// approval_resolved Event, then executes exactly that tool_use exactly once
// via the Runner — bypassing pre_tool_use, the decision was resolved by the
// host — and appends its ordinary tool_result. Call this BEFORE re-entering
// AgentLoop.Run so the next provider call sees a valid assistant→tool_result
// pairing.
func ApproveToolUse(session *Session, runner *Runner, toolUseID string, resolution ApprovalResolution) error {
	if runner == nil {
		return fmt.Errorf("ApproveToolUse requires a Runner")
	}
	toolUse, err := unresolvedToolUse(session, toolUseID)
	if err != nil {
		return err
	}
	session.Log.Append(EventApprovalResolved, approvalResolvedPayload(toolUseID, "approved", resolution))
	if err := session.Log.StorageErr(); err != nil {
		return &StorageWriteError{Cause: err}
	}
	runner.ParentSession = session
	runner.Run(toolUse, session.Log)
	if err := session.Log.StorageErr(); err != nil {
		return &StorageWriteError{Cause: err}
	}
	return nil
}

// DenyToolUse resolves a pending approval as denied (07-permission R9):
// appends an approval_resolved Event followed by the synthesized rejection
// tool_result carrying the approval envelope.
func DenyToolUse(session *Session, toolUseID string, resolution ApprovalResolution) error {
	if _, err := unresolvedToolUse(session, toolUseID); err != nil {
		return err
	}
	session.Log.Append(EventApprovalResolved, approvalResolvedPayload(toolUseID, "denied", resolution))
	session.Log.Append(EventToolResult, map[string]any{
		"tool_use_id": toolUseID,
		"output":      nil,
		"error":       "denied by approval: " + resolution.Reason,
		"approval": map[string]any{
			"decision":     "rejected",
			"rule_matched": resolution.Reason,
			"applied_diff": nil,
		},
	})
	if err := session.Log.StorageErr(); err != nil {
		return &StorageWriteError{Cause: err}
	}
	return nil
}

func approvalResolvedPayload(toolUseID, decision string, resolution ApprovalResolution) map[string]any {
	payload := map[string]any{
		"tool_use_id": toolUseID,
		"decision":    decision,
		"reason":      nil,
		"resolved_by": nil,
	}
	if resolution.Reason != "" {
		payload["reason"] = resolution.Reason
	}
	if resolution.ResolvedBy != "" {
		payload["resolved_by"] = resolution.ResolvedBy
	}
	return payload
}

func unresolvedToolUse(session *Session, toolUseID string) (Event, error) {
	var toolUse Event
	found := false
	for _, event := range session.Log.Events() {
		if event.Type == EventToolUse && event.Payload["id"] == toolUseID {
			toolUse = event
			found = true
		}
		if event.Type == EventToolResult && event.Payload["tool_use_id"] == toolUseID {
			return Event{}, fmt.Errorf("tool_use %q already has a tool_result; approvals resolve exactly once", toolUseID)
		}
	}
	if !found {
		return Event{}, fmt.Errorf("no tool_use with id %q in the session log", toolUseID)
	}
	return toolUse, nil
}

func pendingApprovalByHook(decisions []any) (bool, string, string) {
	for _, decision := range decisions {
		result, ok := decision.(map[string]any)
		if !ok {
			continue
		}
		pending, ok := result["pending_approval"].(bool)
		if !ok || !pending {
			continue
		}
		reason, _ := result["reason"].(string)
		requestedBy, _ := result["requested_by"].(string)
		return true, reason, requestedBy
	}
	return false, "", ""
}
