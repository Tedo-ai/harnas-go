package harnas

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Name identifies this projection in Observation events and durable
// error events (see agent_loop.go projectionName).
func (p OpenAIProjection) Name() string { return "openai" }

type OpenAIProjection struct {
	Model                      string
	System                     string
	Registry                   *Registry
	Store                      AttachmentStore
	ProviderKind               string
	Capabilities               map[string]bool
	CapabilityMismatchBehavior string
}

func (p OpenAIProjection) Project(log *Log) (map[string]any, error) {
	messages := []map[string]any{}
	if p.System != "" {
		messages = append(messages, map[string]any{"role": "system", "content": p.System})
	}
	for _, event := range ApplyMutations(log) {
		switch event.Type {
		case EventUserMessage, EventSummary:
			content, err := p.content(event.Payload)
			if err != nil {
				return nil, err
			}
			messages = append(messages, map[string]any{"role": "user", "content": content})
		case EventAssistantMessage:
			if wire, ok := providerCarrierWire(event.Payload["provider_items"], "openai.chat_completions"); ok {
				messages = append(messages, asMap(wire))
				continue
			}
			content, err := p.content(event.Payload)
			if err != nil {
				return nil, err
			}
			messages = append(messages, map[string]any{"role": "assistant", "content": openAIContentForAssistant(content)})
		case EventToolUse:
			toolCall := map[string]any{
				"id":   event.Payload["id"],
				"type": "function",
				"function": map[string]any{
					"name":      event.Payload["name"],
					"arguments": mustJSON(asMap(event.Payload["arguments"])),
				},
			}
			if len(messages) > 0 && messages[len(messages)-1]["role"] == "assistant" {
				last := messages[len(messages)-1]
				last["tool_calls"] = append(asMapSlice(last["tool_calls"]), toolCall)
				if content, ok := last["content"].(string); ok && content == "" {
					last["content"] = nil
				}
			} else {
				messages = append(messages, map[string]any{
					"role":       "assistant",
					"content":    nil,
					"tool_calls": []map[string]any{toolCall},
				})
			}
		case EventToolResult:
			content := stringValue(event.Payload["output"])
			if errText := stringValue(event.Payload["error"]); errText != "" {
				content = errText
			}
			messages = append(messages, map[string]any{
				"role":         "tool",
				"tool_call_id": event.Payload["tool_use_id"],
				"content":      content,
			})
		}
	}
	request := map[string]any{
		"model":    p.Model,
		"messages": messages,
	}
	if p.Registry != nil && p.Registry.Size() > 0 {
		request["tools"] = openAIToolDescriptors(p.Registry)
	}
	return request, nil
}

func (p OpenAIProjection) content(payload map[string]any) (any, error) {
	blocks := messageContentBlocks(payload)
	if len(blocks) == 1 && stringValue(blocks[0]["type"]) == "text" {
		return stringValue(blocks[0]["text"]), nil
	}
	var wire []map[string]any
	for _, block := range blocks {
		switch stringValue(block["type"]) {
		case "text":
			wire = append(wire, map[string]any{"type": "text", "text": stringValue(block["text"])})
		case "image":
			if fallback, ok, err := p.fallbackIfUnsupported(block); !ok || err != nil {
				return nil, err
			} else if fallback != nil {
				wire = append(wire, fallback)
				continue
			}
			imageURL, err := p.imageURL(block)
			if err != nil {
				return nil, err
			}
			wire = append(wire, map[string]any{"type": "image_url", "image_url": map[string]any{"url": imageURL}})
		case "document":
			if fallback, ok, err := p.fallbackIfUnsupported(block); !ok || err != nil {
				return nil, err
			} else if fallback != nil {
				wire = append(wire, fallback)
				continue
			}
			return nil, fmt.Errorf("OpenAI document content is not supported")
		default:
			return nil, fmt.Errorf("unsupported OpenAI content block type: %s", stringValue(block["type"]))
		}
	}
	return wire, nil
}

func (p OpenAIProjection) fallbackIfUnsupported(block map[string]any) (map[string]any, bool, error) {
	providerKind := p.ProviderKind
	if providerKind == "" {
		providerKind = "openai"
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

func (p OpenAIProjection) imageURL(block map[string]any) (string, error) {
	source := asMap(block["source"])
	if source["kind"] == "url" {
		return stringValue(source["url"]), nil
	}
	resolved, err := resolveContentData(block, p.Store)
	if err != nil {
		return "", err
	}
	return "data:" + resolved.MediaType + ";base64," + resolved.Data, nil
}

func openAIContentForAssistant(content any) any {
	if text, ok := content.(string); ok {
		return text
	}
	return content
}

func mustJSON(value any) string {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(value)
	if err != nil {
		return "{}"
	}
	return strings.TrimSuffix(buf.String(), "\n")
}

func asMapSlice(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if mapped, ok := item.(map[string]any); ok {
				out = append(out, mapped)
			}
		}
		return out
	default:
		return nil
	}
}
func openAIToolDescriptors(registry *Registry) []map[string]any {
	tools := []map[string]any{}
	for _, tool := range registry.Tools() {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  tool.InputSchema,
			},
		})
	}
	return tools
}
