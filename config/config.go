// Package config handles configuration persistence for the Wargate application.
package config

import (
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// PLCFamily represents the type/protocol family of a PLC.
type PLCFamily string

const (
	FamilyLogix    PLCFamily = "logix"    // Allen-Bradley ControlLogix/CompactLogix
	FamilyMicro800 PLCFamily = "micro800" // Allen-Bradley Micro800 series
	FamilyS7       PLCFamily = "s7"       // Siemens S7
	FamilyOmron    PLCFamily = "omron"    // Omron FINS protocol
	FamilyBeckhoff PLCFamily = "beckhoff" // Beckhoff TwinCAT (ADS protocol)
)

// SupportsDiscovery returns true if the PLC family supports tag discovery.
func (f PLCFamily) SupportsDiscovery() bool {
	return f == FamilyLogix || f == "" || f == FamilyMicro800 || f == FamilyBeckhoff
}

// String returns the string representation of the PLC family.
func (f PLCFamily) String() string {
	if f == "" {
		return "logix"
	}
	return string(f)
}

// Config holds the complete application configuration.
type Config struct {
	PLCs     []PLCConfig      `yaml:"plcs"`
	REST     RESTConfig       `yaml:"rest"`
	MQTT     []MQTTConfig     `yaml:"mqtt"`
	Valkey   []ValkeyConfig   `yaml:"valkey,omitempty"`
	Kafka    []KafkaConfig    `yaml:"kafka,omitempty"`
	Triggers []TriggerConfig  `yaml:"triggers,omitempty"`
	PollRate time.Duration    `yaml:"poll_rate"`
}

// PLCConfig stores configuration for a single PLC connection.
type PLCConfig struct {
	Name               string         `yaml:"name"`
	Address            string         `yaml:"address"`
	Slot               byte           `yaml:"slot"`
	Family             PLCFamily      `yaml:"family,omitempty"`
	Enabled            bool           `yaml:"enabled"`
	HealthCheckEnabled *bool          `yaml:"health_check_enabled,omitempty"` // Publish health status (default true)
	PollRate           time.Duration  `yaml:"poll_rate,omitempty"`            // Per-PLC poll rate (0 = use global)
	Tags               []TagSelection `yaml:"tags,omitempty"`

	// Beckhoff/TwinCAT-specific settings
	AmsNetId string `yaml:"ams_net_id,omitempty"` // AMS Net ID (e.g., "192.168.1.100.1.1")
	AmsPort  uint16 `yaml:"ams_port,omitempty"`   // AMS Port (default: 851 for TwinCAT 3)

	// Omron FINS-specific settings
	FinsPort    int  `yaml:"fins_port,omitempty"`    // FINS UDP port (default: 9600)
	FinsNetwork byte `yaml:"fins_network,omitempty"` // FINS network number (default: 0)
	FinsNode    byte `yaml:"fins_node,omitempty"`    // FINS node number (default: 0)
	FinsUnit    byte `yaml:"fins_unit,omitempty"`    // FINS unit number (default: 0)
}

// GetFamily returns the PLC family, defaulting to logix if not set.
func (p *PLCConfig) GetFamily() PLCFamily {
	if p.Family == "" {
		return FamilyLogix
	}
	return p.Family
}

// IsHealthCheckEnabled returns whether health check publishing is enabled (defaults to true).
func (p *PLCConfig) IsHealthCheckEnabled() bool {
	if p.HealthCheckEnabled == nil {
		return true
	}
	return *p.HealthCheckEnabled
}

// TagSelection represents a tag selected for republishing.
type TagSelection struct {
	Name          string   `yaml:"name"`
	Alias         string   `yaml:"alias,omitempty"`
	DataType      string   `yaml:"data_type,omitempty"` // Manual type: BOOL, INT, DINT, REAL, etc.
	Enabled       bool     `yaml:"enabled"`
	Writable      bool     `yaml:"writable,omitempty"`
	IgnoreChanges []string `yaml:"ignore_changes,omitempty"` // UDT member names to ignore for change detection
}

// ShouldIgnoreMember returns true if the given member name is in the ignore list.
func (t *TagSelection) ShouldIgnoreMember(memberName string) bool {
	for _, ignored := range t.IgnoreChanges {
		if ignored == memberName {
			return true
		}
	}
	return false
}

// AddIgnoreMember adds a member name to the ignore list if not already present.
func (t *TagSelection) AddIgnoreMember(memberName string) {
	if !t.ShouldIgnoreMember(memberName) {
		t.IgnoreChanges = append(t.IgnoreChanges, memberName)
	}
}

// RemoveIgnoreMember removes a member name from the ignore list.
func (t *TagSelection) RemoveIgnoreMember(memberName string) {
	for i, ignored := range t.IgnoreChanges {
		if ignored == memberName {
			t.IgnoreChanges = append(t.IgnoreChanges[:i], t.IgnoreChanges[i+1:]...)
			return
		}
	}
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

// KafkaConfig holds Kafka cluster configuration.
type KafkaConfig struct {
	Name          string        `yaml:"name"`
	Enabled       bool          `yaml:"enabled"`
	Brokers       []string      `yaml:"brokers"`
	UseTLS        bool          `yaml:"use_tls,omitempty"`
	TLSSkipVerify bool          `yaml:"tls_skip_verify,omitempty"`
	SASLMechanism string        `yaml:"sasl_mechanism,omitempty"` // PLAIN, SCRAM-SHA-256, SCRAM-SHA-512
	Username      string        `yaml:"username,omitempty"`
	Password      string        `yaml:"password,omitempty"`
	RequiredAcks  int           `yaml:"required_acks,omitempty"` // -1=all, 0=none, 1=leader
	MaxRetries    int           `yaml:"max_retries,omitempty"`
	RetryBackoff  time.Duration `yaml:"retry_backoff,omitempty"`

	// Tag publishing settings
	PublishChanges bool   `yaml:"publish_changes,omitempty"` // Publish tag changes to Kafka
	Topic          string `yaml:"topic,omitempty"`           // Topic for tag change publishing
}

// TriggerCondition defines when a trigger fires.
type TriggerCondition struct {
	Operator string      `yaml:"operator"` // ==, !=, >, <, >=, <=
	Value    interface{} `yaml:"value"`    // Value to compare against
}

// TriggerConfig holds configuration for an event-driven data capture trigger.
type TriggerConfig struct {
	Name         string            `yaml:"name"`
	Enabled      bool              `yaml:"enabled"`
	PLC          string            `yaml:"plc"`                   // PLC name to monitor
	TriggerTag   string            `yaml:"trigger_tag"`           // Tag to watch for condition
	Condition    TriggerCondition  `yaml:"condition"`             // When to fire
	AckTag       string            `yaml:"ack_tag,omitempty"`     // Tag to write status: 1=success, -1=error
	DebounceMS   int               `yaml:"debounce_ms,omitempty"` // Debounce time in milliseconds
	Tags         []string          `yaml:"tags"`                  // Tags to capture when triggered
	KafkaCluster string            `yaml:"kafka_cluster"`         // Kafka cluster name
	Topic        string            `yaml:"topic"`                 // Kafka topic
	Metadata     map[string]string `yaml:"metadata,omitempty"`    // Static metadata to include
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
		MQTT:     []MQTTConfig{},
		Valkey:   []ValkeyConfig{},
		Kafka:    []KafkaConfig{},
		Triggers: []TriggerConfig{},
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

// DefaultKafkaConfig returns a default Kafka configuration.
func DefaultKafkaConfig(name string) KafkaConfig {
	return KafkaConfig{
		Name:         name,
		Enabled:      false,
		Brokers:      []string{"localhost:9092"},
		RequiredAcks: -1, // All replicas must acknowledge
		MaxRetries:   3,
		RetryBackoff: 100 * time.Millisecond,
	}
}

// DefaultTriggerConfig returns a default trigger configuration.
func DefaultTriggerConfig(name string) TriggerConfig {
	return TriggerConfig{
		Name:       name,
		Enabled:    false,
		Condition:  TriggerCondition{Operator: "==", Value: true},
		DebounceMS: 100,
		Tags:       []string{},
		Metadata:   make(map[string]string),
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

// FindKafka returns the Kafka config with the given name, or nil if not found.
func (c *Config) FindKafka(name string) *KafkaConfig {
	for i := range c.Kafka {
		if c.Kafka[i].Name == name {
			return &c.Kafka[i]
		}
	}
	return nil
}

// AddKafka adds a new Kafka configuration.
func (c *Config) AddKafka(kafka KafkaConfig) {
	c.Kafka = append(c.Kafka, kafka)
}

// RemoveKafka removes a Kafka config by name.
func (c *Config) RemoveKafka(name string) bool {
	for i, k := range c.Kafka {
		if k.Name == name {
			c.Kafka = append(c.Kafka[:i], c.Kafka[i+1:]...)
			return true
		}
	}
	return false
}

// UpdateKafka updates an existing Kafka configuration.
func (c *Config) UpdateKafka(name string, updated KafkaConfig) bool {
	for i, k := range c.Kafka {
		if k.Name == name {
			c.Kafka[i] = updated
			return true
		}
	}
	return false
}

// FindTrigger returns the Trigger config with the given name, or nil if not found.
func (c *Config) FindTrigger(name string) *TriggerConfig {
	for i := range c.Triggers {
		if c.Triggers[i].Name == name {
			return &c.Triggers[i]
		}
	}
	return nil
}

// AddTrigger adds a new Trigger configuration.
func (c *Config) AddTrigger(trigger TriggerConfig) {
	c.Triggers = append(c.Triggers, trigger)
}

// RemoveTrigger removes a Trigger config by name.
func (c *Config) RemoveTrigger(name string) bool {
	for i, t := range c.Triggers {
		if t.Name == name {
			c.Triggers = append(c.Triggers[:i], c.Triggers[i+1:]...)
			return true
		}
	}
	return false
}

// UpdateTrigger updates an existing Trigger configuration.
func (c *Config) UpdateTrigger(name string, updated TriggerConfig) bool {
	for i, t := range c.Triggers {
		if t.Name == name {
			c.Triggers[i] = updated
			return true
		}
	}
	return false
}
