package engine

import (
	"fmt"

	"warlink/config"
)

// ConnectPLC connects a PLC and persists the enabled state.
func (e *Engine) ConnectPLC(name string) error {
	// Restore auto-connect (may have been cleared by manual disconnect)
	if plc := e.plcMan.GetPLC(name); plc != nil {
		plc.Config.Enabled = true
	}

	// Persist enabled state
	e.cfg.Lock()
	if plcCfg := e.cfg.FindPLC(name); plcCfg != nil {
		plcCfg.Enabled = true
		e.saveConfig()
	} else {
		e.cfg.Unlock()
	}

	if err := e.plcMan.Connect(name); err != nil {
		return err
	}

	e.emit(EventPLCConnected, PLCEvent{Name: name})
	return nil
}

// DisconnectPLC disconnects a PLC and persists the disabled state.
func (e *Engine) DisconnectPLC(name string) {
	// Clear auto-connect so plcman won't auto-reconnect
	if plc := e.plcMan.GetPLC(name); plc != nil {
		plc.Config.Enabled = false
	}

	// Persist disabled state
	e.cfg.Lock()
	if plcCfg := e.cfg.FindPLC(name); plcCfg != nil {
		plcCfg.Enabled = false
		e.saveConfig()
	} else {
		e.cfg.Unlock()
	}

	e.plcMan.Disconnect(name)
	e.emit(EventPLCDisconnected, PLCEvent{Name: name})
}

// CreatePLC creates a new PLC, saves config, and adds it to the manager.
func (e *Engine) CreatePLC(req PLCCreateRequest) error {
	if req.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	if req.Address == "" {
		return fmt.Errorf("%w: address is required", ErrInvalidInput)
	}
	if e.cfg.FindPLC(req.Name) != nil {
		return fmt.Errorf("%w: PLC '%s'", ErrAlreadyExists, req.Name)
	}

	plcCfg := config.PLCConfig{
		Name:               req.Name,
		Address:            req.Address,
		Slot:               req.Slot,
		Enabled:            req.Enabled,
		HealthCheckEnabled: req.HealthCheckEnabled,
		DiscoverTags:       req.DiscoverTags,
		Family:             req.Family,
		PollRate:           req.PollRate,
		Timeout:            req.Timeout,
		AmsNetId:           req.AmsNetId,
		AmsPort:            req.AmsPort,
		Protocol:           req.Protocol,
		FinsPort:           req.FinsPort,
		FinsNetwork:        req.FinsNetwork,
		FinsNode:           req.FinsNode,
		FinsUnit:           req.FinsUnit,
	}

	e.cfg.Lock()
	e.cfg.PLCs = append(e.cfg.PLCs, plcCfg)
	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	// Add to running manager
	if err := e.plcMan.AddPLC(&e.cfg.PLCs[len(e.cfg.PLCs)-1]); err != nil {
		return fmt.Errorf("PLC created but failed to add to manager: %w", err)
	}

	e.updateMQTTPLCNamesInternal()
	e.emit(EventPLCCreated, PLCEvent{Name: req.Name})
	return nil
}

// UpdatePLC updates a PLC's configuration and saves.
func (e *Engine) UpdatePLC(name string, req PLCUpdateRequest) error {
	e.cfg.Lock()
	plcCfg := e.cfg.FindPLC(name)
	if plcCfg == nil {
		e.cfg.Unlock()
		return fmt.Errorf("%w: PLC '%s'", ErrNotFound, name)
	}

	plcCfg.Address = req.Address
	plcCfg.Slot = req.Slot
	plcCfg.Enabled = req.Enabled
	plcCfg.HealthCheckEnabled = req.HealthCheckEnabled
	plcCfg.Family = req.Family
	plcCfg.PollRate = req.PollRate
	plcCfg.Timeout = req.Timeout
	plcCfg.AmsNetId = req.AmsNetId
	plcCfg.AmsPort = req.AmsPort
	plcCfg.Protocol = req.Protocol
	plcCfg.FinsPort = req.FinsPort
	plcCfg.FinsNetwork = req.FinsNetwork
	plcCfg.FinsNode = req.FinsNode
	plcCfg.FinsUnit = req.FinsUnit
	plcCfg.DiscoverTags = req.DiscoverTags

	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	e.emit(EventPLCUpdated, PLCEvent{Name: name})
	return nil
}

// ReconnectPLC disconnects, removes, re-adds, and optionally reconnects a PLC.
// Used after UpdatePLC to apply runtime config changes (address, family, etc.).
func (e *Engine) ReconnectPLC(name string) error {
	e.plcMan.Disconnect(name)
	_ = e.plcMan.RemovePLC(name)

	plcCfg := e.cfg.FindPLC(name)
	if plcCfg == nil {
		return fmt.Errorf("%w: PLC '%s'", ErrNotFound, name)
	}

	if err := e.plcMan.AddPLC(plcCfg); err != nil {
		return fmt.Errorf("failed to re-add PLC to manager: %w", err)
	}

	if plcCfg.Enabled {
		if err := e.plcMan.Connect(name); err != nil {
			return err
		}
	}

	return nil
}

// DeletePLC removes a PLC from config and the running manager.
func (e *Engine) DeletePLC(name string) error {
	e.cfg.Lock()
	found := false
	newPLCs := make([]config.PLCConfig, 0, len(e.cfg.PLCs))
	for _, plc := range e.cfg.PLCs {
		if plc.Name == name {
			found = true
		} else {
			newPLCs = append(newPLCs, plc)
		}
	}

	if !found {
		e.cfg.Unlock()
		return fmt.Errorf("%w: PLC '%s'", ErrNotFound, name)
	}

	_ = e.plcMan.RemovePLC(name)
	e.cfg.PLCs = newPLCs

	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	e.updateMQTTPLCNamesInternal()
	e.emit(EventPLCDeleted, PLCEvent{Name: name})
	return nil
}
