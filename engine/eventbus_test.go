package engine

import (
	"sync"
	"testing"
)

func TestSubscribeAndEmit(t *testing.T) {
	bus := NewEventBus()
	var received []Event

	bus.Subscribe(func(e Event) {
		received = append(received, e)
	})

	bus.Emit(Event{Type: EventPLCCreated, Payload: PLCEvent{Name: "plc1"}})
	bus.Emit(Event{Type: EventMQTTStarted, Payload: ServiceEvent{Name: "mqtt1"}})

	if len(received) != 2 {
		t.Fatalf("expected 2 events, got %d", len(received))
	}
	if received[0].Type != EventPLCCreated {
		t.Errorf("expected EventPLCCreated, got %d", received[0].Type)
	}
	if received[1].Type != EventMQTTStarted {
		t.Errorf("expected EventMQTTStarted, got %d", received[1].Type)
	}
}

func TestSubscribeTypes(t *testing.T) {
	bus := NewEventBus()
	var received []Event

	bus.SubscribeTypes(func(e Event) {
		received = append(received, e)
	}, EventPLCCreated, EventPLCDeleted)

	bus.Emit(Event{Type: EventPLCCreated, Payload: PLCEvent{Name: "plc1"}})
	bus.Emit(Event{Type: EventMQTTStarted, Payload: ServiceEvent{Name: "mqtt1"}}) // should be filtered
	bus.Emit(Event{Type: EventPLCDeleted, Payload: PLCEvent{Name: "plc2"}})

	if len(received) != 2 {
		t.Fatalf("expected 2 filtered events, got %d", len(received))
	}
	if received[0].Payload.(PLCEvent).Name != "plc1" {
		t.Errorf("expected plc1, got %s", received[0].Payload.(PLCEvent).Name)
	}
	if received[1].Payload.(PLCEvent).Name != "plc2" {
		t.Errorf("expected plc2, got %s", received[1].Payload.(PLCEvent).Name)
	}
}

func TestUnsubscribe(t *testing.T) {
	bus := NewEventBus()
	count := 0

	id := bus.Subscribe(func(e Event) {
		count++
	})

	bus.Emit(Event{Type: EventPLCCreated})
	if count != 1 {
		t.Fatalf("expected 1, got %d", count)
	}

	bus.Unsubscribe(id)
	bus.Emit(Event{Type: EventPLCCreated})
	if count != 1 {
		t.Fatalf("expected 1 after unsubscribe, got %d", count)
	}
}

func TestUnsubscribeNonExistent(t *testing.T) {
	bus := NewEventBus()
	// Should not panic
	bus.Unsubscribe(999)
}

func TestMultipleSubscribers(t *testing.T) {
	bus := NewEventBus()
	var mu sync.Mutex
	counts := make(map[string]int)

	bus.Subscribe(func(e Event) {
		mu.Lock()
		counts["a"]++
		mu.Unlock()
	})
	bus.Subscribe(func(e Event) {
		mu.Lock()
		counts["b"]++
		mu.Unlock()
	})

	bus.Emit(Event{Type: EventPLCCreated})

	mu.Lock()
	defer mu.Unlock()
	if counts["a"] != 1 || counts["b"] != 1 {
		t.Errorf("expected both subscribers called once, got a=%d b=%d", counts["a"], counts["b"])
	}
}

func TestEmitSetsTimestamp(t *testing.T) {
	bus := NewEventBus()
	var received Event

	bus.Subscribe(func(e Event) {
		received = e
	})

	bus.Emit(Event{Type: EventPLCCreated})

	if received.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestConcurrentEmit(t *testing.T) {
	bus := NewEventBus()
	var mu sync.Mutex
	count := 0

	bus.Subscribe(func(e Event) {
		mu.Lock()
		count++
		mu.Unlock()
	})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bus.Emit(Event{Type: EventPLCCreated})
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if count != 100 {
		t.Errorf("expected 100, got %d", count)
	}
}
