package harnas

import (
	"encoding/json"
	"fmt"
)

type AnthropicProjection struct {
	Model     string
	MaxTokens int
	System    string
	Registry  *Registry
	Store     AttachmentStore
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
			blocks = append(blocks, map[string]any{"type": "text", "text": stringValue(block["text"])})
		case "image":
			wire, err := p.anthropicMediaBlock("image", block)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, wire)
		case "document":
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

type OpenAIProjection struct {
	Model    string
	System   string
	Registry *Registry
	Store    AttachmentStore
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
			imageURL, err := p.imageURL(block)
			if err != nil {
				return nil, err
			}
			wire = append(wire, map[string]any{"type": "image_url", "image_url": map[string]any{"url": imageURL}})
		default:
			return nil, fmt.Errorf("unsupported OpenAI content block type: %s", stringValue(block["type"]))
		}
	}
	return wire, nil
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
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
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

type GeminiProjection struct {
	Model    string
	System   string
	Registry *Registry
	Store    AttachmentStore
}

func (p GeminiProjection) Project(log *Log) (map[string]any, error) {
	contents := []map[string]any{}
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
			text := stringValue(block["text"])
			if text != "" {
				parts = append(parts, map[string]any{"text": text})
			}
		case "image", "document":
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
