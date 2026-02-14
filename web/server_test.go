package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"warlink/config"
	"warlink/engine"
	"warlink/kafka"
	"warlink/mqtt"
	"warlink/plcman"
	"warlink/rule"
	"warlink/tagpack"
	"warlink/valkey"

	"golang.org/x/crypto/bcrypt"
)

// Verify testManagers implements engine.Managers.
var _ engine.Managers = (*testManagers)(nil)

type testManagers struct {
	cfg        *config.Config
	configPath string
	plcMan     *plcman.Manager
}

func (m *testManagers) GetConfig() *config.Config       { return m.cfg }
func (m *testManagers) GetConfigPath() string            { return m.configPath }
func (m *testManagers) GetPLCMan() *plcman.Manager       { return m.plcMan }
func (m *testManagers) GetMQTTMgr() *mqtt.Manager        { return nil }
func (m *testManagers) GetValkeyMgr() *valkey.Manager     { return nil }
func (m *testManagers) GetKafkaMgr() *kafka.Manager       { return nil }
func (m *testManagers) GetRuleMgr() *rule.Manager         { return nil }
func (m *testManagers) GetPackMgr() *tagpack.Manager      { return nil }

func TestUnsecuredDeadline(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	cfg := &config.WebConfig{
		Enabled: true,
		Host:    "127.0.0.1",
		Port:    0, // Will use httptest
		API:     config.WebAPIConfig{Enabled: true},
		UI: config.WebUIConfig{
			Enabled:       true,
			SessionSecret: "dGVzdHNlY3JldHRlc3RzZWNyZXR0ZXN0c2VjcmV0dGVzdA==",
			Users: []config.WebUser{{
				Username:           "admin",
				PasswordHash:       string(hash),
				Role:               config.RoleAdmin,
				MustChangePassword: true,
			}},
		},
	}

	fullCfg := &config.Config{Web: *cfg}
	mgrs := &testManagers{
		cfg:        fullCfg,
		configPath: "/tmp/test.yaml",
		plcMan:     plcman.NewManager(time.Second),
	}

	s := NewServer(cfg, mgrs)

	// Start on a random port
	cfg.Port = 19876
	if err := s.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer s.Stop()

	if !s.IsRunning() {
		t.Fatal("expected server to be running")
	}

	// Set a short deadline
	expired := make(chan bool, 1)
	s.SetUnsecuredDeadline(200*time.Millisecond, func() {
		expired <- true
	})

	select {
	case <-expired:
		// Good — timer fired
	case <-time.After(2 * time.Second):
		t.Fatal("deadline timer did not fire within 2s")
	}

	// Server should be stopped
	if s.IsRunning() {
		t.Error("expected server to be stopped after deadline")
	}
}

func TestLoginFlowThroughMount(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	cfg := &config.WebConfig{
		Enabled: true,
		Host:    "127.0.0.1",
		Port:    0,
		API:     config.WebAPIConfig{Enabled: true},
		UI: config.WebUIConfig{
			Enabled:       true,
			SessionSecret: "dGVzdHNlY3JldHRlc3RzZWNyZXR0ZXN0c2VjcmV0dGVzdA==",
			Users: []config.WebUser{{
				Username:           "admin",
				PasswordHash:       string(hash),
				Role:               config.RoleAdmin,
				MustChangePassword: true,
			}},
		},
	}

	fullCfg := &config.Config{Web: *cfg}
	mgrs := &testManagers{
		cfg:        fullCfg,
		configPath: "/tmp/test.yaml",
		plcMan:     plcman.NewManager(time.Second),
	}

	// Use the full web.Server router (chi.Mount) like production
	s := NewServer(cfg, mgrs)
	server := httptest.NewServer(s.router)
	defer server.Close()

	client := server.Client()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	// Step 1: GET / should redirect to /login (not authenticated)
	resp, err := client.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	resp.Body.Close()
	t.Logf("GET / → status=%d location=%s", resp.StatusCode, resp.Header.Get("Location"))
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login" {
		t.Errorf("expected redirect to /login, got %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}

	// Step 2: POST /login with admin/admin
	form := url.Values{"username": {"admin"}, "password": {"admin"}}
	resp, err = client.Post(server.URL+"/login", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("POST /login failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	t.Logf("POST /login → status=%d location=%s cookie=%s", resp.StatusCode, resp.Header.Get("Location"), resp.Header.Get("Set-Cookie"))
	if len(body) > 0 && resp.StatusCode != http.StatusSeeOther {
		t.Logf("POST /login body (first 500): %s", string(body[:min(500, len(body))]))
	}

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Location") != "/change-password" {
		t.Fatalf("expected redirect to /change-password, got %s", resp.Header.Get("Location"))
	}

	// Step 3: GET /change-password with session cookie
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		t.Fatal("no cookies set after login")
	}
	req, _ := http.NewRequest("GET", server.URL+"/change-password", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("GET /change-password failed: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	t.Logf("GET /change-password → status=%d location=%s", resp.StatusCode, resp.Header.Get("Location"))

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d (location=%s)", resp.StatusCode, resp.Header.Get("Location"))
	}
	if !strings.Contains(string(body), "change the default password") {
		t.Error("change-password page doesn't contain expected text")
	}

	// Step 4: Try to access protected route with MustChangePassword — should redirect to /change-password
	req, _ = http.NewRequest("GET", server.URL+"/plcs", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("GET /plcs failed: %v", err)
	}
	resp.Body.Close()
	t.Logf("GET /plcs (with MustChangePassword) → status=%d location=%s", resp.StatusCode, resp.Header.Get("Location"))
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/change-password" {
		t.Errorf("expected redirect to /change-password, got %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestUnsecuredDeadlineClear(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	cfg := &config.WebConfig{
		Enabled: true,
		Host:    "127.0.0.1",
		Port:    19877,
		API:     config.WebAPIConfig{Enabled: true},
		UI: config.WebUIConfig{
			Enabled:       true,
			SessionSecret: "dGVzdHNlY3JldHRlc3RzZWNyZXR0ZXN0c2VjcmV0dGVzdA==",
			Users: []config.WebUser{{
				Username:           "admin",
				PasswordHash:       string(hash),
				Role:               config.RoleAdmin,
				MustChangePassword: true,
			}},
		},
	}

	fullCfg := &config.Config{Web: *cfg}
	mgrs := &testManagers{
		cfg:        fullCfg,
		configPath: "/tmp/test.yaml",
		plcMan:     plcman.NewManager(time.Second),
	}

	s := NewServer(cfg, mgrs)
	if err := s.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer s.Stop()

	// Set deadline then clear it
	s.SetUnsecuredDeadline(200*time.Millisecond, func() {
		t.Error("deadline should not fire after clear")
	})
	s.ClearUnsecuredDeadline()

	// Wait longer than the deadline
	time.Sleep(500 * time.Millisecond)

	// Server should still be running
	if !s.IsRunning() {
		t.Error("expected server to still be running after cleared deadline")
	}
}
