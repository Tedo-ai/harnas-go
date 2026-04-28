package harnas

import (
	"fmt"
	"strings"
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
