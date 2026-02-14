package rule

import (
	"fmt"
	"sync"
	"time"

	"warlink/config"
	"warlink/kafka"
	"warlink/tagpack"
)

// Manager manages all configured rules.
type Manager struct {
	rules     map[string]*Rule
	kafka     *kafka.Manager
	mqtt      MQTTPublisher
	packMgr   *tagpack.Manager
	reader    TagReader
	writer    TagWriter
	namespace string
	mu        sync.RWMutex

	logFn func(format string, args ...interface{})
}

// NewManager creates a new rule manager.
func NewManager(kafkaMgr *kafka.Manager, reader TagReader, writer TagWriter) *Manager {
	return &Manager{
		rules:  make(map[string]*Rule),
		kafka:  kafkaMgr,
		reader: reader,
		writer: writer,
	}
}

// SetPackManager sets the TagPack manager for all rules.
func (m *Manager) SetPackManager(packMgr *tagpack.Manager) {
	m.mu.Lock()
	m.packMgr = packMgr
	for _, r := range m.rules {
		r.SetPackManager(packMgr)
	}
	m.mu.Unlock()
}

// SetMQTTManager sets the MQTT publisher for all rules.
func (m *Manager) SetMQTTManager(mqtt MQTTPublisher) {
	m.mu.Lock()
	m.mqtt = mqtt
	for _, r := range m.rules {
		r.SetMQTTManager(mqtt)
	}
	m.mu.Unlock()
}

// SetNamespace sets the namespace for topic construction.
func (m *Manager) SetNamespace(ns string) {
	m.mu.Lock()
	m.namespace = ns
	for _, r := range m.rules {
		r.SetNamespace(ns)
	}
	m.mu.Unlock()
}

// SetLogFunc sets the logging callback for all rules.
func (m *Manager) SetLogFunc(fn func(format string, args ...interface{})) {
	m.mu.Lock()
	m.logFn = fn
	for _, r := range m.rules {
		r.SetLogFunc(fn)
	}
	m.mu.Unlock()
}

func (m *Manager) log(format string, args ...interface{}) {
	m.mu.RLock()
	fn := m.logFn
	m.mu.RUnlock()
	if fn != nil {
		fn("[RuleMgr] "+format, args...)
	}
}

// AddRule adds a new rule configuration.
func (m *Manager) AddRule(cfg *config.RuleConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.rules[cfg.Name]; exists {
		return fmt.Errorf("rule already exists: %s", cfg.Name)
	}

	rule, err := NewRule(cfg, m.kafka, m.reader, m.writer)
	if err != nil {
		return err
	}

	rule.SetLogFunc(m.logFn)
	rule.SetPackManager(m.packMgr)
	rule.SetMQTTManager(m.mqtt)
	rule.SetNamespace(m.namespace)
	m.rules[cfg.Name] = rule

	return nil
}

// RemoveRule removes and stops a rule.
func (m *Manager) RemoveRule(name string) {
	m.mu.Lock()
	rule, exists := m.rules[name]
	if exists {
		delete(m.rules, name)
	}
	m.mu.Unlock()

	if exists && rule != nil {
		rule.Stop()
	}
}

// GetRule returns the rule with the given name.
func (m *Manager) GetRule(name string) *Rule {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.rules[name]
}

// ListRules returns all rule names.
func (m *Manager) ListRules() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.rules))
	for name := range m.rules {
		names = append(names, name)
	}
	return names
}

// Start starts all enabled rules.
func (m *Manager) Start() {
	m.mu.RLock()
	rules := make([]*Rule, 0, len(m.rules))
	for _, r := range m.rules {
		rules = append(rules, r)
	}
	m.mu.RUnlock()

	for _, r := range rules {
		r.Start()
	}

	m.log("started %d rules", len(rules))
}

// Stop stops all rules.
func (m *Manager) Stop() {
	m.mu.RLock()
	rules := make([]*Rule, 0, len(m.rules))
	for _, r := range m.rules {
		rules = append(rules, r)
	}
	m.mu.RUnlock()

	for _, r := range rules {
		r.Stop()
	}

	m.log("stopped all rules")
}

// StartRule starts a specific rule.
func (m *Manager) StartRule(name string) error {
	m.mu.RLock()
	rule, exists := m.rules[name]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("rule not found: %s", name)
	}

	rule.Start()
	return nil
}

// StopRule stops a specific rule.
func (m *Manager) StopRule(name string) error {
	m.mu.RLock()
	rule, exists := m.rules[name]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("rule not found: %s", name)
	}

	rule.Stop()
	return nil
}

// ResetRule resets a rule from error state.
func (m *Manager) ResetRule(name string) error {
	m.mu.RLock()
	rule, exists := m.rules[name]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("rule not found: %s", name)
	}

	rule.Reset()
	return nil
}

// TestFireRule manually fires a rule for testing purposes.
func (m *Manager) TestFireRule(name string) error {
	m.mu.RLock()
	rule, exists := m.rules[name]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("rule not found: %s", name)
	}

	return rule.TestFire()
}

// GetRuleStatus returns the status of a specific rule.
func (m *Manager) GetRuleStatus(name string) (Status, error, int64, time.Time) {
	if !m.mu.TryRLock() {
		return StatusFiring, nil, 0, time.Time{}
	}
	rule, exists := m.rules[name]
	m.mu.RUnlock()

	if !exists {
		return StatusDisabled, nil, 0, time.Time{}
	}

	status := rule.GetStatus()
	err := rule.GetError()
	count, lastFire := rule.GetStats()

	return status, err, count, lastFire
}

// LoadFromConfig loads rules from configuration.
func (m *Manager) LoadFromConfig(configs []config.RuleConfig) {
	for i := range configs {
		if err := m.AddRule(&configs[i]); err != nil {
			m.log("error adding rule %s: %v", configs[i].Name, err)
		}
	}
}

// UpdateRule updates an existing rule configuration.
func (m *Manager) UpdateRule(cfg *config.RuleConfig) error {
	m.RemoveRule(cfg.Name)
	return m.AddRule(cfg)
}

// RuleInfo holds summary information about a rule.
type RuleInfo struct {
	Name       string
	LogicMode  config.RuleLogicMode
	Conditions int
	Actions    int
	Status     Status
	Error      error
	FireCount  int64
	LastFire   time.Time
}

// GetAllRuleInfo returns info for all rules.
func (m *Manager) GetAllRuleInfo() []RuleInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	infos := make([]RuleInfo, 0, len(m.rules))
	for name, r := range m.rules {
		status := r.GetStatus()
		err := r.GetError()
		count, lastFire := r.GetStats()

		logicMode := r.config.LogicMode
		if logicMode == "" {
			logicMode = config.RuleLogicAND
		}

		infos = append(infos, RuleInfo{
			Name:       name,
			LogicMode:  logicMode,
			Conditions: len(r.config.Conditions),
			Actions:    len(r.config.Actions),
			Status:     status,
			Error:      err,
			FireCount:  count,
			LastFire:   lastFire,
		})
	}
	return infos
}
