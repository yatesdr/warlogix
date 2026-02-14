package tui

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"warlink/logging"
)

// LogMessage represents a single log entry in the debug store.
type LogMessage struct {
	Timestamp time.Time
	Level     string // "ERROR", "SSH", "MQTT", "KAFKA", "VALKEY", "LOGIX", ""
	Message   string
}

// DebugStoreListenerID is a unique identifier for a debug store subscriber.
type DebugStoreListenerID string

// DebugLogStore is a shared store for debug log messages that supports multiple subscribers.
type DebugLogStore struct {
	messages    []LogMessage
	mu          sync.RWMutex
	maxLines    int
	listeners   map[DebugStoreListenerID]func(LogMessage)
	listenersMu sync.RWMutex
	counter     uint64
	fileLogger  *logging.FileLogger
}

var globalDebugStore *DebugLogStore
var storeOnce sync.Once

// InitDebugStore initializes the global debug store with the specified max lines.
// This should be called once at startup.
func InitDebugStore(maxLines int) {
	storeOnce.Do(func() {
		globalDebugStore = &DebugLogStore{
			messages:  make([]LogMessage, 0),
			maxLines:  maxLines,
			listeners: make(map[DebugStoreListenerID]func(LogMessage)),
		}
	})
}

// GetDebugStore returns the global debug store instance.
// Returns nil if InitDebugStore has not been called.
func GetDebugStore() *DebugLogStore {
	return globalDebugStore
}

// Log adds a message to the store and notifies all subscribers.
func (s *DebugLogStore) Log(level, format string, args ...interface{}) {
	msg := LogMessage{
		Timestamp: time.Now(),
		Level:     level,
		Message:   fmt.Sprintf(format, args...),
	}

	// Write to file logger if configured
	if s.fileLogger != nil {
		s.fileLogger.Log("%s", msg.Message)
	}

	// Use TryLock to avoid blocking callers
	if !s.mu.TryLock() {
		return // Drop the message rather than block
	}
	s.messages = append(s.messages, msg)
	if len(s.messages) > s.maxLines {
		s.messages = s.messages[len(s.messages)-s.maxLines:]
	}
	s.mu.Unlock()

	// Notify all listeners in goroutines
	s.listenersMu.RLock()
	listeners := make([]func(LogMessage), 0, len(s.listeners))
	for _, cb := range s.listeners {
		listeners = append(listeners, cb)
	}
	s.listenersMu.RUnlock()

	for _, cb := range listeners {
		go cb(msg)
	}
}

// Subscribe registers a callback to receive new log messages.
// Returns a DebugStoreListenerID that can be used to unsubscribe.
func (s *DebugLogStore) Subscribe(cb func(LogMessage)) DebugStoreListenerID {
	s.listenersMu.Lock()
	defer s.listenersMu.Unlock()
	id := DebugStoreListenerID(fmt.Sprintf("debug-%d", atomic.AddUint64(&s.counter, 1)))
	s.listeners[id] = cb
	return id
}

// Unsubscribe removes a previously registered subscriber.
func (s *DebugLogStore) Unsubscribe(id DebugStoreListenerID) {
	s.listenersMu.Lock()
	defer s.listenersMu.Unlock()
	delete(s.listeners, id)
}

// GetMessages returns a copy of all messages in the store.
func (s *DebugLogStore) GetMessages() []LogMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]LogMessage, len(s.messages))
	copy(result, s.messages)
	return result
}

// Clear removes all messages from the store.
func (s *DebugLogStore) Clear() {
	s.mu.Lock()
	s.messages = make([]LogMessage, 0)
	s.mu.Unlock()
}

// SetFileLogger sets a file logger for writing debug messages to disk.
func (s *DebugLogStore) SetFileLogger(logger *logging.FileLogger) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fileLogger = logger
}

// StoreLog logs a message to the global debug store if it exists.
func StoreLog(format string, args ...interface{}) {
	if globalDebugStore != nil {
		globalDebugStore.Log("", format, args...)
	}
}

// StoreLogSSH logs an SSH message to the global debug store.
func StoreLogSSH(format string, args ...interface{}) {
	if globalDebugStore != nil {
		globalDebugStore.Log("SSH", format, args...)
	}
}

