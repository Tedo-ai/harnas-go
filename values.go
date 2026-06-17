package harnas

import (
	"encoding/json"
	"strconv"
)

func asMap(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{}
}

func asFloat(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case json.Number:
		value, err := strconv.ParseFloat(typed.String(), 64)
		if err != nil {
			return 0
		}
		return value
	case int:
		return float64(typed)
	default:
		return 0
	}
}

func firstMap(value any) map[string]any {
	items := asSlice(value)
	if len(items) == 0 {
		return map[string]any{}
	}
	return asMap(items[0])
}

func stringValue(value any) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	return ""
}

func stringSlice(value any) []string {
	items := asSlice(value)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}
