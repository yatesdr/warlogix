// Package plcman provides PLC connection management with background polling.
package plcman

import (
	"context"
	"fmt"
	"sync"
	"time"

	"warlogix/config"
	"warlogix/logix"
)

// ConnectionStatus represents the state of a PLC connection.
type ConnectionStatus int

const (
	StatusDisconnected ConnectionStatus = iota
	StatusConnecting
	StatusConnected
	StatusError
)

func (s ConnectionStatus) String() string {
	switch s {
	case StatusDisconnected:
		return "Disconnected"
	case StatusConnecting:
		return "Connecting"
	case StatusConnected:
		return "Connected"
	case StatusError:
		return "Error"
	default:
		return "Unknown"
	}
}

// ManagedPLC represents a PLC under management.
type ManagedPLC struct {
	Config    *config.PLCConfig
	Client    *logix.Client
	Identity  *logix.DeviceInfo
	Programs  []string
	Tags      []logix.TagInfo
	Values    map[string]*logix.TagValue
	Status    ConnectionStatus
	LastError error
	LastPoll  time.Time
	mu        sync.RWMutex
}

// GetStatus returns the current connection status thread-safely.
func (m *ManagedPLC) GetStatus() ConnectionStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.Status
}

// GetError returns the last error thread-safely.
func (m *ManagedPLC) GetError() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.LastError
}

// GetValues returns a copy of the current tag values.
func (m *ManagedPLC) GetValues() map[string]*logix.TagValue {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]*logix.TagValue, len(m.Values))
	for k, v := range m.Values {
		result[k] = v
	}
	return result
}

// GetTags returns the discovered tags.
func (m *ManagedPLC) GetTags() []logix.TagInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.Tags
}

// GetPrograms returns the discovered programs.
func (m *ManagedPLC) GetPrograms() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.Programs
}

// GetIdentity returns the device identity info.
func (m *ManagedPLC) GetIdentity() *logix.DeviceInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.Identity
}

// ValueChange represents a tag value that has changed.
type ValueChange struct {
	PLCName  string
	TagName  string
	TypeName string
	Value    interface{}
}

// PollStats tracks polling statistics for debugging.
type PollStats struct {
	LastPollTime   time.Time
	TagsPolled     int
	ChangesFound   int
	LastError      error
}

// Manager manages multiple PLC connections and polling.
type Manager struct {
	plcs          map[string]*ManagedPLC
	mu            sync.RWMutex
	pollRate      time.Duration
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	onChange      func()                     // Callback when status changes
	onValueChange func(changes []ValueChange) // Callback when tag values change
	lastPollStats PollStats                  // Stats from last poll cycle
}

// NewManager creates a new PLC manager.
func NewManager(pollRate time.Duration) *Manager {
	if pollRate <= 0 {
		pollRate = time.Second
	}
	return &Manager{
		plcs:     make(map[string]*ManagedPLC),
		pollRate: pollRate,
	}
}

// SetOnChange sets a callback that fires when PLC status changes.
func (m *Manager) SetOnChange(fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onChange = fn
}

// SetOnValueChange sets a callback that fires when tag values change.
func (m *Manager) SetOnValueChange(fn func(changes []ValueChange)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onValueChange = fn
}

func (m *Manager) notifyChange() {
	m.mu.RLock()
	fn := m.onChange
	m.mu.RUnlock()
	if fn != nil {
		fn()
	}
}

func (m *Manager) notifyValueChange(changes []ValueChange) {
	m.mu.RLock()
	fn := m.onValueChange
	m.mu.RUnlock()
	if fn != nil && len(changes) > 0 {
		fn(changes)
	}
}

// AddPLC adds a PLC to management.
func (m *Manager) AddPLC(cfg *config.PLCConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.plcs[cfg.Name]; exists {
		return nil // Already exists
	}

	m.plcs[cfg.Name] = &ManagedPLC{
		Config: cfg,
		Status: StatusDisconnected,
		Values: make(map[string]*logix.TagValue),
	}
	return nil
}

// RemovePLC removes a PLC from management and disconnects it.
func (m *Manager) RemovePLC(name string) error {
	m.mu.Lock()
	plc, exists := m.plcs[name]
	if exists {
		delete(m.plcs, name)
	}
	m.mu.Unlock()

	if exists && plc.Client != nil {
		plc.Client.Close()
	}
	m.notifyChange()
	return nil
}

// Connect establishes a connection to the named PLC.
func (m *Manager) Connect(name string) error {
	m.mu.RLock()
	plc, exists := m.plcs[name]
	m.mu.RUnlock()

	if !exists {
		return nil
	}

	plc.mu.Lock()
	plc.Status = StatusConnecting
	plc.LastError = nil
	plc.mu.Unlock()
	m.notifyChange()

	// Build connection options
	opts := []logix.Option{}
	if plc.Config.Slot > 0 {
		opts = append(opts, logix.WithSlot(plc.Config.Slot))
	}

	client, err := logix.Connect(plc.Config.Address, opts...)
	if err != nil {
		plc.mu.Lock()
		plc.Status = StatusError
		plc.LastError = err
		plc.mu.Unlock()
		m.notifyChange()
		return err
	}

	// Get identity
	identity, _ := client.Identity()

	// Discover programs and tags
	programs, _ := client.Programs()
	tags, _ := client.AllTags()

	plc.mu.Lock()
	plc.Client = client
	plc.Identity = identity
	plc.Programs = programs
	plc.Tags = tags
	plc.Status = StatusConnected
	plc.mu.Unlock()
	m.notifyChange()

	return nil
}

// Disconnect closes the connection to the named PLC.
func (m *Manager) Disconnect(name string) error {
	m.mu.RLock()
	plc, exists := m.plcs[name]
	m.mu.RUnlock()

	if !exists {
		return nil
	}

	plc.mu.Lock()
	if plc.Client != nil {
		plc.Client.Close()
		plc.Client = nil
	}
	plc.Status = StatusDisconnected
	plc.LastError = nil
	plc.Identity = nil
	plc.mu.Unlock()
	m.notifyChange()

	return nil
}

// GetPLC returns the managed PLC with the given name.
func (m *Manager) GetPLC(name string) *ManagedPLC {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.plcs[name]
}

// ListPLCs returns all managed PLCs.
func (m *Manager) ListPLCs() []*ManagedPLC {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*ManagedPLC, 0, len(m.plcs))
	for _, plc := range m.plcs {
		result = append(result, plc)
	}
	return result
}

// Start begins background polling for all connected PLCs.
func (m *Manager) Start() {
	m.mu.Lock()
	if m.ctx != nil {
		m.mu.Unlock()
		return // Already running
	}
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.mu.Unlock()

	m.wg.Add(1)
	go m.pollLoop()
}

// Stop halts background polling.
func (m *Manager) Stop() {
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.Unlock()

	m.wg.Wait()

	m.mu.Lock()
	m.ctx = nil
	m.cancel = nil
	m.mu.Unlock()
}

func (m *Manager) pollLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.pollRate)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.pollAll()
		}
	}
}

func (m *Manager) pollAll() {
	m.mu.RLock()
	plcs := make([]*ManagedPLC, 0, len(m.plcs))
	for _, plc := range m.plcs {
		plcs = append(plcs, plc)
	}
	m.mu.RUnlock()

	totalTags := 0
	totalChanges := 0
	var lastErr error

	for _, plc := range plcs {
		// Check if auto-reconnect is needed
		m.checkAutoReconnect(plc)
		// Poll for tag values
		tags, changes, err := m.pollPLC(plc)
		totalTags += tags
		totalChanges += changes
		if err != nil {
			lastErr = err
		}
	}

	// Update stats
	m.mu.Lock()
	m.lastPollStats = PollStats{
		LastPollTime: time.Now(),
		TagsPolled:   totalTags,
		ChangesFound: totalChanges,
		LastError:    lastErr,
	}
	m.mu.Unlock()
}

func (m *Manager) checkAutoReconnect(plc *ManagedPLC) {
	plc.mu.RLock()
	status := plc.Status
	enabled := plc.Config.Enabled
	name := plc.Config.Name
	plc.mu.RUnlock()

	// Only auto-reconnect if enabled and currently disconnected or in error state
	if !enabled {
		return
	}
	if status == StatusConnected || status == StatusConnecting {
		return
	}

	// Attempt reconnection
	go m.Connect(name)
}

func (m *Manager) pollPLC(plc *ManagedPLC) (tagsPolled int, changesFound int, pollErr error) {
	plc.mu.RLock()
	client := plc.Client
	status := plc.Status
	cfg := plc.Config
	plcName := cfg.Name
	oldValues := make(map[string]interface{})
	for k, v := range plc.Values {
		if v != nil && v.Error == nil {
			oldValues[k] = v.GoValue()
		}
	}
	plc.mu.RUnlock()

	if status != StatusConnected || client == nil {
		return 0, 0, nil
	}

	// Determine which tags to read
	tagsToRead := []string{}
	for _, sel := range cfg.Tags {
		if sel.Enabled {
			tagsToRead = append(tagsToRead, sel.Name)
		}
	}

	if len(tagsToRead) == 0 {
		return 0, 0, nil
	}

	// Read selected tags
	values, err := client.Read(tagsToRead...)
	if err != nil {
		plc.mu.Lock()
		plc.LastError = err
		plc.Status = StatusError
		plc.mu.Unlock()
		m.notifyChange()
		return len(tagsToRead), 0, err
	}

	// Detect changes and update values
	var changes []ValueChange
	plc.mu.Lock()
	for _, v := range values {
		if v.Error == nil {
			newVal := v.GoValue()
			oldVal, existed := oldValues[v.Name]
			// Check if value changed (or is new)
			if !existed || fmt.Sprintf("%v", oldVal) != fmt.Sprintf("%v", newVal) {
				changes = append(changes, ValueChange{
					PLCName:  plcName,
					TagName:  v.Name,
					TypeName: v.TypeName(),
					Value:    newVal,
				})
			}
		}
		plc.Values[v.Name] = v
	}
	plc.LastPoll = time.Now()
	plc.mu.Unlock()

	m.notifyChange()
	m.notifyValueChange(changes)

	return len(tagsToRead), len(changes), nil
}

// ReadTag reads a single tag from a connected PLC.
func (m *Manager) ReadTag(plcName, tagName string) (*logix.TagValue, error) {
	m.mu.RLock()
	plc, exists := m.plcs[plcName]
	m.mu.RUnlock()

	if !exists || plc.Client == nil {
		return nil, nil
	}

	plc.mu.RLock()
	client := plc.Client
	plc.mu.RUnlock()

	if client == nil {
		return nil, nil
	}

	values, err := client.Read(tagName)
	if err != nil {
		return nil, err
	}
	if len(values) > 0 {
		return values[0], nil
	}
	return nil, nil
}

// WriteTag writes a value to a tag on a connected PLC.
func (m *Manager) WriteTag(plcName, tagName string, value interface{}) error {
	m.mu.RLock()
	plc, exists := m.plcs[plcName]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("PLC not found: %s", plcName)
	}

	plc.mu.RLock()
	client := plc.Client
	status := plc.Status
	plc.mu.RUnlock()

	if client == nil || status != StatusConnected {
		return fmt.Errorf("PLC not connected: %s", plcName)
	}

	return client.Write(tagName, value)
}

// LoadFromConfig adds all PLCs from configuration.
func (m *Manager) LoadFromConfig(cfg *config.Config) {
	for i := range cfg.PLCs {
		m.AddPLC(&cfg.PLCs[i])
	}
}

// ConnectEnabled connects all PLCs marked as enabled.
func (m *Manager) ConnectEnabled() {
	m.mu.RLock()
	plcs := make([]*ManagedPLC, 0)
	for _, plc := range m.plcs {
		if plc.Config.Enabled {
			plcs = append(plcs, plc)
		}
	}
	m.mu.RUnlock()

	for _, plc := range plcs {
		go m.Connect(plc.Config.Name)
	}
}

// DisconnectAll disconnects all PLCs.
func (m *Manager) DisconnectAll() {
	m.mu.RLock()
	names := make([]string, 0, len(m.plcs))
	for name := range m.plcs {
		names = append(names, name)
	}
	m.mu.RUnlock()

	for _, name := range names {
		m.Disconnect(name)
	}
}

// GetPollStats returns the stats from the last poll cycle.
func (m *Manager) GetPollStats() PollStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastPollStats
}

// GetAllCurrentValues returns all currently cached tag values for all PLCs.
// This is used for initial MQTT publish when a broker connects.
func (m *Manager) GetAllCurrentValues() []ValueChange {
	m.mu.RLock()
	plcs := make([]*ManagedPLC, 0, len(m.plcs))
	for _, plc := range m.plcs {
		plcs = append(plcs, plc)
	}
	m.mu.RUnlock()

	var results []ValueChange
	for _, plc := range plcs {
		plc.mu.RLock()
		plcName := plc.Config.Name
		for tagName, val := range plc.Values {
			if val != nil && val.Error == nil {
				results = append(results, ValueChange{
					PLCName:  plcName,
					TagName:  tagName,
					TypeName: val.TypeName(),
					Value:    val.GoValue(),
				})
			}
		}
		plc.mu.RUnlock()
	}
	return results
}

// GetTagType returns the data type code for a tag.
// Returns 0 if the tag type cannot be determined.
func (m *Manager) GetTagType(plcName, tagName string) uint16 {
	m.mu.RLock()
	plc, exists := m.plcs[plcName]
	m.mu.RUnlock()

	if !exists {
		return 0
	}

	// First check cached values
	plc.mu.RLock()
	if val, ok := plc.Values[tagName]; ok && val != nil {
		dataType := val.DataType
		plc.mu.RUnlock()
		return dataType
	}
	client := plc.Client
	status := plc.Status
	plc.mu.RUnlock()

	// If not cached, try to read the tag to get its type
	if client == nil || status != StatusConnected {
		return 0
	}

	values, err := client.Read(tagName)
	if err != nil || len(values) == 0 {
		return 0
	}

	return values[0].DataType
}
