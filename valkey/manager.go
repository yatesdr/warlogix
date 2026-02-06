package valkey

import (
	"sync"

	"warlogix/config"
)

// Manager manages multiple Valkey publishers.
type Manager struct {
	publishers []*Publisher
	mu         sync.RWMutex

	// Shared callbacks
	writeHandler      func(plcName, tagName string, value interface{}) error
	writeValidator    func(plcName, tagName string) bool
	tagTypeLookup     func(plcName, tagName string) uint16
	onConnectCallback func()
	plcNames          []string
}

// NewManager creates a new Valkey manager.
func NewManager() *Manager {
	return &Manager{
		publishers: make([]*Publisher, 0),
	}
}

// LoadFromConfig loads publishers from configuration.
func (m *Manager) LoadFromConfig(configs []config.ValkeyConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range configs {
		pub := NewPublisher(&configs[i])
		pub.SetWriteHandler(m.writeHandler)
		pub.SetWriteValidator(m.writeValidator)
		pub.SetTagTypeLookup(m.tagTypeLookup)
		pub.SetOnConnectCallback(m.onConnectCallback)
		m.publishers = append(m.publishers, pub)
	}
}

// Add adds a new publisher.
func (m *Manager) Add(cfg *config.ValkeyConfig) *Publisher {
	m.mu.Lock()
	defer m.mu.Unlock()

	pub := NewPublisher(cfg)
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

// StopAll stops all publishers.
func (m *Manager) StopAll() {
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

// Publish publishes a tag value to all running publishers.
// For S7 PLCs, alias is the user-defined name and address is the S7 address in uppercase.
func (m *Manager) Publish(plcName, tagName, alias, address, typeName string, value interface{}, writable bool) {
	m.mu.RLock()
	publishers := make([]*Publisher, len(m.publishers))
	copy(publishers, m.publishers)
	m.mu.RUnlock()

	if len(publishers) == 0 {
		debugLog("Manager.Publish: no publishers configured")
		return
	}

	runningCount := 0
	for _, pub := range publishers {
		if pub.IsRunning() {
			runningCount++
			if err := pub.Publish(plcName, tagName, alias, address, typeName, value, writable); err != nil {
				debugLog("Valkey publish error (%s): %v", pub.config.Name, err)
			}
		}
	}
	if runningCount == 0 {
		debugLog("Manager.Publish: no publishers running")
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

// SetPLCNames sets the PLC names for write subscriptions.
func (m *Manager) SetPLCNames(names []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.plcNames = names
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
