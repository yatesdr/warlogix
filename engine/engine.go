package engine

import (
	"encoding/json"

	"warlink/config"
	"warlink/kafka"
	"warlink/logging"
	"warlink/mqtt"
	"warlink/plcman"
	"warlink/push"
	"warlink/tagpack"
	"warlink/trigger"
	"warlink/valkey"
)

// LogFunc is the logging callback signature. Engine never imports the tui package.
type LogFunc func(format string, args ...interface{})

// Config holds the parameters needed to create an Engine.
type Config struct {
	AppConfig  *config.Config
	ConfigPath string
	LogFunc    LogFunc
}

// Engine centralizes all business logic: config mutations, manager orchestration,
// and callback wiring. TUI, WebUI, REST API, and SSH become thin consumers.
type Engine struct {
	cfg        *config.Config
	configPath string
	logFn      LogFunc

	plcMan     *plcman.Manager
	mqttMgr    *mqtt.Manager
	valkeyMgr  *valkey.Manager
	kafkaMgr   *kafka.Manager
	triggerMgr *trigger.Manager
	pushMgr    *push.Manager
	packMgr    *tagpack.Manager

	Events *EventBus

	stopChan chan struct{}
}

// New creates a new Engine. Call Start() to initialize managers and wiring.
func New(c Config) *Engine {
	logFn := c.LogFunc
	if logFn == nil {
		logFn = func(string, ...interface{}) {}
	}
	return &Engine{
		cfg:        c.AppConfig,
		configPath: c.ConfigPath,
		logFn:      logFn,
		Events:     NewEventBus(),
		stopChan:   make(chan struct{}),
	}
}

// Start creates all managers, wires callbacks, and auto-starts enabled services.
func (e *Engine) Start() {
	cfg := e.cfg

	// Create PLC manager
	e.plcMan = plcman.NewManager(cfg.PollRate)
	e.plcMan.LoadFromConfig(cfg)

	// Create MQTT manager
	e.mqttMgr = mqtt.NewManager()
	e.mqttMgr.LoadFromConfig(cfg.MQTT, cfg.Namespace)

	// Create Valkey manager
	e.valkeyMgr = valkey.NewManager()
	e.valkeyMgr.LoadFromConfig(cfg.Valkey, cfg.Namespace)

	// Create Kafka manager
	e.kafkaMgr = kafka.NewManager()
	for i := range cfg.Kafka {
		kc := cfg.Kafka[i]
		e.kafkaMgr.AddCluster(buildKafkaRuntimeConfig(&kc), cfg.Namespace)
	}

	// Create TagPack manager
	provider := &plcDataProvider{manager: e.plcMan}
	e.packMgr = tagpack.NewManager(cfg, provider)
	e.packMgr.SetOnPublish(func(info tagpack.PackPublishInfo) {
		data, err := json.Marshal(info.Value)
		if err != nil {
			logging.DebugLog("tagpack", "JSON marshal error: %v", err)
			return
		}
		logging.DebugLog("tagpack", "Callback for %s: MQTT=%v Kafka=%v Valkey=%v",
			info.Config.Name, info.Config.MQTTEnabled, info.Config.KafkaEnabled, info.Config.ValkeyEnabled)
		if info.Config.MQTTEnabled {
			e.mqttMgr.PublishTagPack(info.Config.Name, data)
		}
		if info.Config.KafkaEnabled {
			e.kafkaMgr.PublishTagPack(info.Config.Name, data)
		}
		if info.Config.ValkeyEnabled {
			logging.DebugLog("tagpack", "Publishing to Valkey channel: %s", info.ValkeyChannel)
			e.valkeyMgr.PublishRaw(info.ValkeyChannel, data)
		}
	})
	e.packMgr.SetLogFunc(func(format string, args ...interface{}) {
		e.logFn(format, args...)
	})

	// Create trigger manager
	tagReader := &plcman.TriggerTagReader{Manager: e.plcMan}
	tagWriter := &plcman.TriggerTagWriter{Manager: e.plcMan}
	e.triggerMgr = trigger.NewManager(e.kafkaMgr, tagReader, tagWriter)
	e.triggerMgr.LoadFromConfig(cfg.Triggers)
	e.triggerMgr.SetPackManager(e.packMgr)
	e.triggerMgr.SetMQTTManager(e.mqttMgr)
	e.triggerMgr.SetNamespace(cfg.Namespace)

	// Create push manager
	e.pushMgr = push.NewManager(tagReader)
	e.pushMgr.LoadFromConfig(cfg.Pushes)

	// Wire value change handlers
	setupValueChangeHandlers(e.plcMan, e.mqttMgr, e.valkeyMgr, e.kafkaMgr, e.packMgr)

	// Wire write handlers
	setupWriteHandlers(cfg, e.plcMan, e.mqttMgr, e.valkeyMgr, e.kafkaMgr)

	// Set PLC names for MQTT write subscriptions
	e.updateMQTTPLCNamesInternal()

	// Set up Valkey on-connect callback for initial sync
	e.valkeyMgr.SetOnConnectCallback(func() {
		e.forcePublishAllValuesToValkey()
	})

	// Set up manager logging
	e.plcMan.SetOnLog(func(format string, args ...interface{}) {
		e.logFn(format, args...)
	})

	// Start manager polling
	e.plcMan.Start()

	// Auto-connect enabled PLCs
	e.plcMan.ConnectEnabled()

	// Auto-start enabled MQTT publishers
	go func() {
		if started := e.mqttMgr.StartAll(); started > 0 {
			e.forcePublishAllValuesToMQTT()
		}
	}()

	// Auto-start enabled Valkey publishers
	go func() {
		if started := e.valkeyMgr.StartAll(); started > 0 {
			e.forcePublishAllValuesToValkey()
		}
	}()

	// Auto-connect enabled Kafka clusters
	go e.kafkaMgr.ConnectEnabled()

	// Set up trigger and push logging + auto-start
	e.triggerMgr.SetLogFunc(func(format string, args ...interface{}) {
		e.logFn(format, args...)
	})
	e.triggerMgr.Start()

	e.pushMgr.SetLogFunc(func(format string, args ...interface{}) {
		e.logFn(format, args...)
	})
	e.pushMgr.Start()

	// Start health publishing loop
	go e.publishHealthLoop()
}

// Stop shuts down all managers gracefully.
func (e *Engine) Stop() {
	select {
	case <-e.stopChan:
	default:
		close(e.stopChan)
	}

	if e.triggerMgr != nil {
		e.triggerMgr.Stop()
	}
	if e.pushMgr != nil {
		e.pushMgr.Stop()
	}
	if e.packMgr != nil {
		e.packMgr.Stop()
	}
	if e.mqttMgr != nil {
		e.mqttMgr.StopAll()
	}
	if e.valkeyMgr != nil {
		e.valkeyMgr.StopAll()
	}
	if e.kafkaMgr != nil {
		e.kafkaMgr.StopAll()
	}
	if e.plcMan != nil {
		e.plcMan.Stop()
		e.plcMan.DisconnectAll()
	}
}

// --- web.Managers interface implementation ---

func (e *Engine) GetConfig() *config.Config       { return e.cfg }
func (e *Engine) GetConfigPath() string            { return e.configPath }
func (e *Engine) GetPLCMan() *plcman.Manager       { return e.plcMan }
func (e *Engine) GetMQTTMgr() *mqtt.Manager        { return e.mqttMgr }
func (e *Engine) GetValkeyMgr() *valkey.Manager    { return e.valkeyMgr }
func (e *Engine) GetKafkaMgr() *kafka.Manager      { return e.kafkaMgr }
func (e *Engine) GetTriggerMgr() *trigger.Manager  { return e.triggerMgr }
func (e *Engine) GetPushMgr() *push.Manager        { return e.pushMgr }
func (e *Engine) GetPackMgr() *tagpack.Manager     { return e.packMgr }

// saveConfig is a helper that locks, saves, and unlocks.
func (e *Engine) saveConfig() error {
	return e.cfg.UnlockAndSave(e.configPath)
}

func (e *Engine) emit(t EventType, payload interface{}) {
	e.Events.Emit(Event{Type: t, Payload: payload})
}
