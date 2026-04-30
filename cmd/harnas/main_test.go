package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	harnas "github.com/Tedo-ai/harnas-go"
)

func TestInspectJSON(t *testing.T) {
	dir := t.TempDir()
	session := harnas.NewSession("ses_json", nil, nil)
	session.Log.Append(harnas.EventUserMessage, map[string]any{"text": "hello"})
	path := filepath.Join(dir, "session.jsonl")
	must(t, session.Save(path))

	var stdout, stderr bytes.Buffer
	status := run([]string{"inspect", path, "--json"}, &stdout, &stderr)
	if status != 0 {
		t.Fatalf("status=%d stderr=%s", status, stderr.String())
	}
	var payload map[string]any
	must(t, json.Unmarshal(stdout.Bytes(), &payload))
	if payload["event_counts"].(map[string]any)["user_message"].(float64) != 1 {
		t.Fatalf("unexpected counts: %v", payload["event_counts"])
	}
}

func TestRunCommandChatsAndSaves(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	manifestPath := filepath.Join(dir, "manifest.json")
	must(t, os.WriteFile(manifestPath, []byte(`{
		"harnas_version":"0.1",
		"name":"cli-test",
		"provider":{"kind":"mock","max_tokens":1024},
		"tools":[],
		"strategies":[]
	}`), 0o644))

	var stdout, stderr bytes.Buffer
	status := run([]string{"run", manifestPath, "--input", "hello"}, &stdout, &stderr)
	if status != 0 {
		t.Fatalf("status=%d stderr=%s", status, stderr.String())
	}
	if stdout.String() != "ok\n" {
		t.Fatalf("unexpected stdout: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "saved: ") {
		t.Fatalf("missing save path: %s", stderr.String())
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".harnas", "runs", "*-cli-test.jsonl"))
	must(t, err)
	if len(matches) != 1 {
		t.Fatalf("expected saved run, got %v", matches)
	}
}

func TestChatCommandLoopsUntilExit(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	manifestPath := filepath.Join(dir, "manifest.json")
	must(t, os.WriteFile(manifestPath, []byte(`{
		"harnas_version":"0.1",
		"name":"cli-chat",
		"provider":{"kind":"mock","max_tokens":1024},
		"tools":[],
		"strategies":[]
	}`), 0o644))

	var stdout, stderr bytes.Buffer
	err := runChat([]string{manifestPath}, strings.NewReader("hello\nexit\n"), &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "harnas chat") || !strings.Contains(stdout.String(), "ok") {
		t.Fatalf("unexpected stdout: %s", stdout.String())
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".harnas", "runs", "*-cli-chat.jsonl"))
	must(t, err)
	if len(matches) != 1 {
		t.Fatalf("expected saved run, got %v", matches)
	}
}

func TestForkWritesPrefix(t *testing.T) {
	dir := t.TempDir()
	session := harnas.NewSession("ses_parent", nil, map[string]any{"label": "demo"})
	session.Log.Append(harnas.EventUserMessage, map[string]any{"text": "hello"})
	session.Log.Append(harnas.EventAssistantMessage, map[string]any{
		"text": "hi", "stop_reason": "end_turn", "usage": map[string]any{},
	})
	session.Log.Append(harnas.EventUserMessage, map[string]any{"text": "again"})
	source := filepath.Join(dir, "source.jsonl")
	target := filepath.Join(dir, "forked.jsonl")
	must(t, session.Save(source))

	var stdout, stderr bytes.Buffer
	status := run([]string{"fork", source, "--at-seq", "1", "--out", target}, &stdout, &stderr)
	if status != 0 {
		t.Fatalf("status=%d stderr=%s", status, stderr.String())
	}
	forked, err := harnas.LoadSession(target)
	must(t, err)
	if !strings.Contains(stdout.String(), "forked ses_parent at seq 1") {
		t.Fatalf("unexpected stdout: %s", stdout.String())
	}
	if forked.ID == session.ID || forked.Metadata["forked_from"] != "ses_parent" {
		t.Fatalf("unexpected fork metadata: %#v", forked.Metadata)
	}
	if len(forked.Log.Events()) != 2 || forked.Log.Events()[1].ID != session.Log.Events()[1].ID {
		t.Fatalf("fork did not preserve prefix ids")
	}
}

func TestDiffReportsMatchAndDifference(t *testing.T) {
	dir := t.TempDir()
	left := harnas.NewSession("ses_diff", nil, nil)
	left.Log.Append(harnas.EventUserMessage, map[string]any{"text": "hello"})
	right := harnas.NewSession("ses_diff", nil, nil)
	right.Log.Append(harnas.EventUserMessage, map[string]any{"text": "goodbye"})
	leftPath := filepath.Join(dir, "left.jsonl")
	samePath := filepath.Join(dir, "same.jsonl")
	rightPath := filepath.Join(dir, "right.jsonl")
	must(t, left.Save(leftPath))
	must(t, left.Save(samePath))
	must(t, right.Save(rightPath))

	var stdout, stderr bytes.Buffer
	status := run([]string{"diff", leftPath, samePath}, &stdout, &stderr)
	if status != 0 || !strings.Contains(stdout.String(), "sessions match (1 events)") {
		t.Fatalf("status=%d stdout=%s stderr=%s", status, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	status = run([]string{"diff", leftPath, rightPath}, &stdout, &stderr)
	if status != 3 || !strings.Contains(stdout.String(), "sessions differ at seq 0") {
		t.Fatalf("status=%d stdout=%s stderr=%s", status, stdout.String(), stderr.String())
	}
}

func TestProjectRendersProviderRequest(t *testing.T) {
	dir := t.TempDir()
	session := harnas.NewSession("ses_project", nil, nil)
	session.Log.Append(harnas.EventUserMessage, map[string]any{"text": "hello"})
	session.Log.Append(harnas.EventAssistantMessage, map[string]any{
		"text": "hi", "stop_reason": "end_turn", "usage": map[string]any{},
	})
	session.Log.Append(harnas.EventUserMessage, map[string]any{"text": "again"})
	sessionPath := filepath.Join(dir, "session.jsonl")
	manifestPath := filepath.Join(dir, "manifest.json")
	must(t, session.Save(sessionPath))
	must(t, os.WriteFile(manifestPath, []byte(`{
		"harnas_version":"0.1",
		"name":"cli-test",
		"provider":{"kind":"mock","model":"mock-test","max_tokens":1024},
		"tools":[],
		"strategies":[]
	}`), 0o644))

	var stdout, stderr bytes.Buffer
	status := run([]string{"project", sessionPath, "--manifest", manifestPath, "--to-seq", "1"}, &stdout, &stderr)
	if status != 0 {
		t.Fatalf("status=%d stderr=%s", status, stderr.String())
	}
	var request map[string]any
	must(t, json.Unmarshal(stdout.Bytes(), &request))
	if request["model"] != "mock-test" || request["max_tokens"].(float64) != 1024 {
		t.Fatalf("unexpected request: %#v", request)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
