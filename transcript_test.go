package harnas

import "testing"

func TestTranscriptProjectMessagesToolsAndErrors(t *testing.T) {
	log := NewLog()
	log.Append(EventUserMessage, map[string]any{"text": "hello"})
	log.Append(EventAssistantMessage, map[string]any{"text": "", "stop_reason": "tool_use", "usage": map[string]any{}})
	log.Append(EventToolUse, map[string]any{"id": "call_1", "name": "read_file", "arguments": map[string]any{"path": "README.md"}})
	log.Append(EventToolResult, map[string]any{"tool_use_id": "call_1", "output": "body", "error": nil})
	log.Append(EventProviderError, map[string]any{"message": "rate limited", "terminal": true})

	items := TranscriptProject(log, DefaultTranscriptOptions())

	kinds := []string{}
	for _, item := range items {
		kinds = append(kinds, item["kind"].(string))
	}
	if got, want := len(kinds), 5; got != want {
		t.Fatalf("got %d items, want %d", got, want)
	}
	if kinds[2] != "tool_use" || items[2]["name"] != "read_file" {
		t.Fatalf("unexpected tool item: %#v", items[2])
	}
	if items[3]["status"] != "ok" {
		t.Fatalf("unexpected tool result: %#v", items[3])
	}
	if items[4]["error"] != "rate limited" {
		t.Fatalf("unexpected provider error: %#v", items[4])
	}
}

func TestTranscriptProjectCanHideTools(t *testing.T) {
	log := NewLog()
	log.Append(EventToolUse, map[string]any{"id": "call_1", "name": "grep", "arguments": map[string]any{}})

	items := TranscriptProject(log, TranscriptOptions{IncludeTools: false, IncludeErrors: true})
	if len(items) != 0 {
		t.Fatalf("expected no items, got %#v", items)
	}
}
