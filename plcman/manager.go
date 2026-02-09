// Package plcman provides PLC connection management with background polling.
package plcman

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"warlogix/ads"
	"warlogix/config"
	"warlogix/driver"
	"warlogix/logging"
	"warlogix/logix"
	"warlogix/omron"
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

// HealthStatus represents the health state of a PLC for publishing.
type HealthStatus struct {
	Driver    string    `json:"driver"`
	Online    bool      `json:"online"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// MaxConnectRetries is the maximum number of connection attempts before giving up.
const MaxConnectRetries = 5

// ManagedPLC represents a PLC under management.
type ManagedPLC struct {
	Config     *config.PLCConfig
	Driver     driver.Driver        // Unified driver interface
	DeviceInfo *driver.DeviceInfo   // Unified device info
	Programs   []string             // Program names (for Logix)
	Tags       []driver.TagInfo     // Discovered tags (for discovery-capable PLCs)
	ManualTags []driver.TagInfo     // Tags from config (for non-discovery PLCs)
	Values     map[string]*TagValue // Tag values from last poll
	Status     ConnectionStatus
	LastError  error
	LastPoll   time.Time
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

// GetHealthStatus returns the current health status for publishing.
func (m *ManagedPLC) GetHealthStatus() HealthStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	health := HealthStatus{
		Timestamp: time.Now().UTC(),
	}

	// Set driver from config
	if m.Config != nil {
		health.Driver = m.Config.GetFamily().Driver()
	}

	// Check if PLC is enabled
	if m.Config != nil && !m.Config.Enabled {
		health.Online = false
		health.Status = "disabled"
		return health
	}

	// Map connection status to health
	switch m.Status {
	case StatusConnected:
		health.Online = true
		health.Status = "connected"
	case StatusConnecting:
		health.Online = false
		health.Status = "connecting"
	case StatusDisconnected:
		health.Online = false
		health.Status = "disconnected"
	case StatusError:
		health.Online = false
		health.Status = "error"
	default:
		health.Online = false
		health.Status = "unknown"
	}

	// Include error message if present
	if m.LastError != nil {
		health.Error = m.LastError.Error()
	} else if !health.Online && health.Status != "disabled" && health.Status != "connecting" {
		health.Error = "unknown error"
	}

	return health
}

// GetValues returns a copy of the current tag values.
func (m *ManagedPLC) GetValues() map[string]*TagValue {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]*TagValue, len(m.Values))
	for k, v := range m.Values {
		result[k] = v
	}
	return result
}

// GetTags returns the appropriate tags based on PLC family.
// For discovery-capable PLCs (logix), returns discovered tags.
// For non-discovery PLCs (micro800, s7, omron), returns manual tags from config.
func (m *ManagedPLC) GetTags() []driver.TagInfo {
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
		var typeName string
		var dimensions []uint32
		var ok bool

		// Use appropriate type lookup based on family
		switch family {
		case config.FamilyS7:
			typeCode, ok = s7.TypeCodeFromName(sel.DataType)
			if !ok {
				typeCode = s7.TypeDInt
			}
			typeName = s7.TypeName(typeCode)
			// Parse S7 address for array dimensions
			if parsed, err := s7.ParseAddress(sel.Name); err == nil && parsed.Count > 1 {
				dimensions = []uint32{uint32(parsed.Count)}
				typeCode = s7.MakeArrayType(typeCode)
			}
		case config.FamilyBeckhoff:
			typeCode, ok = ads.TypeCodeFromName(sel.DataType)
			if !ok {
				typeCode = ads.TypeInt32
			}
			typeName = ads.TypeName(typeCode)
		case config.FamilyOmron:
			typeCode, ok = omron.TypeCodeFromName(sel.DataType)
			if !ok {
				typeCode = omron.TypeWord // Default to WORD
			}
			typeName = omron.TypeName(typeCode)
			// Parse Omron address for array dimensions
			if parsed, err := omron.ParseAddress(sel.Name); err == nil && parsed.Count > 1 {
				dimensions = []uint32{uint32(parsed.Count)}
				typeCode = omron.MakeArrayType(typeCode)
			}
		default:
			typeCode, ok = logix.TypeCodeFromName(sel.DataType)
			if !ok {
				typeCode = logix.TypeDINT
			}
			typeName = logix.TypeName(typeCode)
		}

		tagInfo := driver.TagInfo{
			Name:       sel.Name,
			TypeCode:   typeCode,
			TypeName:   typeName,
			Writable:   sel.Writable,
			Dimensions: dimensions,
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

// GetDeviceInfo returns the device information.
func (m *ManagedPLC) GetDeviceInfo() *driver.DeviceInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.DeviceInfo
}

// GetDriver returns the underlying driver.
func (m *ManagedPLC) GetDriver() driver.Driver {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.Driver
}

// GetLogixClient returns the underlying Logix client, or nil if not available.
// Used for accessing client-specific features like GetElementSize and UDT templates.
func (m *ManagedPLC) GetLogixClient() *logix.Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.Driver == nil {
		return nil
	}
	if adapter, ok := m.Driver.(*driver.LogixAdapter); ok {
		return adapter.Client()
	}
	return nil
}

// GetConnectionMode returns a human-readable string describing the connection mode.
func (m *ManagedPLC) GetConnectionMode() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.Driver != nil {
		return m.Driver.ConnectionMode()
	}
	return "Not connected"
}

// ValueChange represents a tag value that has changed.
type ValueChange struct {
	PLCName  string
	TagName  string      // For S7: the address (e.g., "DB1.8"); for others: the tag name
	Alias    string      // User-defined alias/name (especially useful for S7)
	Address  string      // For S7: the address in uppercase; empty for other families
	TypeName string
	Value    interface{}
	Writable bool
	Family   string      // PLC family ("s7", "logix", "beckhoff", etc.)
	// Service inhibit flags - when true, don't publish to that service
	NoREST   bool
	NoMQTT   bool
	NoKafka  bool
	NoValkey bool
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
	drv := plc.Driver
	status := plc.Status
	cfg := plc.Config
	plcName := cfg.Name
	family := cfg.GetFamily()
	// Copy the tags to read while holding the lock to avoid race with UI
	tagsToRead := make([]string, 0)
	writableMap := make(map[string]bool)
	aliasMap := make(map[string]string)      // Tag name -> alias
	typeMap := make(map[string]string)       // Tag name -> configured data type
	ignoreMap := make(map[string][]string)   // Tag name -> list of members to ignore for change detection
	// Service inhibit maps
	noRESTMap := make(map[string]bool)
	noMQTTMap := make(map[string]bool)
	noKafkaMap := make(map[string]bool)
	noValkeyMap := make(map[string]bool)

	// For S7 family, normalize keys to uppercase since S7 addresses are case-insensitive
	normalizeKey := func(s string) string {
		if family == config.FamilyS7 {
			return strings.ToUpper(s)
		}
		return s
	}

	for _, sel := range cfg.Tags {
		if sel.Enabled {
			tagsToRead = append(tagsToRead, sel.Name)
		}
		key := normalizeKey(sel.Name)
		writableMap[key] = sel.Writable
		aliasMap[key] = sel.Alias
		noRESTMap[key] = sel.NoREST
		noMQTTMap[key] = sel.NoMQTT
		noKafkaMap[key] = sel.NoKafka
		noValkeyMap[key] = sel.NoValkey
		if sel.DataType != "" {
			typeMap[sel.Name] = sel.DataType // typeMap uses original name for driver
		}
		if len(sel.IgnoreChanges) > 0 {
			ignoreMap[key] = sel.IgnoreChanges
		}
	}
	oldStableValues := make(map[string]interface{})
	for k, v := range plc.Values {
		if v != nil && v.Error == nil {
			oldStableValues[k] = v.StableValue
		}
	}
	plc.mu.RUnlock()

	// Check if we have a valid connection via Driver
	hasConnection := drv != nil && drv.IsConnected()

	if status != StatusConnected || !hasConnection {
		// Check if auto-connect is enabled and we should attempt reconnection
		plc.mu.RLock()
		autoConnect := plc.Config.Enabled
		plc.mu.RUnlock()

		needsReconnect := autoConnect && (status == StatusDisconnected || status == StatusError)

		if needsReconnect || (drv != nil && !drv.IsConnected()) {
			plc.mu.Lock()
			plc.Status = StatusDisconnected
			if plc.Driver != nil {
				plc.Driver.Close()
				plc.Driver = nil
			}
			plc.mu.Unlock()
			w.manager.markStatusDirty()
			go w.manager.scheduleReconnect(plcName)
		}
		w.statsMu.Lock()
		w.tagsPolled = 0
		w.changesFound = 0
		w.lastError = nil
		w.statsMu.Unlock()
		return
	}

	if len(tagsToRead) == 0 {
		// No tags to poll, but send keepalive to maintain connection
		if drv != nil {
			_ = drv.Keepalive()
		}
		w.statsMu.Lock()
		w.tagsPolled = 0
		w.changesFound = 0
		w.lastError = nil
		w.statsMu.Unlock()
		return
	}

	// Build read requests with type hints
	requests := make([]driver.TagRequest, len(tagsToRead))
	for i, name := range tagsToRead {
		requests[i] = driver.TagRequest{
			Name:     name,
			TypeHint: typeMap[name],
		}
	}

	// Read via Driver
	driverValues, err := drv.Read(requests)

	// Convert driver values to TagValue and apply ignore lists
	var values []*TagValue
	if err == nil {
		values = make([]*TagValue, len(driverValues))
		for i, dv := range driverValues {
			values[i] = FromDriverTagValue(dv)
			if ignoreList, ok := ignoreMap[normalizeKey(dv.Name)]; ok {
				values[i].SetIgnoreList(ignoreList)
			}
		}
	}

	if err != nil {
		plc.mu.Lock()
		plc.LastError = err
		autoConnect := plc.Config.Enabled

		// Check if driver detected disconnection
		clientDisconnected := drv != nil && !drv.IsConnected()

		// Set status based on whether driver detected disconnection
		if clientDisconnected {
			plc.Status = StatusDisconnected
			if plc.Driver != nil {
				plc.Driver.Close()
				plc.Driver = nil
			}
			logging.DebugLog("plcman", "POLL %s: read error, driver disconnected: %v", plcName, err)
		} else {
			plc.Status = StatusError
			logging.DebugLog("plcman", "POLL %s: read error (driver still connected): %v", plcName, err)
		}

		plcNameForLog := plc.Config.Name
		plc.mu.Unlock()

		w.statsMu.Lock()
		w.tagsPolled = len(tagsToRead)
		w.changesFound = 0
		w.lastError = err
		w.statsMu.Unlock()

		w.manager.markStatusDirty()

		// Schedule reconnection if auto-connect is enabled and connection error detected
		shouldReconnect := autoConnect && (clientDisconnected || drv.IsConnectionError(err))

		if shouldReconnect {
			logging.DebugLog("plcman", "POLL %s: scheduling reconnect after error", plcNameForLog)
			w.manager.log("[yellow]PLC %s connection lost, scheduling reconnect[-]", plcNameForLog)
			go w.manager.scheduleReconnect(plcNameForLog)
		}

		return
	}

	// Detect changes and update values
	var changes []ValueChange
	plc.mu.Lock()
	// writableMap was already built above while holding the lock
	for _, v := range values {
		if v.Error == nil {
			newVal := v.GoValue()
			// Use StableValue for change detection (excludes ignored members)
			newStableVal := v.StableValue
			oldStableVal, existed := oldStableValues[v.Name]
			// Check if stable value changed (or is new)
			if !existed || fmt.Sprintf("%v", oldStableVal) != fmt.Sprintf("%v", newStableVal) {
				// Use normalized key for map lookups (S7 addresses are case-insensitive)
				lookupKey := normalizeKey(v.Name)
				vc := ValueChange{
					PLCName:  plcName,
					TagName:  v.Name,
					Alias:    aliasMap[lookupKey],
					TypeName: v.TypeName(),
					Value:    newVal,
					Writable: writableMap[lookupKey],
					Family:   string(family),
					NoREST:   noRESTMap[lookupKey],
					NoMQTT:   noMQTTMap[lookupKey],
					NoKafka:  noKafkaMap[lookupKey],
					NoValkey: noValkeyMap[lookupKey],
				}
				// For S7/Omron, set Address to uppercase version of TagName for troubleshooting
				if family == config.FamilyS7 || family == config.FamilyOmron {
					vc.Address = strings.ToUpper(v.Name)
				}
				changes = append(changes, vc)
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


// isLikelyConnectionError checks if an error message suggests a connection problem.
// This is used to trigger reconnection attempts when the client's internal detection
// might not have caught the error (e.g., protocol-level errors vs TCP errors).
func isLikelyConnectionError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	// Common connection-related error patterns
	return strings.Contains(errStr, "connection") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "reset by peer") ||
		strings.Contains(errStr, "eof") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "refused") ||
		strings.Contains(errStr, "closed") ||
		strings.Contains(errStr, "not connected") ||
		strings.Contains(errStr, "nil client") ||
		strings.Contains(errStr, "dial")
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
		Values: make(map[string]*TagValue),
	}
	m.plcs[cfg.Name] = plc

	// If manager is running, start a worker for this PLC
	if m.ctx != nil {
		pollRate := m.getEffectivePollRate(cfg)
		worker := newPLCWorker(plc, m, pollRate)
		m.workers[cfg.Name] = worker
		worker.Start()
	}

	return nil
}

// Polling rate limits
const (
	MinPollRate = 250 * time.Millisecond  // Minimum allowed poll rate
	MaxPollRate = 10000 * time.Millisecond // Maximum allowed poll rate (10 seconds)
)

// getEffectivePollRate returns the poll rate for a PLC.
// Uses the PLC's configured rate if set, otherwise falls back to the global rate.
// Enforces minimum of 250ms to protect PLC from excessive polling.
func (m *Manager) getEffectivePollRate(cfg *config.PLCConfig) time.Duration {
	rate := m.pollRate // Start with global default
	if cfg.PollRate > 0 {
		rate = cfg.PollRate
	}

	// Enforce minimum poll rate to protect PLC
	if rate < MinPollRate {
		rate = MinPollRate
	}

	return rate
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

	if exists && plc.Driver != nil {
		plc.Driver.Close()
	}

	m.markStatusDirty()
	return nil
}

// connectPLC establishes a connection to a PLC using the unified driver interface.
func (m *Manager) connectPLC(plc *ManagedPLC) error {
	plc.mu.Lock()
	plc.Status = StatusConnecting
	plc.LastError = nil
	cfg := plc.Config
	family := cfg.GetFamily()
	plcName := cfg.Name
	plc.mu.Unlock()
	m.markStatusDirty()

	logging.DebugLog("plcman", "CONNECT %s: starting connection (family=%s address=%s)",
		plcName, family, cfg.Address)

	// Create driver from config
	drv, err := driver.Create(cfg)
	if err != nil {
		logging.DebugLog("plcman", "CONNECT %s: driver creation failed: %v", plcName, err)
		plc.mu.Lock()
		plc.Status = StatusError
		plc.LastError = err
		plc.mu.Unlock()
		m.markStatusDirty()
		return err
	}

	logging.DebugLog("plcman", "CONNECT %s: driver created, attempting connection", plcName)

	// Connect via driver
	if err := drv.Connect(); err != nil {
		plc.mu.Lock()
		plc.ConnRetries++
		retryCount := plc.ConnRetries
		if plc.ConnRetries >= MaxConnectRetries {
			plc.RetryLimited = true
			plc.Status = StatusDisconnected
			plc.LastError = fmt.Errorf("retry limit reached (%d attempts): %w", MaxConnectRetries, err)
			logging.DebugLog("plcman", "CONNECT %s: FAILED - retry limit reached (%d/%d): %v",
				plcName, retryCount, MaxConnectRetries, err)
		} else {
			plc.Status = StatusError
			plc.LastError = err
			logging.DebugLog("plcman", "CONNECT %s: FAILED attempt %d/%d: %v",
				plcName, retryCount, MaxConnectRetries, err)
		}
		name := plc.Config.Name
		lastErr := plc.LastError
		plc.mu.Unlock()
		m.markStatusDirty()
		m.log("[red]PLC %s connection failed:[-] %v", name, lastErr)
		return err
	}

	logging.DebugLog("plcman", "CONNECT %s: connection established, mode=%s", plcName, drv.ConnectionMode())

	// Get device info
	deviceInfo, _ := drv.GetDeviceInfo()
	if deviceInfo != nil {
		logging.DebugLog("plcman", "CONNECT %s: device info - vendor=%s model=%s version=%s serial=%s",
			plcName, deviceInfo.Vendor, deviceInfo.Model, deviceInfo.Version, deviceInfo.SerialNumber)
	}

	// Check if we have cached tags from a previous connection
	plc.mu.RLock()
	cachedTags := plc.Tags
	cachedPrograms := plc.Programs
	plc.mu.RUnlock()

	var programs []string
	var tags []driver.TagInfo

	// Only discover programs and tags for discovery-capable PLCs
	if drv.SupportsDiscovery() {
		if len(cachedTags) > 0 {
			// Fast reconnect: reuse cached tags
			programs = cachedPrograms
			tags = cachedTags
			logging.DebugLog("plcman", "CONNECT %s: using cached tags (%d tags, %d programs)",
				plcName, len(tags), len(programs))
			m.log("[cyan]PLC %s using cached tags (%d tags)[-]", plc.Config.Name, len(tags))
		} else {
			// Full discovery via driver
			logging.DebugLog("plcman", "CONNECT %s: starting tag discovery", plcName)
			programs, _ = drv.Programs()
			tags, _ = drv.AllTags()
			logging.DebugLog("plcman", "CONNECT %s: discovered %d programs, %d tags",
				plcName, len(programs), len(tags))
		}
	}

	// For Logix adapters, store tags in client for element count lookup
	if family == config.FamilyLogix || family == config.FamilyMicro800 {
		if adapter, ok := drv.(*driver.LogixAdapter); ok {
			// Use SetTags to update array dimensions
			tags = adapter.SetTags(tags)
		}
	}

	// Update PLC state
	plc.mu.Lock()
	plc.Driver = drv
	plc.DeviceInfo = deviceInfo
	plc.Programs = programs
	plc.Tags = tags
	plc.Status = StatusConnected
	plc.ConnRetries = 0
	plc.RetryLimited = false
	name := plc.Config.Name
	plc.mu.Unlock()

	// For non-discovery PLCs, build manual tags from config
	if !drv.SupportsDiscovery() {
		plc.BuildManualTags()
	}

	m.markStatusDirty()
	m.log("[green]PLC %s connected:[-] %s, %d tags", name, drv.ConnectionMode(), len(tags))

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
		logging.DebugLog("plcman", "DISCONNECT %s: PLC not found", name)
		return nil
	}

	logging.DebugLog("plcman", "DISCONNECT %s: closing connection", name)

	plc.mu.Lock()
	if plc.Driver != nil {
		plc.Driver.Close()
		plc.Driver = nil
	}
	plc.Status = StatusDisconnected
	plc.LastError = nil
	plc.DeviceInfo = nil
	plc.mu.Unlock()
	m.markStatusDirty()

	logging.DebugLog("plcman", "DISCONNECT %s: connection closed", name)
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
		logging.DebugLog("plcman", "START: already running, ignoring")
		return // Already running
	}
	m.ctx, m.cancel = context.WithCancel(context.Background())

	logging.DebugLog("plcman", "START: initializing manager with %d PLCs", len(m.plcs))

	// Start workers for all existing PLCs
	for name, plc := range m.plcs {
		pollRate := m.getEffectivePollRate(plc.Config)
		worker := newPLCWorker(plc, m, pollRate)
		m.workers[name] = worker
		worker.Start()
		logging.DebugLog("plcman", "START: started worker for %s (poll_rate=%v)", name, pollRate)
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

	logging.DebugLog("plcman", "START: manager started successfully")
}

// Stop halts all background polling.
func (m *Manager) Stop() {
	logging.DebugLog("plcman", "STOP: shutting down manager")

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

	logging.DebugLog("plcman", "STOP: stopping %d workers", len(workers))

	// Stop workers outside of lock with timeout
	// Workers may be blocked on PLC reads, so don't wait forever
	workersDone := make(chan struct{})
	go func() {
		for _, w := range workers {
			w.Stop()
		}
		close(workersDone)
	}()
	select {
	case <-workersDone:
		logging.DebugLog("plcman", "STOP: all workers stopped")
	case <-time.After(500 * time.Millisecond):
		logging.DebugLog("plcman", "STOP: worker shutdown timeout, proceeding")
	}

	// Wait for manager goroutines with timeout
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		logging.DebugLog("plcman", "STOP: manager goroutines completed")
	case <-time.After(500 * time.Millisecond):
		logging.DebugLog("plcman", "STOP: manager goroutine timeout, proceeding")
	}

	m.mu.Lock()
	m.ctx = nil
	m.cancel = nil
	m.mu.Unlock()

	logging.DebugLog("plcman", "STOP: manager stopped")
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

	logging.DebugLog("plcman", "WATCHDOG: checking %d PLCs for reconnection", len(plcs))

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
			logging.DebugLog("plcman", "WATCHDOG %s: skipped - reconnection already in progress", name)
			continue // Skip - reconnection already in progress
		}
		m.reconnecting[name] = true
		m.reconnectingMu.Unlock()

		logging.DebugLog("plcman", "WATCHDOG %s: scheduling reconnection (status=%s)", name, status)

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

// scheduleReconnect schedules a reconnection attempt for a PLC after a short delay.
// This is called when a connection error is detected during polling.
func (m *Manager) scheduleReconnect(name string) {
	logging.DebugLog("plcman", "RECONNECT %s: scheduled, waiting 2s before attempt", name)
	// Wait a short time before attempting reconnection to avoid rapid retries
	time.Sleep(2 * time.Second)

	m.mu.RLock()
	plc, exists := m.plcs[name]
	m.mu.RUnlock()

	if !exists {
		logging.DebugLog("plcman", "RECONNECT %s: cancelled - PLC no longer exists", name)
		return
	}

	plc.mu.RLock()
	status := plc.Status
	enabled := plc.Config.Enabled
	plc.mu.RUnlock()

	// Only reconnect if still disconnected/error and enabled
	if !enabled || status == StatusConnected || status == StatusConnecting {
		logging.DebugLog("plcman", "RECONNECT %s: skipped - enabled=%v status=%s", name, enabled, status)
		return
	}

	// Check if reconnection is already in progress
	m.reconnectingMu.Lock()
	if m.reconnecting[name] {
		m.reconnectingMu.Unlock()
		logging.DebugLog("plcman", "RECONNECT %s: skipped - already in progress", name)
		return
	}
	m.reconnecting[name] = true
	m.reconnectingMu.Unlock()

	defer func() {
		m.reconnectingMu.Lock()
		delete(m.reconnecting, name)
		m.reconnectingMu.Unlock()
	}()

	// Reset retry state and attempt reconnection
	plc.mu.Lock()
	plc.ConnRetries = 0
	plc.RetryLimited = false
	plc.mu.Unlock()

	logging.DebugLog("plcman", "RECONNECT %s: attempting reconnection", name)
	m.connectPLC(plc)
}

// ReadTag reads a single tag from a connected PLC.
func (m *Manager) ReadTag(plcName, tagName string) (*TagValue, error) {
	m.mu.RLock()
	plc, exists := m.plcs[plcName]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("PLC not found: %s", plcName)
	}

	plc.mu.RLock()
	drv := plc.Driver
	status := plc.Status
	// Get configured type for this tag
	var typeHint string
	for _, sel := range plc.Config.Tags {
		if sel.Name == tagName && sel.DataType != "" {
			typeHint = sel.DataType
			break
		}
	}
	plc.mu.RUnlock()

	// Check connection status first
	if status != StatusConnected || drv == nil {
		return nil, fmt.Errorf("PLC not connected: %s (status: %s)", plcName, status)
	}

	// Read via driver
	requests := []driver.TagRequest{{Name: tagName, TypeHint: typeHint}}
	values, err := drv.Read(requests)
	if err != nil {
		if drv.IsConnectionError(err) {
			m.handleConnectionError(plcName, plc, err)
		}
		return nil, err
	}
	if len(values) > 0 && values[0] != nil {
		return FromDriverTagValue(values[0]), nil
	}
	return nil, fmt.Errorf("no data returned for tag: %s", tagName)
}

// ReadTagWithCount reads a single tag with a specified element count from a connected PLC.
// This is useful for reading arrays where you know the exact element count.
// Note: Count is only supported for Logix PLCs; others use regular ReadTag.
func (m *Manager) ReadTagWithCount(plcName, tagName string, count uint16) (*TagValue, error) {
	m.mu.RLock()
	plc, exists := m.plcs[plcName]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("PLC not found: %s", plcName)
	}

	plc.mu.RLock()
	drv := plc.Driver
	status := plc.Status
	family := plc.Config.GetFamily()
	plc.mu.RUnlock()

	// Check connection status first
	if status != StatusConnected || drv == nil {
		return nil, fmt.Errorf("PLC not connected: %s (status: %s)", plcName, status)
	}

	// For Logix, we can use the underlying client for count-based reads
	if family == config.FamilyLogix || family == config.FamilyMicro800 {
		if adapter, ok := drv.(*driver.LogixAdapter); ok {
			client := adapter.Client()
			if client != nil {
				value, err := client.ReadWithCount(tagName, count)
				if err != nil {
					if drv.IsConnectionError(err) {
						m.handleConnectionError(plcName, plc, err)
					}
					return nil, err
				}
				return FromLogixTagValueDecoded(value, client), nil
			}
		}
	}

	// For other families, fall back to regular ReadTag
	return m.ReadTag(plcName, tagName)
}

// handleConnectionError marks a PLC as disconnected and schedules reconnection.
func (m *Manager) handleConnectionError(plcName string, plc *ManagedPLC, err error) {
	plc.mu.Lock()
	wasConnected := plc.Status == StatusConnected
	plc.Status = StatusDisconnected
	autoConnect := plc.Config.Enabled
	plc.mu.Unlock()

	logging.DebugLog("plcman", "ERROR %s: connection error (wasConnected=%v autoConnect=%v): %v",
		plcName, wasConnected, autoConnect, err)

	if wasConnected {
		m.log("[yellow]PLC %s connection error: %v[-]", plcName, err)
		m.markStatusDirty()

		// Schedule reconnection if auto-connect is enabled
		if autoConnect {
			logging.DebugLog("plcman", "ERROR %s: scheduling reconnection", plcName)
			go m.scheduleReconnect(plcName)
		}
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
	drv := plc.Driver
	status := plc.Status
	plc.mu.RUnlock()

	if status != StatusConnected || drv == nil {
		return fmt.Errorf("PLC not connected: %s", plcName)
	}

	return drv.Write(tagName, value)
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

// GetTagValueChange returns a single tag's current value as a ValueChange.
// Returns nil if the tag is not found or has an error.
func (m *Manager) GetTagValueChange(plcName, tagName string) *ValueChange {
	m.mu.RLock()
	plc, exists := m.plcs[plcName]
	m.mu.RUnlock()

	if !exists || plc == nil {
		return nil
	}

	plc.mu.RLock()
	defer plc.mu.RUnlock()

	val, ok := plc.Values[tagName]
	if !ok {
		logging.DebugLog("plcman", "GetTagValueChange: tag %s not in Values map for PLC %s", tagName, plcName)
		return nil
	}
	if val == nil {
		logging.DebugLog("plcman", "GetTagValueChange: tag %s has nil value for PLC %s", tagName, plcName)
		return nil
	}
	if val.Error != nil {
		logging.DebugLog("plcman", "GetTagValueChange: tag %s has error for PLC %s: %v", tagName, plcName, val.Error)
		return nil
	}

	// Build lookup from config
	var writable bool
	var alias string
	var noREST, noMQTT, noKafka, noValkey bool
	for _, tag := range plc.Config.Tags {
		if tag.Name == tagName {
			writable = tag.Writable
			alias = tag.Alias
			noREST = tag.NoREST
			noMQTT = tag.NoMQTT
			noKafka = tag.NoKafka
			noValkey = tag.NoValkey
			break
		}
	}

	family := plc.Config.GetFamily()
	vc := &ValueChange{
		PLCName:  plcName,
		TagName:  tagName,
		Alias:    alias,
		TypeName: val.TypeName(),
		Value:    val.GoValue(),
		Writable: writable,
		Family:   string(family),
		NoREST:   noREST,
		NoMQTT:   noMQTT,
		NoKafka:  noKafka,
		NoValkey: noValkey,
	}

	// For S7/Omron, set Address to uppercase version of TagName for troubleshooting
	if family == config.FamilyS7 || family == config.FamilyOmron {
		vc.Address = strings.ToUpper(tagName)
	}

	return vc
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
		family := plc.Config.GetFamily()

		// For S7 family, normalize keys to uppercase since S7 addresses are case-insensitive
		normalizeKey := func(s string) string {
			if family == config.FamilyS7 {
				return strings.ToUpper(s)
			}
			return s
		}

		// Build lookup maps from config
		writableMap := make(map[string]bool)
		aliasMap := make(map[string]string)
		noRESTMap := make(map[string]bool)
		noMQTTMap := make(map[string]bool)
		noKafkaMap := make(map[string]bool)
		noValkeyMap := make(map[string]bool)
		for _, tag := range plc.Config.Tags {
			key := normalizeKey(tag.Name)
			writableMap[key] = tag.Writable
			aliasMap[key] = tag.Alias
			noRESTMap[key] = tag.NoREST
			noMQTTMap[key] = tag.NoMQTT
			noKafkaMap[key] = tag.NoKafka
			noValkeyMap[key] = tag.NoValkey
		}
		for tagName, val := range plc.Values {
			if val != nil && val.Error == nil {
				lookupKey := normalizeKey(tagName)
				vc := ValueChange{
					PLCName:  plcName,
					TagName:  tagName,
					Alias:    aliasMap[lookupKey],
					TypeName: val.TypeName(),
					Value:    val.GoValue(),
					Writable: writableMap[lookupKey],
					Family:   string(family),
					NoREST:   noRESTMap[lookupKey],
					NoMQTT:   noMQTTMap[lookupKey],
					NoKafka:  noKafkaMap[lookupKey],
					NoValkey: noValkeyMap[lookupKey],
				}
				// For S7/Omron, set Address to uppercase version of TagName for troubleshooting
				if family == config.FamilyS7 || family == config.FamilyOmron {
					vc.Address = strings.ToUpper(tagName)
				}
				results = append(results, vc)
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
	drv := plc.Driver
	status := plc.Status
	plc.mu.RUnlock()

	// If not cached, try to read the tag to get its type
	if drv == nil || status != StatusConnected {
		return 0
	}

	values, err := drv.Read([]driver.TagRequest{{Name: tagName}})
	if err != nil || len(values) == 0 || values[0] == nil {
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
	drv := plc.Driver
	status := plc.Status
	// Build type hints map from config
	typeMap := make(map[string]string)
	for _, sel := range plc.Config.Tags {
		if sel.DataType != "" {
			typeMap[sel.Name] = sel.DataType
		}
	}
	plc.mu.RUnlock()

	if status != StatusConnected || drv == nil {
		return nil, fmt.Errorf("PLC not connected: %s", plcName)
	}

	// Build read requests with type hints
	requests := make([]driver.TagRequest, len(tagNames))
	for i, name := range tagNames {
		requests[i] = driver.TagRequest{Name: name, TypeHint: typeMap[name]}
	}

	values, err := drv.Read(requests)
	if err != nil {
		return nil, err
	}

	result := make(map[string]interface{})
	for _, v := range values {
		if v != nil && v.Error == nil {
			result[v.Name] = v.Value
		} else if v != nil {
			result[v.Name] = nil
		}
	}
	return result, nil
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
