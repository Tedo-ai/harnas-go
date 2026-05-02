package harnas

import "testing"

func TestSessionObservationReceivesLogAndLoopEvents(t *testing.T) {
	session := CreateSession(nil)
	collector := NewObservationCollector()
	session.Observation.Subscribe(collector.Call)
	session.Log.Append(EventUserMessage, map[string]any{"text": "hello"})

	loop := AgentLoop{
		Session:    session,
		Projection: AnthropicProjection{Model: "claude-test", MaxTokens: 128},
		Provider:   MockProvider{Text: "hi"},
		Ingestor:   AnthropicIngestor{},
		MaxTurns:   1,
	}
	if _, err := loop.Run(); err != nil {
		t.Fatal(err)
	}

	if collector.Count("event_appended") < 2 {
		t.Fatalf("expected event_appended observations, got %#v", collector.Events)
	}
	if collector.Count("projection_invoked") != 1 {
		t.Fatalf("expected projection_invoked, got %#v", collector.Events)
	}
	if collector.Count("provider_called") != 1 || collector.Count("provider_responded") != 1 {
		t.Fatalf("expected provider observations, got %#v", collector.Events)
	}
}

func TestHookPanicsAreIsolatedAndObserved(t *testing.T) {
	session := CreateSession(nil)
	collector := NewObservationCollector()
	session.Observation.Subscribe(collector.Call)
	session.Hooks.On("pre_projection", func(map[string]any) any {
		panic("boom")
	})
	session.Hooks.On("pre_projection", func(map[string]any) any {
		return "ok"
	})

	values := session.Hooks.Invoke("pre_projection", map[string]any{"session": session})
	if len(values) != 1 || values[0] != "ok" {
		t.Fatalf("expected surviving hook return, got %#v", values)
	}
	if collector.Count("hook_handler_failed") != 1 {
		t.Fatalf("expected hook failure observation, got %#v", collector.Events)
	}
}

func TestObservationUnsubscribeAndReset(t *testing.T) {
	observation := NewObservation()
	collector := NewObservationCollector()
	sub := observation.Subscribe(collector.Call)
	observation.Emit("test", map[string]any{"x": 1})
	observation.Unsubscribe(sub)
	observation.Emit("test", map[string]any{"x": 2})
	if collector.Count("test") != 1 {
		t.Fatalf("expected unsubscribe to stop events, got %#v", collector.Events)
	}
	observation.Subscribe(collector.Call)
	observation.Reset()
	observation.Emit("test", map[string]any{"x": 3})
	if collector.Count("test") != 1 {
		t.Fatalf("expected reset to clear subscribers, got %#v", collector.Events)
	}
}
