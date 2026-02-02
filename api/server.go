// Package api provides a REST API server for PLC data.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
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

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Parse path: /, /{name}, /{name}/programs, /{name}/tags, /{name}/tags/{tagname}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		// GET / - List all PLCs
		s.handleListPLCs(w)
		return
	}

	parts := strings.Split(path, "/")
	plcName := parts[0]
	plc := s.manager.GetPLC(plcName)
	if plc == nil {
		s.writeError(w, http.StatusNotFound, "PLC not found")
		return
	}

	if len(parts) == 1 {
		// GET /{name} - PLC details
		s.handlePLCDetails(w, plc)
		return
	}

	switch parts[1] {
	case "programs":
		if len(parts) == 2 {
			// GET /{name}/programs - List programs
			s.handlePrograms(w, plc)
		} else if len(parts) >= 4 && parts[3] == "tags" {
			// GET /{name}/programs/{program}/tags
			s.handleProgramTags(w, plc, parts[2])
		} else {
			s.writeError(w, http.StatusNotFound, "not found")
		}
	case "tags":
		if len(parts) == 2 {
			// GET /{name}/tags - All tags
			s.handleAllTags(w, plc)
		} else {
			// GET /{name}/tags/{tagname} - Single tag
			tagName := strings.Join(parts[2:], "/")
			s.handleSingleTag(w, plc, tagName)
		}
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

		if identity := plc.GetIdentity(); identity != nil {
			resp.ProductName = identity.ProductName
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

	if identity := plc.GetIdentity(); identity != nil {
		resp.ProductName = identity.ProductName
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
