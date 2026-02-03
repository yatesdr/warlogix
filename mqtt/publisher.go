// Package mqtt provides MQTT publishing functionality for tag values.
package mqtt

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"

	"warlogix/config"
)

// DebugLogger is an interface for debug logging.
type DebugLogger interface {
	LogMQTT(format string, args ...interface{})
}

var debugLog DebugLogger

// SetDebugLogger sets the debug logger for MQTT.
func SetDebugLogger(logger DebugLogger) {
	debugLog = logger
}

func logMQTT(format string, args ...interface{}) {
	if debugLog != nil {
		debugLog.LogMQTT(format, args...)
	}
}

// writeJob represents a pending write operation.
type writeJob struct {
	client         pahomqtt.Client
	rootTopic      string
	plcName        string
	tagName        string
	value          interface{}
	convertedValue interface{}
	handler        WriteHandler
}

// MaxWriteWorkers is the maximum number of concurrent write goroutines per publisher.
const MaxWriteWorkers = 5

// MaxWriteQueueSize is the maximum number of pending write jobs per publisher.
const MaxWriteQueueSize = 100

// Publisher handles MQTT connection and publishes tag values to a single broker.
type Publisher struct {
	config  *config.MQTTConfig
	client  pahomqtt.Client
	running bool
	mu      sync.RWMutex

	// Track last published values to detect changes
	lastValues map[string]interface{}
	lastMu     sync.RWMutex

	// Write handling
	writeHandler   WriteHandler
	writeValidator WriteValidator
	tagTypeLookup  TagTypeLookup
	plcNames       []string // PLCs to subscribe for writes

	// Worker pool for bounded write goroutines
	writeQueue chan writeJob
	wg         sync.WaitGroup
	stopChan   chan struct{}
}

// TagMessage is the JSON structure published to MQTT.
type TagMessage struct {
	Topic     string      `json:"topic"`
	PLC       string      `json:"plc"`
	Tag       string      `json:"tag"`
	Value     interface{} `json:"value"`
	Type      string      `json:"type,omitempty"`
	Writable  bool        `json:"writable"`
	Timestamp string      `json:"timestamp"`
}

// WriteRequest is the JSON structure for incoming write requests.
type WriteRequest struct {
	Topic string      `json:"topic"`
	PLC   string      `json:"plc"`
	Tag   string      `json:"tag"`
	Value interface{} `json:"value"`
}

// WriteResponse is the JSON structure for write responses.
type WriteResponse struct {
	Topic     string      `json:"topic"`
	PLC       string      `json:"plc"`
	Tag       string      `json:"tag"`
	Value     interface{} `json:"value"`
	Success   bool        `json:"success"`
	Error     string      `json:"error,omitempty"`
	Timestamp string      `json:"timestamp"`
}

// WriteHandler is a callback for handling write requests.
// Returns an error if the write fails.
type WriteHandler func(plcName, tagName string, value interface{}) error

// TagTypeLookup returns the data type code for a tag.
// Returns 0 if the type cannot be determined.
type TagTypeLookup func(plcName, tagName string) uint16

// WriteValidator checks if a tag is writable.
// Returns true if the tag exists and is write-enabled.
type WriteValidator func(plcName, tagName string) bool

// NewPublisher creates a new MQTT publisher for a single broker.
func NewPublisher(cfg *config.MQTTConfig) *Publisher {
	return &Publisher{
		config:     cfg,
		lastValues: make(map[string]interface{}),
		writeQueue: make(chan writeJob, MaxWriteQueueSize),
		stopChan:   make(chan struct{}),
	}
}

// Name returns the publisher's name.
func (p *Publisher) Name() string {
	return p.config.Name
}

// IsRunning returns whether the publisher is connected.
func (p *Publisher) IsRunning() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.running
}

// Start connects to the MQTT broker.
func (p *Publisher) Start() error {
	// Quick check if already running
	p.mu.RLock()
	if p.running {
		p.mu.RUnlock()
		return nil
	}
	p.mu.RUnlock()

	// Build options WITHOUT holding the lock
	opts := pahomqtt.NewClientOptions()

	// Configure broker URL based on TLS setting
	if p.config.UseTLS {
		opts.AddBroker(fmt.Sprintf("ssl://%s:%d", p.config.Broker, p.config.Port))
		tlsConfig := &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
		opts.SetTLSConfig(tlsConfig)
	} else {
		opts.AddBroker(fmt.Sprintf("tcp://%s:%d", p.config.Broker, p.config.Port))
	}

	opts.SetClientID(p.config.ClientID)

	if p.config.Username != "" {
		opts.SetUsername(p.config.Username)
		opts.SetPassword(p.config.Password)
	}

	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(5 * time.Second)
	opts.SetKeepAlive(30 * time.Second)

	// Create client and connect WITHOUT holding the lock
	client := pahomqtt.NewClient(opts)
	logMQTT("Attempting to connect to MQTT broker %s:%d", p.config.Broker, p.config.Port)

	token := client.Connect()
	if !token.WaitTimeout(5 * time.Second) {
		logMQTT("MQTT connection timeout")
		return fmt.Errorf("connection timeout")
	}
	if token.Error() != nil {
		logMQTT("MQTT connection error: %v", token.Error())
		return token.Error()
	}

	logMQTT("Successfully connected to MQTT broker %s:%d", p.config.Broker, p.config.Port)

	// Now acquire lock to update state
	p.mu.Lock()

	// Double-check we're not already running (race condition check)
	if p.running {
		p.mu.Unlock()
		client.Disconnect(100)
		return nil
	}

	p.client = client
	p.running = true
	p.mu.Unlock()

	// Clear last values to force republish of all values
	p.lastMu.Lock()
	p.lastValues = make(map[string]interface{})
	p.lastMu.Unlock()

	// Start write workers
	p.startWriteWorkers()

	// Subscribe to write topics (must be outside p.mu lock to avoid deadlock)
	p.subscribeWriteTopics()

	return nil
}

// startWriteWorkers starts the write worker goroutines.
func (p *Publisher) startWriteWorkers() {
	for i := 0; i < MaxWriteWorkers; i++ {
		p.wg.Add(1)
		go p.writeWorker()
	}
}

// writeWorker processes write jobs from the queue.
func (p *Publisher) writeWorker() {
	defer p.wg.Done()

	for {
		select {
		case <-p.stopChan:
			return
		case job, ok := <-p.writeQueue:
			if !ok {
				return
			}
			var writeErr error

			// Check if this is an error-only response (queued via queueErrorResponse)
			if errVal, isErr := job.convertedValue.(error); isErr && job.handler == nil {
				writeErr = errVal
			} else if job.handler != nil {
				logMQTT("Executing write: %s/%s = %v", job.plcName, job.tagName, job.convertedValue)
				writeErr = job.handler(job.plcName, job.tagName, job.convertedValue)
				if writeErr != nil {
					logMQTT("Write error: %v", writeErr)
				} else {
					logMQTT("Write successful")
				}
			} else {
				writeErr = fmt.Errorf("no write handler configured")
			}
			p.publishWriteResponse(job.client, job.rootTopic, job.plcName, job.tagName, job.value, writeErr)
		}
	}
}

// Stop disconnects from the MQTT broker.
func (p *Publisher) Stop() {
	p.mu.Lock()
	if !p.running || p.client == nil {
		p.mu.Unlock()
		return
	}

	p.running = false
	client := p.client
	p.client = nil

	// Save old channels and create new ones while holding lock
	oldStopChan := p.stopChan
	p.stopChan = make(chan struct{})
	p.writeQueue = make(chan writeJob, MaxWriteQueueSize)
	p.mu.Unlock()

	// Stop write workers by closing old channel
	close(oldStopChan)

	// Wait for workers to finish (with timeout)
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		logMQTT("Timeout waiting for write workers to stop")
	}

	// Disconnect OUTSIDE the lock to prevent blocking
	if client != nil {
		client.Disconnect(500)
	}
}

// BuildTopic constructs the full topic path.
func (p *Publisher) BuildTopic(plcName, tagName string) string {
	return fmt.Sprintf("%s/%s/tags/%s", p.config.RootTopic, plcName, tagName)
}

// Publish sends a tag value to MQTT if it has changed.
func (p *Publisher) Publish(plcName, tagName, typeName string, value interface{}, writable, force bool) bool {
	p.mu.RLock()
	running := p.running
	client := p.client
	p.mu.RUnlock()

	if !running || client == nil {
		return false
	}

	cacheKey := fmt.Sprintf("%s/%s", plcName, tagName)

	p.lastMu.RLock()
	lastValue, exists := p.lastValues[cacheKey]
	p.lastMu.RUnlock()

	if exists && !force && fmt.Sprintf("%v", lastValue) == fmt.Sprintf("%v", value) {
		return false
	}

	msg := TagMessage{
		Topic:     p.config.RootTopic,
		PLC:       plcName,
		Tag:       tagName,
		Value:     value,
		Type:      typeName,
		Writable:  writable,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		return false
	}

	topic := p.BuildTopic(plcName, tagName)
	token := client.Publish(topic, 1, true, payload)

	// Use timeout to prevent blocking
	if !token.WaitTimeout(2 * time.Second) {
		return false
	}
	if token.Error() != nil {
		return false
	}

	p.lastMu.Lock()
	p.lastValues[cacheKey] = value
	p.lastMu.Unlock()

	return true
}

// Address returns the broker address string.
func (p *Publisher) Address() string {
	if p.config.UseTLS {
		return fmt.Sprintf("ssl://%s:%d", p.config.Broker, p.config.Port)
	}
	return fmt.Sprintf("tcp://%s:%d", p.config.Broker, p.config.Port)
}

// Config returns the publisher's configuration.
func (p *Publisher) Config() *config.MQTTConfig {
	return p.config
}

// SetWriteHandler sets the callback for handling write requests.
func (p *Publisher) SetWriteHandler(handler WriteHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.writeHandler = handler
}

// SetWriteValidator sets the callback for validating write requests.
func (p *Publisher) SetWriteValidator(validator WriteValidator) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.writeValidator = validator
}

// SetTagTypeLookup sets the callback for looking up tag types.
func (p *Publisher) SetTagTypeLookup(lookup TagTypeLookup) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tagTypeLookup = lookup
}

// SetPLCNames sets the PLC names to subscribe for write requests.
func (p *Publisher) SetPLCNames(names []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.plcNames = names
}

// subscribeWriteTopics subscribes to write topics for all configured PLCs.
func (p *Publisher) subscribeWriteTopics() {
	p.mu.RLock()
	client := p.client
	plcNames := p.plcNames
	rootTopic := p.config.RootTopic
	p.mu.RUnlock()

	if client == nil {
		logMQTT("subscribeWriteTopics: client is nil")
		return
	}
	if len(plcNames) == 0 {
		logMQTT("subscribeWriteTopics: no PLC names configured")
		return
	}

	for _, plcName := range plcNames {
		topic := fmt.Sprintf("%s/%s/write", rootTopic, plcName)
		logMQTT("Subscribing to write topic: %s", topic)
		token := client.Subscribe(topic, 1, p.handleWriteMessage)
		if !token.WaitTimeout(2*time.Second) || token.Error() != nil {
			if token.Error() != nil {
				logMQTT("Subscribe error for %s: %v", topic, token.Error())
			} else {
				logMQTT("Subscribe timeout for %s", topic)
			}
			continue
		}
		logMQTT("Subscribed to: %s", topic)
	}
}

// convertJSONValue converts JSON values to more appropriate Go types.
// JSON numbers are always float64, but we want to use int32 for whole numbers
// since DINT is the most common integer type in PLCs.
// PLC type codes (from logix package, duplicated to avoid import cycle)
const (
	plcTypeBOOL  uint16 = 0x00C1
	plcTypeSINT  uint16 = 0x00C2
	plcTypeINT   uint16 = 0x00C3
	plcTypeDINT  uint16 = 0x00C4
	plcTypeLINT  uint16 = 0x00C5
	plcTypeUSINT uint16 = 0x00C6
	plcTypeUINT  uint16 = 0x00C7
	plcTypeUDINT uint16 = 0x00C8
	plcTypeULINT uint16 = 0x00C9
	plcTypeREAL  uint16 = 0x00CA
	plcTypeLREAL uint16 = 0x00CB
)

// convertValueForType converts a JSON value to the appropriate Go type for the PLC tag.
// Returns the converted value and an error if the conversion is not possible.
func convertValueForType(value interface{}, dataType uint16) (interface{}, error) {
	// Mask off array/structure flags
	baseType := dataType & 0x0FFF

	// Get the numeric value from JSON (always float64 for numbers)
	var numVal float64
	var isNumber bool
	var boolVal bool
	var isBool bool
	var strVal string
	var isString bool

	switch v := value.(type) {
	case float64:
		numVal = v
		isNumber = true
	case bool:
		boolVal = v
		isBool = true
	case string:
		strVal = v
		isString = true
	default:
		return nil, fmt.Errorf("unsupported value type: %T", value)
	}

	switch baseType {
	case plcTypeBOOL:
		if isBool {
			return boolVal, nil
		}
		if isNumber {
			return numVal != 0, nil
		}
		return nil, fmt.Errorf("cannot convert %T to BOOL", value)

	case plcTypeSINT: // int8
		if !isNumber {
			return nil, fmt.Errorf("cannot convert %T to SINT", value)
		}
		if numVal < -128 || numVal > 127 || numVal != float64(int8(numVal)) {
			return nil, fmt.Errorf("value %v out of range for SINT (-128 to 127)", numVal)
		}
		return int8(numVal), nil

	case plcTypeINT: // int16
		if !isNumber {
			return nil, fmt.Errorf("cannot convert %T to INT", value)
		}
		if numVal < -32768 || numVal > 32767 || numVal != float64(int16(numVal)) {
			return nil, fmt.Errorf("value %v out of range for INT (-32768 to 32767)", numVal)
		}
		return int16(numVal), nil

	case plcTypeDINT: // int32
		if !isNumber {
			return nil, fmt.Errorf("cannot convert %T to DINT", value)
		}
		if numVal < -2147483648 || numVal > 2147483647 || numVal != float64(int32(numVal)) {
			return nil, fmt.Errorf("value %v out of range for DINT", numVal)
		}
		return int32(numVal), nil

	case plcTypeLINT: // int64
		if !isNumber {
			return nil, fmt.Errorf("cannot convert %T to LINT", value)
		}
		if numVal != float64(int64(numVal)) {
			return nil, fmt.Errorf("value %v cannot be represented as LINT", numVal)
		}
		return int64(numVal), nil

	case plcTypeUSINT: // uint8
		if !isNumber {
			return nil, fmt.Errorf("cannot convert %T to USINT", value)
		}
		if numVal < 0 || numVal > 255 || numVal != float64(uint8(numVal)) {
			return nil, fmt.Errorf("value %v out of range for USINT (0 to 255)", numVal)
		}
		return uint8(numVal), nil

	case plcTypeUINT: // uint16
		if !isNumber {
			return nil, fmt.Errorf("cannot convert %T to UINT", value)
		}
		if numVal < 0 || numVal > 65535 || numVal != float64(uint16(numVal)) {
			return nil, fmt.Errorf("value %v out of range for UINT (0 to 65535)", numVal)
		}
		return uint16(numVal), nil

	case plcTypeUDINT: // uint32
		if !isNumber {
			return nil, fmt.Errorf("cannot convert %T to UDINT", value)
		}
		if numVal < 0 || numVal > 4294967295 || numVal != float64(uint32(numVal)) {
			return nil, fmt.Errorf("value %v out of range for UDINT", numVal)
		}
		return uint32(numVal), nil

	case plcTypeULINT: // uint64
		if !isNumber {
			return nil, fmt.Errorf("cannot convert %T to ULINT", value)
		}
		if numVal < 0 || numVal != float64(uint64(numVal)) {
			return nil, fmt.Errorf("value %v out of range for ULINT", numVal)
		}
		return uint64(numVal), nil

	case plcTypeREAL: // float32
		if !isNumber {
			return nil, fmt.Errorf("cannot convert %T to REAL", value)
		}
		return float32(numVal), nil

	case plcTypeLREAL: // float64
		if !isNumber {
			return nil, fmt.Errorf("cannot convert %T to LREAL", value)
		}
		return numVal, nil

	default:
		// For strings or unknown types, try to use as-is
		if isString {
			return strVal, nil
		}
		// Fall back to original behavior for unknown types
		if isNumber && numVal == float64(int32(numVal)) {
			return int32(numVal), nil
		}
		return value, nil
	}
}

// getTypeName returns a human-readable name for a type code.
func getTypeName(dataType uint16) string {
	baseType := dataType & 0x0FFF
	switch baseType {
	case plcTypeBOOL:
		return "BOOL"
	case plcTypeSINT:
		return "SINT"
	case plcTypeINT:
		return "INT"
	case plcTypeDINT:
		return "DINT"
	case plcTypeLINT:
		return "LINT"
	case plcTypeUSINT:
		return "USINT"
	case plcTypeUINT:
		return "UINT"
	case plcTypeUDINT:
		return "UDINT"
	case plcTypeULINT:
		return "ULINT"
	case plcTypeREAL:
		return "REAL"
	case plcTypeLREAL:
		return "LREAL"
	default:
		return fmt.Sprintf("UNKNOWN(0x%04X)", dataType)
	}
}

// handleWriteMessage processes incoming write requests.
func (p *Publisher) handleWriteMessage(client pahomqtt.Client, msg pahomqtt.Message) {
	logMQTT("Received write request on topic: %s", msg.Topic())
	logMQTT("Payload: %s", string(msg.Payload()))

	p.mu.RLock()
	handler := p.writeHandler
	validator := p.writeValidator
	typeLookup := p.tagTypeLookup
	rootTopic := p.config.RootTopic
	p.mu.RUnlock()

	// Parse the write request
	var req WriteRequest
	if err := json.Unmarshal(msg.Payload(), &req); err != nil {
		logMQTT("JSON parse error: %v", err)
		p.queueErrorResponse(client, rootTopic, "", "", nil, fmt.Errorf("invalid JSON: %v", err))
		return
	}

	// Validate topic matches
	if req.Topic != rootTopic {
		p.queueErrorResponse(client, rootTopic, req.PLC, req.Tag, req.Value,
			fmt.Errorf("topic mismatch: expected %s, got %s", rootTopic, req.Topic))
		return
	}

	// Check if tag is writable
	if validator != nil && !validator(req.PLC, req.Tag) {
		p.queueErrorResponse(client, rootTopic, req.PLC, req.Tag, req.Value,
			fmt.Errorf("tag not writable: %s/%s", req.PLC, req.Tag))
		return
	}

	// Look up tag type and convert value
	var convertedValue interface{} = req.Value
	if typeLookup != nil {
		dataType := typeLookup(req.PLC, req.Tag)
		if dataType != 0 {
			logMQTT("Tag type: %s (0x%04X)", getTypeName(dataType), dataType)
			var err error
			convertedValue, err = convertValueForType(req.Value, dataType)
			if err != nil {
				logMQTT("Value conversion error: %v", err)
				p.queueErrorResponse(client, rootTopic, req.PLC, req.Tag, req.Value, err)
				return
			}
			logMQTT("Converted value: %v (type: %T)", convertedValue, convertedValue)
		} else {
			logMQTT("Could not determine tag type, using value as-is: %v (%T)", req.Value, req.Value)
		}
	}

	// Queue the write job (non-blocking with drop on overflow)
	job := writeJob{
		client:         client,
		rootTopic:      rootTopic,
		plcName:        req.PLC,
		tagName:        req.Tag,
		value:          req.Value,
		convertedValue: convertedValue,
		handler:        handler,
	}
	select {
	case p.writeQueue <- job:
		// Job queued successfully
	default:
		// Queue full, respond with error
		logMQTT("Write queue full, rejecting write for %s/%s", req.PLC, req.Tag)
		go p.publishWriteResponse(client, rootTopic, req.PLC, req.Tag, req.Value,
			fmt.Errorf("write queue full, try again later"))
	}
}

// queueErrorResponse queues an error response through the worker pool.
func (p *Publisher) queueErrorResponse(client pahomqtt.Client, rootTopic, plcName, tagName string, value interface{}, err error) {
	// For error responses, we use a nil handler which will trigger the error path
	job := writeJob{
		client:    client,
		rootTopic: rootTopic,
		plcName:   plcName,
		tagName:   tagName,
		value:     value,
		handler:   nil, // nil handler means we just send the error response
	}
	// Store the error message in convertedValue as a signal
	job.convertedValue = err

	select {
	case p.writeQueue <- job:
		// Job queued
	default:
		// Queue full, log and drop
		logMQTT("Write queue full, dropping error response for %s/%s", plcName, tagName)
	}
}

// publishWriteResponse publishes a write response to MQTT.
func (p *Publisher) publishWriteResponse(client pahomqtt.Client, rootTopic, plcName, tagName string, value interface{}, err error) {
	resp := WriteResponse{
		Topic:     rootTopic,
		PLC:       plcName,
		Tag:       tagName,
		Value:     value,
		Success:   err == nil,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	if err != nil {
		resp.Error = err.Error()
	}

	payload, _ := json.Marshal(resp)

	// Publish to response topic
	responseTopic := fmt.Sprintf("%s/%s/write/response", rootTopic, plcName)
	if plcName == "" {
		responseTopic = fmt.Sprintf("%s/write/response", rootTopic)
	}
	token := client.Publish(responseTopic, 1, false, payload)
	token.WaitTimeout(2 * time.Second)
}

// Manager manages multiple MQTT publishers.
type Manager struct {
	publishers     map[string]*Publisher
	mu             sync.RWMutex
	writeHandler   WriteHandler
	writeValidator WriteValidator
	tagTypeLookup  TagTypeLookup
	plcNames       []string
}

// NewManager creates a new MQTT manager.
func NewManager() *Manager {
	return &Manager{
		publishers: make(map[string]*Publisher),
	}
}

// Add adds a publisher to the manager.
func (m *Manager) Add(pub *Publisher) {
	m.mu.Lock()
	m.publishers[pub.Name()] = pub
	handler := m.writeHandler
	validator := m.writeValidator
	typeLookup := m.tagTypeLookup
	plcNames := m.plcNames
	m.mu.Unlock()

	// Apply current settings to new publisher
	if handler != nil {
		pub.SetWriteHandler(handler)
	}
	if validator != nil {
		pub.SetWriteValidator(validator)
	}
	if typeLookup != nil {
		pub.SetTagTypeLookup(typeLookup)
	}
	if len(plcNames) > 0 {
		pub.SetPLCNames(plcNames)
	}
}

// Remove removes a publisher by name.
func (m *Manager) Remove(name string) {
	m.mu.Lock()
	pub, exists := m.publishers[name]
	if exists {
		delete(m.publishers, name)
	}
	m.mu.Unlock()

	if exists {
		pub.Stop()
	}
}

// Get returns a publisher by name.
func (m *Manager) Get(name string) *Publisher {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.publishers[name]
}

// List returns all publishers.
func (m *Manager) List() []*Publisher {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Publisher, 0, len(m.publishers))
	for _, pub := range m.publishers {
		result = append(result, pub)
	}
	return result
}

// StartAll starts all publishers that are configured as enabled.
// Returns the number of publishers successfully started.
func (m *Manager) StartAll() int {
	m.mu.RLock()
	pubs := make([]*Publisher, 0, len(m.publishers))
	for _, pub := range m.publishers {
		pubs = append(pubs, pub)
	}
	m.mu.RUnlock()

	started := 0
	for _, pub := range pubs {
		if pub.config.Enabled && !pub.IsRunning() {
			logMQTT("Auto-starting MQTT publisher: %s", pub.Name())
			if err := pub.Start(); err != nil {
				logMQTT("Failed to auto-start %s: %v", pub.Name(), err)
			} else {
				logMQTT("Successfully started %s (%s)", pub.Name(), pub.Address())
				started++
			}
		}
	}
	return started
}

// StopAll stops all publishers.
func (m *Manager) StopAll() {
	m.mu.RLock()
	pubs := make([]*Publisher, 0, len(m.publishers))
	for _, pub := range m.publishers {
		pubs = append(pubs, pub)
	}
	m.mu.RUnlock()

	for _, pub := range pubs {
		pub.Stop()
	}
}

// Publish publishes a value to all running publishers.
func (m *Manager) Publish(plcName, tagName, typeName string, value interface{}, force bool) {
	m.mu.RLock()
	pubs := make([]*Publisher, 0, len(m.publishers))
	for _, pub := range m.publishers {
		pubs = append(pubs, pub)
	}
	validator := m.writeValidator
	m.mu.RUnlock()

	if len(pubs) == 0 {
		logMQTT("Manager.Publish: no publishers configured")
		return
	}

	// Check if tag is writable using the validator
	writable := false
	if validator != nil {
		writable = validator(plcName, tagName)
	}

	runningCount := 0
	for _, pub := range pubs {
		if pub.IsRunning() {
			runningCount++
			pub.Publish(plcName, tagName, typeName, value, writable, force)
		}
	}
	if runningCount == 0 {
		logMQTT("Manager.Publish: no publishers running")
	}
}

// AnyRunning returns true if any publisher is running.
func (m *Manager) AnyRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, pub := range m.publishers {
		if pub.IsRunning() {
			return true
		}
	}
	return false
}

// LoadFromConfig creates publishers from configuration.
func (m *Manager) LoadFromConfig(cfgs []config.MQTTConfig) {
	for i := range cfgs {
		pub := NewPublisher(&cfgs[i])
		m.Add(pub)
	}
}

// SetWriteHandler sets the write handler for all publishers.
func (m *Manager) SetWriteHandler(handler WriteHandler) {
	m.mu.Lock()
	m.writeHandler = handler
	pubs := make([]*Publisher, 0, len(m.publishers))
	for _, pub := range m.publishers {
		pubs = append(pubs, pub)
	}
	m.mu.Unlock()

	for _, pub := range pubs {
		pub.SetWriteHandler(handler)
	}
}

// SetWriteValidator sets the write validator for all publishers.
func (m *Manager) SetWriteValidator(validator WriteValidator) {
	m.mu.Lock()
	m.writeValidator = validator
	pubs := make([]*Publisher, 0, len(m.publishers))
	for _, pub := range m.publishers {
		pubs = append(pubs, pub)
	}
	m.mu.Unlock()

	for _, pub := range pubs {
		pub.SetWriteValidator(validator)
	}
}

// SetTagTypeLookup sets the tag type lookup for all publishers.
func (m *Manager) SetTagTypeLookup(lookup TagTypeLookup) {
	m.mu.Lock()
	m.tagTypeLookup = lookup
	pubs := make([]*Publisher, 0, len(m.publishers))
	for _, pub := range m.publishers {
		pubs = append(pubs, pub)
	}
	m.mu.Unlock()

	for _, pub := range pubs {
		pub.SetTagTypeLookup(lookup)
	}
}

// SetPLCNames sets the PLC names for write subscriptions on all publishers.
func (m *Manager) SetPLCNames(names []string) {
	m.mu.Lock()
	m.plcNames = names
	pubs := make([]*Publisher, 0, len(m.publishers))
	for _, pub := range m.publishers {
		pubs = append(pubs, pub)
	}
	m.mu.Unlock()

	for _, pub := range pubs {
		pub.SetPLCNames(names)
	}
}

// UpdateWriteSubscriptions updates write subscriptions for all running publishers.
// Call this when PLCs are added/removed.
func (m *Manager) UpdateWriteSubscriptions() {
	m.mu.RLock()
	pubs := make([]*Publisher, 0, len(m.publishers))
	for _, pub := range m.publishers {
		pubs = append(pubs, pub)
	}
	plcNames := m.plcNames
	m.mu.RUnlock()

	for _, pub := range pubs {
		pub.SetPLCNames(plcNames)
		if pub.IsRunning() {
			pub.subscribeWriteTopics()
		}
	}
}
