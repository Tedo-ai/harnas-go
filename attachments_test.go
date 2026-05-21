package harnas

import (
	"encoding/base64"
	"testing"
)

func TestFilesystemStorePutGetAndListReferenced(t *testing.T) {
	store := NewFilesystemStore(t.TempDir())
	ref, err := store.Put([]byte("image-bytes"), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if ref.URI == "" || ref.Source["kind"] != "ref" {
		t.Fatalf("expected ref source, got %#v", ref)
	}
	data, mediaType, err := store.Get(ref.URI)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "image-bytes" || mediaType != "image/png" {
		t.Fatalf("unexpected attachment: %q %s", data, mediaType)
	}
	log := NewLog()
	log.Append("user_message", map[string]any{
		"content": []any{map[string]any{
			"type": "image", "media_type": "image/png", "source": ref.Source,
		}},
	})
	refs := store.ListReferenced(log)
	if len(refs) != 1 || refs[0] != ref.URI {
		t.Fatalf("unexpected refs: %#v", refs)
	}
	if err := store.Delete(ref.URI); err != nil {
		t.Fatal(err)
	}
	if store.Exists(ref.URI) {
		t.Fatal("expected deleted attachment to be absent")
	}
}

func TestMemoryAndInlineStores(t *testing.T) {
	memory := NewMemoryStore()
	ref, err := memory.Put([]byte("pdf"), "application/pdf")
	if err != nil {
		t.Fatal(err)
	}
	if !memory.Exists(ref.URI) {
		t.Fatal("expected memory ref to exist")
	}
	data, mediaType, err := memory.Get(ref.URI)
	if err != nil || string(data) != "pdf" || mediaType != "application/pdf" {
		t.Fatalf("unexpected memory get: %q %s %v", data, mediaType, err)
	}

	inlineRef, err := (InlineStore{}).Put([]byte("abc"), "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if inlineRef.URI != "" || inlineRef.Source["kind"] != "base64" {
		t.Fatalf("unexpected inline ref: %#v", inlineRef)
	}
	if inlineRef.Source["data"] != base64.StdEncoding.EncodeToString([]byte("abc")) {
		t.Fatalf("unexpected inline data: %#v", inlineRef.Source)
	}
}
