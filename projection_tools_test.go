package harnas

import "testing"

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	registry := NewRegistry()
	err := registry.Register(Tool{
		Name:        "read_file",
		Description: "Read a file.",
		InputSchema: map[string]any{"type": "object"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestAnthropicProjectionEmitsToolDescriptors(t *testing.T) {
	request, err := (AnthropicProjection{
		Model: "claude-test", MaxTokens: 128, Registry: testRegistry(t),
	}).Project(NewLog())
	if err != nil {
		t.Fatal(err)
	}
	tools := request["tools"].([]map[string]any)
	if tools[0]["name"] != "read_file" || tools[0]["input_schema"] == nil {
		t.Fatalf("unexpected tools: %#v", tools)
	}
}

func TestOpenAIProjectionEmitsToolDescriptors(t *testing.T) {
	request, err := (OpenAIProjection{
		Model: "gpt-test", Registry: testRegistry(t),
	}).Project(NewLog())
	if err != nil {
		t.Fatal(err)
	}
	tools := request["tools"].([]map[string]any)
	fn := tools[0]["function"].(map[string]any)
	if tools[0]["type"] != "function" || fn["name"] != "read_file" {
		t.Fatalf("unexpected tools: %#v", tools)
	}
}

func TestOpenAIProjectionProjectsToolRoundTrip(t *testing.T) {
	log := NewLog()
	log.Append(EventUserMessage, map[string]any{"text": "what time is it?"})
	log.Append(EventAssistantMessage, map[string]any{"text": "", "stop_reason": "tool_use"})
	log.Append(EventToolUse, map[string]any{
		"id":        "call_abc123",
		"name":      "get_current_time",
		"arguments": map[string]any{},
	})
	log.Append(EventToolResult, map[string]any{
		"tool_use_id": "call_abc123",
		"output":      "[conformance stub: conformance.get_current_time({})]",
	})

	request, err := (OpenAIProjection{Model: "gpt-test"}).Project(log)
	if err != nil {
		t.Fatal(err)
	}
	messages := request["messages"].([]map[string]any)
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %#v", messages)
	}
	assistant := messages[1]
	if assistant["role"] != "assistant" || assistant["content"] != nil {
		t.Fatalf("assistant message did not normalize content to nil: %#v", assistant)
	}
	calls := assistant["tool_calls"].([]map[string]any)
	if calls[0]["id"] != "call_abc123" {
		t.Fatalf("missing tool call: %#v", calls)
	}
	fn := calls[0]["function"].(map[string]any)
	if fn["name"] != "get_current_time" || fn["arguments"] != "{}" {
		t.Fatalf("unexpected function call: %#v", fn)
	}
	tool := messages[2]
	if tool["role"] != "tool" || tool["tool_call_id"] != "call_abc123" {
		t.Fatalf("missing tool result projection: %#v", tool)
	}
}

func TestGeminiProjectionEmitsToolDescriptors(t *testing.T) {
	request, err := (GeminiProjection{
		Model: "gemini-test", Registry: testRegistry(t),
	}).Project(NewLog())
	if err != nil {
		t.Fatal(err)
	}
	tools := request["tools"].([]map[string]any)
	declarations := tools[0]["functionDeclarations"].([]map[string]any)
	if declarations[0]["name"] != "read_file" {
		t.Fatalf("unexpected tools: %#v", tools)
	}
}
