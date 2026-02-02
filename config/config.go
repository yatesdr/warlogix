// Package config handles configuration persistence for the Wargate application.
package config

import (
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds the complete application configuration.
type Config struct {
	PLCs     []PLCConfig     `yaml:"plcs"`
	REST     RESTConfig      `yaml:"rest"`
	MQTT     []MQTTConfig    `yaml:"mqtt"`
	Valkey   []ValkeyConfig  `yaml:"valkey,omitempty"`
	PollRate time.Duration   `yaml:"poll_rate"`
}

// PLCConfig stores configuration for a single PLC connection.
type PLCConfig struct {
	Name    string         `yaml:"name"`
	Address string         `yaml:"address"`
	Slot    byte           `yaml:"slot"`
	Enabled bool           `yaml:"enabled"`
	Tags    []TagSelection `yaml:"tags,omitempty"`
}

// TagSelection represents a tag selected for republishing.
type TagSelection struct {
	Name     string `yaml:"name"`
	Alias    string `yaml:"alias,omitempty"`
	Enabled  bool   `yaml:"enabled"`
	Writable bool   `yaml:"writable,omitempty"`
}

// RESTConfig holds REST API server configuration.
type RESTConfig struct {
	Enabled bool   `yaml:"enabled"`
	Port    int    `yaml:"port"`
	Host    string `yaml:"host"`
}

// MQTTConfig holds MQTT publisher configuration.
type MQTTConfig struct {
	Name      string `yaml:"name"`
	Enabled   bool   `yaml:"enabled"`
	Broker    string `yaml:"broker"`
	Port      int    `yaml:"port"`
	Username  string `yaml:"username,omitempty"`
	Password  string `yaml:"password,omitempty"`
	ClientID  string `yaml:"client_id"`
	RootTopic string `yaml:"root_topic"`
	UseTLS    bool   `yaml:"use_tls,omitempty"`
}

// ValkeyConfig holds Valkey/Redis publisher configuration.
type ValkeyConfig struct {
	Name            string        `yaml:"name"`
	Enabled         bool          `yaml:"enabled"`
	Address         string        `yaml:"address"`         // host:port format
	Password        string        `yaml:"password,omitempty"`
	Database        int           `yaml:"database"`        // Redis DB number (default 0)
	Factory         string        `yaml:"factory"`         // Factory identifier (key prefix)
	UseTLS          bool          `yaml:"use_tls,omitempty"`
	KeyTTL          time.Duration `yaml:"key_ttl,omitempty"`          // TTL for keys (0 = no expiry)
	PublishChanges  bool          `yaml:"publish_changes,omitempty"`  // Publish to Pub/Sub on changes
	EnableWriteback bool          `yaml:"enable_writeback,omitempty"` // Enable write-back queue
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		PLCs:     []PLCConfig{},
		PollRate: time.Second,
		REST: RESTConfig{
			Enabled: false,
			Port:    8080,
			Host:    "0.0.0.0",
		},
		MQTT:   []MQTTConfig{},
		Valkey: []ValkeyConfig{},
	}
}

// DefaultMQTTConfig returns a default MQTT configuration.
func DefaultMQTTConfig(name string) MQTTConfig {
	return MQTTConfig{
		Name:      name,
		Enabled:   false,
		Broker:    "localhost",
		Port:      1883,
		ClientID:  "warlogix-" + name,
		RootTopic: "factory",
	}
}

// DefaultValkeyConfig returns a default Valkey configuration.
func DefaultValkeyConfig(name string) ValkeyConfig {
	return ValkeyConfig{
		Name:            name,
		Enabled:         false,
		Address:         "localhost:6379",
		Database:        0,
		Factory:         "factory",
		KeyTTL:          0,
		PublishChanges:  true,
		EnableWriteback: false,
	}
}

// FindMQTT returns the MQTT config with the given name, or nil if not found.
func (c *Config) FindMQTT(name string) *MQTTConfig {
	for i := range c.MQTT {
		if c.MQTT[i].Name == name {
			return &c.MQTT[i]
		}
	}
	return nil
}

// AddMQTT adds a new MQTT configuration.
func (c *Config) AddMQTT(mqtt MQTTConfig) {
	c.MQTT = append(c.MQTT, mqtt)
}

// RemoveMQTT removes an MQTT config by name.
func (c *Config) RemoveMQTT(name string) bool {
	for i, m := range c.MQTT {
		if m.Name == name {
			c.MQTT = append(c.MQTT[:i], c.MQTT[i+1:]...)
			return true
		}
	}
	return false
}

// UpdateMQTT updates an existing MQTT configuration.
func (c *Config) UpdateMQTT(name string, updated MQTTConfig) bool {
	for i, m := range c.MQTT {
		if m.Name == name {
			c.MQTT[i] = updated
			return true
		}
	}
	return false
}

// FindValkey returns the Valkey config with the given name, or nil if not found.
func (c *Config) FindValkey(name string) *ValkeyConfig {
	for i := range c.Valkey {
		if c.Valkey[i].Name == name {
			return &c.Valkey[i]
		}
	}
	return nil
}

// AddValkey adds a new Valkey configuration.
func (c *Config) AddValkey(valkey ValkeyConfig) {
	c.Valkey = append(c.Valkey, valkey)
}

// RemoveValkey removes a Valkey config by name.
func (c *Config) RemoveValkey(name string) bool {
	for i, v := range c.Valkey {
		if v.Name == name {
			c.Valkey = append(c.Valkey[:i], c.Valkey[i+1:]...)
			return true
		}
	}
	return false
}

// UpdateValkey updates an existing Valkey configuration.
func (c *Config) UpdateValkey(name string, updated ValkeyConfig) bool {
	for i, v := range c.Valkey {
		if v.Name == name {
			c.Valkey[i] = updated
			return true
		}
	}
	return false
}

// DefaultPath returns the default configuration file path (~/.warlogix/config.yaml).
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "config.yaml"
	}
	return filepath.Join(home, ".warlogix", "config.yaml")
}

// Load reads configuration from a YAML file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, err
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Save writes configuration to a YAML file.
func (c *Config) Save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// FindPLC returns the PLC config with the given name, or nil if not found.
func (c *Config) FindPLC(name string) *PLCConfig {
	for i := range c.PLCs {
		if c.PLCs[i].Name == name {
			return &c.PLCs[i]
		}
	}
	return nil
}

// AddPLC adds a new PLC configuration.
func (c *Config) AddPLC(plc PLCConfig) {
	c.PLCs = append(c.PLCs, plc)
}

// RemovePLC removes a PLC by name.
func (c *Config) RemovePLC(name string) bool {
	for i, plc := range c.PLCs {
		if plc.Name == name {
			c.PLCs = append(c.PLCs[:i], c.PLCs[i+1:]...)
			return true
		}
	}
	return false
}

// UpdatePLC updates an existing PLC configuration.
func (c *Config) UpdatePLC(name string, updated PLCConfig) bool {
	for i, plc := range c.PLCs {
		if plc.Name == name {
			c.PLCs[i] = updated
			return true
		}
	}
	return false
}
