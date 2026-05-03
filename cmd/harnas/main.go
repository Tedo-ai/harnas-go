package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	harnas "github.com/Tedo-ai/harnas-go"
)

const (
	exitSuccess   = 0
	exitUsage     = 1
	exitDifferent = 3
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return exitUsage
	}
	var err error
	var status int
	switch args[0] {
	case "chat":
		err = runChat(args[1:], os.Stdin, stdout, stderr)
	case "diff":
		status, err = runDiff(args[1:], stdout)
	case "fork":
		err = runFork(args[1:], stdout)
	case "inspect":
		err = runInspect(args[1:], stdout)
	case "project":
		err = runProject(args[1:], stdout)
	case "run":
		status, err = runOnce(args[1:], stdout, stderr)
	default:
		usage(stderr)
		return exitUsage
	}
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitUsage
	}
	return status
}

func usage(w io.Writer) {
	fmt.Fprint(w, `usage:
  harnas chat <manifest> [--provider KIND] [--model MODEL]
  harnas diff <a.jsonl> <b.jsonl>
  harnas fork <session.jsonl> --at-seq N --out <new.jsonl>
  harnas inspect <session.jsonl> [--json]
  harnas project <session.jsonl> --manifest PATH [--from-seq N] [--to-seq M]
  harnas run <manifest> --input TEXT [--provider KIND] [--model MODEL]
`)
}

func runChat(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	options, err := parseAgentOptions("chat", args, false)
	if err != nil {
		return err
	}
	agent, err := buildAgent(options)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "harnas chat · agent=%s\n", agent.Name)
	fmt.Fprintln(stdout, "type 'exit' or 'quit' to leave, Ctrl-D to finish")
	scanner := bufio.NewScanner(stdin)
	for {
		fmt.Fprint(stdout, "> ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "exit" || input == "quit" {
			break
		}
		streamed := false
		response, err := agent.Stream(input, func(event harnas.Event) {
			if event.Type == harnas.EventAssistantTextDelta {
				streamed = true
				fmt.Fprint(stdout, event.Payload["chunk"])
			}
		})
		if err != nil {
			return err
		}
		if providerError := terminalProviderError(agent.Session.Log); providerError != nil {
			fmt.Fprintf(stderr, "provider error: %s\n", formatProviderError(providerError))
			continue
		}
		if streamed {
			fmt.Fprintln(stdout)
		} else {
			fmt.Fprintln(stdout, response.Text)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return saveSession(agent, stderr)
}

func runOnce(args []string, stdout, stderr io.Writer) (int, error) {
	options, err := parseAgentOptions("run", args, true)
	if err != nil {
		return exitUsage, err
	}
	agent, err := buildAgent(options)
	if err != nil {
		return exitUsage, err
	}
	response, err := agent.Chat(options.input)
	if err != nil {
		return exitUsage, err
	}
	if err := saveSession(agent, stderr); err != nil {
		return exitUsage, err
	}
	if providerError := terminalProviderError(agent.Session.Log); providerError != nil {
		fmt.Fprintf(stderr, "provider error: %s\n", formatProviderError(providerError))
		return 2, nil
	}
	fmt.Fprintln(stdout, response.Text)
	return exitSuccess, nil
}

type agentOptions struct {
	manifestPath string
	provider     string
	model        string
	input        string
}

func parseAgentOptions(command string, args []string, requireInput bool) (agentOptions, error) {
	options := agentOptions{}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&options.provider, "provider", "", "provider override")
	fs.StringVar(&options.model, "model", "", "model override")
	if requireInput {
		fs.StringVar(&options.input, "input", "", "user input")
	}
	takesValue := map[string]bool{"model": true, "provider": true}
	if requireInput {
		takesValue["input"] = true
	}
	if err := fs.Parse(permuteFlags(args, takesValue)); err != nil {
		return options, err
	}
	if fs.NArg() != 1 {
		return options, fmt.Errorf("usage: harnas %s <manifest>", command)
	}
	if requireInput && options.input == "" {
		return options, fmt.Errorf("--input is required")
	}
	options.manifestPath = fs.Arg(0)
	return options, nil
}

func buildAgent(options agentOptions) (*harnas.Agent, error) {
	manifest, err := harnas.ReadManifest(options.manifestPath)
	if err != nil {
		return nil, err
	}
	if options.provider != "" {
		manifest.Provider.Kind = options.provider
		manifest.Provider.Model = resolveModel(options.provider, options.model)
	} else if options.model != "" {
		manifest.Provider.Model = options.model
	}
	loaded, err := harnas.BuildManifest(manifest, harnas.ManifestOptions{
		ToolHandlers: harnas.BuiltinHandlers(),
	})
	if err != nil {
		return nil, err
	}
	loaded.InstallStrategies()
	return &harnas.Agent{Name: loaded.Name, Session: loaded.Session, Loaded: loaded}, nil
}

func resolveModel(provider, explicit string) string {
	if explicit != "" {
		return explicit
	}
	if env := os.Getenv(strings.ToUpper(provider) + "_MODEL"); env != "" {
		return env
	}
	switch provider {
	case "anthropic":
		return "claude-sonnet-4-5"
	case "openai":
		return "gpt-5.4-mini"
	case "gemini":
		return "gemini-flash-latest"
	default:
		return ""
	}
}

func runInspect(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(permuteFlags(args, map[string]bool{"json": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: harnas inspect <session.jsonl> [--json]")
	}
	session, err := harnas.LoadSession(fs.Arg(0))
	if err != nil {
		return err
	}
	summary := inspectSession(session)
	if *asJSON {
		return writePrettyJSON(stdout, summary)
	}
	fmt.Fprint(stdout, formatInspection(summary))
	return nil
}

func saveSession(agent *harnas.Agent, stderr io.Writer) error {
	path := runPath(agent.Name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := agent.Session.Save(path); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "saved: %s\n", path)
	return nil
}

func runPath(name string) string {
	stamp := time.Now().UTC().Format("20060102-150405")
	return filepath.Join(homeDir(), ".harnas", "runs", stamp+"-"+slug(name)+".jsonl")
}

func homeDir() string {
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return "."
}

func slug(name string) string {
	parts := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	return strings.Join(parts, "-")
}

func terminalProviderError(log *harnas.Log) *harnas.Event {
	events := log.Events()
	var errorEvent *harnas.Event
	var assistantSeq = -1
	for index := range events {
		event := events[index]
		if event.Type == harnas.EventAssistantMessage {
			assistantSeq = event.Seq
		}
		if event.Type == harnas.EventProviderError && event.Payload["terminal"] == true {
			copied := event
			errorEvent = &copied
		}
	}
	if errorEvent != nil && errorEvent.Seq > assistantSeq {
		return errorEvent
	}
	return nil
}

func formatProviderError(event *harnas.Event) string {
	message := stringValue(event.Payload["message"])
	status := event.Payload["status"]
	if status == nil || strings.HasPrefix(message, fmt.Sprintf("HTTP %v", status)) {
		return message
	}
	return fmt.Sprintf("HTTP %v %s", status, message)
}

func inspectSession(session *harnas.Session) map[string]any {
	events := session.Log.Events()
	return map[string]any{
		"session": map[string]any{
			"id":          session.ID,
			"metadata":    session.Metadata,
			"event_count": len(events),
			"first_seq":   firstSeq(events),
			"last_seq":    lastSeq(events),
		},
		"event_counts": eventCounts(events),
		"events":       inspectEvents(events),
	}
}

func firstSeq(events []harnas.Event) any {
	if len(events) == 0 {
		return nil
	}
	return events[0].Seq
}

func lastSeq(events []harnas.Event) any {
	if len(events) == 0 {
		return nil
	}
	return events[len(events)-1].Seq
}

func eventCounts(events []harnas.Event) map[string]int {
	counts := map[string]int{}
	for _, event := range events {
		counts[string(event.Type)]++
	}
	return counts
}

func inspectEvents(events []harnas.Event) []map[string]any {
	out := make([]map[string]any, 0, len(events))
	for _, event := range events {
		out = append(out, map[string]any{
			"seq":     event.Seq,
			"type":    string(event.Type),
			"summary": eventSummary(event),
		})
	}
	return out
}

func eventSummary(event harnas.Event) string {
	payload := event.Payload
	switch event.Type {
	case harnas.EventUserMessage, harnas.EventAssistantMessage, harnas.EventSummary:
		return truncate(stringValue(payload["text"]))
	case harnas.EventToolUse:
		args, _ := json.Marshal(payload["arguments"])
		return fmt.Sprintf("%s %s", payload["name"], args)
	case harnas.EventToolResult:
		if payload["error"] != nil {
			return fmt.Sprintf("error for %s: %s", payload["tool_use_id"], truncate(stringValue(payload["error"])))
		}
		return fmt.Sprintf("ok for %s: %s", payload["tool_use_id"], truncate(stringValue(payload["output"])))
	case harnas.EventProviderError:
		status := payload["status"]
		if status == nil {
			status = "error"
		}
		return fmt.Sprintf("%s %v %s", payload["provider"], status, payload["message"])
	case harnas.EventCompact:
		return fmt.Sprintf("replaces=%v %s", payload["replaces"], truncate(stringValue(payload["summary"])))
	case harnas.EventRevert:
		return fmt.Sprintf("revokes=%v", payload["revokes"])
	default:
		data, _ := json.Marshal(payload)
		return truncate(string(data))
	}
}

func formatInspection(summary map[string]any) string {
	session := summary["session"].(map[string]any)
	lines := []string{
		fmt.Sprintf("session %s", session["id"]),
		fmt.Sprintf("metadata %s", compactJSON(session["metadata"])),
		fmt.Sprintf("events %d seq=%v..%v", session["event_count"], session["first_seq"], session["last_seq"]),
		fmt.Sprintf("counts %s", compactJSON(summary["event_counts"])),
		"",
	}
	for _, event := range summary["events"].([]map[string]any) {
		lines = append(lines, fmt.Sprintf("%4d  %-26s  %s", event["seq"], event["type"], event["summary"]))
	}
	return strings.Join(lines, "\n") + "\n"
}

func runFork(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("fork", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	atSeq := fs.Int("at-seq", -1, "fork at seq")
	out := fs.String("out", "", "output path")
	if err := fs.Parse(permuteFlags(args, map[string]bool{"at-seq": true, "out": true})); err != nil {
		return err
	}
	if fs.NArg() != 1 || *atSeq < 0 || *out == "" {
		return fmt.Errorf("usage: harnas fork <session.jsonl> --at-seq N --out <new.jsonl>")
	}
	session, err := harnas.LoadSession(fs.Arg(0))
	if err != nil {
		return err
	}
	forked := session.Fork(*atSeq)
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		return err
	}
	if err := forked.Save(*out); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "forked %s at seq %d -> %s (%d events)\n", session.ID, *atSeq, *out, len(forked.Log.Events()))
	return nil
}

func runDiff(args []string, stdout io.Writer) (int, error) {
	if len(args) != 2 {
		return exitUsage, fmt.Errorf("usage: harnas diff <a.jsonl> <b.jsonl>")
	}
	leftSession, err := harnas.LoadSession(args[0])
	if err != nil {
		return exitUsage, err
	}
	rightSession, err := harnas.LoadSession(args[1])
	if err != nil {
		return exitUsage, err
	}
	left := comparableRows(leftSession)
	right := comparableRows(rightSession)
	if compactJSON(left) == compactJSON(right) {
		fmt.Fprintf(stdout, "sessions match (%d events)\n", len(left)-1)
		return exitSuccess, nil
	}
	index := firstMismatch(left, right)
	fmt.Fprintf(stdout, "sessions differ at %s\n", diffLabel(index))
	fmt.Fprintf(stdout, "left:  %s\n", formatRow(rowAt(left, index)))
	fmt.Fprintf(stdout, "right: %s\n", formatRow(rowAt(right, index)))
	return exitDifferent, nil
}

func comparableRows(session *harnas.Session) []any {
	rows := []any{map[string]any{"session": map[string]any{
		"id":       session.ID,
		"metadata": session.Metadata,
	}}}
	for _, event := range session.Log.Events() {
		rows = append(rows, map[string]any{
			"seq":     event.Seq,
			"id":      event.ID,
			"type":    string(event.Type),
			"payload": event.Payload,
		})
	}
	return rows
}

func firstMismatch(left, right []any) int {
	limit := len(left)
	if len(right) > limit {
		limit = len(right)
	}
	for i := 0; i < limit; i++ {
		if compactJSON(rowAt(left, i)) != compactJSON(rowAt(right, i)) {
			return i
		}
	}
	return 0
}

func rowAt(rows []any, index int) any {
	if index >= len(rows) {
		return nil
	}
	return rows[index]
}

func diffLabel(index int) string {
	if index == 0 {
		return "session header"
	}
	return fmt.Sprintf("seq %d", index-1)
}

func formatRow(row any) string {
	if row == nil {
		return "<missing>"
	}
	return compactJSON(row)
}

func runProject(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("project", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	manifestPath := fs.String("manifest", "", "manifest path")
	fromSeq := fs.Int("from-seq", 0, "from seq")
	toSeq := fs.Int("to-seq", -1, "to seq")
	provider := fs.String("provider", "", "provider override")
	model := fs.String("model", "", "model override")
	if err := fs.Parse(permuteFlags(args, map[string]bool{
		"from-seq": true,
		"manifest": true,
		"model":    true,
		"provider": true,
		"to-seq":   true,
	})); err != nil {
		return err
	}
	if fs.NArg() != 1 || *manifestPath == "" {
		return fmt.Errorf("usage: harnas project <session.jsonl> --manifest PATH [--from-seq N] [--to-seq M] [--provider KIND] [--model MODEL]")
	}
	session, err := harnas.LoadSession(fs.Arg(0))
	if err != nil {
		return err
	}
	manifest, err := harnas.ReadManifest(*manifestPath)
	if err != nil {
		return err
	}
	if *provider != "" {
		manifest.Provider.Kind = *provider
		manifest.Provider.Model = resolveModel(*provider, *model)
	} else if *model != "" {
		manifest.Provider.Model = *model
	}
	registry, err := projectRegistry(manifest.Tools)
	if err != nil {
		return err
	}
	projection := harnas.ProjectionForWithRegistry(manifest.Provider, manifest.System, registry)
	sliced, err := sliceLog(session.Log, *fromSeq, *toSeq)
	if err != nil {
		return err
	}
	request, err := projection.Project(sliced)
	if err != nil {
		return err
	}
	return writePrettyJSON(stdout, request)
}

func projectRegistry(tools []harnas.ToolSpec) (*harnas.Registry, error) {
	handlers := map[string]harnas.ToolHandler{}
	for _, tool := range tools {
		handlers[tool.Handler] = func(map[string]any) (string, error) { return "", nil }
	}
	return harnas.BuildRegistry(tools, handlers)
}

func sliceLog(log *harnas.Log, fromSeq, toSeq int) (*harnas.Log, error) {
	events := log.Events()
	if toSeq < 0 {
		toSeq = len(events) - 1
	}
	if fromSeq < 0 {
		return nil, fmt.Errorf("--from-seq must be non-negative")
	}
	if toSeq < fromSeq {
		return nil, fmt.Errorf("--to-seq must be >= --from-seq")
	}
	sliced := harnas.NewLog()
	for _, event := range events {
		if event.Seq >= fromSeq && event.Seq <= toSeq {
			sliced.Restore(event)
		}
	}
	return sliced, nil
}

func writePrettyJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func compactJSON(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

func truncate(value string) string {
	text := strings.Join(strings.Fields(value), " ")
	if len(text) <= 96 {
		return text
	}
	return text[:95] + "..."
}

func permuteFlags(args []string, takesValue map[string]bool) []string {
	flags := []string{}
	positionals := []string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			positionals = append(positionals, arg)
			continue
		}
		flags = append(flags, arg)
		name := strings.TrimPrefix(arg, "--")
		if before, _, found := strings.Cut(name, "="); found {
			name = before
		}
		if takesValue[name] && !strings.Contains(arg, "=") && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, positionals...)
}
