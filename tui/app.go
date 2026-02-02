package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"warlogix/api"
	"warlogix/config"
	"warlogix/mqtt"
	"warlogix/plcman"
)

// App is the main TUI application.
type App struct {
	app       *tview.Application
	pages     *tview.Pages
	tabs      *tview.TextView
	statusBar *tview.TextView

	plcsTab    *PLCsTab
	browserTab *BrowserTab
	restTab    *RESTTab
	mqttTab    *MQTTTab
	debugTab   *DebugTab

	manager    *plcman.Manager
	apiServer  *api.Server
	mqttMgr    *mqtt.Manager
	config     *config.Config
	configPath string

	currentTab int
	tabNames   []string
}

// NewApp creates a new TUI application.
func NewApp(cfg *config.Config, configPath string, manager *plcman.Manager, apiServer *api.Server, mqttMgr *mqtt.Manager) *App {
	a := &App{
		app:        tview.NewApplication(),
		config:     cfg,
		configPath: configPath,
		manager:    manager,
		apiServer:  apiServer,
		mqttMgr:    mqttMgr,
		tabNames:   []string{TabPLCs, TabBrowser, TabREST, TabMQTT, TabDebug},
	}

	a.setupUI()
	return a
}

func (a *App) setupUI() {
	// Create tabs header
	a.tabs = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)

	// Create status bar
	a.statusBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)

	// Create pages for tab content
	a.pages = tview.NewPages()

	// Create tab contents
	a.plcsTab = NewPLCsTab(a)
	a.browserTab = NewBrowserTab(a)
	a.restTab = NewRESTTab(a)
	a.mqttTab = NewMQTTTab(a)
	a.debugTab = NewDebugTab(a)

	// Add pages
	a.pages.AddPage(TabPLCs, a.plcsTab.GetPrimitive(), true, true)
	a.pages.AddPage(TabBrowser, a.browserTab.GetPrimitive(), true, false)
	a.pages.AddPage(TabREST, a.restTab.GetPrimitive(), true, false)
	a.pages.AddPage(TabMQTT, a.mqttTab.GetPrimitive(), true, false)
	a.pages.AddPage(TabDebug, a.debugTab.GetPrimitive(), true, false)

	// Create main layout
	mainFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(a.tabs, 1, 0, false).
		AddItem(a.pages, 0, 1, true).
		AddItem(a.statusBar, 1, 0, false)

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
	// Check if a modal is showing - if so, let the modal handle keys
	frontPage, _ := a.pages.GetFrontPage()
	isModal := frontPage != TabPLCs && frontPage != TabBrowser && frontPage != TabREST && frontPage != TabMQTT && frontPage != TabDebug

	// Don't intercept keys when a modal/form is open
	if isModal {
		// Only allow Escape to close modals via their own handlers
		return event
	}

	// Check for quit: Shift+Q (uppercase Q)
	if event.Rune() == 'Q' {
		a.Shutdown()
		return nil
	}

	// Check for help
	if event.Rune() == '?' {
		a.showHelp()
		return nil
	}

	// Tab switching with Shift+Tab only (let regular Tab work in forms)
	if event.Key() == tcell.KeyBacktab {
		a.nextTab()
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
		a.app.SetFocus(a.debugTab.GetFocusable())
	}
}

func (a *App) updateTabsDisplay() {
	text := ""
	for i, name := range a.tabNames {
		if i > 0 {
			text += "  │  "
		}
		if i == a.currentTab {
			text += "[blue::b]" + name + "[-::-]"
		} else {
			text += "[gray]" + name + "[-]"
		}
	}
	a.tabs.SetText(text)
}

func (a *App) setStatus(msg string) {
	a.statusBar.SetText(" " + msg)
}

func (a *App) showHelp() {
	textView := tview.NewTextView().
		SetText(HelpText).
		SetDynamicColors(true)
	textView.SetBorder(true).SetTitle(" Help ")

	textView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape || event.Key() == tcell.KeyEnter || event.Rune() == '?' {
			a.pages.RemovePage("help")
			a.focusCurrentTab()
			return nil
		}
		return event
	})

	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(textView, 24, 1, true).
			AddItem(nil, 0, 1, false), 45, 1, true).
		AddItem(nil, 0, 1, false)

	a.pages.AddPage("help", modal, true, true)
	a.app.SetFocus(textView)
}

func (a *App) showError(title, message string) {
	modal := tview.NewModal().
		SetText(title + "\n\n" + message).
		AddButtons([]string{"OK"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			a.pages.RemovePage("error")
			a.focusCurrentTab()
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

	return a.app.Run()
}

// Shutdown performs a clean shutdown of all resources.
func (a *App) Shutdown() {
	a.setStatus("Shutting down...")

	// Clear callbacks to prevent updates during shutdown
	a.manager.SetOnChange(nil)
	a.manager.SetOnValueChange(nil)

	// Stop the API server
	a.apiServer.Stop()

	// Stop all MQTT publishers
	a.mqttMgr.StopAll()

	// Stop the manager polling
	a.manager.Stop()

	// Disconnect all PLCs
	a.manager.DisconnectAll()

	// Stop the TUI
	a.app.Stop()
}

// Stop halts the TUI application.
func (a *App) Stop() {
	a.app.Stop()
}

// QueueUpdateDraw queues a function to run on the UI thread.
func (a *App) QueueUpdateDraw(f func()) {
	a.app.QueueUpdateDraw(f)
}

// ForcePublishAllValues publishes all current tag values to MQTT brokers.
// This is called when an MQTT broker connects to do an initial sync.
func (a *App) ForcePublishAllValues() {
	values := a.manager.GetAllCurrentValues()
	for _, v := range values {
		a.mqttMgr.Publish(v.PLCName, v.TagName, v.TypeName, v.Value, true)
	}
}
