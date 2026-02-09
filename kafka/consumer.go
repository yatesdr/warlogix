package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl"
	"github.com/segmentio/kafka-go/sasl/plain"
	"github.com/segmentio/kafka-go/sasl/scram"
	"warlink/logging"
	"warlink/namespace"
)

// Consumer batch configuration
const (
	// WriteBackBatchInterval is how often to collect and process write batches.
	WriteBackBatchInterval = 250 * time.Millisecond
)

// WriteRequest is the JSON structure for incoming write requests.
type WriteRequest struct {
	PLC       string      `json:"plc"`
	Tag       string      `json:"tag"`
	Value     interface{} `json:"value"`
	RequestID string      `json:"request_id,omitempty"` // Optional correlation ID
	Timestamp time.Time   `json:"timestamp,omitempty"`  // When the request was created
}

// WriteResponse is the JSON structure for write responses.
type WriteResponse struct {
	PLC          string      `json:"plc"`
	Tag          string      `json:"tag"`
	Value        interface{} `json:"value"`
	RequestID    string      `json:"request_id,omitempty"`
	Success      bool        `json:"success"`
	Error        string      `json:"error,omitempty"`
	Skipped      bool        `json:"skipped,omitempty"`      // True if request was too old (expired)
	Deduplicated bool        `json:"deduplicated,omitempty"` // True if request was replaced by a newer one
	Timestamp    time.Time   `json:"timestamp"`
}

// WriteHandler is a callback for handling write requests.
type WriteHandler func(plcName, tagName string, value interface{}) error

// WriteValidator checks if a tag is writable.
type WriteValidator func(plcName, tagName string) bool

// TagTypeLookup returns the data type code for a tag.
type TagTypeLookup func(plcName, tagName string) uint16

// pendingWrite represents a write request waiting to be processed.
type pendingWrite struct {
	request     WriteRequest
	messageTime time.Time // Kafka message timestamp
	offset      int64
}

// Consumer handles consuming write requests from Kafka.
type Consumer struct {
	config   *Config
	producer *Producer // For producing responses
	builder  *namespace.Builder
	reader   *kafka.Reader
	running  bool
	mu       sync.RWMutex

	// Callbacks
	writeHandler   WriteHandler
	writeValidator WriteValidator
	tagTypeLookup  TagTypeLookup

	// Lifecycle
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// NewConsumer creates a new Kafka consumer for write requests.
func NewConsumer(config *Config, producer *Producer, builder *namespace.Builder) *Consumer {
	return &Consumer{
		config:   config,
		producer: producer,
		builder:  builder,
		stopChan: make(chan struct{}),
	}
}

// SetWriteHandler sets the callback for processing write requests.
func (c *Consumer) SetWriteHandler(handler WriteHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writeHandler = handler
}

// SetWriteValidator sets the callback for validating write requests.
func (c *Consumer) SetWriteValidator(validator WriteValidator) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writeValidator = validator
}

// SetTagTypeLookup sets the callback for looking up tag types.
func (c *Consumer) SetTagTypeLookup(lookup TagTypeLookup) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tagTypeLookup = lookup
}

// Start begins consuming write requests from Kafka.
func (c *Consumer) Start() error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil
	}

	writeTopic := c.builder.KafkaWriteTopic()
	consumerGroup := c.config.GetConsumerGroup()

	logConsumer("Starting consumer for topic '%s' with group '%s'", writeTopic, consumerGroup)

	// Create the reader with consumer group
	// Using a single partition ensures sequential processing
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        c.config.Brokers,
		Topic:          writeTopic,
		GroupID:        consumerGroup,
		MinBytes:       1,                      // Fetch immediately when data available
		MaxBytes:       1e6,                    // 1MB max
		MaxWait:        100 * time.Millisecond, // Short wait for responsiveness
		StartOffset:    kafka.LastOffset,       // Start from latest on first join
		CommitInterval: time.Second,            // Commit offsets every second
		Dialer:         c.createDialer(),
	})

	c.reader = reader
	c.running = true
	c.stopChan = make(chan struct{})
	c.mu.Unlock()

	// Start the consumer goroutine
	c.wg.Add(1)
	go c.consumeLoop()

	logConsumer("Consumer started successfully")
	return nil
}

// Stop stops the consumer.
func (c *Consumer) Stop() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}

	logConsumer("Stopping consumer")
	c.running = false
	close(c.stopChan)
	reader := c.reader
	c.reader = nil
	c.mu.Unlock()

	// Wait for consumer to finish with timeout
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logConsumer("Consumer stopped gracefully")
	case <-time.After(3 * time.Second):
		logConsumer("Consumer stop timeout")
	}

	// Close the reader
	if reader != nil {
		reader.Close()
	}
}

// IsRunning returns whether the consumer is running.
func (c *Consumer) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.running
}

// consumeLoop is the main consumer loop that batches and processes write requests.
func (c *Consumer) consumeLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(WriteBackBatchInterval)
	defer ticker.Stop()

	// Pending writes keyed by "plc.tag" - latest value wins
	pending := make(map[string]pendingWrite)
	// Track deduplicated (discarded) requests so we can send responses
	var discarded []pendingWrite

	for {
		select {
		case <-c.stopChan:
			// Process any remaining pending writes before exiting
			if len(pending) > 0 || len(discarded) > 0 {
				logConsumer("Stop signal received, processing %d pending writes before exit (discarded %d duplicates)", len(pending), len(discarded))
				c.processBatch(pending, discarded)
			} else {
				logConsumer("Stop signal received, no pending writes")
			}
			return

		case <-ticker.C:
			// Process accumulated batch
			if len(pending) > 0 || len(discarded) > 0 {
				logConsumer("Batch interval reached with %d pending writes (discarded %d duplicates)", len(pending), len(discarded))
				c.processBatch(pending, discarded)
				pending = make(map[string]pendingWrite)
				discarded = nil
			}

		default:
			// Try to read a message with short timeout
			c.mu.RLock()
			reader := c.reader
			running := c.running
			c.mu.RUnlock()

			if !running || reader == nil {
				time.Sleep(10 * time.Millisecond)
				continue
			}

			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			msg, err := reader.FetchMessage(ctx)
			cancel()

			if err != nil {
				// Timeout or other error - continue to check ticker/stop
				continue
			}

			logConsumer("Received write request: partition=%d offset=%d key=%s", msg.Partition, msg.Offset, string(msg.Key))
			logConsumer("Payload: %s", string(msg.Value))

			// Parse the write request
			var req WriteRequest
			if err := json.Unmarshal(msg.Value, &req); err != nil {
				logConsumer("JSON parse error: %v", err)
				// Commit the bad message to skip it
				c.commitMessage(reader, msg)
				continue
			}

			// Use message key as dedup key, or construct from plc.tag
			key := string(msg.Key)
			if key == "" {
				key = req.PLC + "." + req.Tag
			}

			// Check if this overwrites an existing pending write
			if existing, exists := pending[key]; exists {
				logConsumer("DEDUP DISCARD: %s/%s value=%v (offset=%d, age=%v) replaced by value=%v (offset=%d)",
					existing.request.PLC, existing.request.Tag, existing.request.Value,
					existing.offset, time.Since(existing.messageTime).Round(time.Millisecond),
					req.Value, msg.Offset)
				// Store the discarded request so we can send a response
				discarded = append(discarded, existing)
			}

			// Store/overwrite in pending map (latest wins)
			pending[key] = pendingWrite{
				request:     req,
				messageTime: msg.Time,
				offset:      msg.Offset,
			}

			// Commit this message
			c.commitMessage(reader, msg)
		}
	}
}

// processBatch processes a batch of deduplicated write requests.
// discarded contains requests that were replaced by newer requests for the same tag.
func (c *Consumer) processBatch(pending map[string]pendingWrite, discarded []pendingWrite) {
	c.mu.RLock()
	handler := c.writeHandler
	validator := c.writeValidator
	producer := c.producer
	maxAge := c.config.GetWriteMaxAge()
	responseTopic := c.builder.KafkaWriteResponseTopic()
	c.mu.RUnlock()

	now := time.Now()
	totalReceived := len(pending) + len(discarded)
	logConsumer("Processing batch: %d received, %d deduplicated, %d to execute", totalReceived, len(discarded), len(pending))

	// Send responses for deduplicated (discarded) requests first
	for _, pw := range discarded {
		req := pw.request
		logConsumer("Sending deduplicated response for %s/%s value=%v (replaced by newer request)",
			req.PLC, req.Tag, req.Value)
		c.sendResponse(producer, responseTopic, WriteResponse{
			PLC:          req.PLC,
			Tag:          req.Tag,
			Value:        req.Value,
			RequestID:    req.RequestID,
			Success:      false,
			Error:        "request superseded by newer write to same tag",
			Deduplicated: true,
			Timestamp:    now,
		})
	}

	// Process pending writes (the ones that weren't deduplicated)
	processed := 0
	skipped := 0
	failed := 0

	for key, pw := range pending {
		req := pw.request

		// Check if request is too old
		age := now.Sub(pw.messageTime)
		if age > maxAge {
			logConsumer("Skipping stale write request for %s (age: %v > max: %v)", key, age, maxAge)
			skipped++

			// Send skipped response
			c.sendResponse(producer, responseTopic, WriteResponse{
				PLC:       req.PLC,
				Tag:       req.Tag,
				Value:     req.Value,
				RequestID: req.RequestID,
				Success:   false,
				Error:     fmt.Sprintf("request expired (age: %v, max: %v)", age.Round(time.Millisecond), maxAge),
				Skipped:   true,
				Timestamp: now,
			})
			continue
		}

		logConsumer("Processing write: %s/%s = %v (age: %v, request_id: %s)",
			req.PLC, req.Tag, req.Value, age.Round(time.Millisecond), req.RequestID)

		// Validate the tag is writable
		if validator != nil && !validator(req.PLC, req.Tag) {
			logConsumer("Write validation failed for %s/%s: tag not writable", req.PLC, req.Tag)
			failed++
			c.sendResponse(producer, responseTopic, WriteResponse{
				PLC:       req.PLC,
				Tag:       req.Tag,
				Value:     req.Value,
				RequestID: req.RequestID,
				Success:   false,
				Error:     "tag is not writable",
				Timestamp: now,
			})
			continue
		}

		// Execute the write
		logConsumer("Executing write: %s/%s = %v (type: %T)", req.PLC, req.Tag, req.Value, req.Value)
		var writeErr error
		if handler != nil {
			writeErr = handler(req.PLC, req.Tag, req.Value)
		} else {
			writeErr = fmt.Errorf("no write handler configured")
		}

		// Send response
		resp := WriteResponse{
			PLC:       req.PLC,
			Tag:       req.Tag,
			Value:     req.Value,
			RequestID: req.RequestID,
			Success:   writeErr == nil,
			Timestamp: now,
		}
		if writeErr != nil {
			resp.Error = writeErr.Error()
			logConsumer("Write error: %s/%s: %v", req.PLC, req.Tag, writeErr)
			failed++
		} else {
			logConsumer("Write successful: %s/%s = %v", req.PLC, req.Tag, req.Value)
			processed++
		}

		c.sendResponse(producer, responseTopic, resp)
	}

	logConsumer("Batch complete: %d succeeded, %d failed, %d expired, %d deduplicated",
		processed, failed, skipped, len(discarded))
}

// sendResponse publishes a write response to the response topic.
func (c *Consumer) sendResponse(producer *Producer, topic string, resp WriteResponse) {
	if producer == nil || producer.GetStatus() != StatusConnected {
		logConsumer("Cannot send response: producer not connected")
		return
	}

	payload, err := json.Marshal(resp)
	if err != nil {
		logConsumer("Failed to marshal response: %v", err)
		return
	}

	key := []byte(resp.PLC + "." + resp.Tag)

	logConsumer("Publishing response to %s: key=%s success=%v", topic, string(key), resp.Success)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := producer.Produce(ctx, topic, key, payload); err != nil {
		logConsumer("Failed to publish response to %s: %v", topic, err)
	}
}

// commitMessage commits a message offset.
func (c *Consumer) commitMessage(reader *kafka.Reader, msg kafka.Message) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := reader.CommitMessages(ctx, msg); err != nil {
		logConsumer("Failed to commit message: %v", err)
	}
}

// createDialer creates a Kafka dialer with auth and TLS.
func (c *Consumer) createDialer() *kafka.Dialer {
	dialer := &kafka.Dialer{
		Timeout:   10 * time.Second,
		DualStack: true,
	}

	if c.config.UseTLS {
		dialer.TLS = c.config.GetTLSConfig()
	}

	if mechanism := c.getSASLMechanism(); mechanism != nil {
		dialer.SASLMechanism = mechanism
	}

	return dialer
}

// getSASLMechanism returns the configured SASL mechanism.
func (c *Consumer) getSASLMechanism() sasl.Mechanism {
	if c.config.Username == "" {
		return nil
	}

	switch c.config.SASLMechanism {
	case SASLPlain:
		return plain.Mechanism{
			Username: c.config.Username,
			Password: c.config.Password,
		}
	case SASLSCRAMSHA256:
		mechanism, _ := scram.Mechanism(scram.SHA256, c.config.Username, c.config.Password)
		return mechanism
	case SASLSCRAMSHA512:
		mechanism, _ := scram.Mechanism(scram.SHA512, c.config.Username, c.config.Password)
		return mechanism
	default:
		return nil
	}
}

func logConsumer(format string, args ...interface{}) {
	logging.DebugLog("Kafka", "[Consumer] "+format, args...)
}
