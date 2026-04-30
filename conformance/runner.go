package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"

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
	manifest, err := LoadManifest(fixtureDir)
	if err != nil {
		return Result{}, err
	}
	streaming := fileExists(filepath.Join(fixtureDir, "provider-script-stream.json"))
	scriptPath := "provider-script.json"
	if streaming {
		scriptPath = "provider-script-stream.json"
	}

	var inputs []any
	if err := readJSON(filepath.Join(fixtureDir, "inputs.json"), &inputs); err != nil {
		return Result{}, err
	}

	expected, err := ReadExpected(filepath.Join(fixtureDir, "expected-log.jsonl"))
	if err != nil {
		return Result{}, err
	}

	session, err := RunSession(manifest, filepath.Join(fixtureDir, scriptPath), inputs, nil)
	if err != nil {
		return Result{}, err
	}

	actual := session.Log.Events()
	diff := FirstDiff(actual, expected)
	return Result{
		Fixture:  fixture,
		Passed:   diff == "",
		Actual:   actual,
		Expected: expected,
		Diff:     diff,
	}, nil
}

func LoadManifest(fixtureDir string) (harnas.Manifest, error) {
	return harnas.ReadManifest(filepath.Join(fixtureDir, "manifest.json"))
}

func RunSession(manifest harnas.Manifest, scriptPath string, inputs []any, session *harnas.Session) (*harnas.Session, error) {
	streaming := filepath.Base(scriptPath) == "provider-script-stream.json" || filepath.Base(scriptPath) == "phase-1-provider-script-stream.json" || filepath.Base(scriptPath) == "phase-2-provider-script-stream.json"

	if session == nil {
		session = harnas.CreateSession(map[string]any{"manifest_name": manifest.Name})
	}
	registry := harnas.NewRegistry()
	for _, tool := range manifest.Tools {
		registry.Register(harnas.Tool{
			Name:        tool.Name,
			Handler:     tool.Handler,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		})
	}
	strategies, err := harnas.BuildStrategies(manifest.Strategies)
	if err != nil {
		return nil, err
	}
	for _, strategy := range strategies {
		strategy.Install(session)
	}

	loop := harnas.AgentLoop{
		Session:    session,
		Projection: harnas.ProjectionFor(manifest.Provider, manifest.System),
		Ingestor:   harnas.IngestorFor(manifest.Provider.Kind),
		RetryPolicy: &harnas.RetryPolicy{
			MaxAttempts:   3,
			RetryableHTTP: map[int]bool{408: true, 429: true, 500: true, 502: true, 503: true, 504: true},
			Backoff:       func(int) time.Duration { return 0 },
		},
		MaxTurns: 3,
	}
	if registry.Size() > 0 {
		loop.Runner = &harnas.Runner{Registry: registry}
	}
	if streaming {
		var streams [][]map[string]any
		if err := readJSON(scriptPath, &streams); err != nil {
			return nil, err
		}
		loop.StreamProvider = NewScriptedStreamProvider(streams)
	} else {
		var script []map[string]any
		if err := readJSON(scriptPath, &script); err != nil {
			return nil, err
		}
		loop.Provider = NewScriptedProvider(script)
	}

	for _, input := range inputs {
		if action, ok := input.(map[string]any); ok {
			if compact, ok := action["compact"]; ok {
				spec := asMap(compact)
				session.Log.Append(harnas.EventCompact, map[string]any{
					"replaces": spec["replaces"],
					"summary":  spec["summary"],
				})
				continue
			}
			if revokes, ok := action["revert"]; ok {
				session.Log.Append(harnas.EventRevert, map[string]any{"revokes": revokes})
				continue
			}
			if forkSpec, ok := action["fork"]; ok {
				atSeq := int(floatValue(asMap(forkSpec)["at_seq"]))
				parent := session
				forked := parent.Fork(atSeq)
				if err := verifyFork(parent, forked, atSeq); err != nil {
					return nil, err
				}
				session = forked
				loop.Session = forked
				continue
			}
			input = action["user"]
		}
		session.Log.Append(harnas.EventUserMessage, map[string]any{"text": stringValue(input)})
		if _, err := loop.Run(); err != nil {
			return nil, err
		}
	}

	return session, nil
}

func verifyFork(parent, forked *harnas.Session, atSeq int) error {
	parentEvents := parent.Log.Events()
	forkedEvents := forked.Log.Events()
	if len(forkedEvents) != atSeq+1 {
		return fmt.Errorf("fork prefix length mismatch")
	}
	for i := 0; i <= atSeq; i++ {
		if !reflect.DeepEqual(forkedEvents[i], parentEvents[i]) {
			return fmt.Errorf("fork prefix mismatch at seq %d", i)
		}
	}
	if forked.Metadata["forked_from"] != parent.ID {
		return fmt.Errorf("forked_from mismatch")
	}
	if int(floatValue(forked.Metadata["forked_at_seq"])) != atSeq {
		return fmt.Errorf("forked_at_seq mismatch")
	}
	return nil
}

func FirstDiff(actual, expected []harnas.Event) string {
	if eventSlicesEqual(actual, expected) {
		return ""
	}
	limit := len(actual)
	if len(expected) < limit {
		limit = len(expected)
	}
	for i := range limit {
		if !eventsEqual(actual[i], expected[i]) {
			return fmt.Sprintf("seq %d actual=%#v expected=%#v", i, actual[i], expected[i])
		}
	}
	return fmt.Sprintf("length actual=%d expected=%d", len(actual), len(expected))
}

func eventSlicesEqual(left, right []harnas.Event) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if !eventsEqual(left[i], right[i]) {
			return false
		}
	}
	return true
}

func eventsEqual(left, right harnas.Event) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
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

func ReadExpected(path string) ([]harnas.Event, error) {
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
