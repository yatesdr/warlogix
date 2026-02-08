package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"
	"warlogix/logging"
	"warlogix/namespace"
)

// TagMessage is the JSON structure published to Kafka for tag changes.
// When a tag has an alias, Tag contains the alias and Offset contains the original address.
type TagMessage struct {
	PLC       string      `json:"plc"`
	Tag       string      `json:"tag"`
	Offset    string      `json:"offset,omitempty"` // Original tag name/address when alias is used
	Value     interface{} `json:"value"`
	Type      string      `json:"type,omitempty"`
	Writable  bool        `json:"writable"`
	Timestamp string      `json:"timestamp"`
}

// HealthMessage is the JSON structure published to Kafka for PLC health status.
type HealthMessage struct {
	PLC       string `json:"plc"`
	Driver    string `json:"driver"`
	Online    bool   `json:"online"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	Timestamp string `json:"timestamp"`
}

// publishJob represents a pending Kafka publish operation.
type publishJob struct {
	producer  *Producer
	topic     string
	key       []byte
	payload   []byte
	cacheKey  string
	value     interface{}
	queueTime time.Time // When the job was queued
}

// topicBatch collects messages for a single topic.
type topicBatch struct {
	producer *Producer
	topic    string
	messages []kafka.Message
	cacheUpdates map[string]interface{} // cacheKey -> value
}

// Manager manages multiple Kafka producer connections.
type Manager struct {
	producers  map[string]*Producer
	consumers  map[string]*Consumer
	builders   map[string]*namespace.Builder // builders per cluster
	mu         sync.RWMutex
	lastValues map[string]interface{} // Track last published values per cluster/plc/tag
	lastMu     sync.RWMutex

	// Batched publishing
	batchChan chan publishJob // Incoming jobs for batching
	wg        sync.WaitGroup
	stopChan  chan struct{}
	started   bool

	// Write handling callbacks (shared across all consumers)
	writeHandler   WriteHandler
	writeValidator WriteValidator
	tagTypeLookup  TagTypeLookup
}

// Batching configuration
const (
	// MaxBatchSize is the maximum number of messages per batch per topic.
	MaxBatchSize = 100
	// BatchFlushInterval is how often to flush partial batches.
	BatchFlushInterval = 20 * time.Millisecond
	// MaxBatchQueueSize is the maximum pending jobs before dropping.
	MaxBatchQueueSize = 5000
)

// NewManager creates a new Kafka manager.
func NewManager() *Manager {
	m := &Manager{
		producers:  make(map[string]*Producer),
		consumers:  make(map[string]*Consumer),
		builders:   make(map[string]*namespace.Builder),
		lastValues: make(map[string]interface{}),
		batchChan:  make(chan publishJob, MaxBatchQueueSize),
		stopChan:   make(chan struct{}),
	}
	m.startBatcher()
	return m
}

// startBatcher starts the batch aggregation goroutine.
func (m *Manager) startBatcher() {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	m.wg.Add(1) // Must be inside lock to prevent race with StopAll()
	m.mu.Unlock()

	go m.batchProcessor()
}

// batchProcessor collects messages and publishes them in batches per topic.
func (m *Manager) batchProcessor() {
	defer m.wg.Done()

	// Batches per producer+topic key
	batches := make(map[string]*topicBatch)
	ticker := time.NewTicker(BatchFlushInterval)
	defer ticker.Stop()

	flushBatch := func(key string, batch *topicBatch) {
		if len(batch.messages) == 0 {
			return
		}

		batchStart := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := batch.producer.ProduceBatch(ctx, batch.topic, batch.messages)
		cancel()

		if err == nil {
			// Update cache for all successful messages
			m.lastMu.Lock()
			for cacheKey, value := range batch.cacheUpdates {
				m.lastValues[cacheKey] = value
			}
			m.lastMu.Unlock()

			if time.Since(batchStart) > 50*time.Millisecond {
				logKafka("BATCH: flushed %d msgs to %s in %v", len(batch.messages), batch.topic, time.Since(batchStart))
			}
		} else {
			logKafka("BATCH: failed to flush %d msgs to %s: %v", len(batch.messages), batch.topic, err)
		}
	}

	flushAll := func() {
		for key, batch := range batches {
			flushBatch(key, batch)
			delete(batches, key)
		}
	}

	for {
		select {
		case <-m.stopChan:
			// Flush remaining batches before exit
			flushAll()
			return

		case job, ok := <-m.batchChan:
			if !ok {
				flushAll()
				return
			}

			// Create batch key from producer name + topic
			batchKey := job.producer.config.Name + ":" + job.topic

			batch, exists := batches[batchKey]
			if !exists {
				batch = &topicBatch{
					producer:     job.producer,
					topic:        job.topic,
					messages:     make([]kafka.Message, 0, MaxBatchSize),
					cacheUpdates: make(map[string]interface{}),
				}
				batches[batchKey] = batch
			}

			// Add message to batch
			batch.messages = append(batch.messages, kafka.Message{
				Key:   job.key,
				Value: job.payload,
				Time:  job.queueTime,
			})
			batch.cacheUpdates[job.cacheKey] = job.value

			// Flush if batch is full
			if len(batch.messages) >= MaxBatchSize {
				flushBatch(batchKey, batch)
				delete(batches, batchKey)
			}

		case <-ticker.C:
			// Periodic flush of all partial batches
			flushAll()
		}
	}
}

// AddCluster adds a new Kafka cluster configuration.
func (m *Manager) AddCluster(config *Config, ns string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.producers[config.Name]; exists {
		return
	}

	builder := namespace.New(ns, config.Selector)
	m.builders[config.Name] = builder

	producer := NewProducer(config)
	m.producers[config.Name] = producer

	// Create consumer if writeback is enabled
	if config.EnableWriteback {
		consumer := NewConsumer(config, producer, builder)
		// Apply current callbacks
		if m.writeHandler != nil {
			consumer.SetWriteHandler(m.writeHandler)
		}
		if m.writeValidator != nil {
			consumer.SetWriteValidator(m.writeValidator)
		}
		if m.tagTypeLookup != nil {
			consumer.SetTagTypeLookup(m.tagTypeLookup)
		}
		m.consumers[config.Name] = consumer
	}
}

// RemoveCluster removes a Kafka cluster and disconnects.
func (m *Manager) RemoveCluster(name string) {
	m.mu.Lock()
	producer, producerExists := m.producers[name]
	consumer, consumerExists := m.consumers[name]
	if producerExists {
		delete(m.producers, name)
	}
	if consumerExists {
		delete(m.consumers, name)
	}
	delete(m.builders, name)
	m.mu.Unlock()

	if consumerExists && consumer != nil {
		consumer.Stop()
	}
	if producerExists && producer != nil {
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

// ConnectEnabled connects to all enabled Kafka clusters and starts consumers.
func (m *Manager) ConnectEnabled() {
	m.mu.RLock()
	producers := make([]*Producer, 0)
	consumers := make([]*Consumer, 0)
	for _, p := range m.producers {
		if p.config.Enabled {
			producers = append(producers, p)
		}
	}
	for _, c := range m.consumers {
		if c.config.Enabled && c.config.EnableWriteback {
			consumers = append(consumers, c)
		}
	}
	m.mu.RUnlock()

	// Connect producers first
	for _, p := range producers {
		go func(prod *Producer) {
			if err := prod.Connect(); err != nil {
				logKafka("Failed to connect producer %s: %v", prod.config.Name, err)
			}
		}(p)
	}

	// Start consumers after a short delay to allow producers to connect
	// (consumers need the producer to send responses)
	if len(consumers) > 0 {
		logKafka("Scheduling %d writeback consumer(s) to start after producer connection", len(consumers))
		go func() {
			time.Sleep(500 * time.Millisecond)
			for _, c := range consumers {
				logKafka("Starting writeback consumer for cluster %s (topic: %s, group: %s)",
					c.config.Name, c.builder.KafkaWriteTopic(), c.config.GetConsumerGroup())
				if err := c.Start(); err != nil {
					logKafka("Failed to start consumer %s: %v", c.config.Name, err)
				} else {
					logKafka("Writeback consumer %s started successfully", c.config.Name)
				}
			}
		}()
	}
}

// StopAll disconnects from all Kafka clusters and stops the batcher.
func (m *Manager) StopAll() {
	// Stop consumers first (they depend on producers for responses)
	m.mu.RLock()
	consumers := make([]*Consumer, 0, len(m.consumers))
	for _, c := range m.consumers {
		consumers = append(consumers, c)
	}
	m.mu.RUnlock()

	if len(consumers) > 0 {
		logKafka("Stopping %d writeback consumer(s)", len(consumers))
	}
	for _, c := range consumers {
		logKafka("Stopping writeback consumer for cluster %s", c.config.Name)
		c.Stop()
	}

	// Stop the batcher goroutine
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		// Still disconnect producers even if batcher wasn't started
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
	m.batchChan = make(chan publishJob, MaxBatchQueueSize)
	m.started = false
	m.mu.Unlock()

	// Stop batcher by closing old channel
	close(oldStopChan)

	// Wait for batcher to finish (with timeout)
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		logKafka("Timeout waiting for batch processor to stop")
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
func (m *Manager) LoadFromConfigs(configs []Config, ns string) {
	for i := range configs {
		m.AddCluster(&configs[i], ns)
	}
}

func logKafka(format string, args ...interface{}) {
	logging.DebugLog("Kafka", format, args...)
}

// Publish sends a tag value to all connected Kafka clusters that have PublishChanges enabled.
// Only publishes if the value has changed (unless force is true).
// For S7 PLCs, alias is the user-defined name and address is the S7 address in uppercase.
func (m *Manager) Publish(plcName, tagName, alias, address, typeName string, value interface{}, writable, force bool) {
	// Ensure workers are running
	m.startBatcher()

	m.mu.RLock()
	producers := make([]*Producer, 0, len(m.producers))
	builders := make(map[string]*namespace.Builder, len(m.builders))
	for name, p := range m.producers {
		producers = append(producers, p)
		builders[name] = m.builders[name]
	}
	m.mu.RUnlock()

	for _, p := range producers {
		// Skip if not connected or not enabled for publishing
		status := p.GetStatus()
		if status != StatusConnected {
			continue
		}
		if !p.config.PublishChanges {
			continue
		}

		builder := builders[p.config.Name]
		if builder == nil {
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
			Offset:    address, // Original tag name/address when alias is used
			Value:     value,
			Type:      typeName,
			Writable:  writable,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}

		payload, err := json.Marshal(msg)
		if err != nil {
			continue
		}

		// Use alias (or tag name if no alias) as key for ordering
		key := []byte(fmt.Sprintf("%s.%s", plcName, displayTag))

		// Queue the publish job for batching (non-blocking with drop on overflow)
		job := publishJob{
			producer:  p,
			topic:     builder.KafkaTagTopic(),
			key:       key,
			payload:   payload,
			cacheKey:  cacheKey,
			value:     value,
			queueTime: time.Now(),
		}
		// Block until queued (with timeout to prevent indefinite blocking)
		// This ensures no messages are dropped - back-pressure to caller
		select {
		case m.batchChan <- job:
			// Job queued for batching
		case <-time.After(5 * time.Second):
			// Queue blocked for too long - this indicates a serious problem
			logKafka("WARN: Batch queue blocked >5s, message may be delayed for %s", cacheKey)
			// Still try to queue (blocking) - don't drop messages
			m.batchChan <- job
		}
	}
}

// PublishHealth publishes PLC health status to all connected Kafka clusters.
func (m *Manager) PublishHealth(plcName, driver string, online bool, status, errMsg string) {
	m.startBatcher()

	m.mu.RLock()
	producers := make([]*Producer, 0, len(m.producers))
	builders := make(map[string]*namespace.Builder, len(m.builders))
	for name, p := range m.producers {
		producers = append(producers, p)
		builders[name] = m.builders[name]
	}
	m.mu.RUnlock()

	for _, p := range producers {
		// Skip if not connected or not enabled for publishing
		if p.GetStatus() != StatusConnected {
			continue
		}
		if !p.config.PublishChanges {
			continue
		}

		builder := builders[p.config.Name]
		if builder == nil {
			continue
		}

		msg := HealthMessage{
			PLC:       plcName,
			Driver:    driver,
			Online:    online,
			Status:    status,
			Error:     errMsg,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}

		payload, err := json.Marshal(msg)
		if err != nil {
			continue
		}

		// Use health topic from builder
		healthTopic := builder.KafkaHealthTopic()
		key := []byte(plcName)

		job := publishJob{
			producer:  p,
			topic:     healthTopic,
			key:       key,
			payload:   payload,
			cacheKey:  fmt.Sprintf("%s/%s/health", p.config.Name, plcName),
			value:     nil, // Health messages are always published
			queueTime: time.Now(),
		}
		// Block until queued (with timeout) - no message dropping
		select {
		case m.batchChan <- job:
			// Job queued for batching
		case <-time.After(5 * time.Second):
			logKafka("WARN: Batch queue blocked >5s for health message %s", plcName)
			m.batchChan <- job
		}
	}
}

// AnyPublishing returns true if any cluster has PublishChanges enabled and is connected.
func (m *Manager) AnyPublishing() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for name, p := range m.producers {
		status := p.GetStatus()
		if status == StatusConnected && p.config.PublishChanges && m.builders[name] != nil {
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

// SetWriteHandler sets the callback for processing write requests.
// This is applied to all consumers.
func (m *Manager) SetWriteHandler(handler WriteHandler) {
	m.mu.Lock()
	m.writeHandler = handler
	consumers := make([]*Consumer, 0, len(m.consumers))
	for _, c := range m.consumers {
		consumers = append(consumers, c)
	}
	m.mu.Unlock()

	for _, c := range consumers {
		c.SetWriteHandler(handler)
	}
}

// SetWriteValidator sets the callback for validating write requests.
// This is applied to all consumers.
func (m *Manager) SetWriteValidator(validator WriteValidator) {
	m.mu.Lock()
	m.writeValidator = validator
	consumers := make([]*Consumer, 0, len(m.consumers))
	for _, c := range m.consumers {
		consumers = append(consumers, c)
	}
	m.mu.Unlock()

	for _, c := range consumers {
		c.SetWriteValidator(validator)
	}
}

// SetTagTypeLookup sets the callback for looking up tag types.
// This is applied to all consumers.
func (m *Manager) SetTagTypeLookup(lookup TagTypeLookup) {
	m.mu.Lock()
	m.tagTypeLookup = lookup
	consumers := make([]*Consumer, 0, len(m.consumers))
	for _, c := range m.consumers {
		consumers = append(consumers, c)
	}
	m.mu.Unlock()

	for _, c := range consumers {
		c.SetTagTypeLookup(lookup)
	}
}

// PublishRaw publishes raw bytes to a topic on all connected clusters.
// The key is used for Kafka partitioning.
func (m *Manager) PublishRaw(topic string, data []byte) {
	m.PublishRawWithKey(topic, topic, data)
}

// PublishRawWithKey publishes raw bytes to a topic with a specific key.
func (m *Manager) PublishRawWithKey(topic, key string, data []byte) {
	m.startBatcher()

	m.mu.RLock()
	producers := make([]*Producer, 0, len(m.producers))
	for _, p := range m.producers {
		producers = append(producers, p)
	}
	m.mu.RUnlock()

	for _, p := range producers {
		if p.GetStatus() != StatusConnected {
			continue
		}

		job := publishJob{
			producer:  p,
			topic:     topic,
			key:       []byte(key),
			payload:   data,
			cacheKey:  "", // No caching for raw publishes
			value:     nil,
			queueTime: time.Now(),
		}

		select {
		case m.batchChan <- job:
			// Queued
		case <-time.After(5 * time.Second):
			logKafka("WARN: Batch queue blocked >5s for raw publish to %s", topic)
			m.batchChan <- job
		}
	}
}

// PublishTagPack publishes a TagPack to all connected clusters.
// Each cluster uses its own namespace builder to compute the correct topic.
func (m *Manager) PublishTagPack(packName string, data []byte) {
	m.startBatcher()

	m.mu.RLock()
	producers := make([]*Producer, 0, len(m.producers))
	builders := make(map[string]*namespace.Builder, len(m.builders))
	for name, p := range m.producers {
		producers = append(producers, p)
		builders[p.config.Name] = m.builders[name]
	}
	m.mu.RUnlock()

	for _, p := range producers {
		if p.GetStatus() != StatusConnected {
			continue
		}

		builder := builders[p.config.Name]
		if builder == nil {
			continue
		}

		// Compute topic using this cluster's namespace builder
		topic := builder.KafkaTagTopic()
		key := "pack:" + packName

		job := publishJob{
			producer:  p,
			topic:     topic,
			key:       []byte(key),
			payload:   data,
			cacheKey:  "", // No caching for TagPacks
			value:     nil,
			queueTime: time.Now(),
		}

		select {
		case m.batchChan <- job:
			// Queued
		case <-time.After(5 * time.Second):
			logKafka("WARN: Batch queue blocked >5s for TagPack %s to %s", packName, topic)
			m.batchChan <- job
		}
	}
}
