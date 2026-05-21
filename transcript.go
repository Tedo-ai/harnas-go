package harnas

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// TranscriptProject returns a UI-neutral semantic view of a Log.
func TranscriptProject(log *Log, options TranscriptOptions) []map[string]any {
	items := []map[string]any{}
	for _, event := range log.Events() {
		payload := event.Payload
		switch event.Type {
		case EventUserMessage:
			items = append(items, transcriptItem(event, map[string]any{
				"kind": "user", "role": "user", "text": transcriptMessageText(payload, options),
			}))
		case EventAssistantMessage:
			items = append(items, transcriptItem(event, map[string]any{
				"kind":        "assistant",
				"role":        "assistant",
				"text":        transcriptMessageText(payload, options),
				"stop_reason": payload["stop_reason"],
				"usage":       payload["usage"],
				"reasoning":   payload["reasoning"],
			}))
		case EventToolUse:
			if options.IncludeTools {
				items = append(items, transcriptItem(event, map[string]any{
					"kind":        "tool_use",
					"name":        payload["name"],
					"tool_use_id": payload["id"],
					"arguments":   payload["arguments"],
				}))
			}
		case EventToolResult:
			if options.IncludeTools {
				status := "ok"
				if payload["error"] != nil {
					status = "error"
				}
				items = append(items, transcriptItem(event, map[string]any{
					"kind":        "tool_result",
					"tool_use_id": payload["tool_use_id"],
					"output":      payload["output"],
					"error":       payload["error"],
					"status":      status,
				}))
			}
		case EventProviderError, EventRuntimeError:
			if options.IncludeErrors {
				items = append(items, transcriptItem(event, map[string]any{
					"kind":     string(event.Type),
					"error":    firstNonEmpty(payload["message"], payload["error"]),
					"terminal": payload["terminal"],
					"payload":  payload,
				}))
			}
		case EventAnnotation:
			if options.IncludeAnnotations {
				items = append(items, transcriptItem(event, map[string]any{
					"kind":            "annotation",
					"annotation_kind": payload["kind"],
					"data":            payload["data"],
				}))
			}
		case EventCompact, EventSummary, EventRevert:
			items = append(items, transcriptItem(event, map[string]any{
				"kind":    string(event.Type),
				"payload": payload,
			}))
		}
	}
	return items
}

type TranscriptOptions struct {
	IncludeTools       bool
	IncludeErrors      bool
	IncludeAnnotations bool
	ContentPlaceholder func(map[string]any) string
}

func DefaultTranscriptOptions() TranscriptOptions {
	return TranscriptOptions{IncludeTools: true, IncludeErrors: true}
}

func transcriptMessageText(payload map[string]any, options TranscriptOptions) string {
	blocks, ok := payload["content"].([]any)
	if !ok {
		return stringValue(payload["text"])
	}
	parts := []string{}
	for _, raw := range blocks {
		block := asMap(raw)
		if block["type"] == "text" {
			parts = append(parts, stringValue(block["text"]))
			continue
		}
		placeholder := ""
		if options.ContentPlaceholder != nil {
			placeholder = options.ContentPlaceholder(block)
		}
		if placeholder == "" {
			placeholder = defaultContentPlaceholder(block)
		}
		parts = append(parts, placeholder)
	}
	return strings.Join(parts, "\n")
}

func defaultContentPlaceholder(block map[string]any) string {
	blockType := stringValue(block["type"])
	mediaType := stringValue(block["media_type"])
	name := stringValue(block["name"])
	size := contentBlockSize(block)
	parts := []string{blockType}
	if name != "" {
		parts = append(parts, name)
	}
	if mediaType != "" {
		parts = append(parts, mediaType)
	}
	if size > 0 {
		parts = append(parts, formatByteSize(size))
	}
	return "[" + strings.Join(parts, ": ") + "]"
}

func contentBlockSize(block map[string]any) int {
	if size := int(asFloat(block["byte_size"])); size > 0 {
		return size
	}
	source := asMap(block["source"])
	if source["kind"] == "base64" {
		decoded, err := base64.StdEncoding.DecodeString(stringValue(source["data"]))
		if err == nil {
			return len(decoded)
		}
	}
	return 0
}

func formatByteSize(size int) string {
	if size >= 1024 {
		return fmt.Sprintf("%dkb", (size+1023)/1024)
	}
	return fmt.Sprintf("%d bytes", size)
}

func transcriptItem(event Event, fields map[string]any) map[string]any {
	item := map[string]any{
		"seq":  event.Seq,
		"id":   event.ID,
		"type": string(event.Type),
	}
	for key, value := range fields {
		item[key] = value
	}
	return item
}

func firstNonEmpty(values ...any) any {
	for _, value := range values {
		if value != nil && value != "" {
			return value
		}
	}
	return nil
}
