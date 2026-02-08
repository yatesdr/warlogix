package trigger

import (
	"context"
	"fmt"
	"sync"
	"time"

	"warlogix/config"
	"warlogix/kafka"
	"warlogix/tagpack"
)

// Status represents the current state of a trigger.
type Status int

const (
	StatusDisabled Status = iota
	StatusArmed
	StatusFiring
	StatusCooldown
	StatusError
)

func (s Status) String() string {
	switch s {
	case StatusDisabled:
		return "Disabled"
	case StatusArmed:
		return "Armed"
	case StatusFiring:
		return "Firing"
	case StatusCooldown:
		return "Cooldown"
	case StatusError:
		return "Error"
	default:
		return "Unknown"
	}
}

// TagReader provides access to PLC tag values.
type TagReader interface {
	// ReadTag reads a single tag value from the named PLC.
	ReadTag(plcName, tagName string) (interface{}, error)
	// ReadTags reads multiple tag values from the named PLC.
	ReadTags(plcName string, tagNames []string) (map[string]interface{}, error)
}

// TagWriter provides write access to PLC tags.
type TagWriter interface {
	// WriteTag writes a value to a tag on the named PLC.
	WriteTag(plcName, tagName string, value interface{}) error
}

// Trigger monitors a PLC tag and captures data when a condition is met.
type Trigger struct {
	config    *config.TriggerConfig
	condition *Condition
	kafka     *kafka.Manager
	packMgr   *tagpack.Manager
	reader    TagReader
	writer    TagWriter

	status    Status
	lastErr   error
	fireCount int64
	lastFire  time.Time
	mu        sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Edge detection
	lastConditionMet bool

	// Debounce
	lastEdgeTime time.Time

	// Logging callback
	logFn func(format string, args ...interface{})
}

// NewTrigger creates a new trigger.
func NewTrigger(cfg *config.TriggerConfig, kafkaMgr *kafka.Manager, reader TagReader, writer TagWriter) (*Trigger, error) {
	op, err := ParseOperator(cfg.Condition.Operator)
	if err != nil {
		return nil, fmt.Errorf("invalid condition operator: %w", err)
	}

	condition := &Condition{
		Operator: op,
		Value:    cfg.Condition.Value,
	}

	return &Trigger{
		config:    cfg,
		condition: condition,
		kafka:     kafkaMgr,
		reader:    reader,
		writer:    writer,
		status:    StatusDisabled,
	}, nil
}

// SetLogFunc sets the logging callback.
func (t *Trigger) SetLogFunc(fn func(format string, args ...interface{})) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.logFn = fn
}

// SetPackManager sets the TagPack manager for publishing packs on trigger fire.
func (t *Trigger) SetPackManager(packMgr *tagpack.Manager) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.packMgr = packMgr
}

func (t *Trigger) log(format string, args ...interface{}) {
	// Use TryRLock to avoid blocking - skip logging if locked
	if !t.mu.TryRLock() {
		return
	}
	fn := t.logFn
	t.mu.RUnlock()
	if fn != nil {
		fn("[Trigger:%s] "+format, append([]interface{}{t.config.Name}, args...)...)
	}
}

// GetStatus returns the current trigger status.
// Uses TryRLock to avoid blocking the UI thread.
func (t *Trigger) GetStatus() Status {
	if !t.mu.TryRLock() {
		return StatusFiring // Return firing if locked (likely in fire())
	}
	defer t.mu.RUnlock()
	return t.status
}

// GetError returns the last error.
// Uses TryRLock to avoid blocking the UI thread.
func (t *Trigger) GetError() error {
	if !t.mu.TryRLock() {
		return nil
	}
	defer t.mu.RUnlock()
	return t.lastErr
}

// GetStats returns trigger statistics.
// Uses TryRLock to avoid blocking the UI thread.
func (t *Trigger) GetStats() (fireCount int64, lastFire time.Time) {
	if !t.mu.TryRLock() {
		return 0, time.Time{}
	}
	defer t.mu.RUnlock()
	return t.fireCount, t.lastFire
}

// Start begins monitoring the trigger condition.
func (t *Trigger) Start() {
	t.mu.Lock()
	if t.ctx != nil {
		t.mu.Unlock()
		return // Already running
	}

	if !t.config.Enabled {
		t.status = StatusDisabled
		t.mu.Unlock()
		return
	}

	t.ctx, t.cancel = context.WithCancel(context.Background())
	t.status = StatusArmed
	t.lastConditionMet = false
	t.mu.Unlock()

	t.wg.Add(1)
	go t.monitorLoop()

	t.log("started, monitoring %s.%s", t.config.PLC, t.config.TriggerTag)
}

// Stop halts the trigger monitoring.
func (t *Trigger) Stop() {
	t.mu.Lock()
	if t.cancel != nil {
		t.cancel()
	}
	t.mu.Unlock()

	// Wait for monitor loop to finish with timeout
	// (don't block forever if a Kafka write is in progress)
	done := make(chan struct{})
	go func() {
		t.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		// Timeout - proceed anyway, goroutine will finish eventually
	}

	t.mu.Lock()
	t.ctx = nil
	t.cancel = nil
	t.status = StatusDisabled
	t.mu.Unlock()

	t.log("stopped")
}

// monitorLoop continuously monitors the trigger tag.
func (t *Trigger) monitorLoop() {
	defer t.wg.Done()

	// Poll interval - fast enough for responsive triggers
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			t.checkTrigger()
		}
	}
}

// checkTrigger reads the trigger tag and checks the condition.
func (t *Trigger) checkTrigger() {
	t.mu.RLock()
	status := t.status
	t.mu.RUnlock()

	// Only check if armed or in cooldown
	if status != StatusArmed && status != StatusCooldown {
		return
	}

	// Read trigger tag
	value, err := t.reader.ReadTag(t.config.PLC, t.config.TriggerTag)
	if err != nil {
		t.log("error reading trigger tag: %v", err)
		return
	}

	// Evaluate condition
	conditionMet, err := t.condition.Evaluate(value)
	if err != nil {
		t.log("error evaluating condition: %v", err)
		return
	}

	t.mu.Lock()
	wasConditionMet := t.lastConditionMet
	t.lastConditionMet = conditionMet

	// Handle state transitions
	switch t.status {
	case StatusArmed:
		// Check for rising edge (false -> true)
		if conditionMet && !wasConditionMet {
			// Check debounce
			debounce := time.Duration(t.config.DebounceMS) * time.Millisecond
			if time.Since(t.lastEdgeTime) < debounce {
				t.mu.Unlock()
				return
			}
			t.lastEdgeTime = time.Now()
			t.status = StatusFiring
			t.mu.Unlock()

			// Fire the trigger (in current goroutine for simplicity)
			t.fire()
			return
		}

	case StatusCooldown:
		// Wait for condition to become false before re-arming
		if !conditionMet {
			t.status = StatusArmed
			t.log("re-armed")
		}
	}
	t.mu.Unlock()
}

// fire captures data and sends to Kafka.
func (t *Trigger) fire() {
	t.log("triggered, capturing %d tags", len(t.config.Tags))

	// Read all data tags
	data, err := t.reader.ReadTags(t.config.PLC, t.config.Tags)
	if err != nil {
		t.handleError(fmt.Errorf("failed to read data tags: %w", err))
		return
	}

	// Build message
	msg := NewMessage(t.config.Name, t.config.PLC, t.config.Metadata, data)

	// Serialize to JSON
	jsonData, err := msg.ToJSON()
	if err != nil {
		t.handleError(fmt.Errorf("failed to serialize message: %w", err))
		return
	}

	// Send to Kafka if configured
	if t.config.KafkaCluster != "" && t.config.Topic != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err = t.kafka.ProduceWithRetry(ctx, t.config.KafkaCluster, t.config.Topic, msg.Key(), jsonData)
		if err != nil {
			t.handleError(fmt.Errorf("failed to send to Kafka: %w", err))
			return
		}
		t.log("sent to Kafka, sequence=%d", msg.Sequence)
	} else {
		t.log("trigger fired, sequence=%d (no Kafka configured)", msg.Sequence)
	}

	// Success - write acknowledgment
	t.mu.Lock()
	t.fireCount++
	t.lastFire = time.Now()
	t.status = StatusCooldown
	t.lastErr = nil
	packMgr := t.packMgr
	t.mu.Unlock()

	// Publish TagPack immediately if configured
	if t.config.PublishPack != "" && packMgr != nil {
		packMgr.PublishPackImmediate(t.config.PublishPack)
		t.log("published pack '%s'", t.config.PublishPack)
	}

	// Write ack tag with success value (1) if configured
	if t.config.AckTag != "" && t.writer != nil {
		if err := t.writer.WriteTag(t.config.PLC, t.config.AckTag, int32(1)); err != nil {
			t.log("warning: failed to write ack tag: %v", err)
		} else {
			t.log("wrote ack=1 (success) to %s", t.config.AckTag)
		}
	}
}

// handleError handles a trigger error by writing to ack tag and logging.
func (t *Trigger) handleError(err error) {
	t.log("error: %v", err)

	t.mu.Lock()
	t.lastErr = err
	t.status = StatusError
	t.mu.Unlock()

	// Write ack tag with error value (-1) if configured
	if t.config.AckTag != "" && t.writer != nil {
		if writeErr := t.writer.WriteTag(t.config.PLC, t.config.AckTag, int32(-1)); writeErr != nil {
			t.log("warning: failed to write ack tag: %v", writeErr)
		} else {
			t.log("wrote ack=-1 (error) to %s", t.config.AckTag)
		}
	}

	// Go to cooldown to wait for condition reset
	t.mu.Lock()
	t.status = StatusCooldown
	t.mu.Unlock()
}

// Reset clears the error state and re-arms the trigger.
func (t *Trigger) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.status == StatusError || t.status == StatusCooldown {
		t.status = StatusArmed
		t.lastErr = nil
		t.lastConditionMet = false
	}
}

// TestFire manually fires the trigger for testing purposes.
// This bypasses the condition check and immediately captures and sends data.
func (t *Trigger) TestFire() error {
	t.mu.RLock()
	status := t.status
	t.mu.RUnlock()

	// Allow test fire from Armed, Cooldown, or Error states
	if status == StatusDisabled {
		return fmt.Errorf("trigger is disabled, start it first")
	}

	// Pre-check Kafka connectivity if Kafka is configured
	if t.config.KafkaCluster != "" && t.config.Topic != "" {
		producer := t.kafka.GetProducer(t.config.KafkaCluster)
		if producer == nil {
			return fmt.Errorf("Kafka cluster '%s' not found", t.config.KafkaCluster)
		}
		if producer.GetStatus() != kafka.StatusConnected {
			return fmt.Errorf("Kafka cluster '%s' not connected - connect via Kafka tab first", t.config.KafkaCluster)
		}
	}

	t.log("TEST FIRE triggered manually")
	t.fire()

	// Check if fire resulted in error
	t.mu.RLock()
	lastErr := t.lastErr
	t.mu.RUnlock()
	if lastErr != nil {
		return lastErr
	}

	return nil
}
