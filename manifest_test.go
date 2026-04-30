package harnas

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadManifestBuildsRuntimeBundle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	mustWrite(t, path, `{
		"harnas_version":"0.1",
		"name":"go-loader",
		"system":"You are terse.",
		"provider":{"kind":"anthropic","model":"claude-test","max_tokens":256},
		"tools":[{
			"name":"echo",
			"handler":"test.echo",
			"description":"Echo text.",
			"input_schema":{"type":"object","properties":{"text":{"type":"string"}}}
		}],
		"strategies":[{
			"name":"Compaction::MarkerTail",
			"config":{"max_messages":3,"keep_recent":1}
		}]
	}`)

	loaded, err := LoadManifest(path, ManifestOptions{
		ToolHandlers: map[string]ToolHandler{
			"test.echo": func(args map[string]any) (string, error) {
				return stringValue(args["text"]), nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if loaded.Name != "go-loader" || loaded.Session.Metadata["manifest_name"] != "go-loader" {
		t.Fatalf("unexpected loaded manifest: %#v", loaded)
	}
	if loaded.Registry.Size() != 1 {
		t.Fatalf("expected registered tool")
	}
	if len(loaded.Strategies) != 1 {
		t.Fatalf("expected strategy installation")
	}
	loaded.InstallStrategies()
	request, err := loaded.Projection.Project(loaded.Session.Log)
	if err != nil {
		t.Fatal(err)
	}
	if request["system"] != "You are terse." || request["model"] != "claude-test" {
		t.Fatalf("unexpected request: %#v", request)
	}
}

func TestLoadManifestRejectsUnsupportedVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	mustWrite(t, path, `{
		"harnas_version":"9.9",
		"name":"bad",
		"provider":{"kind":"mock","max_tokens":1},
		"tools":[],
		"strategies":[]
	}`)

	_, err := LoadManifest(path, ManifestOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadManifestRejectsUnresolvedToolHandler(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	mustWrite(t, path, `{
		"harnas_version":"0.1",
		"name":"bad-tool",
		"provider":{"kind":"mock","max_tokens":1},
		"tools":[{
			"name":"echo",
			"handler":"missing.echo",
			"description":"Echo text.",
			"input_schema":{"type":"object"}
		}],
		"strategies":[]
	}`)

	_, err := LoadManifest(path, ManifestOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func mustWrite(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
