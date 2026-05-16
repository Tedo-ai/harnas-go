package harnas

// TranscriptProject returns a UI-neutral semantic view of a Log.
func TranscriptProject(log *Log, options TranscriptOptions) []map[string]any {
	items := []map[string]any{}
	for _, event := range log.Events() {
		payload := event.Payload
		switch event.Type {
		case EventUserMessage:
			items = append(items, transcriptItem(event, map[string]any{
				"kind": "user", "role": "user", "text": stringValue(payload["text"]),
			}))
		case EventAssistantMessage:
			items = append(items, transcriptItem(event, map[string]any{
				"kind":        "assistant",
				"role":        "assistant",
				"text":        stringValue(payload["text"]),
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
}

func DefaultTranscriptOptions() TranscriptOptions {
	return TranscriptOptions{IncludeTools: true, IncludeErrors: true}
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
