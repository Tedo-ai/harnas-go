package harnas

import (
	"fmt"
)

// Name identifies this projection in Observation events and durable
// error events (see agent_loop.go projectionName).
func (p GeminiProjection) Name() string { return "gemini" }

type GeminiProjection struct {
	Model                      string
	System                     string
	Registry                   *Registry
	Store                      AttachmentStore
	ProviderKind               string
	Capabilities               map[string]bool
	CapabilityMismatchBehavior string
}

func (p GeminiProjection) Project(log *Log) (map[string]any, error) {
	contents := []map[string]any{}
	toolUseNames := map[string]string{}
	for _, event := range ApplyMutations(log) {
		if event.Type == EventToolUse {
			toolUseNames[stringValue(event.Payload["id"])] = stringValue(event.Payload["name"])
		}
	}
	for _, event := range ApplyMutations(log) {
		switch event.Type {
		case EventUserMessage, EventSummary:
			parts, err := p.parts(event.Payload)
			if err != nil {
				return nil, err
			}
			contents = append(contents, map[string]any{
				"role":  "user",
				"parts": parts,
			})
		case EventAssistantMessage:
			parts, err := p.parts(event.Payload)
			if err != nil {
				return nil, err
			}
			if len(parts) > 0 {
				contents = append(contents, map[string]any{
					"role":  "model",
					"parts": parts,
				})
			}
		case EventToolUse:
			part := map[string]any{
				"functionCall": map[string]any{
					"name": event.Payload["name"],
					"args": asMap(event.Payload["arguments"]),
				},
			}
			if len(contents) > 0 && contents[len(contents)-1]["role"] == "model" {
				last := contents[len(contents)-1]
				last["parts"] = append(asMapSlice(last["parts"]), part)
			} else {
				contents = append(contents, map[string]any{
					"role":  "model",
					"parts": []map[string]any{part},
				})
			}
		case EventToolResult:
			response := map[string]any{}
			if errText := stringValue(event.Payload["error"]); errText != "" {
				response["error"] = errText
			} else {
				response["content"] = stringValue(event.Payload["output"])
			}
			toolUseID := stringValue(event.Payload["tool_use_id"])
			wireName := toolUseNames[toolUseID]
			if wireName == "" {
				wireName = toolUseID
			}
			contents = append(contents, map[string]any{
				"role": "user",
				"parts": []map[string]any{{
					"functionResponse": map[string]any{
						"name":     wireName,
						"response": response,
					},
				}},
			})
		}
	}
	request := map[string]any{
		"model":            p.Model,
		"contents":         contents,
		"generationConfig": map[string]any{"thinkingConfig": map[string]any{"thinkingBudget": float64(0)}},
	}
	if p.System != "" {
		request["systemInstruction"] = map[string]any{
			"parts": []map[string]any{{"text": p.System}},
		}
	}
	if p.Registry != nil && p.Registry.Size() > 0 {
		request["tools"] = geminiToolDescriptors(p.Registry)
	}
	return request, nil
}

func (p GeminiProjection) parts(payload map[string]any) ([]map[string]any, error) {
	parts := []map[string]any{}
	for _, block := range messageContentBlocks(payload) {
		switch stringValue(block["type"]) {
		case "text":
			if wire, ok := providerPartWire(block, "gemini.generateContent"); ok {
				parts = append(parts, wire)
				continue
			}
			text := stringValue(block["text"])
			if text != "" {
				parts = append(parts, map[string]any{"text": text})
			}
		case "image", "document":
			if fallback, ok, err := p.fallbackIfUnsupported(block); !ok || err != nil {
				return nil, err
			} else if fallback != nil {
				parts = append(parts, map[string]any{"text": stringValue(fallback["text"])})
				continue
			}
			resolved, err := resolveContentData(block, p.Store)
			if err != nil {
				return nil, err
			}
			parts = append(parts, map[string]any{"inline_data": map[string]any{
				"mime_type": resolved.MediaType,
				"data":      resolved.Data,
			}})
		default:
			return nil, fmt.Errorf("unsupported Gemini content block type: %s", stringValue(block["type"]))
		}
	}
	return parts, nil
}

func (p GeminiProjection) fallbackIfUnsupported(block map[string]any) (map[string]any, bool, error) {
	if capabilitySupported("gemini", p.Model, p.Capabilities, stringValue(block["type"])) {
		return nil, true, nil
	}
	if capabilityMismatchBehavior(p.CapabilityMismatchBehavior) == "error" {
		return nil, false, capabilityMismatch("gemini", p.Model, block)
	}
	fallback, err := fallbackContentBlock(block, p.Store)
	return fallback, true, err
}
func geminiToolDescriptors(registry *Registry) []map[string]any {
	declarations := []map[string]any{}
	for _, tool := range registry.Tools() {
		declarations = append(declarations, map[string]any{
			"name":        tool.Name,
			"description": tool.Description,
			"parameters":  tool.InputSchema,
		})
	}
	return []map[string]any{{"functionDeclarations": declarations}}
}
