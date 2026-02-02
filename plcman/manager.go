// Package plcman provides PLC connection management with background polling.
package plcman

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
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

// GetConnectionMode returns a human-readable string describing the connection mode.
func (m *ManagedPLC) GetConnectionMode() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.Client == nil {
		return "Not connected"
	}
	return m.Client.ConnectionMode()
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
	LastPollTime time.Time
	TagsPolled   int
	ChangesFound int
	LastError    error
}

// PLCWorker manages polling for a single PLC in its own goroutine.
type PLCWorker struct {
	plc      *ManagedPLC
	manager  *Manager
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	pollRate time.Duration

	// Per-worker stats
	tagsPolled   int
	changesFound int
	lastError    error
	statsMu      sync.RWMutex
}

// newPLCWorker creates a new worker for a PLC.
func newPLCWorker(plc *ManagedPLC, manager *Manager, pollRate time.Duration) *PLCWorker {
	ctx, cancel := context.WithCancel(context.Background())
	return &PLCWorker{
		plc:      plc,
		manager:  manager,
		ctx:      ctx,
		cancel:   cancel,
		pollRate: pollRate,
	}
}

// Start begins the worker's poll loop.
func (w *PLCWorker) Start() {
	w.wg.Add(1)
	go w.pollLoop()
}

// Stop halts the worker and waits for it to finish.
func (w *PLCWorker) Stop() {
	w.cancel()
	w.wg.Wait()
}

// GetStats returns the worker's current stats.
func (w *PLCWorker) GetStats() (tagsPolled, changesFound int, lastError error) {
	w.statsMu.RLock()
	defer w.statsMu.RUnlock()
	return w.tagsPolled, w.changesFound, w.lastError
}

func (w *PLCWorker) pollLoop() {
	defer w.wg.Done()

	ticker := time.NewTicker(w.pollRate)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			w.poll()
		}
	}
}

func (w *PLCWorker) poll() {
	plc := w.plc

	// Check if auto-reconnect is needed
	w.checkAutoReconnect()

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
		w.statsMu.Lock()
		w.tagsPolled = 0
		w.changesFound = 0
		w.lastError = nil
		w.statsMu.Unlock()
		return
	}

	// Determine which tags to read
	tagsToRead := []string{}
	for _, sel := range cfg.Tags {
		if sel.Enabled {
			tagsToRead = append(tagsToRead, sel.Name)
		}
	}

	if len(tagsToRead) == 0 {
		w.statsMu.Lock()
		w.tagsPolled = 0
		w.changesFound = 0
		w.lastError = nil
		w.statsMu.Unlock()
		return
	}

	// Read selected tags - this is the blocking I/O operation
	values, err := client.Read(tagsToRead...)
	if err != nil {
		plc.mu.Lock()
		plc.LastError = err
		plc.Status = StatusError
		plc.mu.Unlock()

		w.statsMu.Lock()
		w.tagsPolled = len(tagsToRead)
		w.changesFound = 0
		w.lastError = err
		w.statsMu.Unlock()

		w.manager.markStatusDirty()
		return
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

	w.statsMu.Lock()
	w.tagsPolled = len(tagsToRead)
	w.changesFound = len(changes)
	w.lastError = nil
	w.statsMu.Unlock()

	// Send changes to the manager's aggregator
	if len(changes) > 0 {
		w.manager.sendChanges(changes)
	}
	w.manager.markStatusDirty()
}

func (w *PLCWorker) checkAutoReconnect() {
	plc := w.plc

	plc.mu.RLock()
	status := plc.Status
	enabled := plc.Config.Enabled
	plc.mu.RUnlock()

	// Only auto-reconnect if enabled and currently disconnected or in error state
	if !enabled {
		return
	}
	if status == StatusConnected || status == StatusConnecting {
		return
	}

	// Attempt reconnection (runs in this worker's goroutine)
	w.manager.connectPLC(plc)
}

// Manager manages multiple PLC connections and polling.
type Manager struct {
	plcs    map[string]*ManagedPLC
	workers map[string]*PLCWorker
	mu      sync.RWMutex

	pollRate      time.Duration
	batchInterval time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Callbacks
	onChange      func()
	onValueChange func(changes []ValueChange)

	// Batched update channels
	changeChan  chan []ValueChange // Aggregates value changes from workers
	statusDirty int32              // Atomic flag: 1 if UI needs refresh

	// Aggregated stats
	lastPollStats PollStats
	statsMu       sync.RWMutex
}

// NewManager creates a new PLC manager.
func NewManager(pollRate time.Duration) *Manager {
	if pollRate <= 0 {
		pollRate = time.Second
	}
	return &Manager{
		plcs:          make(map[string]*ManagedPLC),
		workers:       make(map[string]*PLCWorker),
		pollRate:      pollRate,
		batchInterval: 100 * time.Millisecond, // Batch UI updates every 100ms
		changeChan:    make(chan []ValueChange, 100),
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

// markStatusDirty signals that the UI needs to be refreshed.
func (m *Manager) markStatusDirty() {
	atomic.StoreInt32(&m.statusDirty, 1)
}

// sendChanges sends value changes to the aggregator channel.
func (m *Manager) sendChanges(changes []ValueChange) {
	select {
	case m.changeChan <- changes:
	default:
		// Channel full, drop oldest and retry
		select {
		case <-m.changeChan:
		default:
		}
		select {
		case m.changeChan <- changes:
		default:
		}
	}
}

// AddPLC adds a PLC to management.
func (m *Manager) AddPLC(cfg *config.PLCConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.plcs[cfg.Name]; exists {
		return nil // Already exists
	}

	plc := &ManagedPLC{
		Config: cfg,
		Status: StatusDisconnected,
		Values: make(map[string]*logix.TagValue),
	}
	m.plcs[cfg.Name] = plc

	// If manager is running, start a worker for this PLC
	if m.ctx != nil {
		worker := newPLCWorker(plc, m, m.pollRate)
		m.workers[cfg.Name] = worker
		worker.Start()
	}

	return nil
}

// RemovePLC removes a PLC from management and disconnects it.
func (m *Manager) RemovePLC(name string) error {
	m.mu.Lock()
	plc, exists := m.plcs[name]
	worker := m.workers[name]
	if exists {
		delete(m.plcs, name)
		delete(m.workers, name)
	}
	m.mu.Unlock()

	// Stop worker first (outside lock)
	if worker != nil {
		worker.Stop()
	}

	if exists && plc.Client != nil {
		plc.Client.Close()
	}

	m.markStatusDirty()
	return nil
}

// connectPLC establishes a connection to a PLC (called from worker goroutine).
func (m *Manager) connectPLC(plc *ManagedPLC) error {
	plc.mu.Lock()
	plc.Status = StatusConnecting
	plc.LastError = nil
	plc.mu.Unlock()
	m.markStatusDirty()

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
		m.markStatusDirty()
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
	m.markStatusDirty()

	return nil
}

// Connect establishes a connection to the named PLC.
// This can be called from UI thread - runs connection in background.
func (m *Manager) Connect(name string) error {
	m.mu.RLock()
	plc, exists := m.plcs[name]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("PLC not found: %s", name)
	}

	// Run connection in a separate goroutine to not block UI
	go m.connectPLC(plc)
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
	m.markStatusDirty()

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

// Start begins background polling for all PLCs.
func (m *Manager) Start() {
	m.mu.Lock()
	if m.ctx != nil {
		m.mu.Unlock()
		return // Already running
	}
	m.ctx, m.cancel = context.WithCancel(context.Background())

	// Start workers for all existing PLCs
	for name, plc := range m.plcs {
		worker := newPLCWorker(plc, m, m.pollRate)
		m.workers[name] = worker
		worker.Start()
	}
	m.mu.Unlock()

	// Start the batched update loop
	m.wg.Add(1)
	go m.batchedUpdateLoop()

	// Start the stats aggregator
	m.wg.Add(1)
	go m.statsAggregatorLoop()
}

// Stop halts all background polling.
func (m *Manager) Stop() {
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
	}

	// Stop all workers
	workers := make([]*PLCWorker, 0, len(m.workers))
	for _, w := range m.workers {
		workers = append(workers, w)
	}
	m.workers = make(map[string]*PLCWorker)
	m.mu.Unlock()

	// Stop workers outside of lock
	for _, w := range workers {
		w.Stop()
	}

	m.wg.Wait()

	m.mu.Lock()
	m.ctx = nil
	m.cancel = nil
	m.mu.Unlock()
}

// batchedUpdateLoop aggregates changes and triggers UI updates at a controlled rate.
func (m *Manager) batchedUpdateLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.batchInterval)
	defer ticker.Stop()

	var pendingChanges []ValueChange

	for {
		select {
		case <-m.ctx.Done():
			// Flush any remaining changes
			if len(pendingChanges) > 0 {
				m.flushValueChanges(pendingChanges)
			}
			return

		case changes := <-m.changeChan:
			// Aggregate changes
			pendingChanges = append(pendingChanges, changes...)

		case <-ticker.C:
			// Check if status update is needed
			if atomic.CompareAndSwapInt32(&m.statusDirty, 1, 0) {
				m.mu.RLock()
				fn := m.onChange
				m.mu.RUnlock()
				if fn != nil {
					fn()
				}
			}

			// Flush pending value changes
			if len(pendingChanges) > 0 {
				m.flushValueChanges(pendingChanges)
				pendingChanges = nil
			}
		}
	}
}

// flushValueChanges calls the value change callback with accumulated changes.
func (m *Manager) flushValueChanges(changes []ValueChange) {
	m.mu.RLock()
	fn := m.onValueChange
	m.mu.RUnlock()
	if fn != nil && len(changes) > 0 {
		fn(changes)
	}
}

// statsAggregatorLoop periodically aggregates stats from all workers.
func (m *Manager) statsAggregatorLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.aggregateStats()
		}
	}
}

func (m *Manager) aggregateStats() {
	m.mu.RLock()
	workers := make([]*PLCWorker, 0, len(m.workers))
	for _, w := range m.workers {
		workers = append(workers, w)
	}
	m.mu.RUnlock()

	totalTags := 0
	totalChanges := 0
	var lastErr error

	for _, w := range workers {
		tags, changes, err := w.GetStats()
		totalTags += tags
		totalChanges += changes
		if err != nil {
			lastErr = err
		}
	}

	m.statsMu.Lock()
	m.lastPollStats = PollStats{
		LastPollTime: time.Now(),
		TagsPolled:   totalTags,
		ChangesFound: totalChanges,
		LastError:    lastErr,
	}
	m.statsMu.Unlock()
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
		go m.connectPLC(plc)
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

// GetPollStats returns the aggregated stats from all workers.
func (m *Manager) GetPollStats() PollStats {
	m.statsMu.RLock()
	defer m.statsMu.RUnlock()
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
