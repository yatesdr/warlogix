package tagpack

import (
	"encoding/json"
	"net/url"
	"sync"
	"time"

	"warlogix/config"
	"warlogix/logging"
)

const (
	// DebounceMs is the debounce time for pack publishing (250ms).
	DebounceMs = 250
)

// PLCDataProvider provides access to PLC tag values and metadata.
type PLCDataProvider interface {
	// GetTagValue returns the current value, type name, and alias for a tag.
	// Returns nil value if tag not found or has error.
	GetTagValue(plcName, tagName string) (value interface{}, typeName, alias string, ok bool)
	// GetPLCMetadata returns metadata about a PLC.
	GetPLCMetadata(plcName string) PLCMetadata
}

// PublishCallback is called when a pack should be published.
type PublishCallback func(pv PackValue, cfg *config.TagPackConfig)

// Manager manages TagPack change detection and debounced publishing.
type Manager struct {
	mu          sync.RWMutex
	packs       map[string]*config.TagPackConfig // name -> config
	provider    PLCDataProvider
	onPublish   PublishCallback
	config      *config.Config // Reference to config for dynamic lookup

	// Debounce tracking
	pendingMu   sync.Mutex
	pending     map[string]time.Time // pack name -> first trigger time
	stopChan    chan struct{}
	wg          sync.WaitGroup

	logFn func(format string, args ...interface{})
}

// NewManager creates a new TagPack manager.
func NewManager(cfg *config.Config, provider PLCDataProvider) *Manager {
	m := &Manager{
		packs:    make(map[string]*config.TagPackConfig),
		pending:  make(map[string]time.Time),
		provider: provider,
		config:   cfg,
		stopChan: make(chan struct{}),
	}

	// Load packs from config
	for i := range cfg.TagPacks {
		m.packs[cfg.TagPacks[i].Name] = &cfg.TagPacks[i]
	}

	// Start debounce goroutine
	m.wg.Add(1)
	go m.debounceLoop()

	return m
}

// SetLogFunc sets the logging callback.
func (m *Manager) SetLogFunc(fn func(format string, args ...interface{})) {
	m.mu.Lock()
	m.logFn = fn
	m.mu.Unlock()
}

func (m *Manager) log(format string, args ...interface{}) {
	m.mu.RLock()
	fn := m.logFn
	m.mu.RUnlock()
	if fn != nil {
		fn("[TagPack] "+format, args...)
	}
	logging.DebugLog("tagpack", format, args...)
}

// SetOnPublish sets the publish callback.
func (m *Manager) SetOnPublish(callback PublishCallback) {
	m.mu.Lock()
	m.onPublish = callback
	m.mu.Unlock()
}

// Reload reloads pack configurations from config.
func (m *Manager) Reload() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.packs = make(map[string]*config.TagPackConfig)
	for i := range m.config.TagPacks {
		m.packs[m.config.TagPacks[i].Name] = &m.config.TagPacks[i]
	}
}

// Stop stops the manager and waits for goroutines to finish.
func (m *Manager) Stop() {
	close(m.stopChan)
	m.wg.Wait()
}

// OnTagChanges is called when tags change. It checks if any trigger-enabled
// pack members changed and schedules publishing.
func (m *Manager) OnTagChanges(plcName string, changedTags []string) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Build a set of changed tags for quick lookup
	changed := make(map[string]bool)
	for _, tag := range changedTags {
		changed[tag] = true
	}

	// Check each pack to see if any non-ignored member changed
	for name, cfg := range m.packs {
		if !cfg.Enabled {
			continue
		}

		for _, member := range cfg.Members {
			// Trigger if member changed and is not ignored (default behavior is to trigger)
			if member.PLC == plcName && !member.IgnoreChanges && changed[member.Tag] {
				// This pack should be triggered
				m.schedulePack(name)
				break // Only need to trigger once per pack
			}
		}
	}
}

// schedulePack schedules a pack for publishing after debounce period.
func (m *Manager) schedulePack(packName string) {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()

	if _, exists := m.pending[packName]; !exists {
		m.pending[packName] = time.Now()
		m.log("Pack %s triggered, debouncing", packName)
	}
}

// debounceLoop checks for pending packs and publishes them after debounce period.
func (m *Manager) debounceLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			return
		case <-ticker.C:
			m.checkPendingPacks()
		}
	}
}

// checkPendingPacks publishes any packs that have passed their debounce period.
func (m *Manager) checkPendingPacks() {
	m.pendingMu.Lock()
	now := time.Now()
	var toPublish []string

	for name, triggerTime := range m.pending {
		if now.Sub(triggerTime) >= time.Duration(DebounceMs)*time.Millisecond {
			toPublish = append(toPublish, name)
		}
	}

	// Remove from pending before publishing to allow re-triggering
	for _, name := range toPublish {
		delete(m.pending, name)
	}
	m.pendingMu.Unlock()

	// Publish each ready pack
	for _, name := range toPublish {
		m.publishPack(name)
	}
}

// PublishPackImmediate publishes a pack immediately, bypassing debounce.
// Used when a trigger fires with PublishPack set.
func (m *Manager) PublishPackImmediate(packName string) {
	// Remove from pending if it was debouncing
	m.pendingMu.Lock()
	delete(m.pending, packName)
	m.pendingMu.Unlock()

	m.publishPack(packName)
}

// publishPack collects all member values and calls the publish callback.
func (m *Manager) publishPack(packName string) {
	m.mu.RLock()
	cfg := m.packs[packName]
	callback := m.onPublish
	provider := m.provider
	m.mu.RUnlock()

	if cfg == nil || !cfg.Enabled {
		return
	}
	if callback == nil || provider == nil {
		return
	}

	// Build the pack value with flat "plc.tag" keys
	pv := PackValue{
		Name:      packName,
		Timestamp: time.Now().UTC(),
		Tags:      make(map[string]TagData),
		PLCs:      make(map[string]PLCMetadata),
	}

	// Track PLCs with errors for metadata inclusion
	plcErrors := make(map[string]bool)

	// Collect all member values - include all members even if no value yet
	for _, member := range cfg.Members {
		value, typeName, alias, ok := provider.GetTagValue(member.PLC, member.Tag)

		// Use alias in key if available, otherwise use tag name
		// Store original tag name/address as offset when using alias
		tagPart := member.Tag
		offset := ""
		if alias != "" {
			tagPart = alias
			offset = member.Tag
		}

		// Build flat key: "plc.tag"
		key := member.PLC + "." + tagPart

		if ok {
			pv.Tags[key] = TagData{
				Value:  value,
				Type:   typeName,
				PLC:    member.PLC,
				Offset: offset,
			}
		} else {
			// Tag has no value - include with null value and note PLC may have issues
			pv.Tags[key] = TagData{
				Value:  nil,
				Type:   "",
				PLC:    member.PLC,
				Offset: offset,
			}
			plcErrors[member.PLC] = true
		}
	}

	// Only include PLC metadata if there are connection issues
	for plcName := range plcErrors {
		meta := provider.GetPLCMetadata(plcName)
		if !meta.Connected || meta.Error != "" {
			pv.PLCs[plcName] = meta
		}
	}

	// If no errors, set PLCs to nil so it's omitted from JSON
	if len(pv.PLCs) == 0 {
		pv.PLCs = nil
	}

	m.log("Publishing pack %s with %d tags", packName, len(pv.Tags))

	// Call the publish callback
	callback(pv, cfg)
}

// GetPackValue returns the current value of a pack without publishing.
// Used for REST API.
func (m *Manager) GetPackValue(packName string) *PackValue {
	m.mu.RLock()
	cfg := m.packs[packName]
	provider := m.provider
	m.mu.RUnlock()

	if cfg == nil || provider == nil {
		return nil
	}

	pv := &PackValue{
		Name:      packName,
		Timestamp: time.Now().UTC(),
		Tags:      make(map[string]TagData),
		PLCs:      make(map[string]PLCMetadata),
	}

	plcErrors := make(map[string]bool)

	for _, member := range cfg.Members {
		value, typeName, alias, ok := provider.GetTagValue(member.PLC, member.Tag)

		// Use alias in key if available, otherwise use tag name
		// Store original tag name/address as offset when using alias
		tagPart := member.Tag
		offset := ""
		if alias != "" {
			tagPart = alias
			offset = member.Tag
		}

		// Build flat key: "plc.tag"
		key := member.PLC + "." + tagPart

		if ok {
			pv.Tags[key] = TagData{
				Value:  value,
				Type:   typeName,
				PLC:    member.PLC,
				Offset: offset,
			}
		} else {
			pv.Tags[key] = TagData{
				Value:  nil,
				Type:   "",
				PLC:    member.PLC,
				Offset: offset,
			}
			plcErrors[member.PLC] = true
		}
	}

	// Only include PLC metadata if there are connection issues
	for plcName := range plcErrors {
		meta := provider.GetPLCMetadata(plcName)
		if !meta.Connected || meta.Error != "" {
			pv.PLCs[plcName] = meta
		}
	}

	if len(pv.PLCs) == 0 {
		pv.PLCs = nil
	}

	return pv
}

// ListPacks returns the names and enabled status of all packs.
func (m *Manager) ListPacks() []PackInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []PackInfo
	for _, cfg := range m.packs {
		result = append(result, PackInfo{
			Name:    cfg.Name,
			Enabled: cfg.Enabled,
			Topic:   cfg.Topic,
			Members: len(cfg.Members),
			URL:     "/tagpack/" + url.PathEscape(cfg.Name),
		})
	}
	return result
}

// PackInfo contains summary information about a pack.
type PackInfo struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Topic   string `json:"topic"`
	Members int    `json:"members"`
	URL     string `json:"url"`
}

// MarshalPackValue serializes a PackValue to JSON.
func MarshalPackValue(pv PackValue) ([]byte, error) {
	return json.Marshal(pv)
}
