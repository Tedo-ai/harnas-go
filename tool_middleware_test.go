package harnas

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoggedWrapsHandler(t *testing.T) {
	var log bytes.Buffer
	handler := Logged(func(map[string]any) (string, error) {
		return "ok", nil
	}, &log)
	output, err := handler(map[string]any{"x": "y"})
	if err != nil || output != "ok" {
		t.Fatalf("unexpected output: %q %v", output, err)
	}
	if !strings.Contains(log.String(), "tool start") || !strings.Contains(log.String(), "tool ok") {
		t.Fatalf("unexpected log: %s", log.String())
	}
}

func TestRetriedRetriesMatchingErrors(t *testing.T) {
	attempts := 0
	handler := Retried(func(map[string]any) (string, error) {
		attempts++
		if attempts < 2 {
			return "", errors.New("temporary")
		}
		return "ok", nil
	}, 3, func(error) bool { return true })

	output, err := handler(nil)
	if err != nil || output != "ok" || attempts != 2 {
		t.Fatalf("unexpected retry result: %q %v attempts=%d", output, err, attempts)
	}
}

func TestRateLimiterRejectsOverBudget(t *testing.T) {
	limiter := &RateLimiter{PerMinute: 1}
	handler := limiter.Wrap(func(map[string]any) (string, error) { return "ok", nil })
	if _, err := handler(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := handler(nil); err == nil {
		t.Fatalf("expected rate limit error")
	}
}

func TestStaleReadGuardRequiresFreshRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	log := NewLog()
	guard := StaleReadGuard{Log: log, Strict: true}
	read := guard.WrapRead(BuiltinReadFile)
	edit := guard.WrapEdit(BuiltinEditFile)
	if _, err := edit(map[string]any{"path": path, "old_string": "hello", "new_string": "hi"}); err == nil {
		t.Fatalf("expected read-before-edit error")
	}
	if _, err := read(map[string]any{"path": path}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := edit(map[string]any{"path": path, "old_string": "changed", "new_string": "fresh"}); err == nil {
		t.Fatalf("expected stale file error")
	}
}
