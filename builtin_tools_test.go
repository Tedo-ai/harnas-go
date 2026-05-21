package harnas

import (
	"encoding/json"
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
		"harnas.builtin.load_skill",
		"harnas.builtin.bash_session",
	} {
		if handlers[name] == nil {
			t.Fatalf("missing handler %s", name)
		}
	}
}

func TestBuiltinDescriptorsExposeCanonicalToolSchemas(t *testing.T) {
	descriptors := BuiltinDescriptors()
	if len(descriptors) != 10 {
		t.Fatalf("expected 10 descriptors, got %d", len(descriptors))
	}
	byName := map[string]ToolSpec{}
	for _, descriptor := range descriptors {
		byName[descriptor.Name] = descriptor
		if descriptor.Handler == "" || descriptor.Description == "" || descriptor.InputSchema == nil {
			t.Fatalf("incomplete descriptor: %#v", descriptor)
		}
	}
	for _, name := range []string{"read_file", "write_file", "edit_file", "list_dir", "glob", "grep", "run_shell", "fetch_url", "load_skill", "bash_session"} {
		if byName[name].Name == "" {
			t.Fatalf("missing descriptor %s", name)
		}
	}
	required := byName["grep"].InputSchema["required"].([]any)
	if len(required) != 2 || required[0] != "pattern" || required[1] != "path" {
		t.Fatalf("unexpected grep required schema: %#v", required)
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
	if err != nil || body != "     1\talpha\n     2\tbravo\n" {
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

func TestBuiltinReadFileOffsetLimitAndBinaryGuard(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	mustWrite(t, path, "one\ntwo\nthree\n")
	body, err := BuiltinReadFile(map[string]any{"path": path, "offset": float64(1), "limit": float64(1)})
	if err != nil {
		t.Fatal(err)
	}
	if body != "     2\ttwo\n... [file has 3 total lines; showing 1–2]\n" {
		t.Fatalf("unexpected limited read: %q", body)
	}
	body, err = BuiltinReadFile(map[string]any{"path": path, "offset": float64(10)})
	if err != nil {
		t.Fatal(err)
	}
	if body != "... [file has 3 total lines; offset 10 is past EOF]\n" {
		t.Fatalf("unexpected past EOF read: %q", body)
	}
	binary := filepath.Join(dir, "binary.bin")
	mustWrite(t, binary, "abc\x00def")
	_, err = BuiltinReadFile(map[string]any{"path": binary})
	if err == nil || !strings.Contains(err.Error(), "Cannot read binary file") {
		t.Fatalf("expected binary error, got %v", err)
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

func TestBuiltinLoadSkillStripsFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "git_workflow.md")
	if err := os.WriteFile(path, []byte("---\nname: git_workflow\ndescription: Git conventions\n---\nWrite crisp PR descriptions.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := BuiltinLoadSkill(
		map[string]any{"name": "git_workflow"},
		map[string]any{"skills_dir": dir},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result != "Write crisp PR descriptions.\n" {
		t.Fatalf("unexpected skill body: %q", result)
	}
}

func TestBuiltinLoadSkillRejectsInvalidName(t *testing.T) {
	_, err := BuiltinLoadSkill(
		map[string]any{"name": "foo-bar"},
		map[string]any{"skills_dir": t.TempDir()},
	)
	if err == nil || !strings.Contains(err.Error(), "invalid skill name: foo-bar") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBashSessionPersistsWorkingDirectoryAndEnv(t *testing.T) {
	dir := t.TempDir()
	registry := NewBashSessionRegistry()
	defer registry.Close()
	config := map[string]any{"cwd": dir, "max_output_bytes": float64(4096)}

	first := mustBashSession(t, registry, map[string]any{
		"session_id": "s1",
		"command":    "export MYVAR=hello && cd /tmp",
	}, config)
	if first.Status != "completed" || first.ExitCode == nil || *first.ExitCode != 0 {
		t.Fatalf("unexpected first result: %#v", first)
	}

	second := mustBashSession(t, registry, map[string]any{
		"session_id": "s1",
		"command":    "echo $MYVAR && pwd",
	}, config)
	if !strings.Contains(second.Stdout, "hello\n/tmp\n") {
		t.Fatalf("state did not persist: %#v", second)
	}
}

func TestBashSessionReportsCommandLocalOutput(t *testing.T) {
	registry := NewBashSessionRegistry()
	defer registry.Close()
	config := map[string]any{"cwd": t.TempDir(), "max_output_bytes": float64(4096)}

	first := mustBashSession(t, registry, map[string]any{
		"session_id": "s1",
		"command":    "printf first",
	}, config)
	if first.Stdout != "first" || first.CommandStdout != "first" {
		t.Fatalf("unexpected first output: %#v", first)
	}

	second := mustBashSession(t, registry, map[string]any{
		"session_id": "s1",
		"command":    "printf second >&2",
	}, config)
	if second.Stdout != "first" {
		t.Fatalf("expected cumulative stdout, got %#v", second)
	}
	if second.CommandStdout != "" {
		t.Fatalf("expected command-local stdout to exclude prior output, got %#v", second)
	}
	if second.Stderr != "second" || second.CommandStderr != "second" {
		t.Fatalf("expected command-local stderr, got %#v", second)
	}
}

func TestBashSessionPerCommandEnvDoesNotPersist(t *testing.T) {
	registry := NewBashSessionRegistry()
	defer registry.Close()
	config := map[string]any{"cwd": t.TempDir(), "max_output_bytes": float64(4096)}

	first := mustBashSession(t, registry, map[string]any{
		"session_id": "s1",
		"command":    "echo $MYVAR",
		"env":        map[string]any{"MYVAR": "hello $USER"},
	}, config)
	if first.CommandStdout != "hello $USER\n" {
		t.Fatalf("unexpected env output: %#v", first)
	}
	second := mustBashSession(t, registry, map[string]any{
		"session_id": "s1",
		"command":    "echo $MYVAR",
	}, config)
	if second.CommandStdout != "\n" {
		t.Fatalf("env persisted unexpectedly: %#v", second)
	}
	_, err := registry.Handle(map[string]any{
		"session_id": "s1",
		"command":    "true",
		"env":        map[string]any{"BAD KEY": "value"},
	}, config)
	if err == nil || !strings.Contains(err.Error(), "invalid bash_session env key") {
		t.Fatalf("expected invalid env key error, got %v", err)
	}
}

func TestBashSessionTimeoutStatusAndKill(t *testing.T) {
	registry := NewBashSessionRegistry()
	defer registry.Close()
	config := map[string]any{"cwd": t.TempDir()}

	running := mustBashSession(t, registry, map[string]any{
		"session_id": "s1",
		"command":    "sleep 5",
		"timeout_ms": float64(50),
	}, config)
	if running.Status != "running" || running.ExitCode != nil {
		t.Fatalf("expected running timeout result, got %#v", running)
	}

	status := mustBashSession(t, registry, map[string]any{
		"session_id": "s1",
		"action":     "status",
	}, config)
	if status.Status != "running" {
		t.Fatalf("expected running status, got %#v", status)
	}

	killed := mustBashSession(t, registry, map[string]any{
		"session_id": "s1",
		"action":     "kill",
	}, config)
	if killed.Status != "killed" {
		t.Fatalf("expected killed, got %#v", killed)
	}
}

func TestBashSessionTruncatesAndStripsANSI(t *testing.T) {
	registry := NewBashSessionRegistry()
	defer registry.Close()
	result := mustBashSession(t, registry, map[string]any{
		"session_id": "s1",
		"command":    "printf '\\033[31m0123456789\\033[0m'",
	}, map[string]any{"cwd": t.TempDir(), "max_output_bytes": float64(5)})
	if !result.Truncated {
		t.Fatalf("expected truncation: %#v", result)
	}
	if strings.Contains(result.Stdout, "\x1b") {
		t.Fatalf("ANSI escape sequence was not stripped: %q", result.Stdout)
	}
	if result.Stdout != "56789" {
		t.Fatalf("expected tail output, got %q", result.Stdout)
	}
}

func TestBashSessionNonZeroExitIsToolOutput(t *testing.T) {
	registry := NewBashSessionRegistry()
	defer registry.Close()
	result := mustBashSession(t, registry, map[string]any{
		"session_id": "s1",
		"command":    "false",
	}, map[string]any{"cwd": t.TempDir()})
	if result.Status != "completed" || result.ExitCode == nil || *result.ExitCode != 1 {
		t.Fatalf("expected exit code 1 as output, got %#v", result)
	}
}

func TestBashSessionUnknownStatusErrors(t *testing.T) {
	registry := NewBashSessionRegistry()
	defer registry.Close()
	_, err := registry.Handle(map[string]any{"session_id": "missing", "action": "status"}, nil)
	if err == nil || !strings.Contains(err.Error(), "unknown bash_session session_id") {
		t.Fatalf("expected unknown session error, got %v", err)
	}
}

func mustBashSession(t *testing.T, registry *BashSessionRegistry, args map[string]any, config map[string]any) bashSessionResult {
	t.Helper()
	output, err := registry.Handle(args, config)
	if err != nil {
		t.Fatal(err)
	}
	var result bashSessionResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("invalid bash_session JSON %q: %v", output, err)
	}
	return result
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
