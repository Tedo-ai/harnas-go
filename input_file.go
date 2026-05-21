package harnas

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ContentBlocksForInput(text string, paths []string) ([]map[string]any, error) {
	blocks := []map[string]any{{"type": "text", "text": text}}
	for _, path := range paths {
		block, err := ContentBlockForFile(path)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

func ContentBlockForFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	mediaType, blockType, err := mediaTypeForInputFile(path)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"type":       blockType,
		"media_type": mediaType,
		"name":       filepath.Base(path),
		"source": map[string]any{
			"kind": "base64",
			"data": base64.StdEncoding.EncodeToString(data),
		},
	}, nil
}

func mediaTypeForInputFile(path string) (string, string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg", "image", nil
	case ".png":
		return "image/png", "image", nil
	case ".gif":
		return "image/gif", "image", nil
	case ".webp":
		return "image/webp", "image", nil
	case ".pdf":
		return "application/pdf", "document", nil
	default:
		return "", "", fmt.Errorf("unsupported input file type: %s", path)
	}
}
