package push

import (
	"fmt"
	"sync"
	"time"

	"warlink/config"
	"warlink/trigger"
)

// Manager manages all configured pushes.
type Manager struct {
	pushes map[string]*Push
	reader trigger.TagReader
	mu     sync.RWMutex

	logFn func(format string, args ...interface{})
}

// NewManager creates a new push manager.
func NewManager(reader trigger.TagReader) *Manager {
	return &Manager{
		pushes: make(map[string]*Push),
		reader: reader,
	}
}

// SetLogFunc sets the logging callback for all pushes.
func (m *Manager) SetLogFunc(fn func(format string, args ...interface{})) {
	m.mu.Lock()
	m.logFn = fn
	for _, p := range m.pushes {
		p.SetLogFunc(fn)
	}
	m.mu.Unlock()
}

func (m *Manager) log(format string, args ...interface{}) {
	m.mu.RLock()
	fn := m.logFn
	m.mu.RUnlock()
	if fn != nil {
		fn("[PushMgr] "+format, args...)
	}
}

// AddPush adds a new push configuration.
func (m *Manager) AddPush(cfg *config.PushConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.pushes[cfg.Name]; exists {
		return fmt.Errorf("push already exists: %s", cfg.Name)
	}

	push, err := NewPush(cfg, m.reader)
	if err != nil {
		return err
	}

	push.SetLogFunc(m.logFn)
	m.pushes[cfg.Name] = push
	return nil
}

// RemovePush removes and stops a push.
func (m *Manager) RemovePush(name string) {
	m.mu.Lock()
	push, exists := m.pushes[name]
	if exists {
		delete(m.pushes, name)
	}
	m.mu.Unlock()

	if exists && push != nil {
		push.Stop()
	}
}

// UpdatePush updates an existing push configuration.
func (m *Manager) UpdatePush(cfg *config.PushConfig) error {
	m.RemovePush(cfg.Name)
	return m.AddPush(cfg)
}

// GetPush returns the push with the given name.
func (m *Manager) GetPush(name string) *Push {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.pushes[name]
}

// ListPushes returns all push names.
func (m *Manager) ListPushes() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.pushes))
	for name := range m.pushes {
		names = append(names, name)
	}
	return names
}

// Start starts all enabled pushes.
func (m *Manager) Start() {
	m.mu.RLock()
	pushes := make([]*Push, 0, len(m.pushes))
	for _, p := range m.pushes {
		pushes = append(pushes, p)
	}
	m.mu.RUnlock()

	for _, p := range pushes {
		p.Start()
	}

	m.log("started %d pushes", len(pushes))
}

// Stop stops all pushes.
func (m *Manager) Stop() {
	m.mu.RLock()
	pushes := make([]*Push, 0, len(m.pushes))
	for _, p := range m.pushes {
		pushes = append(pushes, p)
	}
	m.mu.RUnlock()

	for _, p := range pushes {
		p.Stop()
	}

	m.log("stopped all pushes")
}

// StartPush starts a specific push.
func (m *Manager) StartPush(name string) error {
	m.mu.RLock()
	push, exists := m.pushes[name]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("push not found: %s", name)
	}

	push.Start()
	return nil
}

// StopPush stops a specific push.
func (m *Manager) StopPush(name string) error {
	m.mu.RLock()
	push, exists := m.pushes[name]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("push not found: %s", name)
	}

	push.Stop()
	return nil
}

// TestFirePush manually fires a push for testing purposes.
func (m *Manager) TestFirePush(name string) error {
	m.mu.RLock()
	push, exists := m.pushes[name]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("push not found: %s", name)
	}

	return push.TestFire()
}

// ResetPush resets a push from error state.
func (m *Manager) ResetPush(name string) error {
	m.mu.RLock()
	push, exists := m.pushes[name]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("push not found: %s", name)
	}

	push.Reset()
	return nil
}

// GetPushStatus returns the status of a specific push.
func (m *Manager) GetPushStatus(name string) (Status, error, int64, time.Time, int) {
	if !m.mu.TryRLock() {
		return StatusFiring, nil, 0, time.Time{}, 0
	}
	push, exists := m.pushes[name]
	m.mu.RUnlock()

	if !exists {
		return StatusDisabled, nil, 0, time.Time{}, 0
	}

	status := push.GetStatus()
	err := push.GetError()
	count, lastSend, lastCode := push.GetStats()
	return status, err, count, lastSend, lastCode
}

// LoadFromConfig loads pushes from configuration.
func (m *Manager) LoadFromConfig(configs []config.PushConfig) {
	for i := range configs {
		if err := m.AddPush(&configs[i]); err != nil {
			m.log("error adding push %s: %v", configs[i].Name, err)
		}
	}
}

// PushInfo holds summary information about a push.
type PushInfo struct {
	Name         string
	URL          string
	Method       string
	Conditions   int
	Status       Status
	Error        error
	SendCount    int64
	LastSend     time.Time
	LastHTTPCode int
}

// GetAllPushInfo returns info for all pushes.
func (m *Manager) GetAllPushInfo() []PushInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	infos := make([]PushInfo, 0, len(m.pushes))
	for name, p := range m.pushes {
		status := p.GetStatus()
		err := p.GetError()
		count, lastSend, lastCode := p.GetStats()

		infos = append(infos, PushInfo{
			Name:         name,
			URL:          p.config.URL,
			Method:       p.config.Method,
			Conditions:   len(p.config.Conditions),
			Status:       status,
			Error:        err,
			SendCount:    count,
			LastSend:     lastSend,
			LastHTTPCode: lastCode,
		})
	}
	return infos
}
