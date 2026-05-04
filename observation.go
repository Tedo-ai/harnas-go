package harnas

import (
	"encoding/json"
	"os"
	"reflect"
	"sync"
)

type ObservationSubscriber func(event string, payload map[string]any)

type Observation struct {
	mu          sync.Mutex
	subscribers []ObservationSubscriber
}

func NewObservation() *Observation {
	return &Observation{subscribers: []ObservationSubscriber{}}
}

func (o *Observation) Subscribe(subscriber ObservationSubscriber) ObservationSubscriber {
	if subscriber == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.subscribers = append(o.subscribers, subscriber)
	return subscriber
}

func (o *Observation) Unsubscribe(subscriber ObservationSubscriber) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for index, existing := range o.subscribers {
		if funcEqual(existing, subscriber) {
			o.subscribers = append(o.subscribers[:index], o.subscribers[index+1:]...)
			return
		}
	}
}

func (o *Observation) Reset() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.subscribers = []ObservationSubscriber{}
}

func (o *Observation) Emit(event string, payload map[string]any) {
	if o == nil {
		return
	}
	o.mu.Lock()
	subscribers := append([]ObservationSubscriber(nil), o.subscribers...)
	o.mu.Unlock()
	for _, subscriber := range subscribers {
		func() {
			defer func() { _ = recover() }()
			subscriber(event, payload)
		}()
	}
}

type ObservationCollector struct {
	Events []ObservedEvent
}

type ObservedEvent struct {
	Event   string
	Payload map[string]any
}

func NewObservationCollector() *ObservationCollector {
	return &ObservationCollector{Events: []ObservedEvent{}}
}

func (c *ObservationCollector) Call(event string, payload map[string]any) {
	c.Events = append(c.Events, ObservedEvent{Event: event, Payload: payload})
}

func (c *ObservationCollector) Of(event string) []ObservedEvent {
	matches := []ObservedEvent{}
	for _, observed := range c.Events {
		if observed.Event == event {
			matches = append(matches, observed)
		}
	}
	return matches
}

func (c *ObservationCollector) Count(event string) int {
	return len(c.Of(event))
}

func (c *ObservationCollector) Reset() {
	c.Events = []ObservedEvent{}
}

func funcEqual(left, right ObservationSubscriber) bool {
	return funcValuePointer(left) == funcValuePointer(right)
}

func funcValuePointer(fn any) uintptr {
	return reflect.ValueOf(fn).Pointer()
}

type DeltaLogger struct {
	path  string
	mu    sync.Mutex
	index int
}

func NewDeltaLogger(path string, observation *Observation) *DeltaLogger {
	logger := &DeltaLogger{path: path}
	observation.Subscribe(logger.Call)
	return logger
}

func (l *DeltaLogger) Call(eventName string, payload map[string]any) {
	if eventName != "stream_event" {
		return
	}
	event, ok := payload["event"].(Event)
	if !ok || !isStreamObservationEvent(event.Type) {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	_ = json.NewEncoder(file).Encode(map[string]any{
		"index":   l.index,
		"type":    string(event.Type),
		"payload": event.Payload,
	})
	l.index++
}

type CostTracker struct {
	mu             sync.Mutex
	InputTokens    int
	OutputTokens   int
	Turns          int
	threshold      int
	onThreshold    func(map[string]int)
	thresholdFired bool
}

func NewCostTracker(observation *Observation, threshold int, onThreshold func(map[string]int)) *CostTracker {
	tracker := &CostTracker{threshold: threshold, onThreshold: onThreshold}
	observation.Subscribe(tracker.Call)
	return tracker
}

func (c *CostTracker) TotalTokens() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.InputTokens + c.OutputTokens
}

func (c *CostTracker) Usage() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.usageLocked()
}

func (c *CostTracker) Call(eventName string, payload map[string]any) {
	if eventName != "event_appended" {
		return
	}
	event, ok := payload["event"].(Event)
	if !ok || event.Type != EventAssistantMessage {
		return
	}
	usage, ok := event.Payload["usage"].(map[string]any)
	if !ok {
		return
	}
	c.mu.Lock()
	c.InputTokens += int(asFloat(usage["input_tokens"]))
	c.OutputTokens += int(asFloat(usage["output_tokens"]))
	c.Turns++
	shouldFire := c.threshold > 0 &&
		!c.thresholdFired &&
		c.InputTokens+c.OutputTokens >= c.threshold &&
		c.onThreshold != nil
	if shouldFire {
		c.thresholdFired = true
	}
	snapshot := c.usageLocked()
	callback := c.onThreshold
	c.mu.Unlock()
	if shouldFire {
		callback(snapshot)
	}
}

func (c *CostTracker) usageLocked() map[string]int {
	return map[string]int{
		"input_tokens":  c.InputTokens,
		"output_tokens": c.OutputTokens,
		"total_tokens":  c.InputTokens + c.OutputTokens,
		"turns":         c.Turns,
	}
}
