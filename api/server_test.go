package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"warlogix/config"
	"warlogix/plcman"
)

func TestNewServer(t *testing.T) {
	manager := plcman.NewManager(time.Second)
	cfg := &config.RESTConfig{
		Enabled: true,
		Host:    "127.0.0.1",
		Port:    8080,
	}

	server := NewServer(manager, cfg)
	if server == nil {
		t.Fatal("NewServer returned nil")
	}
	if server.manager != manager {
		t.Error("manager not set")
	}
	if server.config != cfg {
		t.Error("config not set")
	}
}

func TestServer_IsRunning(t *testing.T) {
	manager := plcman.NewManager(time.Second)
	cfg := &config.RESTConfig{
		Host: "127.0.0.1",
		Port: 0, // Use any available port
	}

	server := NewServer(manager, cfg)

	if server.IsRunning() {
		t.Error("server should not be running initially")
	}
}

func TestServer_Address(t *testing.T) {
	manager := plcman.NewManager(time.Second)
	cfg := &config.RESTConfig{
		Host: "localhost",
		Port: 9999,
	}

	server := NewServer(manager, cfg)
	addr := server.Address()

	if addr != "http://localhost:9999" {
		t.Errorf("expected 'http://localhost:9999', got %s", addr)
	}
}

func TestServer_StartAndStop(t *testing.T) {
	manager := plcman.NewManager(time.Second)
	cfg := &config.RESTConfig{
		Host: "127.0.0.1",
		Port: 0, // Use any available port
	}

	server := NewServer(manager, cfg)

	// Start server
	if err := server.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Give it a moment to start
	time.Sleep(50 * time.Millisecond)

	if !server.IsRunning() {
		t.Error("server should be running after Start")
	}

	// Start again should be no-op
	if err := server.Start(); err != nil {
		t.Errorf("second Start should not error: %v", err)
	}

	// Stop server
	if err := server.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if server.IsRunning() {
		t.Error("server should not be running after Stop")
	}

	// Stop again should be no-op
	if err := server.Stop(); err != nil {
		t.Errorf("second Stop should not error: %v", err)
	}
}

func TestCorsMiddleware(t *testing.T) {
	handler := corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("sets CORS headers", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Error("missing Access-Control-Allow-Origin header")
		}
		if rec.Header().Get("Access-Control-Allow-Methods") == "" {
			t.Error("missing Access-Control-Allow-Methods header")
		}
	})

	t.Run("handles OPTIONS preflight", func(t *testing.T) {
		req := httptest.NewRequest("OPTIONS", "/", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200 for OPTIONS, got %d", rec.Code)
		}
	})
}

func TestServer_WriteJSON(t *testing.T) {
	manager := plcman.NewManager(time.Second)
	cfg := &config.RESTConfig{}
	server := NewServer(manager, cfg)

	rec := httptest.NewRecorder()
	data := map[string]string{"key": "value"}
	server.writeJSON(rec, data)

	if rec.Header().Get("Content-Type") != "application/json" {
		t.Error("Content-Type should be application/json")
	}

	var result map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if result["key"] != "value" {
		t.Error("JSON not correctly encoded")
	}
}

func TestServer_WriteError(t *testing.T) {
	manager := plcman.NewManager(time.Second)
	cfg := &config.RESTConfig{}
	server := NewServer(manager, cfg)

	rec := httptest.NewRecorder()
	server.writeError(rec, http.StatusNotFound, "not found")

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.Code)
	}

	var result map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if result["error"] != "not found" {
		t.Errorf("expected error 'not found', got %s", result["error"])
	}
}

func TestServer_HandleRoot_ListPLCs(t *testing.T) {
	manager := plcman.NewManager(time.Second)
	cfg := &config.RESTConfig{}
	server := NewServer(manager, cfg)

	// Add a test PLC
	manager.AddPLC(&config.PLCConfig{
		Name:    "TestPLC",
		Address: "192.168.1.100",
		Slot:    0,
		Enabled: true,
	})

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	server.handleRoot(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var result []PLCResponse
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected 1 PLC, got %d", len(result))
	}
	if result[0].Name != "TestPLC" {
		t.Errorf("expected name 'TestPLC', got %s", result[0].Name)
	}
}

func TestServer_HandleRoot_PLCNotFound(t *testing.T) {
	manager := plcman.NewManager(time.Second)
	cfg := &config.RESTConfig{}
	server := NewServer(manager, cfg)

	req := httptest.NewRequest("GET", "/nonexistent", nil)
	rec := httptest.NewRecorder()

	server.handleRoot(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.Code)
	}
}

func TestServer_HandleRoot_MethodNotAllowed(t *testing.T) {
	manager := plcman.NewManager(time.Second)
	cfg := &config.RESTConfig{}
	server := NewServer(manager, cfg)

	req := httptest.NewRequest("POST", "/", nil)
	rec := httptest.NewRecorder()

	server.handleRoot(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rec.Code)
	}
}

func TestServer_HandlePLCDetails(t *testing.T) {
	manager := plcman.NewManager(time.Second)
	cfg := &config.RESTConfig{}
	server := NewServer(manager, cfg)

	manager.AddPLC(&config.PLCConfig{
		Name:    "TestPLC",
		Address: "192.168.1.100",
		Slot:    2,
		Enabled: true,
	})

	req := httptest.NewRequest("GET", "/TestPLC", nil)
	rec := httptest.NewRecorder()

	server.handleRoot(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var result PLCResponse
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}

	if result.Name != "TestPLC" {
		t.Errorf("expected name 'TestPLC', got %s", result.Name)
	}
	if result.Address != "192.168.1.100" {
		t.Errorf("expected address '192.168.1.100', got %s", result.Address)
	}
	if result.Slot != 2 {
		t.Errorf("expected slot 2, got %d", result.Slot)
	}
}

func TestServer_HandleWrite_InvalidJSON(t *testing.T) {
	manager := plcman.NewManager(time.Second)
	cfg := &config.RESTConfig{}
	server := NewServer(manager, cfg)

	manager.AddPLC(&config.PLCConfig{
		Name:    "TestPLC",
		Address: "192.168.1.100",
		Enabled: true,
	})

	req := httptest.NewRequest("POST", "/TestPLC/write", strings.NewReader("invalid json"))
	rec := httptest.NewRecorder()

	server.handleRoot(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestServer_HandleWrite_PLCNameMismatch(t *testing.T) {
	manager := plcman.NewManager(time.Second)
	cfg := &config.RESTConfig{}
	server := NewServer(manager, cfg)

	manager.AddPLC(&config.PLCConfig{
		Name:    "TestPLC",
		Address: "192.168.1.100",
		Enabled: true,
	})

	reqBody := `{"plc": "OtherPLC", "tag": "Counter", "value": 100}`
	req := httptest.NewRequest("POST", "/TestPLC/write", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()

	server.handleRoot(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}

	var result WriteResponse
	json.NewDecoder(rec.Body).Decode(&result)
	if result.Success {
		t.Error("expected success=false")
	}
	if !strings.Contains(result.Error, "mismatch") {
		t.Errorf("expected mismatch error, got: %s", result.Error)
	}
}

func TestServer_HandleWrite_PLCNotConnected(t *testing.T) {
	manager := plcman.NewManager(time.Second)
	cfg := &config.RESTConfig{}
	server := NewServer(manager, cfg)

	manager.AddPLC(&config.PLCConfig{
		Name:    "TestPLC",
		Address: "192.168.1.100",
		Enabled: true,
		Tags: []config.TagSelection{
			{Name: "Counter", Enabled: true, Writable: true},
		},
	})

	reqBody := `{"plc": "TestPLC", "tag": "Counter", "value": 100}`
	req := httptest.NewRequest("POST", "/TestPLC/write", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()

	server.handleRoot(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", rec.Code)
	}

	var result WriteResponse
	json.NewDecoder(rec.Body).Decode(&result)
	if result.Success {
		t.Error("expected success=false")
	}
	if !strings.Contains(result.Error, "not connected") {
		t.Errorf("expected 'not connected' error, got: %s", result.Error)
	}
}

func TestServer_HandleTags_NotFound(t *testing.T) {
	manager := plcman.NewManager(time.Second)
	cfg := &config.RESTConfig{}
	server := NewServer(manager, cfg)

	manager.AddPLC(&config.PLCConfig{
		Name:    "TestPLC",
		Address: "192.168.1.100",
		Enabled: true,
	})

	req := httptest.NewRequest("GET", "/TestPLC/tags/NonexistentTag", nil)
	rec := httptest.NewRecorder()

	server.handleRoot(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.Code)
	}
}

func TestServer_HandlePrograms(t *testing.T) {
	manager := plcman.NewManager(time.Second)
	cfg := &config.RESTConfig{}
	server := NewServer(manager, cfg)

	manager.AddPLC(&config.PLCConfig{
		Name:    "TestPLC",
		Address: "192.168.1.100",
		Enabled: true,
	})

	req := httptest.NewRequest("GET", "/TestPLC/programs", nil)
	rec := httptest.NewRecorder()

	server.handleRoot(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	// Programs endpoint returns an array (empty if no programs discovered)
	var result []string
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
}

func TestServer_HandleAllTags(t *testing.T) {
	manager := plcman.NewManager(time.Second)
	cfg := &config.RESTConfig{}
	server := NewServer(manager, cfg)

	manager.AddPLC(&config.PLCConfig{
		Name:    "TestPLC",
		Address: "192.168.1.100",
		Enabled: true,
		Tags: []config.TagSelection{
			{Name: "Counter", Enabled: true},
			{Name: "Disabled", Enabled: false},
		},
	})

	req := httptest.NewRequest("GET", "/TestPLC/tags", nil)
	rec := httptest.NewRecorder()

	server.handleRoot(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var result []TagResponse
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}

	// Should only return enabled tags
	if len(result) != 1 {
		t.Errorf("expected 1 enabled tag, got %d", len(result))
	}
	if len(result) > 0 && result[0].Name != "Counter" {
		t.Errorf("expected tag 'Counter', got %s", result[0].Name)
	}
}
