// Package config handles configuration persistence for the Wargate application.
package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"
)

// ConfigListenerID is a unique identifier for a config change listener.
type ConfigListenerID string

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
	REST      RESTConfig       `yaml:"rest,omitempty"` // Deprecated: use Web instead
	Web       WebConfig        `yaml:"web"`
	MQTT      []MQTTConfig     `yaml:"mqtt"`
	Valkey    []ValkeyConfig   `yaml:"valkey,omitempty"`
	Kafka     []KafkaConfig    `yaml:"kafka,omitempty"`
	Rules     []RuleConfig     `yaml:"rules,omitempty"`
	TagPacks  []TagPackConfig  `yaml:"tag_packs,omitempty"`
	PollRate  time.Duration    `yaml:"poll_rate"`
	UI        UIConfig         `yaml:"ui,omitempty"`
	Warcry    WarcryConfig     `yaml:"warcry,omitempty"`

	// Data mutex protects all config fields against concurrent access.
	// Callers that modify config should Lock(), modify, then call UnlockAndSave().
	// Save() acquires the lock internally for callers that don't hold it.
	dataMu sync.Mutex `yaml:"-"`

	// Change listeners (not serialized)
	changeListeners map[ConfigListenerID]func() `yaml:"-"`
	listenersMu     sync.RWMutex                `yaml:"-"`
	listenerCounter uint64                      `yaml:"-"`
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
	Theme     string `yaml:"theme,omitempty"`      // Theme name: default, retro, mono, amber, highcontrast
	ASCIIMode bool   `yaml:"ascii_mode,omitempty"` // Use ASCII characters for borders (for terminals without Unicode)
}

// WarcryConfig holds configuration for the warcry TCP connector.
type WarcryConfig struct {
	Enabled    bool   `yaml:"enabled"`
	Listen     string `yaml:"listen"`      // e.g. "127.0.0.1:9999"
	BufferSize int    `yaml:"buffer_size"` // ring buffer entries, default 10000
}

// PLCConfig stores configuration for a single PLC connection.
type PLCConfig struct {
	Name               string         `yaml:"name"`
	Address            string         `yaml:"address"`
	Slot               byte           `yaml:"slot"`
	Family             PLCFamily      `yaml:"family,omitempty"`
	Enabled            bool           `yaml:"enabled"`
	DiscoverTags       *bool          `yaml:"discover_tags,omitempty"`        // Auto-discover tags on connect (default true for capable families)
	HealthCheckEnabled *bool          `yaml:"health_check_enabled,omitempty"` // Publish health status (default true)
	PollRate           time.Duration  `yaml:"poll_rate,omitempty"`            // Per-PLC poll rate (0 = use global)
	Timeout            time.Duration  `yaml:"timeout,omitempty"`              // Connection/operation timeout (0 = driver default)
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
// If DiscoverTags is explicitly set, that value is used. Otherwise, the family/protocol
// default applies (true for logix, micro800, beckhoff, omron-eip; false for s7, omron-fins).
func (p *PLCConfig) SupportsDiscovery() bool {
	if p.DiscoverTags != nil {
		return *p.DiscoverTags
	}
	family := p.GetFamily()
	if family == FamilyOmron {
		return p.IsOmronEIP() // Only EIP supports discovery
	}
	return family.SupportsDiscovery()
}

// IsDiscoverTagsExplicit returns whether DiscoverTags was explicitly set in config.
// IsAddressBased returns true if this PLC family uses address-based tag names
// (e.g. S7 "DB1.DBX0.0", Omron FINS "D100") where dots are literal parts of
// the address rather than hierarchy separators. This is independent of whether
// discovery is enabled.
func (p *PLCConfig) IsAddressBased() bool {
	family := p.GetFamily()
	if family == FamilyS7 {
		return true
	}
	if family == FamilyOmron && p.IsOmronFINS() {
		return true
	}
	return false
}

func (p *PLCConfig) IsDiscoverTagsExplicit() bool {
	return p.DiscoverTags != nil
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
// Deprecated: Use WebConfig instead. Kept for backwards compatibility during migration.
type RESTConfig struct {
	Enabled bool   `yaml:"enabled"`
	Port    int    `yaml:"port"`
	Host    string `yaml:"host"`
}

// WebConfig holds unified web server configuration.
type WebConfig struct {
	Enabled bool         `yaml:"enabled"`
	Host    string       `yaml:"host"`
	Port    int          `yaml:"port"`
	API     WebAPIConfig `yaml:"api"`
	UI      WebUIConfig  `yaml:"ui"`
}

// WebAPIConfig holds REST API settings.
type WebAPIConfig struct {
	Enabled bool `yaml:"enabled"`
}

// WebUIConfig holds browser UI settings.
type WebUIConfig struct {
	Enabled       bool      `yaml:"enabled"`
	SessionSecret string    `yaml:"session_secret,omitempty"`
	Users         []WebUser `yaml:"users,omitempty"`
}

// WebUser represents a web interface user.
type WebUser struct {
	Username           string `yaml:"username"`
	PasswordHash       string `yaml:"password_hash"`                  // bcrypt
	Role               string `yaml:"role"`                           // "admin" or "viewer"
	MustChangePassword bool   `yaml:"must_change_password,omitempty"` // Force password change on first login
}

// Web user roles
const (
	RoleAdmin  = "admin"
	RoleViewer = "viewer"
)

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
	ConsumerGroup   string        `yaml:"consumer_group,omitempty"`   // Consumer group ID (default: warlink-{name}-writers)
	WriteMaxAge     time.Duration `yaml:"write_max_age,omitempty"`    // Max age of write requests to process (default: 2s)
}

// RuleLogicMode determines how multiple conditions are combined.
type RuleLogicMode string

const (
	RuleLogicAND RuleLogicMode = "and"
	RuleLogicOR  RuleLogicMode = "or"
)

// RuleCondition defines a single condition for a rule.
type RuleCondition struct {
	PLC      string      `yaml:"plc" json:"plc"`
	Tag      string      `yaml:"tag" json:"tag"`
	Operator string      `yaml:"operator" json:"operator"` // ==, !=, >, <, >=, <=
	Value    interface{} `yaml:"value" json:"value"`
	Not      bool        `yaml:"not,omitempty" json:"not,omitempty"`
}

// RuleActionType identifies the kind of action a rule performs.
type RuleActionType string

const (
	ActionPublish   RuleActionType = "publish"
	ActionWebhook   RuleActionType = "webhook"
	ActionWriteback RuleActionType = "writeback"
)

// RuleAction defines a single action to execute when a rule fires or clears.
type RuleAction struct {
	Type RuleActionType `yaml:"type" json:"type"`
	Name string         `yaml:"name,omitempty" json:"name,omitempty"` // Label for logging/UI

	// --- Publish (type=publish) ---
	TagOrPack      string `yaml:"tag_or_pack,omitempty" json:"tag_or_pack,omitempty"`
	IncludeTrigger bool   `yaml:"include_trigger,omitempty" json:"include_trigger,omitempty"`
	MQTTBroker     string `yaml:"mqtt_broker,omitempty" json:"mqtt_broker,omitempty"`
	MQTTTopic      string `yaml:"mqtt_topic,omitempty" json:"mqtt_topic,omitempty"`
	KafkaCluster   string `yaml:"kafka_cluster,omitempty" json:"kafka_cluster,omitempty"`
	KafkaTopic     string `yaml:"kafka_topic,omitempty" json:"kafka_topic,omitempty"`

	// --- Webhook (type=webhook) ---
	URL         string            `yaml:"url,omitempty" json:"url,omitempty"`
	Method      string            `yaml:"method,omitempty" json:"method,omitempty"`
	ContentType string            `yaml:"content_type,omitempty" json:"content_type,omitempty"`
	Headers     map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	Body        string            `yaml:"body,omitempty" json:"body,omitempty"`
	Auth        RuleAuthConfig    `yaml:"auth,omitempty" json:"auth,omitempty"`
	Timeout     time.Duration     `yaml:"timeout,omitempty" json:"timeout,omitempty"`

	// --- Writeback (type=writeback) ---
	WritePLC   string      `yaml:"write_plc,omitempty" json:"write_plc,omitempty"`
	WriteTag   string      `yaml:"write_tag,omitempty" json:"write_tag,omitempty"`
	WriteValue interface{} `yaml:"write_value,omitempty" json:"write_value,omitempty"`
}

// RuleAuthType represents the authentication method for a webhook action.
type RuleAuthType string

const (
	RuleAuthNone         RuleAuthType = ""
	RuleAuthBearer       RuleAuthType = "bearer"
	RuleAuthBasic        RuleAuthType = "basic"
	RuleAuthJWT          RuleAuthType = "jwt"
	RuleAuthCustomHeader RuleAuthType = "custom_header"
)

// RuleAuthConfig holds authentication configuration for a webhook action.
type RuleAuthConfig struct {
	Type        RuleAuthType `yaml:"type,omitempty" json:"type,omitempty"`
	Token       string       `yaml:"token,omitempty" json:"token,omitempty"`
	Username    string       `yaml:"username,omitempty" json:"username,omitempty"`
	Password    string       `yaml:"password,omitempty" json:"password,omitempty"`
	HeaderName  string       `yaml:"header_name,omitempty" json:"header_name,omitempty"`
	HeaderValue string       `yaml:"header_value,omitempty" json:"header_value,omitempty"`
}

// RuleConfig holds configuration for an automation rule.
type RuleConfig struct {
	Name           string          `yaml:"name"`
	Enabled        bool            `yaml:"enabled"`
	Conditions     []RuleCondition `yaml:"conditions"`                // Up to 10
	LogicMode      RuleLogicMode   `yaml:"logic_mode,omitempty"`     // "and" (default) or "or"
	DebounceMS     int             `yaml:"debounce_ms,omitempty"`
	CooldownMS     int             `yaml:"cooldown_ms,omitempty"`    // Min interval before re-arm
	Actions        []RuleAction    `yaml:"actions"`                  // Fired on rising edge
	ClearedActions []RuleAction    `yaml:"cleared_actions,omitempty"` // Fired on falling edge
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		PLCs:     []PLCConfig{},
		PollRate: time.Second,
		Web: WebConfig{
			Enabled: true,
			Host:    "0.0.0.0",
			Port:    8080,
			API: WebAPIConfig{
				Enabled: true,
			},
			UI: WebUIConfig{
				Enabled: true,
			},
		},
		MQTT:     []MQTTConfig{},
		Valkey:   []ValkeyConfig{},
		Kafka:    []KafkaConfig{},
		Rules:    []RuleConfig{},
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

// DefaultPath returns the default configuration file path (~/.warlink/config.yaml).
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "config.yaml"
	}
	return filepath.Join(home, ".warlink", "config.yaml")
}

// Load reads configuration from a YAML file.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()
	dirty := false

	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		// File doesn't exist — use defaults, will save after auto-admin creation
		dirty = true
	} else {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, err
		}
	}

	// Migrate legacy REST config to Web config
	if cfg.REST.Enabled && !cfg.Web.Enabled {
		if cfg.REST.Host != "" {
			cfg.Web.Host = cfg.REST.Host
		}
		if cfg.REST.Port != 0 {
			cfg.Web.Port = cfg.REST.Port
		}
		cfg.Web.Enabled = true
		cfg.Web.API.Enabled = true
		cfg.REST = RESTConfig{} // Zero out legacy block
		dirty = true
	}

	// Generate session secret if not already set (needed for login/setup pages)
	if cfg.Web.UI.SessionSecret == "" {
		secret := make([]byte, 32)
		rand.Read(secret)
		cfg.Web.UI.SessionSecret = base64.StdEncoding.EncodeToString(secret)
		dirty = true
	}

	if dirty {
		cfg.Save(path) // Best-effort save
	}

	return cfg, nil
}

// AddOnChangeListener registers a callback to be called when the config is saved.
// Returns an ID that can be used to remove the listener later.
func (c *Config) AddOnChangeListener(cb func()) ConfigListenerID {
	c.listenersMu.Lock()
	defer c.listenersMu.Unlock()

	if c.changeListeners == nil {
		c.changeListeners = make(map[ConfigListenerID]func())
	}

	id := ConfigListenerID(fmt.Sprintf("listener-%d", atomic.AddUint64(&c.listenerCounter, 1)))
	c.changeListeners[id] = cb
	return id
}

// RemoveOnChangeListener removes a previously registered listener.
func (c *Config) RemoveOnChangeListener(id ConfigListenerID) {
	c.listenersMu.Lock()
	defer c.listenersMu.Unlock()

	delete(c.changeListeners, id)
}

// notifyChangeListeners calls all registered change listeners.
func (c *Config) notifyChangeListeners() {
	c.listenersMu.RLock()
	listeners := make([]func(), 0, len(c.changeListeners))
	for _, cb := range c.changeListeners {
		listeners = append(listeners, cb)
	}
	c.listenersMu.RUnlock()

	// Call listeners outside the lock to avoid deadlocks
	for _, cb := range listeners {
		go cb() // Run in goroutine to avoid blocking
	}
}

// Lock acquires the config data mutex for exclusive access.
// Use this before modifying config fields, then call UnlockAndSave.
func (c *Config) Lock() { c.dataMu.Lock() }

// Unlock releases the config data mutex without saving.
// Prefer UnlockAndSave when modifications were made.
func (c *Config) Unlock() { c.dataMu.Unlock() }

// Save acquires the lock, marshals, writes, and notifies.
// Use this when the caller does not already hold the lock.
func (c *Config) Save(path string) error {
	c.dataMu.Lock()
	return c.saveLocked(path)
}

// UnlockAndSave marshals, releases the lock, writes, and notifies.
// The caller must already hold the lock via Lock().
func (c *Config) UnlockAndSave(path string) error {
	return c.saveLocked(path)
}

// saveLocked marshals config (lock must be held), unlocks, then writes and notifies.
func (c *Config) saveLocked(path string) error {
	data, err := yaml.Marshal(c)
	c.dataMu.Unlock() // Release lock after marshal, before I/O

	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return err
	}

	// Notify listeners after successful save
	c.notifyChangeListeners()
	return nil
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

// FindRule returns the Rule config with the given name, or nil if not found.
func (c *Config) FindRule(name string) *RuleConfig {
	for i := range c.Rules {
		if c.Rules[i].Name == name {
			return &c.Rules[i]
		}
	}
	return nil
}

// AddRule adds a new Rule configuration.
func (c *Config) AddRule(rule RuleConfig) {
	c.Rules = append(c.Rules, rule)
}

// RemoveRule removes a Rule config by name.
func (c *Config) RemoveRule(name string) bool {
	for i, r := range c.Rules {
		if r.Name == name {
			c.Rules = append(c.Rules[:i], c.Rules[i+1:]...)
			return true
		}
	}
	return false
}

// UpdateRule updates an existing Rule configuration.
func (c *Config) UpdateRule(name string, updated RuleConfig) bool {
	for i, r := range c.Rules {
		if r.Name == name {
			c.Rules[i] = updated
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

// FindWebUser returns the web user with the given username, or nil if not found.
func (c *Config) FindWebUser(username string) *WebUser {
	for i := range c.Web.UI.Users {
		if c.Web.UI.Users[i].Username == username {
			return &c.Web.UI.Users[i]
		}
	}
	return nil
}

// AddWebUser adds a new web user.
func (c *Config) AddWebUser(user WebUser) {
	c.Web.UI.Users = append(c.Web.UI.Users, user)
}

// RemoveWebUser removes a web user by username.
func (c *Config) RemoveWebUser(username string) bool {
	for i, u := range c.Web.UI.Users {
		if u.Username == username {
			c.Web.UI.Users = append(c.Web.UI.Users[:i], c.Web.UI.Users[i+1:]...)
			return true
		}
	}
	return false
}

// UpdateWebUser updates an existing web user.
func (c *Config) UpdateWebUser(username string, updated WebUser) bool {
	for i, u := range c.Web.UI.Users {
		if u.Username == username {
			c.Web.UI.Users[i] = updated
			return true
		}
	}
	return false
}
