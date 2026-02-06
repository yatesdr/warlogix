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
	"warlogix/fins"
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

// HealthStatus represents the health state of a PLC for publishing.
type HealthStatus struct {
	Online    bool      `json:"online"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// MaxConnectRetries is the maximum number of connection attempts before giving up.
const MaxConnectRetries = 5

// ManagedPLC represents a PLC under management.
type ManagedPLC struct {
	Config       *config.PLCConfig
	Client       *logix.Client    // For Logix/Micro800 PLCs
	S7Client     *s7.Client       // For Siemens S7 PLCs
	AdsClient    *ads.Client      // For Beckhoff TwinCAT PLCs
	FinsClient   *fins.Client     // For Omron FINS PLCs
	Identity     *logix.DeviceInfo
	S7Info       *s7.CPUInfo
	AdsInfo      *ads.DeviceInfo  // Beckhoff device info
	FinsInfo     *fins.DeviceInfo // Omron device info
	Programs     []string
	Tags         []logix.TagInfo  // Discovered tags (for discovery-capable PLCs)
	ManualTags   []logix.TagInfo  // Tags from config (for non-discovery PLCs)
	Values       map[string]*TagValue // Unified tag values (S7, Logix, ADS, FINS)
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

// GetHealthStatus returns the current health status for publishing.
func (m *ManagedPLC) GetHealthStatus() HealthStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	health := HealthStatus{
		Timestamp: time.Now().UTC(),
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
		case config.FamilyOmron:
			typeCode, ok = fins.TypeCodeFromName(sel.DataType)
			if !ok {
				typeCode = fins.TypeWord // Default to WORD for FINS
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

// GetLogixClient returns the underlying Logix client, or nil if not available.
// Used for accessing client-specific features like GetElementSize.
func (m *ManagedPLC) GetLogixClient() *logix.Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.Client
}

// GetConnectionMode returns a human-readable string describing the connection mode.
func (m *ManagedPLC) GetConnectionMode() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.FinsClient != nil {
		return m.FinsClient.ConnectionMode()
	}
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
	TagName  string      // For S7: the address (e.g., "DB1.8"); for others: the tag name
	Alias    string      // User-defined alias/name (especially useful for S7)
	Address  string      // For S7: the address in uppercase; empty for other families
	TypeName string
	Value    interface{}
	Writable bool
	Family   string      // PLC family ("s7", "logix", "beckhoff", etc.)
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
	finsClient := plc.FinsClient
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
	for _, sel := range cfg.Tags {
		if sel.Enabled {
			tagsToRead = append(tagsToRead, sel.Name)
		}
		writableMap[sel.Name] = sel.Writable
		aliasMap[sel.Name] = sel.Alias
		if sel.DataType != "" {
			typeMap[sel.Name] = sel.DataType
		}
		if len(sel.IgnoreChanges) > 0 {
			ignoreMap[sel.Name] = sel.IgnoreChanges
		}
	}
	oldStableValues := make(map[string]interface{})
	for k, v := range plc.Values {
		if v != nil && v.Error == nil {
			oldStableValues[k] = v.StableValue
		}
	}
	plc.mu.RUnlock()

	// Check if we have a valid connection based on family
	var hasConnection bool
	switch family {
	case config.FamilyS7:
		hasConnection = s7Client != nil && s7Client.IsConnected()
	case config.FamilyBeckhoff:
		hasConnection = adsClient != nil && adsClient.IsConnected()
	case config.FamilyOmron:
		hasConnection = finsClient != nil && finsClient.IsConnected()
	default:
		hasConnection = client != nil
	}

	if status != StatusConnected || !hasConnection {
		// Check if auto-connect is enabled and we should attempt reconnection
		plc.mu.RLock()
		autoConnect := plc.Config.Enabled
		plc.mu.RUnlock()

		needsReconnect := autoConnect && (status == StatusDisconnected || status == StatusError)

		// If S7 client exists but is not connected, trigger reconnection
		if family == config.FamilyS7 && (needsReconnect || (s7Client != nil && !s7Client.IsConnected())) {
			plc.mu.Lock()
			plc.Status = StatusDisconnected
			if plc.S7Client != nil {
				plc.S7Client.Close()
				plc.S7Client = nil
			}
			plc.mu.Unlock()
			w.manager.markStatusDirty()
			go w.manager.scheduleReconnect(plcName)
		}
		// If ADS client exists but is not connected, trigger reconnection
		if family == config.FamilyBeckhoff && (needsReconnect || (adsClient != nil && !adsClient.IsConnected())) {
			plc.mu.Lock()
			plc.Status = StatusDisconnected
			if plc.AdsClient != nil {
				plc.AdsClient.Close()
				plc.AdsClient = nil
			}
			plc.mu.Unlock()
			w.manager.markStatusDirty()
			go w.manager.scheduleReconnect(plcName)
		}
		// If FINS client exists but is not connected, trigger reconnection
		if family == config.FamilyOmron && (needsReconnect || (finsClient != nil && !finsClient.IsConnected())) {
			plc.mu.Lock()
			plc.Status = StatusDisconnected
			if plc.FinsClient != nil {
				plc.FinsClient.Close()
				plc.FinsClient = nil
			}
			plc.mu.Unlock()
			w.manager.markStatusDirty()
			go w.manager.scheduleReconnect(plcName)
		}
		// If Logix/Micro800 client is not connected, trigger reconnection
		if (family == config.FamilyLogix || family == config.FamilyMicro800) && needsReconnect {
			plc.mu.Lock()
			plc.Status = StatusDisconnected
			if plc.Client != nil {
				plc.Client.Close()
				plc.Client = nil
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
		// No tags to poll, but we need to keep the CIP connection alive
		// by sending periodic traffic. The CIP ForwardOpen connection
		// times out after ~30 seconds of inactivity (RPI × 2^multiplier).
		if family == config.FamilyLogix {
			if client != nil {
				// Send CIP NOP to keep ForwardOpen connection alive
				_ = client.Keepalive()
			}
		}
		// Other families (S7, Beckhoff, Omron) have their own keepalive mechanisms

		w.statsMu.Lock()
		w.tagsPolled = 0
		w.changesFound = 0
		w.lastError = nil
		w.statsMu.Unlock()
		return
	}

	// Read selected tags based on family type
	var values []*TagValue
	var err error

	switch family {
	case config.FamilyS7:
		values, err = w.pollS7(s7Client, tagsToRead, typeMap)
		// Apply ignore lists for S7 values
		if err == nil {
			for _, v := range values {
				if ignoreList, ok := ignoreMap[v.Name]; ok {
					v.SetIgnoreList(ignoreList)
				}
			}
		}
	case config.FamilyBeckhoff:
		values, err = w.pollAds(adsClient, tagsToRead)
		// Apply ignore lists for Beckhoff values
		if err == nil {
			for _, v := range values {
				if ignoreList, ok := ignoreMap[v.Name]; ok {
					v.SetIgnoreList(ignoreList)
				}
			}
		}
	case config.FamilyOmron:
		values, err = w.pollFins(finsClient, tagsToRead, typeMap)
		// Apply ignore lists for Omron values
		if err == nil {
			for _, v := range values {
				if ignoreList, ok := ignoreMap[v.Name]; ok {
					v.SetIgnoreList(ignoreList)
				}
			}
		}
	default:
		var logixValues []*logix.TagValue
		logixValues, err = client.Read(tagsToRead...)
		if err == nil {
			values = make([]*TagValue, len(logixValues))
			for i, lv := range logixValues {
				// Use decoded version to get UDT member names in output
				values[i] = FromLogixTagValueDecoded(lv, client)
				// Apply ignore list for change detection
				if ignoreList, ok := ignoreMap[lv.Name]; ok {
					values[i].SetIgnoreList(ignoreList)
				}
			}
		}
	}

	if err != nil {
		plc.mu.Lock()
		plc.LastError = err
		autoConnect := plc.Config.Enabled

		// Determine if client detected disconnection
		clientDisconnected := false

		// For S7, check if this is a connection error and mark client as disconnected
		if family == config.FamilyS7 && s7Client != nil {
			if !s7Client.IsConnected() {
				clientDisconnected = true
				plc.S7Client.Close()
				plc.S7Client = nil
			}
		}

		// For Beckhoff, check if this is a connection error and mark client as disconnected
		if family == config.FamilyBeckhoff && adsClient != nil {
			if !adsClient.IsConnected() {
				clientDisconnected = true
				plc.AdsClient.Close()
				plc.AdsClient = nil
			}
		}

		// For Omron FINS, check if this is a connection error and mark client as disconnected
		if family == config.FamilyOmron && finsClient != nil {
			if !finsClient.IsConnected() {
				clientDisconnected = true
				plc.FinsClient.Close()
				plc.FinsClient = nil
			}
		}

		// Set status based on whether client detected disconnection
		if clientDisconnected {
			plc.Status = StatusDisconnected
		} else {
			plc.Status = StatusError
		}

		plcNameForLog := plc.Config.Name
		plc.mu.Unlock()

		w.statsMu.Lock()
		w.tagsPolled = len(tagsToRead)
		w.changesFound = 0
		w.lastError = err
		w.statsMu.Unlock()

		w.manager.markStatusDirty()

		// Schedule reconnection if auto-connect is enabled and client detected disconnection
		// OR if there's a repeated error (let scheduleReconnect handle deduplication)
		shouldReconnect := autoConnect && (clientDisconnected || isLikelyConnectionError(err))

		if shouldReconnect {
			switch family {
			case config.FamilyS7:
				w.manager.log("[yellow]PLC %s connection lost, scheduling reconnect[-]", plcNameForLog)
				go w.manager.scheduleReconnect(plcNameForLog)
			case config.FamilyBeckhoff:
				w.manager.log("[yellow]PLC %s connection lost, scheduling reconnect[-]", plcNameForLog)
				go w.manager.scheduleReconnect(plcNameForLog)
			case config.FamilyOmron:
				w.manager.log("[yellow]PLC %s connection lost, scheduling reconnect[-]", plcNameForLog)
				go w.manager.scheduleReconnect(plcNameForLog)
			case config.FamilyLogix, config.FamilyMicro800:
				w.manager.log("[yellow]PLC %s connection lost, scheduling reconnect[-]", plcNameForLog)
				go w.manager.scheduleReconnect(plcNameForLog)
			}
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
				vc := ValueChange{
					PLCName:  plcName,
					TagName:  v.Name,
					Alias:    aliasMap[v.Name],
					TypeName: v.TypeName(),
					Value:    newVal,
					Writable: writableMap[v.Name],
					Family:   string(family),
				}
				// For S7, set Address to uppercase version of TagName
				if family == config.FamilyS7 {
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

// pollS7 reads tags from an S7 PLC and converts them to unified TagValue format.
// typeMap contains user-configured data types used for address parsing and display.
// The S7 package handles parsing with native big-endian byte order.
func (w *PLCWorker) pollS7(client *s7.Client, addresses []string, typeMap map[string]string) ([]*TagValue, error) {
	// Check for nil or disconnected client
	if client == nil {
		return nil, fmt.Errorf("S7 client is nil")
	}
	if !client.IsConnected() {
		return nil, fmt.Errorf("S7 client is not connected")
	}

	// Build requests with type hints from configuration
	requests := make([]s7.TagRequest, len(addresses))
	for i, addr := range addresses {
		requests[i] = s7.TagRequest{
			Address:  addr,
			TypeHint: typeMap[addr], // Pass configured type as hint
		}
	}

	s7Values, err := client.ReadWithTypes(requests)
	if err != nil {
		return nil, err
	}

	// Convert S7 TagValues to unified TagValues
	// The s7 package's GoValue() handles big-endian parsing natively
	results := make([]*TagValue, len(s7Values))
	for i, sv := range s7Values {
		// Use configured type if specified, otherwise use detected type
		dataType := sv.DataType
		if configuredType, ok := typeMap[sv.Name]; ok {
			if s7Type, found := s7.TypeCodeFromName(configuredType); found {
				dataType = s7Type
			}
		}

		// Mark as array if Count > 1
		count := sv.Count
		if count < 1 {
			count = 1
		}
		if count > 1 {
			dataType = s7.MakeArrayType(dataType)
		}

		// Create unified TagValue using S7's native parsing
		value := sv.GoValue() // Uses big-endian (native S7 format)
		results[i] = &TagValue{
			Name:        sv.Name,
			DataType:    dataType,
			Family:      "s7",
			Value:       value,
			StableValue: value, // Default StableValue equals Value; ignore list applied later
			Bytes:       sv.Bytes, // Keep original big-endian bytes
			Count:       count,
			Error:       sv.Error,
		}
	}
	return results, nil
}

// Each PLC family package now handles its own type parsing with native byte order.
// S7: big-endian (native S7 format)
// Logix: little-endian (native Logix format)
// ADS: little-endian (native x86 format)

// pollAds reads tags from a Beckhoff TwinCAT PLC and converts them to unified TagValue format.
// The ADS package handles parsing with native little-endian byte order (x86/TwinCAT).
func (w *PLCWorker) pollAds(client *ads.Client, symbols []string) ([]*TagValue, error) {
	// Check for nil or disconnected client
	if client == nil {
		return nil, fmt.Errorf("ADS client is nil")
	}
	if !client.IsConnected() {
		return nil, fmt.Errorf("ADS client is not connected")
	}

	adsValues, err := client.Read(symbols...)
	if err != nil {
		return nil, err
	}

	// Convert ADS TagValues to unified TagValues
	// The ads package's GoValue() handles little-endian parsing natively
	results := make([]*TagValue, len(adsValues))
	for i, av := range adsValues {
		// Use Count from ADS value for proper array handling
		count := av.Count
		if count < 1 {
			count = 1
		}

		// Mark data type as array if Count > 1
		dataType := av.DataType
		if count > 1 {
			dataType = ads.MakeArrayType(dataType)
		}

		value := av.GoValue() // Uses little-endian (native x86/TwinCAT format)
		results[i] = &TagValue{
			Name:        av.Name,
			DataType:    dataType,
			Family:      "beckhoff",
			Value:       value,
			StableValue: value, // Default StableValue equals Value; ignore list applied later
			Bytes:       av.Bytes,
			Count:       count,
			Error:       av.Error,
		}
	}
	return results, nil
}

// pollFins reads tags from an Omron PLC via FINS and converts them to unified TagValue format.
// The FINS package handles parsing with native big-endian byte order (Omron format).
func (w *PLCWorker) pollFins(client *fins.Client, addresses []string, typeMap map[string]string) ([]*TagValue, error) {
	// Check for nil or disconnected client
	if client == nil {
		return nil, fmt.Errorf("FINS client is nil")
	}
	if !client.IsConnected() {
		return nil, fmt.Errorf("FINS client is not connected")
	}

	// Build requests with type hints from configuration
	requests := make([]fins.TagRequest, len(addresses))
	for i, addr := range addresses {
		requests[i] = fins.TagRequest{
			Address:  addr,
			TypeHint: typeMap[addr], // Pass configured type as hint
		}
	}

	finsValues, err := client.ReadWithTypes(requests)
	if err != nil {
		return nil, err
	}

	// Convert FINS TagValues to unified TagValues
	// The fins package's GoValue() handles big-endian parsing natively
	results := make([]*TagValue, len(finsValues))
	for i, fv := range finsValues {
		// Use configured type if specified, otherwise use detected type
		dataType := fv.DataType
		if configuredType, ok := typeMap[fv.Name]; ok {
			if finsType, found := fins.TypeCodeFromName(configuredType); found {
				dataType = finsType
			}
		}

		// Use Count from FINS value for proper array handling
		count := fv.Count
		if count < 1 {
			count = 1
		}

		// Mark as array if Count > 1
		if count > 1 {
			dataType = fins.MakeArrayType(dataType)
		}

		// Create unified TagValue using FINS's native parsing
		value := fv.GoValue() // Uses big-endian (native Omron format)
		results[i] = &TagValue{
			Name:        fv.Name,
			DataType:    dataType,
			Family:      "omron",
			Value:       value,
			StableValue: value, // Default StableValue equals Value; ignore list applied later
			Bytes:       fv.Bytes,
			Count:       count,
			Error:       fv.Error,
		}
	}
	return results, nil
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
		if plc.FinsClient != nil {
			plc.FinsClient.Close()
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
	finsPort := plc.Config.FinsPort
	finsNetwork := plc.Config.FinsNetwork
	finsNode := plc.Config.FinsNode
	finsUnit := plc.Config.FinsUnit
	plc.mu.Unlock()
	m.markStatusDirty()

	// Handle family-specific connections
	switch family {
	case config.FamilyS7:
		return m.connectS7PLC(plc, address, int(slot))
	case config.FamilyBeckhoff:
		return m.connectBeckhoffPLC(plc, address, amsNetId, amsPort)
	case config.FamilyOmron:
		return m.connectOmronPLC(plc, address, finsPort, finsNetwork, finsNode, finsUnit)
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

	// Check if we have cached tags from a previous connection
	plc.mu.RLock()
	cachedTags := plc.Tags
	cachedPrograms := plc.Programs
	plc.mu.RUnlock()

	var programs []string
	var tags []logix.TagInfo

	// Only discover programs and tags for discovery-capable PLCs
	// If we have cached tags (from a previous connection), reuse them for fast reconnect
	if family.SupportsDiscovery() {
		if len(cachedTags) > 0 {
			// Fast reconnect: reuse cached tags
			programs = cachedPrograms
			tags = cachedTags
			m.log("[cyan]PLC %s using cached tags (%d tags)[-]", plc.Config.Name, len(tags))
		} else {
			// Full discovery: fetch programs and tags
			programs, _ = client.Programs()
			tags, _ = client.AllTags()
		}
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

	// Note: UDT templates are fetched lazily on-demand when structure tags are read.
	// This keeps initial connection fast, especially over slow networks (VPN, etc.)
	// Templates are cached after first fetch for fast subsequent reads.

	return nil
}

// connectS7PLC handles S7 PLC connections.
func (m *Manager) connectS7PLC(plc *ManagedPLC, address string, slot int) error {
	// S7-1200/1500: use rack 0, slot 0 (CPU is in the onboard slot)
	// S7-300/400: use rack 0, slot 2 (or wherever CPU is in the rack)
	// The slot in config directly maps to S7 slot number
	rack := 0
	s7Slot := slot // Use slot as-is - slot 0 is correct for S7-1200/1500

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

// connectOmronPLC handles Omron PLC connections via FINS protocol.
func (m *Manager) connectOmronPLC(plc *ManagedPLC, address string, port int, network, node, unit byte) error {
	opts := []fins.Option{}
	if port > 0 {
		opts = append(opts, fins.WithPort(port))
	}
	if network > 0 {
		opts = append(opts, fins.WithNetwork(network))
	}
	if node > 0 {
		opts = append(opts, fins.WithNode(node))
	}
	if unit > 0 {
		opts = append(opts, fins.WithUnit(unit))
	}

	finsClient, err := fins.Connect(address, opts...)
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
		m.log("[red]PLC %s (Omron) connection failed:[-] %v", name, lastErr)
		return err
	}

	// Get device info
	deviceInfo, _ := finsClient.GetDeviceInfo()

	plc.mu.Lock()
	plc.FinsClient = finsClient
	plc.FinsInfo = deviceInfo
	plc.Status = StatusConnected
	plc.ConnRetries = 0
	plc.RetryLimited = false
	name := plc.Config.Name
	plc.mu.Unlock()

	// Build manual tags from config (FINS doesn't support discovery)
	plc.BuildManualTags()

	m.markStatusDirty()
	m.log("[green]PLC %s (Omron) connected:[-] %s", name, finsClient.ConnectionMode())

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
	if plc.FinsClient != nil {
		plc.FinsClient.Close()
		plc.FinsClient = nil
	}
	plc.Status = StatusDisconnected
	plc.LastError = nil
	plc.Identity = nil
	plc.S7Info = nil
	plc.AdsInfo = nil
	plc.FinsInfo = nil
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
		pollRate := m.getEffectivePollRate(plc.Config)
		worker := newPLCWorker(plc, m, pollRate)
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
	case <-time.After(500 * time.Millisecond):
		// Timeout - proceed anyway
	}

	// Wait for manager goroutines with timeout
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		// Timeout - proceed anyway
	}

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

// scheduleReconnect schedules a reconnection attempt for a PLC after a short delay.
// This is called when a connection error is detected during polling.
func (m *Manager) scheduleReconnect(name string) {
	// Wait a short time before attempting reconnection to avoid rapid retries
	time.Sleep(2 * time.Second)

	m.mu.RLock()
	plc, exists := m.plcs[name]
	m.mu.RUnlock()

	if !exists {
		return
	}

	plc.mu.RLock()
	status := plc.Status
	enabled := plc.Config.Enabled
	plc.mu.RUnlock()

	// Only reconnect if still disconnected/error and enabled
	if !enabled || status == StatusConnected || status == StatusConnecting {
		return
	}

	// Check if reconnection is already in progress
	m.reconnectingMu.Lock()
	if m.reconnecting[name] {
		m.reconnectingMu.Unlock()
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
	client := plc.Client
	s7Client := plc.S7Client
	adsClient := plc.AdsClient
	finsClient := plc.FinsClient
	status := plc.Status
	family := plc.Config.GetFamily()
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
	if status != StatusConnected {
		return nil, fmt.Errorf("PLC not connected: %s (status: %s)", plcName, status)
	}

	switch family {
	case config.FamilyS7:
		if s7Client == nil {
			return nil, fmt.Errorf("S7 client not available for %s", plcName)
		}
		req := []s7.TagRequest{{Address: tagName, TypeHint: typeHint}}
		s7Values, err := s7Client.ReadWithTypes(req)
		if err != nil {
			return nil, err
		}
		if len(s7Values) > 0 {
			sv := s7Values[0]
			// Use configured type if specified, otherwise use detected type
			dataType := sv.DataType
			if typeHint != "" {
				if s7Type, found := s7.TypeCodeFromName(typeHint); found {
					dataType = s7Type
				}
			}
			// Mark as array if Count > 1
			count := sv.Count
			if count < 1 {
				count = 1
			}
			if count > 1 {
				dataType = s7.MakeArrayType(dataType)
			}
			return &TagValue{
				Name:     sv.Name,
				DataType: dataType,
				Family:   "s7",
				Value:    sv.GoValue(), // Uses big-endian (native S7 format)
				Bytes: sv.Bytes,
				Count:    count,
				Error:    sv.Error,
			}, nil
		}
		return nil, fmt.Errorf("no data returned for tag: %s", tagName)

	case config.FamilyBeckhoff:
		if adsClient == nil {
			return nil, fmt.Errorf("ADS client not available for %s", plcName)
		}
		adsValues, err := adsClient.Read(tagName)
		if err != nil {
			return nil, err
		}
		if len(adsValues) > 0 {
			av := adsValues[0]
			return &TagValue{
				Name:     av.Name,
				DataType: av.DataType,
				Family:   "beckhoff",
				Value:    av.GoValue(),
				Bytes:    av.Bytes,
				Count:    1,
				Error:    av.Error,
			}, nil
		}
		return nil, fmt.Errorf("no data returned for tag: %s", tagName)

	case config.FamilyOmron:
		if finsClient == nil {
			return nil, fmt.Errorf("FINS client not available for %s", plcName)
		}
		req := []fins.TagRequest{{Address: tagName, TypeHint: typeHint}}
		finsValues, err := finsClient.ReadWithTypes(req)
		if err != nil {
			return nil, err
		}
		if len(finsValues) > 0 {
			fv := finsValues[0]
			// Use configured type if specified, otherwise use detected type
			dataType := fv.DataType
			if typeHint != "" {
				if finsType, found := fins.TypeCodeFromName(typeHint); found {
					dataType = finsType
				}
			}
			// Mark as array if Count > 1
			count := fv.Count
			if count < 1 {
				count = 1
			}
			if count > 1 {
				dataType = fins.MakeArrayType(dataType)
			}
			return &TagValue{
				Name:     fv.Name,
				DataType: dataType,
				Family:   "omron",
				Value:    fv.GoValue(), // Uses big-endian (native Omron format)
				Bytes:    fv.Bytes,
				Count:    count,
				Error:    fv.Error,
			}, nil
		}
		return nil, fmt.Errorf("no data returned for tag: %s", tagName)

	default:
		// Handle Logix/Micro800 PLCs
		if client == nil {
			return nil, fmt.Errorf("Logix client not available for %s", plcName)
		}
		values, err := client.Read(tagName)
		if err != nil {
			// Check if this is a connection error and schedule reconnect
			if isLikelyConnectionError(err) {
				m.handleConnectionError(plcName, plc, err)
			}
			return nil, err
		}
		if len(values) > 0 {
			// Use decoded version to get UDT member names in output
			return FromLogixTagValueDecoded(values[0], client), nil
		}
		return nil, fmt.Errorf("no data returned for tag: %s", tagName)
	}
}

// handleConnectionError marks a PLC as disconnected and schedules reconnection.
func (m *Manager) handleConnectionError(plcName string, plc *ManagedPLC, err error) {
	plc.mu.Lock()
	wasConnected := plc.Status == StatusConnected
	plc.Status = StatusDisconnected
	autoConnect := plc.Config.Enabled
	plc.mu.Unlock()

	if wasConnected {
		m.log("[yellow]PLC %s connection error: %v[-]", plcName, err)
		m.markStatusDirty()

		// Schedule reconnection if auto-connect is enabled
		if autoConnect {
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
	client := plc.Client
	s7Client := plc.S7Client
	adsClient := plc.AdsClient
	finsClient := plc.FinsClient
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

	case config.FamilyOmron:
		if finsClient == nil {
			return fmt.Errorf("Omron PLC not connected: %s", plcName)
		}
		return finsClient.Write(tagName, value)

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
	if !ok || val == nil || val.Error != nil {
		return nil
	}

	// Build lookup from config
	var writable bool
	var alias string
	for _, tag := range plc.Config.Tags {
		if tag.Name == tagName {
			writable = tag.Writable
			alias = tag.Alias
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
	}

	// For S7, set Address to uppercase version of TagName
	if family == config.FamilyS7 {
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
		// Build lookup maps from config
		writableMap := make(map[string]bool)
		aliasMap := make(map[string]string)
		for _, tag := range plc.Config.Tags {
			writableMap[tag.Name] = tag.Writable
			aliasMap[tag.Name] = tag.Alias
		}
		for tagName, val := range plc.Values {
			if val != nil && val.Error == nil {
				vc := ValueChange{
					PLCName:  plcName,
					TagName:  tagName,
					Alias:    aliasMap[tagName],
					TypeName: val.TypeName(),
					Value:    val.GoValue(),
					Writable: writableMap[tagName],
					Family:   string(family),
				}
				// For S7, set Address to uppercase version of TagName
				if family == config.FamilyS7 {
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
	finsClient := plc.FinsClient
	status := plc.Status
	family := plc.Config.GetFamily()
	// Build type hints map from config
	typeMap := make(map[string]string)
	for _, sel := range plc.Config.Tags {
		if sel.DataType != "" {
			typeMap[sel.Name] = sel.DataType
		}
	}
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
		// Build requests with type hints
		requests := make([]s7.TagRequest, len(tagNames))
		for i, name := range tagNames {
			requests[i] = s7.TagRequest{Address: name, TypeHint: typeMap[name]}
		}
		s7Values, err := s7Client.ReadWithTypes(requests)
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

	case config.FamilyOmron:
		if finsClient == nil {
			return nil, fmt.Errorf("FINS client not available")
		}
		// Build requests with type hints
		requests := make([]fins.TagRequest, len(tagNames))
		for i, name := range tagNames {
			requests[i] = fins.TagRequest{Address: name, TypeHint: typeMap[name]}
		}
		finsValues, err := finsClient.ReadWithTypes(requests)
		if err != nil {
			return nil, err
		}
		for _, v := range finsValues {
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
