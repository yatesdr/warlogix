package push

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"warlink/config"
)

// mockTagReader implements trigger.TagReader for testing.
type mockTagReader struct {
	mu     sync.RWMutex
	values map[string]map[string]interface{} // plc -> tag -> value
}

func newMockReader() *mockTagReader {
	return &mockTagReader{
		values: make(map[string]map[string]interface{}),
	}
}

func (m *mockTagReader) SetTag(plc, tag string, value interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.values[plc] == nil {
		m.values[plc] = make(map[string]interface{})
	}
	m.values[plc][tag] = value
}

func (m *mockTagReader) ReadTag(plcName, tagName string) (interface{}, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if plcTags, ok := m.values[plcName]; ok {
		if val, ok := plcTags[tagName]; ok {
			return val, nil
		}
	}
	return nil, fmt.Errorf("tag not found: %s.%s", plcName, tagName)
}

func (m *mockTagReader) ReadTags(plcName string, tagNames []string) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	for _, tag := range tagNames {
		val, err := m.ReadTag(plcName, tag)
		if err != nil {
			continue
		}
		result[tag] = val
	}
	return result, nil
}

func TestTagResolution(t *testing.T) {
	reader := newMockReader()
	reader.SetTag("PLC1", "Temperature", 72.5)
	reader.SetTag("PLC1", "FaultCode", int32(42))
	reader.SetTag("PLC2", "Pressure", 14.7)

	cfg := &config.PushConfig{
		Name:    "test",
		Method:  "POST",
		URL:     "http://example.com",
		Body:    `{"temp": #PLC1.Temperature, "fault": #PLC1.FaultCode, "pressure": #PLC2.Pressure}`,
		Timeout: 5 * time.Second,
	}

	p, err := NewPush(cfg, reader)
	if err != nil {
		t.Fatal(err)
	}

	resolved := p.resolveBody()

	if !strings.Contains(resolved, "72.5") {
		t.Errorf("expected Temperature resolved, got: %s", resolved)
	}
	if !strings.Contains(resolved, "42") {
		t.Errorf("expected FaultCode resolved, got: %s", resolved)
	}
	if !strings.Contains(resolved, "14.7") {
		t.Errorf("expected Pressure resolved, got: %s", resolved)
	}
}

func TestTagResolutionMissingTag(t *testing.T) {
	reader := newMockReader()

	cfg := &config.PushConfig{
		Name:    "test",
		Method:  "POST",
		URL:     "http://example.com",
		Body:    `{"val": #Missing.Tag}`,
		Timeout: 5 * time.Second,
	}

	p, err := NewPush(cfg, reader)
	if err != nil {
		t.Fatal(err)
	}

	resolved := p.resolveBody()
	if !strings.Contains(resolved, "#Missing.Tag") {
		t.Errorf("expected unresolved ref preserved, got: %s", resolved)
	}
}

func TestBuildRequestAuth(t *testing.T) {
	reader := newMockReader()

	tests := []struct {
		name     string
		auth     config.PushAuthConfig
		checkFn  func(*http.Request) error
	}{
		{
			name: "bearer",
			auth: config.PushAuthConfig{Type: config.PushAuthBearer, Token: "mytoken123"},
			checkFn: func(r *http.Request) error {
				if r.Header.Get("Authorization") != "Bearer mytoken123" {
					return fmt.Errorf("expected Bearer auth, got: %s", r.Header.Get("Authorization"))
				}
				return nil
			},
		},
		{
			name: "basic",
			auth: config.PushAuthConfig{Type: config.PushAuthBasic, Username: "user", Password: "pass"},
			checkFn: func(r *http.Request) error {
				u, p, ok := r.BasicAuth()
				if !ok || u != "user" || p != "pass" {
					return fmt.Errorf("expected basic auth user/pass, got: %s/%s ok=%v", u, p, ok)
				}
				return nil
			},
		},
		{
			name: "custom_header",
			auth: config.PushAuthConfig{Type: config.PushAuthCustomHeader, HeaderName: "X-API-Key", HeaderValue: "secret"},
			checkFn: func(r *http.Request) error {
				if r.Header.Get("X-API-Key") != "secret" {
					return fmt.Errorf("expected custom header, got: %s", r.Header.Get("X-API-Key"))
				}
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.PushConfig{
				Name:    "test",
				Method:  "POST",
				URL:     "http://example.com",
				Auth:    tt.auth,
				Timeout: 5 * time.Second,
			}

			p, err := NewPush(cfg, reader)
			if err != nil {
				t.Fatal(err)
			}

			req, err := p.buildRequest(`{"test": true}`)
			if err != nil {
				t.Fatal(err)
			}

			if err := tt.checkFn(req); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestTestFire(t *testing.T) {
	reader := newMockReader()
	reader.SetTag("PLC1", "Value", 100)

	var receivedBody string
	var receivedMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		body := make([]byte, 1024)
		n, _ := r.Body.Read(body)
		receivedBody = string(body[:n])
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.PushConfig{
		Name:    "test",
		Method:  "POST",
		URL:     server.URL,
		Body:    `{"value": #PLC1.Value}`,
		Timeout: 5 * time.Second,
	}

	p, err := NewPush(cfg, reader)
	if err != nil {
		t.Fatal(err)
	}

	err = p.TestFire()
	if err != nil {
		t.Fatalf("TestFire failed: %v", err)
	}

	if receivedMethod != "POST" {
		t.Errorf("expected POST, got %s", receivedMethod)
	}
	if !strings.Contains(receivedBody, "100") {
		t.Errorf("expected body to contain resolved value, got: %s", receivedBody)
	}

	count, _, _ := p.GetStats()
	if count != 1 {
		t.Errorf("expected sendCount=1, got %d", count)
	}
}

func TestCustomHeaders(t *testing.T) {
	reader := newMockReader()

	var receivedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.PushConfig{
		Name:    "test",
		Method:  "POST",
		URL:     server.URL,
		Body:    "test",
		Headers: map[string]string{"X-Custom": "value1", "X-Another": "value2"},
		Timeout: 5 * time.Second,
	}

	p, err := NewPush(cfg, reader)
	if err != nil {
		t.Fatal(err)
	}

	err = p.TestFire()
	if err != nil {
		t.Fatalf("TestFire failed: %v", err)
	}

	if receivedHeaders.Get("X-Custom") != "value1" {
		t.Errorf("expected X-Custom: value1, got: %s", receivedHeaders.Get("X-Custom"))
	}
	if receivedHeaders.Get("X-Another") != "value2" {
		t.Errorf("expected X-Another: value2, got: %s", receivedHeaders.Get("X-Another"))
	}
}

func TestCooldownStateMachine(t *testing.T) {
	reader := newMockReader()
	reader.SetTag("PLC1", "Alarm", false)

	var requestCount int
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.PushConfig{
		Name:    "test",
		Enabled: true,
		Conditions: []config.PushCondition{
			{PLC: "PLC1", Tag: "Alarm", Operator: "==", Value: true},
		},
		Method:      "POST",
		URL:         server.URL,
		Body:        `{"alarm": true}`,
		CooldownMin: 0, // No min interval for fast testing
		Timeout:     5 * time.Second,
	}

	p, err := NewPush(cfg, reader)
	if err != nil {
		t.Fatal(err)
	}

	p.Start()
	defer p.Stop()

	// Initially armed, condition false - should not fire
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	if requestCount != 0 {
		t.Errorf("expected 0 requests when condition false, got %d", requestCount)
	}
	mu.Unlock()

	// Set condition true - should fire (rising edge)
	reader.SetTag("PLC1", "Alarm", true)
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	if requestCount != 1 {
		t.Errorf("expected 1 request after condition true, got %d", requestCount)
	}
	mu.Unlock()

	// Condition still true - should NOT fire again (waiting clear)
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	if requestCount != 1 {
		t.Errorf("expected still 1 request while condition stays true, got %d", requestCount)
	}
	mu.Unlock()

	// Clear condition - should transition back to armed (CooldownMin=0)
	reader.SetTag("PLC1", "Alarm", false)
	time.Sleep(300 * time.Millisecond)

	// Fire again
	reader.SetTag("PLC1", "Alarm", true)
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	if requestCount != 2 {
		t.Errorf("expected 2 requests after re-fire, got %d", requestCount)
	}
	mu.Unlock()
}

func TestMultiConditionOR(t *testing.T) {
	reader := newMockReader()
	reader.SetTag("PLC1", "Alarm1", false)
	reader.SetTag("PLC2", "Alarm2", false)

	var requestCount int
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.PushConfig{
		Name:    "test",
		Enabled: true,
		Conditions: []config.PushCondition{
			{PLC: "PLC1", Tag: "Alarm1", Operator: "==", Value: true},
			{PLC: "PLC2", Tag: "Alarm2", Operator: "==", Value: true},
		},
		Method:      "POST",
		URL:         server.URL,
		Body:        `{"alarm": true}`,
		CooldownMin: 0,
		Timeout:     5 * time.Second,
	}

	p, err := NewPush(cfg, reader)
	if err != nil {
		t.Fatal(err)
	}

	p.Start()
	defer p.Stop()

	// Fire first condition
	reader.SetTag("PLC1", "Alarm1", true)
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	if requestCount != 1 {
		t.Errorf("expected 1 request from first condition, got %d", requestCount)
	}
	mu.Unlock()

	// Clear and fire second condition
	reader.SetTag("PLC1", "Alarm1", false)
	time.Sleep(300 * time.Millisecond) // Wait for clear

	reader.SetTag("PLC2", "Alarm2", true)
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	if requestCount != 2 {
		t.Errorf("expected 2 requests after second condition, got %d", requestCount)
	}
	mu.Unlock()
}

func TestManager(t *testing.T) {
	reader := newMockReader()
	mgr := NewManager(reader)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.PushConfig{
		Name:    "test-push",
		Enabled: true,
		Conditions: []config.PushCondition{
			{PLC: "PLC1", Tag: "Tag1", Operator: "==", Value: true},
		},
		Method:  "POST",
		URL:     server.URL,
		Timeout: 5 * time.Second,
	}

	// Add push
	err := mgr.AddPush(cfg)
	if err != nil {
		t.Fatalf("AddPush failed: %v", err)
	}

	// List pushes
	names := mgr.ListPushes()
	if len(names) != 1 || names[0] != "test-push" {
		t.Errorf("expected [test-push], got %v", names)
	}

	// Duplicate add should fail
	err = mgr.AddPush(cfg)
	if err == nil {
		t.Error("expected error for duplicate push")
	}

	// Get push
	p := mgr.GetPush("test-push")
	if p == nil {
		t.Fatal("GetPush returned nil")
	}

	// Status
	status, _, _, _, _ := mgr.GetPushStatus("test-push")
	if status != StatusDisabled {
		t.Errorf("expected Disabled before start, got %v", status)
	}

	// Remove
	mgr.RemovePush("test-push")
	names = mgr.ListPushes()
	if len(names) != 0 {
		t.Errorf("expected empty after remove, got %v", names)
	}
}

func TestTestFireHTTPResponse(t *testing.T) {
	reader := newMockReader()

	// Test server that returns JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	cfg := &config.PushConfig{
		Name:    "test",
		Method:  "GET",
		URL:     server.URL,
		Timeout: 5 * time.Second,
	}

	p, err := NewPush(cfg, reader)
	if err != nil {
		t.Fatal(err)
	}

	err = p.TestFire()
	if err != nil {
		t.Fatalf("TestFire failed: %v", err)
	}

	_, _, code := p.GetStats()
	if code != 200 {
		t.Errorf("expected HTTP 200, got %d", code)
	}
}
