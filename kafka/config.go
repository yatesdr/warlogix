// Package kafka provides Kafka producer functionality for event-driven data capture.
package kafka

import (
	"crypto/tls"
	"time"
)

// SASLMechanism represents the SASL authentication mechanism.
type SASLMechanism string

const (
	SASLNone        SASLMechanism = ""
	SASLPlain       SASLMechanism = "PLAIN"
	SASLSCRAMSHA256 SASLMechanism = "SCRAM-SHA-256"
	SASLSCRAMSHA512 SASLMechanism = "SCRAM-SHA-512"
)

// Config holds runtime configuration for a Kafka cluster connection.
// Note: This struct uses non-pointer types for simplicity at runtime.
// The config package has a separate KafkaConfig struct with pointer types for YAML
// serialization (to distinguish "not set" from "explicitly false").
// Conversion from config.KafkaConfig to kafka.Config happens in main.go.
type Config struct {
	Name          string        `yaml:"name"`
	Enabled       bool          `yaml:"enabled"`
	Brokers       []string      `yaml:"brokers"`
	UseTLS        bool          `yaml:"use_tls,omitempty"`
	TLSSkipVerify bool          `yaml:"tls_skip_verify,omitempty"`
	SASLMechanism SASLMechanism `yaml:"sasl_mechanism,omitempty"`
	Username      string        `yaml:"username,omitempty"`
	Password      string        `yaml:"password,omitempty"`

	// Producer settings
	RequiredAcks int           `yaml:"required_acks,omitempty"` // -1=all, 0=none, 1=leader only
	MaxRetries   int           `yaml:"max_retries,omitempty"`
	RetryBackoff time.Duration `yaml:"retry_backoff,omitempty"`

	// Tag publishing settings
	PublishChanges   bool   `yaml:"publish_changes,omitempty"`    // Publish tag changes to Kafka
	Selector         string `yaml:"selector,omitempty"`           // Optional sub-namespace
	AutoCreateTopics bool   `yaml:"auto_create_topics,omitempty"` // Auto-create topics if they don't exist (default true)

	// Writeback settings
	EnableWriteback bool          `yaml:"enable_writeback,omitempty"` // Enable consuming write requests
	ConsumerGroup   string        `yaml:"consumer_group,omitempty"`   // Consumer group ID
	WriteMaxAge     time.Duration `yaml:"write_max_age,omitempty"`    // Max age of write requests to process
}

// DefaultConfig returns a Kafka configuration with sensible defaults.
func DefaultConfig(name string) Config {
	return Config{
		Name:             name,
		Enabled:          false,
		Brokers:          []string{"localhost:9092"},
		RequiredAcks:     -1, // All replicas must acknowledge
		MaxRetries:       3,
		RetryBackoff:     100 * time.Millisecond,
		AutoCreateTopics: true,
		EnableWriteback:  false,
		WriteMaxAge:      2 * time.Second,
	}
}

// GetTLSConfig returns a TLS configuration if TLS is enabled.
func (c *Config) GetTLSConfig() *tls.Config {
	if !c.UseTLS {
		return nil
	}
	return &tls.Config{
		InsecureSkipVerify: c.TLSSkipVerify,
	}
}

// GetConsumerGroup returns the consumer group ID.
// Defaults to warlink-{Name}-writers if not explicitly set.
func (c *Config) GetConsumerGroup() string {
	if c.ConsumerGroup != "" {
		return c.ConsumerGroup
	}
	return "warlink-" + c.Name + "-writers"
}

// GetWriteMaxAge returns the maximum age of write requests to process.
// Defaults to 2 seconds if not set.
func (c *Config) GetWriteMaxAge() time.Duration {
	if c.WriteMaxAge > 0 {
		return c.WriteMaxAge
	}
	return 2 * time.Second
}
