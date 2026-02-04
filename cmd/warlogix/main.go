// Warlogix - PLC Gateway TUI Application
//
// A text user interface for managing PLC connections, browsing tags,
// and republishing data via REST API and MQTT.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"warlogix/api"
	"warlogix/config"
	"warlogix/kafka"
	"warlogix/logging"
	"warlogix/mqtt"
	"warlogix/plcman"
	"warlogix/ssh"
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

// Command line flags
var (
	configPath  = flag.String("config", config.DefaultPath(), "Path to configuration file")
	showVersion = flag.Bool("version", false, "Show version and exit")
	daemonMode  = flag.Bool("d", false, "Run in daemon mode (serve TUI over SSH)")
	sshPort     = flag.Int("p", 2222, "SSH port (daemon mode only)")
	sshPassword = flag.String("ssh-password", "", "Password for SSH authentication (daemon mode only)")
	sshKeys     = flag.String("ssh-keys", "", "Path to authorized_keys file or directory (daemon mode only)")
	logFile     = flag.String("log", "", "Path to log file (optional, writes alongside debug window)")
)

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Printf("warlogix %s\n", Version)
		os.Exit(0)
	}

	// Validate daemon mode flags
	if *daemonMode {
		if *sshPassword == "" && *sshKeys == "" {
			fmt.Fprintf(os.Stderr, "Error: daemon mode requires --ssh-password or --ssh-keys\n")
			os.Exit(1)
		}
	}

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	if *daemonMode {
		runDaemonMode(cfg)
	} else {
		runLocalMode(cfg)
	}
}

// runLocalMode runs the TUI in local mode (the original behavior).
func runLocalMode(cfg *config.Config) {
	// Create PLC manager
	manager := plcman.NewManager(cfg.PollRate)
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

	// Create trigger manager
	tagReader := &plcman.TriggerTagReader{Manager: manager}
	tagWriter := &plcman.TriggerTagWriter{Manager: manager}
	triggerMgr := trigger.NewManager(kafkaMgr, tagReader, tagWriter)
	triggerMgr.LoadFromConfig(cfg.Triggers)

	// Set up publishing on value changes
	setupValueChangeHandlers(manager, mqttMgr, valkeyMgr, kafkaMgr)

	// Set up MQTT/Valkey write handling
	setupWriteHandlers(cfg, manager, mqttMgr, valkeyMgr)

	// Set PLC names for write subscriptions
	plcNames := make([]string, len(cfg.PLCs))
	for i, plc := range cfg.PLCs {
		plcNames[i] = plc.Name
	}
	mqttMgr.SetPLCNames(plcNames)
	valkeyMgr.SetPLCNames(plcNames)

	// Create TUI app first (this sets up the debug logger)
	app := tui.NewApp(cfg, *configPath, manager, apiServer, mqttMgr, valkeyMgr, kafkaMgr, triggerMgr)

	// Set up file logging if specified
	if *logFile != "" {
		fileLogger, err := logging.NewFileLogger(*logFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to open log file: %v\n", err)
		} else {
			tui.SetDebugFileLogger(fileLogger)
			defer fileLogger.Close()
		}
	}

	// Set up debug loggers
	mqtt.SetDebugLogger(tuiDebugLogger{})
	valkey.SetDebugLogger(tuiValkeyDebugLogger{})
	kafka.SetDebugLogger(tuiKafkaDebugLogger{})

	// Set up Valkey on-connect callback
	valkeyMgr.SetOnConnectCallback(func() {
		app.ForcePublishAllValuesToValkey()
	})

	// Start manager polling
	manager.Start()

	// Auto-start REST server if enabled
	if cfg.REST.Enabled {
		if err := apiServer.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to start REST server: %v\n", err)
		}
	}

	// Auto-connect enabled PLCs
	manager.ConnectEnabled()

	// Auto-start enabled MQTT publishers
	go func() {
		if started := mqttMgr.StartAll(); started > 0 {
			app.ForcePublishAllValues()
		}
	}()

	// Auto-start enabled Valkey publishers
	go func() {
		if started := valkeyMgr.StartAll(); started > 0 {
			app.ForcePublishAllValuesToValkey()
		}
	}()

	// Auto-connect enabled Kafka clusters
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

// runDaemonMode runs the TUI in daemon mode, serving it over SSH.
func runDaemonMode(cfg *config.Config) {
	fmt.Printf("Starting warlogix daemon on port %d...\n", *sshPort)

	// Create PLC manager
	manager := plcman.NewManager(cfg.PollRate)
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

	// Create trigger manager
	tagReader := &plcman.TriggerTagReader{Manager: manager}
	tagWriter := &plcman.TriggerTagWriter{Manager: manager}
	triggerMgr := trigger.NewManager(kafkaMgr, tagReader, tagWriter)
	triggerMgr.LoadFromConfig(cfg.Triggers)

	// Set up publishing on value changes
	setupValueChangeHandlers(manager, mqttMgr, valkeyMgr, kafkaMgr)

	// Set up MQTT/Valkey write handling
	setupWriteHandlers(cfg, manager, mqttMgr, valkeyMgr)

	// Set PLC names for write subscriptions
	plcNames := make([]string, len(cfg.PLCs))
	for i, plc := range cfg.PLCs {
		plcNames[i] = plc.Name
	}
	mqttMgr.SetPLCNames(plcNames)
	valkeyMgr.SetPLCNames(plcNames)

	// Create SSH server
	sshServer := ssh.NewServer(&ssh.Config{
		Port:           *sshPort,
		Password:       *sshPassword,
		AuthorizedKeys: *sshKeys,
	})

	// Start SSH server first to get the PTY
	if err := sshServer.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting SSH server: %v\n", err)
		os.Exit(1)
	}

	// Create TUI app with PTY
	app, err := tui.NewAppWithPTY(cfg, *configPath, manager, apiServer, mqttMgr, valkeyMgr, kafkaMgr, triggerMgr, sshServer.GetPTYSlave())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating TUI: %v\n", err)
		sshServer.Stop()
		os.Exit(1)
	}

	// Set up file logging if specified
	var fileLogger *logging.FileLogger
	if *logFile != "" {
		var err error
		fileLogger, err = logging.NewFileLogger(*logFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to open log file: %v\n", err)
		} else {
			tui.SetDebugFileLogger(fileLogger)
		}
	}

	// Set up debug loggers
	mqtt.SetDebugLogger(tuiDebugLogger{})
	valkey.SetDebugLogger(tuiValkeyDebugLogger{})
	kafka.SetDebugLogger(tuiKafkaDebugLogger{})

	// Set up Valkey on-connect callback
	valkeyMgr.SetOnConnectCallback(func() {
		app.ForcePublishAllValuesToValkey()
	})

	// Set up session callbacks for logging
	sshServer.SetOnSessionConnect(func(remoteAddr string) {
		tui.DebugLogSSH("Client connected from %s (total sessions: %d)", remoteAddr, sshServer.SessionCount())
	})
	sshServer.SetOnSessionDisconnect(func(remoteAddr string) {
		tui.DebugLogSSH("Client disconnected from %s (total sessions: %d)", remoteAddr, sshServer.SessionCount())
	})

	// Set up daemon mode callbacks
	// In PTY multiplexing mode, disconnect is a no-op since we can't disconnect individual sessions
	// from within the TUI. The session ends when the SSH client disconnects.
	app.SetOnDisconnect(func() {
		tui.DebugLogSSH("Disconnect requested (clients should close their SSH connection)")
	})

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start manager polling
	manager.Start()

	// Auto-start REST server if enabled
	if cfg.REST.Enabled {
		if err := apiServer.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to start REST server: %v\n", err)
		}
	}

	// Auto-connect enabled PLCs
	manager.ConnectEnabled()

	// Auto-start enabled MQTT publishers
	go func() {
		if started := mqttMgr.StartAll(); started > 0 {
			app.ForcePublishAllValues()
		}
	}()

	// Auto-start enabled Valkey publishers
	go func() {
		if started := valkeyMgr.StartAll(); started > 0 {
			app.ForcePublishAllValuesToValkey()
		}
	}()

	// Auto-connect enabled Kafka clusters
	go kafkaMgr.ConnectEnabled()

	// Set up trigger debug logging
	triggerMgr.SetLogFunc(func(format string, args ...interface{}) {
		tui.DebugLog(format, args...)
	})

	// Auto-start enabled triggers
	triggerMgr.Start()

	fmt.Printf("Daemon started. SSH available on port %d\n", *sshPort)
	fmt.Printf("Connect with: ssh -p %d localhost\n", *sshPort)
	fmt.Printf("Press Ctrl+C to stop the daemon\n")

	// Run TUI in background
	go func() {
		if err := app.Run(); err != nil {
			tui.DebugLogError("TUI error: %v", err)
		}
	}()

	// Wait for shutdown signal
	sig := <-sigChan
	fmt.Printf("\nReceived %v, shutting down...\n", sig)

	// Graceful shutdown
	app.Shutdown()
	sshServer.Stop()

	if fileLogger != nil {
		fileLogger.Close()
	}

	fmt.Println("Daemon stopped")
}

// setupValueChangeHandlers sets up the value change callback for publishing to MQTT, Valkey, and Kafka.
func setupValueChangeHandlers(manager *plcman.Manager, mqttMgr *mqtt.Manager, valkeyMgr *valkey.Manager, kafkaMgr *kafka.Manager) {
	manager.SetOnValueChange(func(changes []plcman.ValueChange) {
		mqttRunning := mqttMgr.AnyRunning()
		valkeyRunning := valkeyMgr.AnyRunning()
		kafkaPublishing := kafkaMgr.AnyPublishing()

		tui.DebugLog("OnValueChange: %d changes, MQTT: %v, Valkey: %v, Kafka: %v",
			len(changes), mqttRunning, valkeyRunning, kafkaPublishing)

		if !mqttRunning && !valkeyRunning && !kafkaPublishing {
			return
		}

		changesCopy := make([]plcman.ValueChange, len(changes))
		copy(changesCopy, changes)

		if mqttRunning {
			go func() {
				for _, c := range changesCopy {
					mqttMgr.Publish(c.PLCName, c.TagName, c.Alias, c.Address, c.TypeName, c.Value, true)
				}
			}()
		}

		if valkeyRunning {
			go func() {
				for _, c := range changesCopy {
					valkeyMgr.Publish(c.PLCName, c.TagName, c.Alias, c.Address, c.TypeName, c.Value, c.Writable)
				}
			}()
		}

		if kafkaPublishing {
			go func() {
				for _, c := range changesCopy {
					kafkaMgr.Publish(c.PLCName, c.TagName, c.Alias, c.Address, c.TypeName, c.Value, c.Writable, true)
				}
			}()
		}
	})
}

// setupWriteHandlers sets up MQTT and Valkey write handling.
func setupWriteHandlers(cfg *config.Config, manager *plcman.Manager, mqttMgr *mqtt.Manager, valkeyMgr *valkey.Manager) {
	// MQTT write handler
	mqttMgr.SetWriteHandler(func(plcName, tagName string, value interface{}) error {
		return manager.WriteTag(plcName, tagName, value)
	})

	// MQTT write validator
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

	// MQTT tag type lookup
	mqttMgr.SetTagTypeLookup(func(plcName, tagName string) uint16 {
		return manager.GetTagType(plcName, tagName)
	})

	// Valkey write handler
	valkeyMgr.SetWriteHandler(func(plcName, tagName string, value interface{}) error {
		return manager.WriteTag(plcName, tagName, value)
	})

	// Valkey write validator
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

	// Valkey tag type lookup
	valkeyMgr.SetTagTypeLookup(func(plcName, tagName string) uint16 {
		return manager.GetTagType(plcName, tagName)
	})
}
