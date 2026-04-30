package harnas

import "testing"

func TestTokenMarkerTailCompactsWhenTokenEstimateExceedsThreshold(t *testing.T) {
	session := CreateSession(nil)
	session.Log.Append(EventUserMessage, map[string]any{"text": "one long message"})
	session.Log.Append(EventAssistantMessage, map[string]any{"text": "two long message", "stop_reason": "end_turn", "usage": map[string]any{}})
	session.Log.Append(EventUserMessage, map[string]any{"text": "three long message"})

	TokenMarkerTail{MaxTokens: 1, Threshold: 1, KeepRecent: 1}.OnPreProjection(session)

	events := session.Log.Events()
	last := events[len(events)-1]
	if last.Type != EventCompact {
		t.Fatalf("expected compact, got %#v", last)
	}
}

func TestTokenMarkerTailKeepsToolPairsTogether(t *testing.T) {
	session := CreateSession(nil)
	session.Log.Append(EventUserMessage, map[string]any{"text": "hello"})
	session.Log.Append(EventToolUse, map[string]any{"id": "toolu_1", "name": "read", "arguments": map[string]any{}})
	session.Log.Append(EventUserMessage, map[string]any{"text": "tail"})

	TokenMarkerTail{MaxTokens: 1, Threshold: 1, KeepRecent: 1}.OnPreProjection(session)

	events := session.Log.Events()
	last := events[len(events)-1]
	replaces := asSlice(last.Payload["replaces"])
	for _, seq := range replaces {
		if int(asFloat(seq)) == 1 {
			t.Fatalf("in-flight tool_use should not be compacted without result")
		}
	}
}

func TestSummaryTailUsesProviderSummary(t *testing.T) {
	session := CreateSession(nil)
	session.Log.Append(EventUserMessage, map[string]any{"text": "one"})
	session.Log.Append(EventAssistantMessage, map[string]any{"text": "two", "stop_reason": "end_turn", "usage": map[string]any{}})
	session.Log.Append(EventUserMessage, map[string]any{"text": "three"})
	strategy := SummaryTail{
		Projection:  AnthropicProjection{Model: "mock", MaxTokens: 128},
		Provider:    MockProvider{Text: "short summary"},
		Ingestor:    AnthropicIngestor{},
		MaxMessages: 2,
		KeepRecent:  1,
	}

	strategy.OnPreProjection(session)

	events := session.Log.Events()
	last := events[len(events)-1]
	if last.Type != EventCompact || last.Payload["summary"] != "short summary" {
		t.Fatalf("unexpected compact: %#v", last)
	}
}
