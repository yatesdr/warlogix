package engine

import (
	"fmt"

	"warlink/config"
	"warlink/mqtt"
)

// CreateMQTT creates a new MQTT broker, saves config, and adds to the manager.
func (e *Engine) CreateMQTT(req MQTTCreateRequest) error {
	if req.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	if req.Broker == "" {
		return fmt.Errorf("%w: broker address is required", ErrInvalidInput)
	}
	if e.cfg.FindMQTT(req.Name) != nil {
		return fmt.Errorf("%w: MQTT broker '%s'", ErrAlreadyExists, req.Name)
	}

	if req.Port == 0 {
		req.Port = 1883
	}

	mqttCfg := config.MQTTConfig{
		Name:     req.Name,
		Broker:   req.Broker,
		Port:     req.Port,
		ClientID: req.ClientID,
		Username: req.Username,
		Password: req.Password,
		Selector: req.Selector,
		UseTLS:   req.UseTLS,
		Enabled:  req.Enabled,
	}

	e.cfg.Lock()
	e.cfg.AddMQTT(mqttCfg)
	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	pub := mqtt.NewPublisher(e.cfg.FindMQTT(req.Name), e.cfg.Namespace)
	e.mqttMgr.Add(pub)

	if req.Enabled {
		pub.Start()
	}

	e.emit(EventMQTTCreated, ServiceEvent{Name: req.Name})
	return nil
}

// UpdateMQTT updates an MQTT broker, saves config, and recreates the publisher.
func (e *Engine) UpdateMQTT(name string, req MQTTUpdateRequest) error {
	existing := e.cfg.FindMQTT(name)
	if existing == nil {
		return fmt.Errorf("%w: MQTT broker '%s'", ErrNotFound, name)
	}

	if req.Port == 0 {
		req.Port = 1883
	}

	// Preserve password if not provided
	password := req.Password
	if password == "" {
		password = existing.Password
	}

	updated := config.MQTTConfig{
		Name:     name,
		Broker:   req.Broker,
		Port:     req.Port,
		ClientID: req.ClientID,
		Username: req.Username,
		Password: password,
		Selector: req.Selector,
		UseTLS:   req.UseTLS,
		Enabled:  req.Enabled,
	}

	e.cfg.Lock()
	e.cfg.UpdateMQTT(name, updated)
	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	// Recreate publisher with new config
	e.mqttMgr.Remove(name)
	pub := mqtt.NewPublisher(e.cfg.FindMQTT(name), e.cfg.Namespace)
	e.mqttMgr.Add(pub)
	if req.Enabled {
		pub.Start()
	}

	e.emit(EventMQTTUpdated, ServiceEvent{Name: name})
	return nil
}

// DeleteMQTT removes an MQTT broker from config and the running manager.
func (e *Engine) DeleteMQTT(name string) error {
	e.cfg.Lock()
	if !e.cfg.RemoveMQTT(name) {
		e.cfg.Unlock()
		return fmt.Errorf("%w: MQTT broker '%s'", ErrNotFound, name)
	}

	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	e.mqttMgr.Remove(name)

	e.emit(EventMQTTDeleted, ServiceEvent{Name: name})
	return nil
}

// StartMQTT starts an MQTT publisher.
func (e *Engine) StartMQTT(name string) error {
	pub := e.mqttMgr.Get(name)
	if pub == nil {
		return fmt.Errorf("%w: MQTT publisher '%s'", ErrNotFound, name)
	}

	if err := pub.Start(); err != nil {
		return err
	}

	e.emit(EventMQTTStarted, ServiceEvent{Name: name})
	return nil
}

// StopMQTT stops an MQTT publisher.
func (e *Engine) StopMQTT(name string) {
	pub := e.mqttMgr.Get(name)
	if pub != nil {
		pub.Stop()
	}
	e.emit(EventMQTTStopped, ServiceEvent{Name: name})
}
