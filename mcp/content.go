package mcp

import (
	"encoding/base64"
	"fmt"
)

func Flatten(contentItems []map[string]any) string {
	if len(contentItems) == 0 {
		return ""
	}
	out := ""
	for i, item := range contentItems {
		if i > 0 {
			out += "\n\n"
		}
		out += flattenItem(item)
	}
	return out
}

func flattenItem(item map[string]any) string {
	typ, _ := item["type"].(string)
	switch typ {
	case "text":
		return stringValue(item["text"])
	case "image":
		return fmt.Sprintf("[image: %s, %d bytes]", stringValue(item["mimeType"]), decodedSize(stringValue(item["data"])))
	case "resource", "resource_link":
		uri := stringValue(item["uri"])
		if uri == "" {
			if resource, ok := item["resource"].(map[string]any); ok {
				uri = stringValue(resource["uri"])
			}
		}
		return fmt.Sprintf("[resource: %s]", uri)
	default:
		return fmt.Sprintf("[%s]", typ)
	}
}

func decodedSize(data string) int {
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return len(data)
	}
	return len(decoded)
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}
