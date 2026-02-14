package engine

import "fmt"

// SetNamespace updates the namespace in config and saves.
func (e *Engine) SetNamespace(ns string) error {
	e.cfg.Lock()
	e.cfg.Namespace = ns
	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	e.emit(EventNamespaceChanged, SystemEvent{Detail: ns})
	return nil
}

// ToggleAPI toggles the REST API enabled state. Returns the new state.
func (e *Engine) ToggleAPI() (enabled bool, err error) {
	e.cfg.Lock()
	e.cfg.Web.API.Enabled = !e.cfg.Web.API.Enabled
	enabled = e.cfg.Web.API.Enabled
	if err := e.saveConfig(); err != nil {
		return false, fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}

	e.emit(EventAPIToggled, SystemEvent{Detail: fmt.Sprintf("enabled=%v", enabled)})
	return enabled, nil
}

// SetUITheme updates the UI theme in config and saves.
func (e *Engine) SetUITheme(theme string) error {
	e.cfg.Lock()
	e.cfg.UI.Theme = theme
	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}
	return nil
}

// SetWebHost updates the web server host in config and saves.
func (e *Engine) SetWebHost(host string) error {
	e.cfg.Lock()
	e.cfg.Web.Host = host
	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}
	return nil
}

// SetWebPort updates the web server port in config and saves.
func (e *Engine) SetWebPort(port int) error {
	e.cfg.Lock()
	e.cfg.Web.Port = port
	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}
	return nil
}

// SetWebAPIEnabled sets the web API enabled state in config and saves.
func (e *Engine) SetWebAPIEnabled(enabled bool) error {
	e.cfg.Lock()
	e.cfg.Web.API.Enabled = enabled
	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}
	return nil
}

// SetWebUIEnabled sets the web UI enabled state in config and saves.
func (e *Engine) SetWebUIEnabled(enabled bool) error {
	e.cfg.Lock()
	e.cfg.Web.UI.Enabled = enabled
	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}
	return nil
}

// SetWebEnabled sets the web server enabled state in config and saves.
func (e *Engine) SetWebEnabled(enabled bool) error {
	e.cfg.Lock()
	e.cfg.Web.Enabled = enabled
	if err := e.saveConfig(); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}
	return nil
}

// ForcePublishAll publishes all current tag values to all services.
func (e *Engine) ForcePublishAll() {
	e.forcePublishAllValuesToMQTT()
	e.forcePublishAllValuesToValkey()
	e.forcePublishAllValuesToKafka()
	e.emit(EventForcePublished, SystemEvent{Detail: "all"})
}

// ForcePublishAllToMQTT publishes all current tag values to MQTT brokers.
func (e *Engine) ForcePublishAllToMQTT() {
	e.forcePublishAllValuesToMQTT()
}

// ForcePublishAllToValkey publishes all current tag values to Valkey servers.
func (e *Engine) ForcePublishAllToValkey() {
	e.forcePublishAllValuesToValkey()
}

// ForcePublishAllToKafka publishes all current tag values to Kafka clusters.
func (e *Engine) ForcePublishAllToKafka() {
	e.forcePublishAllValuesToKafka()
}

// ForcePublishTag publishes a single tag's current value to all enabled services.
func (e *Engine) ForcePublishTag(plcName, tagName string) {
	v := e.plcMan.GetTagValueChange(plcName, tagName)
	if v == nil {
		return
	}

	if !v.NoMQTT {
		e.mqttMgr.Publish(v.PLCName, v.TagName, v.Alias, v.Address, v.TypeName, v.Value, true)
	}
	if !v.NoValkey {
		e.valkeyMgr.Publish(v.PLCName, v.TagName, v.Alias, v.Address, v.TypeName, v.Value, v.Writable)
	}
	if !v.NoKafka {
		e.kafkaMgr.Publish(v.PLCName, v.TagName, v.Alias, v.Address, v.TypeName, v.Value, v.Writable, true)
	}
}

// UpdateMQTTPLCNames updates the MQTT manager with current PLC names and refreshes write subscriptions.
func (e *Engine) UpdateMQTTPLCNames() {
	e.updateMQTTPLCNamesInternal()
	e.mqttMgr.UpdateWriteSubscriptions()
}

// forcePublishAllValuesToKafka publishes all current tag values to Kafka clusters.
func (e *Engine) forcePublishAllValuesToKafka() {
	values := e.plcMan.GetAllCurrentValues()
	e.logFn("ForcePublishAllValuesToKafka: publishing %d values", len(values))
	for _, v := range values {
		if !v.NoKafka {
			e.kafkaMgr.Publish(v.PLCName, v.TagName, v.Alias, v.Address, v.TypeName, v.Value, v.Writable, true)
		}
	}
}
