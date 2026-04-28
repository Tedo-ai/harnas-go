package harnas

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"testing"
)

func randomLog(rng *rand.Rand, messageCount int, compact bool) *Log {
	log := NewLog()
	if messageCount == 0 {
		messageCount = rng.Intn(12) + 1
	}
	for i := range messageCount {
		appendRandomMessage(log, rng, i)
	}
	if compact && messageCount >= 4 && rng.Float64() < 0.6 {
		upper := rng.Intn(messageCount-2) + 1
		replaces := []any{}
		for seq := 0; seq <= upper; seq++ {
			replaces = append(replaces, float64(seq))
		}
		log.Append(EventCompact, map[string]any{
			"replaces": replaces,
			"summary":  fmt.Sprintf("summary up to %d", upper),
		})
	}
	return log
}

func appendRandomMessage(log *Log, rng *rand.Rand, index int) {
	text := fmt.Sprintf("message-%d-%d", index, rng.Intn(10000))
	if index%2 == 0 {
		log.Append(EventUserMessage, map[string]any{"text": text})
		return
	}
	log.Append(EventAssistantMessage, map[string]any{
		"text":        text,
		"stop_reason": "end_turn",
		"usage":       map[string]any{"input_tokens": 0, "output_tokens": 0},
	})
}

func eventJSON(events []Event) []string {
	out := make([]string, 0, len(events))
	for _, event := range events {
		encoded, err := json.Marshal(event)
		if err != nil {
			panic(err)
		}
		out = append(out, string(encoded))
	}
	return out
}

func TestPropertyMutationsApplyIsIdempotent(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for range 100 {
		log := randomLog(rng, 0, true)
		once := ApplyMutations(log)
		twiceLog := NewLog()
		for _, event := range once {
			twiceLog.Restore(event)
		}
		twice := ApplyMutations(twiceLog)
		if !reflect.DeepEqual(eventJSON(twice), eventJSON(once)) {
			t.Fatalf("ApplyMutations was not idempotent")
		}
	}
}

func TestPropertyProjectionsArePure(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for range 100 {
		log := randomLog(rng, 0, true)
		projection := AnthropicProjection{Model: "claude-test", MaxTokens: 128}
		first, err := projection.Project(log)
		if err != nil {
			t.Fatal(err)
		}
		second, err := projection.Project(log)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("projection was not pure")
		}
	}
}

func TestPropertyAppendPreservesDenseSeqOrder(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for range 100 {
		log := randomLog(rng, 0, false)
		for index, event := range log.Events() {
			if event.Seq != index {
				t.Fatalf("seq gap at %d: got %d", index, event.Seq)
			}
		}
	}
}

func TestPropertyForkPreservesSelectedPrefix(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	for range 100 {
		session := CreateSession(nil)
		count := rng.Intn(12) + 1
		for i := range count {
			appendRandomMessage(session.Log, rng, i)
		}
		atSeq := rng.Intn(count)
		forked := session.Fork(atSeq)

		if !reflect.DeepEqual(eventJSON(forked.Log.Events()), eventJSON(session.Log.Events()[:atSeq+1])) {
			t.Fatalf("fork prefix mismatch")
		}
		if forked.Metadata["forked_from"] != session.ID {
			t.Fatalf("forked_from mismatch")
		}
		if int(asFloat(forked.Metadata["forked_at_seq"])) != atSeq {
			t.Fatalf("forked_at_seq mismatch")
		}
	}
}

func TestPropertyCompactRevertComposesBackToOriginalEffectiveStream(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	for range 100 {
		count := rng.Intn(7) + 2
		log := randomLog(rng, count, false)
		original := ApplyMutations(log)
		replaces := []any{}
		for seq := range log.Events() {
			replaces = append(replaces, float64(seq))
		}
		compact := log.Append(EventCompact, map[string]any{
			"replaces": replaces,
			"summary":  "temporary summary",
		})
		log.Append(EventRevert, map[string]any{"revokes": float64(compact.Seq)})

		if !reflect.DeepEqual(eventJSON(ApplyMutations(log)), eventJSON(original)) {
			t.Fatalf("compact + revert did not reproduce original effective stream")
		}
	}
}
