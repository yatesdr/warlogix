package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// TagMessage is the JSON structure published to Kafka for tag changes.
type TagMessage struct {
	PLC       string      `json:"plc"`
	Tag       string      `json:"tag"`
	Address   string      `json:"address,omitempty"` // S7 address in uppercase (empty for non-S7)
	Value     interface{} `json:"value"`
	Type      string      `json:"type,omitempty"`
	Writable  bool        `json:"writable"`
	Timestamp string      `json:"timestamp"`
}

// HealthMessage is the JSON structure published to Kafka for PLC health status.
type HealthMessage struct {
	PLC       string `json:"plc"`
	Online    bool   `json:"online"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	Timestamp string `json:"timestamp"`
}

// publishJob represents a pending Kafka publish operation.
type publishJob struct {
	producer *Producer
	topic    string
	key      []byte
	payload  []byte
	cacheKey string
	value    interface{}
}

// Manager manages multiple Kafka producer connections.
type Manager struct {
	producers  map[string]*Producer
	mu         sync.RWMutex
	lastValues map[string]interface{} // Track last published values per cluster/plc/tag
	lastMu     sync.RWMutex

	// Worker pool for bounded publish goroutines
	publishQueue chan publishJob
	wg           sync.WaitGroup
	stopChan     chan struct{}
	started      bool
}

// MaxPublishWorkers is the maximum number of concurrent publish goroutines.
const MaxPublishWorkers = 10

// MaxPublishQueueSize is the maximum number of pending publish jobs.
const MaxPublishQueueSize = 1000

// NewManager creates a new Kafka manager.
func NewManager() *Manager {
	m := &Manager{
		producers:    make(map[string]*Producer),
		lastValues:   make(map[string]interface{}),
		publishQueue: make(chan publishJob, MaxPublishQueueSize),
		stopChan:     make(chan struct{}),
	}
	m.startWorkers()
	return m
}

// startWorkers starts the publish worker goroutines.
func (m *Manager) startWorkers() {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	m.mu.Unlock()

	for i := 0; i < MaxPublishWorkers; i++ {
		m.wg.Add(1)
		go m.publishWorker()
	}
}

// publishWorker processes publish jobs from the queue.
func (m *Manager) publishWorker() {
	defer m.wg.Done()

	for {
		select {
		case <-m.stopChan:
			return
		case job, ok := <-m.publishQueue:
			if !ok {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := job.producer.Produce(ctx, job.topic, job.key, job.payload); err == nil {
				m.lastMu.Lock()
				m.lastValues[job.cacheKey] = job.value
				m.lastMu.Unlock()
			} else {
				logKafka("Failed to publish %s: %v", job.cacheKey, err)
			}
			cancel()
		}
	}
}

// AddCluster adds a new Kafka cluster configuration.
func (m *Manager) AddCluster(config *Config) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.producers[config.Name]; exists {
		return
	}

	m.producers[config.Name] = NewProducer(config)
}

// RemoveCluster removes a Kafka cluster and disconnects.
func (m *Manager) RemoveCluster(name string) {
	m.mu.Lock()
	producer, exists := m.producers[name]
	if exists {
		delete(m.producers, name)
	}
	m.mu.Unlock()

	if exists && producer != nil {
		producer.Disconnect()
	}
}

// GetProducer returns the producer for the named cluster.
func (m *Manager) GetProducer(name string) *Producer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.producers[name]
}

// ListClusters returns all cluster names.
func (m *Manager) ListClusters() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.producers))
	for name := range m.producers {
		names = append(names, name)
	}
	return names
}

// Connect connects to the named Kafka cluster.
func (m *Manager) Connect(name string) error {
	m.mu.RLock()
	producer, exists := m.producers[name]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("kafka cluster not found: %s", name)
	}

	return producer.Connect()
}

// Disconnect disconnects from the named Kafka cluster.
func (m *Manager) Disconnect(name string) {
	m.mu.RLock()
	producer, exists := m.producers[name]
	m.mu.RUnlock()

	if exists && producer != nil {
		producer.Disconnect()
	}
}

// ConnectEnabled connects to all enabled Kafka clusters.
func (m *Manager) ConnectEnabled() {
	m.mu.RLock()
	producers := make([]*Producer, 0)
	for _, p := range m.producers {
		if p.config.Enabled {
			producers = append(producers, p)
		}
	}
	m.mu.RUnlock()

	for _, p := range producers {
		go p.Connect()
	}
}

// StopAll disconnects from all Kafka clusters and stops workers.
func (m *Manager) StopAll() {
	// Stop the worker goroutines first
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		// Still disconnect producers even if workers weren't started
		m.mu.RLock()
		producers := make([]*Producer, 0, len(m.producers))
		for _, p := range m.producers {
			producers = append(producers, p)
		}
		m.mu.RUnlock()
		for _, p := range producers {
			p.Disconnect()
		}
		return
	}

	// Save old channels and create new ones while holding lock
	oldStopChan := m.stopChan
	m.stopChan = make(chan struct{})
	m.publishQueue = make(chan publishJob, MaxPublishQueueSize)
	m.started = false
	m.mu.Unlock()

	// Stop workers by closing old channel
	close(oldStopChan)

	// Wait for workers to finish (with timeout)
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		logKafka("Timeout waiting for publish workers to stop")
	}

	// Disconnect all producers
	m.mu.RLock()
	producers := make([]*Producer, 0, len(m.producers))
	for _, p := range m.producers {
		producers = append(producers, p)
	}
	m.mu.RUnlock()

	for _, p := range producers {
		p.Disconnect()
	}
}

// Produce sends a message to a topic on the named cluster.
func (m *Manager) Produce(ctx context.Context, clusterName, topic string, key, value []byte) error {
	m.mu.RLock()
	producer, exists := m.producers[clusterName]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("kafka cluster not found: %s", clusterName)
	}

	return producer.Produce(ctx, topic, key, value)
}

// ProduceWithRetry sends a message with retries.
func (m *Manager) ProduceWithRetry(ctx context.Context, clusterName, topic string, key, value []byte) error {
	m.mu.RLock()
	producer, exists := m.producers[clusterName]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("kafka cluster not found: %s", clusterName)
	}

	config := producer.config
	return producer.ProduceWithRetry(ctx, topic, key, value, config.MaxRetries, config.RetryBackoff)
}

// GetClusterStatus returns the status of a specific cluster.
func (m *Manager) GetClusterStatus(name string) (ConnectionStatus, error) {
	m.mu.RLock()
	producer, exists := m.producers[name]
	m.mu.RUnlock()

	if !exists {
		return StatusDisconnected, fmt.Errorf("cluster not found")
	}

	return producer.GetStatus(), producer.GetError()
}

// LoadFromConfigs loads multiple cluster configurations.
func (m *Manager) LoadFromConfigs(configs []Config) {
	for i := range configs {
		m.AddCluster(&configs[i])
	}
}

// DebugLogger is an interface for debug logging.
type DebugLogger interface {
	LogKafka(format string, args ...interface{})
}

var debugLog DebugLogger

// SetDebugLogger sets the debug logger for Kafka.
func SetDebugLogger(logger DebugLogger) {
	debugLog = logger
}

func logKafka(format string, args ...interface{}) {
	if debugLog != nil {
		debugLog.LogKafka(format, args...)
	}
}

// Publish sends a tag value to all connected Kafka clusters that have PublishChanges enabled.
// Only publishes if the value has changed (unless force is true).
// For S7 PLCs, alias is the user-defined name and address is the S7 address in uppercase.
func (m *Manager) Publish(plcName, tagName, alias, address, typeName string, value interface{}, writable, force bool) {
	// Ensure workers are running
	m.startWorkers()

	m.mu.RLock()
	producers := make([]*Producer, 0, len(m.producers))
	for _, p := range m.producers {
		producers = append(producers, p)
	}
	m.mu.RUnlock()

	for _, p := range producers {
		// Skip if not connected, not enabled for publishing, or no topic configured
		status := p.GetStatus()
		if status != StatusConnected {
			continue
		}
		if !p.config.PublishChanges {
			continue
		}
		if p.config.Topic == "" {
			continue
		}

		// Check if value changed
		cacheKey := fmt.Sprintf("%s/%s/%s", p.config.Name, plcName, tagName)

		m.lastMu.RLock()
		lastValue, exists := m.lastValues[cacheKey]
		m.lastMu.RUnlock()

		if exists && !force && fmt.Sprintf("%v", lastValue) == fmt.Sprintf("%v", value) {
			continue // No change
		}

		// Build message - use alias as tag name if provided
		displayTag := tagName
		if alias != "" {
			displayTag = alias
		}

		msg := TagMessage{
			PLC:       plcName,
			Tag:       displayTag,
			Address:   address, // S7 address in uppercase, empty for non-S7
			Value:     value,
			Type:      typeName,
			Writable:  writable,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}

		payload, err := json.Marshal(msg)
		if err != nil {
			continue
		}

		// Use tag name as key for ordering
		key := []byte(fmt.Sprintf("%s.%s", plcName, tagName))

		// Queue the publish job (non-blocking with drop on overflow)
		job := publishJob{
			producer: p,
			topic:    p.config.Topic,
			key:      key,
			payload:  payload,
			cacheKey: cacheKey,
			value:    value,
		}
		select {
		case m.publishQueue <- job:
			// Job queued successfully
		default:
			// Queue full, drop the message and log
			logKafka("Publish queue full, dropping message for %s", cacheKey)
		}
	}
}

// PublishHealth publishes PLC health status to all connected Kafka clusters.
func (m *Manager) PublishHealth(plcName string, online bool, status, errMsg string) {
	m.startWorkers()

	m.mu.RLock()
	producers := make([]*Producer, 0, len(m.producers))
	for _, p := range m.producers {
		producers = append(producers, p)
	}
	m.mu.RUnlock()

	for _, p := range producers {
		// Skip if not connected or no topic configured
		if p.GetStatus() != StatusConnected {
			continue
		}
		if !p.config.PublishChanges || p.config.Topic == "" {
			continue
		}

		msg := HealthMessage{
			PLC:       plcName,
			Online:    online,
			Status:    status,
			Error:     errMsg,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}

		payload, err := json.Marshal(msg)
		if err != nil {
			continue
		}

		// Use health topic: base topic with .health suffix
		healthTopic := p.config.Topic + ".health"
		key := []byte(plcName)

		job := publishJob{
			producer: p,
			topic:    healthTopic,
			key:      key,
			payload:  payload,
			cacheKey: fmt.Sprintf("%s/%s/health", p.config.Name, plcName),
			value:    nil, // Health messages are always published
		}
		select {
		case m.publishQueue <- job:
			// Job queued successfully
		default:
			logKafka("Publish queue full, dropping health message for %s", plcName)
		}
	}
}

// AnyPublishing returns true if any cluster has PublishChanges enabled and is connected.
func (m *Manager) AnyPublishing() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, p := range m.producers {
		status := p.GetStatus()
		if status == StatusConnected && p.config.PublishChanges && p.config.Topic != "" {
			return true
		}
	}
	return false
}

// ClearLastValues clears the change tracking cache, forcing republish of all values.
func (m *Manager) ClearLastValues() {
	m.lastMu.Lock()
	m.lastValues = make(map[string]interface{})
	m.lastMu.Unlock()
}
