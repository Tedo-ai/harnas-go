package harnas

import (
	"encoding/json"
	"fmt"
	"sort"
)

type Tool struct {
	Name        string
	Handler     string
	Description string
	InputSchema map[string]any
	Config      map[string]any
	Call        func(map[string]any) (string, error)
	CallConfig  func(map[string]any, map[string]any) (string, error)
}

type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}}
}

func (r *Registry) Register(tool Tool) error {
	if tool.Name == "" {
		return fmt.Errorf("tool name must not be empty")
	}
	if _, exists := r.tools[tool.Name]; exists {
		return fmt.Errorf("tool already registered: %s", tool.Name)
	}
	r.tools[tool.Name] = tool
	return nil
}

func (r *Registry) Find(name string) (Tool, bool) {
	tool, ok := r.tools[name]
	return tool, ok
}

func (r *Registry) Size() int {
	return len(r.tools)
}

func (r *Registry) Tools() []Tool {
	tools := make([]Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		tools = append(tools, tool)
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools
}

type Runner struct {
	Registry *Registry
}

func (r *Runner) Run(toolUse Event, log *Log) {
	name, _ := toolUse.Payload["name"].(string)
	id, _ := toolUse.Payload["id"].(string)
	args := asMap(toolUse.Payload["arguments"])
	tool, ok := r.Registry.Find(name)
	if !ok {
		log.Append(EventToolResult, map[string]any{
			"tool_use_id": id,
			"output":      nil,
			"error":       fmt.Sprintf("unknown tool: %s", name),
		})
		return
	}
	if tool.Handler == "conformance.raise_error" {
		log.Append(EventToolResult, map[string]any{
			"tool_use_id": id,
			"output":      nil,
			"error":       "RuntimeError: conformance tool error",
		})
		return
	}
	if tool.Handler == "conformance.echo_config" {
		config := tool.Config
		if config == nil {
			config = map[string]any{}
		}
		encoded, _ := json.Marshal(config)
		log.Append(EventToolResult, map[string]any{
			"tool_use_id": id,
			"output":      fmt.Sprintf("[conformance config: %s]", string(encoded)),
			"error":       nil,
		})
		return
	}
	if tool.CallConfig != nil {
		output, err := tool.CallConfig(args, tool.Config)
		if err != nil {
			log.Append(EventToolResult, map[string]any{
				"tool_use_id": id,
				"output":      nil,
				"error":       err.Error(),
			})
			return
		}
		log.Append(EventToolResult, map[string]any{
			"tool_use_id": id,
			"output":      output,
			"error":       nil,
		})
		return
	}
	if tool.Call != nil {
		output, err := tool.Call(args)
		if err != nil {
			log.Append(EventToolResult, map[string]any{
				"tool_use_id": id,
				"output":      nil,
				"error":       err.Error(),
			})
			return
		}
		log.Append(EventToolResult, map[string]any{
			"tool_use_id": id,
			"output":      output,
			"error":       nil,
		})
		return
	}

	encoded, _ := json.Marshal(args)
	log.Append(EventToolResult, map[string]any{
		"tool_use_id": id,
		"output":      fmt.Sprintf("[conformance stub: %s(%s)]", tool.Handler, string(encoded)),
		"error":       nil,
	})
}
