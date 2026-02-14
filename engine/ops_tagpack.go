package engine

import (
	"fmt"

	"warlink/config"
)

// CreateTagPack creates a new TagPack, saves config, and reloads the manager.
func (e *Engine) CreateTagPack(req TagPackCreateRequest) error {
	if req.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	if e.cfg.FindTagPack(req.Name) != nil {
		return fmt.Errorf("%w: TagPack '%s'", ErrAlreadyExists, req.Name)
	}

	packCfg := config.TagPackConfig{
		Name:          req.Name,
		Enabled:       req.Enabled,
		MQTTEnabled:   req.MQTTEnabled,
		KafkaEnabled:  req.KafkaEnabled,
		ValkeyEnabled: req.ValkeyEnabled,
		Members:       req.Members,
	}

	e.cfg.Lock()
	e.cfg.AddTagPack(packCfg)
	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	e.packMgr.Reload()

	e.emit(EventTagPackCreated, TagPackEvent{Name: req.Name})
	return nil
}

// UpdateTagPack updates a TagPack, saves config, and reloads the manager.
func (e *Engine) UpdateTagPack(name string, req TagPackUpdateRequest) error {
	if e.cfg.FindTagPack(name) == nil {
		return fmt.Errorf("%w: TagPack '%s'", ErrNotFound, name)
	}

	updated := config.TagPackConfig{
		Name:          name,
		Enabled:       req.Enabled,
		MQTTEnabled:   req.MQTTEnabled,
		KafkaEnabled:  req.KafkaEnabled,
		ValkeyEnabled: req.ValkeyEnabled,
		Members:       req.Members,
	}

	e.cfg.Lock()
	e.cfg.UpdateTagPack(name, updated)
	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	e.packMgr.Reload()

	e.emit(EventTagPackUpdated, TagPackEvent{Name: name})
	return nil
}

// DeleteTagPack removes a TagPack from config and reloads the manager.
func (e *Engine) DeleteTagPack(name string) error {
	e.cfg.Lock()
	if !e.cfg.RemoveTagPack(name) {
		e.cfg.Unlock()
		return fmt.Errorf("%w: TagPack '%s'", ErrNotFound, name)
	}

	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	e.packMgr.Reload()

	e.emit(EventTagPackDeleted, TagPackEvent{Name: name})
	return nil
}

// ToggleTagPack toggles a TagPack's enabled state. Returns the new state.
func (e *Engine) ToggleTagPack(name string) (enabled bool, err error) {
	e.cfg.Lock()
	pc := e.cfg.FindTagPack(name)
	if pc == nil {
		e.cfg.Unlock()
		return false, fmt.Errorf("%w: TagPack '%s'", ErrNotFound, name)
	}

	pc.Enabled = !pc.Enabled
	enabled = pc.Enabled
	if err := e.saveConfig(); err != nil {
		return false, fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	e.packMgr.Reload()

	e.emit(EventTagPackToggled, TagPackEvent{Name: name})
	return enabled, nil
}

// ToggleTagPackService toggles a specific service on a TagPack. Returns the new state.
func (e *Engine) ToggleTagPackService(name, service string) (enabled bool, err error) {
	e.cfg.Lock()
	pc := e.cfg.FindTagPack(name)
	if pc == nil {
		e.cfg.Unlock()
		return false, fmt.Errorf("%w: TagPack '%s'", ErrNotFound, name)
	}

	switch service {
	case "mqtt":
		pc.MQTTEnabled = !pc.MQTTEnabled
		enabled = pc.MQTTEnabled
	case "kafka":
		pc.KafkaEnabled = !pc.KafkaEnabled
		enabled = pc.KafkaEnabled
	case "valkey":
		pc.ValkeyEnabled = !pc.ValkeyEnabled
		enabled = pc.ValkeyEnabled
	default:
		e.cfg.Unlock()
		return false, fmt.Errorf("%w: invalid service '%s'", ErrInvalidInput, service)
	}

	if err := e.saveConfig(); err != nil {
		return false, fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	e.packMgr.Reload()

	e.emit(EventTagPackServiceToggled, TagPackServiceEvent{Name: name, Service: service, Enabled: enabled})
	return enabled, nil
}

// AddTagPackMember adds a member to a TagPack.
func (e *Engine) AddTagPackMember(name string, member config.TagPackMember) error {
	if member.PLC == "" || member.Tag == "" {
		return fmt.Errorf("%w: PLC and tag are required", ErrInvalidInput)
	}

	e.cfg.Lock()
	pc := e.cfg.FindTagPack(name)
	if pc == nil {
		e.cfg.Unlock()
		return fmt.Errorf("%w: TagPack '%s'", ErrNotFound, name)
	}

	pc.Members = append(pc.Members, member)
	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	e.packMgr.Reload()

	e.emit(EventTagPackMemberAdded, TagPackMemberEvent{PackName: name, Index: len(pc.Members) - 1})
	return nil
}

// RemoveTagPackMember removes a member from a TagPack by index.
func (e *Engine) RemoveTagPackMember(name string, index int) error {
	e.cfg.Lock()
	pc := e.cfg.FindTagPack(name)
	if pc == nil {
		e.cfg.Unlock()
		return fmt.Errorf("%w: TagPack '%s'", ErrNotFound, name)
	}

	if index < 0 || index >= len(pc.Members) {
		e.cfg.Unlock()
		return fmt.Errorf("%w: member index %d out of range", ErrInvalidInput, index)
	}

	pc.Members = append(pc.Members[:index], pc.Members[index+1:]...)
	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	e.packMgr.Reload()

	e.emit(EventTagPackMemberRemoved, TagPackMemberEvent{PackName: name, Index: index})
	return nil
}

// ToggleTagPackMemberIgnore toggles the IgnoreChanges flag on a TagPack member.
// Returns the new ignore state.
func (e *Engine) ToggleTagPackMemberIgnore(name string, index int) (ignoreChanges bool, err error) {
	e.cfg.Lock()
	pc := e.cfg.FindTagPack(name)
	if pc == nil {
		e.cfg.Unlock()
		return false, fmt.Errorf("%w: TagPack '%s'", ErrNotFound, name)
	}

	if index < 0 || index >= len(pc.Members) {
		e.cfg.Unlock()
		return false, fmt.Errorf("%w: member index %d out of range", ErrInvalidInput, index)
	}

	pc.Members[index].IgnoreChanges = !pc.Members[index].IgnoreChanges
	ignoreChanges = pc.Members[index].IgnoreChanges
	if err := e.saveConfig(); err != nil {
		return false, fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	e.packMgr.Reload()

	e.emit(EventTagPackMemberIgnoreToggled, TagPackMemberEvent{PackName: name, Index: index})
	return ignoreChanges, nil
}
