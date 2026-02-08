// Package config handles configuration persistence for the Wargate application.
package config

import (
	"fmt"
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
	FamilyOmron    PLCFamily = "omron"    // Omron PLCs (FINS or EIP based on Protocol field)
	FamilyBeckhoff PLCFamily = "beckhoff" // Beckhoff TwinCAT (ADS protocol)
)

// SupportsDiscovery returns true if the PLC family supports tag discovery.
// Note: For Omron PLCs, discovery depends on the protocol (EIP supports it, FINS doesn't).
// Use PLCConfig.SupportsDiscovery() for protocol-aware check.
func (f PLCFamily) SupportsDiscovery() bool {
	// Omron discovery depends on protocol, so we return false here.
	// PLCConfig.SupportsDiscovery() handles the protocol-aware check.
	return f == FamilyLogix || f == "" || f == FamilyMicro800 || f == FamilyBeckhoff
}

// String returns the string representation of the PLC family.
func (f PLCFamily) String() string {
	if f == "" {
		return "logix"
	}
	return string(f)
}

// Driver returns the driver/protocol name used by this PLC family.
// Returns: "logix", "s7", "ads", or "omron".
// Note: For Omron, the actual protocol (FINS vs EIP) is determined by PLCConfig.Protocol.
func (f PLCFamily) Driver() string {
	switch f {
	case FamilyS7:
		return "s7"
	case FamilyBeckhoff:
		return "ads"
	case FamilyOmron:
		return "omron"
	default:
		return "logix" // Logix, Micro800, and empty default to logix
	}
}

// Config holds the complete application configuration.
type Config struct {
	Namespace string           `yaml:"namespace"` // Required: instance namespace for topic/key isolation
	PLCs      []PLCConfig      `yaml:"plcs"`
	REST      RESTConfig       `yaml:"rest"`
	MQTT      []MQTTConfig     `yaml:"mqtt"`
	Valkey    []ValkeyConfig   `yaml:"valkey,omitempty"`
	Kafka     []KafkaConfig    `yaml:"kafka,omitempty"`
	Triggers  []TriggerConfig  `yaml:"triggers,omitempty"`
	TagPacks  []TagPackConfig  `yaml:"tag_packs,omitempty"`
	PollRate  time.Duration    `yaml:"poll_rate"`
	UI        UIConfig         `yaml:"ui,omitempty"`
}

// TagPackConfig holds configuration for a Tag Pack.
type TagPackConfig struct {
	Name          string          `yaml:"name"`
	Enabled       bool            `yaml:"enabled"`
	MQTTEnabled   bool            `yaml:"mqtt_enabled"`
	KafkaEnabled  bool            `yaml:"kafka_enabled"`
	ValkeyEnabled bool            `yaml:"valkey_enabled"`
	Members       []TagPackMember `yaml:"members"`
}

// TagPackMember represents a single tag in a TagPack.
type TagPackMember struct {
	PLC           string `yaml:"plc"`             // PLC name
	Tag           string `yaml:"tag"`             // Tag name (uses alias if set)
	IgnoreChanges bool   `yaml:"ignore_changes"`  // If true, changes to this tag don't trigger publish
}

// UIConfig stores user interface preferences.
type UIConfig struct {
	Theme string `yaml:"theme,omitempty"` // Theme name: default, retro, mono, amber, highcontrast
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

	// Omron-specific settings
	Protocol    string `yaml:"protocol,omitempty"`     // Protocol: "fins" (default) or "eip"
	FinsPort    int    `yaml:"fins_port,omitempty"`    // FINS port (default: 9600)
	FinsNetwork byte   `yaml:"fins_network,omitempty"` // FINS network number (default: 0)
	FinsNode    byte   `yaml:"fins_node,omitempty"`    // FINS node number (default: 0)
	FinsUnit    byte   `yaml:"fins_unit,omitempty"`    // FINS unit number (default: 0)
}

// GetFamily returns the PLC family, defaulting to logix if not set.
func (p *PLCConfig) GetFamily() PLCFamily {
	if p.Family == "" {
		return FamilyLogix
	}
	return p.Family
}

// GetProtocol returns the protocol for Omron PLCs ("fins" or "eip").
// Returns "fins" as default for Omron PLCs, empty for non-Omron.
func (p *PLCConfig) GetProtocol() string {
	if p.GetFamily() != FamilyOmron {
		return ""
	}
	if p.Protocol == "" || p.Protocol == "fins" {
		return "fins"
	}
	return p.Protocol
}

// IsOmronEIP returns true if this is an Omron PLC using EtherNet/IP protocol.
func (p *PLCConfig) IsOmronEIP() bool {
	return p.GetFamily() == FamilyOmron && p.GetProtocol() == "eip"
}

// IsOmronFINS returns true if this is an Omron PLC using FINS protocol.
func (p *PLCConfig) IsOmronFINS() bool {
	return p.GetFamily() == FamilyOmron && p.GetProtocol() == "fins"
}

// SupportsDiscovery returns true if this PLC configuration supports tag discovery.
// This is protocol-aware for Omron PLCs (EIP supports discovery, FINS doesn't).
func (p *PLCConfig) SupportsDiscovery() bool {
	family := p.GetFamily()
	if family == FamilyOmron {
		return p.IsOmronEIP() // Only EIP supports discovery
	}
	return family.SupportsDiscovery()
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
	// Service inhibit flags - when true, tag is NOT published to that service
	NoREST   bool `yaml:"no_rest,omitempty"`
	NoMQTT   bool `yaml:"no_mqtt,omitempty"`
	NoKafka  bool `yaml:"no_kafka,omitempty"`
	NoValkey bool `yaml:"no_valkey,omitempty"`
}

// PublishesToAny returns true if the tag publishes to at least one service.
func (t *TagSelection) PublishesToAny() bool {
	return !t.NoREST || !t.NoMQTT || !t.NoKafka || !t.NoValkey
}

// GetEnabledServices returns a list of service names this tag publishes to.
func (t *TagSelection) GetEnabledServices() []string {
	var services []string
	if !t.NoREST {
		services = append(services, "REST")
	}
	if !t.NoMQTT {
		services = append(services, "MQTT")
	}
	if !t.NoKafka {
		services = append(services, "Kafka")
	}
	if !t.NoValkey {
		services = append(services, "Valkey")
	}
	return services
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
	Name     string `yaml:"name"`
	Enabled  bool   `yaml:"enabled"`
	Broker   string `yaml:"broker"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
	ClientID string `yaml:"client_id"`
	Selector string `yaml:"selector,omitempty"` // Optional sub-namespace
	UseTLS   bool   `yaml:"use_tls,omitempty"`
}

// ValkeyConfig holds Valkey/Redis publisher configuration.
type ValkeyConfig struct {
	Name            string        `yaml:"name"`
	Enabled         bool          `yaml:"enabled"`
	Address         string        `yaml:"address"` // host:port format
	Password        string        `yaml:"password,omitempty"`
	Database        int           `yaml:"database"`           // Redis DB number (default 0)
	Selector        string        `yaml:"selector,omitempty"` // Optional sub-namespace
	UseTLS          bool          `yaml:"use_tls,omitempty"`
	KeyTTL          time.Duration `yaml:"key_ttl,omitempty"`          // TTL for keys (0 = no expiry)
	PublishChanges  bool          `yaml:"publish_changes,omitempty"`  // Publish to Pub/Sub on changes
	EnableWriteback bool          `yaml:"enable_writeback,omitempty"` // Enable write-back queue
}

// KafkaConfig holds Kafka cluster configuration for YAML persistence.
// Note: This struct uses pointer types (e.g., *bool) for optional fields to distinguish
// between "not set" (nil = use default) and "explicitly set to false".
// The kafka package has its own Config struct with non-pointer types for runtime use.
// Conversion happens in main.go when loading configs into the kafka manager.
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
	PublishChanges   bool   `yaml:"publish_changes,omitempty"`    // Publish tag changes to Kafka
	Selector         string `yaml:"selector,omitempty"`           // Optional sub-namespace
	AutoCreateTopics *bool  `yaml:"auto_create_topics,omitempty"` // Auto-create topics if they don't exist (default true)

	// Writeback settings
	EnableWriteback bool          `yaml:"enable_writeback,omitempty"` // Enable consuming write requests from Kafka
	ConsumerGroup   string        `yaml:"consumer_group,omitempty"`   // Consumer group ID (default: warlogix-{name}-writers)
	WriteMaxAge     time.Duration `yaml:"write_max_age,omitempty"`    // Max age of write requests to process (default: 2s)
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
	PLC          string            `yaml:"plc"`                    // PLC name to monitor
	TriggerTag   string            `yaml:"trigger_tag"`            // Tag to watch for condition
	Condition    TriggerCondition  `yaml:"condition"`              // When to fire
	AckTag       string            `yaml:"ack_tag,omitempty"`      // Tag to write status: 1=success, -1=error
	DebounceMS   int               `yaml:"debounce_ms,omitempty"`  // Debounce time in milliseconds
	Tags         []string          `yaml:"tags"`                   // Tags to capture when triggered
	MQTTBroker   string            `yaml:"mqtt_broker"`            // MQTT broker: "all", "none", or specific broker name
	KafkaCluster string            `yaml:"kafka_cluster"`          // Kafka cluster: "all", "none", or specific cluster name
	Selector     string            `yaml:"selector,omitempty"`     // Optional sub-namespace for topic
	Metadata     map[string]string `yaml:"metadata,omitempty"`     // Static metadata to include
	PublishPack  string            `yaml:"publish_pack,omitempty"` // TagPack name to republish on trigger
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
		Name:     name,
		Enabled:  false,
		Broker:   "localhost",
		Port:     1883,
		ClientID: "warlogix-" + name,
	}
}

// DefaultValkeyConfig returns a default Valkey configuration.
func DefaultValkeyConfig(name string) ValkeyConfig {
	return ValkeyConfig{
		Name:            name,
		Enabled:         false,
		Address:         "localhost:6379",
		Database:        0,
		KeyTTL:          0,
		PublishChanges:  true,
		EnableWriteback: false,
	}
}

// DefaultKafkaConfig returns a default Kafka configuration.
func DefaultKafkaConfig(name string) KafkaConfig {
	return KafkaConfig{
		Name:            name,
		Enabled:         false,
		Brokers:         []string{"localhost:9092"},
		RequiredAcks:    -1, // All replicas must acknowledge
		MaxRetries:      3,
		RetryBackoff:    100 * time.Millisecond,
		EnableWriteback: false,
		WriteMaxAge:     2 * time.Second, // Ignore write requests older than 2 seconds
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

// FindTagPack returns the TagPack config with the given name, or nil if not found.
func (c *Config) FindTagPack(name string) *TagPackConfig {
	for i := range c.TagPacks {
		if c.TagPacks[i].Name == name {
			return &c.TagPacks[i]
		}
	}
	return nil
}

// AddTagPack adds a new TagPack configuration.
func (c *Config) AddTagPack(pack TagPackConfig) {
	c.TagPacks = append(c.TagPacks, pack)
}

// RemoveTagPack removes a TagPack config by name.
func (c *Config) RemoveTagPack(name string) bool {
	for i, p := range c.TagPacks {
		if p.Name == name {
			c.TagPacks = append(c.TagPacks[:i], c.TagPacks[i+1:]...)
			return true
		}
	}
	return false
}

// UpdateTagPack updates an existing TagPack configuration.
func (c *Config) UpdateTagPack(name string, updated TagPackConfig) bool {
	for i, p := range c.TagPacks {
		if p.Name == name {
			c.TagPacks[i] = updated
			return true
		}
	}
	return false
}

// DefaultTagPackConfig returns a default TagPack configuration.
func DefaultTagPackConfig(name string) TagPackConfig {
	return TagPackConfig{
		Name:          name,
		Enabled:       true,
		MQTTEnabled:   true,
		KafkaEnabled:  false,
		ValkeyEnabled: false,
		Members:       []TagPackMember{},
	}
}

// Validate checks the configuration for errors.
// Note: Empty namespace is allowed here - the TUI will prompt for it interactively.
func (c *Config) Validate() error {
	// Only validate namespace format if one is set
	if c.Namespace != "" && !IsValidNamespace(c.Namespace) {
		return fmt.Errorf("invalid namespace: must contain only alphanumeric characters, hyphens, and underscores")
	}
	return nil
}

// IsValidNamespace returns true if the namespace is valid.
// Valid namespaces contain only alphanumeric characters, hyphens, underscores, and dots.
func IsValidNamespace(ns string) bool {
	if ns == "" {
		return false
	}
	for _, r := range ns {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}
