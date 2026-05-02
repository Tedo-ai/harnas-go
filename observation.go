package harnas

import (
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
