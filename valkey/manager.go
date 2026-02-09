package valkey

import (
	"sync"
	"time"

	"warlink/config"
)

// Batching configuration for Valkey
const (
	ValkeyBatchSize     = 100
	ValkeyBatchInterval = 20 * time.Millisecond
	ValkeyBatchQueueSize = 5000
)

// valkeyJob represents a pending publish operation.
type valkeyJob struct {
	item TagPublishItem
}

// Manager manages multiple Valkey publishers.
type Manager struct {
	publishers []*Publisher
	mu         sync.RWMutex

	// Shared callbacks
	writeHandler      func(plcName, tagName string, value interface{}) error
	writeValidator    func(plcName, tagName string) bool
	tagTypeLookup     func(plcName, tagName string) uint16
	onConnectCallback func()

	// Batching
	batchChan chan valkeyJob
	stopChan  chan struct{}
	wg        sync.WaitGroup
	started   bool
}

// NewManager creates a new Valkey manager.
func NewManager() *Manager {
	m := &Manager{
		publishers: make([]*Publisher, 0),
		batchChan:  make(chan valkeyJob, ValkeyBatchQueueSize),
		stopChan:   make(chan struct{}),
	}
	m.startBatcher()
	return m
}

// startBatcher starts the batch processor goroutine.
func (m *Manager) startBatcher() {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	m.wg.Add(1) // Must be inside lock to prevent race with StopAll()
	m.mu.Unlock()

	go m.batchProcessor()
}

// batchProcessor collects items and publishes them in batches.
func (m *Manager) batchProcessor() {
	defer m.wg.Done()

	var batch []TagPublishItem
	ticker := time.NewTicker(ValkeyBatchInterval)
	defer ticker.Stop()

	flushBatch := func() {
		if len(batch) == 0 {
			return
		}

		m.mu.RLock()
		publishers := make([]*Publisher, len(m.publishers))
		copy(publishers, m.publishers)
		m.mu.RUnlock()

		for _, pub := range publishers {
			if pub.IsRunning() {
				if err := pub.PublishBatch(batch); err != nil {
					debugLog("Valkey batch publish error (%s): %v", pub.config.Name, err)
				}
			}
		}
		batch = batch[:0] // Clear but keep capacity
	}

	for {
		select {
		case <-m.stopChan:
			flushBatch()
			return

		case job, ok := <-m.batchChan:
			if !ok {
				flushBatch()
				return
			}
			batch = append(batch, job.item)
			if len(batch) >= ValkeyBatchSize {
				flushBatch()
			}

		case <-ticker.C:
			flushBatch()
		}
	}
}

// LoadFromConfig loads publishers from configuration.
func (m *Manager) LoadFromConfig(configs []config.ValkeyConfig, ns string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range configs {
		pub := NewPublisher(&configs[i], ns)
		pub.SetWriteHandler(m.writeHandler)
		pub.SetWriteValidator(m.writeValidator)
		pub.SetTagTypeLookup(m.tagTypeLookup)
		pub.SetOnConnectCallback(m.onConnectCallback)
		m.publishers = append(m.publishers, pub)
	}
}

// Add adds a new publisher.
func (m *Manager) Add(cfg *config.ValkeyConfig, ns string) *Publisher {
	m.mu.Lock()
	defer m.mu.Unlock()

	pub := NewPublisher(cfg, ns)
	pub.SetWriteHandler(m.writeHandler)
	pub.SetWriteValidator(m.writeValidator)
	pub.SetTagTypeLookup(m.tagTypeLookup)
	pub.SetOnConnectCallback(m.onConnectCallback)
	m.publishers = append(m.publishers, pub)
	return pub
}

// Remove removes a publisher by name.
func (m *Manager) Remove(name string) bool {
	m.mu.Lock()

	var pubToStop *Publisher
	for i, pub := range m.publishers {
		if pub.config.Name == name {
			pubToStop = pub
			m.publishers = append(m.publishers[:i], m.publishers[i+1:]...)
			break
		}
	}
	m.mu.Unlock()

	// Stop OUTSIDE the lock to prevent blocking
	if pubToStop != nil {
		pubToStop.Stop()
		return true
	}
	return false
}

// Get returns a publisher by name.
func (m *Manager) Get(name string) *Publisher {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, pub := range m.publishers {
		if pub.config.Name == name {
			return pub
		}
	}
	return nil
}

// List returns all publishers.
func (m *Manager) List() []*Publisher {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Publisher, len(m.publishers))
	copy(result, m.publishers)
	return result
}

// Start starts a publisher by name.
func (m *Manager) Start(name string) error {
	pub := m.Get(name)
	if pub == nil {
		return nil
	}
	return pub.Start()
}

// Stop stops a publisher by name.
func (m *Manager) Stop(name string) error {
	pub := m.Get(name)
	if pub == nil {
		return nil
	}
	return pub.Stop()
}

// StartAll starts all enabled publishers.
func (m *Manager) StartAll() int {
	m.mu.RLock()
	publishers := make([]*Publisher, len(m.publishers))
	copy(publishers, m.publishers)
	m.mu.RUnlock()

	started := 0
	for _, pub := range publishers {
		if pub.config.Enabled {
			if err := pub.Start(); err != nil {
				debugLog("Failed to start Valkey %s: %v", pub.config.Name, err)
			} else {
				debugLog("Started Valkey %s at %s", pub.config.Name, pub.Address())
				started++
			}
		}
	}
	return started
}

// StopAll stops all publishers and the batcher.
func (m *Manager) StopAll() {
	// Stop batcher first
	m.mu.Lock()
	if m.started {
		oldStopChan := m.stopChan
		m.stopChan = make(chan struct{})
		m.batchChan = make(chan valkeyJob, ValkeyBatchQueueSize)
		m.started = false
		m.mu.Unlock()

		close(oldStopChan)

		// Wait for batcher with timeout
		done := make(chan struct{})
		go func() {
			m.wg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			debugLog("Timeout waiting for Valkey batcher to stop")
		}
	} else {
		m.mu.Unlock()
	}

	// Stop all publishers
	m.mu.RLock()
	publishers := make([]*Publisher, len(m.publishers))
	copy(publishers, m.publishers)
	m.mu.RUnlock()

	for _, pub := range publishers {
		pub.Stop()
	}
}

// AnyRunning returns true if any publisher is running.
func (m *Manager) AnyRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, pub := range m.publishers {
		if pub.IsRunning() {
			return true
		}
	}
	return false
}

// Publish queues a tag value for batched publishing to all running publishers.
// For S7 PLCs, alias is the user-defined name and address is the S7 address in uppercase.
func (m *Manager) Publish(plcName, tagName, alias, address, typeName string, value interface{}, writable bool) {
	// Ensure batcher is running
	m.startBatcher()

	job := valkeyJob{
		item: TagPublishItem{
			PLCName:  plcName,
			TagName:  tagName,
			Alias:    alias,
			Address:  address,
			TypeName: typeName,
			Value:    value,
			Writable: writable,
		},
	}

	// Block until queued (with timeout) - no message dropping
	select {
	case m.batchChan <- job:
		// Queued for batching
	case <-time.After(5 * time.Second):
		debugLog("WARN: Valkey batch queue blocked >5s for %s/%s", plcName, tagName)
		m.batchChan <- job
	}
}

// PublishHealth publishes PLC health status to all running Valkey publishers.
func (m *Manager) PublishHealth(plcName, driver string, online bool, status, errMsg string) {
	m.mu.RLock()
	publishers := make([]*Publisher, len(m.publishers))
	copy(publishers, m.publishers)
	m.mu.RUnlock()

	for _, pub := range publishers {
		if pub.IsRunning() {
			if err := pub.PublishHealth(plcName, driver, online, status, errMsg); err != nil {
				debugLog("Valkey health publish error (%s): %v", pub.config.Name, err)
			}
		}
	}
}

// SetWriteHandler sets the write handler for all publishers.
func (m *Manager) SetWriteHandler(handler func(plcName, tagName string, value interface{}) error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.writeHandler = handler
	for _, pub := range m.publishers {
		pub.SetWriteHandler(handler)
	}
}

// SetWriteValidator sets the write validator for all publishers.
func (m *Manager) SetWriteValidator(validator func(plcName, tagName string) bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.writeValidator = validator
	for _, pub := range m.publishers {
		pub.SetWriteValidator(validator)
	}
}

// SetTagTypeLookup sets the tag type lookup for all publishers.
func (m *Manager) SetTagTypeLookup(lookup func(plcName, tagName string) uint16) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.tagTypeLookup = lookup
	for _, pub := range m.publishers {
		pub.SetTagTypeLookup(lookup)
	}
}

// SetOnConnectCallback sets the callback invoked after connection is established.
func (m *Manager) SetOnConnectCallback(callback func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.onConnectCallback = callback
	for _, pub := range m.publishers {
		pub.SetOnConnectCallback(callback)
	}
}

// PublishRaw publishes raw bytes to a channel on all running publishers.
// Used for TagPack publishing.
func (m *Manager) PublishRaw(channel string, data []byte) {
	m.mu.RLock()
	publishers := make([]*Publisher, len(m.publishers))
	copy(publishers, m.publishers)
	m.mu.RUnlock()

	for _, pub := range publishers {
		if pub.IsRunning() {
			if err := pub.PublishRaw(channel, data); err != nil {
				debugLog("Valkey raw publish error (%s): %v", pub.config.Name, err)
			}
		}
	}
}
