package mcp

import "testing"

func TestToolDescriptorFromMCP(t *testing.T) {
	descriptor := ToolDescriptorFromMCP(map[string]any{
		"name":        "fetch_story",
		"description": "Fetch a story",
		"inputSchema": map[string]any{
			"type": "object",
		},
	}, "editorial-ai")

	if descriptor.Name != "editorial-ai.fetch_story" {
		t.Fatalf("unexpected name: %s", descriptor.Name)
	}
	if descriptor.Handler != "mcp_passthrough.editorial-ai" {
		t.Fatalf("unexpected handler: %s", descriptor.Handler)
	}
	if descriptor.Config["mcp_tool_name"] != "fetch_story" {
		t.Fatalf("unexpected config: %#v", descriptor.Config)
	}
}
