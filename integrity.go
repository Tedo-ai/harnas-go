package harnas

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

type IntegrityDomain string

const (
	IntegrityDomainDurableLog          IntegrityDomain = "durable_log"
	IntegrityDomainEffectiveTranscript IntegrityDomain = "effective_transcript"
)

const (
	IntegrityMissingToolUseID        = "missing_tool_use_id"
	IntegrityDuplicateToolUseID      = "duplicate_tool_use_id"
	IntegrityMissingToolResultID     = "missing_tool_result_reference"
	IntegrityOrphanToolResult        = "orphan_tool_result"
	IntegrityToolResultBeforeUse     = "tool_result_before_tool_use"
	IntegrityDuplicateToolResult     = "duplicate_tool_result"
	IntegrityUnresolvedToolUse       = "unresolved_tool_use"
	IntegrityResolvedApprovalOpen    = "resolved_approval_missing_result"
	IntegrityPreparedTranscriptStale = "prepared_transcript_stale"
)

// IntegrityViolation describes a mechanically detected transcript violation.
// Code and Domain are stable machine-readable values; Message is diagnostic.
type IntegrityViolation struct {
	Domain    IntegrityDomain
	Code      string
	EventSeq  int
	ToolUseID string
	Message   string
}

type TranscriptIntegrityError struct {
	Violations []IntegrityViolation
}

func (e *TranscriptIntegrityError) Error() string {
	if e == nil || len(e.Violations) == 0 {
		return "transcript integrity violation"
	}
	parts := make([]string, 0, len(e.Violations))
	for _, violation := range e.Violations {
		parts = append(parts, fmt.Sprintf("%s/%s: %s", violation.Domain, violation.Code, violation.Message))
	}
	return "transcript integrity violation: " + strings.Join(parts, "; ")
}

// AnalyzeDurableLog validates immutable-history referential integrity. An
// unresolved tool use is not itself a durable-log violation: it may represent
// active execution or approval. Provider readiness is stricter and is checked
// by PrepareProviderCall against the effective transcript.
func AnalyzeDurableLog(log *Log) []IntegrityViolation {
	if log == nil {
		return []IntegrityViolation{{
			Domain:   IntegrityDomainDurableLog,
			Code:     IntegrityUnresolvedToolUse,
			EventSeq: -1,
			Message:  "log is nil",
		}}
	}
	return analyzeToolReferences(log.Events(), IntegrityDomainDurableLog)
}

// PreparedTranscript is an opaque, validated provider input bound to one
// Session snapshot. Its fields deliberately remain private so callers cannot
// fabricate a provider-ready transcript from unchecked events.
type PreparedTranscript struct {
	sessionID     string
	nextSeq       int
	effectiveHash string
	log           *Log
}

func (p PreparedTranscript) SessionID() string     { return p.sessionID }
func (p PreparedTranscript) NextSeq() int          { return p.nextSeq }
func (p PreparedTranscript) EffectiveHash() string { return p.effectiveHash }

// Project renders this validated snapshot through a Projection. The zero value
// is rejected, so callers cannot bypass PrepareProviderCall by constructing a
// PreparedTranscript literal.
func (p PreparedTranscript) Project(projection Projection) (map[string]any, error) {
	if p.log == nil {
		return nil, &TranscriptIntegrityError{Violations: []IntegrityViolation{{
			Domain:   IntegrityDomainEffectiveTranscript,
			Code:     IntegrityUnresolvedToolUse,
			EventSeq: -1,
			Message:  "PreparedTranscript was not created by PrepareProviderCall",
		}}}
	}
	return projection.Project(p.log)
}

func (p PreparedTranscript) current(session *Session) bool {
	if session == nil || session.Log == nil || session.ID != p.sessionID {
		return false
	}
	if len(session.Log.Events()) != p.nextSeq {
		return false
	}
	hash, err := effectiveTranscriptHash(ApplyMutations(session.Log))
	return err == nil && hash == p.effectiveHash
}

// PrepareProviderCall validates both truth domains and returns an opaque view
// of the exact effective event stream projections are allowed to render.
func PrepareProviderCall(session *Session) (PreparedTranscript, error) {
	if session == nil || session.Log == nil {
		return PreparedTranscript{}, &TranscriptIntegrityError{Violations: []IntegrityViolation{{
			Domain:   IntegrityDomainDurableLog,
			Code:     IntegrityUnresolvedToolUse,
			EventSeq: -1,
			Message:  "session or log is nil",
		}}}
	}
	violations := AnalyzeDurableLog(session.Log)
	effective := ApplyMutations(session.Log)
	violations = append(violations, analyzeToolReferences(effective, IntegrityDomainEffectiveTranscript)...)
	violations = append(violations, unresolvedToolViolations(effective)...)
	if len(violations) > 0 {
		return PreparedTranscript{}, &TranscriptIntegrityError{Violations: violations}
	}
	hash, err := effectiveTranscriptHash(effective)
	if err != nil {
		return PreparedTranscript{}, err
	}
	preparedLog := NewLog()
	for _, event := range effective {
		event.Payload = clonePayload(event.Payload)
		preparedLog.Restore(event)
	}
	return PreparedTranscript{
		sessionID:     session.ID,
		nextSeq:       len(session.Log.Events()),
		effectiveHash: hash,
		log:           preparedLog,
	}, nil
}

func analyzeToolReferences(events []Event, domain IntegrityDomain) []IntegrityViolation {
	uses := map[string]Event{}
	violations := []IntegrityViolation{}
	for _, event := range events {
		if event.Type != EventToolUse {
			continue
		}
		id := stringValue(event.Payload["id"])
		if id == "" {
			violations = append(violations, IntegrityViolation{
				Domain: domain, Code: IntegrityMissingToolUseID, EventSeq: event.Seq,
				Message: "tool_use has no id",
			})
			continue
		}
		if original, exists := uses[id]; exists {
			violations = append(violations, IntegrityViolation{
				Domain: domain, Code: IntegrityDuplicateToolUseID, EventSeq: event.Seq, ToolUseID: id,
				Message: fmt.Sprintf("tool_use id %q duplicates seq %d", id, original.Seq),
			})
			continue
		}
		uses[id] = event
	}
	results := map[string]Event{}
	for _, event := range events {
		if event.Type != EventToolResult {
			continue
		}
		id := stringValue(event.Payload["tool_use_id"])
		if id == "" {
			violations = append(violations, IntegrityViolation{
				Domain: domain, Code: IntegrityMissingToolResultID, EventSeq: event.Seq,
				Message: "tool_result has no tool_use_id",
			})
			continue
		}
		use, exists := uses[id]
		if !exists {
			violations = append(violations, IntegrityViolation{
				Domain: domain, Code: IntegrityOrphanToolResult, EventSeq: event.Seq, ToolUseID: id,
				Message: fmt.Sprintf("tool_result references unknown tool_use id %q", id),
			})
		} else if event.Seq <= use.Seq {
			violations = append(violations, IntegrityViolation{
				Domain: domain, Code: IntegrityToolResultBeforeUse, EventSeq: event.Seq, ToolUseID: id,
				Message: fmt.Sprintf("tool_result for %q occurs before its tool_use", id),
			})
		}
		if original, duplicate := results[id]; duplicate {
			violations = append(violations, IntegrityViolation{
				Domain: domain, Code: IntegrityDuplicateToolResult, EventSeq: event.Seq, ToolUseID: id,
				Message: fmt.Sprintf("tool_result for %q duplicates seq %d", id, original.Seq),
			})
			continue
		}
		results[id] = event
	}
	return violations
}

func unresolvedToolViolations(events []Event) []IntegrityViolation {
	fulfilled := map[string]bool{}
	for _, event := range events {
		if event.Type == EventToolResult {
			fulfilled[stringValue(event.Payload["tool_use_id"])] = true
		}
	}
	violations := []IntegrityViolation{}
	for _, event := range events {
		if event.Type != EventToolUse {
			continue
		}
		id := stringValue(event.Payload["id"])
		if id != "" && !fulfilled[id] {
			violations = append(violations, IntegrityViolation{
				Domain: IntegrityDomainEffectiveTranscript, Code: IntegrityUnresolvedToolUse,
				EventSeq: event.Seq, ToolUseID: id,
				Message: fmt.Sprintf("tool_use %q has no provider-visible tool_result", id),
			})
		}
	}
	return violations
}

func effectiveTranscriptHash(events []Event) (string, error) {
	rows := make([]any, 0, len(events))
	for _, event := range events {
		rows = append(rows, map[string]any{
			"seq":     event.Seq,
			"id":      event.ID,
			"type":    string(event.Type),
			"payload": event.Payload,
		})
	}
	encoded, err := json.Marshal(rows)
	if err != nil {
		return "", err
	}
	canonical, err := CanonicalizeJCSV1JSON(encoded)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}
