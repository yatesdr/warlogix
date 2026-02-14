package engine

import (
	"fmt"

	"warlink/config"
)

// CreatePush creates a new push webhook, saves config, and adds to the manager.
func (e *Engine) CreatePush(req PushCreateRequest) error {
	if req.Name == "" || req.URL == "" {
		return fmt.Errorf("%w: name and URL are required", ErrInvalidInput)
	}
	if e.cfg.FindPush(req.Name) != nil {
		return fmt.Errorf("%w: push '%s'", ErrAlreadyExists, req.Name)
	}

	method := req.Method
	if method == "" {
		method = "POST"
	}
	conditions := req.Conditions
	if conditions == nil {
		conditions = []config.PushCondition{}
	}

	pushCfg := config.PushConfig{
		Name:            req.Name,
		Enabled:         req.Enabled,
		Conditions:      conditions,
		URL:             req.URL,
		Method:          method,
		ContentType:     req.ContentType,
		Headers:         req.Headers,
		Body:            req.Body,
		Auth:            req.Auth,
		CooldownMin:     req.CooldownMin,
		CooldownPerCond: req.CooldownPerCond,
		Timeout:         req.Timeout,
	}

	e.cfg.Lock()
	e.cfg.AddPush(pushCfg)
	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	if e.pushMgr != nil {
		pc := e.cfg.FindPush(req.Name)
		if pc != nil {
			e.pushMgr.AddPush(pc)
			if req.Enabled {
				e.pushMgr.StartPush(req.Name)
			}
		}
	}

	e.emit(EventPushCreated, PushEvent{Name: req.Name})
	return nil
}

// UpdatePush updates a push webhook, saves config, and updates the runtime.
func (e *Engine) UpdatePush(name string, req PushUpdateRequest) error {
	if e.cfg.FindPush(name) == nil {
		return fmt.Errorf("%w: push '%s'", ErrNotFound, name)
	}

	method := req.Method
	if method == "" {
		method = "POST"
	}
	conditions := req.Conditions
	if conditions == nil {
		conditions = []config.PushCondition{}
	}

	updated := config.PushConfig{
		Name:            name,
		Enabled:         req.Enabled,
		Conditions:      conditions,
		URL:             req.URL,
		Method:          method,
		ContentType:     req.ContentType,
		Headers:         req.Headers,
		Body:            req.Body,
		Auth:            req.Auth,
		CooldownMin:     req.CooldownMin,
		CooldownPerCond: req.CooldownPerCond,
		Timeout:         req.Timeout,
	}

	e.cfg.Lock()
	e.cfg.UpdatePush(name, updated)
	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	if e.pushMgr != nil {
		pc := e.cfg.FindPush(name)
		if pc != nil {
			e.pushMgr.UpdatePush(pc)
			if req.Enabled {
				e.pushMgr.StartPush(name)
			}
		}
	}

	e.emit(EventPushUpdated, PushEvent{Name: name})
	return nil
}

// DeletePush removes a push from config and the running manager.
func (e *Engine) DeletePush(name string) error {
	e.cfg.Lock()
	if !e.cfg.RemovePush(name) {
		e.cfg.Unlock()
		return fmt.Errorf("%w: push '%s'", ErrNotFound, name)
	}

	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	if e.pushMgr != nil {
		e.pushMgr.RemovePush(name)
	}

	e.emit(EventPushDeleted, PushEvent{Name: name})
	return nil
}

// StartPush starts a push webhook.
func (e *Engine) StartPush(name string) error {
	if e.pushMgr == nil {
		return fmt.Errorf("push manager not available")
	}
	if err := e.pushMgr.StartPush(name); err != nil {
		return err
	}
	e.emit(EventPushStarted, PushEvent{Name: name})
	return nil
}

// StopPush stops a push webhook.
func (e *Engine) StopPush(name string) {
	if e.pushMgr != nil {
		e.pushMgr.StopPush(name)
	}
	e.emit(EventPushStopped, PushEvent{Name: name})
}

// TestFirePush fires a push webhook in test mode.
func (e *Engine) TestFirePush(name string) error {
	if e.pushMgr == nil {
		return fmt.Errorf("push manager not available")
	}
	if err := e.pushMgr.TestFirePush(name); err != nil {
		return err
	}
	e.emit(EventPushTestFired, PushEvent{Name: name})
	return nil
}
