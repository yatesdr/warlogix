package engine

import (
	"fmt"

	"warlink/config"
)

// CreateKafka creates a new Kafka cluster, saves config, and adds to the manager.
func (e *Engine) CreateKafka(req KafkaCreateRequest) error {
	if req.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	if len(req.Brokers) == 0 {
		return fmt.Errorf("%w: at least one broker is required", ErrInvalidInput)
	}
	if e.cfg.FindKafka(req.Name) != nil {
		return fmt.Errorf("%w: Kafka cluster '%s'", ErrAlreadyExists, req.Name)
	}

	autoCreate := req.AutoCreateTopics
	autoCreatePtr := &autoCreate

	kafkaCfg := config.KafkaConfig{
		Name:             req.Name,
		Brokers:          req.Brokers,
		UseTLS:           req.UseTLS,
		TLSSkipVerify:    req.TLSSkipVerify,
		SASLMechanism:    req.SASLMechanism,
		Username:         req.Username,
		Password:         req.Password,
		Selector:         req.Selector,
		PublishChanges:   req.PublishChanges,
		EnableWriteback:  req.EnableWriteback,
		AutoCreateTopics: autoCreatePtr,
		Enabled:          req.Enabled,
		RequiredAcks:     req.RequiredAcks,
		MaxRetries:       req.MaxRetries,
		RetryBackoff:     req.RetryBackoff,
		ConsumerGroup:    req.ConsumerGroup,
		WriteMaxAge:      req.WriteMaxAge,
	}

	e.cfg.Lock()
	e.cfg.AddKafka(kafkaCfg)
	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	kc := e.cfg.FindKafka(req.Name)
	e.kafkaMgr.AddCluster(buildKafkaRuntimeConfig(kc), e.cfg.Namespace)

	if req.Enabled {
		e.kafkaMgr.Connect(req.Name)
	}

	e.emit(EventKafkaCreated, ServiceEvent{Name: req.Name})
	return nil
}

// UpdateKafka updates a Kafka cluster, saves config, and recreates the producer.
func (e *Engine) UpdateKafka(name string, req KafkaUpdateRequest) error {
	if len(req.Brokers) == 0 {
		return fmt.Errorf("%w: at least one broker is required", ErrInvalidInput)
	}
	if e.cfg.FindKafka(name) == nil {
		return fmt.Errorf("%w: Kafka cluster '%s'", ErrNotFound, name)
	}

	// Preserve password if not provided
	password := req.Password
	if password == "" {
		if existing := e.cfg.FindKafka(name); existing != nil {
			password = existing.Password
		}
	}

	autoCreate := req.AutoCreateTopics
	autoCreatePtr := &autoCreate

	updated := config.KafkaConfig{
		Name:             name,
		Brokers:          req.Brokers,
		UseTLS:           req.UseTLS,
		TLSSkipVerify:    req.TLSSkipVerify,
		SASLMechanism:    req.SASLMechanism,
		Username:         req.Username,
		Password:         password,
		Selector:         req.Selector,
		PublishChanges:   req.PublishChanges,
		EnableWriteback:  req.EnableWriteback,
		AutoCreateTopics: autoCreatePtr,
		Enabled:          req.Enabled,
		RequiredAcks:     req.RequiredAcks,
		MaxRetries:       req.MaxRetries,
		RetryBackoff:     req.RetryBackoff,
		ConsumerGroup:    req.ConsumerGroup,
		WriteMaxAge:      req.WriteMaxAge,
	}

	e.cfg.Lock()
	e.cfg.UpdateKafka(name, updated)
	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	e.kafkaMgr.RemoveCluster(name)
	kc := e.cfg.FindKafka(name)
	e.kafkaMgr.AddCluster(buildKafkaRuntimeConfig(kc), e.cfg.Namespace)

	if req.Enabled {
		e.kafkaMgr.Connect(name)
	}

	e.emit(EventKafkaUpdated, ServiceEvent{Name: name})
	return nil
}

// DeleteKafka removes a Kafka cluster from config and the running manager.
func (e *Engine) DeleteKafka(name string) error {
	e.cfg.Lock()
	if !e.cfg.RemoveKafka(name) {
		e.cfg.Unlock()
		return fmt.Errorf("%w: Kafka cluster '%s'", ErrNotFound, name)
	}

	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	e.kafkaMgr.RemoveCluster(name)

	e.emit(EventKafkaDeleted, ServiceEvent{Name: name})
	return nil
}

// ConnectKafka connects a Kafka cluster.
func (e *Engine) ConnectKafka(name string) error {
	if err := e.kafkaMgr.Connect(name); err != nil {
		return err
	}
	e.emit(EventKafkaConnected, ServiceEvent{Name: name})
	return nil
}

// DisconnectKafka disconnects a Kafka cluster.
func (e *Engine) DisconnectKafka(name string) {
	e.kafkaMgr.Disconnect(name)
	e.emit(EventKafkaDisconnected, ServiceEvent{Name: name})
}
