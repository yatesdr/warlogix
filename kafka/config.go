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

// Config holds configuration for a Kafka cluster connection.
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
	PublishChanges bool   `yaml:"publish_changes,omitempty"` // Publish tag changes to Kafka
	Topic          string `yaml:"topic,omitempty"`           // Topic for tag change publishing
}

// DefaultConfig returns a Kafka configuration with sensible defaults.
func DefaultConfig(name string) Config {
	return Config{
		Name:         name,
		Enabled:      false,
		Brokers:      []string{"localhost:9092"},
		RequiredAcks: -1, // All replicas must acknowledge
		MaxRetries:   3,
		RetryBackoff: 100 * time.Millisecond,
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
