// Package plcman provides PLC connection management with background polling.
package plcman

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"warlogix/ads"
	"warlogix/config"
	"warlogix/logix"
	"warlogix/s7"
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

// MaxConnectRetries is the maximum number of connection attempts before giving up.
const MaxConnectRetries = 5

// ManagedPLC represents a PLC under management.
type ManagedPLC struct {
	Config       *config.PLCConfig
	Client       *logix.Client    // For Logix/Micro800 PLCs
	S7Client     *s7.Client       // For Siemens S7 PLCs
	AdsClient    *ads.Client      // For Beckhoff TwinCAT PLCs
	Identity     *logix.DeviceInfo
	S7Info       *s7.CPUInfo
	AdsInfo      *ads.DeviceInfo  // Beckhoff device info
	Programs     []string
	Tags         []logix.TagInfo  // Discovered tags (for discovery-capable PLCs)
	ManualTags   []logix.TagInfo  // Tags from config (for non-discovery PLCs)
	Values       map[string]*logix.TagValue
	Status       ConnectionStatus
	LastError    error
	LastPoll     time.Time
	ConnRetries  int  // Number of consecutive failed connection attempts
	RetryLimited bool // True if retry limit reached, stops auto-reconnect
	mu           sync.RWMutex
}

// GetStatus returns the current connection status thread-safely.
func (m *ManagedPLC) GetStatus() ConnectionStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.Status
}

// IsTagWritable returns whether a tag is configured as writable.
func (m *ManagedPLC) IsTagWritable(tagName string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.Config == nil {
		return false
	}
	for _, tag := range m.Config.Tags {
		if tag.Name == tagName {
			return tag.Writable
		}
	}
	return false
}

// GetTagInfo returns whether a tag exists and if it's writable, thread-safely.
// Returns (found, writable).
func (m *ManagedPLC) GetTagInfo(tagName string) (bool, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.Config == nil {
		return false, false
	}
	for _, tag := range m.Config.Tags {
		if tag.Name == tagName {
			return true, tag.Writable
		}
	}
	return false, false
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

// GetTags returns the appropriate tags based on PLC family.
// For discovery-capable PLCs (logix), returns discovered tags.
// For non-discovery PLCs (micro800, s7, omron), returns manual tags from config.
func (m *ManagedPLC) GetTags() []logix.TagInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.Config.GetFamily().SupportsDiscovery() {
		return m.Tags
	}
	return m.ManualTags
}

// BuildManualTags creates TagInfo entries from config.Tags for non-discovery PLCs.
func (m *ManagedPLC) BuildManualTags() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ManualTags = nil
	if m.Config == nil {
		return
	}

	family := m.Config.GetFamily()

	for _, sel := range m.Config.Tags {
		var typeCode uint16
		var ok bool

		// Use appropriate type lookup based on family
		switch family {
		case config.FamilyS7:
			typeCode, ok = s7.TypeCodeFromName(sel.DataType)
			if !ok {
				typeCode = s7.TypeDInt
			}
		case config.FamilyBeckhoff:
			typeCode, ok = ads.TypeCodeFromName(sel.DataType)
			if !ok {
				typeCode = ads.TypeInt32
			}
		default:
			typeCode, ok = logix.TypeCodeFromName(sel.DataType)
			if !ok {
				typeCode = logix.TypeDINT
			}
		}

		tagInfo := logix.TagInfo{
			Name:     sel.Name,
			TypeCode: typeCode,
			Instance: 0, // Not applicable for manual tags
		}
		m.ManualTags = append(m.ManualTags, tagInfo)
	}
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
	if m.AdsClient != nil {
		return m.AdsClient.ConnectionMode()
	}
	if m.S7Client != nil {
		return m.S7Client.ConnectionMode()
	}
	if m.Client != nil {
		return m.Client.ConnectionMode()
	}
	return "Not connected"
}

// ValueChange represents a tag value that has changed.
type ValueChange struct {
	PLCName  string
	TagName  string
	TypeName string
	Value    interface{}
	Writable bool
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

	plc.mu.RLock()
	client := plc.Client
	s7Client := plc.S7Client
	adsClient := plc.AdsClient
	status := plc.Status
	cfg := plc.Config
	plcName := cfg.Name
	family := cfg.GetFamily()
	// Copy the tags to read while holding the lock to avoid race with UI
	tagsToRead := make([]string, 0)
	writableMap := make(map[string]bool)
	for _, sel := range cfg.Tags {
		if sel.Enabled {
			tagsToRead = append(tagsToRead, sel.Name)
		}
		writableMap[sel.Name] = sel.Writable
	}
	oldValues := make(map[string]interface{})
	for k, v := range plc.Values {
		if v != nil && v.Error == nil {
			oldValues[k] = v.GoValue()
		}
	}
	plc.mu.RUnlock()

	// Check if we have a valid connection based on family
	var hasConnection bool
	switch family {
	case config.FamilyS7:
		hasConnection = s7Client != nil
	case config.FamilyBeckhoff:
		hasConnection = adsClient != nil
	default:
		hasConnection = client != nil
	}

	if status != StatusConnected || !hasConnection {
		w.statsMu.Lock()
		w.tagsPolled = 0
		w.changesFound = 0
		w.lastError = nil
		w.statsMu.Unlock()
		return
	}

	if len(tagsToRead) == 0 {
		w.statsMu.Lock()
		w.tagsPolled = 0
		w.changesFound = 0
		w.lastError = nil
		w.statsMu.Unlock()
		return
	}

	// Read selected tags based on family type
	var values []*logix.TagValue
	var err error

	switch family {
	case config.FamilyS7:
		values, err = w.pollS7(s7Client, tagsToRead)
	case config.FamilyBeckhoff:
		values, err = w.pollAds(adsClient, tagsToRead)
	default:
		values, err = client.Read(tagsToRead...)
	}

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
	// writableMap was already built above while holding the lock
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
					Writable: writableMap[v.Name],
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

// pollS7 reads tags from an S7 PLC and converts them to logix.TagValue format.
func (w *PLCWorker) pollS7(client *s7.Client, addresses []string) ([]*logix.TagValue, error) {
	s7Values, err := client.Read(addresses...)
	if err != nil {
		return nil, err
	}

	// Convert S7 TagValues to logix TagValues for compatibility
	results := make([]*logix.TagValue, len(s7Values))
	for i, sv := range s7Values {
		results[i] = &logix.TagValue{
			Name:     sv.Name,
			DataType: sv.DataType,
			Bytes:    sv.Bytes,
			Error:    sv.Error,
		}
	}
	return results, nil
}

// pollAds reads tags from a Beckhoff TwinCAT PLC and converts them to logix.TagValue format.
func (w *PLCWorker) pollAds(client *ads.Client, symbols []string) ([]*logix.TagValue, error) {
	adsValues, err := client.Read(symbols...)
	if err != nil {
		return nil, err
	}

	// Convert ADS TagValues to logix TagValues for compatibility
	results := make([]*logix.TagValue, len(adsValues))
	for i, av := range adsValues {
		results[i] = &logix.TagValue{
			Name:     av.Name,
			DataType: av.DataType,
			Bytes:    av.Bytes,
			Error:    av.Error,
		}
	}
	return results, nil
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
	onLog         func(format string, args ...interface{}) // Log callback for TUI integration

	// Batched update channels
	changeChan  chan []ValueChange // Aggregates value changes from workers
	statusDirty int32              // Atomic flag: 1 if UI needs refresh

	// Aggregated stats
	lastPollStats PollStats
	statsMu       sync.RWMutex

	// Track in-progress reconnections to prevent duplicates
	reconnecting   map[string]bool
	reconnectingMu sync.Mutex
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
		reconnecting:  make(map[string]bool),
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

// SetOnLog sets a callback for logging messages (for TUI integration).
func (m *Manager) SetOnLog(fn func(format string, args ...interface{})) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onLog = fn
}

// log calls the logging callback if set.
func (m *Manager) log(format string, args ...interface{}) {
	m.mu.RLock()
	fn := m.onLog
	m.mu.RUnlock()
	if fn != nil {
		fn(format, args...)
	}
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

	if exists {
		if plc.Client != nil {
			plc.Client.Close()
		}
		if plc.S7Client != nil {
			plc.S7Client.Close()
		}
		if plc.AdsClient != nil {
			plc.AdsClient.Close()
		}
	}

	m.markStatusDirty()
	return nil
}

// connectPLC establishes a connection to a PLC (called from worker goroutine).
func (m *Manager) connectPLC(plc *ManagedPLC) error {
	plc.mu.Lock()
	plc.Status = StatusConnecting
	plc.LastError = nil
	family := plc.Config.GetFamily()
	address := plc.Config.Address
	slot := plc.Config.Slot
	amsNetId := plc.Config.AmsNetId
	amsPort := plc.Config.AmsPort
	plc.mu.Unlock()
	m.markStatusDirty()

	// Handle family-specific connections
	switch family {
	case config.FamilyS7:
		return m.connectS7PLC(plc, address, int(slot))
	case config.FamilyBeckhoff:
		return m.connectBeckhoffPLC(plc, address, amsNetId, amsPort)
	}

	// Logix/Micro800 connection
	opts := []logix.Option{}
	if family == config.FamilyMicro800 {
		// Micro800 PLCs don't use backplane routing and need unconnected messaging
		opts = append(opts, logix.WithMicro800())
	} else if slot > 0 {
		opts = append(opts, logix.WithSlot(slot))
	}

	client, err := logix.Connect(address, opts...)
	if err != nil {
		plc.mu.Lock()
		plc.ConnRetries++
		if plc.ConnRetries >= MaxConnectRetries {
			plc.RetryLimited = true
			plc.Status = StatusDisconnected
			plc.LastError = fmt.Errorf("retry limit reached (%d attempts): %w", MaxConnectRetries, err)
		} else {
			plc.Status = StatusError
			plc.LastError = err
		}
		name := plc.Config.Name
		lastErr := plc.LastError
		plc.mu.Unlock()
		m.markStatusDirty()
		m.log("[red]PLC %s connection failed:[-] %v", name, lastErr)
		return err
	}

	// Get identity
	identity, _ := client.Identity()

	var programs []string
	var tags []logix.TagInfo

	// Only discover programs and tags for discovery-capable PLCs
	if family.SupportsDiscovery() {
		programs, _ = client.Programs()
		tags, _ = client.AllTags()
		// Store tags in client for element count lookup during reads
		// For Micro800, this also probes array sizes and returns updated tags
		tags = client.SetTags(tags)
	}

	plc.mu.Lock()
	plc.Client = client
	plc.Identity = identity
	plc.Programs = programs
	plc.Tags = tags
	plc.Status = StatusConnected
	plc.ConnRetries = 0     // Reset on successful connection
	plc.RetryLimited = false // Clear retry limit on success
	name := plc.Config.Name
	plc.mu.Unlock()

	// For non-discovery PLCs, build manual tags from config
	if !family.SupportsDiscovery() {
		plc.BuildManualTags()
	}

	m.markStatusDirty()
	m.log("[green]PLC %s connected:[-] %s, %d tags", name, client.ConnectionMode(), len(tags))

	return nil
}

// connectS7PLC handles S7 PLC connections.
func (m *Manager) connectS7PLC(plc *ManagedPLC, address string, slot int) error {
	// S7-1200/1500 typically use rack 0, slot 0 or 1
	// The slot in our config will be used as S7 slot
	rack := 0
	s7Slot := slot
	if s7Slot == 0 {
		s7Slot = 1 // Default to slot 1 for S7-300/400 compatibility
	}

	s7Client, err := s7.Connect(address, s7.WithRackSlot(rack, s7Slot))
	if err != nil {
		plc.mu.Lock()
		plc.ConnRetries++
		if plc.ConnRetries >= MaxConnectRetries {
			plc.RetryLimited = true
			plc.Status = StatusDisconnected
			plc.LastError = fmt.Errorf("retry limit reached (%d attempts): %w", MaxConnectRetries, err)
		} else {
			plc.Status = StatusError
			plc.LastError = err
		}
		name := plc.Config.Name
		lastErr := plc.LastError
		plc.mu.Unlock()
		m.markStatusDirty()
		m.log("[red]PLC %s (S7) connection failed:[-] %v", name, lastErr)
		return err
	}

	// Get CPU info
	cpuInfo, _ := s7Client.GetCPUInfo()

	plc.mu.Lock()
	plc.S7Client = s7Client
	plc.S7Info = cpuInfo
	plc.Status = StatusConnected
	plc.ConnRetries = 0
	plc.RetryLimited = false
	name := plc.Config.Name
	plc.mu.Unlock()

	// Build manual tags from config (S7 doesn't support discovery)
	plc.BuildManualTags()

	m.markStatusDirty()
	m.log("[green]PLC %s (S7) connected:[-] %s", name, s7Client.ConnectionMode())

	return nil
}

// connectBeckhoffPLC handles Beckhoff TwinCAT PLC connections via ADS protocol.
func (m *Manager) connectBeckhoffPLC(plc *ManagedPLC, address, amsNetId string, amsPort uint16) error {
	opts := []ads.Option{}
	if amsNetId != "" {
		opts = append(opts, ads.WithAmsNetId(amsNetId))
	}
	if amsPort > 0 {
		opts = append(opts, ads.WithAmsPort(amsPort))
	}

	adsClient, err := ads.Connect(address, opts...)
	if err != nil {
		plc.mu.Lock()
		plc.ConnRetries++
		if plc.ConnRetries >= MaxConnectRetries {
			plc.RetryLimited = true
			plc.Status = StatusDisconnected
			plc.LastError = fmt.Errorf("retry limit reached (%d attempts): %w", MaxConnectRetries, err)
		} else {
			plc.Status = StatusError
			plc.LastError = err
		}
		name := plc.Config.Name
		lastErr := plc.LastError
		plc.mu.Unlock()
		m.markStatusDirty()
		m.log("[red]PLC %s (Beckhoff) connection failed:[-] %v", name, lastErr)
		return err
	}

	// Get device info
	deviceInfo, _ := adsClient.GetDeviceInfo()

	// Discover all tags (Beckhoff supports symbol discovery)
	var tags []logix.TagInfo
	adsTags, err := adsClient.AllTags()
	if err == nil {
		// Convert ADS TagInfo to logix TagInfo for compatibility
		for _, at := range adsTags {
			tags = append(tags, logix.TagInfo{
				Name:     at.Name,
				TypeCode: at.TypeCode,
				Instance: 0,
			})
		}
	}

	// Get program names (POU prefixes)
	programs, _ := adsClient.Programs()

	plc.mu.Lock()
	plc.AdsClient = adsClient
	plc.AdsInfo = deviceInfo
	plc.Programs = programs
	plc.Tags = tags
	plc.Status = StatusConnected
	plc.ConnRetries = 0
	plc.RetryLimited = false
	name := plc.Config.Name
	plc.mu.Unlock()

	m.markStatusDirty()
	m.log("[green]PLC %s (Beckhoff) connected:[-] %s, %d tags", name, adsClient.ConnectionMode(), len(tags))

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

	// Reset retry state for manual connection attempts
	plc.mu.Lock()
	plc.ConnRetries = 0
	plc.RetryLimited = false
	plc.mu.Unlock()

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
	if plc.S7Client != nil {
		plc.S7Client.Close()
		plc.S7Client = nil
	}
	if plc.AdsClient != nil {
		plc.AdsClient.Close()
		plc.AdsClient = nil
	}
	plc.Status = StatusDisconnected
	plc.LastError = nil
	plc.Identity = nil
	plc.S7Info = nil
	plc.AdsInfo = nil
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

	// Start the reconnection watchdog
	m.wg.Add(1)
	go m.watchdogLoop()
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

// watchdogLoop periodically checks for disconnected PLCs and attempts reconnection.
// Runs every 1 minute for PLCs that have auto-connect enabled.
func (m *Manager) watchdogLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.checkReconnections()
		}
	}
}

// checkReconnections attempts to reconnect PLCs that are disconnected and have auto-connect enabled.
func (m *Manager) checkReconnections() {
	m.mu.RLock()
	plcs := make([]*ManagedPLC, 0, len(m.plcs))
	for _, plc := range m.plcs {
		plcs = append(plcs, plc)
	}
	m.mu.RUnlock()

	for _, plc := range plcs {
		plc.mu.RLock()
		status := plc.Status
		enabled := plc.Config.Enabled
		name := plc.Config.Name
		plc.mu.RUnlock()

		// Only attempt reconnection if:
		// - Auto-connect is enabled
		// - PLC is disconnected or in error state (not connected or connecting)
		if !enabled {
			continue
		}
		if status == StatusConnected || status == StatusConnecting {
			continue
		}

		// Check if reconnection is already in progress for this PLC
		m.reconnectingMu.Lock()
		if m.reconnecting[name] {
			m.reconnectingMu.Unlock()
			continue // Skip - reconnection already in progress
		}
		m.reconnecting[name] = true
		m.reconnectingMu.Unlock()

		// Attempt reconnection in a separate goroutine to not block the watchdog
		go func(p *ManagedPLC, n string) {
			defer func() {
				m.reconnectingMu.Lock()
				delete(m.reconnecting, n)
				m.reconnectingMu.Unlock()
			}()

			// Reset retry state before attempting reconnection
			p.mu.Lock()
			p.ConnRetries = 0
			p.RetryLimited = false
			p.mu.Unlock()

			m.connectPLC(p)
		}(plc, name)
	}
}

// ReadTag reads a single tag from a connected PLC.
func (m *Manager) ReadTag(plcName, tagName string) (*logix.TagValue, error) {
	m.mu.RLock()
	plc, exists := m.plcs[plcName]
	m.mu.RUnlock()

	if !exists {
		return nil, nil
	}

	plc.mu.RLock()
	client := plc.Client
	s7Client := plc.S7Client
	adsClient := plc.AdsClient
	family := plc.Config.GetFamily()
	plc.mu.RUnlock()

	switch family {
	case config.FamilyS7:
		if s7Client == nil {
			return nil, nil
		}
		s7Values, err := s7Client.Read(tagName)
		if err != nil {
			return nil, err
		}
		if len(s7Values) > 0 {
			return &logix.TagValue{
				Name:     s7Values[0].Name,
				DataType: s7Values[0].DataType,
				Bytes:    s7Values[0].Bytes,
				Error:    s7Values[0].Error,
			}, nil
		}
		return nil, nil

	case config.FamilyBeckhoff:
		if adsClient == nil {
			return nil, nil
		}
		adsValues, err := adsClient.Read(tagName)
		if err != nil {
			return nil, err
		}
		if len(adsValues) > 0 {
			return &logix.TagValue{
				Name:     adsValues[0].Name,
				DataType: adsValues[0].DataType,
				Bytes:    adsValues[0].Bytes,
				Error:    adsValues[0].Error,
			}, nil
		}
		return nil, nil

	default:
		// Handle Logix/Micro800 PLCs
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
	s7Client := plc.S7Client
	adsClient := plc.AdsClient
	status := plc.Status
	family := plc.Config.GetFamily()
	plc.mu.RUnlock()

	if status != StatusConnected {
		return fmt.Errorf("PLC not connected: %s", plcName)
	}

	switch family {
	case config.FamilyS7:
		if s7Client == nil {
			return fmt.Errorf("S7 PLC not connected: %s", plcName)
		}
		return s7Client.Write(tagName, value)

	case config.FamilyBeckhoff:
		if adsClient == nil {
			return fmt.Errorf("Beckhoff PLC not connected: %s", plcName)
		}
		return adsClient.Write(tagName, value)

	default:
		// Handle Logix/Micro800 PLCs
		if client == nil {
			return fmt.Errorf("PLC not connected: %s", plcName)
		}
		return client.Write(tagName, value)
	}
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
		// Build writable lookup map from config
		writableMap := make(map[string]bool)
		for _, tag := range plc.Config.Tags {
			writableMap[tag.Name] = tag.Writable
		}
		for tagName, val := range plc.Values {
			if val != nil && val.Error == nil {
				results = append(results, ValueChange{
					PLCName:  plcName,
					TagName:  tagName,
					TypeName: val.TypeName(),
					Value:    val.GoValue(),
					Writable: writableMap[tagName],
				})
			}
		}
		plc.mu.RUnlock()
	}
	return results
}

// RefreshManualTags rebuilds manual tags from config for a specific PLC.
// Called after UI adds/removes tags for non-discovery PLCs.
func (m *Manager) RefreshManualTags(name string) {
	m.mu.RLock()
	plc, exists := m.plcs[name]
	m.mu.RUnlock()

	if !exists || plc == nil {
		return
	}

	// Only rebuild for non-discovery PLCs
	if !plc.Config.GetFamily().SupportsDiscovery() {
		plc.BuildManualTags()
		m.markStatusDirty()
	}
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

// ReadTagValue reads a single tag and returns its Go value.
// This implements the trigger.TagReader interface.
func (m *Manager) ReadTagValue(plcName, tagName string) (interface{}, error) {
	val, err := m.ReadTag(plcName, tagName)
	if err != nil {
		return nil, err
	}
	if val == nil {
		return nil, fmt.Errorf("tag not found: %s", tagName)
	}
	if val.Error != nil {
		return nil, val.Error
	}
	return val.GoValue(), nil
}

// ReadTagValues reads multiple tags and returns their Go values.
// This implements the trigger.TagReader interface.
func (m *Manager) ReadTagValues(plcName string, tagNames []string) (map[string]interface{}, error) {
	m.mu.RLock()
	plc, exists := m.plcs[plcName]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("PLC not found: %s", plcName)
	}

	plc.mu.RLock()
	client := plc.Client
	s7Client := plc.S7Client
	adsClient := plc.AdsClient
	status := plc.Status
	family := plc.Config.GetFamily()
	plc.mu.RUnlock()

	if status != StatusConnected {
		return nil, fmt.Errorf("PLC not connected: %s", plcName)
	}

	result := make(map[string]interface{})

	switch family {
	case config.FamilyS7:
		if s7Client == nil {
			return nil, fmt.Errorf("S7 client not available")
		}
		s7Values, err := s7Client.Read(tagNames...)
		if err != nil {
			return nil, err
		}
		for _, v := range s7Values {
			if v.Error == nil {
				result[v.Name] = v.GoValue()
			} else {
				result[v.Name] = nil
			}
		}
		return result, nil

	case config.FamilyBeckhoff:
		if adsClient == nil {
			return nil, fmt.Errorf("ADS client not available")
		}
		adsValues, err := adsClient.Read(tagNames...)
		if err != nil {
			return nil, err
		}
		for _, v := range adsValues {
			if v.Error == nil {
				result[v.Name] = v.GoValue()
			} else {
				result[v.Name] = nil
			}
		}
		return result, nil

	default:
		// Handle Logix/Micro800 PLCs
		if client == nil {
			return nil, fmt.Errorf("client not available")
		}
		values, err := client.Read(tagNames...)
		if err != nil {
			return nil, err
		}
		for _, v := range values {
			if v.Error == nil {
				result[v.Name] = v.GoValue()
			} else {
				result[v.Name] = nil
			}
		}
		return result, nil
	}
}

// TriggerTagReader wraps the Manager to implement the trigger.TagReader interface.
type TriggerTagReader struct {
	Manager *Manager
}

// ReadTag implements trigger.TagReader.
func (r *TriggerTagReader) ReadTag(plcName, tagName string) (interface{}, error) {
	return r.Manager.ReadTagValue(plcName, tagName)
}

// ReadTags implements trigger.TagReader.
func (r *TriggerTagReader) ReadTags(plcName string, tagNames []string) (map[string]interface{}, error) {
	return r.Manager.ReadTagValues(plcName, tagNames)
}

// TriggerTagWriter wraps the Manager to implement the trigger.TagWriter interface.
type TriggerTagWriter struct {
	Manager *Manager
}

// WriteTag implements trigger.TagWriter.
func (w *TriggerTagWriter) WriteTag(plcName, tagName string, value interface{}) error {
	return w.Manager.WriteTag(plcName, tagName, value)
}
