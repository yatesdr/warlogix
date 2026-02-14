package engine

import (
	"fmt"

	"warlink/config"
)

// CreateTrigger creates a new trigger, saves config, and adds to the manager.
func (e *Engine) CreateTrigger(req TriggerCreateRequest) error {
	if req.Name == "" || req.PLC == "" || req.TriggerTag == "" {
		return fmt.Errorf("%w: name, PLC, and trigger tag are required", ErrInvalidInput)
	}
	if e.cfg.FindTrigger(req.Name) != nil {
		return fmt.Errorf("%w: trigger '%s'", ErrAlreadyExists, req.Name)
	}

	triggerCfg := config.TriggerConfig{
		Name:         req.Name,
		Enabled:      req.Enabled,
		PLC:          req.PLC,
		TriggerTag:   req.TriggerTag,
		Condition:    req.Condition,
		AckTag:       req.AckTag,
		DebounceMS:   req.DebounceMS,
		Tags:         req.Tags,
		MQTTBroker:   req.MQTTBroker,
		KafkaCluster: req.KafkaCluster,
		Selector:     req.Selector,
		Metadata:     req.Metadata,
		PublishPack:  req.PublishPack,
	}

	e.cfg.Lock()
	e.cfg.AddTrigger(triggerCfg)
	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	e.triggerMgr.AddTrigger(e.cfg.FindTrigger(req.Name))
	if req.Enabled {
		e.triggerMgr.StartTrigger(req.Name)
	}

	e.emit(EventTriggerCreated, TriggerEvent{Name: req.Name})
	return nil
}

// UpdateTrigger updates a trigger, saves config, and updates the runtime.
func (e *Engine) UpdateTrigger(name string, req TriggerUpdateRequest) error {
	if e.cfg.FindTrigger(name) == nil {
		return fmt.Errorf("%w: trigger '%s'", ErrNotFound, name)
	}

	updated := config.TriggerConfig{
		Name:         name,
		Enabled:      req.Enabled,
		PLC:          req.PLC,
		TriggerTag:   req.TriggerTag,
		Condition:    req.Condition,
		AckTag:       req.AckTag,
		DebounceMS:   req.DebounceMS,
		Tags:         req.Tags,
		MQTTBroker:   req.MQTTBroker,
		KafkaCluster: req.KafkaCluster,
		Selector:     req.Selector,
		Metadata:     req.Metadata,
		PublishPack:  req.PublishPack,
	}

	e.cfg.Lock()
	e.cfg.UpdateTrigger(name, updated)
	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	e.triggerMgr.UpdateTrigger(e.cfg.FindTrigger(name))
	if req.Enabled {
		e.triggerMgr.StartTrigger(name)
	}

	e.emit(EventTriggerUpdated, TriggerEvent{Name: name})
	return nil
}

// DeleteTrigger removes a trigger from config and the running manager.
func (e *Engine) DeleteTrigger(name string) error {
	e.cfg.Lock()
	if !e.cfg.RemoveTrigger(name) {
		e.cfg.Unlock()
		return fmt.Errorf("%w: trigger '%s'", ErrNotFound, name)
	}

	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	e.triggerMgr.RemoveTrigger(name)

	e.emit(EventTriggerDeleted, TriggerEvent{Name: name})
	return nil
}

// StartTrigger starts a trigger.
func (e *Engine) StartTrigger(name string) error {
	if err := e.triggerMgr.StartTrigger(name); err != nil {
		return err
	}
	e.emit(EventTriggerStarted, TriggerEvent{Name: name})
	return nil
}

// StopTrigger stops a trigger.
func (e *Engine) StopTrigger(name string) {
	e.triggerMgr.StopTrigger(name)
	e.emit(EventTriggerStopped, TriggerEvent{Name: name})
}

// TestFireTrigger fires a trigger in test mode.
func (e *Engine) TestFireTrigger(name string) error {
	if err := e.triggerMgr.TestFireTrigger(name); err != nil {
		return err
	}
	e.emit(EventTriggerTestFired, TriggerEvent{Name: name})
	return nil
}

// AddTriggerTag adds a tag to a trigger's tag list.
func (e *Engine) AddTriggerTag(name, tag string) error {
	if tag == "" {
		return fmt.Errorf("%w: tag is required", ErrInvalidInput)
	}

	e.cfg.Lock()
	tc := e.cfg.FindTrigger(name)
	if tc == nil {
		e.cfg.Unlock()
		return fmt.Errorf("%w: trigger '%s'", ErrNotFound, name)
	}

	for _, t := range tc.Tags {
		if t == tag {
			e.cfg.Unlock()
			return fmt.Errorf("%w: tag '%s' in trigger '%s'", ErrAlreadyExists, tag, name)
		}
	}

	tc.Tags = append(tc.Tags, tag)
	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	e.triggerMgr.UpdateTrigger(tc)

	e.emit(EventTriggerTagAdded, TriggerTagEvent{TriggerName: name, Tag: tag})
	return nil
}

// RemoveTriggerTag removes a tag from a trigger by index.
func (e *Engine) RemoveTriggerTag(name string, index int) error {
	e.cfg.Lock()
	tc := e.cfg.FindTrigger(name)
	if tc == nil {
		e.cfg.Unlock()
		return fmt.Errorf("%w: trigger '%s'", ErrNotFound, name)
	}

	if index < 0 || index >= len(tc.Tags) {
		e.cfg.Unlock()
		return fmt.Errorf("%w: tag index %d out of range", ErrInvalidInput, index)
	}

	tag := tc.Tags[index]
	tc.Tags = append(tc.Tags[:index], tc.Tags[index+1:]...)
	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	e.triggerMgr.UpdateTrigger(tc)

	e.emit(EventTriggerTagRemoved, TriggerTagEvent{TriggerName: name, Tag: tag, Index: index})
	return nil
}
