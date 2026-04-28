package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	harnas "github.com/Tedo-ai/harnas-go"
)

type Result struct {
	Fixture  string
	Passed   bool
	Actual   []harnas.Event
	Expected []harnas.Event
	Diff     string
}

func Run(fixtureDir string) (Result, error) {
	fixture := filepath.Base(fixtureDir)

	var manifest struct {
		Name     string `json:"name"`
		Provider struct {
			Kind      string `json:"kind"`
			Model     string `json:"model"`
			MaxTokens int    `json:"max_tokens"`
		} `json:"provider"`
		Tools []struct {
			Name    string `json:"name"`
			Handler string `json:"handler"`
		} `json:"tools"`
		Strategies []struct {
			Name   string         `json:"name"`
			Config map[string]any `json:"config"`
		} `json:"strategies"`
	}
	if err := readJSON(filepath.Join(fixtureDir, "manifest.json"), &manifest); err != nil {
		return Result{}, err
	}

	streaming := fileExists(filepath.Join(fixtureDir, "provider-script-stream.json"))

	var inputs []string
	if err := readJSON(filepath.Join(fixtureDir, "inputs.json"), &inputs); err != nil {
		return Result{}, err
	}

	expected, err := readExpected(filepath.Join(fixtureDir, "expected-log.jsonl"))
	if err != nil {
		return Result{}, err
	}

	session := harnas.CreateSession(map[string]any{"manifest_name": manifest.Name})
	registry := harnas.NewRegistry()
	for _, tool := range manifest.Tools {
		registry.Register(harnas.Tool{Name: tool.Name, Handler: tool.Handler})
	}
	for _, strategy := range manifest.Strategies {
		if strategy.Name == "Compaction::MarkerTail" {
			harnas.MarkerTail{
				MaxMessages: int(strategy.Config["max_messages"].(float64)),
				KeepRecent:  int(strategy.Config["keep_recent"].(float64)),
			}.Install(session)
		}
		if strategy.Name == "Permission::DenyByName" {
			harnas.DenyByName{
				Names:        stringSlice(strategy.Config["names"]),
				ReasonFormat: optionalString(strategy.Config["reason_format"]),
			}.Install(session)
		}
	}

	loop := harnas.AgentLoop{
		Session:    session,
		Projection: projectionFor(manifest.Provider.Model, manifest.Provider.MaxTokens),
		Ingestor:   ingestorFor(manifest.Provider.Kind),
		MaxTurns:   3,
	}
	if registry.Size() > 0 {
		loop.Runner = &harnas.Runner{Registry: registry}
	}
	if streaming {
		var streams [][]map[string]any
		if err := readJSON(filepath.Join(fixtureDir, "provider-script-stream.json"), &streams); err != nil {
			return Result{}, err
		}
		loop.StreamProvider = NewScriptedStreamProvider(streams)
	} else {
		var script []map[string]any
		if err := readJSON(filepath.Join(fixtureDir, "provider-script.json"), &script); err != nil {
			return Result{}, err
		}
		loop.Provider = NewScriptedProvider(script)
	}

	for _, input := range inputs {
		session.Log.Append(harnas.EventUserMessage, map[string]any{"text": input})
		if _, err := loop.Run(); err != nil {
			return Result{}, err
		}
	}

	actual := session.Log.Events()
	diff := firstDiff(actual, expected)
	return Result{
		Fixture:  fixture,
		Passed:   diff == "",
		Actual:   actual,
		Expected: expected,
		Diff:     diff,
	}, nil
}

func firstDiff(actual, expected []harnas.Event) string {
	if reflect.DeepEqual(actual, expected) {
		return ""
	}
	limit := len(actual)
	if len(expected) < limit {
		limit = len(expected)
	}
	for i := range limit {
		if !reflect.DeepEqual(actual[i], expected[i]) {
			return fmt.Sprintf("seq %d actual=%#v expected=%#v", i, actual[i], expected[i])
		}
	}
	return fmt.Sprintf("length actual=%d expected=%d", len(actual), len(expected))
}

func projectionFor(model string, maxTokens int) harnas.Projection {
	return harnas.AnthropicProjection{Model: model, MaxTokens: maxTokens}
}

func ingestorFor(kind string) harnas.Ingestor {
	switch kind {
	case "openai":
		return harnas.OpenAIIngestor{}
	case "gemini":
		return &harnas.GeminiIngestor{}
	default:
		return harnas.AnthropicIngestor{}
	}
}

func stringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func optionalString(value any) string {
	text, _ := value.(string)
	return text
}

func readJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func fileExists(path string) bool {
	stat, err := os.Stat(path)
	return err == nil && !stat.IsDir()
}

func readExpected(path string) ([]harnas.Event, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := splitJSONLines(data)
	events := make([]harnas.Event, 0, len(lines))
	for _, line := range lines {
		var event harnas.Event
		if err := json.Unmarshal(line, &event); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		events = append(events, event)
	}
	return events, nil
}

func splitJSONLines(data []byte) [][]byte {
	out := [][]byte{}
	for _, line := range bytesSplit(data, '\n') {
		if len(line) > 0 {
			out = append(out, line)
		}
	}
	return out
}

func bytesSplit(data []byte, sep byte) [][]byte {
	var out [][]byte
	start := 0
	for i, b := range data {
		if b == sep {
			out = append(out, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		out = append(out, data[start:])
	}
	return out
}
