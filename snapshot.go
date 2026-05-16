package harnas

import "encoding/json"

// ToolDescriptors snapshots the public descriptors of a registry.
func ToolDescriptors(registry *Registry) []ToolSpec {
	if registry == nil {
		return []ToolSpec{}
	}
	descriptors := make([]ToolSpec, 0, registry.Size())
	for _, tool := range registry.Tools() {
		handler := tool.Handler
		if handler == "" {
			handler = tool.Name
		}
		descriptors = append(descriptors, ToolSpec{
			Name:        tool.Name,
			Handler:     handler,
			Description: tool.Description,
			InputSchema: cloneMap(tool.InputSchema),
			Config:      cloneMap(tool.Config),
		})
	}
	return descriptors
}

// ManifestSnapshotMetadata packages dynamic descriptors for Session metadata.
func ManifestSnapshotMetadata(registry *Registry, skills any, mcp any) map[string]any {
	metadata := map[string]any{"tools": ToolDescriptors(registry)}
	if skills != nil {
		metadata["skills"] = cloneAny(skills)
	}
	if mcp != nil {
		metadata["mcp"] = cloneAny(mcp)
	}
	return metadata
}

func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	cloned, _ := cloneAny(value).(map[string]any)
	if cloned == nil {
		return map[string]any{}
	}
	return cloned
}

func cloneAny(value any) any {
	data, _ := json.Marshal(value)
	var out any
	_ = json.Unmarshal(data, &out)
	return out
}
