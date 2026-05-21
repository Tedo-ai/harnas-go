package harnas

import (
	"fmt"
	"strings"
)

type CapabilityMismatchError struct {
	BlockType string
	Message   string
}

func (e CapabilityMismatchError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "provider does not support " + e.BlockType + " content blocks"
}

func capabilitySupported(providerKind, model string, overrides map[string]bool, blockType string) bool {
	key := "user_message_" + blockType + "s"
	if value, ok := overrides[key]; ok {
		return value
	}
	images, documents := defaultCapabilities(providerKind, model)
	if blockType == "image" {
		return images
	}
	if blockType == "document" {
		return documents
	}
	return true
}

func capabilityMismatchBehavior(value string) string {
	if value == "error" {
		return "error"
	}
	return "metadata_fallback"
}

func fallbackContentBlock(block map[string]any, store AttachmentStore) (map[string]any, error) {
	meta, err := resolveContentData(block, store)
	if err != nil {
		return nil, err
	}
	return map[string]any{"type": "text", "text": metadataFallbackText(block, meta)}, nil
}

func capabilityMismatch(providerKind, model string, block map[string]any) CapabilityMismatchError {
	return CapabilityMismatchError{
		BlockType: stringValue(block["type"]),
		Message:   fmt.Sprintf("%s/%s does not support %s content blocks", providerKind, model, stringValue(block["type"])),
	}
}

func defaultCapabilities(providerKind, model string) (bool, bool) {
	model = strings.ToLower(model)
	switch providerKind {
	case "anthropic", "mock":
		if strings.HasPrefix(model, "claude-2-") {
			return false, false
		}
		if strings.Contains(model, "claude-3-5") ||
			strings.Contains(model, "claude-3-7") ||
			strings.Contains(model, "claude-sonnet-4") ||
			strings.Contains(model, "claude-opus-4") {
			return true, true
		}
		if strings.HasPrefix(model, "claude-3-") || strings.HasPrefix(model, "claude-") {
			return true, false
		}
	case "openai":
		if strings.HasPrefix(model, "gpt-4o") ||
			model == "gpt-4-turbo" ||
			model == "gpt-4-vision-preview" {
			return true, false
		}
	case "gemini":
		if strings.HasPrefix(model, "gemini-1.0-") {
			return true, false
		}
		if strings.HasPrefix(model, "gemini-1.5-") ||
			strings.HasPrefix(model, "gemini-2.0-") ||
			strings.HasPrefix(model, "gemini-3.") ||
			strings.HasPrefix(model, "gemini-") {
			return true, true
		}
	}
	return false, false
}

func metadataFallbackText(block map[string]any, meta resolvedContentData) string {
	blockType := stringValue(block["type"])
	mediaType := stringValue(block["media_type"])
	if mediaType == "" {
		mediaType = meta.MediaType
	}
	segments := []string{
		fmt.Sprintf("[Note: A %s was attached to this message but cannot be viewed by this provider.", blockType),
	}
	if name := stringValue(block["name"]); name != "" {
		segments = append(segments, "Name: "+name+".")
	}
	if mediaType != "" {
		segments = append(segments, "Type: "+mediaType+".")
	}
	if meta.ByteSize > 0 {
		segments = append(segments, fmt.Sprintf("Size: %d bytes.", meta.ByteSize))
	}
	if meta.URI != "" {
		segments = append(segments, "URI: "+meta.URI+".")
	}
	segments = append(segments, "Use available tools to access the content.]")
	return strings.Join(segments, " ")
}
