package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPLCFamily(t *testing.T) {
	t.Run("SupportsDiscovery", func(t *testing.T) {
		tests := []struct {
			family   PLCFamily
			expected bool
		}{
			{FamilyLogix, true},
			{FamilyMicro800, true},
			{FamilyBeckhoff, true},
			{FamilyS7, false},
			{FamilyOmron, false},
			{"", true}, // Empty defaults to logix
		}

		for _, tc := range tests {
			result := tc.family.SupportsDiscovery()
			if result != tc.expected {
				t.Errorf("SupportsDiscovery(%q) = %v, want %v", tc.family, result, tc.expected)
			}
		}
	})

	t.Run("String", func(t *testing.T) {
		tests := []struct {
			family   PLCFamily
			expected string
		}{
			{FamilyLogix, "logix"},
			{FamilyS7, "s7"},
			{FamilyOmron, "omron"},
			{FamilyBeckhoff, "beckhoff"},
			{"", "logix"}, // Empty defaults to logix
		}

		for _, tc := range tests {
			result := tc.family.String()
			if result != tc.expected {
				t.Errorf("String(%q) = %q, want %q", tc.family, result, tc.expected)
			}
		}
	})
}

func boolPtr(b bool) *bool { return &b }

func TestPLCConfig_SupportsDiscovery(t *testing.T) {
	tests := []struct {
		name     string
		cfg      PLCConfig
		expected bool
	}{
		{"logix default", PLCConfig{Family: FamilyLogix}, true},
		{"logix discover=false", PLCConfig{Family: FamilyLogix, DiscoverTags: boolPtr(false)}, false},
		{"logix discover=true", PLCConfig{Family: FamilyLogix, DiscoverTags: boolPtr(true)}, true},
		{"s7 default", PLCConfig{Family: FamilyS7}, false},
		{"s7 discover=true", PLCConfig{Family: FamilyS7, DiscoverTags: boolPtr(true)}, true},
		{"beckhoff default", PLCConfig{Family: FamilyBeckhoff}, true},
		{"beckhoff discover=false", PLCConfig{Family: FamilyBeckhoff, DiscoverTags: boolPtr(false)}, false},
		{"omron fins default", PLCConfig{Family: FamilyOmron, Protocol: "fins"}, false},
		{"omron eip default", PLCConfig{Family: FamilyOmron, Protocol: "eip"}, true},
		{"omron eip discover=false", PLCConfig{Family: FamilyOmron, Protocol: "eip", DiscoverTags: boolPtr(false)}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.cfg.SupportsDiscovery()
			if result != tc.expected {
				t.Errorf("SupportsDiscovery() = %v, want %v", result, tc.expected)
			}
		})
	}
}

func TestPLCConfig_GetFamily(t *testing.T) {
	t.Run("returns set family", func(t *testing.T) {
		plc := PLCConfig{Family: FamilyS7}
		if plc.GetFamily() != FamilyS7 {
			t.Error("expected FamilyS7")
		}
	})

	t.Run("defaults to logix", func(t *testing.T) {
		plc := PLCConfig{}
		if plc.GetFamily() != FamilyLogix {
			t.Error("expected FamilyLogix as default")
		}
	})
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg == nil {
		t.Fatal("DefaultConfig returned nil")
	}
	if cfg.PollRate != time.Second {
		t.Errorf("expected 1s poll rate, got %v", cfg.PollRate)
	}
	if !cfg.Web.Enabled {
		t.Error("expected Web.Enabled true by default")
	}
	if !cfg.Web.UI.Enabled {
		t.Error("expected Web.UI.Enabled true by default")
	}
	if !cfg.Web.API.Enabled {
		t.Error("expected Web.API.Enabled true by default")
	}
	if cfg.Web.Port != 8080 {
		t.Errorf("expected Web port 8080, got %d", cfg.Web.Port)
	}
	if cfg.Web.Host != "0.0.0.0" {
		t.Errorf("expected Web host 0.0.0.0, got %s", cfg.Web.Host)
	}
	if len(cfg.PLCs) != 0 {
		t.Errorf("expected empty PLCs slice")
	}
}

func TestDefaultMQTTConfig(t *testing.T) {
	mqtt := DefaultMQTTConfig("test")

	if mqtt.Name != "test" {
		t.Errorf("expected name 'test', got %s", mqtt.Name)
	}
	if mqtt.Broker != "localhost" {
		t.Errorf("expected broker 'localhost', got %s", mqtt.Broker)
	}
	if mqtt.Port != 1883 {
		t.Errorf("expected port 1883, got %d", mqtt.Port)
	}
	// Selector is empty by default (namespace handles base path)
	if mqtt.Selector != "" {
		t.Errorf("expected selector '', got %s", mqtt.Selector)
	}
}

func TestDefaultValkeyConfig(t *testing.T) {
	valkey := DefaultValkeyConfig("test")

	if valkey.Name != "test" {
		t.Errorf("expected name 'test', got %s", valkey.Name)
	}
	if valkey.Address != "localhost:6379" {
		t.Errorf("expected address 'localhost:6379', got %s", valkey.Address)
	}
	if !valkey.PublishChanges {
		t.Error("expected PublishChanges to be true")
	}
}

func TestDefaultKafkaConfig(t *testing.T) {
	kafka := DefaultKafkaConfig("test")

	if kafka.Name != "test" {
		t.Errorf("expected name 'test', got %s", kafka.Name)
	}
	if len(kafka.Brokers) != 1 || kafka.Brokers[0] != "localhost:9092" {
		t.Errorf("expected brokers ['localhost:9092'], got %v", kafka.Brokers)
	}
	if kafka.RequiredAcks != -1 {
		t.Errorf("expected RequiredAcks -1, got %d", kafka.RequiredAcks)
	}
}

func TestDefaultTriggerConfig(t *testing.T) {
	trigger := DefaultTriggerConfig("test")

	if trigger.Name != "test" {
		t.Errorf("expected name 'test', got %s", trigger.Name)
	}
	if trigger.Condition.Operator != "==" {
		t.Errorf("expected operator '==', got %s", trigger.Condition.Operator)
	}
	if trigger.DebounceMS != 100 {
		t.Errorf("expected DebounceMS 100, got %d", trigger.DebounceMS)
	}
}

func TestLoadAndSave(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("returns default for nonexistent file", func(t *testing.T) {
		cfg, err := Load(filepath.Join(tmpDir, "nonexistent.yaml"))
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if cfg.PollRate != time.Second {
			t.Error("expected default config")
		}
	})

	t.Run("save and load roundtrip", func(t *testing.T) {
		path := filepath.Join(tmpDir, "test.yaml")

		cfg := &Config{
			PollRate: 500 * time.Millisecond,
			PLCs: []PLCConfig{
				{Name: "TestPLC", Address: "192.168.1.100", Enabled: true},
			},
			REST: RESTConfig{Enabled: true, Port: 9090},
			MQTT: []MQTTConfig{
				{Name: "TestMQTT", Broker: "mqtt.local", Port: 1883},
			},
		}

		if err := cfg.Save(path); err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		loaded, err := Load(path)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		if loaded.PollRate != 500*time.Millisecond {
			t.Errorf("expected 500ms poll rate, got %v", loaded.PollRate)
		}
		if len(loaded.PLCs) != 1 || loaded.PLCs[0].Name != "TestPLC" {
			t.Error("PLC config not preserved")
		}
		// REST config migrates to Web on load
		if loaded.Web.Port != 9090 {
			t.Errorf("REST->Web migration: expected port 9090, got %d", loaded.Web.Port)
		}
		if !loaded.Web.API.Enabled {
			t.Error("REST->Web migration: expected API enabled")
		}
		if len(loaded.MQTT) != 1 || loaded.MQTT[0].Broker != "mqtt.local" {
			t.Error("MQTT config not preserved")
		}
	})

	t.Run("creates directory if needed", func(t *testing.T) {
		path := filepath.Join(tmpDir, "subdir", "nested", "config.yaml")
		cfg := DefaultConfig()

		if err := cfg.Save(path); err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Error("config file was not created")
		}
	})

	t.Run("returns error for invalid yaml", func(t *testing.T) {
		path := filepath.Join(tmpDir, "invalid.yaml")
		os.WriteFile(path, []byte("invalid: yaml: content: ["), 0644)

		_, err := Load(path)
		if err == nil {
			t.Error("expected error for invalid YAML")
		}
	})
}

func TestPLCOperations(t *testing.T) {
	cfg := DefaultConfig()

	t.Run("AddPLC and FindPLC", func(t *testing.T) {
		plc := PLCConfig{Name: "PLC1", Address: "192.168.1.1"}
		cfg.AddPLC(plc)

		found := cfg.FindPLC("PLC1")
		if found == nil {
			t.Fatal("FindPLC returned nil")
		}
		if found.Address != "192.168.1.1" {
			t.Errorf("expected address '192.168.1.1', got %s", found.Address)
		}
	})

	t.Run("FindPLC returns nil for nonexistent", func(t *testing.T) {
		if cfg.FindPLC("nonexistent") != nil {
			t.Error("expected nil for nonexistent PLC")
		}
	})

	t.Run("UpdatePLC", func(t *testing.T) {
		updated := PLCConfig{Name: "PLC1", Address: "192.168.1.2", Enabled: true}
		if !cfg.UpdatePLC("PLC1", updated) {
			t.Error("UpdatePLC returned false")
		}

		found := cfg.FindPLC("PLC1")
		if found.Address != "192.168.1.2" {
			t.Error("PLC not updated")
		}
	})

	t.Run("UpdatePLC returns false for nonexistent", func(t *testing.T) {
		if cfg.UpdatePLC("nonexistent", PLCConfig{}) {
			t.Error("expected false for nonexistent PLC")
		}
	})

	t.Run("RemovePLC", func(t *testing.T) {
		if !cfg.RemovePLC("PLC1") {
			t.Error("RemovePLC returned false")
		}
		if cfg.FindPLC("PLC1") != nil {
			t.Error("PLC not removed")
		}
	})

	t.Run("RemovePLC returns false for nonexistent", func(t *testing.T) {
		if cfg.RemovePLC("nonexistent") {
			t.Error("expected false for nonexistent PLC")
		}
	})
}

func TestMQTTOperations(t *testing.T) {
	cfg := DefaultConfig()

	t.Run("AddMQTT and FindMQTT", func(t *testing.T) {
		mqtt := MQTTConfig{Name: "Broker1", Broker: "mqtt.local"}
		cfg.AddMQTT(mqtt)

		found := cfg.FindMQTT("Broker1")
		if found == nil {
			t.Fatal("FindMQTT returned nil")
		}
		if found.Broker != "mqtt.local" {
			t.Errorf("expected broker 'mqtt.local', got %s", found.Broker)
		}
	})

	t.Run("UpdateMQTT", func(t *testing.T) {
		updated := MQTTConfig{Name: "Broker1", Broker: "mqtt2.local", Port: 8883}
		if !cfg.UpdateMQTT("Broker1", updated) {
			t.Error("UpdateMQTT returned false")
		}

		found := cfg.FindMQTT("Broker1")
		if found.Port != 8883 {
			t.Error("MQTT not updated")
		}
	})

	t.Run("RemoveMQTT", func(t *testing.T) {
		if !cfg.RemoveMQTT("Broker1") {
			t.Error("RemoveMQTT returned false")
		}
		if cfg.FindMQTT("Broker1") != nil {
			t.Error("MQTT not removed")
		}
	})
}

func TestValkeyOperations(t *testing.T) {
	cfg := DefaultConfig()

	t.Run("AddValkey and FindValkey", func(t *testing.T) {
		valkey := ValkeyConfig{Name: "Redis1", Address: "localhost:6379"}
		cfg.AddValkey(valkey)

		found := cfg.FindValkey("Redis1")
		if found == nil {
			t.Fatal("FindValkey returned nil")
		}
		if found.Address != "localhost:6379" {
			t.Errorf("expected address 'localhost:6379', got %s", found.Address)
		}
	})

	t.Run("UpdateValkey", func(t *testing.T) {
		updated := ValkeyConfig{Name: "Redis1", Address: "redis.local:6380"}
		if !cfg.UpdateValkey("Redis1", updated) {
			t.Error("UpdateValkey returned false")
		}

		found := cfg.FindValkey("Redis1")
		if found.Address != "redis.local:6380" {
			t.Error("Valkey not updated")
		}
	})

	t.Run("RemoveValkey", func(t *testing.T) {
		if !cfg.RemoveValkey("Redis1") {
			t.Error("RemoveValkey returned false")
		}
		if cfg.FindValkey("Redis1") != nil {
			t.Error("Valkey not removed")
		}
	})
}

func TestKafkaOperations(t *testing.T) {
	cfg := DefaultConfig()

	t.Run("AddKafka and FindKafka", func(t *testing.T) {
		kafka := KafkaConfig{Name: "Cluster1", Brokers: []string{"kafka:9092"}}
		cfg.AddKafka(kafka)

		found := cfg.FindKafka("Cluster1")
		if found == nil {
			t.Fatal("FindKafka returned nil")
		}
		if len(found.Brokers) != 1 || found.Brokers[0] != "kafka:9092" {
			t.Errorf("expected brokers ['kafka:9092'], got %v", found.Brokers)
		}
	})

	t.Run("UpdateKafka", func(t *testing.T) {
		updated := KafkaConfig{Name: "Cluster1", Brokers: []string{"kafka1:9092", "kafka2:9092"}}
		if !cfg.UpdateKafka("Cluster1", updated) {
			t.Error("UpdateKafka returned false")
		}

		found := cfg.FindKafka("Cluster1")
		if len(found.Brokers) != 2 {
			t.Error("Kafka not updated")
		}
	})

	t.Run("RemoveKafka", func(t *testing.T) {
		if !cfg.RemoveKafka("Cluster1") {
			t.Error("RemoveKafka returned false")
		}
		if cfg.FindKafka("Cluster1") != nil {
			t.Error("Kafka not removed")
		}
	})
}

func TestTriggerOperations(t *testing.T) {
	cfg := DefaultConfig()

	t.Run("AddTrigger and FindTrigger", func(t *testing.T) {
		trigger := TriggerConfig{Name: "Trigger1", PLC: "MainPLC", TriggerTag: "Ready"}
		cfg.AddTrigger(trigger)

		found := cfg.FindTrigger("Trigger1")
		if found == nil {
			t.Fatal("FindTrigger returned nil")
		}
		if found.TriggerTag != "Ready" {
			t.Errorf("expected trigger_tag 'Ready', got %s", found.TriggerTag)
		}
	})

	t.Run("UpdateTrigger", func(t *testing.T) {
		updated := TriggerConfig{Name: "Trigger1", PLC: "MainPLC", TriggerTag: "Complete"}
		if !cfg.UpdateTrigger("Trigger1", updated) {
			t.Error("UpdateTrigger returned false")
		}

		found := cfg.FindTrigger("Trigger1")
		if found.TriggerTag != "Complete" {
			t.Error("Trigger not updated")
		}
	})

	t.Run("RemoveTrigger", func(t *testing.T) {
		if !cfg.RemoveTrigger("Trigger1") {
			t.Error("RemoveTrigger returned false")
		}
		if cfg.FindTrigger("Trigger1") != nil {
			t.Error("Trigger not removed")
		}
	})
}

func TestRESTMigration(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "migrate.yaml")

	// Write a config with legacy REST enabled and web disabled
	os.WriteFile(path, []byte(`
rest:
  enabled: true
  host: "127.0.0.1"
  port: 9090
web:
  enabled: false
  host: "0.0.0.0"
  port: 8080
  ui:
    users:
      - username: existing
        password_hash: "$2a$10$test"
        role: admin
`), 0644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if !cfg.Web.Enabled {
		t.Error("expected Web.Enabled after migration")
	}
	if !cfg.Web.API.Enabled {
		t.Error("expected Web.API.Enabled after migration")
	}
	if cfg.Web.Host != "127.0.0.1" {
		t.Errorf("expected Web.Host '127.0.0.1', got %s", cfg.Web.Host)
	}
	if cfg.Web.Port != 9090 {
		t.Errorf("expected Web.Port 9090, got %d", cfg.Web.Port)
	}
	if cfg.REST.Enabled {
		t.Error("expected REST.Enabled to be zeroed")
	}
}

func TestNoAutoAdminCreation(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "autoadmin.yaml")

	// Write a config with no users
	os.WriteFile(path, []byte(`
namespace: test
web:
  enabled: true
  host: "0.0.0.0"
  port: 8080
  ui:
    enabled: true
`), 0644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// No auto-admin should be created (setup wizard handles first user)
	if len(cfg.Web.UI.Users) != 0 {
		t.Fatalf("expected 0 users (no auto-admin), got %d", len(cfg.Web.UI.Users))
	}

	// Session secret should still be generated
	if cfg.Web.UI.SessionSecret == "" {
		t.Error("expected session secret to be generated")
	}
}

func TestDefaultPath(t *testing.T) {
	path := DefaultPath()
	if path == "" {
		t.Error("DefaultPath returned empty string")
	}
	if !filepath.IsAbs(path) && path != "config.yaml" {
		t.Error("expected absolute path or 'config.yaml'")
	}
}
