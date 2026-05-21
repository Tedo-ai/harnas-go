package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
	var err error
	fixtureDir, err = filepath.Abs(fixtureDir)
	if err != nil {
		return Result{}, err
	}
	fixture := filepath.Base(fixtureDir)
	manifest, err := LoadManifest(fixtureDir)
	if err != nil {
		return Result{}, err
	}
	resolveFixturePaths(&manifest, fixtureDir)
	streaming := fileExists(filepath.Join(fixtureDir, "provider-script-stream.json"))
	scriptPath := "provider-script.json"
	if streaming {
		scriptPath = "provider-script-stream.json"
	}
	scriptPath = filepath.Join(fixtureDir, scriptPath)

	var inputs []any
	if err := readJSON(filepath.Join(fixtureDir, "inputs.json"), &inputs); err != nil {
		return Result{}, err
	}

	expected, err := ReadExpected(filepath.Join(fixtureDir, "expected-log.jsonl"))
	if err != nil {
		return Result{}, err
	}

	expectedDeltasPath := filepath.Join(fixtureDir, "expected-deltas.jsonl")
	expectedStrategyEventsPath := filepath.Join(fixtureDir, "expected-strategy-events.jsonl")
	cwd, err := os.Getwd()
	if err != nil {
		return Result{}, err
	}
	if err := os.Chdir(fixtureDir); err != nil {
		return Result{}, err
	}
	defer os.Chdir(cwd)
	session, actualDeltas, actualStrategyEvents, err := RunSessionWithSidecars(
		manifest,
		scriptPath,
		inputs,
		nil,
		expectedDeltasPath,
		expectedStrategyEventsPath,
	)
	if err != nil {
		return Result{}, err
	}

	actual := session.Log.Events()
	diff := FirstDiff(actual, expected)
	if diff == "" && fileExists(expectedDeltasPath) {
		expectedDeltas, err := ReadDeltaExpected(expectedDeltasPath)
		if err != nil {
			return Result{}, err
		}
		diff = FirstDeltaDiff(actualDeltas, expectedDeltas)
	}
	if diff == "" && fileExists(expectedStrategyEventsPath) {
		expectedStrategyEvents, err := ReadStrategyEventExpected(expectedStrategyEventsPath)
		if err != nil {
			return Result{}, err
		}
		diff = FirstStrategyEventDiff(actualStrategyEvents, expectedStrategyEvents)
	}
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

func resolveFixturePaths(manifest *harnas.Manifest, fixtureDir string) {
	for i := range manifest.Tools {
		if manifest.Tools[i].Config == nil {
			continue
		}
		skillsDir := stringValue(manifest.Tools[i].Config["skills_dir"])
		if skillsDir != "" && !filepath.IsAbs(skillsDir) {
			manifest.Tools[i].Config["skills_dir"] = filepath.Join(fixtureDir, skillsDir)
		}
		cwd := stringValue(manifest.Tools[i].Config["cwd"])
		if cwd != "" && !filepath.IsAbs(cwd) {
			manifest.Tools[i].Config["cwd"] = filepath.Join(fixtureDir, cwd)
		}
	}
}

func RunSession(manifest harnas.Manifest, scriptPath string, inputs []any, session *harnas.Session) (*harnas.Session, error) {
	session, _, err := RunSessionWithDeltaPath(manifest, scriptPath, inputs, session, "")
	return session, err
}

func RunSessionWithDeltaPath(manifest harnas.Manifest, scriptPath string, inputs []any, session *harnas.Session, expectedDeltasPath string) (*harnas.Session, []DeltaRow, error) {
	session, deltas, _, err := RunSessionWithSidecars(manifest, scriptPath, inputs, session, expectedDeltasPath, "")
	return session, deltas, err
}

func RunSessionWithSidecars(manifest harnas.Manifest, scriptPath string, inputs []any, session *harnas.Session, expectedDeltasPath string, expectedStrategyEventsPath string) (*harnas.Session, []DeltaRow, []StrategyEventRow, error) {
	streaming := filepath.Base(scriptPath) == "provider-script-stream.json" || filepath.Base(scriptPath) == "phase-1-provider-script-stream.json" || filepath.Base(scriptPath) == "phase-2-provider-script-stream.json"

	if session == nil {
		session = harnas.CreateSession(map[string]any{
			"manifest_name": manifest.Name,
			"manifest":      manifestSnapshot(manifest),
		})
	}
	var deltaPath string
	if expectedDeltasPath != "" && fileExists(expectedDeltasPath) {
		file, err := os.CreateTemp("", "harnas-deltas-*.jsonl")
		if err != nil {
			return nil, nil, nil, err
		}
		deltaPath = file.Name()
		file.Close()
		defer os.Remove(deltaPath)
		harnas.NewDeltaLogger(deltaPath, session.Observation)
	}
	var strategyEventsPath string
	if expectedStrategyEventsPath != "" && fileExists(expectedStrategyEventsPath) {
		file, err := os.CreateTemp("", "harnas-strategy-events-*.jsonl")
		if err != nil {
			return nil, nil, nil, err
		}
		strategyEventsPath = file.Name()
		file.Close()
		defer os.Remove(strategyEventsPath)
		NewStrategyEventCollector(strategyEventsPath, session.Observation)
	}
	registry := harnas.NewRegistry()
	builtinHandlers := harnas.BuiltinHandlers()
	builtinHandlers["harnas.builtin.fetch_url"] = func(args map[string]any) (string, error) {
		encoded, _ := json.Marshal(args)
		return "[conformance stub: harnas.builtin.fetch_url(" + string(encoded) + ")]", nil
	}
	configuredHandlers := harnas.BuiltinConfiguredHandlers()
	for _, tool := range manifest.Tools {
		registered := harnas.Tool{
			Name:        tool.Name,
			Handler:     tool.Handler,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
			Config:      tool.Config,
		}
		if handler := configuredHandlers[tool.Handler]; handler != nil {
			registered.CallConfig = handler
		} else if handler := builtinHandlers[tool.Handler]; handler != nil {
			registered.Call = handler
		}
		registry.Register(registered)
	}
	strategies, err := harnas.BuildStrategies(manifest.Strategies, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, strategy := range strategies {
		strategy.Install(session)
	}
	if err := installHooks(session, manifest.Hooks); err != nil {
		return nil, nil, nil, err
	}

	loop := harnas.AgentLoop{
		Session:    session,
		Projection: harnas.ProjectionForWithRegistry(manifest.Provider, manifest.System, registry),
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
			return nil, nil, nil, err
		}
		loop.StreamProvider = NewScriptedStreamProvider(streams)
	} else {
		var script []map[string]any
		if err := readJSON(scriptPath, &script); err != nil {
			return nil, nil, nil, err
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
					return nil, nil, nil, err
				}
				session = forked
				loop.Session = forked
				continue
			}
			if _, ok := action["save_load"]; ok {
				file, err := os.CreateTemp("", "harnas-session-*.jsonl")
				if err != nil {
					return nil, nil, nil, err
				}
				path := file.Name()
				file.Close()
				defer os.Remove(path)
				if err := session.Save(path); err != nil {
					return nil, nil, nil, err
				}
				reloaded, err := harnas.LoadSession(path)
				if err != nil {
					return nil, nil, nil, err
				}
				if !reflect.DeepEqual(reloaded.Metadata["manifest"], manifestSnapshot(manifest)) {
					return nil, nil, nil, fmt.Errorf("manifest snapshot mismatch")
				}
				session = reloaded
				loop.Session = reloaded
				continue
			}
			input = action["user"]
		}
		session.Log.Append(harnas.EventUserMessage, map[string]any{"text": stringValue(input)})
		if _, err := loop.Run(); err != nil {
			return nil, nil, nil, err
		}
	}

	deltas := []DeltaRow{}
	if deltaPath != "" {
		var err error
		deltas, err = ReadDeltaExpected(deltaPath)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	strategyEvents := []StrategyEventRow{}
	if strategyEventsPath != "" {
		var err error
		strategyEvents, err = ReadStrategyEventExpected(strategyEventsPath)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	return session, deltas, strategyEvents, nil
}

func manifestSnapshot(manifest harnas.Manifest) map[string]any {
	data, _ := json.Marshal(manifest)
	out := map[string]any{}
	_ = json.Unmarshal(data, &out)
	return out
}

func installHooks(session *harnas.Session, hooks []harnas.HookSpec) error {
	handlers := conformanceHookHandlers()
	for _, hook := range hooks {
		handler := handlers[hook.Handler]
		if handler == nil {
			return fmt.Errorf("hook handler %q not in hook_handlers", hook.Handler)
		}
		config := hook.Config
		session.Hooks.OnWithOptions(strings.TrimPrefix(hook.Point, ":"), func(ctx map[string]any) any {
			if ctx == nil {
				ctx = map[string]any{}
			}
			ctx["config"] = config
			return handler(ctx)
		}, harnas.HookOptions{
			OnError: harnas.HookErrorPolicy(onErrorDefault(hook.OnError)),
			Name:    hook.Handler,
			Source:  "hook",
		})
	}
	return nil
}

func conformanceHookHandlers() map[string]harnas.HookHandler {
	return map[string]harnas.HookHandler{
		"conformance.audit_post_tool_use": func(ctx map[string]any) any {
			session, _ := ctx["session"].(*harnas.Session)
			toolUse, _ := ctx["tool_use"].(harnas.Event)
			toolResult, _ := ctx["tool_result"].(*harnas.Event)
			resultSeq := 0
			if toolResult != nil {
				resultSeq = toolResult.Seq
			}
			session.Log.Append(harnas.EventAnnotation, map[string]any{
				"kind": "conformance.hook",
				"data": map[string]any{
					"tool_use_id": toolUse.Payload["id"],
					"result_seq":  float64(resultSeq),
				},
			})
			return nil
		},
		"conformance.raise_hook": func(ctx map[string]any) any {
			panic("conformance hook failure")
		},
	}
}

func onErrorDefault(value string) string {
	if value == "" {
		return "isolate"
	}
	return value
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

type DeltaRow struct {
	Index   float64        `json:"index"`
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload"`
}

func ReadDeltaExpected(path string) ([]DeltaRow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := splitJSONLines(data)
	rows := make([]DeltaRow, 0, len(lines))
	for _, line := range lines {
		var row DeltaRow
		if err := json.Unmarshal(line, &row); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func FirstDeltaDiff(actual, expected []DeltaRow) string {
	if reflect.DeepEqual(actual, expected) {
		return ""
	}
	limit := len(actual)
	if len(expected) < limit {
		limit = len(expected)
	}
	for i := range limit {
		if !reflect.DeepEqual(actual[i], expected[i]) {
			return fmt.Sprintf("delta %d actual=%#v expected=%#v", i, actual[i], expected[i])
		}
	}
	return fmt.Sprintf("delta length actual=%d expected=%d", len(actual), len(expected))
}

type StrategyEventRow struct {
	Index   float64        `json:"index"`
	Event   string         `json:"event"`
	Payload map[string]any `json:"payload"`
}

func ReadStrategyEventExpected(path string) ([]StrategyEventRow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := splitJSONLines(data)
	rows := make([]StrategyEventRow, 0, len(lines))
	for _, line := range lines {
		var row StrategyEventRow
		if err := json.Unmarshal(line, &row); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func FirstStrategyEventDiff(actual, expected []StrategyEventRow) string {
	if reflect.DeepEqual(actual, expected) {
		return ""
	}
	limit := len(actual)
	if len(expected) < limit {
		limit = len(expected)
	}
	for i := range limit {
		if !reflect.DeepEqual(actual[i], expected[i]) {
			return fmt.Sprintf("strategy event %d actual=%#v expected=%#v", i, actual[i], expected[i])
		}
	}
	return fmt.Sprintf("strategy event length actual=%d expected=%d", len(actual), len(expected))
}

type StrategyEventCollector struct {
	path  string
	index int
}

func NewStrategyEventCollector(path string, observation *harnas.Observation) *StrategyEventCollector {
	collector := &StrategyEventCollector{path: path}
	observation.Subscribe(collector.Call)
	return collector
}

func (c *StrategyEventCollector) Call(eventName string, payload map[string]any) {
	if eventName != "strategy_started" && eventName != "strategy_completed" {
		return
	}
	file, err := os.OpenFile(c.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	_ = json.NewEncoder(file).Encode(map[string]any{
		"index":   c.index,
		"event":   eventName,
		"payload": payload,
	})
	c.index++
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
