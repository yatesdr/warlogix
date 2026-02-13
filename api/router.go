package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"warlink/config"
	"warlink/kafka"
	"warlink/mqtt"
	"warlink/plcman"
	"warlink/push"
	"warlink/tagpack"
	"warlink/trigger"
	"warlink/valkey"
)

// Managers provides access to shared backend managers.
type Managers interface {
	GetConfig() *config.Config
	GetConfigPath() string
	GetPLCMan() *plcman.Manager
	GetMQTTMgr() *mqtt.Manager
	GetValkeyMgr() *valkey.Manager
	GetKafkaMgr() *kafka.Manager
	GetTriggerMgr() *trigger.Manager
	GetPushMgr() *push.Manager
	GetPackMgr() *tagpack.Manager
}

// PLCResponse is the JSON response for PLC info.
type PLCResponse struct {
	Name        string `json:"name"`
	Address     string `json:"address"`
	Slot        byte   `json:"slot"`
	Status      string `json:"status"`
	ProductName string `json:"product_name,omitempty"`
	Error       string `json:"error,omitempty"`
}

// TagResponse is the JSON response for a tag value.
// When a tag has an alias, Name contains the alias and MemLoc contains the original address.
type TagResponse struct {
	PLC       string      `json:"plc"`
	Name      string      `json:"name"`
	MemLoc    string      `json:"memloc,omitempty"` // Memory location (S7/Omron address) when alias is used
	Type      string      `json:"type"`
	Value     interface{} `json:"value"`
	Error     string      `json:"error,omitempty"`
	Timestamp string      `json:"timestamp,omitempty"`
}

// HealthResponse is the JSON structure for PLC health status.
type HealthResponse struct {
	PLC       string `json:"plc"`
	Online    bool   `json:"online"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	Timestamp string `json:"timestamp"`
}

// WriteRequest is the JSON request for writing a tag value.
type WriteRequest struct {
	PLC   string      `json:"plc"`
	Tag   string      `json:"tag"`
	Value interface{} `json:"value"`
}

// WriteResponse is the JSON response after writing a tag value.
type WriteResponse struct {
	PLC       string      `json:"plc"`
	Tag       string      `json:"tag"`
	Value     interface{} `json:"value"`
	Success   bool        `json:"success"`
	Error     string      `json:"error,omitempty"`
	Timestamp string      `json:"timestamp"`
}

// handlers holds the API handler functions.
type handlers struct {
	managers Managers
}

// NewRouter creates the REST API router.
func NewRouter(managers Managers) chi.Router {
	r := chi.NewRouter()
	h := &handlers{managers: managers}

	// Root - list PLCs
	r.Get("/", h.handleListPLCs)

	// TagPack endpoints
	r.Get("/tagpack", h.handlePackList)
	r.Get("/tagpack/{pack}", h.handlePackDetails)

	// PLC-specific endpoints
	r.Route("/{plc}", func(r chi.Router) {
		r.Get("/", h.handlePLCDetails)
		r.Get("/health", h.handlePLCHealth)
		r.Get("/programs", h.handlePrograms)
		r.Get("/programs/{program}/tags", h.handleProgramTags)
		r.Get("/tags", h.handleAllTags)
		r.Get("/tags/*", h.handleSingleTag)
		r.Post("/write", h.handleWrite)
	})

	return r
}

func (h *handlers) writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (h *handlers) writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func (h *handlers) handleListPLCs(w http.ResponseWriter, r *http.Request) {
	manager := h.managers.GetPLCMan()
	plcs := manager.ListPLCs()
	response := make([]PLCResponse, 0, len(plcs))

	for _, plc := range plcs {
		resp := PLCResponse{
			Name:    plc.Config.Name,
			Address: plc.Config.Address,
			Slot:    plc.Config.Slot,
			Status:  plc.GetStatus().String(),
		}

		if info := plc.GetDeviceInfo(); info != nil {
			resp.ProductName = info.Model
		}
		if err := plc.GetError(); err != nil {
			resp.Error = err.Error()
		}

		response = append(response, resp)
	}

	h.writeJSON(w, response)
}

func (h *handlers) handlePLCDetails(w http.ResponseWriter, r *http.Request) {
	plcName := chi.URLParam(r, "plc")
	plcName, _ = url.PathUnescape(plcName)

	manager := h.managers.GetPLCMan()
	plc := manager.GetPLC(plcName)
	if plc == nil {
		h.writeError(w, http.StatusNotFound, "PLC not found")
		return
	}

	resp := PLCResponse{
		Name:    plc.Config.Name,
		Address: plc.Config.Address,
		Slot:    plc.Config.Slot,
		Status:  plc.GetStatus().String(),
	}

	if info := plc.GetDeviceInfo(); info != nil {
		resp.ProductName = info.Model
	}
	if err := plc.GetError(); err != nil {
		resp.Error = err.Error()
	}

	h.writeJSON(w, resp)
}

func (h *handlers) handlePLCHealth(w http.ResponseWriter, r *http.Request) {
	plcName := chi.URLParam(r, "plc")
	plcName, _ = url.PathUnescape(plcName)

	manager := h.managers.GetPLCMan()
	plc := manager.GetPLC(plcName)
	if plc == nil {
		h.writeError(w, http.StatusNotFound, "PLC not found")
		return
	}

	health := plc.GetHealthStatus()
	resp := HealthResponse{
		PLC:       plc.Config.Name,
		Online:    health.Online,
		Status:    health.Status,
		Error:     health.Error,
		Timestamp: health.Timestamp.Format(time.RFC3339),
	}

	h.writeJSON(w, resp)
}

func (h *handlers) handlePrograms(w http.ResponseWriter, r *http.Request) {
	plcName := chi.URLParam(r, "plc")
	plcName, _ = url.PathUnescape(plcName)

	manager := h.managers.GetPLCMan()
	plc := manager.GetPLC(plcName)
	if plc == nil {
		h.writeError(w, http.StatusNotFound, "PLC not found")
		return
	}

	programs := plc.GetPrograms()
	h.writeJSON(w, programs)
}

func (h *handlers) handleProgramTags(w http.ResponseWriter, r *http.Request) {
	plcName := chi.URLParam(r, "plc")
	plcName, _ = url.PathUnescape(plcName)
	programName := chi.URLParam(r, "program")
	programName, _ = url.PathUnescape(programName)

	manager := h.managers.GetPLCMan()
	plc := manager.GetPLC(plcName)
	if plc == nil {
		h.writeError(w, http.StatusNotFound, "PLC not found")
		return
	}

	values := plc.GetValues()
	prefix := "Program:" + programName + "."

	response := make(map[string]TagResponse)

	for _, sel := range plc.Config.Tags {
		if !sel.Enabled || sel.NoREST {
			continue
		}
		if !strings.HasPrefix(sel.Name, prefix) {
			continue
		}

		tagPart := sel.Name
		offset := ""
		if sel.Alias != "" {
			tagPart = sel.Alias
			offset = sel.Name
		}

		key := plc.Config.Name + "." + tagPart
		resp := TagResponse{
			PLC:    plc.Config.Name,
			Name:   tagPart,
			MemLoc: offset,
		}

		if v, ok := values[sel.Name]; ok {
			resp.Type = v.TypeName()
			resp.Value = v.GoValue()
			if v.Error != nil {
				resp.Error = v.Error.Error()
			}
		}

		response[key] = resp
	}

	h.writeJSON(w, response)
}

func (h *handlers) handleAllTags(w http.ResponseWriter, r *http.Request) {
	plcName := chi.URLParam(r, "plc")
	plcName, _ = url.PathUnescape(plcName)

	manager := h.managers.GetPLCMan()
	plc := manager.GetPLC(plcName)
	if plc == nil {
		h.writeError(w, http.StatusNotFound, "PLC not found")
		return
	}

	values := plc.GetValues()
	response := make(map[string]TagResponse)

	for _, sel := range plc.Config.Tags {
		if !sel.Enabled || sel.NoREST {
			continue
		}

		tagPart := sel.Name
		offset := ""
		if sel.Alias != "" {
			tagPart = sel.Alias
			offset = sel.Name
		}

		key := plc.Config.Name + "." + tagPart
		resp := TagResponse{
			PLC:    plc.Config.Name,
			Name:   tagPart,
			MemLoc: offset,
		}

		if v, ok := values[sel.Name]; ok {
			resp.Type = v.TypeName()
			resp.Value = v.GoValue()
			if v.Error != nil {
				resp.Error = v.Error.Error()
			}
		}

		response[key] = resp
	}

	h.writeJSON(w, response)
}

func (h *handlers) handleSingleTag(w http.ResponseWriter, r *http.Request) {
	plcName := chi.URLParam(r, "plc")
	plcName, _ = url.PathUnescape(plcName)

	// Get tag name from wildcard (everything after /tags/)
	tagName := chi.URLParam(r, "*")
	tagName, _ = url.PathUnescape(tagName)

	manager := h.managers.GetPLCMan()
	plc := manager.GetPLC(plcName)
	if plc == nil {
		h.writeError(w, http.StatusNotFound, "PLC not found")
		return
	}

	// Find the tag by name or alias
	var sel *config.TagSelection
	var actualTagName string
	for i := range plc.Config.Tags {
		tag := &plc.Config.Tags[i]
		if !tag.Enabled || tag.NoREST {
			continue
		}
		if tag.Name == tagName || (tag.Alias != "" && tag.Alias == tagName) {
			sel = tag
			actualTagName = tag.Name
			break
		}
	}

	if sel == nil {
		h.writeError(w, http.StatusNotFound, "tag not found or not enabled for REST")
		return
	}

	name := actualTagName
	memloc := ""
	if sel.Alias != "" {
		name = sel.Alias
		memloc = actualTagName
	}

	values := plc.GetValues()
	if v, ok := values[actualTagName]; ok {
		resp := TagResponse{
			PLC:    plc.Config.Name,
			Name:   name,
			MemLoc: memloc,
			Type:   v.TypeName(),
			Value:  v.GoValue(),
		}
		if v.Error != nil {
			resp.Error = v.Error.Error()
		}
		h.writeJSON(w, resp)
		return
	}

	// Try reading from PLC directly
	v, err := manager.ReadTag(plc.Config.Name, actualTagName)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v == nil {
		h.writeError(w, http.StatusNotFound, "tag not found")
		return
	}

	resp := TagResponse{
		PLC:    plc.Config.Name,
		Name:   name,
		MemLoc: memloc,
		Type:   v.TypeName(),
		Value:  v.GoValue(),
	}
	if v.Error != nil {
		resp.Error = v.Error.Error()
	}
	h.writeJSON(w, resp)
}

func (h *handlers) handleWrite(w http.ResponseWriter, r *http.Request) {
	plcName := chi.URLParam(r, "plc")
	plcName, _ = url.PathUnescape(plcName)

	manager := h.managers.GetPLCMan()
	plc := manager.GetPLC(plcName)
	if plc == nil {
		h.writeError(w, http.StatusNotFound, "PLC not found")
		return
	}

	var req WriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.PLC != plc.Config.Name {
		resp := WriteResponse{
			PLC:       req.PLC,
			Tag:       req.Tag,
			Value:     req.Value,
			Success:   false,
			Error:     fmt.Sprintf("PLC name mismatch: URL has '%s', request has '%s'", plc.Config.Name, req.PLC),
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		w.WriteHeader(http.StatusBadRequest)
		h.writeJSON(w, resp)
		return
	}

	status := plc.GetStatus()
	tagFound, tagWritable := plc.GetTagInfo(req.Tag)

	if status != plcman.StatusConnected {
		resp := WriteResponse{
			PLC:       req.PLC,
			Tag:       req.Tag,
			Value:     req.Value,
			Success:   false,
			Error:     "PLC not connected",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		h.writeJSON(w, resp)
		return
	}

	if !tagFound {
		resp := WriteResponse{
			PLC:       req.PLC,
			Tag:       req.Tag,
			Value:     req.Value,
			Success:   false,
			Error:     "tag not found",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		w.WriteHeader(http.StatusNotFound)
		h.writeJSON(w, resp)
		return
	}

	if !tagWritable {
		resp := WriteResponse{
			PLC:       req.PLC,
			Tag:       req.Tag,
			Value:     req.Value,
			Success:   false,
			Error:     "tag is not writable",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		w.WriteHeader(http.StatusForbidden)
		h.writeJSON(w, resp)
		return
	}

	resultChan := make(chan error, 1)
	go func() {
		resultChan <- manager.WriteTag(plc.Config.Name, req.Tag, req.Value)
	}()

	var writeErr error
	select {
	case writeErr = <-resultChan:
	case <-time.After(3 * time.Second):
		writeErr = fmt.Errorf("write timeout: PLC did not respond within 3 seconds")
	}

	resp := WriteResponse{
		PLC:       req.PLC,
		Tag:       req.Tag,
		Value:     req.Value,
		Success:   writeErr == nil,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	if writeErr != nil {
		resp.Error = writeErr.Error()
		w.WriteHeader(http.StatusInternalServerError)
	}

	h.writeJSON(w, resp)
}

func (h *handlers) handlePackList(w http.ResponseWriter, r *http.Request) {
	packMgr := h.managers.GetPackMgr()
	if packMgr == nil {
		h.writeJSON(w, []tagpack.PackInfo{})
		return
	}

	packs := packMgr.ListPacks()
	if packs == nil {
		packs = []tagpack.PackInfo{}
	}
	h.writeJSON(w, packs)
}

func (h *handlers) handlePackDetails(w http.ResponseWriter, r *http.Request) {
	packName := chi.URLParam(r, "pack")
	packName, _ = url.PathUnescape(packName)

	packMgr := h.managers.GetPackMgr()
	if packMgr == nil {
		h.writeError(w, http.StatusNotFound, "pack not found")
		return
	}

	pv := packMgr.GetPackValue(packName)
	if pv == nil {
		h.writeError(w, http.StatusNotFound, "pack not found")
		return
	}

	h.writeJSON(w, pv)
}
