// Package plcman provides PLC connection management with background polling.
package plcman

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"warlink/ads"
	"warlink/config"
	"warlink/driver"
	"warlink/logging"
	"warlink/logix"
	"warlink/omron"
	"warlink/s7"
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
	ManualTags    []driver.TagInfo   // Tags from config (for non-discovery PLCs)
	ManualTagGen  uint64             // Incremented when ManualTags are rebuilt
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

// AllowManualTags returns whether manual tag addition should be offered.
// True when discovery is disabled for this PLC (either by family default or explicit config).
func (m *ManagedPLC) AllowManualTags() bool {
	return !m.Config.SupportsDiscovery()
}

// GetManualTagGen returns the manual tag generation counter.
// This increments whenever ManualTags are rebuilt (connect, config change, type resolution).
func (m *ManagedPLC) GetManualTagGen() uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ManualTagGen
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
	if m.Config.SupportsDiscovery() && len(m.Tags) > 0 {
		// Merge: start with discovered tags, append any manual tags not already discovered
		if len(m.ManualTags) == 0 {
			return m.Tags
		}
		// Build set of discovered tag names (excluding program entries like "Program:MainProgram")
		discovered := make(map[string]bool, len(m.Tags))
		for _, t := range m.Tags {
			discovered[t.Name] = true
		}
		merged := make([]driver.TagInfo, len(m.Tags))
		copy(merged, m.Tags)
		for _, mt := range m.ManualTags {
			if discovered[mt.Name] {
				continue // Already in discovered list
			}
			if isChildOfAnyParent(mt.Name, discovered) {
				continue // Child of a discovered tag (e.g., UDT member)
			}
			merged = append(merged, mt)
		}
		return merged
	}
	// Manual mode: filter out children of known structure tags.
	// They'll be shown as sub-nodes via lazyExpandUDT when the parent is expanded.
	return m.filterStructChildren(m.ManualTags)
}

// isChildOfAnyParent returns true if tagName is a dot-delimited child of any name in parents.
// E.g., "HMI_Edit.Access_Level" is a child of "HMI_Edit" if it exists in parents.
// Program entries like "Program:MainProgram" are skipped (they're section headers, not data tags).
func isChildOfAnyParent(tagName string, parents map[string]bool) bool {
	for i := len(tagName) - 1; i >= 0; i-- {
		if tagName[i] == '.' {
			prefix := tagName[:i]
			// Skip program entries (e.g., "Program:MainProgram")
			if strings.HasPrefix(prefix, "Program:") && !strings.Contains(prefix[8:], ".") {
				continue
			}
			if parents[prefix] {
				return true
			}
			// Strip array index: "Employee_Data[0]" -> "Employee_Data"
			if bracketIdx := strings.IndexByte(prefix, '['); bracketIdx >= 0 {
				if parents[prefix[:bracketIdx]] {
					return true
				}
			}
		}
	}
	return false
}

// filterStructChildren removes ManualTags that are children of resolved structure tags.
// Before type resolution, all tags have default types so no filtering occurs.
// After resolution, structure tags have IsStructure(TypeCode) == true, and their
// children are filtered since lazyExpandUDT will show them as sub-nodes.
func (m *ManagedPLC) filterStructChildren(tags []driver.TagInfo) []driver.TagInfo {
	if len(tags) == 0 {
		return tags
	}
	// Build set of structure tag names (confirmed via type resolution)
	structNames := make(map[string]bool)
	for _, t := range tags {
		if logix.IsStructure(t.TypeCode) {
			structNames[t.Name] = true
		}
	}
	if len(structNames) == 0 {
		return tags
	}
	filtered := make([]driver.TagInfo, 0, len(tags))
	for _, t := range tags {
		if !isChildOfAnyParent(t.Name, structNames) {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// BuildManualTags creates TagInfo entries from config.Tags for non-discovery PLCs.
// Preserves previously resolved TypeCodes so that struct types don't get reset to
// defaults on every rebuild, avoiding unnecessary resolveManualTagTypes cycles.
func (m *ManagedPLC) BuildManualTags() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.Config == nil {
		m.ManualTags = nil
		m.ManualTagGen++
		return
	}

	// Index existing resolved types by name so we can carry them forward
	oldTypes := make(map[string]driver.TagInfo, len(m.ManualTags))
	for _, t := range m.ManualTags {
		oldTypes[t.Name] = t
	}

	family := m.Config.GetFamily()
	newTags := make([]driver.TagInfo, 0, len(m.Config.Tags))

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

		// Carry forward previously resolved type if config didn't specify one
		// (e.g., struct types that can't be persisted to config)
		if old, exists := oldTypes[sel.Name]; exists && !ok && old.TypeCode != typeCode {
			typeCode = old.TypeCode
			typeName = old.TypeName
		}

		tagInfo := driver.TagInfo{
			Name:       sel.Name,
			TypeCode:   typeCode,
			TypeName:   typeName,
			Writable:   sel.Writable,
			Dimensions: dimensions,
		}
		newTags = append(newTags, tagInfo)
	}

	// Only bump generation if the tag list actually changed
	changed := len(newTags) != len(m.ManualTags)
	if !changed {
		for i := range newTags {
			if newTags[i].Name != m.ManualTags[i].Name || newTags[i].TypeCode != m.ManualTags[i].TypeCode {
				changed = true
				break
			}
		}
	}

	m.ManualTags = newTags
	if changed {
		m.ManualTagGen++
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

	// Manual tag type resolution (runs once per ManualTags rebuild)
	lastResolvedGen uint64
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

		// Update ManualTags type codes from actual poll results.
		// On first successful read, the real DataType replaces the default DINT.
		// Skip CIP structure responses (0x02A0) for logix — these lack the template ID
		// needed by IsStructure() and lazyExpandUDT. The resolveManualTagTypes method
		// handles structures via symbol table lookup.
		if v.Error == nil && len(plc.ManualTags) > 0 {
			for i := range plc.ManualTags {
				if plc.ManualTags[i].Name == v.Name && plc.ManualTags[i].TypeCode != v.DataType {
					if (family == config.FamilyLogix || family == config.FamilyMicro800) && logix.IsCIPStructResponse(v.DataType) {
						break
					}
					plc.ManualTags[i].TypeCode = v.DataType
					plc.ManualTagGen++
					var resolvedName string
					switch family {
					case config.FamilyS7:
						resolvedName = s7.TypeName(v.DataType)
					case config.FamilyBeckhoff:
						resolvedName = ads.TypeName(v.DataType)
					case config.FamilyOmron:
						resolvedName = omron.TypeName(v.DataType)
					default:
						resolvedName = logix.TypeName(v.DataType)
					}
					plc.ManualTags[i].TypeName = resolvedName
					// Persist to config if the type can round-trip (atomic types only, not "STRUCT(n)")
					for j := range cfg.Tags {
						if cfg.Tags[j].Name == v.Name {
							var canPersist bool
							switch family {
							case config.FamilyS7:
								_, canPersist = s7.TypeCodeFromName(resolvedName)
							case config.FamilyBeckhoff:
								_, canPersist = ads.TypeCodeFromName(resolvedName)
							case config.FamilyOmron:
								_, canPersist = omron.TypeCodeFromName(resolvedName)
							default:
								_, canPersist = logix.TypeCodeFromName(resolvedName)
							}
							if canPersist {
								cfg.Tags[j].DataType = resolvedName
							}
							break
						}
					}
					break
				}
			}
		}
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

	// One-time type resolution for manual tags with unknown types.
	// Reads non-enabled tags and resolves structure TypeCodes via symbol lookup.
	// Re-runs after each ManualTags rebuild (reconnect, config change).
	if !cfg.SupportsDiscovery() {
		plc.mu.RLock()
		needsResolve := plc.ManualTagGen != w.lastResolvedGen
		plc.mu.RUnlock()
		if needsResolve {
			w.resolveManualTagTypes()
		}
	}
}

// resolveManualTagTypes discovers the correct type codes for manual tags that still
// have their default type (e.g. DINT). For atomic types, a simple read reveals the
// correct DataType. For structures (CIP DataType 0x02A0), a targeted symbol table
// lookup retrieves the real TypeCode with template ID for UDT expansion.
// Runs once per ManualTags rebuild (tracked via ManualTagGen).
func (w *PLCWorker) resolveManualTagTypes() {
	plc := w.plc

	plc.mu.RLock()
	drv := plc.Driver
	family := plc.Config.GetFamily()
	cfg := plc.Config
	gen := plc.ManualTagGen

	if drv == nil || !drv.IsConnected() {
		plc.mu.RUnlock()
		return
	}

	// Determine the default type code for this family
	var defaultTypeCode uint16
	switch family {
	case config.FamilyS7:
		defaultTypeCode = s7.TypeDInt
	case config.FamilyBeckhoff:
		defaultTypeCode = ads.TypeInt32
	case config.FamilyOmron:
		defaultTypeCode = omron.TypeWord
	default:
		defaultTypeCode = logix.TypeDINT
	}

	// Collect tags that still have the default type, checking polled values first
	type unresolvedTag struct {
		Name     string
		DataType uint16 // >0 if already available from poll values
	}
	var unresolved []unresolvedTag

	for _, mt := range plc.ManualTags {
		if mt.TypeCode == defaultTypeCode {
			var dt uint16
			if val, ok := plc.Values[mt.Name]; ok && val != nil && val.Error == nil && val.DataType != 0 {
				dt = val.DataType
			}
			unresolved = append(unresolved, unresolvedTag{Name: mt.Name, DataType: dt})
		}
	}
	plc.mu.RUnlock()

	if len(unresolved) == 0 {
		w.lastResolvedGen = gen
		return
	}

	logging.DebugLog("plcman", "RESOLVE %s: resolving types for %d manual tags", cfg.Name, len(unresolved))

	// Read tags that don't already have a value (outside lock — network I/O)
	var needsRead []string
	for _, u := range unresolved {
		if u.DataType == 0 {
			needsRead = append(needsRead, u.Name)
		}
	}

	if len(needsRead) > 0 {
		requests := make([]driver.TagRequest, len(needsRead))
		for i, name := range needsRead {
			requests[i] = driver.TagRequest{Name: name}
		}
		values, err := drv.Read(requests)
		if err == nil {
			readResults := make(map[string]uint16)
			for _, val := range values {
				if val != nil && val.Error == nil && val.DataType != 0 {
					readResults[val.Name] = val.DataType
				}
			}
			for i := range unresolved {
				if unresolved[i].DataType == 0 {
					if dt, ok := readResults[unresolved[i].Name]; ok {
						unresolved[i].DataType = dt
					}
				}
			}
		}
	}

	// Resolve CIP structure types via symbol table lookup (outside lock — network I/O)
	type resolvedInfo struct {
		Name     string
		TypeCode uint16
		TypeName string
	}
	var resolved []resolvedInfo

	for _, u := range unresolved {
		if u.DataType == 0 {
			continue
		}

		dataType := u.DataType

		// For logix structures, resolve via symbol lookup to get template ID
		if (family == config.FamilyLogix || family == config.FamilyMicro800) && logix.IsCIPStructResponse(dataType) {
			if adapter, ok := drv.(*driver.LogixAdapter); ok {
				if tc, found := adapter.ResolveTagType(u.Name); found {
					logging.DebugLog("plcman", "RESOLVE %s: symbol lookup found TypeCode 0x%04X", u.Name, tc)
					dataType = tc
				} else {
					logging.DebugLog("plcman", "RESOLVE %s: symbol lookup failed (CIP type 0x%04X)", u.Name, u.DataType)
				}
			}
		}

		var typeName string
		switch family {
		case config.FamilyS7:
			typeName = s7.TypeName(dataType)
		case config.FamilyBeckhoff:
			typeName = ads.TypeName(dataType)
		case config.FamilyOmron:
			typeName = omron.TypeName(dataType)
		default:
			typeName = logix.TypeName(dataType)
		}

		resolved = append(resolved, resolvedInfo{
			Name:     u.Name,
			TypeCode: dataType,
			TypeName: typeName,
		})
	}

	if len(resolved) == 0 {
		return
	}

	// Apply results under lock, tracking whether anything actually changed
	plc.mu.Lock()
	anyChanged := false
	for _, r := range resolved {
		for i := range plc.ManualTags {
			if plc.ManualTags[i].Name == r.Name {
				if plc.ManualTags[i].TypeCode != r.TypeCode {
					plc.ManualTags[i].TypeCode = r.TypeCode
					plc.ManualTags[i].TypeName = r.TypeName
					anyChanged = true
				}

				// Persist atomic types to config
				for j := range cfg.Tags {
					if cfg.Tags[j].Name == r.Name {
						var canPersist bool
						switch family {
						case config.FamilyS7:
							_, canPersist = s7.TypeCodeFromName(r.TypeName)
						case config.FamilyBeckhoff:
							_, canPersist = ads.TypeCodeFromName(r.TypeName)
						case config.FamilyOmron:
							_, canPersist = omron.TypeCodeFromName(r.TypeName)
						default:
							_, canPersist = logix.TypeCodeFromName(r.TypeName)
						}
						if canPersist {
							cfg.Tags[j].DataType = r.TypeName
						}
						break
					}
				}
				break
			}
		}
	}
	// Only bump generation if a type actually changed (avoids unnecessary tree rebuilds)
	if anyChanged {
		plc.ManualTagGen++
	}
	updatedGen := plc.ManualTagGen
	plc.mu.Unlock()

	// Update the logix client's tagInfo map so reads use correct TypeCodes
	// (otherwise CIP struct response 0x02A0 gets overridden to old default type)
	if family == config.FamilyLogix || family == config.FamilyMicro800 {
		if adapter, ok := drv.(*driver.LogixAdapter); ok {
			plc.mu.RLock()
			tagsCopy := make([]driver.TagInfo, len(plc.ManualTags))
			copy(tagsCopy, plc.ManualTags)
			plc.mu.RUnlock()
			adapter.SetTags(tagsCopy)
		}
	}

	// Mark both the UI generation and the worker's resolved generation
	w.lastResolvedGen = updatedGen

	w.manager.markStatusDirty()
}

// ListenerID is a unique identifier for a registered listener.
type ListenerID string

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

	// Legacy single callbacks (for backward compatibility)
	onChange      func()
	onValueChange func(changes []ValueChange)
	onLog         func(format string, args ...interface{}) // Log callback for TUI integration

	// Multi-listener support
	changeListeners      map[ListenerID]func()
	valueChangeListeners map[ListenerID]func([]ValueChange)
	listenersMu          sync.RWMutex
	listenerCounter      uint64

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
		plcs:                 make(map[string]*ManagedPLC),
		workers:              make(map[string]*PLCWorker),
		pollRate:             pollRate,
		batchInterval:        100 * time.Millisecond, // Batch UI updates every 100ms
		changeChan:           make(chan []ValueChange, 100),
		reconnecting:         make(map[string]bool),
		changeListeners:      make(map[ListenerID]func()),
		valueChangeListeners: make(map[ListenerID]func([]ValueChange)),
	}
}

// SetOnChange sets a callback that fires when PLC status changes.
// For backward compatibility. Use AddOnChangeListener for multi-listener support.
func (m *Manager) SetOnChange(fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onChange = fn
}

// SetOnValueChange sets a callback that fires when tag values change.
// For backward compatibility. Use AddOnValueChangeListener for multi-listener support.
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

// AddOnChangeListener registers a callback for PLC status changes.
// Returns a ListenerID that can be used to remove the listener.
// The callback is called in a goroutine to avoid blocking.
func (m *Manager) AddOnChangeListener(cb func()) ListenerID {
	m.listenersMu.Lock()
	defer m.listenersMu.Unlock()
	id := ListenerID(fmt.Sprintf("change-%d", atomic.AddUint64(&m.listenerCounter, 1)))
	m.changeListeners[id] = cb
	return id
}

// RemoveOnChangeListener removes a previously registered change listener.
func (m *Manager) RemoveOnChangeListener(id ListenerID) {
	m.listenersMu.Lock()
	defer m.listenersMu.Unlock()
	delete(m.changeListeners, id)
}

// AddOnValueChangeListener registers a callback for tag value changes.
// Returns a ListenerID that can be used to remove the listener.
// The callback is called in a goroutine to avoid blocking.
func (m *Manager) AddOnValueChangeListener(cb func([]ValueChange)) ListenerID {
	m.listenersMu.Lock()
	defer m.listenersMu.Unlock()
	id := ListenerID(fmt.Sprintf("value-%d", atomic.AddUint64(&m.listenerCounter, 1)))
	m.valueChangeListeners[id] = cb
	return id
}

// RemoveOnValueChangeListener removes a previously registered value change listener.
func (m *Manager) RemoveOnValueChangeListener(id ListenerID) {
	m.listenersMu.Lock()
	defer m.listenersMu.Unlock()
	delete(m.valueChangeListeners, id)
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

	// Build manual tags from config immediately so they're available
	// before the PLC connects. No-op when config.Tags is empty.
	plc.BuildManualTags()

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

	// Only discover programs and tags when discovery is enabled in config
	if cfg.SupportsDiscovery() {
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
			tags, err = drv.AllTags()
			if err != nil {
				m.log("[yellow]PLC %s tag discovery failed: %v[-]", plcName, err)
			}
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

	// Build manual tags from config (no-op when config.Tags is empty)
	plc.BuildManualTags()

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
				m.fireOnChange()
			}

			// Flush pending value changes
			if len(pendingChanges) > 0 {
				m.flushValueChanges(pendingChanges)
				pendingChanges = nil
			}
		}
	}
}

// fireOnChange calls all registered change listeners in goroutines.
func (m *Manager) fireOnChange() {
	// Call legacy callback
	m.mu.RLock()
	fn := m.onChange
	m.mu.RUnlock()
	if fn != nil {
		fn()
	}

	// Call all multi-listener callbacks in goroutines
	m.listenersMu.RLock()
	listeners := make([]func(), 0, len(m.changeListeners))
	for _, cb := range m.changeListeners {
		listeners = append(listeners, cb)
	}
	m.listenersMu.RUnlock()

	for _, cb := range listeners {
		go cb()
	}
}

// flushValueChanges calls all value change callbacks with accumulated changes.
func (m *Manager) flushValueChanges(changes []ValueChange) {
	if len(changes) == 0 {
		return
	}

	// Call legacy callback
	m.mu.RLock()
	fn := m.onValueChange
	m.mu.RUnlock()
	if fn != nil {
		fn(changes)
	}

	// Call all multi-listener callbacks in goroutines
	m.listenersMu.RLock()
	listeners := make([]func([]ValueChange), 0, len(m.valueChangeListeners))
	for _, cb := range m.valueChangeListeners {
		listeners = append(listeners, cb)
	}
	m.listenersMu.RUnlock()

	// Make a copy for each listener to avoid races
	for _, cb := range listeners {
		changesCopy := make([]ValueChange, len(changes))
		copy(changesCopy, changes)
		go cb(changesCopy)
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

	if plc.AllowManualTags() {
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
