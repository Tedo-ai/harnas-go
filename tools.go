package harnas

import (
	"encoding/json"
	"fmt"
)

type Tool struct {
	Name    string
	Handler string
}

type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}}
}

func (r *Registry) Register(tool Tool) {
	r.tools[tool.Name] = tool
}

func (r *Registry) Find(name string) (Tool, bool) {
	tool, ok := r.tools[name]
	return tool, ok
}

func (r *Registry) Size() int {
	return len(r.tools)
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

	encoded, _ := json.Marshal(args)
	log.Append(EventToolResult, map[string]any{
		"tool_use_id": id,
		"output":      fmt.Sprintf("[conformance stub: %s(%s)]", tool.Handler, string(encoded)),
		"error":       nil,
	})
}
