package harnas

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type AttachmentReference struct {
	URI       string         `json:"uri,omitempty"`
	MediaType string         `json:"media_type"`
	ByteSize  int            `json:"byte_size"`
	SHA256    string         `json:"sha256"`
	Source    map[string]any `json:"source,omitempty"`
}

type AttachmentStore interface {
	Put(data []byte, mediaType string) (AttachmentReference, error)
	Get(uri string) ([]byte, string, error)
	Delete(uri string) error
	Exists(uri string) bool
	ListReferenced(log *Log) []string
}

type FilesystemStore struct {
	Root string
}

func NewFilesystemStore(root string) *FilesystemStore {
	return &FilesystemStore{Root: root}
}

func (s *FilesystemStore) Put(data []byte, mediaType string) (AttachmentReference, error) {
	digest := attachmentSHA256(data)
	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		return AttachmentReference{}, err
	}
	path := filepath.Join(s.Root, digest+attachmentExt(mediaType))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return AttachmentReference{}, err
	}
	return refAttachment(digest, mediaType, len(data), digest), nil
}

func (s *FilesystemStore) Get(uri string) ([]byte, string, error) {
	id, err := attachmentID(uri)
	if err != nil {
		return nil, "", err
	}
	matches, err := filepath.Glob(filepath.Join(s.Root, id+".*"))
	if err != nil {
		return nil, "", err
	}
	if len(matches) == 0 {
		return nil, "", os.ErrNotExist
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		return nil, "", err
	}
	return data, mediaTypeForExt(filepath.Ext(matches[0])), nil
}

func (s *FilesystemStore) Delete(uri string) error {
	id, err := attachmentID(uri)
	if err != nil {
		return err
	}
	matches, err := filepath.Glob(filepath.Join(s.Root, id+".*"))
	if err != nil {
		return err
	}
	for _, path := range matches {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (s *FilesystemStore) Exists(uri string) bool {
	id, err := attachmentID(uri)
	if err != nil {
		return false
	}
	matches, err := filepath.Glob(filepath.Join(s.Root, id+".*"))
	return err == nil && len(matches) > 0
}

func (s *FilesystemStore) ListReferenced(log *Log) []string {
	return ListReferencedAttachments(log)
}

type MemoryStore struct {
	items map[string]memoryAttachment
}

type memoryAttachment struct {
	data      []byte
	mediaType string
	sha256    string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{items: map[string]memoryAttachment{}}
}

func (s *MemoryStore) Put(data []byte, mediaType string) (AttachmentReference, error) {
	digest := attachmentSHA256(data)
	copied := append([]byte(nil), data...)
	s.items["attachment://"+digest] = memoryAttachment{data: copied, mediaType: mediaType, sha256: digest}
	return refAttachment(digest, mediaType, len(data), digest), nil
}

func (s *MemoryStore) Get(uri string) ([]byte, string, error) {
	item, ok := s.items[uri]
	if !ok {
		return nil, "", os.ErrNotExist
	}
	return append([]byte(nil), item.data...), item.mediaType, nil
}

func (s *MemoryStore) Delete(uri string) error {
	delete(s.items, uri)
	return nil
}

func (s *MemoryStore) Exists(uri string) bool {
	_, ok := s.items[uri]
	return ok
}

func (s *MemoryStore) ListReferenced(log *Log) []string {
	return ListReferencedAttachments(log)
}

type InlineStore struct{}

func (InlineStore) Put(data []byte, mediaType string) (AttachmentReference, error) {
	digest := attachmentSHA256(data)
	return AttachmentReference{
		MediaType: mediaType,
		ByteSize:  len(data),
		SHA256:    digest,
		Source: map[string]any{
			"kind": "base64",
			"data": base64.StdEncoding.EncodeToString(data),
		},
	}, nil
}

func (InlineStore) Get(_ string) ([]byte, string, error) {
	return nil, "", fmt.Errorf("InlineStore does not resolve attachment:// refs")
}

func (InlineStore) Delete(_ string) error { return nil }
func (InlineStore) Exists(_ string) bool  { return false }
func (InlineStore) ListReferenced(log *Log) []string {
	return ListReferencedAttachments(log)
}

func ListReferencedAttachments(log *Log) []string {
	seen := map[string]bool{}
	var refs []string
	for _, event := range log.Events() {
		if event.Type != "user_message" && event.Type != "assistant_message" {
			continue
		}
		for _, block := range asContentBlocks(event.Payload["content"]) {
			source := asMap(block["source"])
			if source["kind"] == "ref" {
				uri := stringValue(source["uri"])
				if uri != "" && !seen[uri] {
					seen[uri] = true
					refs = append(refs, uri)
				}
			}
		}
	}
	return refs
}

func asContentBlocks(value any) []map[string]any {
	var out []map[string]any
	for _, item := range anySlice(value) {
		if block := asMap(item); len(block) > 0 {
			out = append(out, block)
		}
	}
	return out
}

func refAttachment(id, mediaType string, byteSize int, digest string) AttachmentReference {
	uri := "attachment://" + id
	return AttachmentReference{
		URI:       uri,
		MediaType: mediaType,
		ByteSize:  byteSize,
		SHA256:    digest,
		Source:    map[string]any{"kind": "ref", "uri": uri},
	}
}

func attachmentSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func attachmentID(uri string) (string, error) {
	id := strings.TrimPrefix(uri, "attachment://")
	if id == uri || id == "" || strings.Contains(id, "/") {
		return "", fmt.Errorf("invalid attachment uri: %s", uri)
	}
	return id, nil
}

func attachmentExt(mediaType string) string {
	switch mediaType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "application/pdf":
		return ".pdf"
	default:
		return ".bin"
	}
}

func mediaTypeForExt(ext string) string {
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".pdf":
		return "application/pdf"
	default:
		return "application/octet-stream"
	}
}
