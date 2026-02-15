package engine

import (
	"time"

	"warlink/config"
	"warlink/kafka"
	"warlink/logging"
	"warlink/mqtt"
	"warlink/plcman"
	"warlink/tagpack"
	"warlink/valkey"
	"warlink/warcry"
)

// plcDataProvider implements tagpack.PLCDataProvider using the PLC manager.
type plcDataProvider struct {
	manager *plcman.Manager
}

func (p *plcDataProvider) GetTagValue(plcName, tagName string) (value interface{}, typeName, alias string, ok bool) {
	vc := p.manager.GetTagValueChange(plcName, tagName)
	if vc == nil {
		return nil, "", "", false
	}
	return vc.Value, vc.TypeName, vc.Alias, true
}

func (p *plcDataProvider) GetPLCMetadata(plcName string) tagpack.PLCMetadata {
	plc := p.manager.GetPLC(plcName)
	if plc == nil {
		return tagpack.PLCMetadata{}
	}

	meta := tagpack.PLCMetadata{
		Address:   plc.Config.Address,
		Family:    string(plc.Config.GetFamily()),
		Connected: plc.GetStatus() == plcman.StatusConnected,
	}

	if err := plc.GetError(); err != nil {
		meta.Error = err.Error()
	}

	return meta
}

// setupValueChangeHandlers sets up the value change callback for publishing to MQTT, Valkey, Kafka, and Warcry.
func setupValueChangeHandlers(manager *plcman.Manager, mqttMgr *mqtt.Manager, valkeyMgr *valkey.Manager, kafkaMgr *kafka.Manager, packMgr *tagpack.Manager, warcryMgr *warcry.Server) {
	manager.SetOnValueChange(func(changes []plcman.ValueChange) {
		mqttRunning := mqttMgr.AnyRunning()
		valkeyRunning := valkeyMgr.AnyRunning()
		kafkaPublishing := kafkaMgr.AnyPublishing()

		logging.DebugLog("engine", "OnValueChange: %d changes, MQTT: %v, Valkey: %v, Kafka: %v",
			len(changes), mqttRunning, valkeyRunning, kafkaPublishing)

		changesCopy := make([]plcman.ValueChange, len(changes))
		copy(changesCopy, changes)

		changesByPLC := make(map[string][]string)
		for _, c := range changesCopy {
			changesByPLC[c.PLCName] = append(changesByPLC[c.PLCName], c.TagName)
		}
		for plcName, tags := range changesByPLC {
			packMgr.OnTagChanges(plcName, tags)
		}

		if !mqttRunning && !valkeyRunning && !kafkaPublishing {
			return
		}

		if mqttRunning {
			go func() {
				for _, c := range changesCopy {
					if !c.NoMQTT {
						mqttMgr.Publish(c.PLCName, c.TagName, c.Alias, c.Address, c.TypeName, c.Value, true)
					}
				}
			}()
		}

		if valkeyRunning {
			go func() {
				for _, c := range changesCopy {
					if !c.NoValkey {
						valkeyMgr.Publish(c.PLCName, c.TagName, c.Alias, c.Address, c.TypeName, c.Value, c.Writable)
					}
				}
			}()
		}

		if kafkaPublishing {
			go func() {
				for _, c := range changesCopy {
					if !c.NoKafka {
						kafkaMgr.Publish(c.PLCName, c.TagName, c.Alias, c.Address, c.TypeName, c.Value, c.Writable, true)
					}
				}
			}()
		}

		if warcryMgr != nil && warcryMgr.HasClients() {
			go func() {
				for _, c := range changesCopy {
					warcryMgr.BroadcastTag(c.PLCName, c.TagName, c.Alias, c.Address, c.TypeName, c.Value, c.Writable)
				}
			}()
		}
	})
}

// setupWriteHandlers sets up MQTT, Valkey, and Kafka write handling.
func setupWriteHandlers(cfg *config.Config, manager *plcman.Manager, mqttMgr *mqtt.Manager, valkeyMgr *valkey.Manager, kafkaMgr *kafka.Manager) {
	writeHandler := func(plcName, tagName string, value interface{}) error {
		return manager.WriteTag(plcName, tagName, value)
	}

	writeValidator := func(plcName, tagName string) bool {
		plcCfg := cfg.FindPLC(plcName)
		if plcCfg == nil {
			return false
		}
		for _, tag := range plcCfg.Tags {
			if tag.Name == tagName && tag.Writable {
				return true
			}
		}
		return false
	}

	tagTypeLookup := func(plcName, tagName string) uint16 {
		return manager.GetTagType(plcName, tagName)
	}

	mqttMgr.SetWriteHandler(writeHandler)
	mqttMgr.SetWriteValidator(writeValidator)
	mqttMgr.SetTagTypeLookup(tagTypeLookup)

	valkeyMgr.SetWriteHandler(writeHandler)
	valkeyMgr.SetWriteValidator(writeValidator)
	valkeyMgr.SetTagTypeLookup(tagTypeLookup)

	kafkaMgr.SetWriteHandler(writeHandler)
	kafkaMgr.SetWriteValidator(writeValidator)
	kafkaMgr.SetTagTypeLookup(tagTypeLookup)
}

// forcePublishAllValuesToMQTT publishes all current tag values to MQTT brokers.
func (e *Engine) forcePublishAllValuesToMQTT() {
	values := e.plcMan.GetAllCurrentValues()
	e.logFn("ForcePublishAllValues: publishing %d values to MQTT", len(values))
	for _, v := range values {
		if !v.NoMQTT {
			e.mqttMgr.Publish(v.PLCName, v.TagName, v.Alias, v.Address, v.TypeName, v.Value, true)
		}
	}
}

// forcePublishAllValuesToValkey publishes all current tag values to Valkey servers.
func (e *Engine) forcePublishAllValuesToValkey() {
	values := e.plcMan.GetAllCurrentValues()
	e.logFn("ForcePublishAllValuesToValkey: publishing %d values", len(values))
	for _, v := range values {
		if !v.NoValkey {
			e.valkeyMgr.Publish(v.PLCName, v.TagName, v.Alias, v.Address, v.TypeName, v.Value, v.Writable)
		}
	}
}

// publishHealthLoop publishes PLC health status to all services every 10 seconds.
func (e *Engine) publishHealthLoop() {
	time.Sleep(2 * time.Second)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	e.publishAllHealth()

	for {
		select {
		case <-e.stopChan:
			return
		case <-ticker.C:
			e.publishAllHealth()
		}
	}
}

// publishAllHealth publishes health status for all PLCs to MQTT, Valkey, and Kafka.
func (e *Engine) publishAllHealth() {
	plcs := e.plcMan.ListPLCs()
	e.logFn("Publishing health for %d PLCs", len(plcs))
	for _, plc := range plcs {
		if !plc.Config.IsHealthCheckEnabled() {
			continue
		}

		health := plc.GetHealthStatus()

		if e.mqttMgr != nil {
			e.mqttMgr.PublishHealth(plc.Config.Name, health.Driver, health.Online, health.Status, health.Error)
		}
		if e.valkeyMgr != nil {
			e.valkeyMgr.PublishHealth(plc.Config.Name, health.Driver, health.Online, health.Status, health.Error)
		}
		if e.kafkaMgr != nil {
			e.kafkaMgr.PublishHealth(plc.Config.Name, health.Driver, health.Online, health.Status, health.Error)
		}
		if e.warcryMgr != nil && e.warcryMgr.HasClients() {
			e.warcryMgr.BroadcastHealth(plc.Config.Name, health.Driver, health.Online, health.Status, health.Error)
		}
	}
}

// updateMQTTPLCNamesInternal updates the MQTT manager with current PLC names.
func (e *Engine) updateMQTTPLCNamesInternal() {
	plcNames := make([]string, len(e.cfg.PLCs))
	for i, plc := range e.cfg.PLCs {
		plcNames[i] = plc.Name
	}
	e.mqttMgr.SetPLCNames(plcNames)
}

// warcryPLCAdapter implements warcry.PLCProvider using the PLC manager.
type warcryPLCAdapter struct {
	mgr *plcman.Manager
}

func (a warcryPLCAdapter) GetAllCurrentValues() []warcry.TagSnapshot {
	values := a.mgr.GetAllCurrentValues()
	result := make([]warcry.TagSnapshot, len(values))
	for i, v := range values {
		result[i] = warcry.TagSnapshot{
			PLCName: v.PLCName, TagName: v.TagName, Alias: v.Alias,
			Address: v.Address, TypeName: v.TypeName, Value: v.Value, Writable: v.Writable,
		}
	}
	return result
}

func (a warcryPLCAdapter) ListPLCNames() []string {
	plcs := a.mgr.ListPLCs()
	names := make([]string, len(plcs))
	for i, p := range plcs {
		names[i] = p.Config.Name
	}
	return names
}

// warcryPackAdapter implements warcry.PackProvider using the tagpack manager.
type warcryPackAdapter struct {
	mgr *tagpack.Manager
}

func (a warcryPackAdapter) ListPacks() []warcry.PackInfo {
	packs := a.mgr.ListPacks()
	result := make([]warcry.PackInfo, len(packs))
	for i, p := range packs {
		result[i] = warcry.PackInfo{Name: p.Name, Enabled: p.Enabled, Members: p.Members}
	}
	return result
}

// buildKafkaRuntimeConfig converts a config.KafkaConfig to a kafka.Config.
func buildKafkaRuntimeConfig(kc *config.KafkaConfig) *kafka.Config {
	return &kafka.Config{
		Name:             kc.Name,
		Enabled:          kc.Enabled,
		Brokers:          kc.Brokers,
		UseTLS:           kc.UseTLS,
		TLSSkipVerify:    kc.TLSSkipVerify,
		SASLMechanism:    kafka.SASLMechanism(kc.SASLMechanism),
		Username:         kc.Username,
		Password:         kc.Password,
		RequiredAcks:     kc.RequiredAcks,
		MaxRetries:       kc.MaxRetries,
		RetryBackoff:     kc.RetryBackoff,
		PublishChanges:   kc.PublishChanges,
		Selector:         kc.Selector,
		AutoCreateTopics: kc.AutoCreateTopics == nil || *kc.AutoCreateTopics,
		EnableWriteback:  kc.EnableWriteback,
		ConsumerGroup:    kc.ConsumerGroup,
		WriteMaxAge:      kc.WriteMaxAge,
	}
}
