package harnas

import (
	"fmt"
)

// Name identifies this projection in Observation events and durable
// error events (see agent_loop.go projectionName).
func (p AnthropicProjection) Name() string { return "anthropic" }

type AnthropicProjection struct {
	Model                      string
	MaxTokens                  int
	System                     string
	Registry                   *Registry
	Store                      AttachmentStore
	ProviderKind               string
	Capabilities               map[string]bool
	CapabilityMismatchBehavior string
}

func (p AnthropicProjection) Project(log *Log) (map[string]any, error) {
	groups := []map[string]any{}
	var current map[string]any
	for _, event := range ApplyMutations(log) {
		role, blocks, err := p.translate(event)
		if err != nil {
			return nil, err
		}
		if role == "" || len(blocks) == 0 {
			continue
		}
		if current != nil && current["role"] == role {
			current["blocks"] = append(asSlice(current["blocks"]), blocks...)
		} else {
			if current != nil {
				groups = append(groups, current)
			}
			current = map[string]any{"role": role, "blocks": blocks}
		}
	}
	if current != nil {
		groups = append(groups, current)
	}
	messages := []map[string]any{}
	for _, group := range groups {
		messages = append(messages, finalizeAnthropicGroup(stringValue(group["role"]), asSlice(group["blocks"])))
	}
	request := map[string]any{
		"model":      p.Model,
		"max_tokens": p.MaxTokens,
		"messages":   messages,
	}
	if p.System != "" {
		request["system"] = p.System
	}
	if p.Registry != nil && p.Registry.Size() > 0 {
		request["tools"] = anthropicToolDescriptors(p.Registry)
	}
	return request, nil
}

func (p AnthropicProjection) translate(event Event) (string, []any, error) {
	switch event.Type {
	case EventUserMessage, EventSummary:
		blocks, err := p.contentBlocks(event.Payload)
		return "user", blocks, err
	case EventAssistantMessage:
		if wire, ok := providerCarrierWires(event.Payload["provider_items"], "anthropic.messages"); ok {
			return "assistant", wire, nil
		}
		blocks := anthropicReasoningBlocks(event)
		contentBlocks, err := p.contentBlocks(event.Payload)
		if err != nil {
			return "", nil, err
		}
		contentBlocks = nonEmptyAnthropicTextBlocks(contentBlocks)
		blocks = append(blocks, contentBlocks...)
		return "assistant", blocks, nil
	case EventToolUse:
		return "assistant", []any{map[string]any{
			"type":  "tool_use",
			"id":    event.Payload["id"],
			"name":  event.Payload["name"],
			"input": asMap(event.Payload["arguments"]),
		}}, nil
	case EventToolResult:
		block := map[string]any{
			"type":        "tool_result",
			"tool_use_id": event.Payload["tool_use_id"],
		}
		if errText := stringValue(event.Payload["error"]); errText != "" {
			block["content"] = errText
			block["is_error"] = true
		} else {
			block["content"] = stringValue(event.Payload["output"])
		}
		return "user", []any{block}, nil
	default:
		return "", nil, nil
	}
}

func nonEmptyAnthropicTextBlocks(blocks []any) []any {
	out := []any{}
	for _, block := range blocks {
		mapped := asMap(block)
		if mapped["type"] == "text" && stringValue(mapped["text"]) == "" {
			continue
		}
		out = append(out, block)
	}
	return out
}

func (p AnthropicProjection) contentBlocks(payload map[string]any) ([]any, error) {
	var blocks []any
	for _, block := range messageContentBlocks(payload) {
		switch stringValue(block["type"]) {
		case "text":
			if wire, ok := providerPartWire(block, "anthropic.messages"); ok {
				blocks = append(blocks, wire)
				continue
			}
			blocks = append(blocks, map[string]any{"type": "text", "text": stringValue(block["text"])})
		case "image":
			if fallback, ok, err := p.fallbackIfUnsupported(block); !ok || err != nil {
				return nil, err
			} else if fallback != nil {
				blocks = append(blocks, fallback)
				continue
			}
			wire, err := p.anthropicMediaBlock("image", block)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, wire)
		case "document":
			if fallback, ok, err := p.fallbackIfUnsupported(block); !ok || err != nil {
				return nil, err
			} else if fallback != nil {
				blocks = append(blocks, fallback)
				continue
			}
			wire, err := p.anthropicMediaBlock("document", block)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, wire)
		default:
			return nil, fmt.Errorf("unsupported content block type: %s", stringValue(block["type"]))
		}
	}
	return blocks, nil
}

func (p AnthropicProjection) fallbackIfUnsupported(block map[string]any) (map[string]any, bool, error) {
	providerKind := p.ProviderKind
	if providerKind == "" {
		providerKind = "anthropic"
	}
	blockType := stringValue(block["type"])
	if capabilitySupported(providerKind, p.Model, p.Capabilities, blockType) {
		return nil, true, nil
	}
	if capabilityMismatchBehavior(p.CapabilityMismatchBehavior) == "error" {
		return nil, false, capabilityMismatch(providerKind, p.Model, block)
	}
	fallback, err := fallbackContentBlock(block, p.Store)
	return fallback, true, err
}

func (p AnthropicProjection) anthropicMediaBlock(kind string, block map[string]any) (map[string]any, error) {
	source := asMap(block["source"])
	if kind == "image" && source["kind"] == "url" {
		return map[string]any{
			"type":   "image",
			"source": map[string]any{"type": "url", "url": stringValue(source["url"])},
		}, nil
	}
	resolved, err := resolveContentData(block, p.Store)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"type": kind,
		"source": map[string]any{
			"type":       "base64",
			"media_type": resolved.MediaType,
			"data":       resolved.Data,
		},
	}, nil
}

func finalizeAnthropicGroup(role string, blocks []any) map[string]any {
	if len(blocks) == 1 {
		if block := asMap(blocks[0]); block["type"] == "text" {
			return map[string]any{"role": role, "content": block["text"]}
		}
	}
	return map[string]any{"role": role, "content": blocks}
}

func anthropicReasoningBlocks(event Event) []any {
	blocks := []any{}
	for _, raw := range asSlice(event.Payload["reasoning"]) {
		block := asMap(raw)
		if block["type"] != "text" {
			continue
		}
		out := map[string]any{"type": "thinking", "thinking": stringValue(block["text"])}
		if signature := stringValue(block["signature"]); signature != "" {
			out["signature"] = signature
		}
		blocks = append(blocks, out)
	}
	return blocks
}
func anthropicToolDescriptors(registry *Registry) []map[string]any {
	tools := []map[string]any{}
	for _, tool := range registry.Tools() {
		tools = append(tools, map[string]any{
			"name":         tool.Name,
			"description":  tool.Description,
			"input_schema": tool.InputSchema,
		})
	}
	return tools
}
