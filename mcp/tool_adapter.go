package mcp

import harnas "github.com/Tedo-ai/harnas-go"

func ToolDescriptorFromMCP(mcpTool map[string]any, serverName string) harnas.ToolSpec {
	originalName := stringValue(mcpTool["name"])
	inputSchema, _ := mcpTool["inputSchema"].(map[string]any)
	if inputSchema == nil {
		inputSchema = map[string]any{}
	}
	return harnas.ToolSpec{
		Name:        serverName + "." + originalName,
		Handler:     passthroughHandlerName(serverName),
		Description: stringValue(mcpTool["description"]),
		InputSchema: inputSchema,
		Config: map[string]any{
			"mcp_server_name": serverName,
			"mcp_tool_name":   originalName,
		},
	}
}
