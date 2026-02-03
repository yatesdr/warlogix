// Warlogix - PLC Gateway TUI Application
//
// A text user interface for managing PLC connections, browsing tags,
// and republishing data via REST API and MQTT.
package main

import (
	"flag"
	"fmt"
	"os"

	"warlogix/api"
	"warlogix/config"
	"warlogix/kafka"
	"warlogix/mqtt"
	"warlogix/plcman"
	"warlogix/trigger"
	"warlogix/tui"
	"warlogix/valkey"
)

// Version is set at build time via -ldflags
var Version = "dev"

// tuiDebugLogger adapts the TUI debug logging for MQTT.
type tuiDebugLogger struct{}

func (t tuiDebugLogger) LogMQTT(format string, args ...interface{}) {
	tui.DebugLogMQTT(format, args...)
}

// tuiValkeyDebugLogger adapts the TUI debug logging for Valkey.
type tuiValkeyDebugLogger struct{}

func (t tuiValkeyDebugLogger) LogValkey(format string, args ...interface{}) {
	tui.DebugLogValkey(format, args...)
}

// tuiKafkaDebugLogger adapts the TUI debug logging for Kafka.
type tuiKafkaDebugLogger struct{}

func (t tuiKafkaDebugLogger) LogKafka(format string, args ...interface{}) {
	tui.DebugLogKafka(format, args...)
}

func main() {
	// Parse command line flags
	configPath := flag.String("config", config.DefaultPath(), "Path to configuration file")
	showVersion := flag.Bool("version", false, "Show version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("warlogix %s\n", Version)
		os.Exit(0)
	}

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Create PLC manager
	manager := plcman.NewManager(cfg.PollRate)

	// Load PLCs from config
	manager.LoadFromConfig(cfg)

	// Create REST API server
	apiServer := api.NewServer(manager, &cfg.REST)

	// Create MQTT manager
	mqttMgr := mqtt.NewManager()
	mqttMgr.LoadFromConfig(cfg.MQTT)

	// Create Valkey manager
	valkeyMgr := valkey.NewManager()
	valkeyMgr.LoadFromConfig(cfg.Valkey)

	// Create Kafka manager
	kafkaMgr := kafka.NewManager()
	for i := range cfg.Kafka {
		kc := cfg.Kafka[i]
		kafkaMgr.AddCluster(&kafka.Config{
			Name:           kc.Name,
			Enabled:        kc.Enabled,
			Brokers:        kc.Brokers,
			UseTLS:         kc.UseTLS,
			TLSSkipVerify:  kc.TLSSkipVerify,
			SASLMechanism:  kafka.SASLMechanism(kc.SASLMechanism),
			Username:       kc.Username,
			Password:       kc.Password,
			RequiredAcks:   kc.RequiredAcks,
			MaxRetries:     kc.MaxRetries,
			RetryBackoff:   kc.RetryBackoff,
			PublishChanges: kc.PublishChanges,
			Topic:          kc.Topic,
		})
	}

	// Create trigger manager with PLC manager as reader/writer
	tagReader := &plcman.TriggerTagReader{Manager: manager}
	tagWriter := &plcman.TriggerTagWriter{Manager: manager}
	triggerMgr := trigger.NewManager(kafkaMgr, tagReader, tagWriter)
	triggerMgr.LoadFromConfig(cfg.Triggers)

	// Set up publishing on value changes
	// MQTT, Valkey, and Kafka run in separate goroutines to avoid blocking each other
	manager.SetOnValueChange(func(changes []plcman.ValueChange) {
		// Check running status without blocking
		mqttRunning := mqttMgr.AnyRunning()
		valkeyRunning := valkeyMgr.AnyRunning()
		kafkaPublishing := kafkaMgr.AnyPublishing()

		tui.DebugLog("OnValueChange: %d changes, MQTT: %v, Valkey: %v, Kafka: %v",
			len(changes), mqttRunning, valkeyRunning, kafkaPublishing)

		if !mqttRunning && !valkeyRunning && !kafkaPublishing {
			return
		}

		// Copy changes for goroutines
		changesCopy := make([]plcman.ValueChange, len(changes))
		copy(changesCopy, changes)

		// Publish to MQTT in its own goroutine
		if mqttRunning {
			go func() {
				for _, c := range changesCopy {
					mqttMgr.Publish(c.PLCName, c.TagName, c.TypeName, c.Value, true)
				}
			}()
		}

		// Publish to Valkey in its own goroutine
		if valkeyRunning {
			go func() {
				for _, c := range changesCopy {
					valkeyMgr.Publish(c.PLCName, c.TagName, c.TypeName, c.Value, c.Writable)
				}
			}()
		}

		// Publish to Kafka in its own goroutine (if PublishChanges enabled)
		if kafkaPublishing {
			go func() {
				for _, c := range changesCopy {
					// Use force=true since OnValueChange already confirms this is a changed value
					kafkaMgr.Publish(c.PLCName, c.TagName, c.TypeName, c.Value, c.Writable, true)
				}
			}()
		}
	})

	// Set up MQTT write handling
	mqttMgr.SetWriteHandler(func(plcName, tagName string, value interface{}) error {
		return manager.WriteTag(plcName, tagName, value)
	})

	// Set up write validation - check if tag is writable in config
	mqttMgr.SetWriteValidator(func(plcName, tagName string) bool {
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
	})

	// Set up tag type lookup for proper value conversion
	mqttMgr.SetTagTypeLookup(func(plcName, tagName string) uint16 {
		return manager.GetTagType(plcName, tagName)
	})

	// Set PLC names for write subscriptions
	plcNames := make([]string, len(cfg.PLCs))
	for i, plc := range cfg.PLCs {
		plcNames[i] = plc.Name
	}
	mqttMgr.SetPLCNames(plcNames)
	valkeyMgr.SetPLCNames(plcNames)

	// Set up Valkey write handling
	valkeyMgr.SetWriteHandler(func(plcName, tagName string, value interface{}) error {
		return manager.WriteTag(plcName, tagName, value)
	})

	// Set up Valkey write validation - check if tag is writable in config
	valkeyMgr.SetWriteValidator(func(plcName, tagName string) bool {
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
	})

	// Set up Valkey tag type lookup for proper value conversion
	valkeyMgr.SetTagTypeLookup(func(plcName, tagName string) uint16 {
		return manager.GetTagType(plcName, tagName)
	})

	// Create TUI app first (this sets up the debug logger)
	app := tui.NewApp(cfg, *configPath, manager, apiServer, mqttMgr, valkeyMgr, kafkaMgr, triggerMgr)

	// Set up MQTT debug logging to the TUI debug tab
	mqtt.SetDebugLogger(tuiDebugLogger{})

	// Set up Valkey debug logging to the TUI debug tab
	valkey.SetDebugLogger(tuiValkeyDebugLogger{})

	// Set up Kafka debug logging to the TUI debug tab
	kafka.SetDebugLogger(tuiKafkaDebugLogger{})

	// Set up Valkey on-connect callback to publish all values
	valkeyMgr.SetOnConnectCallback(func() {
		app.ForcePublishAllValuesToValkey()
	})

	// Start manager polling
	manager.Start()

	// Auto-start REST server if enabled in config
	if cfg.REST.Enabled {
		if err := apiServer.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to start REST server: %v\n", err)
		}
	}

	// Auto-connect enabled PLCs first (so we have values to publish)
	manager.ConnectEnabled()

	// Auto-start enabled MQTT publishers in background
	go func() {
		if started := mqttMgr.StartAll(); started > 0 {
			// Force publish all current values for initial sync
			app.ForcePublishAllValues()
		}
	}()

	// Auto-start enabled Valkey publishers in background
	go func() {
		if started := valkeyMgr.StartAll(); started > 0 {
			// Force publish all current values for initial sync
			app.ForcePublishAllValuesToValkey()
		}
	}()

	// Auto-connect enabled Kafka clusters in background
	go kafkaMgr.ConnectEnabled()

	// Set up trigger debug logging
	triggerMgr.SetLogFunc(func(format string, args ...interface{}) {
		tui.DebugLog(format, args...)
	})

	// Auto-start enabled triggers
	triggerMgr.Start()

	// Run TUI (Shutdown handles all cleanup)
	if err := app.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
