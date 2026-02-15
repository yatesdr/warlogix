package warcry

import (
	"sync"
	"time"
)

// ringEntry holds a single buffered event.
type ringEntry struct {
	data      []byte
	timestamp time.Time
}

// RingBuffer is a fixed-size circular buffer of serialized events.
type RingBuffer struct {
	mu      sync.Mutex
	entries []ringEntry
	head    int
	count   int
	size    int
}

// NewRingBuffer creates a ring buffer with the given capacity.
func NewRingBuffer(size int) *RingBuffer {
	if size <= 0 {
		size = 10000
	}
	return &RingBuffer{
		entries: make([]ringEntry, size),
		size:    size,
	}
}

// Add appends a serialized event to the buffer, overwriting the oldest if full.
func (r *RingBuffer) Add(data []byte, ts time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	idx := (r.head + r.count) % r.size
	if r.count == r.size {
		// Buffer full — overwrite oldest, advance head.
		idx = r.head
		r.head = (r.head + 1) % r.size
	} else {
		r.count++
	}

	// Copy data so the caller can reuse the slice.
	cp := make([]byte, len(data))
	copy(cp, data)
	r.entries[idx] = ringEntry{data: cp, timestamp: ts}
}

// Since returns all entries with timestamps strictly after ts, in order.
func (r *RingBuffer) Since(ts time.Time) [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()

	var result [][]byte
	for i := 0; i < r.count; i++ {
		idx := (r.head + i) % r.size
		e := r.entries[idx]
		if e.timestamp.After(ts) {
			result = append(result, e.data)
		}
	}
	return result
}
