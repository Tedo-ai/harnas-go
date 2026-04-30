package harnas

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type MarkerTail struct {
	MaxMessages int
	KeepRecent  int
}

func (m MarkerTail) Install(session *Session) {
	session.Hooks.On("pre_projection", func(ctx map[string]any) any {
		m.OnPreProjection(session)
		return nil
	})
}

func (m MarkerTail) OnPreProjection(session *Session) {
	messages := messageEvents(session.Log)
	if len(messages) <= m.MaxMessages {
		return
	}
	cutoff := len(messages) - m.KeepRecent
	replaces := make([]any, 0, cutoff)
	for _, event := range messages[:cutoff] {
		replaces = append(replaces, float64(event.Seq))
	}
	session.Log.Append(EventCompact, map[string]any{
		"replaces": replaces,
		"summary":  fmt.Sprintf("[snipped %d earlier messages]", len(replaces)),
	})
}

func messageEvents(log *Log) []Event {
	events := []Event{}
	replaced := map[int]bool{}
	for _, event := range log.Events() {
		if event.Type != EventCompact {
			continue
		}
		for _, seq := range asSlice(event.Payload["replaces"]) {
			replaced[int(asFloat(seq))] = true
		}
	}
	for _, event := range log.Events() {
		if replaced[event.Seq] {
			continue
		}
		switch event.Type {
		case EventUserMessage, EventAssistantMessage, EventToolUse, EventToolResult:
			events = append(events, event)
		}
	}
	return events
}

type ToolOutputCap struct {
	MaxBytes      int
	PrefixBytes   int
	SummaryFormat string
}

func (t ToolOutputCap) Install(session *Session) {
	session.Hooks.On("pre_projection", func(ctx map[string]any) any {
		t.OnPreProjection(session)
		return nil
	})
}

func (t ToolOutputCap) OnPreProjection(session *Session) {
	maxBytes := t.MaxBytes
	if maxBytes == 0 {
		maxBytes = 4096
	}
	prefixBytes := t.PrefixBytes
	format := t.SummaryFormat
	if format == "" {
		format = "[tool `$TOOL` output capped at $CAP bytes (original $ORIGINAL bytes)]\n$PREFIX"
	}
	toolUses := indexToolUses(session.Log)
	for _, event := range effectiveEvents(session.Log) {
		if event.Type != EventToolResult {
			continue
		}
		output, _ := event.Payload["output"].(string)
		if len([]byte(output)) <= maxBytes {
			continue
		}
		toolUseID, _ := event.Payload["tool_use_id"].(string)
		use, ok := toolUses[toolUseID]
		if !ok {
			continue
		}
		summary := buildToolOutputSummary(format, maxBytes, prefixBytes, use, output)
		session.Log.Append(EventCompact, map[string]any{
			"replaces": []any{float64(use.Seq), float64(event.Seq)},
			"summary":  summary,
		})
	}
}

func effectiveEvents(log *Log) []Event {
	replaced := map[int]bool{}
	for _, event := range log.Events() {
		if event.Type != EventCompact {
			continue
		}
		for _, seq := range asSlice(event.Payload["replaces"]) {
			replaced[int(asFloat(seq))] = true
		}
	}
	events := []Event{}
	for _, event := range log.Events() {
		if event.Type == EventCompact || replaced[event.Seq] {
			continue
		}
		events = append(events, event)
	}
	return events
}

func indexToolUses(log *Log) map[string]Event {
	out := map[string]Event{}
	for _, event := range log.Events() {
		if event.Type == EventToolUse {
			id, _ := event.Payload["id"].(string)
			out[id] = event
		}
	}
	return out
}

func buildToolOutputSummary(format string, maxBytes, prefixBytes int, use Event, output string) string {
	toolName, _ := use.Payload["name"].(string)
	summary := strings.ReplaceAll(format, "$TOOL", toolName)
	summary = strings.ReplaceAll(summary, "$CAP", fmt.Sprintf("%d", maxBytes))
	summary = strings.ReplaceAll(summary, "$ORIGINAL", fmt.Sprintf("%d", len([]byte(output))))
	summary = strings.ReplaceAll(summary, "$PREFIX", utf8Prefix(output, prefixBytes))
	return summary
}

func utf8Prefix(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len([]byte(value)) <= maxBytes {
		return value
	}
	cut := maxBytes
	for cut > 0 && !utf8.ValidString(value[:cut]) {
		cut--
	}
	return value[:cut]
}

type DenyByName struct {
	Names        []string
	ReasonFormat string
}

func (d DenyByName) Install(session *Session) {
	session.Hooks.On("pre_tool_use", func(ctx map[string]any) any {
		toolUse, _ := ctx["tool_use"].(Event)
		name, _ := toolUse.Payload["name"].(string)
		if !containsString(d.Names, name) {
			return map[string]any{"allow": true}
		}
		reasonFormat := d.ReasonFormat
		if reasonFormat == "" {
			reasonFormat = "tool $NAME is on the deny-list"
		}
		return map[string]any{
			"allow":  false,
			"reason": replaceName(reasonFormat, name),
		}
	})
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func replaceName(format, name string) string {
	return strings.ReplaceAll(format, "$NAME", fmt.Sprintf("%q", name))
}

type AlwaysAllow struct{}

func (a AlwaysAllow) Install(session *Session) {
	session.Hooks.On("pre_tool_use", func(ctx map[string]any) any {
		return map[string]any{"allow": true}
	})
}

type HumanApproval struct {
	Prompt       func(Event) bool
	DenialReason string
}

func (h HumanApproval) Install(session *Session) {
	session.Hooks.On("pre_tool_use", func(ctx map[string]any) any {
		toolUse, _ := ctx["tool_use"].(Event)
		if h.Prompt != nil && h.Prompt(toolUse) {
			return map[string]any{"allow": true}
		}
		reason := h.DenialReason
		if reason == "" {
			reason = "human declined"
		}
		return map[string]any{"allow": false, "reason": reason}
	})
}
