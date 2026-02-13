package www

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"warlink/ads"
	"warlink/config"
	"warlink/driver"
	kafkapkg "warlink/kafka"
	"warlink/logix"
	"warlink/mqtt"
	"warlink/omron"
	"warlink/push"
	"warlink/s7"
	"warlink/tui"
)

// htmx partial handlers for live updates

// handlePLCsPartial returns the PLCs table partial.
func (h *Handlers) handlePLCsPartial(w http.ResponseWriter, r *http.Request) {
	data := h.getUserInfo(r)
	data["PLCs"] = h.getPLCsData()
	h.renderTemplate(w, "plcs_table.html", data)
}

// handleRepublisherPartial returns the republisher tree partial.
func (h *Handlers) handleRepublisherPartial(w http.ResponseWriter, r *http.Request) {
	data := h.getUserInfo(r)
	data["PLCs"] = h.getRepublisherData()
	h.renderTemplate(w, "republisher_tree.html", data)
}

// handleMQTTPartial returns the MQTT brokers table partial.
func (h *Handlers) handleMQTTPartial(w http.ResponseWriter, r *http.Request) {
	data := h.getUserInfo(r)
	data["Brokers"] = h.getMQTTData()
	h.renderTemplate(w, "mqtt_table.html", data)
}

// handleValkeyPartial returns the Valkey servers table partial.
func (h *Handlers) handleValkeyPartial(w http.ResponseWriter, r *http.Request) {
	data := h.getUserInfo(r)
	data["Servers"] = h.getValkeyData()
	h.renderTemplate(w, "valkey_table.html", data)
}

// handleKafkaPartial returns the Kafka clusters table partial.
func (h *Handlers) handleKafkaPartial(w http.ResponseWriter, r *http.Request) {
	data := h.getUserInfo(r)
	data["Clusters"] = h.getKafkaData()
	h.renderTemplate(w, "kafka_table.html", data)
}

// handleTagPacksPartial returns the TagPacks table partial.
func (h *Handlers) handleTagPacksPartial(w http.ResponseWriter, r *http.Request) {
	data := h.getUserInfo(r)
	data["TagPacks"] = h.getTagPacksData()
	h.renderTemplate(w, "tagpacks_table.html", data)
}

// handleTriggersPartial returns the Triggers table partial.
func (h *Handlers) handleTriggersPartial(w http.ResponseWriter, r *http.Request) {
	data := h.getUserInfo(r)
	data["Triggers"] = h.getTriggersData()
	h.renderTemplate(w, "triggers_table.html", data)
}

// handleDebugPartial returns the debug log partial.
func (h *Handlers) handleDebugPartial(w http.ResponseWriter, r *http.Request) {
	data := make(map[string]interface{})
	data["Logs"] = h.getDebugLogs()
	h.renderTemplate(w, "debug_log.html", data)
}

// Action handlers (admin only)

// handlePLCConnect connects a PLC.
func (h *Handlers) handlePLCConnect(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	plcMan := h.managers.GetPLCMan()

	// Restore auto-connect (may have been cleared by manual disconnect)
	if plc := plcMan.GetPLC(name); plc != nil {
		plc.Config.Enabled = true
	}

	// Persist enabled state
	cfg := h.managers.GetConfig()
	cfg.Lock()
	if plcCfg := cfg.FindPLC(name); plcCfg != nil {
		plcCfg.Enabled = true
		cfg.UnlockAndSave(h.managers.GetConfigPath())
	} else {
		cfg.Unlock()
	}

	if err := plcMan.Connect(name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return updated partial
	h.handlePLCsPartial(w, r)
}

// handlePLCDisconnect disconnects a PLC.
func (h *Handlers) handlePLCDisconnect(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	plcMan := h.managers.GetPLCMan()

	// Clear auto-connect so plcman won't auto-reconnect
	if plc := plcMan.GetPLC(name); plc != nil {
		plc.Config.Enabled = false
	}

	// Persist disabled state
	cfg := h.managers.GetConfig()
	cfg.Lock()
	if plcCfg := cfg.FindPLC(name); plcCfg != nil {
		plcCfg.Enabled = false
		cfg.UnlockAndSave(h.managers.GetConfigPath())
	} else {
		cfg.Unlock()
	}

	plcMan.Disconnect(name)

	// Return updated partial
	h.handlePLCsPartial(w, r)
}

// handleMQTTStart starts an MQTT publisher.
func (h *Handlers) handleMQTTStart(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	mqttMgr := h.managers.GetMQTTMgr()
	pub := mqttMgr.Get(name)
	if pub == nil {
		http.Error(w, "Publisher not found", http.StatusNotFound)
		return
	}

	if err := pub.Start(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// handleMQTTStop stops an MQTT publisher.
func (h *Handlers) handleMQTTStop(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	mqttMgr := h.managers.GetMQTTMgr()
	pub := mqttMgr.Get(name)
	if pub != nil {
		pub.Stop()
	}

	w.WriteHeader(http.StatusOK)
}

// handleValkeyStart starts a Valkey publisher.
func (h *Handlers) handleValkeyStart(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	valkeyMgr := h.managers.GetValkeyMgr()
	pub := valkeyMgr.Get(name)
	if pub == nil {
		http.Error(w, "Publisher not found", http.StatusNotFound)
		return
	}

	if err := pub.Start(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// handleValkeyStop stops a Valkey publisher.
func (h *Handlers) handleValkeyStop(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	valkeyMgr := h.managers.GetValkeyMgr()
	pub := valkeyMgr.Get(name)
	if pub != nil {
		pub.Stop()
	}

	w.WriteHeader(http.StatusOK)
}

// handleKafkaConnect connects a Kafka cluster.
func (h *Handlers) handleKafkaConnect(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	kafkaMgr := h.managers.GetKafkaMgr()
	if err := kafkaMgr.Connect(name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// handleKafkaDisconnect disconnects a Kafka cluster.
func (h *Handlers) handleKafkaDisconnect(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	kafkaMgr := h.managers.GetKafkaMgr()
	kafkaMgr.Disconnect(name)

	w.WriteHeader(http.StatusOK)
}

// handleTriggerStart starts a trigger.
func (h *Handlers) handleTriggerStart(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	triggerMgr := h.managers.GetTriggerMgr()
	if err := triggerMgr.StartTrigger(name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// handleTriggerStop stops a trigger.
func (h *Handlers) handleTriggerStop(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	triggerMgr := h.managers.GetTriggerMgr()
	triggerMgr.StopTrigger(name)

	w.WriteHeader(http.StatusOK)
}

// PLCUpdateRequest holds the fields for updating a PLC.
type PLCUpdateRequest struct {
	Address            string `json:"address"`
	Slot               int    `json:"slot"`
	Family             string `json:"family"`
	Enabled            bool   `json:"enabled"`
	HealthCheckEnabled *bool  `json:"health_check_enabled"`
	DiscoverTags       *bool  `json:"discover_tags"`
	PollRate           string `json:"poll_rate"`
	Timeout            string `json:"timeout"`
	AmsNetId           string `json:"ams_net_id"`
	AmsPort            int    `json:"ams_port"`
	Protocol           string `json:"protocol"`
	FinsPort           int    `json:"fins_port"`
	FinsNetwork        int    `json:"fins_network"`
	FinsNode           int    `json:"fins_node"`
	FinsUnit           int    `json:"fins_unit"`
}

// handlePLCUpdate updates a PLC configuration.
func (h *Handlers) handlePLCUpdate(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	var req PLCUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	cfg := h.managers.GetConfig()
	cfg.Lock()
	plcCfg := cfg.FindPLC(name)
	if plcCfg == nil {
		cfg.Unlock()
		http.Error(w, "PLC not found", http.StatusNotFound)
		return
	}

	// Update configuration
	plcCfg.Address = req.Address
	plcCfg.Slot = byte(req.Slot)
	plcCfg.Enabled = req.Enabled
	plcCfg.HealthCheckEnabled = req.HealthCheckEnabled

	if req.Family != "" {
		plcCfg.Family = config.PLCFamily(req.Family)
	}

	if req.PollRate != "" {
		if d, err := time.ParseDuration(req.PollRate); err == nil {
			plcCfg.PollRate = d
		}
	} else {
		plcCfg.PollRate = 0
	}

	if req.Timeout != "" {
		if d, err := time.ParseDuration(req.Timeout); err == nil {
			plcCfg.Timeout = d
		}
	} else {
		plcCfg.Timeout = 0
	}

	// Beckhoff fields
	plcCfg.AmsNetId = req.AmsNetId
	plcCfg.AmsPort = uint16(req.AmsPort)

	// Omron fields
	plcCfg.Protocol = req.Protocol
	plcCfg.FinsPort = req.FinsPort
	plcCfg.FinsNetwork = byte(req.FinsNetwork)
	plcCfg.FinsNode = byte(req.FinsNode)
	plcCfg.FinsUnit = byte(req.FinsUnit)

	// Discover tags
	plcCfg.DiscoverTags = req.DiscoverTags

	// Save config
	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	h.eventHub.BroadcastEntityChange("plc", "update", name)
	w.WriteHeader(http.StatusOK)
}

// handlePLCGet returns PLC configuration as JSON.
func (h *Handlers) handlePLCGet(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	cfg := h.managers.GetConfig()
	plcCfg := cfg.FindPLC(name)
	if plcCfg == nil {
		http.Error(w, "PLC not found", http.StatusNotFound)
		return
	}

	resp := map[string]interface{}{
		"name":                 plcCfg.Name,
		"address":              plcCfg.Address,
		"slot":                 plcCfg.Slot,
		"family":               plcCfg.GetFamily().String(),
		"enabled":              plcCfg.Enabled,
		"health_check_enabled": plcCfg.HealthCheckEnabled == nil || *plcCfg.HealthCheckEnabled,
		"discover_tags":        plcCfg.SupportsDiscovery(),
		"poll_rate":            plcCfg.PollRate.String(),
		"timeout":             plcCfg.Timeout.String(),
		"ams_net_id":           plcCfg.AmsNetId,
		"ams_port":             plcCfg.AmsPort,
		"protocol":             plcCfg.Protocol,
		"fins_port":            plcCfg.FinsPort,
		"fins_network":         plcCfg.FinsNetwork,
		"fins_node":            plcCfg.FinsNode,
		"fins_unit":            plcCfg.FinsUnit,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// PLCCreateRequest holds the fields for creating a new PLC.
type PLCCreateRequest struct {
	Name               string `json:"name"`
	Address            string `json:"address"`
	Slot               int    `json:"slot"`
	Family             string `json:"family"`
	Enabled            bool   `json:"enabled"`
	HealthCheckEnabled *bool  `json:"health_check_enabled"`
	DiscoverTags       *bool  `json:"discover_tags"`
	PollRate           string `json:"poll_rate"`
	Timeout            string `json:"timeout"`
	AmsNetId           string `json:"ams_net_id"`
	AmsPort            int    `json:"ams_port"`
	Protocol           string `json:"protocol"`
	FinsPort           int    `json:"fins_port"`
	FinsNetwork        int    `json:"fins_network"`
	FinsNode           int    `json:"fins_node"`
	FinsUnit           int    `json:"fins_unit"`
}

// handlePLCCreate creates a new PLC.
func (h *Handlers) handlePLCCreate(w http.ResponseWriter, r *http.Request) {
	var req PLCCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}
	if req.Address == "" {
		http.Error(w, "Address is required", http.StatusBadRequest)
		return
	}

	cfg := h.managers.GetConfig()

	// Check if PLC already exists
	if cfg.FindPLC(req.Name) != nil {
		http.Error(w, "PLC with this name already exists", http.StatusConflict)
		return
	}

	// Create new PLC config
	plcCfg := config.PLCConfig{
		Name:               req.Name,
		Address:            req.Address,
		Slot:               byte(req.Slot),
		Enabled:            req.Enabled,
		HealthCheckEnabled: req.HealthCheckEnabled,
		DiscoverTags:       req.DiscoverTags,
	}

	if req.Family != "" {
		plcCfg.Family = config.PLCFamily(req.Family)
	}

	// Parse poll rate
	if req.PollRate != "" {
		if d, err := time.ParseDuration(req.PollRate); err == nil {
			plcCfg.PollRate = d
		}
	}

	// Parse timeout
	if req.Timeout != "" {
		if d, err := time.ParseDuration(req.Timeout); err == nil {
			plcCfg.Timeout = d
		}
	}

	// Beckhoff fields
	plcCfg.AmsNetId = req.AmsNetId
	if req.AmsPort > 0 {
		plcCfg.AmsPort = uint16(req.AmsPort)
	}

	// Omron fields
	plcCfg.Protocol = req.Protocol
	if req.FinsPort > 0 {
		plcCfg.FinsPort = req.FinsPort
	}
	plcCfg.FinsNetwork = byte(req.FinsNetwork)
	plcCfg.FinsNode = byte(req.FinsNode)
	plcCfg.FinsUnit = byte(req.FinsUnit)

	// Add to config
	cfg.Lock()
	cfg.PLCs = append(cfg.PLCs, plcCfg)

	// Save config
	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Add to running manager
	plcMan := h.managers.GetPLCMan()
	if err := plcMan.AddPLC(&cfg.PLCs[len(cfg.PLCs)-1]); err != nil {
		// Log but don't fail - config is saved
		http.Error(w, "PLC created but failed to add to manager: "+err.Error(), http.StatusInternalServerError)
		return
	}

	h.eventHub.BroadcastEntityChange("plc", "add", req.Name)
	w.WriteHeader(http.StatusCreated)
}

// handlePLCDelete deletes a PLC.
func (h *Handlers) handlePLCDelete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	cfg := h.managers.GetConfig()
	plcMan := h.managers.GetPLCMan()

	// Find and remove PLC from config
	cfg.Lock()
	found := false
	newPLCs := make([]config.PLCConfig, 0, len(cfg.PLCs))
	for _, plc := range cfg.PLCs {
		if plc.Name == name {
			found = true
		} else {
			newPLCs = append(newPLCs, plc)
		}
	}

	if !found {
		cfg.Unlock()
		http.Error(w, "PLC not found", http.StatusNotFound)
		return
	}

	// Remove from running manager (this also disconnects)
	_ = plcMan.RemovePLC(name)

	cfg.PLCs = newPLCs

	// Save config
	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	h.eventHub.BroadcastEntityChange("plc", "remove", name)
	w.WriteHeader(http.StatusOK)
}

// TagUpdateRequest holds the fields for updating a tag.
type TagUpdateRequest struct {
	Enabled      *bool    `json:"enabled,omitempty"`
	Writable     *bool    `json:"writable,omitempty"`
	AddIgnore    []string `json:"add_ignore,omitempty"`    // Paths to add to IgnoreChanges
	RemoveIgnore []string `json:"remove_ignore,omitempty"` // Paths to remove from IgnoreChanges
}

// TagCreateRequest holds the fields for creating/updating a tag via PUT.
type TagCreateRequest struct {
	Enabled  bool   `json:"enabled"`
	Writable bool   `json:"writable"`
	DataType string `json:"data_type"`
	Alias    string `json:"alias"`
}

// handleTagRead reads a tag's current value on demand.
// Falls back to cached values when the PLC is offline.
func (h *Handlers) handleTagRead(w http.ResponseWriter, r *http.Request) {
	plcName := chi.URLParam(r, "plc")
	plcName, _ = url.PathUnescape(plcName)
	tagName := chi.URLParam(r, "tag")
	tagName, _ = url.PathUnescape(tagName)

	plcMan := h.managers.GetPLCMan()
	tv, err := plcMan.ReadTag(plcName, tagName)
	if err != nil {
		// Fall back to cached value if PLC is offline
		plc := plcMan.GetPLC(plcName)
		if plc != nil {
			values := plc.GetValues()
			if cached, ok := values[tagName]; ok && cached != nil {
				tv = cached
				err = nil
			}
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	resp := map[string]interface{}{
		"value": tv.GoValue(),
		"type":  tv.TypeName(),
	}

	// Include member types for struct values so the UI shows correct PLC types
	if _, ok := tv.GoValue().(map[string]interface{}); ok && logix.IsStructure(tv.DataType) {
		plc := plcMan.GetPLC(plcName)
		if plc != nil {
			if drv := plc.GetDriver(); drv != nil {
				if adapter, ok := drv.(*driver.LogixAdapter); ok {
					if types := adapter.GetMemberTypes(tv.DataType); types != nil {
						resp["member_types"] = types
					}
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleTagUpdate updates a tag's configuration.
func (h *Handlers) handleTagUpdate(w http.ResponseWriter, r *http.Request) {
	plcName := chi.URLParam(r, "plc")
	plcName, _ = url.PathUnescape(plcName)
	tagName := chi.URLParam(r, "tag")
	tagName, _ = url.PathUnescape(tagName)

	var req TagUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	cfg := h.managers.GetConfig()
	cfg.Lock()
	plcCfg := cfg.FindPLC(plcName)
	if plcCfg == nil {
		cfg.Unlock()
		http.Error(w, "PLC not found", http.StatusNotFound)
		return
	}

	// Find the tag
	var tagFound bool
	for i := range plcCfg.Tags {
		if plcCfg.Tags[i].Name == tagName {
			if req.Enabled != nil {
				plcCfg.Tags[i].Enabled = *req.Enabled
			}
			if req.Writable != nil {
				plcCfg.Tags[i].Writable = *req.Writable
			}
			// Handle ignore list modifications
			if len(req.AddIgnore) > 0 {
				for _, path := range req.AddIgnore {
					// Check if already in list
					found := false
					for _, existing := range plcCfg.Tags[i].IgnoreChanges {
						if existing == path {
							found = true
							break
						}
					}
					if !found {
						plcCfg.Tags[i].IgnoreChanges = append(plcCfg.Tags[i].IgnoreChanges, path)
					}
				}
			}
			if len(req.RemoveIgnore) > 0 {
				newList := make([]string, 0)
				for _, existing := range plcCfg.Tags[i].IgnoreChanges {
					keep := true
					for _, remove := range req.RemoveIgnore {
						if existing == remove {
							keep = false
							break
						}
					}
					if keep {
						newList = append(newList, existing)
					}
				}
				plcCfg.Tags[i].IgnoreChanges = newList
			}
			tagFound = true
			break
		}
	}

	if !tagFound {
		// Auto-create the tag entry with defaults, then apply requested changes
		newTag := config.TagSelection{Name: tagName}
		if req.Enabled != nil {
			newTag.Enabled = *req.Enabled
		}
		if req.Writable != nil {
			newTag.Writable = *req.Writable
		}
		if len(req.AddIgnore) > 0 {
			newTag.IgnoreChanges = req.AddIgnore
		}
		plcCfg.Tags = append(plcCfg.Tags, newTag)
	}

	// Save config
	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Only rebuild manual tags when a new tag was auto-created.
	// Toggling enabled/writable/ignore on existing tags doesn't change the tag list.
	if !tagFound {
		h.managers.GetPLCMan().RefreshManualTags(plcName)
	}

	// Broadcast config-change so the web UI updates indicators without a full tree refresh.
	// Find the final tag state to broadcast.
	for _, sel := range plcCfg.Tags {
		if sel.Name == tagName {
			h.eventHub.BroadcastConfigChange(plcName, tagName, sel.Enabled, sel.Writable, sel.IgnoreChanges)
			break
		}
	}
	w.WriteHeader(http.StatusOK)
}

// handleTagPut creates or updates a tag (used for adding child tags as separate entries).
func (h *Handlers) handleTagPut(w http.ResponseWriter, r *http.Request) {
	plcName := chi.URLParam(r, "plc")
	plcName, _ = url.PathUnescape(plcName)
	tagName := chi.URLParam(r, "tag")
	tagName, _ = url.PathUnescape(tagName)

	var req TagCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	cfg := h.managers.GetConfig()
	cfg.Lock()
	plcCfg := cfg.FindPLC(plcName)
	if plcCfg == nil {
		cfg.Unlock()
		http.Error(w, "PLC not found", http.StatusNotFound)
		return
	}

	// Check if tag already exists
	tagFound := false
	for i := range plcCfg.Tags {
		if plcCfg.Tags[i].Name == tagName {
			// Update existing tag
			plcCfg.Tags[i].Enabled = req.Enabled
			plcCfg.Tags[i].Writable = req.Writable
			plcCfg.Tags[i].DataType = req.DataType
			plcCfg.Tags[i].Alias = req.Alias
			tagFound = true
			break
		}
	}

	if !tagFound {
		// Add new tag
		plcCfg.Tags = append(plcCfg.Tags, config.TagSelection{
			Name:     tagName,
			DataType: req.DataType,
			Alias:    req.Alias,
			Enabled:  req.Enabled,
			Writable: req.Writable,
		})
	}

	// Save config
	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Only rebuild manual tags when a new tag was added.
	// Toggling enabled/writable on existing tags doesn't change the tag list.
	if !tagFound {
		h.managers.GetPLCMan().RefreshManualTags(plcName)
	}

	// Broadcast appropriate SSE event:
	// - Child tags (dotted paths like "Parent.Member"): config-change only (no tree refresh)
	// - New root tags: entity-change (triggers tree refresh to show new tag)
	// - Existing root tag updates: config-change only
	isChildTag := strings.Contains(tagName, ".")
	if isChildTag || tagFound {
		h.eventHub.BroadcastConfigChange(plcName, tagName, req.Enabled, req.Writable, nil)
	} else {
		h.eventHub.BroadcastEntityChange("plc", "update", plcName)
	}
	w.WriteHeader(http.StatusOK)
}

// handleTagDelete deletes a manually configured tag from a PLC.
func (h *Handlers) handleTagDelete(w http.ResponseWriter, r *http.Request) {
	plcName := chi.URLParam(r, "plc")
	plcName, _ = url.PathUnescape(plcName)
	tagName := chi.URLParam(r, "tag")
	tagName, _ = url.PathUnescape(tagName)

	cfg := h.managers.GetConfig()
	cfg.Lock()
	plcCfg := cfg.FindPLC(plcName)
	if plcCfg == nil {
		cfg.Unlock()
		http.Error(w, "PLC not found", http.StatusNotFound)
		return
	}

	// Find and remove the tag
	found := false
	for i, tag := range plcCfg.Tags {
		if tag.Name == tagName {
			plcCfg.Tags = append(plcCfg.Tags[:i], plcCfg.Tags[i+1:]...)
			found = true
			break
		}
	}

	if !found {
		cfg.Unlock()
		http.Error(w, "Tag not found", http.StatusNotFound)
		return
	}

	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	h.managers.GetPLCMan().RefreshManualTags(plcName)
	h.eventHub.BroadcastEntityChange("plc", "update", plcName)
	w.WriteHeader(http.StatusOK)
}

// handlePLCTypeNames returns the supported type names and family metadata for a PLC.
func (h *Handlers) handlePLCTypeNames(w http.ResponseWriter, r *http.Request) {
	plcName := chi.URLParam(r, "plc")
	plcName, _ = url.PathUnescape(plcName)

	cfg := h.managers.GetConfig()
	plcCfg := cfg.FindPLC(plcName)
	if plcCfg == nil {
		http.Error(w, "PLC not found", http.StatusNotFound)
		return
	}

	family := plcCfg.GetFamily()
	var typeNames []string
	var addressBased bool
	var addressLabel string

	switch family {
	case config.FamilyS7:
		typeNames = s7.SupportedTypeNames()
		addressBased = true
		addressLabel = "DB.Offset"
	case config.FamilyOmron:
		typeNames = omron.SupportedTypeNames()
		if plcCfg.IsOmronFINS() {
			addressBased = true
			addressLabel = "Address"
		} else {
			addressLabel = "Tag Name"
		}
	case config.FamilyBeckhoff:
		typeNames = ads.SupportedTypeNames()
		addressLabel = "Tag Name"
	default:
		typeNames = logix.SupportedTypeNames()
		addressLabel = "Tag Name"
	}

	resp := map[string]interface{}{
		"family":        family.String(),
		"address_based": addressBased,
		"address_label": addressLabel,
		"types":         typeNames,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// TagWriteRequest holds a value to write to a tag.
type TagWriteRequest struct {
	Value interface{} `json:"value"`
}

// handleTagWrite writes a value to a tag on a connected PLC.
func (h *Handlers) handleTagWrite(w http.ResponseWriter, r *http.Request) {
	plcName := chi.URLParam(r, "plc")
	plcName, _ = url.PathUnescape(plcName)
	tagName := chi.URLParam(r, "tag")
	tagName, _ = url.PathUnescape(tagName)

	var req TagWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	plcMan := h.managers.GetPLCMan()
	if err := plcMan.WriteTag(plcName, tagName, req.Value); err != nil {
		http.Error(w, "Write failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// DiscoveredPLCResponse represents a discovered PLC for API response.
type DiscoveredPLCResponse struct {
	IP          string            `json:"ip"`
	Port        uint16            `json:"port"`
	Family      string            `json:"family"`
	ProductName string            `json:"product_name"`
	Protocol    string            `json:"protocol"`
	Vendor      string            `json:"vendor"`
	Extra       map[string]string `json:"extra,omitempty"`
}

// handleDiscoverPLCs performs network discovery for PLCs.
func (h *Handlers) handleDiscoverPLCs(w http.ResponseWriter, r *http.Request) {
	// Get local subnets for scanning
	subnets := driver.GetLocalSubnets()
	var scanCIDR string
	if len(subnets) > 0 {
		scanCIDR = subnets[0] // Use first subnet
	}

	// Perform discovery (5 second timeout, 50 concurrent connections)
	devices := driver.DiscoverAll("255.255.255.255", scanCIDR, 5*time.Second, 50)

	// Convert to response format
	response := make([]DiscoveredPLCResponse, 0, len(devices))
	for _, dev := range devices {
		response = append(response, DiscoveredPLCResponse{
			IP:          dev.IP.String(),
			Port:        dev.Port,
			Family:      string(dev.Family),
			ProductName: dev.ProductName,
			Protocol:    dev.Protocol,
			Vendor:      dev.Vendor,
			Extra:       dev.Extra,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleDebugClear clears the debug log.
func (h *Handlers) handleDebugClear(w http.ResponseWriter, r *http.Request) {
	store := tui.GetDebugStore()
	if store == nil {
		http.Error(w, "Debug store not available", http.StatusInternalServerError)
		return
	}
	store.Clear()
	w.WriteHeader(http.StatusOK)
}

// handleAPIToggle toggles the REST API enabled state.
func (h *Handlers) handleAPIToggle(w http.ResponseWriter, r *http.Request) {
	cfg := h.managers.GetConfig()
	cfg.Lock()
	cfg.Web.API.Enabled = !cfg.Web.API.Enabled
	enabled := cfg.Web.API.Enabled
	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"enabled": enabled})
}

// --- MQTT CRUD ---

type mqttRequest struct {
	Name     string `json:"name"`
	Broker   string `json:"broker"`
	Port     int    `json:"port"`
	ClientID string `json:"client_id"`
	Username string `json:"username"`
	Password string `json:"password"`
	Selector string `json:"selector"`
	UseTLS   bool   `json:"use_tls"`
	Enabled  bool   `json:"enabled"`
}

func (h *Handlers) handleMQTTCreate(w http.ResponseWriter, r *http.Request) {
	var req mqttRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}
	if req.Broker == "" {
		http.Error(w, "Broker address is required", http.StatusBadRequest)
		return
	}

	cfg := h.managers.GetConfig()
	if cfg.FindMQTT(req.Name) != nil {
		http.Error(w, "MQTT broker with this name already exists", http.StatusConflict)
		return
	}

	if req.Port == 0 {
		req.Port = 1883
	}

	mqttCfg := config.MQTTConfig{
		Name:     req.Name,
		Broker:   req.Broker,
		Port:     req.Port,
		ClientID: req.ClientID,
		Username: req.Username,
		Password: req.Password,
		Selector: req.Selector,
		UseTLS:   req.UseTLS,
		Enabled:  req.Enabled,
	}

	cfg.Lock()
	cfg.AddMQTT(mqttCfg)
	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Add to running manager
	mqttMgr := h.managers.GetMQTTMgr()
	pub := mqtt.NewPublisher(cfg.FindMQTT(req.Name), cfg.Namespace)
	mqttMgr.Add(pub)

	// Auto-start if enabled
	if req.Enabled {
		pub.Start()
	}

	h.eventHub.BroadcastEntityChange("mqtt", "add", req.Name)
	w.WriteHeader(http.StatusCreated)
}

func (h *Handlers) handleMQTTGet(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	cfg := h.managers.GetConfig()
	mqttCfg := cfg.FindMQTT(name)
	if mqttCfg == nil {
		http.Error(w, "MQTT broker not found", http.StatusNotFound)
		return
	}

	resp := map[string]interface{}{
		"name":         mqttCfg.Name,
		"broker":       mqttCfg.Broker,
		"port":         mqttCfg.Port,
		"client_id":    mqttCfg.ClientID,
		"username":     mqttCfg.Username,
		"selector":     mqttCfg.Selector,
		"use_tls":      mqttCfg.UseTLS,
		"enabled":      mqttCfg.Enabled,
		"has_password": mqttCfg.Password != "",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *Handlers) handleMQTTUpdate(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	var req mqttRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	cfg := h.managers.GetConfig()
	existing := cfg.FindMQTT(name)
	if existing == nil {
		http.Error(w, "MQTT broker not found", http.StatusNotFound)
		return
	}

	if req.Port == 0 {
		req.Port = 1883
	}

	// Preserve password if not provided in update (since GET omits it)
	password := req.Password
	if password == "" {
		password = existing.Password
	}

	updated := config.MQTTConfig{
		Name:     name,
		Broker:   req.Broker,
		Port:     req.Port,
		ClientID: req.ClientID,
		Username: req.Username,
		Password: password,
		Selector: req.Selector,
		UseTLS:   req.UseTLS,
		Enabled:  req.Enabled,
	}

	cfg.Lock()
	cfg.UpdateMQTT(name, updated)
	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Recreate publisher with new config
	mqttMgr := h.managers.GetMQTTMgr()
	mqttMgr.Remove(name)
	pub := mqtt.NewPublisher(cfg.FindMQTT(name), cfg.Namespace)
	mqttMgr.Add(pub)
	if req.Enabled {
		pub.Start()
	}

	h.eventHub.BroadcastEntityChange("mqtt", "update", name)
	w.WriteHeader(http.StatusOK)
}

func (h *Handlers) handleMQTTDelete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	cfg := h.managers.GetConfig()
	cfg.Lock()
	if !cfg.RemoveMQTT(name) {
		cfg.Unlock()
		http.Error(w, "MQTT broker not found", http.StatusNotFound)
		return
	}

	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Remove from running manager (also stops it)
	mqttMgr := h.managers.GetMQTTMgr()
	mqttMgr.Remove(name)

	h.eventHub.BroadcastEntityChange("mqtt", "remove", name)
	w.WriteHeader(http.StatusOK)
}

// --- Valkey CRUD ---

type valkeyRequest struct {
	Name            string `json:"name"`
	Address         string `json:"address"`
	Password        string `json:"password"`
	Database        int    `json:"database"`
	Selector        string `json:"selector"`
	KeyTTL          string `json:"key_ttl"`
	UseTLS          bool   `json:"use_tls"`
	PublishChanges  bool   `json:"publish_changes"`
	EnableWriteback bool   `json:"enable_writeback"`
	Enabled         bool   `json:"enabled"`
}

func (h *Handlers) handleValkeyCreate(w http.ResponseWriter, r *http.Request) {
	var req valkeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.Address == "" {
		http.Error(w, "Name and address are required", http.StatusBadRequest)
		return
	}

	cfg := h.managers.GetConfig()
	if cfg.FindValkey(req.Name) != nil {
		http.Error(w, "Valkey server with this name already exists", http.StatusConflict)
		return
	}

	var keyTTL time.Duration
	if req.KeyTTL != "" {
		if d, err := time.ParseDuration(req.KeyTTL); err == nil {
			keyTTL = d
		}
	}

	valkeyCfg := config.ValkeyConfig{
		Name:            req.Name,
		Address:         req.Address,
		Password:        req.Password,
		Database:        req.Database,
		Selector:        req.Selector,
		KeyTTL:          keyTTL,
		UseTLS:          req.UseTLS,
		PublishChanges:  req.PublishChanges,
		EnableWriteback: req.EnableWriteback,
		Enabled:         req.Enabled,
	}

	cfg.Lock()
	cfg.AddValkey(valkeyCfg)
	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	valkeyMgr := h.managers.GetValkeyMgr()
	pub := valkeyMgr.Add(cfg.FindValkey(req.Name), cfg.Namespace)
	if req.Enabled {
		pub.Start()
	}

	h.eventHub.BroadcastEntityChange("valkey", "add", req.Name)
	w.WriteHeader(http.StatusCreated)
}

func (h *Handlers) handleValkeyGet(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	cfg := h.managers.GetConfig()
	vc := cfg.FindValkey(name)
	if vc == nil {
		http.Error(w, "Valkey server not found", http.StatusNotFound)
		return
	}

	resp := map[string]interface{}{
		"name":             vc.Name,
		"address":          vc.Address,
		"database":         vc.Database,
		"selector":         vc.Selector,
		"use_tls":          vc.UseTLS,
		"publish_changes":  vc.PublishChanges,
		"enable_writeback": vc.EnableWriteback,
		"enabled":          vc.Enabled,
		"has_password":     vc.Password != "",
	}
	if vc.KeyTTL > 0 {
		resp["key_ttl"] = vc.KeyTTL.String()
	} else {
		resp["key_ttl"] = ""
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *Handlers) handleValkeyUpdate(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	var req valkeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	cfg := h.managers.GetConfig()
	existing := cfg.FindValkey(name)
	if existing == nil {
		http.Error(w, "Valkey server not found", http.StatusNotFound)
		return
	}

	var keyTTL time.Duration
	if req.KeyTTL != "" {
		if d, err := time.ParseDuration(req.KeyTTL); err == nil {
			keyTTL = d
		}
	}

	// Preserve password if not provided in update (since GET omits it)
	password := req.Password
	if password == "" {
		password = existing.Password
	}

	updated := config.ValkeyConfig{
		Name:            name,
		Address:         req.Address,
		Password:        password,
		Database:        req.Database,
		Selector:        req.Selector,
		KeyTTL:          keyTTL,
		UseTLS:          req.UseTLS,
		PublishChanges:  req.PublishChanges,
		EnableWriteback: req.EnableWriteback,
		Enabled:         req.Enabled,
	}

	cfg.Lock()
	cfg.UpdateValkey(name, updated)
	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	valkeyMgr := h.managers.GetValkeyMgr()
	valkeyMgr.Remove(name)
	pub := valkeyMgr.Add(cfg.FindValkey(name), cfg.Namespace)
	if req.Enabled {
		pub.Start()
	}

	h.eventHub.BroadcastEntityChange("valkey", "update", name)
	w.WriteHeader(http.StatusOK)
}

func (h *Handlers) handleValkeyDelete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	cfg := h.managers.GetConfig()
	cfg.Lock()
	if !cfg.RemoveValkey(name) {
		cfg.Unlock()
		http.Error(w, "Valkey server not found", http.StatusNotFound)
		return
	}

	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	valkeyMgr := h.managers.GetValkeyMgr()
	valkeyMgr.Remove(name)

	h.eventHub.BroadcastEntityChange("valkey", "remove", name)
	w.WriteHeader(http.StatusOK)
}

// --- Kafka CRUD ---

type kafkaRequest struct {
	Name             string   `json:"name"`
	Brokers          string   `json:"brokers"` // comma-separated
	UseTLS           bool     `json:"use_tls"`
	TLSSkipVerify    bool     `json:"tls_skip_verify"`
	SASLMechanism    string   `json:"sasl_mechanism"`
	Username         string   `json:"username"`
	Password         string   `json:"password"`
	Selector         string   `json:"selector"`
	PublishChanges   bool     `json:"publish_changes"`
	EnableWriteback  bool     `json:"enable_writeback"`
	AutoCreateTopics bool     `json:"auto_create_topics"`
	Enabled          bool     `json:"enabled"`
	BrokerList       []string `json:"broker_list,omitempty"` // alternative to comma-separated
	RequiredAcks     int      `json:"required_acks"`
	MaxRetries       int      `json:"max_retries"`
	RetryBackoff     string   `json:"retry_backoff"`
	ConsumerGroup    string   `json:"consumer_group"`
	WriteMaxAge      string   `json:"write_max_age"`
}

func (h *Handlers) parseBrokerList(req kafkaRequest) []string {
	if len(req.BrokerList) > 0 {
		return req.BrokerList
	}
	if req.Brokers == "" {
		return nil
	}
	var brokers []string
	for _, b := range strings.Split(req.Brokers, ",") {
		b = strings.TrimSpace(b)
		if b != "" {
			brokers = append(brokers, b)
		}
	}
	return brokers
}

func (h *Handlers) handleKafkaCreate(w http.ResponseWriter, r *http.Request) {
	var req kafkaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	brokers := h.parseBrokerList(req)
	if len(brokers) == 0 {
		http.Error(w, "At least one broker is required", http.StatusBadRequest)
		return
	}

	cfg := h.managers.GetConfig()
	if cfg.FindKafka(req.Name) != nil {
		http.Error(w, "Kafka cluster with this name already exists", http.StatusConflict)
		return
	}

	autoCreate := req.AutoCreateTopics
	autoCreatePtr := &autoCreate

	var retryBackoff time.Duration
	if req.RetryBackoff != "" {
		if d, err := time.ParseDuration(req.RetryBackoff); err == nil {
			retryBackoff = d
		}
	}
	var writeMaxAge time.Duration
	if req.WriteMaxAge != "" {
		if d, err := time.ParseDuration(req.WriteMaxAge); err == nil {
			writeMaxAge = d
		}
	}

	kafkaCfg := config.KafkaConfig{
		Name:             req.Name,
		Brokers:          brokers,
		UseTLS:           req.UseTLS,
		TLSSkipVerify:    req.TLSSkipVerify,
		SASLMechanism:    req.SASLMechanism,
		Username:         req.Username,
		Password:         req.Password,
		Selector:         req.Selector,
		PublishChanges:   req.PublishChanges,
		EnableWriteback:  req.EnableWriteback,
		AutoCreateTopics: autoCreatePtr,
		Enabled:          req.Enabled,
		RequiredAcks:     req.RequiredAcks,
		MaxRetries:       req.MaxRetries,
		RetryBackoff:     retryBackoff,
		ConsumerGroup:    req.ConsumerGroup,
		WriteMaxAge:      writeMaxAge,
	}

	cfg.Lock()
	cfg.AddKafka(kafkaCfg)
	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Add to running manager
	kc := cfg.FindKafka(req.Name)
	kafkaMgr := h.managers.GetKafkaMgr()
	kafkaMgr.AddCluster(h.buildKafkaRuntimeConfig(kc), cfg.Namespace)

	if req.Enabled {
		kafkaMgr.Connect(req.Name)
	}

	h.eventHub.BroadcastEntityChange("kafka", "add", req.Name)
	w.WriteHeader(http.StatusCreated)
}

func (h *Handlers) handleKafkaGet(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	cfg := h.managers.GetConfig()
	kc := cfg.FindKafka(name)
	if kc == nil {
		http.Error(w, "Kafka cluster not found", http.StatusNotFound)
		return
	}

	resp := map[string]interface{}{
		"name":               kc.Name,
		"brokers":            kc.Brokers,
		"use_tls":            kc.UseTLS,
		"tls_skip_verify":    kc.TLSSkipVerify,
		"sasl_mechanism":     kc.SASLMechanism,
		"username":           kc.Username,
		"selector":           kc.Selector,
		"publish_changes":    kc.PublishChanges,
		"enable_writeback":   kc.EnableWriteback,
		"auto_create_topics": kc.AutoCreateTopics == nil || *kc.AutoCreateTopics,
		"enabled":            kc.Enabled,
		"required_acks":      kc.RequiredAcks,
		"max_retries":        kc.MaxRetries,
		"consumer_group":     kc.ConsumerGroup,
		"has_password":       kc.Password != "",
	}
	if kc.RetryBackoff > 0 {
		resp["retry_backoff"] = kc.RetryBackoff.String()
	} else {
		resp["retry_backoff"] = ""
	}
	if kc.WriteMaxAge > 0 {
		resp["write_max_age"] = kc.WriteMaxAge.String()
	} else {
		resp["write_max_age"] = ""
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *Handlers) handleKafkaUpdate(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	var req kafkaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	brokers := h.parseBrokerList(req)
	if len(brokers) == 0 {
		http.Error(w, "At least one broker is required", http.StatusBadRequest)
		return
	}

	cfg := h.managers.GetConfig()
	if cfg.FindKafka(name) == nil {
		http.Error(w, "Kafka cluster not found", http.StatusNotFound)
		return
	}

	autoCreate := req.AutoCreateTopics
	autoCreatePtr := &autoCreate

	var retryBackoff time.Duration
	if req.RetryBackoff != "" {
		if d, err := time.ParseDuration(req.RetryBackoff); err == nil {
			retryBackoff = d
		}
	}
	var writeMaxAge time.Duration
	if req.WriteMaxAge != "" {
		if d, err := time.ParseDuration(req.WriteMaxAge); err == nil {
			writeMaxAge = d
		}
	}

	// Preserve password if not provided in update (since GET omits it)
	password := req.Password
	if password == "" {
		if existing := cfg.FindKafka(name); existing != nil {
			password = existing.Password
		}
	}

	updated := config.KafkaConfig{
		Name:             name,
		Brokers:          brokers,
		UseTLS:           req.UseTLS,
		TLSSkipVerify:    req.TLSSkipVerify,
		SASLMechanism:    req.SASLMechanism,
		Username:         req.Username,
		Password:         password,
		Selector:         req.Selector,
		PublishChanges:   req.PublishChanges,
		EnableWriteback:  req.EnableWriteback,
		AutoCreateTopics: autoCreatePtr,
		Enabled:          req.Enabled,
		RequiredAcks:     req.RequiredAcks,
		MaxRetries:       req.MaxRetries,
		RetryBackoff:     retryBackoff,
		ConsumerGroup:    req.ConsumerGroup,
		WriteMaxAge:      writeMaxAge,
	}

	cfg.Lock()
	cfg.UpdateKafka(name, updated)
	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	kafkaMgr := h.managers.GetKafkaMgr()
	kafkaMgr.RemoveCluster(name)

	kc := cfg.FindKafka(name)
	kafkaMgr.AddCluster(h.buildKafkaRuntimeConfig(kc), cfg.Namespace)

	if req.Enabled {
		kafkaMgr.Connect(name)
	}

	h.eventHub.BroadcastEntityChange("kafka", "update", name)
	w.WriteHeader(http.StatusOK)
}

func (h *Handlers) handleKafkaDelete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	cfg := h.managers.GetConfig()
	cfg.Lock()
	if !cfg.RemoveKafka(name) {
		cfg.Unlock()
		http.Error(w, "Kafka cluster not found", http.StatusNotFound)
		return
	}

	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	kafkaMgr := h.managers.GetKafkaMgr()
	kafkaMgr.RemoveCluster(name)

	h.eventHub.BroadcastEntityChange("kafka", "remove", name)
	w.WriteHeader(http.StatusOK)
}

// buildKafkaRuntimeConfig converts a config.KafkaConfig to a kafka.Config for the runtime manager.
func (h *Handlers) buildKafkaRuntimeConfig(kc *config.KafkaConfig) *kafkapkg.Config {
	return &kafkapkg.Config{
		Name:             kc.Name,
		Enabled:          kc.Enabled,
		Brokers:          kc.Brokers,
		UseTLS:           kc.UseTLS,
		TLSSkipVerify:    kc.TLSSkipVerify,
		SASLMechanism:    kafkapkg.SASLMechanism(kc.SASLMechanism),
		Username:         kc.Username,
		Password:         kc.Password,
		RequiredAcks:     kc.RequiredAcks,
		MaxRetries:       kc.MaxRetries,
		RetryBackoff:     kc.RetryBackoff,
		PublishChanges:   kc.PublishChanges,
		Selector:         kc.Selector,
		AutoCreateTopics: kc.AutoCreateTopics == nil || *kc.AutoCreateTopics,
		EnableWriteback:  kc.EnableWriteback,
		ConsumerGroup:    kc.ConsumerGroup,
		WriteMaxAge:      kc.WriteMaxAge,
	}
}

// --- TagPack CRUD ---

type tagPackRequest struct {
	Name          string               `json:"name"`
	Enabled       bool                 `json:"enabled"`
	MQTTEnabled   bool                 `json:"mqtt_enabled"`
	KafkaEnabled  bool                 `json:"kafka_enabled"`
	ValkeyEnabled bool                 `json:"valkey_enabled"`
	Members       []config.TagPackMember `json:"members,omitempty"`
}

func (h *Handlers) handleTagPackCreate(w http.ResponseWriter, r *http.Request) {
	var req tagPackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	cfg := h.managers.GetConfig()
	if cfg.FindTagPack(req.Name) != nil {
		http.Error(w, "TagPack with this name already exists", http.StatusConflict)
		return
	}

	packCfg := config.TagPackConfig{
		Name:          req.Name,
		Enabled:       req.Enabled,
		MQTTEnabled:   req.MQTTEnabled,
		KafkaEnabled:  req.KafkaEnabled,
		ValkeyEnabled: req.ValkeyEnabled,
		Members:       req.Members,
	}

	cfg.Lock()
	cfg.AddTagPack(packCfg)
	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	h.managers.GetPackMgr().Reload()

	h.eventHub.BroadcastEntityChange("tagpack", "add", req.Name)
	w.WriteHeader(http.StatusCreated)
}

func (h *Handlers) handleTagPackGet(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	cfg := h.managers.GetConfig()
	pc := cfg.FindTagPack(name)
	if pc == nil {
		http.Error(w, "TagPack not found", http.StatusNotFound)
		return
	}

	// Build members list with snake_case keys
	members := make([]map[string]interface{}, len(pc.Members))
	for i, m := range pc.Members {
		members[i] = map[string]interface{}{
			"plc":            m.PLC,
			"tag":            m.Tag,
			"ignore_changes": m.IgnoreChanges,
		}
	}

	resp := map[string]interface{}{
		"name":           pc.Name,
		"enabled":        pc.Enabled,
		"mqtt_enabled":   pc.MQTTEnabled,
		"kafka_enabled":  pc.KafkaEnabled,
		"valkey_enabled": pc.ValkeyEnabled,
		"members":        members,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *Handlers) handleTagPackUpdate(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	var req tagPackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	cfg := h.managers.GetConfig()
	if cfg.FindTagPack(name) == nil {
		http.Error(w, "TagPack not found", http.StatusNotFound)
		return
	}

	updated := config.TagPackConfig{
		Name:          name,
		Enabled:       req.Enabled,
		MQTTEnabled:   req.MQTTEnabled,
		KafkaEnabled:  req.KafkaEnabled,
		ValkeyEnabled: req.ValkeyEnabled,
		Members:       req.Members,
	}

	cfg.Lock()
	cfg.UpdateTagPack(name, updated)
	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	h.managers.GetPackMgr().Reload()

	h.eventHub.BroadcastEntityChange("tagpack", "update", name)
	w.WriteHeader(http.StatusOK)
}

func (h *Handlers) handleTagPackDelete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	cfg := h.managers.GetConfig()
	cfg.Lock()
	if !cfg.RemoveTagPack(name) {
		cfg.Unlock()
		http.Error(w, "TagPack not found", http.StatusNotFound)
		return
	}

	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	h.managers.GetPackMgr().Reload()

	h.eventHub.BroadcastEntityChange("tagpack", "remove", name)
	w.WriteHeader(http.StatusOK)
}

func (h *Handlers) handleTagPackToggle(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	cfg := h.managers.GetConfig()
	cfg.Lock()
	pc := cfg.FindTagPack(name)
	if pc == nil {
		cfg.Unlock()
		http.Error(w, "TagPack not found", http.StatusNotFound)
		return
	}

	pc.Enabled = !pc.Enabled
	enabled := pc.Enabled
	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	h.managers.GetPackMgr().Reload()

	h.eventHub.BroadcastEntityChange("tagpack", "update", name)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"enabled": enabled})
}

func (h *Handlers) handleTagPackServiceToggle(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)
	service := chi.URLParam(r, "service")

	cfg := h.managers.GetConfig()
	cfg.Lock()
	pc := cfg.FindTagPack(name)
	if pc == nil {
		cfg.Unlock()
		http.Error(w, "TagPack not found", http.StatusNotFound)
		return
	}

	var enabled bool
	switch service {
	case "mqtt":
		pc.MQTTEnabled = !pc.MQTTEnabled
		enabled = pc.MQTTEnabled
	case "kafka":
		pc.KafkaEnabled = !pc.KafkaEnabled
		enabled = pc.KafkaEnabled
	case "valkey":
		pc.ValkeyEnabled = !pc.ValkeyEnabled
		enabled = pc.ValkeyEnabled
	default:
		cfg.Unlock()
		http.Error(w, "Invalid service: "+service, http.StatusBadRequest)
		return
	}

	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	h.managers.GetPackMgr().Reload()
	h.eventHub.BroadcastEntityChange("tagpack", "update", name)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"enabled": enabled})
}

func (h *Handlers) handleTagPackAddMember(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	var member config.TagPackMember
	if err := json.NewDecoder(r.Body).Decode(&member); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if member.PLC == "" || member.Tag == "" {
		http.Error(w, "PLC and tag are required", http.StatusBadRequest)
		return
	}

	cfg := h.managers.GetConfig()
	cfg.Lock()
	pc := cfg.FindTagPack(name)
	if pc == nil {
		cfg.Unlock()
		http.Error(w, "TagPack not found", http.StatusNotFound)
		return
	}

	pc.Members = append(pc.Members, member)
	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	h.managers.GetPackMgr().Reload()
	h.eventHub.BroadcastEntityChange("tagpack", "update", name)
	w.WriteHeader(http.StatusCreated)
}

func (h *Handlers) handleTagPackRemoveMember(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)
	indexStr := chi.URLParam(r, "index")
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		http.Error(w, "Invalid member index", http.StatusBadRequest)
		return
	}

	cfg := h.managers.GetConfig()
	cfg.Lock()
	pc := cfg.FindTagPack(name)
	if pc == nil {
		cfg.Unlock()
		http.Error(w, "TagPack not found", http.StatusNotFound)
		return
	}

	if index < 0 || index >= len(pc.Members) {
		cfg.Unlock()
		http.Error(w, "Member index out of range", http.StatusBadRequest)
		return
	}

	pc.Members = append(pc.Members[:index], pc.Members[index+1:]...)
	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	h.managers.GetPackMgr().Reload()
	h.eventHub.BroadcastEntityChange("tagpack", "update", name)
	w.WriteHeader(http.StatusOK)
}

func (h *Handlers) handleTagPackToggleMemberIgnore(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)
	indexStr := chi.URLParam(r, "index")
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		http.Error(w, "Invalid member index", http.StatusBadRequest)
		return
	}

	cfg := h.managers.GetConfig()
	cfg.Lock()
	pc := cfg.FindTagPack(name)
	if pc == nil {
		cfg.Unlock()
		http.Error(w, "TagPack not found", http.StatusNotFound)
		return
	}

	if index < 0 || index >= len(pc.Members) {
		cfg.Unlock()
		http.Error(w, "Member index out of range", http.StatusBadRequest)
		return
	}

	pc.Members[index].IgnoreChanges = !pc.Members[index].IgnoreChanges
	ignoreChanges := pc.Members[index].IgnoreChanges
	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	h.managers.GetPackMgr().Reload()
	h.eventHub.BroadcastEntityChange("tagpack", "update", name)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ignore_changes": ignoreChanges})
}

// --- Trigger CRUD ---

type triggerRequest struct {
	Name         string            `json:"name"`
	Enabled      bool              `json:"enabled"`
	PLC          string            `json:"plc"`
	TriggerTag   string            `json:"trigger_tag"`
	Condition    config.TriggerCondition `json:"condition"`
	AckTag       string            `json:"ack_tag"`
	DebounceMS   int               `json:"debounce_ms"`
	Tags         []string          `json:"tags"`
	MQTTBroker   string            `json:"mqtt_broker"`
	KafkaCluster string            `json:"kafka_cluster"`
	Selector     string            `json:"selector"`
	Metadata     map[string]string `json:"metadata"`
	PublishPack  string            `json:"publish_pack"`
}

func (h *Handlers) handleTriggerCreate(w http.ResponseWriter, r *http.Request) {
	var req triggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.PLC == "" || req.TriggerTag == "" {
		http.Error(w, "Name, PLC, and trigger tag are required", http.StatusBadRequest)
		return
	}

	cfg := h.managers.GetConfig()
	if cfg.FindTrigger(req.Name) != nil {
		http.Error(w, "Trigger with this name already exists", http.StatusConflict)
		return
	}

	triggerCfg := config.TriggerConfig{
		Name:         req.Name,
		Enabled:      req.Enabled,
		PLC:          req.PLC,
		TriggerTag:   req.TriggerTag,
		Condition:    req.Condition,
		AckTag:       req.AckTag,
		DebounceMS:   req.DebounceMS,
		Tags:         req.Tags,
		MQTTBroker:   req.MQTTBroker,
		KafkaCluster: req.KafkaCluster,
		Selector:     req.Selector,
		Metadata:     req.Metadata,
		PublishPack:  req.PublishPack,
	}

	cfg.Lock()
	cfg.AddTrigger(triggerCfg)
	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	triggerMgr := h.managers.GetTriggerMgr()
	triggerMgr.AddTrigger(cfg.FindTrigger(req.Name))
	if req.Enabled {
		triggerMgr.StartTrigger(req.Name)
	}

	h.eventHub.BroadcastEntityChange("trigger", "add", req.Name)
	w.WriteHeader(http.StatusCreated)
}

func (h *Handlers) handleTriggerGet(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	cfg := h.managers.GetConfig()
	tc := cfg.FindTrigger(name)
	if tc == nil {
		http.Error(w, "Trigger not found", http.StatusNotFound)
		return
	}

	// Also get runtime status
	triggerMgr := h.managers.GetTriggerMgr()
	status, trigErr, fireCount, lastFire := triggerMgr.GetTriggerStatus(name)

	resp := map[string]interface{}{
		"name":          tc.Name,
		"enabled":       tc.Enabled,
		"plc":           tc.PLC,
		"trigger_tag":   tc.TriggerTag,
		"condition":     tc.Condition,
		"ack_tag":       tc.AckTag,
		"debounce_ms":   tc.DebounceMS,
		"tags":          tc.Tags,
		"mqtt_broker":   tc.MQTTBroker,
		"kafka_cluster": tc.KafkaCluster,
		"selector":      tc.Selector,
		"metadata":      tc.Metadata,
		"publish_pack":  tc.PublishPack,
		"status":        triggerStatusString(status),
		"fire_count":    fireCount,
	}
	if trigErr != nil {
		resp["error"] = trigErr.Error()
	}
	if !lastFire.IsZero() {
		resp["last_fire"] = lastFire.Format("2006-01-02 15:04:05")
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *Handlers) handleTriggerUpdate(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	var req triggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	cfg := h.managers.GetConfig()
	if cfg.FindTrigger(name) == nil {
		http.Error(w, "Trigger not found", http.StatusNotFound)
		return
	}

	updated := config.TriggerConfig{
		Name:         name,
		Enabled:      req.Enabled,
		PLC:          req.PLC,
		TriggerTag:   req.TriggerTag,
		Condition:    req.Condition,
		AckTag:       req.AckTag,
		DebounceMS:   req.DebounceMS,
		Tags:         req.Tags,
		MQTTBroker:   req.MQTTBroker,
		KafkaCluster: req.KafkaCluster,
		Selector:     req.Selector,
		Metadata:     req.Metadata,
		PublishPack:  req.PublishPack,
	}

	cfg.Lock()
	cfg.UpdateTrigger(name, updated)
	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	triggerMgr := h.managers.GetTriggerMgr()
	triggerMgr.UpdateTrigger(cfg.FindTrigger(name))
	if req.Enabled {
		triggerMgr.StartTrigger(name)
	}

	h.eventHub.BroadcastEntityChange("trigger", "update", name)
	w.WriteHeader(http.StatusOK)
}

func (h *Handlers) handleTriggerDelete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	cfg := h.managers.GetConfig()
	cfg.Lock()
	if !cfg.RemoveTrigger(name) {
		cfg.Unlock()
		http.Error(w, "Trigger not found", http.StatusNotFound)
		return
	}

	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	triggerMgr := h.managers.GetTriggerMgr()
	triggerMgr.RemoveTrigger(name)

	h.eventHub.BroadcastEntityChange("trigger", "remove", name)
	w.WriteHeader(http.StatusOK)
}

func (h *Handlers) handleTriggerTestFire(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	triggerMgr := h.managers.GetTriggerMgr()
	if err := triggerMgr.TestFireTrigger(name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handlers) handleTriggerAddTag(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	var req struct {
		Tag string `json:"tag"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Tag == "" {
		http.Error(w, "Tag is required", http.StatusBadRequest)
		return
	}

	cfg := h.managers.GetConfig()
	cfg.Lock()
	tc := cfg.FindTrigger(name)
	if tc == nil {
		cfg.Unlock()
		http.Error(w, "Trigger not found", http.StatusNotFound)
		return
	}

	// Check for duplicate
	for _, t := range tc.Tags {
		if t == req.Tag {
			cfg.Unlock()
			http.Error(w, "Tag already exists in trigger", http.StatusConflict)
			return
		}
	}

	tc.Tags = append(tc.Tags, req.Tag)
	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	triggerMgr := h.managers.GetTriggerMgr()
	triggerMgr.UpdateTrigger(tc)

	h.eventHub.BroadcastEntityChange("trigger", "update", name)
	w.WriteHeader(http.StatusCreated)
}

func (h *Handlers) handleTriggerRemoveTag(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)
	indexStr := chi.URLParam(r, "index")
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		http.Error(w, "Invalid tag index", http.StatusBadRequest)
		return
	}

	cfg := h.managers.GetConfig()
	cfg.Lock()
	tc := cfg.FindTrigger(name)
	if tc == nil {
		cfg.Unlock()
		http.Error(w, "Trigger not found", http.StatusNotFound)
		return
	}

	if index < 0 || index >= len(tc.Tags) {
		cfg.Unlock()
		http.Error(w, "Tag index out of range", http.StatusBadRequest)
		return
	}

	tc.Tags = append(tc.Tags[:index], tc.Tags[index+1:]...)
	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	triggerMgr := h.managers.GetTriggerMgr()
	triggerMgr.UpdateTrigger(tc)

	h.eventHub.BroadcastEntityChange("trigger", "update", name)
	w.WriteHeader(http.StatusOK)
}

// handleAvailableTags returns all available tags across all PLCs for the tag picker.
func (h *Handlers) handleAvailableTags(w http.ResponseWriter, r *http.Request) {
	plcMan := h.managers.GetPLCMan()
	plcs := plcMan.ListPLCs()

	type tagInfo struct {
		PLC  string `json:"plc"`
		Tag  string `json:"tag"`
		Type string `json:"type"`
	}

	var tags []tagInfo
	for _, plc := range plcs {
		for _, tag := range plc.GetTags() {
			tags = append(tags, tagInfo{
				PLC:  plc.Config.Name,
				Tag:  tag.Name,
				Type: tag.TypeName,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tags)
}

// plcTagEntry is used by handlePLCTags for the JSON response.
type plcTagEntry struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// handlePLCTags returns tags for a specific PLC, including flattened struct members.
func (h *Handlers) handlePLCTags(w http.ResponseWriter, r *http.Request) {
	plcName := chi.URLParam(r, "plc")
	plcName, _ = url.PathUnescape(plcName)

	plcMan := h.managers.GetPLCMan()
	plc := plcMan.GetPLC(plcName)
	if plc == nil {
		http.Error(w, "PLC not found", http.StatusNotFound)
		return
	}

	var tags []plcTagEntry

	// Start with top-level tags
	for _, tag := range plc.GetTags() {
		tags = append(tags, plcTagEntry{
			Name: tag.Name,
			Type: tag.TypeName,
		})
	}

	// Try to flatten struct members from live values
	values := plc.GetValues()
	if values != nil {
		for _, tag := range plc.GetTags() {
			tv, ok := values[tag.Name]
			if !ok || tv == nil {
				continue
			}
			val := tv.GoValue()
			if m, ok := val.(map[string]interface{}); ok {
				flattenTagValue(tag.Name, m, &tags)
			}
		}
	}

	// Sort alphabetically so parents appear before children
	sort.Slice(tags, func(i, j int) bool {
		return tags[i].Name < tags[j].Name
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tags)
}

// flattenTagValue recursively walks a map value and appends child paths with dot notation.
func flattenTagValue(prefix string, m map[string]interface{}, results *[]plcTagEntry) {
	for key, val := range m {
		path := prefix + "." + key
		typeName := goValueTypeName(val)
		*results = append(*results, plcTagEntry{Name: path, Type: typeName})

		if child, ok := val.(map[string]interface{}); ok {
			flattenTagValue(path, child, results)
		}
	}
}

// --- Push CRUD ---

type pushRequest struct {
	Name            string                `json:"name"`
	Enabled         bool                  `json:"enabled"`
	Conditions      []config.PushCondition `json:"conditions"`
	URL             string                `json:"url"`
	Method          string                `json:"method"`
	ContentType     string                `json:"content_type"`
	Headers         map[string]string     `json:"headers,omitempty"`
	Body            string                `json:"body"`
	Auth            config.PushAuthConfig `json:"auth"`
	CooldownMin     string                `json:"cooldown_min"`
	CooldownPerCond bool                  `json:"cooldown_per_condition"`
	Timeout         string                `json:"timeout"`
}

func (h *Handlers) handlePushCreate(w http.ResponseWriter, r *http.Request) {
	var req pushRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.URL == "" {
		http.Error(w, "Name and URL are required", http.StatusBadRequest)
		return
	}

	cfg := h.managers.GetConfig()
	if cfg.FindPush(req.Name) != nil {
		http.Error(w, "Push with this name already exists", http.StatusConflict)
		return
	}

	var cooldown time.Duration
	if req.CooldownMin != "" {
		if d, err := time.ParseDuration(req.CooldownMin); err == nil {
			cooldown = d
		}
	}
	var timeout time.Duration
	if req.Timeout != "" {
		if d, err := time.ParseDuration(req.Timeout); err == nil {
			timeout = d
		}
	}

	method := req.Method
	if method == "" {
		method = "POST"
	}

	conditions := req.Conditions
	if conditions == nil {
		conditions = []config.PushCondition{}
	}

	pushCfg := config.PushConfig{
		Name:            req.Name,
		Enabled:         req.Enabled,
		Conditions:      conditions,
		URL:             req.URL,
		Method:          method,
		ContentType:     req.ContentType,
		Headers:         req.Headers,
		Body:            req.Body,
		Auth:            req.Auth,
		CooldownMin:     cooldown,
		CooldownPerCond: req.CooldownPerCond,
		Timeout:         timeout,
	}

	cfg.Lock()
	cfg.AddPush(pushCfg)
	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	pushMgr := h.managers.GetPushMgr()
	if pushMgr != nil {
		pc := cfg.FindPush(req.Name)
		if pc != nil {
			pushMgr.AddPush(pc)
			if req.Enabled {
				pushMgr.StartPush(req.Name)
			}
		}
	}

	h.eventHub.BroadcastEntityChange("push", "add", req.Name)
	w.WriteHeader(http.StatusCreated)
}

func (h *Handlers) handlePushGet(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	cfg := h.managers.GetConfig()
	pc := cfg.FindPush(name)
	if pc == nil {
		http.Error(w, "Push not found", http.StatusNotFound)
		return
	}

	pushMgr := h.managers.GetPushMgr()
	var statusStr string
	var sendCount int64
	var lastSendStr string
	var lastHTTPCode int
	var errStr string

	if pushMgr != nil {
		pStatus, pErr, count, lastSend, lastCode := pushMgr.GetPushStatus(name)
		statusStr = pushStatusStr(pStatus)
		sendCount = count
		lastHTTPCode = lastCode
		if !lastSend.IsZero() {
			lastSendStr = lastSend.Format("2006-01-02 15:04:05")
		}
		if pErr != nil {
			errStr = pErr.Error()
		}
	}

	resp := map[string]interface{}{
		"name":                  pc.Name,
		"enabled":               pc.Enabled,
		"conditions":            pc.Conditions,
		"url":                   pc.URL,
		"method":                pc.Method,
		"content_type":          pc.ContentType,
		"headers":               pc.Headers,
		"body":                  pc.Body,
		"cooldown_min":          pc.CooldownMin.String(),
		"cooldown_per_condition": pc.CooldownPerCond,
		"timeout":               pc.Timeout.String(),
		"status":                statusStr,
		"send_count":            sendCount,
		"last_http_code":        lastHTTPCode,
		"auth_type":             string(pc.Auth.Type),
		"has_token":             pc.Auth.Token != "",
	}
	if lastSendStr != "" {
		resp["last_send"] = lastSendStr
	}
	if errStr != "" {
		resp["error"] = errStr
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *Handlers) handlePushUpdate(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	var req pushRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	cfg := h.managers.GetConfig()
	if cfg.FindPush(name) == nil {
		http.Error(w, "Push not found", http.StatusNotFound)
		return
	}

	var cooldown time.Duration
	if req.CooldownMin != "" {
		if d, err := time.ParseDuration(req.CooldownMin); err == nil {
			cooldown = d
		}
	}
	var timeout time.Duration
	if req.Timeout != "" {
		if d, err := time.ParseDuration(req.Timeout); err == nil {
			timeout = d
		}
	}

	method := req.Method
	if method == "" {
		method = "POST"
	}

	conditions := req.Conditions
	if conditions == nil {
		conditions = []config.PushCondition{}
	}

	updated := config.PushConfig{
		Name:            name,
		Enabled:         req.Enabled,
		Conditions:      conditions,
		URL:             req.URL,
		Method:          method,
		ContentType:     req.ContentType,
		Headers:         req.Headers,
		Body:            req.Body,
		Auth:            req.Auth,
		CooldownMin:     cooldown,
		CooldownPerCond: req.CooldownPerCond,
		Timeout:         timeout,
	}

	cfg.Lock()
	cfg.UpdatePush(name, updated)
	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	pushMgr := h.managers.GetPushMgr()
	if pushMgr != nil {
		pc := cfg.FindPush(name)
		if pc != nil {
			pushMgr.UpdatePush(pc)
			if req.Enabled {
				pushMgr.StartPush(name)
			}
		}
	}

	h.eventHub.BroadcastEntityChange("push", "update", name)
	w.WriteHeader(http.StatusOK)
}

func (h *Handlers) handlePushDelete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	cfg := h.managers.GetConfig()
	cfg.Lock()
	if !cfg.RemovePush(name) {
		cfg.Unlock()
		http.Error(w, "Push not found", http.StatusNotFound)
		return
	}

	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	pushMgr := h.managers.GetPushMgr()
	if pushMgr != nil {
		pushMgr.RemovePush(name)
	}

	h.eventHub.BroadcastEntityChange("push", "remove", name)
	w.WriteHeader(http.StatusOK)
}

func (h *Handlers) handlePushStart(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	pushMgr := h.managers.GetPushMgr()
	if pushMgr == nil {
		http.Error(w, "Push manager not available", http.StatusInternalServerError)
		return
	}

	if err := pushMgr.StartPush(name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handlers) handlePushStop(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	pushMgr := h.managers.GetPushMgr()
	if pushMgr == nil {
		http.Error(w, "Push manager not available", http.StatusInternalServerError)
		return
	}

	pushMgr.StopPush(name)
	w.WriteHeader(http.StatusOK)
}

func (h *Handlers) handlePushTestFire(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	pushMgr := h.managers.GetPushMgr()
	if pushMgr == nil {
		http.Error(w, "Push manager not available", http.StatusInternalServerError)
		return
	}

	if err := pushMgr.TestFirePush(name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// handlePushPartial returns the push list partial.
func (h *Handlers) handlePushPartial(w http.ResponseWriter, r *http.Request) {
	data := h.getUserInfo(r)
	data["Pushes"] = h.getPushData()
	h.renderTemplate(w, "push_table.html", data)
}

func pushStatusStr(s push.Status) string {
	switch s {
	case push.StatusArmed:
		return "armed"
	case push.StatusFiring:
		return "firing"
	case push.StatusWaitingClear:
		return "waiting"
	case push.StatusMinInterval:
		return "cooldown"
	case push.StatusError:
		return "error"
	default:
		return "disabled"
	}
}

// goValueTypeName derives a PLC-style type name from a Go value.
func goValueTypeName(val interface{}) string {
	switch val.(type) {
	case bool:
		return "BOOL"
	case float64:
		return "REAL"
	case int64, int:
		return "DINT"
	case uint64:
		return "UDINT"
	case string:
		return "STRING"
	case map[string]interface{}:
		return "Struct"
	case []interface{}:
		return "Array"
	default:
		return fmt.Sprintf("%T", val)
	}
}

