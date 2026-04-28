package harnas

import "testing"

func TestLogAppendAssignsSeq(t *testing.T) {
	log := NewLog()
	event := log.Append(EventUserMessage, map[string]any{"text": "hi"})
	if event.Seq != 0 {
		t.Fatalf("expected seq 0, got %d", event.Seq)
	}
	if event.Type != EventUserMessage {
		t.Fatalf("expected user_message, got %s", event.Type)
	}
}
