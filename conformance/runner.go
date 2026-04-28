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
}

func Run(fixtureDir string) (Result, error) {
	fixture := filepath.Base(fixtureDir)

	var manifest struct {
		Name     string `json:"name"`
		Provider struct {
			Model     string `json:"model"`
			MaxTokens int    `json:"max_tokens"`
		} `json:"provider"`
	}
	if err := readJSON(filepath.Join(fixtureDir, "manifest.json"), &manifest); err != nil {
		return Result{}, err
	}

	var script []map[string]any
	if err := readJSON(filepath.Join(fixtureDir, "provider-script.json"), &script); err != nil {
		return Result{}, err
	}

	var inputs []string
	if err := readJSON(filepath.Join(fixtureDir, "inputs.json"), &inputs); err != nil {
		return Result{}, err
	}

	expected, err := readExpected(filepath.Join(fixtureDir, "expected-log.jsonl"))
	if err != nil {
		return Result{}, err
	}

	session := harnas.CreateSession(map[string]any{"manifest_name": manifest.Name})
	loop := harnas.AgentLoop{
		Session: session,
		Projection: harnas.AnthropicProjection{
			Model:     manifest.Provider.Model,
			MaxTokens: manifest.Provider.MaxTokens,
		},
		Provider: NewScriptedProvider(script),
		Ingestor: harnas.AnthropicIngestor{},
		MaxTurns: 3,
	}

	for _, input := range inputs {
		session.Log.Append(harnas.EventUserMessage, map[string]any{"text": input})
		if _, err := loop.Run(); err != nil {
			return Result{}, err
		}
	}

	actual := session.Log.Events()
	return Result{
		Fixture:  fixture,
		Passed:   reflect.DeepEqual(actual, expected),
		Actual:   actual,
		Expected: expected,
	}, nil
}

func readJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
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
