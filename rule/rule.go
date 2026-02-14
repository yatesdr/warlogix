package rule

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"warlink/config"
	"warlink/kafka"
	"warlink/tagpack"
)

// Status represents the current state of a rule.
type Status int

const (
	StatusDisabled     Status = iota
	StatusArmed               // Monitoring conditions, ready to fire
	StatusFiring              // Executing actions
	StatusWaitingClear        // Actions dispatched, waiting for condition aggregate to go false
	StatusCooldown            // Cleared, waiting cooldown interval
	StatusError               // Error state
)

func (s Status) String() string {
	switch s {
	case StatusDisabled:
		return "Stopped"
	case StatusArmed:
		return "Armed"
	case StatusFiring:
		return "Firing"
	case StatusWaitingClear:
		return "Waiting Clear"
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
	ReadTag(plcName, tagName string) (interface{}, error)
	ReadTags(plcName string, tagNames []string) (map[string]interface{}, error)
}

// TagWriter provides write access to PLC tags.
type TagWriter interface {
	WriteTag(plcName, tagName string, value interface{}) error
}

// MQTTPublisher provides MQTT publishing for rules.
type MQTTPublisher interface {
	PublishRawQoS2ToBroker(broker, topic string, data []byte) bool
	ListBrokers() []string
}

// Rule monitors PLC tags and executes actions when conditions are met.
type Rule struct {
	config     *config.RuleConfig
	conditions []*Condition
	kafka      *kafka.Manager
	mqtt       MQTTPublisher
	packMgr    *tagpack.Manager
	reader     TagReader
	writer     TagWriter
	namespace  string

	status    Status
	lastErr   error
	fireCount int64
	lastFire  time.Time
	mu        sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Edge detection
	lastAggregateResult bool
	lastEdgeTime        time.Time

	httpClient *http.Client
	logFn      func(format string, args ...interface{})
}

// NewRule creates a new rule from configuration.
func NewRule(cfg *config.RuleConfig, kafkaMgr *kafka.Manager, reader TagReader, writer TagWriter) (*Rule, error) {
	conditions := make([]*Condition, len(cfg.Conditions))
	for i, cond := range cfg.Conditions {
		op, err := ParseOperator(cond.Operator)
		if err != nil {
			return nil, fmt.Errorf("condition %d: invalid operator: %w", i, err)
		}
		conditions[i] = &Condition{
			Operator: op,
			Value:    cond.Value,
			Not:      cond.Not,
		}
	}

	return &Rule{
		config:     cfg,
		conditions: conditions,
		kafka:      kafkaMgr,
		reader:     reader,
		writer:     writer,
		status:     StatusDisabled,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// SetLogFunc sets the logging callback.
func (r *Rule) SetLogFunc(fn func(format string, args ...interface{})) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logFn = fn
}

// SetPackManager sets the TagPack manager.
func (r *Rule) SetPackManager(packMgr *tagpack.Manager) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.packMgr = packMgr
}

// SetMQTTManager sets the MQTT publisher.
func (r *Rule) SetMQTTManager(mqtt MQTTPublisher) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.mqtt = mqtt
}

// SetNamespace sets the namespace for topic construction.
func (r *Rule) SetNamespace(ns string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.namespace = ns
}

func (r *Rule) log(format string, args ...interface{}) {
	if !r.mu.TryRLock() {
		return
	}
	fn := r.logFn
	r.mu.RUnlock()
	if fn != nil {
		fn("[Rule:%s] "+format, append([]interface{}{r.config.Name}, args...)...)
	}
}

// GetStatus returns the current rule status.
func (r *Rule) GetStatus() Status {
	if !r.mu.TryRLock() {
		return StatusFiring
	}
	defer r.mu.RUnlock()
	return r.status
}

// GetError returns the last error.
func (r *Rule) GetError() error {
	if !r.mu.TryRLock() {
		return nil
	}
	defer r.mu.RUnlock()
	return r.lastErr
}

// GetStats returns rule statistics.
func (r *Rule) GetStats() (fireCount int64, lastFire time.Time) {
	if !r.mu.TryRLock() {
		return 0, time.Time{}
	}
	defer r.mu.RUnlock()
	return r.fireCount, r.lastFire
}

// Start begins monitoring the rule conditions.
func (r *Rule) Start() {
	r.mu.Lock()
	if r.ctx != nil {
		r.mu.Unlock()
		return // Already running
	}

	if !r.config.Enabled {
		r.status = StatusDisabled
		r.mu.Unlock()
		return
	}

	r.ctx, r.cancel = context.WithCancel(context.Background())
	r.status = StatusArmed
	r.lastAggregateResult = false
	r.mu.Unlock()

	r.wg.Add(1)
	go r.monitorLoop()

	r.log("started, monitoring %d conditions", len(r.config.Conditions))
}

// Stop halts the rule monitoring.
func (r *Rule) Stop() {
	r.mu.Lock()
	if r.cancel != nil {
		r.cancel()
	}
	r.mu.Unlock()

	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
	}

	r.mu.Lock()
	r.ctx = nil
	r.cancel = nil
	r.status = StatusDisabled
	r.mu.Unlock()

	r.log("stopped")
}

// monitorLoop continuously monitors rule conditions.
func (r *Rule) monitorLoop() {
	defer r.wg.Done()

	r.mu.RLock()
	ctx := r.ctx
	r.mu.RUnlock()
	if ctx == nil {
		return
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.checkRule()
		}
	}
}

// checkRule reads condition tags and handles state transitions.
func (r *Rule) checkRule() {
	r.mu.RLock()
	status := r.status
	r.mu.RUnlock()

	switch status {
	case StatusArmed:
		r.checkArmed()
	case StatusWaitingClear:
		r.checkWaitingClear()
	case StatusCooldown:
		r.checkCooldown()
	}
}

// evaluateAggregate reads all conditions and returns the aggregate result.
func (r *Rule) evaluateAggregate() (bool, error) {
	if len(r.conditions) == 0 {
		return false, nil
	}

	logicMode := r.config.LogicMode
	if logicMode == "" {
		logicMode = config.RuleLogicAND
	}

	for i, cond := range r.conditions {
		cfg := r.config.Conditions[i]
		value, err := r.reader.ReadTag(cfg.PLC, cfg.Tag)
		if err != nil {
			return false, fmt.Errorf("condition %d (%s.%s): %w", i, cfg.PLC, cfg.Tag, err)
		}

		met, err := cond.Evaluate(value)
		if err != nil {
			return false, fmt.Errorf("condition %d: %w", i, err)
		}

		if logicMode == config.RuleLogicOR && met {
			return true, nil
		}
		if logicMode == config.RuleLogicAND && !met {
			return false, nil
		}
	}

	// AND: all true → true; OR: none true → false
	if logicMode == config.RuleLogicAND {
		return true, nil
	}
	return false, nil
}

// checkArmed evaluates conditions and fires on rising edge.
func (r *Rule) checkArmed() {
	aggregate, err := r.evaluateAggregate()
	if err != nil {
		r.log("error evaluating conditions: %v", err)
		return
	}

	r.mu.Lock()
	wasTrue := r.lastAggregateResult
	r.lastAggregateResult = aggregate

	// Rising edge: false → true
	if aggregate && !wasTrue {
		debounce := time.Duration(r.config.DebounceMS) * time.Millisecond
		if time.Since(r.lastEdgeTime) < debounce {
			r.mu.Unlock()
			return
		}
		r.lastEdgeTime = time.Now()
		r.status = StatusFiring
		r.mu.Unlock()

		r.log("conditions met, firing %d actions", len(r.config.Actions))
		r.fireActions(r.config.Actions, false)
		return
	}
	r.mu.Unlock()
}

// checkWaitingClear waits for aggregate to go false, then fires cleared actions.
func (r *Rule) checkWaitingClear() {
	aggregate, err := r.evaluateAggregate()
	if err != nil {
		return
	}

	r.mu.Lock()
	r.lastAggregateResult = aggregate

	if !aggregate {
		// Falling edge: fire cleared actions
		if len(r.config.ClearedActions) > 0 {
			r.mu.Unlock()
			r.log("conditions cleared, firing %d cleared actions", len(r.config.ClearedActions))
			r.fireActions(r.config.ClearedActions, true)
			return
		}

		// No cleared actions — transition based on cooldown
		if r.config.CooldownMS > 0 {
			r.status = StatusCooldown
			r.log("conditions cleared, entering cooldown")
		} else {
			r.status = StatusArmed
			r.log("conditions cleared, re-armed")
		}
	}
	r.mu.Unlock()
}

// checkCooldown waits for cooldown to elapse.
func (r *Rule) checkCooldown() {
	r.mu.Lock()
	defer r.mu.Unlock()

	cooldown := time.Duration(r.config.CooldownMS) * time.Millisecond
	if time.Since(r.lastFire) >= cooldown {
		r.status = StatusArmed
		r.lastAggregateResult = false
		r.log("cooldown elapsed, re-armed")
	}
}

// fireActions dispatches all actions in parallel and transitions state.
func (r *Rule) fireActions(actions []config.RuleAction, isCleared bool) {
	var wg sync.WaitGroup
	for i := range actions {
		wg.Add(1)
		go func(action *config.RuleAction) {
			defer wg.Done()
			r.executeAction(action)
		}(&actions[i])
	}
	wg.Wait()

	r.mu.Lock()
	if !isCleared {
		r.fireCount++
		r.lastFire = time.Now()
		r.lastErr = nil
		r.status = StatusWaitingClear
	} else {
		if r.config.CooldownMS > 0 {
			r.status = StatusCooldown
		} else {
			r.status = StatusArmed
		}
	}
	r.mu.Unlock()
}

// Reset clears the error state and re-arms the rule.
func (r *Rule) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.status == StatusError || r.status == StatusWaitingClear || r.status == StatusCooldown {
		r.status = StatusArmed
		r.lastErr = nil
		r.lastAggregateResult = false
	}
}

// TestFire manually fires the rule actions for testing.
func (r *Rule) TestFire() error {
	r.log("TEST FIRE triggered manually")
	r.fireActions(r.config.Actions, false)

	r.mu.RLock()
	lastErr := r.lastErr
	r.mu.RUnlock()
	if lastErr != nil {
		return lastErr
	}

	// Reset status back to armed/disabled after test fire
	r.mu.Lock()
	if r.status == StatusWaitingClear {
		r.status = StatusArmed
	}
	r.mu.Unlock()

	return nil
}
