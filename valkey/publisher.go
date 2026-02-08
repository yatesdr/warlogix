// Package valkey provides Valkey/Redis publishing functionality for PLC tag values.
package valkey

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"warlogix/config"
	"warlogix/logging"
	"warlogix/namespace"
)

// TagMessage represents a tag value message stored in Valkey.
// When a tag has an alias, Tag contains the alias and Offset contains the original address.
type TagMessage struct {
	Factory   string      `json:"factory"`
	PLC       string      `json:"plc"`
	Tag       string      `json:"tag"`
	Offset    string      `json:"offset,omitempty"` // Original tag name/address when alias is used
	Value     interface{} `json:"value"`
	Type      string      `json:"type"`
	Writable  bool        `json:"writable"`
	Timestamp time.Time   `json:"timestamp"`
}

// WriteRequest represents a write request from the write queue.
type WriteRequest struct {
	Factory string      `json:"factory"`
	PLC     string      `json:"plc"`
	Tag     string      `json:"tag"`
	Value   interface{} `json:"value"`
}

// WriteResponse represents a response to a write request.
type WriteResponse struct {
	Factory   string      `json:"factory"`
	PLC       string      `json:"plc"`
	Tag       string      `json:"tag"`
	Value     interface{} `json:"value"`
	Success   bool        `json:"success"`
	Error     string      `json:"error,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
}

// HealthMessage represents a PLC health status message stored in Valkey.
type HealthMessage struct {
	Factory   string    `json:"factory"`
	PLC       string    `json:"plc"`
	Driver    string    `json:"driver"`
	Online    bool      `json:"online"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// Publisher handles publishing tag values to a Valkey server.
type Publisher struct {
	config  *config.ValkeyConfig
	builder *namespace.Builder
	client  *redis.Client
	running bool
	mu      sync.RWMutex

	// Callbacks
	writeHandler      func(plcName, tagName string, value interface{}) error
	writeValidator    func(plcName, tagName string) bool
	tagTypeLookup     func(plcName, tagName string) uint16
	onConnectCallback func()

	// Write-back processing
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// NewPublisher creates a new Valkey publisher.
func NewPublisher(cfg *config.ValkeyConfig, ns string) *Publisher {
	return &Publisher{
		config:   cfg,
		builder:  namespace.New(ns, cfg.Selector),
		stopChan: make(chan struct{}),
	}
}

// Start connects to the Valkey server.
func (p *Publisher) Start() error {
	// Check if already running (quick check with lock)
	p.mu.RLock()
	if p.running {
		p.mu.RUnlock()
		return nil
	}
	p.mu.RUnlock()

	// Create client options
	opts := &redis.Options{
		Addr:         p.config.Address,
		Password:     p.config.Password,
		DB:           p.config.Database,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	}

	if p.config.UseTLS {
		opts.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	// Create client and test connection WITHOUT holding the lock
	client := redis.NewClient(opts)

	debugLog("Attempting to connect to Valkey at %s (DB: %d, TLS: %v)",
		p.config.Address, p.config.Database, p.config.UseTLS)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		debugLog("Valkey connection failed: %v", err)
		client.Close()
		return fmt.Errorf("failed to connect to Valkey at %s: %w", p.config.Address, err)
	}

	debugLog("Successfully connected to Valkey at %s", p.config.Address)

	// Now acquire lock to update state
	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check we're not already running (race condition check)
	if p.running {
		client.Close()
		return nil
	}

	p.client = client
	p.running = true
	p.stopChan = make(chan struct{})

	// Start write-back listener if enabled
	if p.config.EnableWriteback {
		p.wg.Add(1)
		go p.writebackListener()
	}

	// Call on-connect callback to publish initial values
	if p.onConnectCallback != nil {
		go p.onConnectCallback()
	}

	return nil
}

// Stop disconnects from the Valkey server.
func (p *Publisher) Stop() error {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return nil
	}

	p.running = false

	// Signal write-back listener to stop
	close(p.stopChan)

	// Get client reference and clear it
	client := p.client
	p.client = nil
	p.mu.Unlock()

	// Wait for goroutines to finish with timeout
	// (writebackListener uses 1s BLPop timeout, so wait slightly longer)
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		// Timeout - proceed anyway
	}

	// Close the client
	if client != nil {
		return client.Close()
	}

	return nil
}

// IsRunning returns whether the publisher is connected.
func (p *Publisher) IsRunning() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.running
}

// Config returns the publisher's configuration.
func (p *Publisher) Config() *config.ValkeyConfig {
	return p.config
}

// Address returns the server address.
func (p *Publisher) Address() string {
	scheme := "redis"
	if p.config.UseTLS {
		scheme = "rediss"
	}
	return fmt.Sprintf("%s://%s", scheme, p.config.Address)
}

// TagPublishItem represents a single tag to publish (used for batching).
type TagPublishItem struct {
	PLCName  string
	TagName  string
	Alias    string
	Address  string
	TypeName string
	Value    interface{}
	Writable bool
}

// Publish stores a tag value in Valkey.
// For S7 PLCs, alias is the user-friendly name and address is the S7 address in uppercase.
func (p *Publisher) Publish(plcName, tagName, alias, address, typeName string, value interface{}, writable bool) error {
	p.mu.RLock()
	if !p.running || p.client == nil {
		p.mu.RUnlock()
		return nil
	}
	client := p.client
	cfg := p.config
	builder := p.builder
	p.mu.RUnlock()

	// Build key using namespace builder
	key := builder.ValkeyTagKey(plcName, tagName)

	// For S7 with alias, use alias as "tag" and include address
	displayTag := tagName
	if alias != "" {
		displayTag = alias
	}

	// Build message
	msg := TagMessage{
		Factory:   builder.ValkeyFactory(),
		PLC:       plcName,
		Tag:       displayTag,
		Offset:    address, // Original tag name/address when alias is used
		Value:     value,
		Type:      typeName,
		Writable:  writable,
		Timestamp: time.Now().UTC(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal tag value: %w", err)
	}

	// Use a short timeout to prevent blocking
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Set the key with optional TTL
	if cfg.KeyTTL > 0 {
		err = client.Set(ctx, key, data, cfg.KeyTTL).Err()
	} else {
		err = client.Set(ctx, key, data, 0).Err()
	}
	if err != nil {
		return fmt.Errorf("failed to set key: %w", err)
	}

	// Publish to Pub/Sub if enabled
	if cfg.PublishChanges {
		// Publish to PLC-specific channel
		channel := builder.ValkeyChangesChannel(plcName)
		client.Publish(ctx, channel, data)

		// Also publish to the all-changes channel
		allChannel := builder.ValkeyAllChangesChannel()
		client.Publish(ctx, allChannel, data)
	}

	return nil
}

// PublishBatch stores multiple tag values in Valkey using a pipeline for efficiency.
// All SET and PUBLISH commands are combined into a single pipeline to minimize round-trips.
func (p *Publisher) PublishBatch(items []TagPublishItem) error {
	if len(items) == 0 {
		return nil
	}

	p.mu.RLock()
	if !p.running || p.client == nil {
		p.mu.RUnlock()
		return nil
	}
	client := p.client
	cfg := p.config
	builder := p.builder
	p.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use a single pipeline for all commands (SETs + PUBLISHes)
	pipe := client.Pipeline()
	now := time.Now().UTC()

	factory := builder.ValkeyFactory()
	for _, item := range items {
		key := builder.ValkeyTagKey(item.PLCName, item.TagName)

		displayTag := item.TagName
		if item.Alias != "" {
			displayTag = item.Alias
		}

		msg := TagMessage{
			Factory:   factory,
			PLC:       item.PLCName,
			Tag:       displayTag,
			Offset:    item.Address, // Original tag name/address when alias is used
			Value:     item.Value,
			Type:      item.TypeName,
			Writable:  item.Writable,
			Timestamp: now,
		}

		data, err := json.Marshal(msg)
		if err != nil {
			continue
		}

		// Add SET command
		if cfg.KeyTTL > 0 {
			pipe.Set(ctx, key, data, cfg.KeyTTL)
		} else {
			pipe.Set(ctx, key, data, 0)
		}

		// Add PUBLISH commands to same pipeline (fire-and-forget)
		if cfg.PublishChanges {
			channel := builder.ValkeyChangesChannel(item.PLCName)
			pipe.Publish(ctx, channel, data)
			allChannel := builder.ValkeyAllChangesChannel()
			pipe.Publish(ctx, allChannel, data)
		}
	}

	// Execute single pipeline with all commands
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("pipeline exec failed: %w", err)
	}

	debugLog("PublishBatch: sent %d items via pipeline", len(items))
	return nil
}

// PublishHealth publishes PLC health status to Valkey.
func (p *Publisher) PublishHealth(plcName, driver string, online bool, status, errMsg string) error {
	p.mu.RLock()
	if !p.running || p.client == nil {
		p.mu.RUnlock()
		return nil
	}
	client := p.client
	cfg := p.config
	builder := p.builder
	p.mu.RUnlock()

	// Build key using namespace builder
	key := builder.ValkeyHealthKey(plcName)

	msg := HealthMessage{
		Factory:   builder.ValkeyFactory(),
		PLC:       plcName,
		Driver:    driver,
		Online:    online,
		Status:    status,
		Error:     errMsg,
		Timestamp: time.Now().UTC(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal health status: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Set the key with optional TTL
	if cfg.KeyTTL > 0 {
		err = client.Set(ctx, key, data, cfg.KeyTTL).Err()
	} else {
		err = client.Set(ctx, key, data, 0).Err()
	}
	if err != nil {
		return fmt.Errorf("failed to set health key: %w", err)
	}

	// Publish to health-specific Pub/Sub channel
	if cfg.PublishChanges {
		client.Publish(ctx, key, data)
	}

	return nil
}

// SetWriteHandler sets the callback for processing write requests.
func (p *Publisher) SetWriteHandler(handler func(plcName, tagName string, value interface{}) error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.writeHandler = handler
}

// SetWriteValidator sets the callback for validating write requests.
func (p *Publisher) SetWriteValidator(validator func(plcName, tagName string) bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.writeValidator = validator
}

// SetTagTypeLookup sets the callback for looking up tag types.
func (p *Publisher) SetTagTypeLookup(lookup func(plcName, tagName string) uint16) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tagTypeLookup = lookup
}

// SetOnConnectCallback sets the callback invoked after connection is established.
func (p *Publisher) SetOnConnectCallback(callback func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onConnectCallback = callback
}

// writebackListener listens for write requests on the write queue.
func (p *Publisher) writebackListener() {
	defer p.wg.Done()

	queueKey := p.builder.ValkeyWriteQueue()
	responseChannel := p.builder.ValkeyWriteResponseChannel()

	for {
		select {
		case <-p.stopChan:
			return
		default:
		}

		p.mu.RLock()
		if !p.running || p.client == nil {
			p.mu.RUnlock()
			time.Sleep(100 * time.Millisecond)
			continue
		}
		client := p.client
		p.mu.RUnlock()

		// Block waiting for write requests (with timeout for checking stop)
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		result, err := client.BLPop(ctx, 1*time.Second, queueKey).Result()
		cancel()

		if err != nil {
			if err != redis.Nil {
				// Log error but continue
				debugLog("Valkey write queue error: %v", err)
			}
			continue
		}

		if len(result) < 2 {
			continue
		}

		// Parse the write request
		var req WriteRequest
		if err := json.Unmarshal([]byte(result[1]), &req); err != nil {
			debugLog("Failed to parse write request: %v", err)
			continue
		}

		// Process the write request
		p.processWriteRequest(client, req, responseChannel)
	}
}

// processWriteRequest handles a single write request.
func (p *Publisher) processWriteRequest(client *redis.Client, req WriteRequest, responseChannel string) {
	p.mu.RLock()
	handler := p.writeHandler
	validator := p.writeValidator
	builder := p.builder
	p.mu.RUnlock()

	response := WriteResponse{
		Factory:   builder.ValkeyFactory(),
		PLC:       req.PLC,
		Tag:       req.Tag,
		Value:     req.Value,
		Timestamp: time.Now().UTC(),
	}

	// Validate the write is allowed
	if validator != nil && !validator(req.PLC, req.Tag) {
		response.Success = false
		response.Error = "tag is not writable"
	} else if handler == nil {
		response.Success = false
		response.Error = "no write handler configured"
	} else {
		// Execute the write
		if err := handler(req.PLC, req.Tag, req.Value); err != nil {
			response.Success = false
			response.Error = err.Error()
		} else {
			response.Success = true
		}
	}

	// Publish the response
	data, _ := json.Marshal(response)
	ctx := context.Background()
	client.Publish(ctx, responseChannel, data)

	debugLog("Valkey write %s:%s = %v -> success=%v", req.PLC, req.Tag, req.Value, response.Success)
}

// PublishRaw publishes raw bytes to a channel.
// Used for TagPack publishing.
func (p *Publisher) PublishRaw(channel string, data []byte) error {
	p.mu.RLock()
	if !p.running || p.client == nil {
		p.mu.RUnlock()
		return nil
	}
	client := p.client
	p.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	return client.Publish(ctx, channel, data).Err()
}

func debugLog(format string, args ...interface{}) {
	logging.DebugLog("Valkey", format, args...)
}
