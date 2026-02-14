package engine

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"warlink/config"
)

// PLCHTTPRequest is the JSON-serializable form of PLC create/update fields.
type PLCHTTPRequest struct {
	Name               string `json:"name"`
	Address            string `json:"address"`
	Slot               int    `json:"slot"`
	Family             string `json:"family"`
	Enabled            bool   `json:"enabled"`
	HealthCheckEnabled *bool  `json:"health_check_enabled"`
	DiscoverTags       *bool  `json:"discover_tags"`
	PollRate           string `json:"poll_rate"`
	Timeout            string `json:"timeout"`
	AmsNetId           string `json:"ams_net_id"`
	AmsPort            int    `json:"ams_port"`
	Protocol           string `json:"protocol"`
	FinsPort           int    `json:"fins_port"`
	FinsNetwork        int    `json:"fins_network"`
	FinsNode           int    `json:"fins_node"`
	FinsUnit           int    `json:"fins_unit"`
}

// ToCreateRequest converts to an engine PLCCreateRequest.
func (r PLCHTTPRequest) ToCreateRequest() PLCCreateRequest {
	req := PLCCreateRequest{
		Name: r.Name, Address: r.Address, Slot: byte(r.Slot),
		Enabled: r.Enabled, HealthCheckEnabled: r.HealthCheckEnabled,
		DiscoverTags: r.DiscoverTags,
		AmsNetId: r.AmsNetId, AmsPort: uint16(r.AmsPort),
		Protocol: r.Protocol, FinsPort: r.FinsPort,
		FinsNetwork: byte(r.FinsNetwork), FinsNode: byte(r.FinsNode), FinsUnit: byte(r.FinsUnit),
	}
	if r.Family != "" {
		req.Family = config.PLCFamily(r.Family)
	}
	if r.PollRate != "" {
		if d, err := time.ParseDuration(r.PollRate); err == nil {
			req.PollRate = d
		}
	}
	if r.Timeout != "" {
		if d, err := time.ParseDuration(r.Timeout); err == nil {
			req.Timeout = d
		}
	}
	return req
}

// ToUpdateRequest converts to an engine PLCUpdateRequest.
func (r PLCHTTPRequest) ToUpdateRequest() PLCUpdateRequest {
	req := PLCUpdateRequest{
		Address: r.Address, Slot: byte(r.Slot),
		Enabled: r.Enabled, HealthCheckEnabled: r.HealthCheckEnabled,
		DiscoverTags: r.DiscoverTags,
		AmsNetId: r.AmsNetId, AmsPort: uint16(r.AmsPort),
		Protocol: r.Protocol, FinsPort: r.FinsPort,
		FinsNetwork: byte(r.FinsNetwork), FinsNode: byte(r.FinsNode), FinsUnit: byte(r.FinsUnit),
	}
	if r.Family != "" {
		req.Family = config.PLCFamily(r.Family)
	}
	if r.PollRate != "" {
		if d, err := time.ParseDuration(r.PollRate); err == nil {
			req.PollRate = d
		}
	}
	if r.Timeout != "" {
		if d, err := time.ParseDuration(r.Timeout); err == nil {
			req.Timeout = d
		}
	}
	return req
}

// MQTTHTTPRequest is the JSON-serializable form of MQTT create/update fields.
type MQTTHTTPRequest struct {
	Name     string `json:"name"`
	Broker   string `json:"broker"`
	Port     int    `json:"port"`
	ClientID string `json:"client_id"`
	Username string `json:"username"`
	Password string `json:"password"`
	Selector string `json:"selector"`
	UseTLS   bool   `json:"use_tls"`
	Enabled  bool   `json:"enabled"`
}

// ToCreateRequest converts to an engine MQTTCreateRequest.
func (r MQTTHTTPRequest) ToCreateRequest() MQTTCreateRequest {
	return MQTTCreateRequest{
		Name: r.Name, Broker: r.Broker, Port: r.Port,
		ClientID: r.ClientID, Username: r.Username, Password: r.Password,
		Selector: r.Selector, UseTLS: r.UseTLS, Enabled: r.Enabled,
	}
}

// ToUpdateRequest converts to an engine MQTTUpdateRequest.
func (r MQTTHTTPRequest) ToUpdateRequest() MQTTUpdateRequest {
	return MQTTUpdateRequest{
		Broker: r.Broker, Port: r.Port,
		ClientID: r.ClientID, Username: r.Username, Password: r.Password,
		Selector: r.Selector, UseTLS: r.UseTLS, Enabled: r.Enabled,
	}
}

// ValkeyHTTPRequest is the JSON-serializable form of Valkey create/update fields.
type ValkeyHTTPRequest struct {
	Name            string `json:"name"`
	Address         string `json:"address"`
	Password        string `json:"password"`
	Database        int    `json:"database"`
	Selector        string `json:"selector"`
	KeyTTL          string `json:"key_ttl"`
	UseTLS          bool   `json:"use_tls"`
	PublishChanges  bool   `json:"publish_changes"`
	EnableWriteback bool   `json:"enable_writeback"`
	Enabled         bool   `json:"enabled"`
}

func (r ValkeyHTTPRequest) parseKeyTTL() time.Duration {
	if r.KeyTTL != "" {
		if d, err := time.ParseDuration(r.KeyTTL); err == nil {
			return d
		}
	}
	return 0
}

// ToCreateRequest converts to an engine ValkeyCreateRequest.
func (r ValkeyHTTPRequest) ToCreateRequest() ValkeyCreateRequest {
	return ValkeyCreateRequest{
		Name: r.Name, Address: r.Address, Password: r.Password,
		Database: r.Database, Selector: r.Selector, KeyTTL: r.parseKeyTTL(),
		UseTLS: r.UseTLS, PublishChanges: r.PublishChanges,
		EnableWriteback: r.EnableWriteback, Enabled: r.Enabled,
	}
}

// ToUpdateRequest converts to an engine ValkeyUpdateRequest.
func (r ValkeyHTTPRequest) ToUpdateRequest() ValkeyUpdateRequest {
	return ValkeyUpdateRequest{
		Address: r.Address, Password: r.Password,
		Database: r.Database, Selector: r.Selector, KeyTTL: r.parseKeyTTL(),
		UseTLS: r.UseTLS, PublishChanges: r.PublishChanges,
		EnableWriteback: r.EnableWriteback, Enabled: r.Enabled,
	}
}

// KafkaHTTPRequest is the JSON-serializable form of Kafka create/update fields.
// Supports both comma-separated "brokers" string and "broker_list" array.
type KafkaHTTPRequest struct {
	Name             string   `json:"name"`
	Brokers          string   `json:"brokers"`                // comma-separated
	BrokerList       []string `json:"broker_list,omitempty"`  // alternative to comma-separated
	UseTLS           bool     `json:"use_tls"`
	TLSSkipVerify    bool     `json:"tls_skip_verify"`
	SASLMechanism    string   `json:"sasl_mechanism"`
	Username         string   `json:"username"`
	Password         string   `json:"password"`
	Selector         string   `json:"selector"`
	PublishChanges   bool     `json:"publish_changes"`
	EnableWriteback  bool     `json:"enable_writeback"`
	AutoCreateTopics bool     `json:"auto_create_topics"`
	Enabled          bool     `json:"enabled"`
	RequiredAcks     int      `json:"required_acks"`
	MaxRetries       int      `json:"max_retries"`
	RetryBackoff     string   `json:"retry_backoff"`
	ConsumerGroup    string   `json:"consumer_group"`
	WriteMaxAge      string   `json:"write_max_age"`
}

// ParseBrokers returns the broker list, preferring BrokerList over comma-separated Brokers.
func (r KafkaHTTPRequest) ParseBrokers() []string {
	if len(r.BrokerList) > 0 {
		return r.BrokerList
	}
	if r.Brokers == "" {
		return nil
	}
	var brokers []string
	for _, b := range strings.Split(r.Brokers, ",") {
		b = strings.TrimSpace(b)
		if b != "" {
			brokers = append(brokers, b)
		}
	}
	return brokers
}

func (r KafkaHTTPRequest) parseDurations() (retryBackoff, writeMaxAge time.Duration) {
	if r.RetryBackoff != "" {
		if d, err := time.ParseDuration(r.RetryBackoff); err == nil {
			retryBackoff = d
		}
	}
	if r.WriteMaxAge != "" {
		if d, err := time.ParseDuration(r.WriteMaxAge); err == nil {
			writeMaxAge = d
		}
	}
	return
}

// ToCreateRequest converts to an engine KafkaCreateRequest.
func (r KafkaHTTPRequest) ToCreateRequest() KafkaCreateRequest {
	retryBackoff, writeMaxAge := r.parseDurations()
	return KafkaCreateRequest{
		Name: r.Name, Brokers: r.ParseBrokers(), UseTLS: r.UseTLS,
		TLSSkipVerify: r.TLSSkipVerify, SASLMechanism: r.SASLMechanism,
		Username: r.Username, Password: r.Password, Selector: r.Selector,
		PublishChanges: r.PublishChanges, EnableWriteback: r.EnableWriteback,
		AutoCreateTopics: r.AutoCreateTopics, Enabled: r.Enabled,
		RequiredAcks: r.RequiredAcks, MaxRetries: r.MaxRetries,
		RetryBackoff: retryBackoff, ConsumerGroup: r.ConsumerGroup, WriteMaxAge: writeMaxAge,
	}
}

// ToUpdateRequest converts to an engine KafkaUpdateRequest.
func (r KafkaHTTPRequest) ToUpdateRequest() KafkaUpdateRequest {
	retryBackoff, writeMaxAge := r.parseDurations()
	return KafkaUpdateRequest{
		Brokers: r.ParseBrokers(), UseTLS: r.UseTLS,
		TLSSkipVerify: r.TLSSkipVerify, SASLMechanism: r.SASLMechanism,
		Username: r.Username, Password: r.Password, Selector: r.Selector,
		PublishChanges: r.PublishChanges, EnableWriteback: r.EnableWriteback,
		AutoCreateTopics: r.AutoCreateTopics, Enabled: r.Enabled,
		RequiredAcks: r.RequiredAcks, MaxRetries: r.MaxRetries,
		RetryBackoff: retryBackoff, ConsumerGroup: r.ConsumerGroup, WriteMaxAge: writeMaxAge,
	}
}

// RuleHTTPRequest is the JSON-serializable form of Rule create/update fields.
type RuleHTTPRequest struct {
	Name           string                 `json:"name"`
	Enabled        bool                   `json:"enabled"`
	Conditions     []config.RuleCondition `json:"conditions"`
	LogicMode      config.RuleLogicMode   `json:"logic_mode"`
	DebounceMS     int                    `json:"debounce_ms"`
	CooldownMS     int                    `json:"cooldown_ms"`
	Actions        []config.RuleAction    `json:"actions"`
	ClearedActions []config.RuleAction    `json:"cleared_actions"`
}

// ToCreateRequest converts to an engine RuleCreateRequest.
func (r RuleHTTPRequest) ToCreateRequest() RuleCreateRequest {
	return RuleCreateRequest{
		Name: r.Name, Enabled: r.Enabled, Conditions: r.Conditions,
		LogicMode: r.LogicMode, DebounceMS: r.DebounceMS, CooldownMS: r.CooldownMS,
		Actions: r.Actions, ClearedActions: r.ClearedActions,
	}
}

// ToUpdateRequest converts to an engine RuleUpdateRequest.
func (r RuleHTTPRequest) ToUpdateRequest() RuleUpdateRequest {
	return RuleUpdateRequest{
		Enabled: r.Enabled, Conditions: r.Conditions,
		LogicMode: r.LogicMode, DebounceMS: r.DebounceMS, CooldownMS: r.CooldownMS,
		Actions: r.Actions, ClearedActions: r.ClearedActions,
	}
}

// TagPackHTTPRequest is the JSON-serializable form of TagPack create/update fields.
type TagPackHTTPRequest struct {
	Name          string                 `json:"name"`
	Enabled       bool                   `json:"enabled"`
	MQTTEnabled   bool                   `json:"mqtt_enabled"`
	KafkaEnabled  bool                   `json:"kafka_enabled"`
	ValkeyEnabled bool                   `json:"valkey_enabled"`
	Members       []config.TagPackMember `json:"members,omitempty"`
}

// ToCreateRequest converts to an engine TagPackCreateRequest.
func (r TagPackHTTPRequest) ToCreateRequest() TagPackCreateRequest {
	return TagPackCreateRequest{
		Name: r.Name, Enabled: r.Enabled,
		MQTTEnabled: r.MQTTEnabled, KafkaEnabled: r.KafkaEnabled,
		ValkeyEnabled: r.ValkeyEnabled, Members: r.Members,
	}
}

// ToUpdateRequest converts to an engine TagPackUpdateRequest.
func (r TagPackHTTPRequest) ToUpdateRequest() TagPackUpdateRequest {
	return TagPackUpdateRequest{
		Enabled: r.Enabled,
		MQTTEnabled: r.MQTTEnabled, KafkaEnabled: r.KafkaEnabled,
		ValkeyEnabled: r.ValkeyEnabled, Members: r.Members,
	}
}

// EngineHTTPStatus maps engine sentinel errors to HTTP status codes.
func EngineHTTPStatus(err error) int {
	switch {
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrAlreadyExists):
		return http.StatusConflict
	case errors.Is(err, ErrInvalidInput):
		return http.StatusBadRequest
	case errors.Is(err, ErrSaveFailed):
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}
