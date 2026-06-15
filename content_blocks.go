package harnas

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"time"
)

var DefaultAttachmentHTTPTimeout = 60 * time.Second

type resolvedContentData struct {
	Data      string
	MediaType string
	ByteSize  int
	URI       string
}

func messageContentBlocks(payload map[string]any) []map[string]any {
	if _, ok := payload["content"]; ok {
		return asContentBlocks(payload["content"])
	}
	if text, ok := payload["text"].(string); ok {
		return []map[string]any{{"type": "text", "text": text}}
	}
	return nil
}

func resolveContentData(block map[string]any, store AttachmentStore) (resolvedContentData, error) {
	source := asMap(block["source"])
	kind := stringValue(source["kind"])
	mediaType := stringValue(block["media_type"])
	switch kind {
	case "base64":
		data := stringValue(source["data"])
		byteSize := 0
		if decoded, err := base64.StdEncoding.DecodeString(data); err == nil {
			byteSize = len(decoded)
		}
		return resolvedContentData{Data: data, MediaType: mediaType, ByteSize: byteSize}, nil
	case "ref":
		uri := stringValue(source["uri"])
		if store == nil {
			return resolvedContentData{}, fmt.Errorf("attachment store required to resolve %s", uri)
		}
		bytes, resolvedMediaType, err := store.Get(uri)
		if err != nil {
			return resolvedContentData{}, fmt.Errorf("resolve attachment %s: %w", uri, err)
		}
		if mediaType == "" {
			mediaType = resolvedMediaType
		}
		return resolvedContentData{
			Data:      base64.StdEncoding.EncodeToString(bytes),
			MediaType: mediaType,
			ByteSize:  len(bytes),
			URI:       uri,
		}, nil
	case "url":
		url := stringValue(source["url"])
		bytes, resolvedMediaType, err := fetchAttachmentURL(url)
		if err != nil {
			return resolvedContentData{}, err
		}
		if mediaType == "" {
			mediaType = resolvedMediaType
		}
		return resolvedContentData{
			Data:      base64.StdEncoding.EncodeToString(bytes),
			MediaType: mediaType,
			ByteSize:  len(bytes),
		}, nil
	default:
		return resolvedContentData{}, fmt.Errorf("unsupported content source kind: %s", kind)
	}
}

func fetchAttachmentURL(url string) ([]byte, string, error) {
	return fetchAttachmentURLWithClient(&http.Client{Timeout: DefaultAttachmentHTTPTimeout}, url)
}

func fetchAttachmentURLWithClient(client HTTPDoer, url string) ([]byte, string, error) {
	if url == "" {
		return nil, "", fmt.Errorf("content source url is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), DefaultAttachmentHTTPTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("fetch attachment url %s: %w", url, err)
	}
	resp, err := client.Do(request) //nolint:gosec // URL sources are explicit user content references.
	if err != nil {
		return nil, "", fmt.Errorf("fetch attachment url %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("fetch attachment url %s: status %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	return data, resp.Header.Get("Content-Type"), nil
}
