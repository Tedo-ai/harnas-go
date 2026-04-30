package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
	case "diff":
		status, err = runDiff(args[1:], stdout)
	case "fork":
		err = runFork(args[1:], stdout)
	case "inspect":
		err = runInspect(args[1:], stdout)
	case "project":
		err = runProject(args[1:], stdout)
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
  harnas diff <a.jsonl> <b.jsonl>
  harnas fork <session.jsonl> --at-seq N --out <new.jsonl>
  harnas inspect <session.jsonl> [--json]
  harnas project <session.jsonl> --manifest PATH [--from-seq N] [--to-seq M]
`)
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
		return fmt.Errorf("usage: harnas project <session.jsonl> --manifest PATH [--from-seq N] [--to-seq M]")
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
	}
	if *model != "" {
		manifest.Provider.Model = *model
	}
	projection := harnas.ProjectionFor(manifest.Provider, manifest.System)
	request, err := projection.Project(sliceLog(session.Log, *fromSeq, *toSeq))
	if err != nil {
		return err
	}
	return writePrettyJSON(stdout, request)
}

func sliceLog(log *harnas.Log, fromSeq, toSeq int) *harnas.Log {
	events := log.Events()
	if toSeq < 0 {
		toSeq = len(events) - 1
	}
	sliced := harnas.NewLog()
	for _, event := range events {
		if event.Seq >= fromSeq && event.Seq <= toSeq {
			sliced.Restore(event)
		}
	}
	return sliced
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
