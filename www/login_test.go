package www

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"warlink/config"
	"warlink/kafka"
	"warlink/mqtt"
	"warlink/plcman"
	"warlink/push"
	"warlink/tagpack"
	"warlink/trigger"
	"warlink/valkey"
)

// testManagers implements the Managers interface for testing.
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
func (m *testManagers) GetTriggerMgr() *trigger.Manager   { return nil }
func (m *testManagers) GetPackMgr() *tagpack.Manager      { return nil }
func (m *testManagers) GetPushMgr() *push.Manager          { return nil }

func TestBcryptHashYAMLRoundtrip(t *testing.T) {
	// Verify that bcrypt hashes survive YAML marshal/unmarshal
	hash, _ := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	original := string(hash)

	cfg := &config.Config{
		Web: config.WebConfig{
			UI: config.WebUIConfig{
				Users: []config.WebUser{{
					Username:           "admin",
					PasswordHash:       original,
					Role:               config.RoleAdmin,
					MustChangePassword: true,
				}},
			},
		},
	}

	tmpDir := t.TempDir()
	path := tmpDir + "/test.yaml"
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(loaded.Web.UI.Users) == 0 {
		t.Fatal("no users after load")
	}

	loadedHash := loaded.Web.UI.Users[0].PasswordHash
	t.Logf("Original hash: %s", original)
	t.Logf("Loaded hash:   %s", loadedHash)
	t.Logf("Hashes match:  %v", original == loadedHash)

	if err := bcrypt.CompareHashAndPassword([]byte(loadedHash), []byte("admin")); err != nil {
		t.Errorf("bcrypt verify FAILED after YAML roundtrip: %v", err)
	}

	if !loaded.Web.UI.Users[0].MustChangePassword {
		t.Error("MustChangePassword was lost in roundtrip")
	}
}

func TestLoginRedirectsToChangePassword(t *testing.T) {
	// Create config with admin user that must change password
	hash, _ := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	cfg := &config.Config{
		Web: config.WebConfig{
			Enabled: true,
			Host:    "0.0.0.0",
			Port:    8080,
			UI: config.WebUIConfig{
				Enabled:       true,
				SessionSecret: "dGVzdHNlY3JldHRlc3RzZWNyZXR0ZXN0c2VjcmV0dGVzdA==", // 32 bytes base64
				Users: []config.WebUser{{
					Username:           "admin",
					PasswordHash:       string(hash),
					Role:               config.RoleAdmin,
					MustChangePassword: true,
				}},
			},
		},
	}

	managers := &testManagers{cfg: cfg, configPath: "/tmp/test.yaml", plcMan: plcman.NewManager(time.Second)}
	router := NewRouter(&cfg.Web.UI, managers, nil)
	server := httptest.NewServer(router)
	defer server.Close()

	client := server.Client()
	// Don't follow redirects — we want to inspect them
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	// Step 1: POST /login with admin/admin
	form := url.Values{"username": {"admin"}, "password": {"admin"}}
	resp, err := client.Post(server.URL+"/login", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("POST /login failed: %v", err)
	}
	defer resp.Body.Close()

	t.Logf("POST /login status: %d", resp.StatusCode)
	t.Logf("POST /login Location: %s", resp.Header.Get("Location"))
	t.Logf("POST /login Set-Cookie: %s", resp.Header.Get("Set-Cookie"))

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303, got %d", resp.StatusCode)
	}

	location := resp.Header.Get("Location")
	if location != "/change-password" {
		t.Errorf("expected redirect to /change-password, got %s", location)
	}

	// Step 2: GET /change-password with cookie from step 1
	cookies := resp.Cookies()
	req, _ := http.NewRequest("GET", server.URL+"/change-password", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp2, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /change-password failed: %v", err)
	}
	defer resp2.Body.Close()

	t.Logf("GET /change-password status: %d", resp2.StatusCode)
	t.Logf("GET /change-password Location: %s", resp2.Header.Get("Location"))

	if resp2.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for change-password page, got %d (Location: %s)", resp2.StatusCode, resp2.Header.Get("Location"))
	}
}
