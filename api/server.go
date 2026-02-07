// Package api provides a REST API server for PLC data.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"warlogix/config"
	"warlogix/plcman"
)

// Server is the REST API server.
type Server struct {
	manager *plcman.Manager
	config  *config.RESTConfig
	server  *http.Server
	running bool
	mu      sync.RWMutex
}

// NewServer creates a new REST API server.
func NewServer(manager *plcman.Manager, cfg *config.RESTConfig) *Server {
	return &Server{
		manager: manager,
		config:  cfg,
	}
}

// IsRunning returns whether the server is currently running.
func (s *Server) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// Start begins the HTTP server.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRoot)

	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	s.server = &http.Server{
		Addr:    addr,
		Handler: corsMiddleware(mux),
	}

	go func() {
		if err := s.server.ListenAndServe(); err != http.ErrServerClosed {
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
		}
	}()

	s.running = true
	return nil
}

// Stop halts the HTTP server.
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running || s.server == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := s.server.Shutdown(ctx)
	s.running = false
	s.server = nil
	return err
}

// Address returns the server address.
func (s *Server) Address() string {
	return fmt.Sprintf("http://%s:%d", s.config.Host, s.config.Port)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// getTagWriteInfo safely retrieves tag write information under the PLC's lock.
// Returns: connection status, whether tag was found, whether tag is writable.
func (s *Server) getTagWriteInfo(plc *plcman.ManagedPLC, tagName string) (plcman.ConnectionStatus, bool, bool) {
	// Use the ManagedPLC's exported methods which handle locking internally
	status := plc.GetStatus()
	tagFound, tagWritable := plc.GetTagInfo(tagName)
	return status, tagFound, tagWritable
}

// PLCResponse is the JSON response for a PLC.
type PLCResponse struct {
	Name        string `json:"name"`
	Address     string `json:"address"`
	Slot        byte   `json:"slot"`
	Status      string `json:"status"`
	ProductName string `json:"product_name,omitempty"`
	Error       string `json:"error,omitempty"`
}

// TagResponse is the JSON response for a tag value.
type TagResponse struct {
	PLC       string      `json:"plc"`
	Name      string      `json:"name"`
	Alias     string      `json:"alias,omitempty"`
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

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	// Parse path: /, /{name}, /{name}/programs, /{name}/tags, /{name}/tags/{tagname}
	path := strings.TrimPrefix(r.URL.Path, "/")

	// Handle root path
	if path == "" {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.handleListPLCs(w)
		return
	}

	parts := strings.Split(path, "/")
	plcName, err := url.PathUnescape(parts[0])
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid URL encoding in PLC name")
		return
	}
	plc := s.manager.GetPLC(plcName)
	if plc == nil {
		s.writeError(w, http.StatusNotFound, "PLC not found")
		return
	}

	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.handlePLCDetails(w, plc)
		return
	}

	switch parts[1] {
	case "programs":
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if len(parts) == 2 {
			s.handlePrograms(w, plc)
		} else if len(parts) >= 4 && parts[3] == "tags" {
			programName, err := url.PathUnescape(parts[2])
			if err != nil {
				s.writeError(w, http.StatusBadRequest, "invalid URL encoding in program name")
				return
			}
			s.handleProgramTags(w, plc, programName)
		} else {
			s.writeError(w, http.StatusNotFound, "not found")
		}
	case "tags":
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if len(parts) == 2 {
			s.handleAllTags(w, plc)
		} else {
			tagName := strings.Join(parts[2:], "/")
			tagName, err = url.PathUnescape(tagName)
			if err != nil {
				s.writeError(w, http.StatusBadRequest, "invalid URL encoding in tag name")
				return
			}
			s.handleSingleTag(w, plc, tagName)
		}
	case "write":
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed, use POST")
			return
		}
		s.handleWrite(w, r, plc)
	case "health":
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.handlePLCHealth(w, plc)
	default:
		s.writeError(w, http.StatusNotFound, "not found")
	}
}

func (s *Server) handleListPLCs(w http.ResponseWriter) {
	plcs := s.manager.ListPLCs()
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

	s.writeJSON(w, response)
}

func (s *Server) handlePLCDetails(w http.ResponseWriter, plc *plcman.ManagedPLC) {
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

	s.writeJSON(w, resp)
}

func (s *Server) handlePrograms(w http.ResponseWriter, plc *plcman.ManagedPLC) {
	programs := plc.GetPrograms()
	s.writeJSON(w, programs)
}

func (s *Server) handleProgramTags(w http.ResponseWriter, plc *plcman.ManagedPLC, program string) {
	values := plc.GetValues()

	prefix := "Program:" + program + "."
	response := []TagResponse{}

	// Only return tags that are enabled for republishing
	for _, sel := range plc.Config.Tags {
		if !sel.Enabled {
			continue
		}
		if !strings.HasPrefix(sel.Name, prefix) {
			continue
		}

		resp := TagResponse{
			PLC:   plc.Config.Name,
			Name:  sel.Name,
			Alias: sel.Alias,
		}

		if v, ok := values[sel.Name]; ok {
			resp.Type = v.TypeName()
			resp.Value = v.GoValue()
			if v.Error != nil {
				resp.Error = v.Error.Error()
			}
		}

		response = append(response, resp)
	}

	s.writeJSON(w, response)
}

func (s *Server) handleAllTags(w http.ResponseWriter, plc *plcman.ManagedPLC) {
	values := plc.GetValues()

	// Only return tags that are enabled for republishing
	response := make([]TagResponse, 0)

	for _, sel := range plc.Config.Tags {
		if !sel.Enabled {
			continue
		}

		resp := TagResponse{
			PLC:   plc.Config.Name,
			Name:  sel.Name,
			Alias: sel.Alias,
		}

		if v, ok := values[sel.Name]; ok {
			resp.Type = v.TypeName()
			resp.Value = v.GoValue()
			if v.Error != nil {
				resp.Error = v.Error.Error()
			}
		}

		response = append(response, resp)
	}

	s.writeJSON(w, response)
}

func (s *Server) handleSingleTag(w http.ResponseWriter, plc *plcman.ManagedPLC, tagName string) {
	// Check if this tag is enabled for republishing
	var sel *config.TagSelection
	for i := range plc.Config.Tags {
		if plc.Config.Tags[i].Name == tagName && plc.Config.Tags[i].Enabled {
			sel = &plc.Config.Tags[i]
			break
		}
	}

	if sel == nil {
		s.writeError(w, http.StatusNotFound, "tag not found or not enabled for republishing")
		return
	}

	// Check cached values first
	values := plc.GetValues()
	if v, ok := values[tagName]; ok {
		resp := TagResponse{
			PLC:   plc.Config.Name,
			Name:  tagName,
			Alias: sel.Alias,
			Type:  v.TypeName(),
			Value: v.GoValue(),
		}
		if v.Error != nil {
			resp.Error = v.Error.Error()
		}
		s.writeJSON(w, resp)
		return
	}

	// Try reading from PLC directly
	v, err := s.manager.ReadTag(plc.Config.Name, tagName)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if v == nil {
		s.writeError(w, http.StatusNotFound, "tag not found")
		return
	}

	resp := TagResponse{
		PLC:   plc.Config.Name,
		Name:  tagName,
		Alias: sel.Alias,
		Type:  v.TypeName(),
		Value: v.GoValue(),
	}
	if v.Error != nil {
		resp.Error = v.Error.Error()
	}
	s.writeJSON(w, resp)
}

// WriteRequest is the JSON request for writing a tag value.
// This matches the MQTT write request format for consistency.
type WriteRequest struct {
	PLC   string      `json:"plc"`
	Tag   string      `json:"tag"`
	Value interface{} `json:"value"`
}

// WriteResponse is the JSON response after writing a tag value.
// This matches the MQTT write response format for consistency.
type WriteResponse struct {
	PLC       string      `json:"plc"`
	Tag       string      `json:"tag"`
	Value     interface{} `json:"value"`
	Success   bool        `json:"success"`
	Error     string      `json:"error,omitempty"`
	Timestamp string      `json:"timestamp"`
}

func (s *Server) handleWrite(w http.ResponseWriter, r *http.Request, plc *plcman.ManagedPLC) {
	// Parse request body
	var req WriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	// Validate PLC name matches URL
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
		s.writeJSON(w, resp)
		return
	}

	// Check if PLC is connected and find the tag (under lock to avoid races with UI)
	status, tagFound, tagWritable := s.getTagWriteInfo(plc, req.Tag)

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
		s.writeJSON(w, resp)
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
		s.writeJSON(w, resp)
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
		s.writeJSON(w, resp)
		return
	}

	// Write to PLC in a goroutine with timeout to prevent blocking
	resultChan := make(chan error, 1)
	go func() {
		resultChan <- s.manager.WriteTag(plc.Config.Name, req.Tag, req.Value)
	}()

	// Wait for result or timeout
	var writeErr error
	select {
	case writeErr = <-resultChan:
		// Write completed (success or error)
	case <-time.After(3 * time.Second):
		// Timeout
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

	s.writeJSON(w, resp)
}

func (s *Server) handlePLCHealth(w http.ResponseWriter, plc *plcman.ManagedPLC) {
	health := plc.GetHealthStatus()

	resp := HealthResponse{
		PLC:       plc.Config.Name,
		Online:    health.Online,
		Status:    health.Status,
		Error:     health.Error,
		Timestamp: health.Timestamp.Format(time.RFC3339),
	}

	s.writeJSON(w, resp)
}
