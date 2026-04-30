package harnas

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinHandlersContainsCanonicalTools(t *testing.T) {
	handlers := BuiltinHandlers()
	for _, name := range []string{
		"harnas.builtin.read_file",
		"harnas.builtin.write_file",
		"harnas.builtin.edit_file",
		"harnas.builtin.list_dir",
		"harnas.builtin.glob",
		"harnas.builtin.grep",
		"harnas.builtin.run_shell",
		"harnas.builtin.fetch_url",
	} {
		if handlers[name] == nil {
			t.Fatalf("missing handler %s", name)
		}
	}
}

func TestBuiltinReadWriteEditFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	result, err := BuiltinWriteFile(map[string]any{"path": path, "content": "alpha\nbravo\n"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "12 bytes") {
		t.Fatalf("unexpected result: %s", result)
	}
	body, err := BuiltinReadFile(map[string]any{"path": path})
	if err != nil || body != "alpha\nbravo\n" {
		t.Fatalf("unexpected read: %q %v", body, err)
	}
	_, err = BuiltinEditFile(map[string]any{"path": path, "old_string": "bravo", "new_string": "BRAVO"})
	if err != nil {
		t.Fatal(err)
	}
	if string(mustRead(t, path)) != "alpha\nBRAVO\n" {
		t.Fatalf("edit failed")
	}
}

func TestBuiltinListGlobAndGrep(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.txt"), "Needle\n")
	mustWrite(t, filepath.Join(dir, "b.go"), "needle\n")

	listing, err := BuiltinListDir(map[string]any{"path": dir})
	if err != nil {
		t.Fatal(err)
	}
	if listing != "a.txt\nb.go" {
		t.Fatalf("unexpected listing: %q", listing)
	}
	matches, err := BuiltinGlob(map[string]any{"path": dir, "pattern": "*.go"})
	if err != nil || !strings.Contains(matches, "b.go") {
		t.Fatalf("unexpected glob: %q %v", matches, err)
	}
	grep, err := BuiltinGrep(map[string]any{"path": dir, "pattern": "needle", "case_insensitive": true})
	if err != nil || !strings.Contains(grep, "a.txt:1:Needle") {
		t.Fatalf("unexpected grep: %q %v", grep, err)
	}
}

func TestBuiltinRunShell(t *testing.T) {
	result, err := BuiltinRunShell(map[string]any{"command": "echo hello"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "[exit 0]") || !strings.Contains(result, "hello") {
		t.Fatalf("unexpected shell result: %s", result)
	}
}

func TestBuiltinFetchURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))
	defer server.Close()

	result, err := BuiltinFetchURL(map[string]any{"url": server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "HTTP 200") || !strings.Contains(result, "hello") {
		t.Fatalf("unexpected fetch result: %s", result)
	}
}

func TestBuiltinFetchURLRejectsUnsupportedSchemes(t *testing.T) {
	if _, err := BuiltinFetchURL(map[string]any{"url": "file:///etc/passwd"}); err == nil {
		t.Fatalf("expected error")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
