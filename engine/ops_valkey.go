package engine

import (
	"fmt"

	"warlink/config"
)

// CreateValkey creates a new Valkey server, saves config, and adds to the manager.
func (e *Engine) CreateValkey(req ValkeyCreateRequest) error {
	if req.Name == "" || req.Address == "" {
		return fmt.Errorf("%w: name and address are required", ErrInvalidInput)
	}
	if e.cfg.FindValkey(req.Name) != nil {
		return fmt.Errorf("%w: Valkey server '%s'", ErrAlreadyExists, req.Name)
	}

	valkeyCfg := config.ValkeyConfig{
		Name:            req.Name,
		Address:         req.Address,
		Password:        req.Password,
		Database:        req.Database,
		Selector:        req.Selector,
		KeyTTL:          req.KeyTTL,
		UseTLS:          req.UseTLS,
		PublishChanges:  req.PublishChanges,
		EnableWriteback: req.EnableWriteback,
		Enabled:         req.Enabled,
	}

	e.cfg.Lock()
	e.cfg.AddValkey(valkeyCfg)
	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	pub := e.valkeyMgr.Add(e.cfg.FindValkey(req.Name), e.cfg.Namespace)
	if req.Enabled {
		pub.Start()
	}

	e.emit(EventValkeyCreated, ServiceEvent{Name: req.Name})
	return nil
}

// UpdateValkey updates a Valkey server, saves config, and recreates the publisher.
func (e *Engine) UpdateValkey(name string, req ValkeyUpdateRequest) error {
	existing := e.cfg.FindValkey(name)
	if existing == nil {
		return fmt.Errorf("%w: Valkey server '%s'", ErrNotFound, name)
	}

	// Preserve password if not provided
	password := req.Password
	if password == "" {
		password = existing.Password
	}

	updated := config.ValkeyConfig{
		Name:            name,
		Address:         req.Address,
		Password:        password,
		Database:        req.Database,
		Selector:        req.Selector,
		KeyTTL:          req.KeyTTL,
		UseTLS:          req.UseTLS,
		PublishChanges:  req.PublishChanges,
		EnableWriteback: req.EnableWriteback,
		Enabled:         req.Enabled,
	}

	e.cfg.Lock()
	e.cfg.UpdateValkey(name, updated)
	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	e.valkeyMgr.Remove(name)
	pub := e.valkeyMgr.Add(e.cfg.FindValkey(name), e.cfg.Namespace)
	if req.Enabled {
		pub.Start()
	}

	e.emit(EventValkeyUpdated, ServiceEvent{Name: name})
	return nil
}

// DeleteValkey removes a Valkey server from config and the running manager.
func (e *Engine) DeleteValkey(name string) error {
	e.cfg.Lock()
	if !e.cfg.RemoveValkey(name) {
		e.cfg.Unlock()
		return fmt.Errorf("%w: Valkey server '%s'", ErrNotFound, name)
	}

	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	e.valkeyMgr.Remove(name)

	e.emit(EventValkeyDeleted, ServiceEvent{Name: name})
	return nil
}

// StartValkey starts a Valkey publisher.
func (e *Engine) StartValkey(name string) error {
	pub := e.valkeyMgr.Get(name)
	if pub == nil {
		return fmt.Errorf("%w: Valkey publisher '%s'", ErrNotFound, name)
	}

	if err := pub.Start(); err != nil {
		return err
	}

	e.emit(EventValkeyStarted, ServiceEvent{Name: name})
	return nil
}

// StopValkey stops a Valkey publisher.
func (e *Engine) StopValkey(name string) {
	pub := e.valkeyMgr.Get(name)
	if pub != nil {
		pub.Stop()
	}
	e.emit(EventValkeyStopped, ServiceEvent{Name: name})
}
