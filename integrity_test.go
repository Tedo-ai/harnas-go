package harnas

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestPrepareProviderCallChecksDurableAndEffectiveIntegrity(t *testing.T) {
	session := NewSession("ses_integrity", NewLog(), nil)
	session.Log.Append(EventUserMessage, map[string]any{"text": "hello"})
	session.Log.Append(EventAssistantMessage, map[string]any{"text": "", "stop_reason": "tool_use"})
	toolUse := session.Log.Append(EventToolUse, map[string]any{
		"id": "tool_1", "name": "read", "arguments": map[string]any{},
	})
	result := session.Log.Append(EventToolResult, map[string]any{
		"tool_use_id": "tool_1", "output": "ok",
	})

	prepared, err := PrepareProviderCall(session)
	if err != nil {
		t.Fatalf("prepare valid transcript: %v", err)
	}
	if prepared.SessionID() != session.ID || prepared.NextSeq() != 4 || prepared.EffectiveHash() == "" {
		t.Fatalf("prepared metadata is incomplete: %#v", prepared)
	}

	// The durable history remains referentially valid, but this mutation hides
	// only the result and makes the provider-visible transcript unsafe.
	session.Log.Append(EventCompact, map[string]any{
		"replaces": []any{float64(result.Seq)},
		"summary":  "result removed incorrectly",
	})
	if violations := AnalyzeDurableLog(session.Log); len(violations) != 0 {
		t.Fatalf("raw log should remain valid, got %#v", violations)
	}
	_, err = PrepareProviderCall(session)
	var integrityErr *TranscriptIntegrityError
	if !errors.As(err, &integrityErr) {
		t.Fatalf("expected effective transcript error, got %v", err)
	}
	if !containsViolation(integrityErr.Violations, IntegrityDomainEffectiveTranscript, IntegrityUnresolvedToolUse, stringValue(toolUse.Payload["id"])) {
		t.Fatalf("missing effective unresolved-tool violation: %#v", integrityErr.Violations)
	}
}

func TestPreparedTranscriptZeroValueCannotProject(t *testing.T) {
	_, err := (PreparedTranscript{}).Project(AnthropicProjection{Model: "m", MaxTokens: 16})
	var integrityErr *TranscriptIntegrityError
	if !errors.As(err, &integrityErr) {
		t.Fatalf("zero PreparedTranscript projected without validation: %v", err)
	}
}

func TestAnalyzeDurableLogRejectsDuplicateToolIdentity(t *testing.T) {
	log := NewLog()
	log.Append(EventToolUse, map[string]any{"id": "duplicate", "name": "a", "arguments": map[string]any{}})
	log.Append(EventToolUse, map[string]any{"id": "duplicate", "name": "b", "arguments": map[string]any{}})
	violations := AnalyzeDurableLog(log)
	if !containsViolation(violations, IntegrityDomainDurableLog, IntegrityDuplicateToolUseID, "duplicate") {
		t.Fatalf("missing duplicate id violation: %#v", violations)
	}
}

func TestAgentLoopRejectsDanglingToolUseBeforeProviderCall(t *testing.T) {
	session := CreateSession(nil)
	session.Log.Append(EventAssistantMessage, map[string]any{"text": "", "stop_reason": "max_tokens"})
	session.Log.Append(EventToolUse, map[string]any{
		"id": "orphan", "name": "write", "arguments": map[string]any{},
	})
	provider := &integrityScriptedProvider{}
	loop := AgentLoop{
		Session: session, Projection: AnthropicProjection{Model: "m", MaxTokens: 16},
		Provider: provider, Ingestor: AnthropicIngestor{},
	}
	_, err := loop.Run()
	var integrityErr *TranscriptIntegrityError
	if !errors.As(err, &integrityErr) {
		t.Fatalf("expected TranscriptIntegrityError, got %v", err)
	}
	if provider.calls != 0 {
		t.Fatalf("provider calls = %d, want zero", provider.calls)
	}
}

func TestRunEntryRejectsDuplicateToolIDsBeforeDispatch(t *testing.T) {
	toolCalls := 0
	registry := NewRegistry()
	if err := registry.Register(Tool{
		Name: "dangerous", Description: "dangerous", InputSchema: map[string]any{"type": "object"},
		Call: func(map[string]any) (string, error) { toolCalls++; return "done", nil },
	}); err != nil {
		t.Fatal(err)
	}
	session := CreateSession(nil)
	session.Log.Append(EventAssistantMessage, map[string]any{"text": "", "stop_reason": "tool_use"})
	for range 2 {
		session.Log.Append(EventToolUse, map[string]any{"id": "duplicate", "name": "dangerous", "arguments": map[string]any{}})
	}
	provider := &integrityScriptedProvider{}
	loop := AgentLoop{
		Session: session, Projection: AnthropicProjection{Model: "m", MaxTokens: 16, Registry: registry},
		Provider: provider, Ingestor: AnthropicIngestor{}, Runner: &Runner{Registry: registry},
	}
	_, err := loop.Run()
	var integrityErr *TranscriptIntegrityError
	if !errors.As(err, &integrityErr) {
		t.Fatalf("expected TranscriptIntegrityError, got %v", err)
	}
	if toolCalls != 0 || provider.calls != 0 {
		t.Fatalf("corrupt log caused side effects: tools=%d provider=%d", toolCalls, provider.calls)
	}
}

func TestPreparedTranscriptBecomingStaleBlocksProviderCall(t *testing.T) {
	session := CreateSession(nil)
	session.Log.Append(EventUserMessage, map[string]any{"text": "hello"})
	session.Hooks.On("pre_provider_call", func(map[string]any) any {
		session.Log.Append(EventAnnotation, map[string]any{"text": "changed after prepare"})
		return nil
	})
	provider := &integrityScriptedProvider{responses: []map[string]any{textProviderResponse("never")}}
	loop := AgentLoop{
		Session: session, Projection: AnthropicProjection{Model: "m", MaxTokens: 16},
		Provider: provider, Ingestor: AnthropicIngestor{},
	}
	_, err := loop.Run()
	var integrityErr *TranscriptIntegrityError
	if !errors.As(err, &integrityErr) || !containsViolation(integrityErr.Violations, IntegrityDomainEffectiveTranscript, IntegrityPreparedTranscriptStale, "") {
		t.Fatalf("expected stale prepared transcript error, got %v", err)
	}
	if provider.calls != 0 {
		t.Fatalf("provider calls = %d, want zero", provider.calls)
	}
}

func TestIncompleteStreamedToolUseGetsDurableErrorAndSessionRecovers(t *testing.T) {
	toolCalls := 0
	registry := NewRegistry()
	if err := registry.Register(Tool{
		Name: "files.execute_code", Description: "execute", InputSchema: map[string]any{"type": "object"},
		Call: func(map[string]any) (string, error) {
			toolCalls++
			return "unexpected", nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	session := CreateSession(nil)
	session.Log.Append(EventUserMessage, map[string]any{"text": "populate the workbook"})
	provider := &incompleteThenRecoveringStreamProvider{}
	loop := AgentLoop{
		Session: session, Projection: AnthropicProjection{Model: "claude-test", MaxTokens: 64, Registry: registry},
		StreamProvider: provider, Ingestor: AnthropicIngestor{}, Runner: &Runner{Registry: registry},
	}
	reason, err := loop.Run()
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if reason != RunReasonIncompleteToolBatch {
		t.Fatalf("first reason = %q, want %q", reason, RunReasonIncompleteToolBatch)
	}
	if toolCalls != 0 {
		t.Fatalf("tool calls = %d, want zero", toolCalls)
	}
	assertExactlyOneToolPair(t, session.Log.Events(), "tu_incomplete")
	result := matchingResult(session.Log.Events(), "tu_incomplete")
	if result == nil || result.Payload["error_class"] != "IncompleteToolResult" || result.Payload["reason"] != "incomplete_tool_result" || result.Payload["stop_reason"] != "max_tokens" {
		t.Fatalf("incomplete result payload = %#v", result)
	}

	session.Log.Append(EventUserMessage, map[string]any{"text": "continue"})
	reason, err = loop.Run()
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if reason != "end_turn" {
		t.Fatalf("second reason = %q, want end_turn", reason)
	}
	wire, err := json.Marshal(provider.requests[1])
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{`"tool_use_id":"tu_incomplete"`, `"is_error":true`, `"type":"tool_result"`, "max_tokens"} {
		if !strings.Contains(string(wire), required) {
			t.Fatalf("second request missing %s: %s", required, wire)
		}
	}
	assertExactlyOneToolPair(t, session.Log.Events(), "tu_incomplete")
}

func TestTruncatedMultiToolBatchExecutesNone(t *testing.T) {
	calls := 0
	registry := NewRegistry()
	for _, name := range []string{"first", "second"} {
		name := name
		if err := registry.Register(Tool{
			Name: name, Description: name, InputSchema: map[string]any{"type": "object"},
			Call: func(map[string]any) (string, error) { calls++; return name, nil },
		}); err != nil {
			t.Fatal(err)
		}
	}
	provider := &integrityScriptedProvider{responses: []map[string]any{{
		"content": []any{
			map[string]any{"type": "tool_use", "id": "call_1", "name": "first", "input": map[string]any{}},
			map[string]any{"type": "tool_use", "id": "call_2", "name": "second", "input": map[string]any{}},
		},
		"stop_reason": "max_tokens",
		"usage":       map[string]any{},
	}}}
	session := CreateSession(nil)
	session.Log.Append(EventUserMessage, map[string]any{"text": "do both"})
	loop := AgentLoop{
		Session: session, Projection: AnthropicProjection{Model: "m", MaxTokens: 16, Registry: registry},
		Provider: provider, Ingestor: AnthropicIngestor{}, Runner: &Runner{Registry: registry},
	}
	reason, err := loop.Run()
	if err != nil || reason != RunReasonIncompleteToolBatch {
		t.Fatalf("Run = (%q, %v), want incomplete batch", reason, err)
	}
	if calls != 0 {
		t.Fatalf("tool calls = %d, want zero", calls)
	}
	assertExactlyOneToolPair(t, session.Log.Events(), "call_1")
	assertExactlyOneToolPair(t, session.Log.Events(), "call_2")
}

func TestToolUseStopWithoutCallsFailsWithoutPersistingStep(t *testing.T) {
	provider := &integrityScriptedProvider{responses: []map[string]any{{
		"content": []any{}, "stop_reason": "tool_use", "usage": map[string]any{},
	}}}
	session := CreateSession(nil)
	session.Log.Append(EventUserMessage, map[string]any{"text": "do something"})
	loop := AgentLoop{
		Session: session, Projection: AnthropicProjection{Model: "m", MaxTokens: 16},
		Provider: provider, Ingestor: AnthropicIngestor{},
	}
	_, err := loop.Run()
	var responseErr *ProviderResponseIntegrityError
	if !errors.As(err, &responseErr) || responseErr.Reason != "missing_tool_use" {
		t.Fatalf("expected missing-tool response error, got %v", err)
	}
	if events := session.Log.Events(); len(events) != 1 || events[0].Type != EventUserMessage {
		t.Fatalf("invalid provider step was persisted: %#v", events)
	}
}

func TestToolUseWithoutRunnerFailsClosedBeforeSecondProviderCall(t *testing.T) {
	provider := &integrityScriptedProvider{responses: []map[string]any{{
		"content":     []any{map[string]any{"type": "tool_use", "id": "call_1", "name": "missing_runner", "input": map[string]any{}}},
		"stop_reason": "tool_use", "usage": map[string]any{},
	}}}
	session := CreateSession(nil)
	session.Log.Append(EventUserMessage, map[string]any{"text": "call it"})
	loop := AgentLoop{
		Session: session, Projection: AnthropicProjection{Model: "m", MaxTokens: 16},
		Provider: provider, Ingestor: AnthropicIngestor{},
	}
	_, err := loop.Run()
	var integrityErr *TranscriptIntegrityError
	if !errors.As(err, &integrityErr) {
		t.Fatalf("expected fail-closed integrity error, got %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls = %d, want only the producing call", provider.calls)
	}
	_, err = loop.Run()
	if !errors.As(err, &integrityErr) || provider.calls != 1 {
		t.Fatalf("re-entry forwarded orphan: err=%v provider_calls=%d", err, provider.calls)
	}
}

func TestIncompleteToolClosureBatchIsAtomicOnStorageFailure(t *testing.T) {
	adapter := &failingAdapter{MemoryStorageAdapter: NewMemoryStorageAdapter(), failAfter: 1, err: fmt.Errorf("storage down")}
	session := CreateSession(nil)
	if err := session.BindStorage(adapter); err != nil {
		t.Fatal(err)
	}
	session.Log.Append(EventUserMessage, map[string]any{"text": "hello"})
	provider := &integrityScriptedProvider{responses: []map[string]any{{
		"content":     []any{map[string]any{"type": "tool_use", "id": "call_1", "name": "write", "input": map[string]any{}}},
		"stop_reason": "max_tokens", "usage": map[string]any{},
	}}}
	loop := AgentLoop{
		Session: session, Projection: AnthropicProjection{Model: "m", MaxTokens: 16},
		Provider: provider, Ingestor: AnthropicIngestor{},
	}
	_, err := loop.Run()
	var writeErr *StorageWriteError
	if !errors.As(err, &writeErr) {
		t.Fatalf("expected StorageWriteError, got %v", err)
	}
	if events := session.Log.Events(); len(events) != 1 || events[0].Type != EventUserMessage {
		t.Fatalf("partial provider step reached memory: %#v", events)
	}
	rows, rowsErr := adapter.EventsSince(nil)
	if rowsErr != nil || len(rows) != 1 || rows[0].Type != EventUserMessage {
		t.Fatalf("partial provider step reached storage: rows=%#v err=%v", rows, rowsErr)
	}
}

func TestRunEntryResumesMixedApprovalBatchBeforeProvider(t *testing.T) {
	calls := map[string]int{}
	registry := NewRegistry()
	for _, name := range []string{"needs_approval", "allowed"} {
		name := name
		if err := registry.Register(Tool{
			Name: name, Description: name, InputSchema: map[string]any{"type": "object"},
			Call: func(map[string]any) (string, error) { calls[name]++; return name + " done", nil },
		}); err != nil {
			t.Fatal(err)
		}
	}
	session := CreateSession(nil)
	RequireApproval{Names: []string{"needs_approval"}}.Install(session)
	session.Log.Append(EventUserMessage, map[string]any{"text": "do both"})
	provider := &integrityScriptedProvider{responses: []map[string]any{
		{
			"content": []any{
				map[string]any{"type": "tool_use", "id": "call_a", "name": "needs_approval", "input": map[string]any{}},
				map[string]any{"type": "tool_use", "id": "call_b", "name": "allowed", "input": map[string]any{}},
			},
			"stop_reason": "tool_use", "usage": map[string]any{},
		},
		textProviderResponse("done"),
	}}
	runner := &Runner{Registry: registry}
	loop := AgentLoop{
		Session: session, Projection: AnthropicProjection{Model: "m", MaxTokens: 16, Registry: registry},
		Provider: provider, Ingestor: AnthropicIngestor{}, Runner: runner,
	}
	reason, err := loop.Run()
	if err != nil || reason != "awaiting_approval" {
		t.Fatalf("first Run = (%q, %v), want awaiting_approval", reason, err)
	}
	if calls["needs_approval"] != 0 || calls["allowed"] != 0 || provider.calls != 1 {
		t.Fatalf("batch executed before approval: calls=%v provider=%d", calls, provider.calls)
	}
	if err := ApproveToolUse(session, runner, "call_a", ApprovalResolution{ResolvedBy: "test"}); err != nil {
		t.Fatal(err)
	}
	reason, err = loop.Run()
	if err != nil || reason != "end_turn" {
		t.Fatalf("second Run = (%q, %v), want end_turn", reason, err)
	}
	if calls["needs_approval"] != 1 || calls["allowed"] != 1 {
		t.Fatalf("tools did not execute exactly once: %v", calls)
	}
	if provider.calls != 2 {
		t.Fatalf("provider calls = %d, want 2", provider.calls)
	}
	assertExactlyOneToolPair(t, session.Log.Events(), "call_a")
	assertExactlyOneToolPair(t, session.Log.Events(), "call_b")
}

type integrityScriptedProvider struct {
	responses []map[string]any
	requests  []map[string]any
	calls     int
}

func (p *integrityScriptedProvider) Call(request map[string]any) (map[string]any, error) {
	p.requests = append(p.requests, request)
	index := p.calls
	p.calls++
	if index >= len(p.responses) {
		return textProviderResponse("ok"), nil
	}
	return p.responses[index], nil
}

type incompleteThenRecoveringStreamProvider struct {
	calls    int
	requests []map[string]any
}

func (p *incompleteThenRecoveringStreamProvider) Call(request map[string]any, emit func(EventArgs)) error {
	p.requests = append(p.requests, request)
	p.calls++
	if p.calls == 1 {
		emit(EventArgs{Type: EventAssistantTurnStarted, Payload: map[string]any{"turn_id": "turn_incomplete"}})
		emit(EventArgs{Type: EventAssistantTurnDone, Payload: map[string]any{"turn_id": "turn_incomplete", "stop_reason": "max_tokens", "usage": map[string]any{}}})
		emit(EventArgs{Type: EventAssistantMessage, Payload: map[string]any{"text": "", "stop_reason": "max_tokens", "usage": map[string]any{}}})
		emit(EventArgs{Type: EventToolUse, Payload: map[string]any{
			"id": "tu_incomplete", "name": "files.execute_code", "arguments": map[string]any{"name": "populate.py"},
		}})
		return nil
	}
	emit(EventArgs{Type: EventAssistantTurnStarted, Payload: map[string]any{"turn_id": "turn_recovered"}})
	emit(EventArgs{Type: EventAssistantTurnDone, Payload: map[string]any{"turn_id": "turn_recovered", "stop_reason": "end_turn", "usage": map[string]any{}}})
	emit(EventArgs{Type: EventAssistantMessage, Payload: map[string]any{"text": "Recovered.", "stop_reason": "end_turn", "usage": map[string]any{}}})
	return nil
}

func textProviderResponse(text string) map[string]any {
	return map[string]any{
		"content":     []any{map[string]any{"type": "text", "text": text}},
		"stop_reason": "end_turn",
		"usage":       map[string]any{},
	}
}

func containsViolation(violations []IntegrityViolation, domain IntegrityDomain, code, toolUseID string) bool {
	for _, violation := range violations {
		if violation.Domain == domain && violation.Code == code && (toolUseID == "" || violation.ToolUseID == toolUseID) {
			return true
		}
	}
	return false
}

func matchingResult(events []Event, id string) *Event {
	for _, event := range events {
		if event.Type == EventToolResult && event.Payload["tool_use_id"] == id {
			copy := event
			return &copy
		}
	}
	return nil
}

func assertExactlyOneToolPair(t *testing.T, events []Event, id string) {
	t.Helper()
	uses := 0
	results := 0
	for _, event := range events {
		if event.Type == EventToolUse && event.Payload["id"] == id {
			uses++
		}
		if event.Type == EventToolResult && event.Payload["tool_use_id"] == id {
			results++
		}
	}
	if uses != 1 || results != 1 {
		t.Fatalf("tool pair %q: uses=%d results=%d, want exactly one each", id, uses, results)
	}
}
