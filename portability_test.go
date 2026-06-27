package harnas

import (
	"math"
	"strings"
	"testing"
)

func TestVerifySessionPortable_GoodSession(t *testing.T) {
	s := CreateSession(map[string]any{"workspace_id": "ws_1"})
	s.Log.Append(EventUserMessage, map[string]any{"text": "hello"})
	s.Log.Append(EventAssistantMessage, map[string]any{"text": "hi", "stop_reason": "end_turn"})

	if err := VerifySessionPortable(s); err != nil {
		t.Fatalf("portable session reported error: %v", err)
	}
}

func TestVerifySessionPortable_NonDenseSeqFails(t *testing.T) {
	s := CreateSession(nil)
	// Inject a seq gap, the symptom of an append that bypassed the Log.
	s.Log.Restore(Event{ID: "evt_gap", Seq: 5, Type: EventUserMessage, Payload: map[string]any{"text": "x"}})

	err := VerifySessionPortable(s)
	if err == nil {
		t.Fatal("expected non-dense seq to fail portability")
	}
	if !strings.Contains(err.Error(), "non-dense seq") {
		t.Fatalf("expected non-dense seq error, got: %v", err)
	}
}

func TestVerifySessionPortable_NonCanonicalPayloadFails(t *testing.T) {
	s := CreateSession(nil)
	// A NaN cannot be encoded as canonical JSON, so it cannot cross to another
	// implementation — exactly what a hand-rolled store might let slip through.
	s.Log.Append(EventUserMessage, map[string]any{"bad": math.NaN()})

	err := VerifySessionPortable(s)
	if err == nil {
		t.Fatal("expected non-canonical payload to fail portability")
	}
	if !strings.Contains(err.Error(), "not portable") {
		t.Fatalf("expected portability error, got: %v", err)
	}
}
