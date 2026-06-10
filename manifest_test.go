package harnas

import (
	"context"
	"errors"
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
		APIKeys: map[string]string{"anthropic": "sk-test"},
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
	var versionError UnsupportedVersionError
	if !errors.As(err, &versionError) {
		t.Fatalf("expected UnsupportedVersionError, got %T: %v", err, err)
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
	var handlerError UnresolvedHandlerError
	if !errors.As(err, &handlerError) {
		t.Fatalf("expected UnresolvedHandlerError, got %T: %v", err, err)
	}
}

func TestConfiguredToolHandlerReceivesManifestConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	mustWrite(t, path, `{
		"harnas_version":"0.1",
		"name":"configured-tool",
		"provider":{"kind":"mock","max_tokens":1},
		"tools":[{
			"name":"echo",
			"handler":"test.configured",
			"description":"Echo configured value.",
			"input_schema":{"type":"object"},
			"config":{"prefix":"cfg"}
		}],
		"strategies":[]
	}`)

	loaded, err := LoadManifest(path, ManifestOptions{
		ConfiguredHandlers: map[string]ConfiguredToolHandler{
			"test.configured": func(args map[string]any, config map[string]any) (string, error) {
				return stringValue(config["prefix"]) + ":" + stringValue(args["text"]), nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	event := loaded.Session.Log.Append(EventToolUse, map[string]any{
		"id":        "toolu_1",
		"name":      "echo",
		"arguments": map[string]any{"text": "hello"},
	})
	(&Runner{Registry: loaded.Registry}).Run(event, loaded.Session.Log)
	result := loaded.Session.Log.Events()[1]
	if result.Payload["output"] != "cfg:hello" {
		t.Fatalf("expected configured output, got %#v", result.Payload)
	}
}

func TestWrapV1HandlerKeepsSingleArgumentHandlersCompatible(t *testing.T) {
	wrapped := WrapV1Handler(func(args map[string]any) (string, error) {
		return stringValue(args["text"]), nil
	})

	output, err := wrapped(map[string]any{"text": "hello"}, map[string]any{"ignored": true})
	if err != nil {
		t.Fatal(err)
	}
	if output != "hello" {
		t.Fatalf("expected hello, got %q", output)
	}
}

func TestContextualToolHandlerReceivesToolContext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	mustWrite(t, path, `{
		"harnas_version":"0.1",
		"name":"contextual-tool",
		"provider":{"kind":"mock","max_tokens":1},
		"tools":[{
			"name":"echo",
			"handler":"test.contextual",
			"description":"Echo context.",
			"input_schema":{"type":"object"},
			"config":{"prefix":"ctx"}
		}],
		"strategies":[]
	}`)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	var seen ToolContext
	loaded, err := LoadManifest(path, ManifestOptions{
		ContextualHandlers: map[string]ContextualToolHandler{
			"test.contextual": func(args map[string]any, ctx ToolContext) (string, error) {
				seen = ctx
				if ctx.Context.Err() == nil {
					t.Fatalf("expected canceled context")
				}
				return stringValue(ctx.Config["prefix"]) + ":" + stringValue(args["text"]), nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	event := loaded.Session.Log.Append(EventToolUse, map[string]any{
		"id":        "toolu_ctx",
		"name":      "echo",
		"arguments": map[string]any{"text": "hello"},
	})
	runner := loaded.Runner()
	runner.Context = cancelled
	runner.Extra = map[string]any{"surface": "test"}
	runner.Run(event, loaded.Session.Log)

	result := loaded.Session.Log.Events()[1]
	if result.Payload["output"] != "ctx:hello" {
		t.Fatalf("expected contextual output, got %#v", result.Payload)
	}
	if seen.SessionID != loaded.Session.ID {
		t.Fatalf("expected session id %q, got %q", loaded.Session.ID, seen.SessionID)
	}
	if seen.ToolUseID != "toolu_ctx" || seen.SourceToolUseID != "toolu_ctx" {
		t.Fatalf("expected tool_use id provenance, got %#v", seen)
	}
	if seen.Extra["surface"] != "test" {
		t.Fatalf("expected extra bag to pass through, got %#v", seen.Extra)
	}
}

func TestLoadManifestRejectsSchemaShapeProblems(t *testing.T) {
	cases := map[string]string{
		"missing strategies": `{
			"harnas_version":"0.1",
			"name":"bad",
			"provider":{"kind":"mock","max_tokens":1},
			"tools":[]
		}`,
		"empty system": `{
			"harnas_version":"0.1",
			"name":"bad",
			"system":"",
			"provider":{"kind":"mock","max_tokens":1},
			"tools":[],
			"strategies":[]
		}`,
		"unknown field": `{
			"harnas_version":"0.1",
			"name":"bad",
			"provider":{"kind":"mock","max_tokens":1},
			"tools":[],
			"strategies":[],
			"extra":true
		}`,
		"tool missing description": `{
			"harnas_version":"0.1",
			"name":"bad",
			"provider":{"kind":"mock","max_tokens":1},
			"tools":[{"name":"echo","handler":"test.echo","input_schema":{"type":"object"}}],
			"strategies":[]
		}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "manifest.json")
			mustWrite(t, path, body)

			_, err := LoadManifest(path, ManifestOptions{})
			if err == nil {
				t.Fatal("expected error")
			}
			var validationError ValidationError
			if !errors.As(err, &validationError) {
				t.Fatalf("expected ValidationError, got %T: %v", err, err)
			}
		})
	}
}

func mustWrite(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
