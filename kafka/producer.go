package kafka

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl"
	"github.com/segmentio/kafka-go/sasl/plain"
	"github.com/segmentio/kafka-go/sasl/scram"

	"warlogix/logging"
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
	name := p.config.Name
	brokers := p.config.Brokers
	p.mu.Unlock()

	logging.DebugLog("Kafka", "CONNECT %s: connecting to brokers %v", name, brokers)

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
		logging.DebugLog("Kafka", "CONNECT %s: FAILED - %v", name, err)
		return p.lastErr
	}
	conn.Close()

	p.mu.Lock()
	p.status = StatusConnected
	p.mu.Unlock()

	logging.DebugLog("Kafka", "CONNECT %s: connected successfully", name)
	return nil
}

// Disconnect closes all writers and disconnects.
func (p *Producer) Disconnect() {
	p.mu.Lock()
	defer p.mu.Unlock()

	name := p.config.Name
	logging.DebugLog("Kafka", "DISCONNECT %s: closing %d topic writers", name, len(p.writers))

	for topic, writer := range p.writers {
		writer.Close()
		delete(p.writers, topic)
	}

	p.status = StatusDisconnected
	p.lastErr = nil
	logging.DebugLog("Kafka", "DISCONNECT %s: disconnected", name)
}

// Produce sends a message to the specified topic with exactly-once semantics.
// This is a synchronous call that blocks until the message is acknowledged.
func (p *Producer) Produce(ctx context.Context, topic string, key, value []byte) error {
	produceStart := time.Now()

	writerStart := time.Now()
	writer, err := p.getWriter(topic)
	writerDuration := time.Since(writerStart)
	if writerDuration > 100*time.Millisecond {
		logging.DebugLog("Kafka", "PRODUCE %s: getWriter('%s') took %v", p.config.Name, topic, writerDuration)
	}
	if err != nil {
		return err
	}

	msg := kafka.Message{
		Key:   key,
		Value: value,
		Time:  time.Now(),
	}

	// Synchronous write with retries handled by the writer
	writeStart := time.Now()
	err = writer.WriteMessages(ctx, msg)
	writeDuration := time.Since(writeStart)
	if writeDuration > 100*time.Millisecond {
		logging.DebugLog("Kafka", "PRODUCE %s: WriteMessages('%s') took %v", p.config.Name, topic, writeDuration)
	}

	if err != nil {
		p.mu.Lock()
		p.messagesError++
		p.lastErr = err
		p.mu.Unlock()
		// Log specific error for topic not found
		if strings.Contains(err.Error(), "Unknown Topic") {
			logging.DebugLog("Kafka", "TOPIC %s: topic '%s' not found on broker", p.config.Name, topic)
		}
		logging.DebugLog("Kafka", "PRODUCE %s: FAILED topic '%s' after %v: %v", p.config.Name, topic, time.Since(produceStart), err)
		return fmt.Errorf("kafka produce failed: %w", err)
	}

	totalDuration := time.Since(produceStart)
	if totalDuration > 100*time.Millisecond {
		logging.DebugLog("Kafka", "PRODUCE %s: topic '%s' completed in %v (writer: %v, write: %v)",
			p.config.Name, topic, totalDuration, writerDuration, writeDuration)
	}

	p.mu.Lock()
	p.messagesSent++
	p.lastSendTime = time.Now()
	p.lastErr = nil
	p.mu.Unlock()

	return nil
}

// ProduceBatch sends multiple messages to the specified topic in a single call.
// This is more efficient than calling Produce multiple times.
func (p *Producer) ProduceBatch(ctx context.Context, topic string, messages []kafka.Message) error {
	if len(messages) == 0 {
		return nil
	}

	produceStart := time.Now()

	writerStart := time.Now()
	writer, err := p.getWriter(topic)
	writerDuration := time.Since(writerStart)
	if writerDuration > 100*time.Millisecond {
		logging.DebugLog("Kafka", "PRODUCE_BATCH %s: getWriter('%s') took %v", p.config.Name, topic, writerDuration)
	}
	if err != nil {
		return err
	}

	// Write all messages in a single call
	writeStart := time.Now()
	err = writer.WriteMessages(ctx, messages...)
	writeDuration := time.Since(writeStart)

	if err != nil {
		p.mu.Lock()
		p.messagesError += int64(len(messages))
		p.lastErr = err
		p.mu.Unlock()
		logging.DebugLog("Kafka", "PRODUCE_BATCH %s: FAILED topic '%s' (%d msgs) after %v: %v",
			p.config.Name, topic, len(messages), time.Since(produceStart), err)
		return fmt.Errorf("kafka batch produce failed: %w", err)
	}

	totalDuration := time.Since(produceStart)
	if totalDuration > 50*time.Millisecond || len(messages) >= 10 {
		logging.DebugLog("Kafka", "PRODUCE_BATCH %s: topic '%s' sent %d msgs in %v (write: %v, %.1f msg/s)",
			p.config.Name, topic, len(messages), totalDuration, writeDuration, float64(len(messages))/totalDuration.Seconds())
	}

	p.mu.Lock()
	p.messagesSent += int64(len(messages))
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

	getWriterStart := time.Now()

	// NOTE: We rely on AllowAutoTopicCreation on the Writer instead of
	// calling ensureTopicExists(), which creates 2 TCP connections per topic.
	// The broker will auto-create topics on first produce if configured.

	// Create new writer for this topic with batching enabled
	transport := p.createTransport()

	writer := &kafka.Writer{
		Addr:      kafka.TCP(p.config.Brokers...),
		Topic:     topic,
		Balancer:  &kafka.LeastBytes{},
		Transport: transport,

		// Delivery guarantees
		RequiredAcks: kafka.RequiredAcks(p.config.RequiredAcks),
		Async:        false, // Synchronous for delivery guarantee
		MaxAttempts:  p.config.MaxRetries,

		// Batching settings for performance
		BatchSize:    100,                    // Batch up to 100 messages
		BatchBytes:   1048576,                // Or 1MB, whichever comes first
		BatchTimeout: 10 * time.Millisecond,  // Flush after 10ms if batch not full

		// Auto-create topics on first produce
		AllowAutoTopicCreation: p.config.AutoCreateTopics,
	}

	p.writers[topic] = writer
	totalDuration := time.Since(getWriterStart)
	logging.DebugLog("Kafka", "TOPIC %s: created writer for topic '%s' in %v (auto-create=%v, batch=100/10ms)",
		p.config.Name, topic, totalDuration, p.config.AutoCreateTopics)
	return writer, nil
}

// ensureTopicExists creates the topic if it doesn't exist.
// Must be called with p.mu held.
func (p *Producer) ensureTopicExists(topic string) error {
	totalStart := time.Now()
	dialer := p.createDialer()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dialStart := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", p.config.Brokers[0])
	if err != nil {
		return fmt.Errorf("failed to connect after %v: %w", time.Since(dialStart), err)
	}
	defer conn.Close()
	logging.DebugLog("Kafka", "TOPIC %s: dial to broker took %v", p.config.Name, time.Since(dialStart))

	// Get the controller to create topics
	controllerStart := time.Now()
	controller, err := conn.Controller()
	if err != nil {
		return fmt.Errorf("failed to get controller after %v: %w", time.Since(controllerStart), err)
	}
	logging.DebugLog("Kafka", "TOPIC %s: get controller took %v", p.config.Name, time.Since(controllerStart))

	controllerDialStart := time.Now()
	controllerConn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", controller.Host, controller.Port))
	if err != nil {
		return fmt.Errorf("failed to connect to controller after %v: %w", time.Since(controllerDialStart), err)
	}
	defer controllerConn.Close()
	logging.DebugLog("Kafka", "TOPIC %s: dial to controller took %v", p.config.Name, time.Since(controllerDialStart))

	// Create the topic with default settings
	createStart := time.Now()
	err = controllerConn.CreateTopics(kafka.TopicConfig{
		Topic:             topic,
		NumPartitions:     1,
		ReplicationFactor: 1,
	})

	if err != nil {
		// Ignore "topic already exists" error
		if strings.Contains(err.Error(), "Topic with this name already exists") ||
			strings.Contains(err.Error(), "already exists") {
			logging.DebugLog("Kafka", "TOPIC %s: topic '%s' already exists (checked in %v, total %v)",
				p.config.Name, topic, time.Since(createStart), time.Since(totalStart))
			return nil
		}
		return fmt.Errorf("failed to create topic after %v: %w", time.Since(createStart), err)
	}

	logging.DebugLog("Kafka", "TOPIC %s: created topic '%s' in %v (total %v)",
		p.config.Name, topic, time.Since(createStart), time.Since(totalStart))
	return nil
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
