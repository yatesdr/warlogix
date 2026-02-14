// Warlink - PLC Gateway TUI Application
//
// A text user interface for managing PLC connections, browsing tags,
// and republishing data via REST API and MQTT.
package main

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"

	"warlink/brokertest"
	"warlink/config"
	"warlink/engine"
	"warlink/logging"
	"warlink/ssh"
	"warlink/tui"
	"warlink/web"
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
	configPath  = flag.String("config", config.DefaultPath(), "Path to configuration file")
	showVersion = flag.Bool("version", false, "Show version and exit")
	noTUI       = flag.Bool("d", false, "Disable local TUI (headless mode)")
	noTUILong   = flag.Bool("no-tui", false, "Disable local TUI (headless mode)")
	namespace   = flag.String("namespace", "", "Set namespace (saved to config)")
	httpPort    = flag.Int("p", 0, "HTTP listen port (overrides config)")
	httpHost    = flag.String("host", "", "HTTP bind address (overrides config)")
	adminUser   = flag.String("admin-user", "", "Create/update admin user (saves to config)")
	adminPass   = flag.String("admin-pass", "", "Password for admin user (saves to config)")
	sshPortFlag = flag.Int("ssh-port", 2222, "SSH listen port")
	sshPass     = flag.String("ssh-pass", "", "SSH password for remote TUI access")
	sshKeys     = flag.String("ssh-keys", "", "Path to authorized_keys file or directory")
	noAPI       = flag.Bool("no-api", false, "Disable REST API (ephemeral)")
	noWebUI     = flag.Bool("no-webui", false, "Disable browser UI (ephemeral)")
	logFile     = flag.String("log", "", "Path to log file (optional)")
	logDebug    = flag.String("log-debug", "", "Enable debug logging to debug.log")

	// Stress test flags
	testBrokers  = flag.Bool("stress-test-republishing", false, "Run stress tests for republishing and exit")
	testDuration = flag.Duration("test-duration", 10*time.Second, "Duration for each broker stress test")
	testTags     = flag.Int("test-tags", 100, "Number of simulated tags for stress test")
	testPLCs     = flag.Int("test-plcs", 50, "Number of simulated PLCs for stress test")
	testYes      = flag.Bool("y", false, "Skip confirmation prompt for stress tests")
)

func main() {
	// Pre-process args to handle --log-debug without a value
	preprocessLogDebugFlag()

	flag.Parse()

	if *showVersion {
		fmt.Printf("warlink %s\n", Version)
		os.Exit(0)
	}

	// Merge -d and --no-tui
	headless := *noTUI || *noTUILong

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

	// Override web config from flags (in memory only)
	if *httpPort != 0 {
		cfg.Web.Port = *httpPort
	}
	if *httpHost != "" {
		cfg.Web.Host = *httpHost
	}
	if *noAPI {
		cfg.Web.API.Enabled = false
	}
	if *noWebUI {
		cfg.Web.UI.Enabled = false
	}
	if *noAPI && *noWebUI {
		cfg.Web.Enabled = false
	}

	// Create/update admin user if credentials provided (persisted)
	if *adminUser != "" && *adminPass != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(*adminPass), bcrypt.DefaultCost)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error hashing password: %v\n", err)
			os.Exit(1)
		}

		if existing := cfg.FindWebUser(*adminUser); existing != nil {
			existing.PasswordHash = string(hash)
			existing.Role = config.RoleAdmin
			existing.MustChangePassword = false
		} else {
			cfg.AddWebUser(config.WebUser{
				Username:     *adminUser,
				PasswordHash: string(hash),
				Role:         config.RoleAdmin,
			})
		}

		// Generate session secret if not set
		if cfg.Web.UI.SessionSecret == "" {
			secret := make([]byte, 32)
			rand.Read(secret)
			cfg.Web.UI.SessionSecret = base64.StdEncoding.EncodeToString(secret)
		}

		if err := cfg.Save(*configPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Admin user '%s' configured for web UI\n", *adminUser)
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

	run(cfg, headless)
}

// run is the unified startup flow for both TUI and headless modes.
func run(cfg *config.Config, headless bool) {
	// Initialize shared debug store (always — TUI + SSH can coexist)
	tui.InitDebugStore(1000)

	// Create and start the engine (replaces all manager creation, wiring, and orchestration)
	eng := engine.New(engine.Config{
		AppConfig:  cfg,
		ConfigPath: *configPath,
		LogFunc:    tui.StoreLog,
	})
	eng.Start()

	// Set up file logging if specified
	var fileLogger *logging.FileLogger
	if *logFile != "" {
		var err error
		fileLogger, err = logging.NewFileLogger(*logFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to open log file: %v\n", err)
		} else {
			store := tui.GetDebugStore()
			if store != nil {
				store.SetFileLogger(fileLogger)
			}
			if !headless {
				tui.SetDebugFileLogger(fileLogger)
			}
		}
	}

	// Set up debug logging if specified
	var debugLoggerFile *logging.DebugLogger
	if *logDebug != "" {
		var err error
		debugLoggerFile, err = logging.NewDebugLogger("debug.log")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to open debug log: %v\n", err)
		} else {
			filter := *logDebug
			if filter == "all" || filter == "true" || filter == "1" {
				filter = ""
			}
			debugLoggerFile.SetFilter(filter)
			logging.SetGlobalDebugLogger(debugLoggerFile)
			if filter == "" {
				tui.StoreLog("Debug logging enabled (all protocols) - writing to debug.log")
			} else {
				tui.StoreLog("Debug logging enabled (filter: %s) - writing to debug.log", filter)
			}
		}
	}

	// Start HTTP server (unless disabled)
	// Use the tui.WebServer interface type so a nil stays a true nil interface
	// (a typed nil *web.Server would become a non-nil interface and cause panics).
	var webServer tui.WebServer
	if cfg.Web.Enabled {
		ws := web.NewServer(&cfg.Web, eng)
		if err := ws.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to start web server on port %d: %v\n", cfg.Web.Port, err)
			fmt.Fprintf(os.Stderr, "Continuing without HTTP server.\n")
		} else {
			webServer = ws
			fmt.Printf("Web server at %s\n", webServer.Address())
			if cfg.Web.API.Enabled {
				fmt.Printf("  REST API: %s/api/\n", webServer.Address())
			}
			if cfg.Web.UI.Enabled {
				if len(cfg.Web.UI.Users) == 0 {
					fmt.Printf("  First-time setup: %s/setup\n", webServer.Address())
				} else {
					fmt.Printf("  Browser UI: %s/\n", webServer.Address())
				}
			}
		}
	}

	// Start SSH server if credentials provided
	var sshServer *ssh.Server
	if *sshPass != "" || *sshKeys != "" {
		sharedManagers := &ssh.SharedManagers{
			Config:     cfg,
			ConfigPath: *configPath,
			Engine:     eng,
			WebServer:  webServer,
		}

		sshServer = ssh.NewServer(&ssh.Config{
			Port:           *sshPortFlag,
			Password:       *sshPass,
			AuthorizedKeys: *sshKeys,
		})
		sshServer.SetSharedManagers(sharedManagers)

		sshServer.SetOnSessionConnect(func(remoteAddr string) {
			tui.StoreLogSSH("Client connected from %s (total sessions: %d)", remoteAddr, sshServer.SessionCount())
		})
		sshServer.SetOnSessionDisconnect(func(remoteAddr string) {
			tui.StoreLogSSH("Client disconnected from %s (total sessions: %d)", remoteAddr, sshServer.SessionCount())
		})

		if err := sshServer.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to start SSH server: %v\n", err)
			sshServer = nil
		} else {
			fmt.Printf("SSH server on port %d\n", *sshPortFlag)
		}
	}

	if headless {
		// Headless mode: block on signal
		if sshServer == nil {
			fmt.Fprintf(os.Stderr, "Warning: Running headless with no SSH. Use --ssh-pass for remote access.\n")
		}

		fmt.Println("Running in headless mode. Press Ctrl+C to stop.")

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigChan
		fmt.Printf("\nReceived %v, shutting down...\n", sig)

		// Graceful shutdown
		shutdownDone := make(chan struct{})
		go func() {
			if sshServer != nil {
				sshServer.DisconnectAllSessions()
				sshServer.Stop()
			}
			if webServer != nil {
				webServer.Stop()
			}
			eng.Stop()
			close(shutdownDone)
		}()

		select {
		case <-shutdownDone:
		case <-time.After(2 * time.Second):
		}

		if fileLogger != nil {
			fileLogger.Close()
		}
		if debugLoggerFile != nil {
			debugLoggerFile.Close()
		}

		fmt.Println("Stopped")
	} else {
		// TUI mode: redirect stderr to a file to prevent runtime errors
		// (e.g. data races, panics) from corrupting the terminal display.
		stderrPath := filepath.Join(filepath.Dir(*configPath), "warlink-crash.log")
		if f, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
			redirectStderr(f)
			defer f.Close()
		}

		app := tui.NewApp(cfg, eng, webServer)

		// Set debug file logger for TUI mode if not already set above
		if fileLogger != nil {
			tui.SetDebugFileLogger(fileLogger)
		}

		if err := app.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Shutdown web server on TUI exit
		if webServer != nil {
			webServer.Stop()
		}
		if sshServer != nil {
			sshServer.DisconnectAllSessions()
			sshServer.Stop()
		}

		if fileLogger != nil {
			fileLogger.Close()
		}
		if debugLoggerFile != nil {
			debugLoggerFile.Close()
		}
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

	if !*testYes {
		fmt.Println()
		fmt.Println("WARNING: Stress test")
		fmt.Println()
		fmt.Printf("This will stress test %d broker(s) for %v:\n\n", enabledCount, *testDuration)
		for _, b := range brokerList {
			fmt.Printf("  - %s\n", b)
		}
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

	for _, result := range results {
		if !result.Success {
			os.Exit(1)
		}
	}
}

