package engine

import (
	"sync"
	"time"
)

// SubscriberID uniquely identifies an EventBus subscriber.
type SubscriberID uint64

// SubscriberFunc is a callback invoked when an event is emitted.
type SubscriberFunc func(Event)

type subscriber struct {
	id     SubscriberID
	fn     SubscriberFunc
	filter map[EventType]struct{} // nil means all events
}

// EventBus provides synchronous, typed event dispatch.
// Subscribers are called in registration order on the emitting goroutine.
type EventBus struct {
	mu          sync.RWMutex
	subscribers []subscriber
	nextID      SubscriberID
}

// NewEventBus creates a new EventBus.
func NewEventBus() *EventBus {
	return &EventBus{}
}

// Subscribe registers a callback for all event types.
// Returns a SubscriberID that can be used with Unsubscribe.
func (eb *EventBus) Subscribe(fn SubscriberFunc) SubscriberID {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.nextID++
	id := eb.nextID
	eb.subscribers = append(eb.subscribers, subscriber{id: id, fn: fn})
	return id
}

// SubscribeTypes registers a callback that is only invoked for the given event types.
func (eb *EventBus) SubscribeTypes(fn SubscriberFunc, types ...EventType) SubscriberID {
	filter := make(map[EventType]struct{}, len(types))
	for _, t := range types {
		filter[t] = struct{}{}
	}
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.nextID++
	id := eb.nextID
	eb.subscribers = append(eb.subscribers, subscriber{id: id, fn: fn, filter: filter})
	return id
}

// Unsubscribe removes a subscriber by ID.
func (eb *EventBus) Unsubscribe(id SubscriberID) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	for i, s := range eb.subscribers {
		if s.id == id {
			eb.subscribers = append(eb.subscribers[:i], eb.subscribers[i+1:]...)
			return
		}
	}
}

// Emit dispatches an event synchronously to all matching subscribers.
// Subscribers with a type filter only receive events matching their filter.
func (eb *EventBus) Emit(evt Event) {
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now()
	}
	eb.mu.RLock()
	// Copy slice header under read lock so we can release it before calling subscribers.
	subs := make([]subscriber, len(eb.subscribers))
	copy(subs, eb.subscribers)
	eb.mu.RUnlock()

	for _, s := range subs {
		if s.filter != nil {
			if _, ok := s.filter[evt.Type]; !ok {
				continue
			}
		}
		s.fn(evt)
	}
}
