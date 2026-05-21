package harnas

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContentBlockForInputFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.pdf")
	if err := os.WriteFile(path, []byte("pdf"), 0o644); err != nil {
		t.Fatal(err)
	}

	block, err := ContentBlockForFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if block["type"] != "document" || block["media_type"] != "application/pdf" {
		t.Fatalf("unexpected block: %#v", block)
	}
	source := block["source"].(map[string]any)
	if source["kind"] != "base64" || source["data"] != "cGRm" {
		t.Fatalf("unexpected source: %#v", source)
	}
}

func TestContentBlockForInputFileRejectsUnsupportedType(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("text"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := ContentBlockForFile(path); err == nil {
		t.Fatal("expected unsupported input file error")
	}
}
