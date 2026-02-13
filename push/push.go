package push

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"warlink/config"
	"warlink/trigger"
)

// Status represents the current state of a push.
type Status int

const (
	StatusDisabled     Status = iota
	StatusArmed               // Monitoring conditions
	StatusFiring              // Sending HTTP request
	StatusWaitingClear        // Sent, waiting for triggering condition(s) to clear
	StatusMinInterval         // Cleared, waiting minimum cooldown interval
	StatusError               // Error state
)

func (s Status) String() string {
	switch s {
	case StatusDisabled:
		return "Disabled"
	case StatusArmed:
		return "Armed"
	case StatusFiring:
		return "Firing"
	case StatusWaitingClear:
		return "Waiting Clear"
	case StatusMinInterval:
		return "Cooldown"
	case StatusError:
		return "Error"
	default:
		return "Unknown"
	}
}

// tagRefRegex matches #PLCName.tagName references in body templates.
var tagRefRegex = regexp.MustCompile(`#([a-zA-Z_]\w*(?:\.\w+)+)`)

// conditionState tracks per-condition edge detection and cooldown state.
type conditionState struct {
	lastMet       bool      // Previous evaluation result
	inCooldown    bool      // Per-condition cooldown tracking
	condClearedAt time.Time // When this condition went false
}

// Push monitors PLC tag conditions and sends HTTP requests when conditions are met.
type Push struct {
	config     *config.PushConfig
	conditions []*trigger.Condition
	reader     trigger.TagReader

	status       Status
	lastErr      error
	sendCount    int64
	lastSend     time.Time
	lastHTTPCode int
	mu           sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	condStates []conditionState

	httpClient *http.Client
	logFn      func(format string, args ...interface{})
}

// NewPush creates a new push from configuration.
func NewPush(cfg *config.PushConfig, reader trigger.TagReader) (*Push, error) {
	conditions := make([]*trigger.Condition, len(cfg.Conditions))
	for i, cond := range cfg.Conditions {
		op, err := trigger.ParseOperator(cond.Operator)
		if err != nil {
			return nil, fmt.Errorf("condition %d: invalid operator: %w", i, err)
		}
		conditions[i] = &trigger.Condition{
			Operator: op,
			Value:    cond.Value,
		}
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &Push{
		config:     cfg,
		conditions: conditions,
		reader:     reader,
		status:     StatusDisabled,
		condStates: make([]conditionState, len(cfg.Conditions)),
		httpClient: &http.Client{Timeout: timeout},
	}, nil
}

// SetLogFunc sets the logging callback.
func (p *Push) SetLogFunc(fn func(format string, args ...interface{})) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.logFn = fn
}

func (p *Push) log(format string, args ...interface{}) {
	if !p.mu.TryRLock() {
		return
	}
	fn := p.logFn
	p.mu.RUnlock()
	if fn != nil {
		fn("[Push:%s] "+format, append([]interface{}{p.config.Name}, args...)...)
	}
}

// GetStatus returns the current push status.
func (p *Push) GetStatus() Status {
	if !p.mu.TryRLock() {
		return StatusFiring
	}
	defer p.mu.RUnlock()
	return p.status
}

// GetError returns the last error.
func (p *Push) GetError() error {
	if !p.mu.TryRLock() {
		return nil
	}
	defer p.mu.RUnlock()
	return p.lastErr
}

// GetStats returns push statistics.
func (p *Push) GetStats() (sendCount int64, lastSend time.Time, lastHTTPCode int) {
	if !p.mu.TryRLock() {
		return 0, time.Time{}, 0
	}
	defer p.mu.RUnlock()
	return p.sendCount, p.lastSend, p.lastHTTPCode
}

// Start begins monitoring push conditions.
func (p *Push) Start() {
	p.mu.Lock()
	if p.ctx != nil {
		p.mu.Unlock()
		return
	}

	if !p.config.Enabled {
		p.status = StatusDisabled
		p.mu.Unlock()
		return
	}

	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.status = StatusArmed
	p.condStates = make([]conditionState, len(p.config.Conditions))
	p.mu.Unlock()

	p.wg.Add(1)
	go p.monitorLoop()

	p.log("started, monitoring %d conditions", len(p.config.Conditions))
}

// Stop halts push monitoring.
func (p *Push) Stop() {
	p.mu.Lock()
	if p.cancel != nil {
		p.cancel()
	}
	p.mu.Unlock()

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
	}

	p.mu.Lock()
	p.ctx = nil
	p.cancel = nil
	p.status = StatusDisabled
	p.mu.Unlock()

	p.log("stopped")
}

// monitorLoop continuously monitors push conditions.
func (p *Push) monitorLoop() {
	defer p.wg.Done()

	p.mu.RLock()
	ctx := p.ctx
	p.mu.RUnlock()
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
			p.checkConditions()
		}
	}
}

// checkConditions reads and evaluates all conditions, handling state transitions.
func (p *Push) checkConditions() {
	p.mu.RLock()
	status := p.status
	p.mu.RUnlock()

	switch status {
	case StatusArmed:
		p.checkArmed()
	case StatusWaitingClear:
		p.checkWaitingClear()
	case StatusMinInterval:
		p.checkMinInterval()
	}
}

// checkArmed evaluates conditions and fires on rising edge.
func (p *Push) checkArmed() {
	for i, cond := range p.config.Conditions {
		value, err := p.reader.ReadTag(cond.PLC, cond.Tag)
		if err != nil {
			continue
		}

		met, err := p.conditions[i].Evaluate(value)
		if err != nil {
			continue
		}

		p.mu.Lock()
		wasMet := p.condStates[i].lastMet
		p.condStates[i].lastMet = met

		// Rising edge detection
		if met && !wasMet && !p.condStates[i].inCooldown {
			p.status = StatusFiring
			p.mu.Unlock()

			p.log("condition %d fired: %s.%s %s %v", i, cond.PLC, cond.Tag, cond.Operator, cond.Value)
			p.fire(i)
			return
		}
		p.mu.Unlock()
	}
}

// checkWaitingClear checks if the triggering conditions have cleared.
func (p *Push) checkWaitingClear() {
	// Read all conditions to update state
	for i, cond := range p.config.Conditions {
		value, err := p.reader.ReadTag(cond.PLC, cond.Tag)
		if err != nil {
			continue
		}

		met, err := p.conditions[i].Evaluate(value)
		if err != nil {
			continue
		}

		p.mu.Lock()
		p.condStates[i].lastMet = met
		p.mu.Unlock()
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.config.CooldownPerCond {
		// Per-condition mode: check each condition's cooldown individually
		for i := range p.condStates {
			if p.condStates[i].inCooldown && !p.condStates[i].lastMet {
				// Condition cleared
				if p.config.CooldownMin == 0 {
					p.condStates[i].inCooldown = false
				} else {
					p.condStates[i].condClearedAt = time.Now()
				}
			}
		}
		// Check if all cooldowns are clear
		allClear := true
		for i := range p.condStates {
			if p.condStates[i].inCooldown {
				allClear = false
				break
			}
		}
		if allClear {
			if p.config.CooldownMin == 0 {
				p.status = StatusArmed
				p.log("all conditions cleared, re-armed")
			} else {
				p.status = StatusMinInterval
				p.log("all conditions cleared, entering min interval")
			}
		}
	} else {
		// Global mode: ALL conditions must be false
		allFalse := true
		for i := range p.condStates {
			if p.condStates[i].lastMet {
				allFalse = false
				break
			}
		}

		if allFalse {
			if p.config.CooldownMin == 0 {
				p.status = StatusArmed
				p.log("all conditions cleared, re-armed")
			} else {
				p.status = StatusMinInterval
				p.log("all conditions cleared, entering min interval")
			}
		}
	}
}

// checkMinInterval checks if the minimum cooldown interval has elapsed.
func (p *Push) checkMinInterval() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if time.Since(p.lastSend) >= p.config.CooldownMin {
		p.status = StatusArmed
		// Reset per-condition cooldown states
		for i := range p.condStates {
			p.condStates[i].inCooldown = false
			p.condStates[i].lastMet = false
		}
		p.log("cooldown elapsed, re-armed")
	}
}

// fire sends the HTTP request.
func (p *Push) fire(condIndex int) {
	// Resolve body template
	body := p.resolveBody()

	// Build HTTP request
	req, err := p.buildRequest(body)
	if err != nil {
		p.handleError(fmt.Errorf("failed to build request: %w", err))
		return
	}

	// Send request
	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.handleError(fmt.Errorf("HTTP request failed: %w", err))
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	p.log("sent HTTP %s to %s, status=%d", p.config.Method, p.config.URL, resp.StatusCode)

	p.mu.Lock()
	p.sendCount++
	p.lastSend = time.Now()
	p.lastHTTPCode = resp.StatusCode
	p.lastErr = nil

	if p.config.CooldownPerCond {
		p.condStates[condIndex].inCooldown = true
	}
	p.status = StatusWaitingClear
	p.mu.Unlock()
}

// TestFire sends the HTTP request immediately, bypassing conditions and cooldown.
func (p *Push) TestFire() error {
	p.log("TEST FIRE triggered manually")

	body := p.resolveBody()
	req, err := p.buildRequest(body)
	if err != nil {
		return fmt.Errorf("failed to build request: %w", err)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	p.mu.Lock()
	p.sendCount++
	p.lastSend = time.Now()
	p.lastHTTPCode = resp.StatusCode
	p.lastErr = nil
	p.mu.Unlock()

	p.log("test fire sent, status=%d", resp.StatusCode)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// resolveBody replaces #PLC.tagName references in the body template with live values.
func (p *Push) resolveBody() string {
	if p.config.Body == "" {
		return ""
	}

	return tagRefRegex.ReplaceAllStringFunc(p.config.Body, func(match string) string {
		// Remove leading #
		ref := match[1:]
		// Split on first dot: PLC name + tag path
		dotIdx := strings.IndexByte(ref, '.')
		if dotIdx < 0 {
			return match // No dot, leave as-is
		}
		plcName := ref[:dotIdx]
		tagPath := ref[dotIdx+1:]

		value, err := p.reader.ReadTag(plcName, tagPath)
		if err != nil {
			return match // Leave reference as-is on error
		}
		return fmt.Sprintf("%v", value)
	})
}

// buildRequest constructs the HTTP request with headers and auth.
func (p *Push) buildRequest(body string) (*http.Request, error) {
	method := p.config.Method
	if method == "" {
		method = "POST"
	}

	var bodyReader io.Reader
	if body != "" {
		bodyReader = bytes.NewBufferString(body)
	}

	req, err := http.NewRequest(method, p.config.URL, bodyReader)
	if err != nil {
		return nil, err
	}

	// Set Content-Type
	ct := p.config.ContentType
	if ct == "" {
		ct = "application/json"
	}
	if body != "" {
		req.Header.Set("Content-Type", ct)
	}

	// Set custom headers
	for k, v := range p.config.Headers {
		req.Header.Set(k, v)
	}

	// Apply auth
	switch p.config.Auth.Type {
	case config.PushAuthBearer, config.PushAuthJWT:
		req.Header.Set("Authorization", "Bearer "+p.config.Auth.Token)
	case config.PushAuthBasic:
		req.SetBasicAuth(p.config.Auth.Username, p.config.Auth.Password)
	case config.PushAuthCustomHeader:
		if p.config.Auth.HeaderName != "" {
			req.Header.Set(p.config.Auth.HeaderName, p.config.Auth.HeaderValue)
		}
	}

	return req, nil
}

// handleError handles a push error.
func (p *Push) handleError(err error) {
	p.log("error: %v", err)

	p.mu.Lock()
	p.lastErr = err
	p.status = StatusError
	p.mu.Unlock()

	// Transition to waiting clear so we don't spam on persistent errors
	p.mu.Lock()
	p.status = StatusWaitingClear
	p.mu.Unlock()
}

// Reset clears the error state and re-arms the push.
func (p *Push) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.status == StatusError || p.status == StatusWaitingClear || p.status == StatusMinInterval {
		p.status = StatusArmed
		p.lastErr = nil
		for i := range p.condStates {
			p.condStates[i].lastMet = false
			p.condStates[i].inCooldown = false
		}
	}
}
