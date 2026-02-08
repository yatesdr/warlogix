// Warlogix - PLC Gateway TUI Application
//
// A text user interface for managing PLC connections, browsing tags,
// and republishing data via REST API and MQTT.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"warlogix/api"
	"warlogix/brokertest"
	"warlogix/config"
	"warlogix/kafka"
	"warlogix/logging"
	"warlogix/mqtt"
	"warlogix/plcman"
	"warlogix/ssh"
	"warlogix/tagpack"
	"warlogix/trigger"
	"warlogix/tui"
	"warlogix/valkey"
)

// Version is set at build time via -ldflags
var Version = "dev"

// preprocessLogDebugFlag handles --log-debug without a value by injecting "all" as the default.
// This allows users to use `--log-debug` alone to enable all protocol logging.
func preprocessLogDebugFlag() {
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]
		// Check for --log-debug or -log-debug without =
		if arg == "--log-debug" || arg == "-log-debug" {
			// Check if next arg exists and is not another flag
			if i+1 >= len(args) || (len(args[i+1]) > 0 && args[i+1][0] == '-') {
				// No value provided, inject "all"
				os.Args = append(os.Args[:i+2], append([]string{"all"}, os.Args[i+2:]...)...)
			}
			return
		}
		// If it has = sign, value is already provided
		if len(arg) > 11 && (arg[:12] == "--log-debug=" || arg[:11] == "-log-debug=") {
			return
		}
	}
}

// Command line flags
var (
	configPath   = flag.String("config", config.DefaultPath(), "Path to configuration file")
	showVersion  = flag.Bool("version", false, "Show version and exit")
	daemonMode   = flag.Bool("d", false, "Run in daemon mode (serve TUI over SSH)")
	namespace    = flag.String("namespace", "", "Set namespace (saved to config, required for daemon mode if not in config)")
	sshPort      = flag.Int("p", 2222, "SSH port (daemon mode only)")
	sshPassword  = flag.String("ssh-password", "", "Password for SSH authentication (daemon mode only)")
	sshKeys      = flag.String("ssh-keys", "", "Path to authorized_keys file or directory (daemon mode only)")
	logFile      = flag.String("log", "", "Path to log file (optional, writes alongside debug window)")
	logDebug     = flag.String("log-debug", "", "Enable debug logging to debug.log. Use without value for all, or specify protocol (omron,ads,logix,s7,mqtt,kafka,valkey,tui)")
	testBrokers  = flag.Bool("stress-test-republishing", false, "Run stress tests for republishing (Kafka, MQTT, Valkey) and exit")
	testDuration = flag.Duration("test-duration", 10*time.Second, "Duration for each broker stress test")
	testTags     = flag.Int("test-tags", 100, "Number of simulated tags for stress test")
	testPLCs     = flag.Int("test-plcs", 50, "Number of simulated PLCs for stress test")
	testYes      = flag.Bool("y", false, "Skip confirmation prompt for stress tests")
)

func main() {
	// Pre-process args to handle --log-debug without a value
	// Go's flag package requires a value for string flags, but we want --log-debug
	// alone to mean "all protocols"
	preprocessLogDebugFlag()

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

	// Handle --namespace flag: overwrite config and save
	if *namespace != "" {
		if !config.IsValidNamespace(*namespace) {
			fmt.Fprintf(os.Stderr, "Error: invalid namespace '%s' (use alphanumeric, hyphen, underscore, dot)\n", *namespace)
			os.Exit(1)
		}
		cfg.Namespace = *namespace
		if err := cfg.Save(*configPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Namespace set to '%s' and saved to config\n", *namespace)
	}

	// Daemon mode requires namespace to be configured
	if *daemonMode && cfg.Namespace == "" {
		fmt.Fprintf(os.Stderr, "Error: daemon mode requires a namespace\n")
		fmt.Fprintf(os.Stderr, "Provide --namespace=<name> or run in local mode first to configure\n")
		os.Exit(1)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		os.Exit(1)
	}

	// Run broker stress tests if requested
	if *testBrokers {
		runBrokerTests(cfg)
		return
	}

	if *daemonMode {
		runDaemonMode(cfg)
	} else {
		runLocalMode(cfg)
	}
}

// runBrokerTests runs stress tests against configured message brokers.
func runBrokerTests(cfg *config.Config) {
	// Count enabled brokers
	enabledCount := 0
	var brokerList []string
	for _, k := range cfg.Kafka {
		if k.Enabled {
			enabledCount++
			brokerList = append(brokerList, fmt.Sprintf("Kafka/%s (%s)", k.Name, strings.Join(k.Brokers, ",")))
		}
	}
	for _, m := range cfg.MQTT {
		if m.Enabled {
			enabledCount++
			brokerList = append(brokerList, fmt.Sprintf("MQTT/%s (%s:%d)", m.Name, m.Broker, m.Port))
		}
	}
	for _, v := range cfg.Valkey {
		if v.Enabled {
			enabledCount++
			brokerList = append(brokerList, fmt.Sprintf("Valkey/%s (%s)", v.Name, v.Address))
		}
	}

	if enabledCount == 0 {
		fmt.Println("No enabled brokers found in configuration.")
		fmt.Println("Enable brokers in your config file to run stress tests.")
		return
	}

	// Warning and confirmation (skip if -y flag is set)
	if !*testYes {
		fmt.Println()
		fmt.Println("╔══════════════════════════════════════════════════════════════════╗")
		fmt.Println("║                         ⚠ WARNING ⚠                              ║")
		fmt.Println("╚══════════════════════════════════════════════════════════════════╝")
		fmt.Println()
		fmt.Printf("This will stress test %d broker(s) for %v:\n\n", enabledCount, *testDuration)
		for _, b := range brokerList {
			fmt.Printf("  • %s\n", b)
		}
		fmt.Println()
		fmt.Println("The test will send significant traffic that may saturate these servers.")
		fmt.Println("Do not run in a production environment unless it is safe to do so.")
		fmt.Println()
		fmt.Println("Test topics/keys are prefixed with 'warlogix-test-stress'.")
		fmt.Println()
		fmt.Print("Continue? [y/N]: ")

		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))

		if response != "y" && response != "yes" {
			fmt.Println("Aborted.")
			return
		}
	}

	testCfg := brokertest.TestConfig{
		Duration: *testDuration,
		NumTags:  *testTags,
		NumPLCs:  *testPLCs,
	}

	runner := brokertest.NewRunner(cfg, testCfg)
	results := runner.Run()

	// Exit with error code if any test failed
	for _, result := range results {
		if !result.Success {
			os.Exit(1)
		}
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
	mqttMgr.LoadFromConfig(cfg.MQTT, cfg.Namespace)

	// Create Valkey manager
	valkeyMgr := valkey.NewManager()
	valkeyMgr.LoadFromConfig(cfg.Valkey, cfg.Namespace)

	// Create Kafka manager
	kafkaMgr := kafka.NewManager()
	for i := range cfg.Kafka {
		kc := cfg.Kafka[i]
		kafkaMgr.AddCluster(&kafka.Config{
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
		}, cfg.Namespace)
	}

	// Create TagPack manager
	packProvider := &plcDataProvider{manager: manager}
	packMgr := tagpack.NewManager(cfg, packProvider)
	defer packMgr.Stop()
	packMgr.SetOnPublish(func(info tagpack.PackPublishInfo) {
		data, err := json.Marshal(info.Value)
		if err != nil {
			logging.DebugLog("tagpack", "JSON marshal error: %v", err)
			return
		}
		logging.DebugLog("tagpack", "Callback for %s: MQTT=%v Kafka=%v Valkey=%v",
			info.Config.Name, info.Config.MQTTEnabled, info.Config.KafkaEnabled, info.Config.ValkeyEnabled)
		if info.Config.MQTTEnabled {
			mqttMgr.PublishTagPack(info.Config.Name, data)
		}
		if info.Config.KafkaEnabled {
			kafkaMgr.PublishTagPack(info.Config.Name, data)
		}
		if info.Config.ValkeyEnabled {
			logging.DebugLog("tagpack", "Publishing to Valkey channel: %s", info.ValkeyChannel)
			valkeyMgr.PublishRaw(info.ValkeyChannel, data)
		}
	})
	packMgr.SetLogFunc(func(format string, args ...interface{}) {
		tui.DebugLog(format, args...)
	})

	// Create trigger manager
	tagReader := &plcman.TriggerTagReader{Manager: manager}
	tagWriter := &plcman.TriggerTagWriter{Manager: manager}
	triggerMgr := trigger.NewManager(kafkaMgr, tagReader, tagWriter)
	triggerMgr.LoadFromConfig(cfg.Triggers)
	triggerMgr.SetPackManager(packMgr)
	triggerMgr.SetMQTTManager(mqttMgr)
	triggerMgr.SetNamespace(cfg.Namespace)

	// Set up publishing on value changes
	setupValueChangeHandlers(manager, mqttMgr, valkeyMgr, kafkaMgr, packMgr)

	// Set up MQTT/Valkey write handling
	setupWriteHandlers(cfg, manager, mqttMgr, valkeyMgr, kafkaMgr)

	// Set PLC names for MQTT write subscriptions
	plcNames := make([]string, len(cfg.PLCs))
	for i, plc := range cfg.PLCs {
		plcNames[i] = plc.Name
	}
	mqttMgr.SetPLCNames(plcNames)

	// Create TUI app first (this sets up the debug logger)
	app := tui.NewApp(cfg, *configPath, manager, apiServer, mqttMgr, valkeyMgr, kafkaMgr, triggerMgr)
	app.SetPackManager(packMgr)
	apiServer.SetPackManager(packMgr)

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

	// Set up debug logging if specified
	// Supports: --log-debug=all (or true/1) for all protocols
	//           --log-debug=omron,ads for specific protocols
	if *logDebug != "" {
		debugLogger, err := logging.NewDebugLogger("debug.log")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to open debug log: %v\n", err)
		} else {
			// Parse filter: "all", "true", "1" = no filter, otherwise use as protocol filter
			filter := *logDebug
			if filter == "all" || filter == "true" || filter == "1" {
				filter = "" // Empty = log all
			}
			debugLogger.SetFilter(filter)
			logging.SetGlobalDebugLogger(debugLogger)
			defer debugLogger.Close()
			if filter == "" {
				tui.DebugLog("Debug logging enabled (all protocols) - writing to debug.log")
			} else {
				tui.DebugLog("Debug logging enabled (filter: %s) - writing to debug.log", filter)
			}
		}
	}

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
	mqttMgr.LoadFromConfig(cfg.MQTT, cfg.Namespace)

	// Create Valkey manager
	valkeyMgr := valkey.NewManager()
	valkeyMgr.LoadFromConfig(cfg.Valkey, cfg.Namespace)

	// Create Kafka manager
	kafkaMgr := kafka.NewManager()
	for i := range cfg.Kafka {
		kc := cfg.Kafka[i]
		kafkaMgr.AddCluster(&kafka.Config{
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
		}, cfg.Namespace)
	}

	// Create TagPack manager
	packProviderDaemon := &plcDataProvider{manager: manager}
	packMgrDaemon := tagpack.NewManager(cfg, packProviderDaemon)
	defer packMgrDaemon.Stop()
	packMgrDaemon.SetOnPublish(func(info tagpack.PackPublishInfo) {
		data, err := json.Marshal(info.Value)
		if err != nil {
			logging.DebugLog("tagpack", "JSON marshal error: %v", err)
			return
		}
		logging.DebugLog("tagpack", "Callback for %s: MQTT=%v Kafka=%v Valkey=%v",
			info.Config.Name, info.Config.MQTTEnabled, info.Config.KafkaEnabled, info.Config.ValkeyEnabled)
		if info.Config.MQTTEnabled {
			mqttMgr.PublishTagPack(info.Config.Name, data)
		}
		if info.Config.KafkaEnabled {
			kafkaMgr.PublishTagPack(info.Config.Name, data)
		}
		if info.Config.ValkeyEnabled {
			logging.DebugLog("tagpack", "Publishing to Valkey channel: %s", info.ValkeyChannel)
			valkeyMgr.PublishRaw(info.ValkeyChannel, data)
		}
	})
	packMgrDaemon.SetLogFunc(func(format string, args ...interface{}) {
		tui.DebugLog(format, args...)
	})

	// Create trigger manager
	tagReader := &plcman.TriggerTagReader{Manager: manager}
	tagWriter := &plcman.TriggerTagWriter{Manager: manager}
	triggerMgr := trigger.NewManager(kafkaMgr, tagReader, tagWriter)
	triggerMgr.LoadFromConfig(cfg.Triggers)
	triggerMgr.SetPackManager(packMgrDaemon)
	triggerMgr.SetMQTTManager(mqttMgr)
	triggerMgr.SetNamespace(cfg.Namespace)

	// Set up publishing on value changes
	setupValueChangeHandlers(manager, mqttMgr, valkeyMgr, kafkaMgr, packMgrDaemon)

	// Set up MQTT/Valkey write handling
	setupWriteHandlers(cfg, manager, mqttMgr, valkeyMgr, kafkaMgr)

	// Set PLC names for MQTT write subscriptions
	plcNames := make([]string, len(cfg.PLCs))
	for i, plc := range cfg.PLCs {
		plcNames[i] = plc.Name
	}
	mqttMgr.SetPLCNames(plcNames)

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
	app.SetPackManager(packMgrDaemon)
	apiServer.SetPackManager(packMgrDaemon)

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

	// Set up debug logging if specified
	// Supports: --log-debug=all (or true/1) for all protocols
	//           --log-debug=omron,ads for specific protocols
	var debugLogger *logging.DebugLogger
	if *logDebug != "" {
		var err error
		debugLogger, err = logging.NewDebugLogger("debug.log")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to open debug log: %v\n", err)
		} else {
			// Parse filter: "all", "true", "1" = no filter, otherwise use as protocol filter
			filter := *logDebug
			if filter == "all" || filter == "true" || filter == "1" {
				filter = "" // Empty = log all
			}
			debugLogger.SetFilter(filter)
			logging.SetGlobalDebugLogger(debugLogger)
			if filter == "" {
				tui.DebugLog("Debug logging enabled (all protocols) - writing to debug.log")
			} else {
				tui.DebugLog("Debug logging enabled (filter: %s) - writing to debug.log", filter)
			}
		}
	}

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
	// In PTY multiplexing mode, all sessions share the same view, so Shift-Q disconnects everyone
	app.SetOnDisconnect(func() {
		tui.DebugLogSSH("Disconnect requested via Shift-Q")
		sshServer.DisconnectAllSessions()
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

	// Graceful shutdown with timeout
	shutdownDone := make(chan struct{})
	go func() {
		// Close PTY first to unblock TUI's event loop
		sshServer.DisconnectAllSessions()
		sshServer.Stop()

		// Now shutdown the app (TUI exits quickly since PTY is closed)
		app.Shutdown()

		close(shutdownDone)
	}()

	// Wait for shutdown with timeout
	select {
	case <-shutdownDone:
		// Clean shutdown
	case <-time.After(1 * time.Second):
		// Timeout - proceed to exit anyway
	}

	if fileLogger != nil {
		fileLogger.Close()
	}
	if debugLogger != nil {
		debugLogger.Close()
	}

	fmt.Println("Daemon stopped")
}

// setupValueChangeHandlers sets up the value change callback for publishing to MQTT, Valkey, and Kafka.
func setupValueChangeHandlers(manager *plcman.Manager, mqttMgr *mqtt.Manager, valkeyMgr *valkey.Manager, kafkaMgr *kafka.Manager, packMgr *tagpack.Manager) {
	manager.SetOnValueChange(func(changes []plcman.ValueChange) {
		mqttRunning := mqttMgr.AnyRunning()
		valkeyRunning := valkeyMgr.AnyRunning()
		kafkaPublishing := kafkaMgr.AnyPublishing()

		tui.DebugLog("OnValueChange: %d changes, MQTT: %v, Valkey: %v, Kafka: %v",
			len(changes), mqttRunning, valkeyRunning, kafkaPublishing)

		changesCopy := make([]plcman.ValueChange, len(changes))
		copy(changesCopy, changes)

		// Notify TagPack manager of changes (grouped by PLC)
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
	})
}

// setupWriteHandlers sets up MQTT, Valkey, and Kafka write handling.
func setupWriteHandlers(cfg *config.Config, manager *plcman.Manager, mqttMgr *mqtt.Manager, valkeyMgr *valkey.Manager, kafkaMgr *kafka.Manager) {
	// Shared write handler - all services use the same PLC manager
	writeHandler := func(plcName, tagName string, value interface{}) error {
		return manager.WriteTag(plcName, tagName, value)
	}

	// Shared write validator - checks if tag is configured as writable
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

	// Shared tag type lookup
	tagTypeLookup := func(plcName, tagName string) uint16 {
		return manager.GetTagType(plcName, tagName)
	}

	// MQTT write handling
	mqttMgr.SetWriteHandler(writeHandler)
	mqttMgr.SetWriteValidator(writeValidator)
	mqttMgr.SetTagTypeLookup(tagTypeLookup)

	// Valkey write handling
	valkeyMgr.SetWriteHandler(writeHandler)
	valkeyMgr.SetWriteValidator(writeValidator)
	valkeyMgr.SetTagTypeLookup(tagTypeLookup)

	// Kafka write handling
	kafkaMgr.SetWriteHandler(writeHandler)
	kafkaMgr.SetWriteValidator(writeValidator)
	kafkaMgr.SetTagTypeLookup(tagTypeLookup)
}

// plcDataProvider implements tagpack.PLCDataProvider using the PLC manager.
type plcDataProvider struct {
	manager *plcman.Manager
}

// GetTagValue returns the current value, type name, and alias for a tag.
func (p *plcDataProvider) GetTagValue(plcName, tagName string) (value interface{}, typeName, alias string, ok bool) {
	vc := p.manager.GetTagValueChange(plcName, tagName)
	if vc == nil {
		return nil, "", "", false
	}
	return vc.Value, vc.TypeName, vc.Alias, true
}

// GetPLCMetadata returns metadata about a PLC.
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
