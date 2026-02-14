package engine

import (
	"time"

	"warlink/config"
)

// PLCCreateRequest holds fields for creating a new PLC.
type PLCCreateRequest struct {
	Name               string
	Address            string
	Slot               byte
	Family             config.PLCFamily
	Enabled            bool
	HealthCheckEnabled *bool
	DiscoverTags       *bool
	PollRate           time.Duration
	Timeout            time.Duration
	AmsNetId           string
	AmsPort            uint16
	Protocol           string
	FinsPort           int
	FinsNetwork        byte
	FinsNode           byte
	FinsUnit           byte
}

// PLCUpdateRequest holds fields for updating a PLC.
type PLCUpdateRequest struct {
	Address            string
	Slot               byte
	Family             config.PLCFamily
	Enabled            bool
	HealthCheckEnabled *bool
	DiscoverTags       *bool
	PollRate           time.Duration
	Timeout            time.Duration
	AmsNetId           string
	AmsPort            uint16
	Protocol           string
	FinsPort           int
	FinsNetwork        byte
	FinsNode           byte
	FinsUnit           byte
}

// TagUpdateRequest holds fields for updating a tag's configuration.
type TagUpdateRequest struct {
	Enabled      *bool
	Writable     *bool
	NoREST       *bool
	NoMQTT       *bool
	NoKafka      *bool
	NoValkey     *bool
	AddIgnore    []string
	RemoveIgnore []string
}

// TagCreateOrUpdateRequest holds fields for creating or fully replacing a tag.
type TagCreateOrUpdateRequest struct {
	Enabled  bool
	Writable bool
	DataType string
	Alias    string
}

// MQTTCreateRequest holds fields for creating an MQTT broker.
type MQTTCreateRequest struct {
	Name     string
	Broker   string
	Port     int
	ClientID string
	Username string
	Password string
	Selector string
	UseTLS   bool
	Enabled  bool
}

// MQTTUpdateRequest holds fields for updating an MQTT broker.
type MQTTUpdateRequest struct {
	Broker   string
	Port     int
	ClientID string
	Username string
	Password string
	Selector string
	UseTLS   bool
	Enabled  bool
}

// ValkeyCreateRequest holds fields for creating a Valkey server.
type ValkeyCreateRequest struct {
	Name            string
	Address         string
	Password        string
	Database        int
	Selector        string
	KeyTTL          time.Duration
	UseTLS          bool
	PublishChanges  bool
	EnableWriteback bool
	Enabled         bool
}

// ValkeyUpdateRequest holds fields for updating a Valkey server.
type ValkeyUpdateRequest struct {
	Address         string
	Password        string
	Database        int
	Selector        string
	KeyTTL          time.Duration
	UseTLS          bool
	PublishChanges  bool
	EnableWriteback bool
	Enabled         bool
}

// KafkaCreateRequest holds fields for creating a Kafka cluster.
type KafkaCreateRequest struct {
	Name             string
	Brokers          []string
	UseTLS           bool
	TLSSkipVerify    bool
	SASLMechanism    string
	Username         string
	Password         string
	Selector         string
	PublishChanges   bool
	EnableWriteback  bool
	AutoCreateTopics bool
	Enabled          bool
	RequiredAcks     int
	MaxRetries       int
	RetryBackoff     time.Duration
	ConsumerGroup    string
	WriteMaxAge      time.Duration
}

// KafkaUpdateRequest holds fields for updating a Kafka cluster.
type KafkaUpdateRequest struct {
	Brokers          []string
	UseTLS           bool
	TLSSkipVerify    bool
	SASLMechanism    string
	Username         string
	Password         string
	Selector         string
	PublishChanges   bool
	EnableWriteback  bool
	AutoCreateTopics bool
	Enabled          bool
	RequiredAcks     int
	MaxRetries       int
	RetryBackoff     time.Duration
	ConsumerGroup    string
	WriteMaxAge      time.Duration
}

// RuleCreateRequest holds fields for creating a rule.
type RuleCreateRequest struct {
	Name           string
	Enabled        bool
	Conditions     []config.RuleCondition
	LogicMode      config.RuleLogicMode
	DebounceMS     int
	CooldownMS     int
	Actions        []config.RuleAction
	ClearedActions []config.RuleAction
}

// RuleUpdateRequest holds fields for updating a rule.
type RuleUpdateRequest struct {
	Enabled        bool
	Conditions     []config.RuleCondition
	LogicMode      config.RuleLogicMode
	DebounceMS     int
	CooldownMS     int
	Actions        []config.RuleAction
	ClearedActions []config.RuleAction
}

// TagPackCreateRequest holds fields for creating a TagPack.
type TagPackCreateRequest struct {
	Name          string
	Enabled       bool
	MQTTEnabled   bool
	KafkaEnabled  bool
	ValkeyEnabled bool
	Members       []config.TagPackMember
}

// TagPackUpdateRequest holds fields for updating a TagPack.
type TagPackUpdateRequest struct {
	Enabled       bool
	MQTTEnabled   bool
	KafkaEnabled  bool
	ValkeyEnabled bool
	Members       []config.TagPackMember
}
