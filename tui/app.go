package tui

import (
	"os"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"warlogix/api"
	"warlogix/config"
	"warlogix/kafka"
	"warlogix/mqtt"
	"warlogix/plcman"
	"warlogix/trigger"
	"warlogix/valkey"
)

// App is the main TUI application.
type App struct {
	app            *tview.Application
	pages          *tview.Pages
	tabs           *tview.TextView
	statusBar      *tview.TextView
	themeIndicator *tview.TextView

	plcsTab     *PLCsTab
	browserTab  *BrowserTab
	restTab     *RESTTab
	mqttTab     *MQTTTab
	valkeyTab   *ValkeyTab
	kafkaTab    *KafkaTab
	triggersTab *TriggersTab
	debugTab    *DebugTab

	manager    *plcman.Manager
	apiServer  *api.Server
	mqttMgr    *mqtt.Manager
	valkeyMgr  *valkey.Manager
	kafkaMgr   *kafka.Manager
	triggerMgr *trigger.Manager
	config     *config.Config
	configPath string

	currentTab int
	tabNames   []string

	stopChan chan struct{}

	// Daemon mode support
	daemonMode       bool
	onDisconnect     func() // Called when user requests disconnect in daemon mode
	onShutdownDaemon func() // Called when daemon needs to shutdown
}

// NewApp creates a new TUI application.
func NewApp(cfg *config.Config, configPath string, manager *plcman.Manager, apiServer *api.Server, mqttMgr *mqtt.Manager, valkeyMgr *valkey.Manager, kafkaMgr *kafka.Manager, triggerMgr *trigger.Manager) *App {
	// Apply theme from config
	if cfg.UI.Theme != "" {
		SetTheme(cfg.UI.Theme)
	}

	a := &App{
		app:        tview.NewApplication(),
		config:     cfg,
		configPath: configPath,
		manager:    manager,
		apiServer:  apiServer,
		mqttMgr:    mqttMgr,
		valkeyMgr:  valkeyMgr,
		kafkaMgr:   kafkaMgr,
		triggerMgr: triggerMgr,
		tabNames:   []string{TabPLCs, TabBrowser, TabREST, TabMQTT, TabValkey, TabKafka, TabTriggers, TabDebug},
		stopChan:   make(chan struct{}),
	}

	a.setupUI()
	return a
}

// NewAppWithScreen creates a TUI application that uses the provided tcell.Screen.
// This is used for daemon mode where the TUI runs on a PTY.
func NewAppWithScreen(cfg *config.Config, configPath string, manager *plcman.Manager, apiServer *api.Server, mqttMgr *mqtt.Manager, valkeyMgr *valkey.Manager, kafkaMgr *kafka.Manager, triggerMgr *trigger.Manager, screen tcell.Screen) *App {
	// Apply theme from config
	if cfg.UI.Theme != "" {
		SetTheme(cfg.UI.Theme)
	}

	a := &App{
		app:        tview.NewApplication().SetScreen(screen),
		config:     cfg,
		configPath: configPath,
		manager:    manager,
		apiServer:  apiServer,
		mqttMgr:    mqttMgr,
		valkeyMgr:  valkeyMgr,
		kafkaMgr:   kafkaMgr,
		triggerMgr: triggerMgr,
		tabNames:   []string{TabPLCs, TabBrowser, TabREST, TabMQTT, TabValkey, TabKafka, TabTriggers, TabDebug},
		stopChan:   make(chan struct{}),
		daemonMode: true,
	}

	a.setupUI()
	return a
}

// NewAppWithPTY creates a TUI application that uses the provided PTY file descriptors.
// This is used for daemon mode where the TUI runs on a PTY for SSH multiplexing.
func NewAppWithPTY(cfg *config.Config, configPath string, manager *plcman.Manager, apiServer *api.Server, mqttMgr *mqtt.Manager, valkeyMgr *valkey.Manager, kafkaMgr *kafka.Manager, triggerMgr *trigger.Manager, ptyFile *os.File) (*App, error) {
	// Apply theme from config
	if cfg.UI.Theme != "" {
		SetTheme(cfg.UI.Theme)
	}

	// Create a PTYTty wrapper that implements tcell.Tty
	ptyTty := NewPTYTty(ptyFile)

	// Create a tcell screen using the PTY
	screen, err := tcell.NewTerminfoScreenFromTty(ptyTty)
	if err != nil {
		return nil, err
	}

	if err := screen.Init(); err != nil {
		return nil, err
	}

	a := &App{
		app:        tview.NewApplication().SetScreen(screen),
		config:     cfg,
		configPath: configPath,
		manager:    manager,
		apiServer:  apiServer,
		mqttMgr:    mqttMgr,
		valkeyMgr:  valkeyMgr,
		kafkaMgr:   kafkaMgr,
		triggerMgr: triggerMgr,
		tabNames:   []string{TabPLCs, TabBrowser, TabREST, TabMQTT, TabValkey, TabKafka, TabTriggers, TabDebug},
		stopChan:   make(chan struct{}),
		daemonMode: true,
	}

	a.setupUI()
	return a, nil
}

// SetDaemonMode sets whether the app is running in daemon mode.
func (a *App) SetDaemonMode(daemon bool) {
	a.daemonMode = daemon
}

// SetOnDisconnect sets a callback for when the user requests disconnect in daemon mode.
func (a *App) SetOnDisconnect(fn func()) {
	a.onDisconnect = fn
}

// SetOnShutdownDaemon sets a callback for when the daemon needs to shutdown.
func (a *App) SetOnShutdownDaemon(fn func()) {
	a.onShutdownDaemon = fn
}

func (a *App) setupUI() {
	// Create tabs header
	a.tabs = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)

	// Create status bar
	a.statusBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft).
		SetTextColor(CurrentTheme.Text)

	// Create theme indicator (bottom right)
	// Dynamic colors disabled to ensure all theme names display correctly
	a.themeIndicator = tview.NewTextView().
		SetTextAlign(tview.AlignRight)
	a.updateThemeIndicator()

	// Create pages for tab content
	a.pages = tview.NewPages()

	// Create tab contents
	a.plcsTab = NewPLCsTab(a)
	a.browserTab = NewBrowserTab(a)
	a.restTab = NewRESTTab(a)
	a.mqttTab = NewMQTTTab(a)
	a.valkeyTab = NewValkeyTab(a)
	a.kafkaTab = NewKafkaTab(a)
	a.triggersTab = NewTriggersTab(a)
	a.debugTab = NewDebugTab(a)

	// Add pages
	a.pages.AddPage(TabPLCs, a.plcsTab.GetPrimitive(), true, true)
	a.pages.AddPage(TabBrowser, a.browserTab.GetPrimitive(), true, false)
	a.pages.AddPage(TabREST, a.restTab.GetPrimitive(), true, false)
	a.pages.AddPage(TabMQTT, a.mqttTab.GetPrimitive(), true, false)
	a.pages.AddPage(TabValkey, a.valkeyTab.GetPrimitive(), true, false)
	a.pages.AddPage(TabKafka, a.kafkaTab.GetPrimitive(), true, false)
	a.pages.AddPage(TabTriggers, a.triggersTab.GetPrimitive(), true, false)
	a.pages.AddPage(TabDebug, a.debugTab.GetPrimitive(), true, false)

	// Create bottom bar with status (left) and theme indicator (right)
	// Width 30 to accommodate "Theme (F6): highcontrast " (longest theme name)
	bottomBar := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(a.statusBar, 0, 1, false).
		AddItem(a.themeIndicator, 30, 0, false)

	// Create main layout
	mainFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(a.tabs, 1, 0, false).
		AddItem(a.pages, 0, 1, true).
		AddItem(bottomBar, 1, 0, false)

	// Set up key handling
	a.app.SetInputCapture(a.handleGlobalKeys)

	// Set root and focus
	a.app.SetRoot(mainFlex, true)
	a.updateTabsDisplay()
	a.setStatus("Ready. Press ? for help.")

	// Focus on first tab's main element
	a.focusCurrentTab()
}

func (a *App) handleGlobalKeys(event *tcell.EventKey) *tcell.EventKey {
	if event == nil {
		return nil
	}

	// Check if a modal is showing - if so, let the modal handle keys
	frontPage, _ := a.pages.GetFrontPage()

	// List of known tab pages - anything else is considered a modal
	isMainTab := frontPage == TabPLCs || frontPage == TabBrowser || frontPage == TabREST || frontPage == TabMQTT || frontPage == TabValkey || frontPage == TabKafka || frontPage == TabTriggers || frontPage == TabDebug

	// Don't intercept keys (including Shift-Q) when a modal/form is open
	if !isMainTab {
		return event
	}

	// Handle quit: Shift+Q (uppercase Q) - only when not in a modal
	if event.Rune() == 'Q' {
		if a.daemonMode {
			// In daemon mode, Shift-Q disconnects the session, not quits the daemon
			if a.onDisconnect != nil {
				a.onDisconnect()
			}
		} else {
			a.Shutdown()
		}
		return nil
	}

	// Tab switching with Shift+Tab only
	if event.Key() == tcell.KeyBacktab {
		a.nextTab()
		return nil
	}

	// Check for help
	if event.Rune() == '?' {
		a.showHelp()
		return nil
	}

	// F6: Cycle through themes
	if event.Key() == tcell.KeyF6 {
		themeName := NextTheme()
		a.updateTabsDisplay()
		a.updateThemeIndicator()
		// Refresh all tabs to apply new theme colors
		a.refreshAllThemes()
		// Save theme preference to config
		a.config.UI.Theme = themeName
		a.SaveConfig()
		// Force full redraw to apply theme changes
		a.app.Sync()
		return nil
	}

	// Let the current tab handle the key
	return event
}

func (a *App) nextTab() {
	a.currentTab = (a.currentTab + 1) % len(a.tabNames)
	a.switchToTab(a.currentTab)
}

func (a *App) prevTab() {
	a.currentTab--
	if a.currentTab < 0 {
		a.currentTab = len(a.tabNames) - 1
	}
	a.switchToTab(a.currentTab)
}

func (a *App) switchToTab(index int) {
	a.currentTab = index
	a.pages.SwitchToPage(a.tabNames[index])
	a.updateTabsDisplay()
	a.focusCurrentTab()
}

func (a *App) focusCurrentTab() {
	switch a.currentTab {
	case 0:
		a.app.SetFocus(a.plcsTab.GetFocusable())
	case 1:
		a.app.SetFocus(a.browserTab.GetFocusable())
	case 2:
		a.app.SetFocus(a.restTab.GetFocusable())
	case 3:
		a.app.SetFocus(a.mqttTab.GetFocusable())
	case 4:
		a.app.SetFocus(a.valkeyTab.GetFocusable())
	case 5:
		a.app.SetFocus(a.kafkaTab.GetFocusable())
	case 6:
		a.app.SetFocus(a.triggersTab.GetFocusable())
	case 7:
		a.app.SetFocus(a.debugTab.GetFocusable())
	}
}

func (a *App) updateTabsDisplay() {
	th := CurrentTheme
	text := ""
	for i, name := range a.tabNames {
		if i > 0 {
			text += th.TagTextDim + "  │  " + th.TagReset
		}
		if i == a.currentTab {
			// TagAccent is "[#RRGGBB]", need to insert "::b" before the closing bracket
			colorTag := th.TagAccent[:len(th.TagAccent)-1] + "::b]"
			text += colorTag + name + "[-::-]"
		} else {
			text += th.TagTextDim + name + th.TagReset
		}
	}
	a.tabs.SetText(text)
	a.tabs.SetTextColor(th.Text)
}

func (a *App) setStatus(msg string) {
	a.statusBar.SetText(" " + msg)
}

func (a *App) updateThemeIndicator() {
	th := CurrentTheme
	themeName := GetThemeName()
	// Simple text without color tags - use SetTextColor for the color
	a.themeIndicator.SetText("Theme (F6): " + themeName + " ")
	a.themeIndicator.SetTextColor(th.TextDim)
	a.statusBar.SetTextColor(th.Text)
}

func (a *App) showHelp() {
	const pageName = "help"

	helpText := HelpText
	if a.daemonMode {
		helpText = HelpTextDaemon
	}
	textView := tview.NewTextView().
		SetText(helpText).
		SetDynamicColors(true)
	textView.SetBorder(true).SetTitle(" Help ")

	textView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape || event.Key() == tcell.KeyEnter || event.Rune() == '?' {
			a.closeModal(pageName)
			return nil
		}
		return event
	})

	a.showCenteredModal(pageName, textView, 45, 24)
}

func (a *App) showError(title, message string) {
	a.showErrorWithFocus(title, message, nil)
}

// showErrorWithFocus shows an error dialog and restores focus to the given primitive when dismissed.
// If focusTarget is nil, it focuses the current tab.
func (a *App) showErrorWithFocus(title, message string, focusTarget tview.Primitive) {
	modal := tview.NewModal().
		SetText(title + "\n\n" + message).
		AddButtons([]string{"OK"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			a.pages.RemovePage("error")
			if focusTarget != nil {
				a.app.SetFocus(focusTarget)
			} else {
				a.focusCurrentTab()
			}
		})

	a.pages.AddPage("error", modal, true, true)
}

func (a *App) showConfirm(title, message string, onConfirm func()) {
	modal := tview.NewModal().
		SetText(title + "\n\n" + message).
		AddButtons([]string{"Yes", "No"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			a.pages.RemovePage("confirm")
			if buttonIndex == 0 {
				onConfirm()
			}
			a.focusCurrentTab()
		})

	a.pages.AddPage("confirm", modal, true, true)
}

// SaveConfig saves the current configuration.
func (a *App) SaveConfig() error {
	return a.config.Save(a.configPath)
}

// Run starts the TUI application.
func (a *App) Run() error {
	// Set up manager change callback to trigger UI updates
	a.manager.SetOnChange(func() {
		a.app.QueueUpdateDraw(func() {
			a.plcsTab.Refresh()
			a.browserTab.Refresh()
		})
	})

	// Set up manager logging to go to debug panel
	a.manager.SetOnLog(func(format string, args ...interface{}) {
		DebugLog(format, args...)
	})

	// Refresh all tabs to reflect current state after auto-connect/auto-start
	a.plcsTab.Refresh()
	a.browserTab.Refresh()
	a.mqttTab.Refresh()
	a.valkeyTab.Refresh()
	a.kafkaTab.Refresh()
	a.triggersTab.Refresh()
	a.restTab.Refresh()

	// Start periodic refresh goroutine for MQTT, Valkey, and Debug tabs
	go a.periodicRefresh()

	// Start health publishing goroutine (publishes every 10 seconds)
	go a.publishHealthLoop()

	return a.app.Run()
}

// periodicRefresh periodically refreshes tabs that need updates from background goroutines.
func (a *App) periodicRefresh() {
	// Wait for the app to fully start
	time.Sleep(500 * time.Millisecond)

	for {
		select {
		case <-a.stopChan:
			return
		case <-time.After(1 * time.Second):
			a.app.QueueUpdateDraw(func() {
				// Skip refresh if a modal dialog is open to avoid interference with form input
				frontPage, _ := a.pages.GetFrontPage()
				isModalOpen := frontPage != TabPLCs && frontPage != TabBrowser &&
					frontPage != TabREST && frontPage != TabMQTT &&
					frontPage != TabValkey && frontPage != TabKafka &&
					frontPage != TabTriggers && frontPage != TabDebug

				// Only refresh if tabs are initialized and no modal is open
				if a.debugTab != nil {
					a.debugTab.Refresh()
				}
				// Skip table refreshes when a modal dialog is open
				if isModalOpen {
					return
				}
				if a.mqttTab != nil && a.currentTab == 3 {
					a.mqttTab.Refresh()
				}
				if a.valkeyTab != nil && a.currentTab == 4 {
					a.valkeyTab.Refresh()
				}
				if a.kafkaTab != nil && a.currentTab == 5 {
					a.kafkaTab.Refresh()
				}
				if a.triggersTab != nil && a.currentTab == 6 {
					a.triggersTab.Refresh()
				}
			})
		}
	}
}

// publishHealthLoop publishes PLC health status to all services every 10 seconds.
func (a *App) publishHealthLoop() {
	// Wait for initial services to start
	time.Sleep(2 * time.Second)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// Publish immediately on start, then every 10 seconds
	a.publishAllHealth()

	for {
		select {
		case <-a.stopChan:
			return
		case <-ticker.C:
			a.publishAllHealth()
		}
	}
}

// publishAllHealth publishes health status for all PLCs to MQTT, Valkey, and Kafka.
func (a *App) publishAllHealth() {
	plcs := a.manager.ListPLCs()
	DebugLog("Publishing health for %d PLCs", len(plcs))
	for _, plc := range plcs {
		// Skip PLCs with health check disabled
		if !plc.Config.IsHealthCheckEnabled() {
			continue
		}

		health := plc.GetHealthStatus()

		// Publish to MQTT
		if a.mqttMgr != nil {
			a.mqttMgr.PublishHealth(plc.Config.Name, health.Driver, health.Online, health.Status, health.Error)
		}

		// Publish to Valkey
		if a.valkeyMgr != nil {
			a.valkeyMgr.PublishHealth(plc.Config.Name, health.Driver, health.Online, health.Status, health.Error)
		}

		// Publish to Kafka
		if a.kafkaMgr != nil {
			a.kafkaMgr.PublishHealth(plc.Config.Name, health.Driver, health.Online, health.Status, health.Error)
		}
	}
}

// Shutdown performs a clean shutdown of all resources.
func (a *App) Shutdown() {
	// Stop periodic refresh goroutine first
	select {
	case <-a.stopChan:
		// Already closed
	default:
		close(a.stopChan)
	}

	// Clear callbacks to prevent updates during shutdown
	a.manager.SetOnChange(nil)
	a.manager.SetOnValueChange(nil)
	a.manager.SetOnLog(nil)

	// Stop the TUI immediately to prevent writes to closed PTY
	// This is non-blocking - it just signals the event loop to stop
	a.app.Stop()

	// Stop all services with a single timeout
	// All these operations can potentially block, so wrap them together
	done := make(chan struct{})
	go func() {
		// Stop triggers first (they may be waiting on PLC reads or Kafka writes)
		if a.triggerMgr != nil {
			a.triggerMgr.Stop()
		}

		// Stop messaging services
		a.mqttMgr.StopAll()
		a.valkeyMgr.StopAll()
		if a.kafkaMgr != nil {
			a.kafkaMgr.StopAll()
		}

		// Stop API and manager
		a.apiServer.Stop()
		a.manager.Stop()

		close(done)
	}()

	// Wait with timeout for all services (reduced to allow room in outer 2s timeout)
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		// Timeout - proceed anyway
	}

	// Disconnect PLCs in background (don't wait - can be slow)
	go a.manager.DisconnectAll()
}

// Stop halts the TUI application.
func (a *App) Stop() {
	a.app.Stop()
}

// ShutdownDaemon performs a full daemon shutdown.
// This is called when SIGTERM/SIGINT is received in daemon mode.
func (a *App) ShutdownDaemon() {
	if a.onShutdownDaemon != nil {
		a.onShutdownDaemon()
	}
	a.Shutdown()
}

// IsDaemonMode returns whether the app is running in daemon mode.
func (a *App) IsDaemonMode() bool {
	return a.daemonMode
}

// QueueUpdateDraw queues a function to run on the UI thread.
func (a *App) QueueUpdateDraw(f func()) {
	a.app.QueueUpdateDraw(f)
}

// ForcePublishTag publishes a single tag's current value to all services (MQTT, Valkey, Kafka).
// This is called when a tag is newly enabled to publish its current value immediately.
func (a *App) ForcePublishTag(plcName, tagName string) {
	v := a.manager.GetTagValueChange(plcName, tagName)
	if v == nil {
		return
	}

	DebugLog("ForcePublishTag: publishing %s.%s", plcName, tagName)

	// Publish to all services with force=true
	a.mqttMgr.Publish(v.PLCName, v.TagName, v.Alias, v.Address, v.TypeName, v.Value, true)
	a.valkeyMgr.Publish(v.PLCName, v.TagName, v.Alias, v.Address, v.TypeName, v.Value, v.Writable)
	a.kafkaMgr.Publish(v.PLCName, v.TagName, v.Alias, v.Address, v.TypeName, v.Value, v.Writable, true)
}

// ForcePublishAllValues publishes all current tag values to MQTT brokers.
// This is called when an MQTT broker connects to do an initial sync.
func (a *App) ForcePublishAllValues() {
	values := a.manager.GetAllCurrentValues()
	DebugLogMQTT("ForcePublishAllValues: publishing %d values", len(values))
	for _, v := range values {
		a.mqttMgr.Publish(v.PLCName, v.TagName, v.Alias, v.Address, v.TypeName, v.Value, true)
	}
}

// ForcePublishAllValuesToValkey publishes all current tag values to Valkey servers.
// This is called when a Valkey server connects to do an initial sync.
func (a *App) ForcePublishAllValuesToValkey() {
	values := a.manager.GetAllCurrentValues()
	DebugLogValkey("ForcePublishAllValuesToValkey: publishing %d values", len(values))
	for _, v := range values {
		a.valkeyMgr.Publish(v.PLCName, v.TagName, v.Alias, v.Address, v.TypeName, v.Value, v.Writable)
	}
}

// ForcePublishAllValuesToKafka publishes all current tag values to Kafka clusters.
// This is called when a Kafka cluster connects with PublishChanges enabled.
func (a *App) ForcePublishAllValuesToKafka() {
	values := a.manager.GetAllCurrentValues()
	DebugLog("ForcePublishAllValuesToKafka: publishing %d values", len(values))
	for _, v := range values {
		a.kafkaMgr.Publish(v.PLCName, v.TagName, v.Alias, v.Address, v.TypeName, v.Value, v.Writable, true)
	}
}

// UpdateMQTTPLCNames updates the MQTT manager with current PLC names.
// Call this when PLCs are added or removed.
func (a *App) UpdateMQTTPLCNames() {
	plcNames := make([]string, len(a.config.PLCs))
	for i, plc := range a.config.PLCs {
		plcNames[i] = plc.Name
	}
	a.mqttMgr.SetPLCNames(plcNames)
	a.mqttMgr.UpdateWriteSubscriptions()
}

// showCenteredModal displays a modal dialog centered on the screen.
// pageName is the unique identifier for this modal in the pages stack.
// content is the tview primitive to display.
// width and height are the dimensions of the modal.
// The content will receive focus automatically.
func (a *App) showCenteredModal(pageName string, content tview.Primitive, width, height int) {
	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(content, height, 1, true).
			AddItem(nil, 0, 1, false), width, 1, true).
		AddItem(nil, 0, 1, false)

	a.pages.AddPage(pageName, modal, true, true)
	a.app.SetFocus(content)
}

// showFormModal displays a form in a centered modal dialog.
// pageName is the unique identifier for this modal.
// form is the form to display.
// width and height are the dimensions of the modal.
// onEscape is called when Escape is pressed (typically to close the modal).
func (a *App) showFormModal(pageName string, form *tview.Form, width, height int, onEscape func()) {
	form.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			if onEscape != nil {
				onEscape()
			}
			return nil
		}
		return event
	})

	a.showCenteredModal(pageName, form, width, height)
}

// closeModal removes a modal from the pages stack and restores focus to the current tab.
func (a *App) closeModal(pageName string) {
	a.pages.RemovePage(pageName)
	a.focusCurrentTab()
}

// refreshAllThemes calls RefreshTheme on all tabs to apply theme changes.
func (a *App) refreshAllThemes() {
	if a.plcsTab != nil {
		a.plcsTab.RefreshTheme()
	}
	if a.browserTab != nil {
		a.browserTab.RefreshTheme()
	}
	if a.mqttTab != nil {
		a.mqttTab.RefreshTheme()
	}
	if a.valkeyTab != nil {
		a.valkeyTab.RefreshTheme()
	}
	if a.kafkaTab != nil {
		a.kafkaTab.RefreshTheme()
	}
	if a.triggersTab != nil {
		a.triggersTab.RefreshTheme()
	}
	if a.restTab != nil {
		a.restTab.RefreshTheme()
	}
	if a.debugTab != nil {
		a.debugTab.RefreshTheme()
	}
}
