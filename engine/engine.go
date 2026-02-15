package engine

import (
	"encoding/json"

	"warlink/config"
	"warlink/kafka"
	"warlink/logging"
	"warlink/mqtt"
	"warlink/plcman"
	"warlink/rule"
	"warlink/tagpack"
	"warlink/valkey"
	"warlink/warcry"
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
	ruleMgr    *rule.Manager
	packMgr    *tagpack.Manager
	warcryMgr  *warcry.Server

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
		if e.warcryMgr != nil && e.warcryMgr.HasClients() {
			e.warcryMgr.BroadcastTagPack(info.Config.Name, data)
		}
	})
	e.packMgr.SetLogFunc(func(format string, args ...interface{}) {
		e.logFn(format, args...)
	})

	// Create rule manager
	ruleReader := &plcman.RuleTagReader{Manager: e.plcMan}
	ruleWriter := &plcman.RuleTagWriter{Manager: e.plcMan}
	e.ruleMgr = rule.NewManager(e.kafkaMgr, ruleReader, ruleWriter)
	e.ruleMgr.LoadFromConfig(cfg.Rules)
	e.ruleMgr.SetPackManager(e.packMgr)
	e.ruleMgr.SetMQTTManager(e.mqttMgr)
	e.ruleMgr.SetNamespace(cfg.Namespace)

	// Create warcry connector server
	e.warcryMgr = warcry.NewServer()
	e.warcryMgr.SetLogFunc(func(format string, args ...interface{}) {
		e.logFn(format, args...)
	})
	e.warcryMgr.SetNamespace(cfg.Namespace)
	e.warcryMgr.SetPLCProvider(warcryPLCAdapter{mgr: e.plcMan})
	e.warcryMgr.SetPackProvider(warcryPackAdapter{mgr: e.packMgr})
	if cfg.Warcry.Enabled && cfg.Warcry.Listen != "" {
		if err := e.warcryMgr.Start(cfg.Warcry.Listen, cfg.Warcry.BufferSize); err != nil {
			e.logFn("Warcry connector failed to start: %v", err)
		}
	}

	// Wire value change handlers
	setupValueChangeHandlers(e.plcMan, e.mqttMgr, e.valkeyMgr, e.kafkaMgr, e.packMgr, e.warcryMgr)

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

	// Set up rule logging + auto-start
	e.ruleMgr.SetLogFunc(func(format string, args ...interface{}) {
		e.logFn(format, args...)
	})
	e.ruleMgr.Start()

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

	if e.ruleMgr != nil {
		e.ruleMgr.Stop()
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
	if e.warcryMgr != nil {
		e.warcryMgr.Stop()
	}
	if e.plcMan != nil {
		e.plcMan.Stop()
		e.plcMan.DisconnectAll()
	}
}

// Managers provides access to shared backend managers.
// *Engine satisfies this interface via its accessor methods.
type Managers interface {
	GetConfig() *config.Config
	GetConfigPath() string
	GetPLCMan() *plcman.Manager
	GetMQTTMgr() *mqtt.Manager
	GetValkeyMgr() *valkey.Manager
	GetKafkaMgr() *kafka.Manager
	GetRuleMgr() *rule.Manager
	GetPackMgr() *tagpack.Manager
}

// Verify *Engine implements Managers at compile time.
var _ Managers = (*Engine)(nil)

func (e *Engine) GetConfig() *config.Config       { return e.cfg }
func (e *Engine) GetConfigPath() string            { return e.configPath }
func (e *Engine) GetPLCMan() *plcman.Manager       { return e.plcMan }
func (e *Engine) GetMQTTMgr() *mqtt.Manager        { return e.mqttMgr }
func (e *Engine) GetValkeyMgr() *valkey.Manager    { return e.valkeyMgr }
func (e *Engine) GetKafkaMgr() *kafka.Manager      { return e.kafkaMgr }
func (e *Engine) GetRuleMgr() *rule.Manager         { return e.ruleMgr }
func (e *Engine) GetPackMgr() *tagpack.Manager     { return e.packMgr }
func (e *Engine) GetWarcryMgr() *warcry.Server      { return e.warcryMgr }

// saveConfig is a helper that locks, saves, and unlocks.
func (e *Engine) saveConfig() error {
	return e.cfg.UnlockAndSave(e.configPath)
}

func (e *Engine) emit(t EventType, payload interface{}) {
	e.Events.Emit(Event{Type: t, Payload: payload})
}
