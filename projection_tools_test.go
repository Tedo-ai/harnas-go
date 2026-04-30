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
