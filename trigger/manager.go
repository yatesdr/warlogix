package trigger

import (
	"fmt"
	"sync"
	"time"

	"warlogix/config"
	"warlogix/kafka"
)

// Manager manages all configured triggers.
type Manager struct {
	triggers map[string]*Trigger
	kafka    *kafka.Manager
	reader   TagReader
	writer   TagWriter
	mu       sync.RWMutex

	logFn func(format string, args ...interface{})
}

// NewManager creates a new trigger manager.
func NewManager(kafkaMgr *kafka.Manager, reader TagReader, writer TagWriter) *Manager {
	return &Manager{
		triggers: make(map[string]*Trigger),
		kafka:    kafkaMgr,
		reader:   reader,
		writer:   writer,
	}
}

// SetLogFunc sets the logging callback for all triggers.
func (m *Manager) SetLogFunc(fn func(format string, args ...interface{})) {
	m.mu.Lock()
	m.logFn = fn
	for _, t := range m.triggers {
		t.SetLogFunc(fn)
	}
	m.mu.Unlock()
}

func (m *Manager) log(format string, args ...interface{}) {
	m.mu.RLock()
	fn := m.logFn
	m.mu.RUnlock()
	if fn != nil {
		fn("[TriggerMgr] "+format, args...)
	}
}

// AddTrigger adds a new trigger configuration.
func (m *Manager) AddTrigger(cfg *config.TriggerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.triggers[cfg.Name]; exists {
		return fmt.Errorf("trigger already exists: %s", cfg.Name)
	}

	trigger, err := NewTrigger(cfg, m.kafka, m.reader, m.writer)
	if err != nil {
		return err
	}

	trigger.SetLogFunc(m.logFn)
	m.triggers[cfg.Name] = trigger

	return nil
}

// RemoveTrigger removes and stops a trigger.
func (m *Manager) RemoveTrigger(name string) {
	m.mu.Lock()
	trigger, exists := m.triggers[name]
	if exists {
		delete(m.triggers, name)
	}
	m.mu.Unlock()

	if exists && trigger != nil {
		trigger.Stop()
	}
}

// GetTrigger returns the trigger with the given name.
func (m *Manager) GetTrigger(name string) *Trigger {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.triggers[name]
}

// ListTriggers returns all trigger names.
func (m *Manager) ListTriggers() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.triggers))
	for name := range m.triggers {
		names = append(names, name)
	}
	return names
}

// Start starts all enabled triggers.
func (m *Manager) Start() {
	m.mu.RLock()
	triggers := make([]*Trigger, 0, len(m.triggers))
	for _, t := range m.triggers {
		triggers = append(triggers, t)
	}
	m.mu.RUnlock()

	for _, t := range triggers {
		t.Start()
	}

	m.log("started %d triggers", len(triggers))
}

// Stop stops all triggers.
func (m *Manager) Stop() {
	m.mu.RLock()
	triggers := make([]*Trigger, 0, len(m.triggers))
	for _, t := range m.triggers {
		triggers = append(triggers, t)
	}
	m.mu.RUnlock()

	for _, t := range triggers {
		t.Stop()
	}

	m.log("stopped all triggers")
}

// StartTrigger starts a specific trigger.
func (m *Manager) StartTrigger(name string) error {
	m.mu.RLock()
	trigger, exists := m.triggers[name]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("trigger not found: %s", name)
	}

	trigger.Start()
	return nil
}

// StopTrigger stops a specific trigger.
func (m *Manager) StopTrigger(name string) error {
	m.mu.RLock()
	trigger, exists := m.triggers[name]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("trigger not found: %s", name)
	}

	trigger.Stop()
	return nil
}

// ResetTrigger resets a trigger from error state.
func (m *Manager) ResetTrigger(name string) error {
	m.mu.RLock()
	trigger, exists := m.triggers[name]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("trigger not found: %s", name)
	}

	trigger.Reset()
	return nil
}

// TestFireTrigger manually fires a trigger for testing purposes.
func (m *Manager) TestFireTrigger(name string) error {
	m.mu.RLock()
	trigger, exists := m.triggers[name]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("trigger not found: %s", name)
	}

	return trigger.TestFire()
}

// GetTriggerStatus returns the status of a specific trigger.
// Uses TryRLock to avoid blocking the UI thread.
func (m *Manager) GetTriggerStatus(name string) (Status, error, int64, time.Time) {
	if !m.mu.TryRLock() {
		return StatusFiring, nil, 0, time.Time{} // Return firing if manager is busy
	}
	trigger, exists := m.triggers[name]
	m.mu.RUnlock()

	if !exists {
		return StatusDisabled, nil, 0, time.Time{}
	}

	status := trigger.GetStatus()
	err := trigger.GetError()
	count, lastFire := trigger.GetStats()

	return status, err, count, lastFire
}

// LoadFromConfig loads triggers from configuration.
func (m *Manager) LoadFromConfig(configs []config.TriggerConfig) {
	for i := range configs {
		if err := m.AddTrigger(&configs[i]); err != nil {
			m.log("error adding trigger %s: %v", configs[i].Name, err)
		}
	}
}

// UpdateTrigger updates an existing trigger configuration.
// This stops the old trigger and creates a new one.
func (m *Manager) UpdateTrigger(cfg *config.TriggerConfig) error {
	m.RemoveTrigger(cfg.Name)
	return m.AddTrigger(cfg)
}

// TriggerInfo holds summary information about a trigger.
type TriggerInfo struct {
	Name      string
	PLC       string
	Tag       string
	Topic     string
	Status    Status
	Error     error
	FireCount int64
	LastFire  time.Time
}

// GetAllTriggerInfo returns info for all triggers.
func (m *Manager) GetAllTriggerInfo() []TriggerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	infos := make([]TriggerInfo, 0, len(m.triggers))
	for name, t := range m.triggers {
		status := t.GetStatus()
		err := t.GetError()
		count, lastFire := t.GetStats()

		infos = append(infos, TriggerInfo{
			Name:      name,
			PLC:       t.config.PLC,
			Tag:       t.config.TriggerTag,
			Topic:     t.config.Topic,
			Status:    status,
			Error:     err,
			FireCount: count,
			LastFire:  lastFire,
		})
	}
	return infos
}
