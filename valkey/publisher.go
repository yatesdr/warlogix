// Package valkey provides Valkey/Redis publishing functionality for PLC tag values.
package valkey

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"warlogix/config"
)

// joinKey joins key segments with colons, trimming leading/trailing colons
// from each segment to avoid empty key parts (e.g., "foo::bar" or ":foo:bar:").
func joinKey(segments ...string) string {
	var parts []string
	for _, s := range segments {
		s = strings.Trim(s, ":")
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ":")
}

// TagMessage represents a tag value message stored in Valkey.
type TagMessage struct {
	Factory   string      `json:"factory"`
	PLC       string      `json:"plc"`
	Tag       string      `json:"tag"`
	Address   string      `json:"address,omitempty"` // S7 address in uppercase (empty for non-S7)
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
func NewPublisher(cfg *config.ValkeyConfig) *Publisher {
	return &Publisher{
		config:   cfg,
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
	p.mu.RUnlock()

	// Build key: factory:plc:tags:tag (standard Redis convention)
	// Note: tag names may contain : but that's OK since tag is always the last segment
	key := joinKey(cfg.Factory, plcName, "tags", tagName)

	// For S7 with alias, use alias as "tag" and include address
	displayTag := tagName
	if alias != "" {
		displayTag = alias
	}

	// Build message
	msg := TagMessage{
		Factory:   cfg.Factory,
		PLC:       plcName,
		Tag:       displayTag,
		Address:   address, // Empty for non-S7, uppercase address for S7
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
		channel := joinKey(cfg.Factory, plcName, "changes")
		client.Publish(ctx, channel, data)

		// Also publish to the all-changes channel
		allChannel := joinKey(cfg.Factory, "_all", "changes")
		client.Publish(ctx, allChannel, data)
	}

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
	p.mu.RUnlock()

	// Build key: factory:plc:health
	key := joinKey(cfg.Factory, plcName, "health")

	msg := HealthMessage{
		Factory:   cfg.Factory,
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
		channel := joinKey(cfg.Factory, plcName, "health")
		client.Publish(ctx, channel, data)
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

	queueKey := joinKey(p.config.Factory, "writes")
	responseChannel := joinKey(p.config.Factory, "write", "responses")

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
	p.mu.RUnlock()

	response := WriteResponse{
		Factory:   req.Factory,
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

// Debug logging
var debugLogger DebugLogger

// DebugLogger interface for debug logging.
type DebugLogger interface {
	LogValkey(format string, args ...interface{})
}

// SetDebugLogger sets the debug logger.
func SetDebugLogger(logger DebugLogger) {
	debugLogger = logger
}

func debugLog(format string, args ...interface{}) {
	if debugLogger != nil {
		debugLogger.LogValkey(format, args...)
	}
}
