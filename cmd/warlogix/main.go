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
	"warlogix/mqtt"
	"warlogix/plcman"
	"warlogix/tui"
)

// Version is set at build time via -ldflags
var Version = "dev"

// tuiDebugLogger adapts the TUI debug logging for MQTT.
type tuiDebugLogger struct{}

func (t tuiDebugLogger) LogMQTT(format string, args ...interface{}) {
	tui.DebugLogMQTT(format, args...)
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

	// Set up MQTT publishing on value changes
	manager.SetOnValueChange(func(changes []plcman.ValueChange) {
		if !mqttMgr.AnyRunning() {
			return
		}
		for _, c := range changes {
			// Always publish when a change is detected (force=true to bypass publisher cache)
			mqttMgr.Publish(c.PLCName, c.TagName, c.TypeName, c.Value, true)
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

	// Create TUI app first (this sets up the debug logger)
	app := tui.NewApp(cfg, *configPath, manager, apiServer, mqttMgr)

	// Set up MQTT debug logging to the TUI debug tab
	mqtt.SetDebugLogger(tuiDebugLogger{})

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

	// Auto-start enabled MQTT publishers
	mqttMgr.StartAll()

	// Run TUI (Shutdown handles all cleanup)
	if err := app.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
