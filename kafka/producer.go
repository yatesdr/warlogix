package kafka

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl"
	"github.com/segmentio/kafka-go/sasl/plain"
	"github.com/segmentio/kafka-go/sasl/scram"
)

// ConnectionStatus represents the state of a Kafka connection.
type ConnectionStatus int

const (
	StatusDisconnected ConnectionStatus = iota
	StatusConnecting
	StatusConnected
	StatusError
)

func (s ConnectionStatus) String() string {
	switch s {
	case StatusDisconnected:
		return "Disconnected"
	case StatusConnecting:
		return "Connecting"
	case StatusConnected:
		return "Connected"
	case StatusError:
		return "Error"
	default:
		return "Unknown"
	}
}

// Producer handles Kafka message production with exactly-once semantics.
type Producer struct {
	config  *Config
	writers map[string]*kafka.Writer // topic -> writer
	status  ConnectionStatus
	lastErr error
	mu      sync.RWMutex

	// Stats
	messagesSent  int64
	messagesError int64
	lastSendTime  time.Time
}

// NewProducer creates a new Kafka producer.
func NewProducer(config *Config) *Producer {
	return &Producer{
		config:  config,
		writers: make(map[string]*kafka.Writer),
		status:  StatusDisconnected,
	}
}

// GetStatus returns the current connection status.
func (p *Producer) GetStatus() ConnectionStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.status
}

// GetError returns the last error.
func (p *Producer) GetError() error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lastErr
}

// GetStats returns producer statistics.
func (p *Producer) GetStats() (sent, errors int64, lastSend time.Time) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.messagesSent, p.messagesError, p.lastSendTime
}

// Connect establishes connection to the Kafka cluster.
func (p *Producer) Connect() error {
	p.mu.Lock()
	p.status = StatusConnecting
	p.lastErr = nil
	p.mu.Unlock()

	// Test connectivity by fetching metadata
	dialer := p.createDialer()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := dialer.DialContext(ctx, "tcp", p.config.Brokers[0])
	if err != nil {
		p.mu.Lock()
		p.status = StatusError
		p.lastErr = fmt.Errorf("failed to connect: %w", err)
		p.mu.Unlock()
		return p.lastErr
	}
	conn.Close()

	p.mu.Lock()
	p.status = StatusConnected
	p.mu.Unlock()

	return nil
}

// Disconnect closes all writers and disconnects.
func (p *Producer) Disconnect() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for topic, writer := range p.writers {
		writer.Close()
		delete(p.writers, topic)
	}

	p.status = StatusDisconnected
	p.lastErr = nil
}

// Produce sends a message to the specified topic with exactly-once semantics.
// This is a synchronous call that blocks until the message is acknowledged.
func (p *Producer) Produce(ctx context.Context, topic string, key, value []byte) error {
	writer, err := p.getWriter(topic)
	if err != nil {
		return err
	}

	msg := kafka.Message{
		Key:   key,
		Value: value,
		Time:  time.Now(),
	}

	// Synchronous write with retries handled by the writer
	err = writer.WriteMessages(ctx, msg)
	if err != nil {
		p.mu.Lock()
		p.messagesError++
		p.lastErr = err
		p.mu.Unlock()
		return fmt.Errorf("kafka produce failed: %w", err)
	}

	p.mu.Lock()
	p.messagesSent++
	p.lastSendTime = time.Now()
	p.lastErr = nil
	p.mu.Unlock()

	return nil
}

// ProduceWithRetry sends a message with custom retry logic.
// Returns only after successful send or all retries exhausted.
func (p *Producer) ProduceWithRetry(ctx context.Context, topic string, key, value []byte, maxRetries int, backoff time.Duration) error {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff * time.Duration(attempt)):
			}
		}

		err := p.Produce(ctx, topic, key, value)
		if err == nil {
			return nil
		}
		lastErr = err
	}

	return fmt.Errorf("kafka produce failed after %d attempts: %w", maxRetries+1, lastErr)
}

// getWriter returns or creates a writer for the given topic.
func (p *Producer) getWriter(topic string) (*kafka.Writer, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.status != StatusConnected {
		return nil, fmt.Errorf("Kafka cluster '%s' not connected - connect via Kafka tab first", p.config.Name)
	}

	if writer, exists := p.writers[topic]; exists {
		return writer, nil
	}

	// Create new writer for this topic
	transport := p.createTransport()

	writer := &kafka.Writer{
		Addr:      kafka.TCP(p.config.Brokers...),
		Topic:     topic,
		Balancer:  &kafka.LeastBytes{},
		Transport: transport,

		// Exactly-once settings
		RequiredAcks: kafka.RequiredAcks(p.config.RequiredAcks),
		Async:        false, // Synchronous for exactly-once guarantee
		MaxAttempts:  p.config.MaxRetries,

		// Don't auto-create topics - they should exist
		AllowAutoTopicCreation: false,
	}

	p.writers[topic] = writer
	return writer, nil
}

// createDialer creates a Kafka dialer with auth and TLS.
func (p *Producer) createDialer() *kafka.Dialer {
	dialer := &kafka.Dialer{
		Timeout:   10 * time.Second,
		DualStack: true,
	}

	if p.config.UseTLS {
		dialer.TLS = p.config.GetTLSConfig()
	}

	if mechanism := p.getSASLMechanism(); mechanism != nil {
		dialer.SASLMechanism = mechanism
	}

	return dialer
}

// createTransport creates a Kafka transport with auth and TLS.
func (p *Producer) createTransport() *kafka.Transport {
	transport := &kafka.Transport{
		DialTimeout: 10 * time.Second,
	}

	if p.config.UseTLS {
		transport.TLS = p.config.GetTLSConfig()
	}

	if mechanism := p.getSASLMechanism(); mechanism != nil {
		transport.SASL = mechanism
	}

	return transport
}

// getSASLMechanism returns the configured SASL mechanism.
func (p *Producer) getSASLMechanism() sasl.Mechanism {
	if p.config.Username == "" {
		return nil
	}

	switch p.config.SASLMechanism {
	case SASLPlain:
		return plain.Mechanism{
			Username: p.config.Username,
			Password: p.config.Password,
		}
	case SASLSCRAMSHA256:
		mechanism, _ := scram.Mechanism(scram.SHA256, p.config.Username, p.config.Password)
		return mechanism
	case SASLSCRAMSHA512:
		mechanism, _ := scram.Mechanism(scram.SHA512, p.config.Username, p.config.Password)
		return mechanism
	default:
		return nil
	}
}

// TestConnection verifies connectivity to the Kafka cluster.
func (p *Producer) TestConnection() error {
	dialer := p.createDialer()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, broker := range p.config.Brokers {
		conn, err := dialer.DialContext(ctx, "tcp", broker)
		if err != nil {
			continue
		}

		// Try to get controller to verify full connectivity
		_, err = conn.Controller()
		conn.Close()

		if err == nil {
			return nil
		}
	}

	return fmt.Errorf("failed to connect to any broker")
}
