package www

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/yatesdr/plcio/ads"
	"warlink/config"
	"github.com/yatesdr/plcio/driver"
	"warlink/engine"
	"github.com/yatesdr/plcio/logix"
	"github.com/yatesdr/plcio/omron"
	"github.com/yatesdr/plcio/pccc"
	"warlink/rule"
	"github.com/yatesdr/plcio/s7"
	"warlink/tui"
)

// writeEngineError maps engine sentinel errors to appropriate HTTP status codes.
func (h *Handlers) writeEngineError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), engine.EngineHTTPStatus(err))
}

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

// handleRulesPartial returns the Rules table partial.
func (h *Handlers) handleRulesPartial(w http.ResponseWriter, r *http.Request) {
	data := h.getUserInfo(r)
	data["Rules"] = h.getRulesData()
	h.renderTemplate(w, "rules_table.html", data)
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

	if err := h.engine.ConnectPLC(name); err != nil {
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

	h.engine.DisconnectPLC(name)

	// Return updated partial
	h.handlePLCsPartial(w, r)
}

// handleMQTTStart starts an MQTT publisher.
func (h *Handlers) handleMQTTStart(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	if err := h.engine.StartMQTT(name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// handleMQTTStop stops an MQTT publisher.
func (h *Handlers) handleMQTTStop(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	h.engine.StopMQTT(name)
	w.WriteHeader(http.StatusOK)
}

// handleValkeyStart starts a Valkey publisher.
func (h *Handlers) handleValkeyStart(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	if err := h.engine.StartValkey(name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// handleValkeyStop stops a Valkey publisher.
func (h *Handlers) handleValkeyStop(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	h.engine.StopValkey(name)
	w.WriteHeader(http.StatusOK)
}

// handleKafkaConnect connects a Kafka cluster.
func (h *Handlers) handleKafkaConnect(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	if err := h.engine.ConnectKafka(name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// handleKafkaDisconnect disconnects a Kafka cluster.
func (h *Handlers) handleKafkaDisconnect(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	h.engine.DisconnectKafka(name)
	w.WriteHeader(http.StatusOK)
}

// handleRuleStart starts a rule.
func (h *Handlers) handleRuleStart(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	if err := h.engine.StartRule(name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// handleRuleStop stops a rule.
func (h *Handlers) handleRuleStop(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	h.engine.StopRule(name)
	w.WriteHeader(http.StatusOK)
}

// handlePLCUpdate updates a PLC configuration.
func (h *Handlers) handlePLCUpdate(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	var req engine.PLCHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.engine.UpdatePLC(name, req.ToUpdateRequest()); err != nil {
		h.writeEngineError(w, err)
		return
	}

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
		"connection_path":      plcCfg.ConnectionPath,
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

// handlePLCCreate creates a new PLC.
func (h *Handlers) handlePLCCreate(w http.ResponseWriter, r *http.Request) {
	var req engine.PLCHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.engine.CreatePLC(req.ToCreateRequest()); err != nil {
		h.writeEngineError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// handlePLCDelete deletes a PLC.
func (h *Handlers) handlePLCDelete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	if err := h.engine.DeletePLC(name); err != nil {
		h.writeEngineError(w, err)
		return
	}

	h.invalidateRepubCache(name)
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

	engReq := engine.TagUpdateRequest{
		Enabled:      req.Enabled,
		Writable:     req.Writable,
		AddIgnore:    req.AddIgnore,
		RemoveIgnore: req.RemoveIgnore,
	}

	if err := h.engine.UpdateTag(plcName, tagName, engReq); err != nil {
		h.writeEngineError(w, err)
		return
	}

	h.invalidateRepubCache(plcName)
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

	_, err := h.engine.CreateOrUpdateTag(plcName, tagName, engine.TagCreateOrUpdateRequest{
		Enabled:  req.Enabled,
		Writable: req.Writable,
		DataType: req.DataType,
		Alias:    req.Alias,
	})
	if err != nil {
		h.writeEngineError(w, err)
		return
	}

	h.invalidateRepubCache(plcName)
	w.WriteHeader(http.StatusOK)
}

// handleTagDelete deletes a manually configured tag from a PLC.
func (h *Handlers) handleTagDelete(w http.ResponseWriter, r *http.Request) {
	plcName := chi.URLParam(r, "plc")
	plcName, _ = url.PathUnescape(plcName)
	tagName := chi.URLParam(r, "tag")
	tagName, _ = url.PathUnescape(tagName)

	if err := h.engine.DeleteTag(plcName, tagName); err != nil {
		h.writeEngineError(w, err)
		return
	}

	h.invalidateRepubCache(plcName)
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
	case config.FamilySLC500, config.FamilyPLC5, config.FamilyMicroLogix:
		typeNames = pccc.SupportedTypeNames()
		addressBased = true
		addressLabel = "Data Table Address"
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

	// Convert []interface{} (from JSON decode) to typed slices
	// so the PLC driver receives the same types as the TUI write path.
	value := coerceWriteValue(req.Value)

	if err := h.engine.WriteTag(plcName, tagName, value); err != nil {
		http.Error(w, "Write failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// coerceWriteValue converts JSON-decoded values to typed Go values that
// PLC drivers expect. JSON decoding produces float64 for numbers and
// []interface{} for arrays; drivers need int32/float32/typed slices.
func coerceWriteValue(v interface{}) interface{} {
	switch val := v.(type) {
	case float64:
		// If it looks like an integer, send as int32 (DINT is most common)
		if val == float64(int32(val)) {
			return int32(val)
		}
		return float32(val)
	case []interface{}:
		if len(val) == 0 {
			return val
		}
		// Detect element types and build typed slices
		allInt, allFloat, allBool, allString := true, true, true, true
		allFitIn32 := true
		for _, elem := range val {
			switch e := elem.(type) {
			case float64:
				allBool = false
				allString = false
				if e != float64(int64(e)) {
					allInt = false
				} else if e < -2147483648 || e > 2147483647 {
					allFitIn32 = false
				}
			case bool:
				allInt = false
				allFloat = false
				allString = false
			case string:
				allInt = false
				allFloat = false
				allBool = false
			default:
				allInt = false
				allFloat = false
				allBool = false
				allString = false
			}
		}
		if allInt {
			if allFitIn32 {
				out := make([]int32, len(val))
				for i, e := range val {
					out[i] = int32(e.(float64))
				}
				return out
			}
			out := make([]int64, len(val))
			for i, e := range val {
				out[i] = int64(e.(float64))
			}
			return out
		}
		if allFloat {
			out := make([]float32, len(val))
			for i, e := range val {
				out[i] = float32(e.(float64))
			}
			return out
		}
		if allBool {
			out := make([]bool, len(val))
			for i, e := range val {
				out[i] = e.(bool)
			}
			return out
		}
		if allString {
			out := make([]string, len(val))
			for i, e := range val {
				out[i] = e.(string)
			}
			return out
		}
		return val
	default:
		return v
	}
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
	enabled, err := h.engine.ToggleAPI()
	if err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"enabled": enabled})
}

// --- MQTT CRUD ---

func (h *Handlers) handleMQTTCreate(w http.ResponseWriter, r *http.Request) {
	var req engine.MQTTHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.engine.CreateMQTT(req.ToCreateRequest()); err != nil {
		h.writeEngineError(w, err)
		return
	}

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

	var req engine.MQTTHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.engine.UpdateMQTT(name, req.ToUpdateRequest()); err != nil {
		h.writeEngineError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handlers) handleMQTTDelete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	if err := h.engine.DeleteMQTT(name); err != nil {
		h.writeEngineError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// --- Valkey CRUD ---

func (h *Handlers) handleValkeyCreate(w http.ResponseWriter, r *http.Request) {
	var req engine.ValkeyHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.engine.CreateValkey(req.ToCreateRequest()); err != nil {
		h.writeEngineError(w, err)
		return
	}

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

	var req engine.ValkeyHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.engine.UpdateValkey(name, req.ToUpdateRequest()); err != nil {
		h.writeEngineError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handlers) handleValkeyDelete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	if err := h.engine.DeleteValkey(name); err != nil {
		h.writeEngineError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// --- Kafka CRUD ---

func (h *Handlers) handleKafkaCreate(w http.ResponseWriter, r *http.Request) {
	var req engine.KafkaHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.engine.CreateKafka(req.ToCreateRequest()); err != nil {
		h.writeEngineError(w, err)
		return
	}

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

	var req engine.KafkaHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.engine.UpdateKafka(name, req.ToUpdateRequest()); err != nil {
		h.writeEngineError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handlers) handleKafkaDelete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	if err := h.engine.DeleteKafka(name); err != nil {
		h.writeEngineError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// --- TagPack CRUD ---

func (h *Handlers) handleTagPackCreate(w http.ResponseWriter, r *http.Request) {
	var req engine.TagPackHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.engine.CreateTagPack(req.ToCreateRequest()); err != nil {
		h.writeEngineError(w, err)
		return
	}

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

	var req engine.TagPackHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.engine.UpdateTagPack(name, req.ToUpdateRequest()); err != nil {
		h.writeEngineError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handlers) handleTagPackDelete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	if err := h.engine.DeleteTagPack(name); err != nil {
		h.writeEngineError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handlers) handleTagPackToggle(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	enabled, err := h.engine.ToggleTagPack(name)
	if err != nil {
		h.writeEngineError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"enabled": enabled})
}

func (h *Handlers) handleTagPackServiceToggle(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)
	service := chi.URLParam(r, "service")

	enabled, err := h.engine.ToggleTagPackService(name, service)
	if err != nil {
		h.writeEngineError(w, err)
		return
	}

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

	if err := h.engine.AddTagPackMember(name, member); err != nil {
		h.writeEngineError(w, err)
		return
	}

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

	if err := h.engine.RemoveTagPackMember(name, index); err != nil {
		h.writeEngineError(w, err)
		return
	}

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

	ignoreChanges, err := h.engine.ToggleTagPackMemberIgnore(name, index)
	if err != nil {
		h.writeEngineError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ignore_changes": ignoreChanges})
}

// --- Rule CRUD ---

func (h *Handlers) handleRuleCreate(w http.ResponseWriter, r *http.Request) {
	var req engine.RuleHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.engine.CreateRule(req.ToCreateRequest()); err != nil {
		h.writeEngineError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (h *Handlers) handleRuleGet(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	cfg := h.managers.GetConfig()
	rc := cfg.FindRule(name)
	if rc == nil {
		http.Error(w, "Rule not found", http.StatusNotFound)
		return
	}

	// Also get runtime status
	ruleMgr := h.managers.GetRuleMgr()
	status, ruleErr, fireCount, lastFire := ruleMgr.GetRuleStatus(name)

	resp := map[string]interface{}{
		"name":            rc.Name,
		"enabled":         rc.Enabled,
		"conditions":      rc.Conditions,
		"logic_mode":      rc.LogicMode,
		"debounce_ms":     rc.DebounceMS,
		"cooldown_ms":     rc.CooldownMS,
		"actions":         rc.Actions,
		"cleared_actions": rc.ClearedActions,
		"status":          ruleStatusString(status),
		"fire_count":      fireCount,
	}
	if ruleErr != nil {
		resp["error"] = ruleErr.Error()
	}
	if !lastFire.IsZero() {
		resp["last_fire"] = lastFire.Format("2006-01-02 15:04:05")
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *Handlers) handleRuleUpdate(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	var req engine.RuleHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.engine.UpdateRule(name, req.ToUpdateRequest()); err != nil {
		h.writeEngineError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handlers) handleRuleDelete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	if err := h.engine.DeleteRule(name); err != nil {
		h.writeEngineError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handlers) handleRuleTestFire(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	name, _ = url.PathUnescape(name)

	if err := h.engine.TestFireRule(name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

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
		// Use the Values map: it contains every tag actually being polled,
		// including struct-member children like Access_Card.Login_Name.
		values := plc.GetValues()
		if values != nil {
			for name, tv := range values {
				if tv == nil || tv.Error != nil {
					continue
				}
				tags = append(tags, tagInfo{
					PLC:  plc.Config.Name,
					Tag:  name,
					Type: tv.TypeName(),
				})
			}
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

	// Build from Values map: contains every tag actually being polled,
	// including struct-member children like Access_Card.Login_Name.
	values := plc.GetValues()
	seen := make(map[string]bool)
	if values != nil {
		for name, tv := range values {
			if tv == nil || tv.Error != nil {
				continue
			}
			seen[name] = true
			// Mark all parent prefixes as seen so discovered parents
			// (e.g., Access_Card) don't appear when only a child is polled.
			for i := range name {
				if name[i] == '.' {
					seen[name[:i]] = true
				}
			}
			tags = append(tags, plcTagEntry{Name: name, Type: tv.TypeName()})
		}
	}

	// Fallback: add enabled config tags not yet in Values (e.g., PLC not connected).
	// Uses Config.Tags (explicitly configured) instead of GetTags() to avoid
	// flooding the list with all discovered tags.
	for _, sel := range plc.Config.Tags {
		if sel.Enabled && !seen[sel.Name] {
			tags = append(tags, plcTagEntry{Name: sel.Name, Type: sel.DataType})
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

func ruleStatusString(s rule.Status) string {
	switch s {
	case rule.StatusArmed:
		return "armed"
	case rule.StatusFiring:
		return "firing"
	case rule.StatusWaitingClear:
		return "waiting_clear"
	case rule.StatusCooldown:
		return "cooldown"
	case rule.StatusError:
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

