package engine

import (
	"fmt"

	"warlink/config"
)

// CreateRule creates a new rule, saves config, and adds to the manager.
func (e *Engine) CreateRule(req RuleCreateRequest) error {
	if req.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	if len(req.Conditions) == 0 {
		return fmt.Errorf("%w: at least one condition is required", ErrInvalidInput)
	}
	if e.cfg.FindRule(req.Name) != nil {
		return fmt.Errorf("%w: rule '%s'", ErrAlreadyExists, req.Name)
	}

	ruleCfg := config.RuleConfig{
		Name:           req.Name,
		Enabled:        req.Enabled,
		Conditions:     req.Conditions,
		LogicMode:      req.LogicMode,
		DebounceMS:     req.DebounceMS,
		CooldownMS:     req.CooldownMS,
		Actions:        req.Actions,
		ClearedActions: req.ClearedActions,
	}

	e.cfg.Lock()
	e.cfg.AddRule(ruleCfg)
	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	if e.ruleMgr != nil {
		e.ruleMgr.AddRule(e.cfg.FindRule(req.Name))
		if req.Enabled {
			e.ruleMgr.StartRule(req.Name)
		}
	}

	e.emit(EventRuleCreated, RuleEvent{Name: req.Name})
	return nil
}

// UpdateRule updates a rule, saves config, and updates the runtime.
func (e *Engine) UpdateRule(name string, req RuleUpdateRequest) error {
	if e.cfg.FindRule(name) == nil {
		return fmt.Errorf("%w: rule '%s'", ErrNotFound, name)
	}

	updated := config.RuleConfig{
		Name:           name,
		Enabled:        req.Enabled,
		Conditions:     req.Conditions,
		LogicMode:      req.LogicMode,
		DebounceMS:     req.DebounceMS,
		CooldownMS:     req.CooldownMS,
		Actions:        req.Actions,
		ClearedActions: req.ClearedActions,
	}

	e.cfg.Lock()
	e.cfg.UpdateRule(name, updated)
	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	if e.ruleMgr != nil {
		e.ruleMgr.UpdateRule(e.cfg.FindRule(name))
		if req.Enabled {
			e.ruleMgr.StartRule(name)
		}
	}

	e.emit(EventRuleUpdated, RuleEvent{Name: name})
	return nil
}

// DeleteRule removes a rule from config and the running manager.
func (e *Engine) DeleteRule(name string) error {
	e.cfg.Lock()
	if !e.cfg.RemoveRule(name) {
		e.cfg.Unlock()
		return fmt.Errorf("%w: rule '%s'", ErrNotFound, name)
	}

	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	if e.ruleMgr != nil {
		e.ruleMgr.RemoveRule(name)
	}

	e.emit(EventRuleDeleted, RuleEvent{Name: name})
	return nil
}

// StartRule starts a rule.
func (e *Engine) StartRule(name string) error {
	if e.ruleMgr == nil {
		return fmt.Errorf("rule manager not available")
	}
	if err := e.ruleMgr.StartRule(name); err != nil {
		return err
	}
	e.emit(EventRuleStarted, RuleEvent{Name: name})
	return nil
}

// StopRule stops a rule.
func (e *Engine) StopRule(name string) {
	if e.ruleMgr != nil {
		e.ruleMgr.StopRule(name)
	}
	e.emit(EventRuleStopped, RuleEvent{Name: name})
}

// TestFireRule fires a rule in test mode.
func (e *Engine) TestFireRule(name string) error {
	if e.ruleMgr == nil {
		return fmt.Errorf("rule manager not available")
	}
	if err := e.ruleMgr.TestFireRule(name); err != nil {
		return err
	}
	e.emit(EventRuleTestFired, RuleEvent{Name: name})
	return nil
}
