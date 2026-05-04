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
		return observeStrategy(session, "Compaction::MarkerTail", "pre_projection", func() any {
			m.OnPreProjection(session)
			return nil
		})
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
		return observeStrategy(session, "Compaction::ToolOutputCap", "pre_projection", func() any {
			t.OnPreProjection(session)
			return nil
		})
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

type TokenMarkerTail struct {
	MaxTokens     int
	Threshold     float64
	KeepRecent    int
	SummaryFormat string
}

func (t TokenMarkerTail) Install(session *Session) {
	session.Hooks.On("pre_projection", func(ctx map[string]any) any {
		return observeStrategy(session, "Compaction::TokenMarkerTail", "pre_projection", func() any {
			t.OnPreProjection(session)
			return nil
		})
	})
}

func (t TokenMarkerTail) OnPreProjection(session *Session) {
	maxTokens := t.MaxTokens
	if maxTokens == 0 {
		maxTokens = 100000
	}
	threshold := t.Threshold
	if threshold == 0 {
		threshold = 0.85
	}
	format := t.SummaryFormat
	if format == "" {
		format = "[compacted $N earlier messages (~$E tokens -> threshold $T)]"
	}
	messages := messageEvents(session.Log)
	estimated := estimateTokens(messages)
	triggerTokens := int(float64(maxTokens) * threshold)
	if estimated <= triggerTokens {
		return
	}
	count := len(messages) - t.KeepRecent
	if count <= 0 {
		return
	}
	candidates := make([]int, 0, count)
	for _, event := range messages[:count] {
		candidates = append(candidates, event.Seq)
	}
	safeSeqs := toolPairSafeRange(session.Log, candidates)
	if len(safeSeqs) == 0 {
		return
	}
	replaces := make([]any, 0, len(safeSeqs))
	for _, seq := range safeSeqs {
		replaces = append(replaces, float64(seq))
	}
	summary := strings.ReplaceAll(format, "$N", fmt.Sprintf("%d", len(safeSeqs)))
	summary = strings.ReplaceAll(summary, "$E", fmt.Sprintf("%d", estimated))
	summary = strings.ReplaceAll(summary, "$T", fmt.Sprintf("%d", triggerTokens))
	session.Log.Append(EventCompact, map[string]any{"replaces": replaces, "summary": summary})
}

type SummaryTail struct {
	Projection  Projection
	Provider    Provider
	Ingestor    Ingestor
	MaxMessages int
	KeepRecent  int
	Prompt      string
}

func (s SummaryTail) Install(session *Session) {
	session.Hooks.On("pre_projection", func(ctx map[string]any) any {
		return observeStrategy(session, "Compaction::SummaryTail", "pre_projection", func() any {
			s.OnPreProjection(session)
			return nil
		})
	})
}

func (s SummaryTail) OnPreProjection(session *Session) {
	if s.Projection == nil || s.Provider == nil || s.Ingestor == nil {
		return
	}
	maxMessages := s.MaxMessages
	if maxMessages == 0 {
		maxMessages = 20
	}
	messages := messageEvents(session.Log)
	if len(messages) <= maxMessages {
		return
	}
	count := len(messages) - s.KeepRecent
	if count <= 0 {
		return
	}
	candidates := make([]int, 0, count)
	for _, event := range messages[:count] {
		candidates = append(candidates, event.Seq)
	}
	safeSeqs := toolPairSafeRange(session.Log, candidates)
	if len(safeSeqs) == 0 {
		return
	}
	summary := s.summarize(messages, safeSeqs)
	if summary == "" {
		return
	}
	replaces := make([]any, 0, len(safeSeqs))
	for _, seq := range safeSeqs {
		replaces = append(replaces, float64(seq))
	}
	session.Log.Append(EventCompact, map[string]any{"replaces": replaces, "summary": summary})
}

func (s SummaryTail) summarize(messages []Event, seqs []int) string {
	selected := map[int]bool{}
	for _, seq := range seqs {
		selected[seq] = true
	}
	subLog := NewLog()
	for _, event := range messages {
		if selected[event.Seq] {
			subLog.Append(event.Type, event.Payload)
		}
	}
	prompt := s.Prompt
	if prompt == "" {
		prompt = "Summarize the preceding conversation tersely, preserving facts the agent will need to continue the work. Return only the summary text, no preamble."
	}
	subLog.Append(EventUserMessage, map[string]any{"text": prompt})
	request, err := s.Projection.Project(subLog)
	if err != nil {
		return ""
	}
	response, err := s.Provider.Call(request)
	if err != nil {
		return ""
	}
	events, err := s.Ingestor.Ingest(response)
	if err != nil {
		return ""
	}
	for _, event := range events {
		subLog.Append(event.Type, event.Payload)
	}
	last, ok := subLog.LastAssistantMessage()
	if !ok {
		return ""
	}
	return stringValue(last.Payload["text"])
}

func estimateTokens(events []Event) int {
	chars := 0
	for _, event := range events {
		for _, value := range event.Payload {
			if text, ok := value.(string); ok {
				chars += len([]rune(text))
			}
		}
	}
	return (chars + 3) / 4
}

func toolPairSafeRange(log *Log, candidates []int) []int {
	candidateSet := map[int]bool{}
	for _, seq := range candidates {
		candidateSet[seq] = true
	}
	uses := map[string]int{}
	results := map[string]int{}
	for _, event := range log.Events() {
		switch event.Type {
		case EventToolUse:
			uses[stringValue(event.Payload["id"])] = event.Seq
		case EventToolResult:
			results[stringValue(event.Payload["tool_use_id"])] = event.Seq
		}
	}
	for id, useSeq := range uses {
		resultSeq, hasResult := results[id]
		if !hasResult {
			delete(candidateSet, useSeq)
			continue
		}
		if candidateSet[useSeq] != candidateSet[resultSeq] {
			delete(candidateSet, useSeq)
			delete(candidateSet, resultSeq)
		}
	}
	out := []int{}
	for _, seq := range candidates {
		if candidateSet[seq] {
			out = append(out, seq)
		}
	}
	return out
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
		return observeStrategy(session, "Permission::HumanApproval", "pre_tool_use", func() any {
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
	})
}

func observeStrategy(session *Session, name, hookPoint string, body func() any) (result any) {
	session.Observation.Emit("strategy_started", map[string]any{
		"name":       name,
		"hook_point": hookPoint,
	})
	before := len(session.Log.Events())
	defer func() {
		if recovered := recover(); recovered != nil {
			session.Observation.Emit("strategy_completed", map[string]any{
				"name":       name,
				"hook_point": hookPoint,
				"effect":     "error",
			})
			panic(recovered)
		}
		effect := "noop"
		if len(session.Log.Events()) > before {
			effect = "mutated"
		} else if decision, ok := result.(map[string]any); ok {
			if allow, _ := decision["allow"].(bool); !allow {
				effect = "refused"
			}
		}
		session.Observation.Emit("strategy_completed", map[string]any{
			"name":       name,
			"hook_point": hookPoint,
			"effect":     effect,
		})
	}()
	return body()
}
