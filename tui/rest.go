package tui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// RESTTab handles the Web server configuration tab.
type RESTTab struct {
	app        *App
	flex       *tview.Flex
	formBox    *tview.Flex
	hostInput  *tview.InputField
	portInput  *tview.InputField
	apiCheck   *tview.Checkbox
	uiCheck    *tview.Checkbox
	startBtn   *tview.Button
	stopBtn    *tview.Button
	endpoints  *tview.TextView
	statusBar  *tview.TextView
	focusables []tview.Primitive
	focusIndex int
}

// NewRESTTab creates a new Web tab.
func NewRESTTab(app *App) *RESTTab {
	t := &RESTTab{app: app}
	t.setupUI()
	t.Refresh()
	return t
}

func (t *RESTTab) setupUI() {
	// Host input
	t.hostInput = tview.NewInputField().
		SetLabel("Host: ").
		SetText(t.app.config.Web.Host).
		SetFieldWidth(15).
		SetChangedFunc(func(text string) {
			t.app.LockConfig()
			t.app.config.Web.Host = text
			t.app.UnlockAndSaveConfig()
			t.updateEndpointsList()
		})

	// Port input
	t.portInput = tview.NewInputField().
		SetLabel("Port: ").
		SetText(fmt.Sprintf("%d", t.app.config.Web.Port)).
		SetFieldWidth(6).
		SetAcceptanceFunc(tview.InputFieldInteger).
		SetChangedFunc(func(text string) {
			var port int
			fmt.Sscanf(text, "%d", &port)
			if port > 0 && port < 65536 {
				t.app.LockConfig()
				t.app.config.Web.Port = port
				t.app.UnlockAndSaveConfig()
				t.updateEndpointsList()
			}
		})

	// Apply theme to input fields
	ApplyInputFieldTheme(t.hostInput)
	ApplyInputFieldTheme(t.portInput)

	// API enabled checkbox
	t.apiCheck = tview.NewCheckbox().
		SetLabel("API ").
		SetChecked(t.app.config.Web.API.Enabled).
		SetChangedFunc(func(checked bool) {
			t.app.LockConfig()
			t.app.config.Web.API.Enabled = checked
			t.app.UnlockAndSaveConfig()
			if t.app.webServer != nil {
				t.app.webServer.Reload(&t.app.config.Web)
			}
			t.updateEndpointsList()
		})

	// UI enabled checkbox
	t.uiCheck = tview.NewCheckbox().
		SetLabel("UI ").
		SetChecked(t.app.config.Web.UI.Enabled).
		SetChangedFunc(func(checked bool) {
			t.app.LockConfig()
			t.app.config.Web.UI.Enabled = checked
			t.app.UnlockAndSaveConfig()
			if t.app.webServer != nil {
				t.app.webServer.Reload(&t.app.config.Web)
			}
			t.updateEndpointsList()
		})

	// Buttons
	t.startBtn = tview.NewButton("Start").SetSelectedFunc(t.startServer)
	t.stopBtn = tview.NewButton("Stop").SetSelectedFunc(t.stopServer)
	ApplyButtonTheme(t.startBtn)
	ApplyButtonTheme(t.stopBtn)

	// Track focusable elements for Tab navigation
	t.focusables = []tview.Primitive{t.hostInput, t.portInput, t.apiCheck, t.uiCheck, t.startBtn, t.stopBtn}
	t.focusIndex = 0

	// Set up Tab key navigation within the config area
	for _, p := range t.focusables {
		switch w := p.(type) {
		case *tview.InputField:
			w.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
				if event.Key() == tcell.KeyTab {
					t.focusNext()
					return nil
				} else if event.Key() == tcell.KeyBacktab {
					t.focusPrev()
					return nil
				}
				return event
			})
		case *tview.Button:
			w.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
				if event.Key() == tcell.KeyTab {
					t.focusNext()
					return nil
				} else if event.Key() == tcell.KeyBacktab {
					t.focusPrev()
					return nil
				}
				return event
			})
		case *tview.Checkbox:
			w.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
				if event.Key() == tcell.KeyTab {
					t.focusNext()
					return nil
				} else if event.Key() == tcell.KeyBacktab {
					t.focusPrev()
					return nil
				}
				return event
			})
		}
	}

	// Horizontal row with inputs, checkboxes, and buttons (with spacing)
	inputRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(t.hostInput, 22, 0, true).
		AddItem(nil, 2, 0, false). // spacer
		AddItem(t.portInput, 14, 0, false).
		AddItem(nil, 2, 0, false). // spacer
		AddItem(t.apiCheck, 7, 0, false).
		AddItem(nil, 1, 0, false). // spacer
		AddItem(t.uiCheck, 6, 0, false).
		AddItem(nil, 2, 0, false). // spacer
		AddItem(t.startBtn, 9, 0, false).
		AddItem(nil, 2, 0, false). // spacer
		AddItem(t.stopBtn, 8, 0, false).
		AddItem(nil, 0, 1, false) // fill remaining space

	// Status bar showing running state
	t.statusBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextColor(CurrentTheme.Text)

	// Combine status and inputs in config box
	t.formBox = tview.NewFlex().SetDirection(tview.FlexRow)
	t.formBox.SetBorder(true).SetTitle(" Web Server Configuration ").SetBorderColor(CurrentTheme.Border).SetTitleColor(CurrentTheme.Accent)
	t.formBox.AddItem(t.statusBar, 1, 0, false)
	t.formBox.AddItem(inputRow, 1, 0, true)

	// Endpoints display
	t.endpoints = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true)
	t.endpoints.SetBorder(true).SetTitle(" Available Endpoints ").SetBorderColor(CurrentTheme.Border).SetTitleColor(CurrentTheme.Accent)

	// Main layout
	t.flex = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(t.formBox, 5, 0, true).
		AddItem(t.endpoints, 0, 1, false)
}

func (t *RESTTab) focusNext() {
	t.focusIndex = (t.focusIndex + 1) % len(t.focusables)
	t.app.app.SetFocus(t.focusables[t.focusIndex])
}

func (t *RESTTab) focusPrev() {
	t.focusIndex = (t.focusIndex - 1 + len(t.focusables)) % len(t.focusables)
	t.app.app.SetFocus(t.focusables[t.focusIndex])
}

func (t *RESTTab) updateEndpointsList() {
	th := CurrentTheme
	baseURL := fmt.Sprintf("http://%s:%d", t.app.config.Web.Host, t.app.config.Web.Port)

	text := "\n"

	if t.app.config.Web.API.Enabled {
		apiURL := baseURL + "/api"
		text += fmt.Sprintf(" %sREST API%s  (%s/api)%s\n", th.TagAccent, th.TagTextDim, baseURL, th.TagReset)
		text += fmt.Sprintf(" %sGET%s   %s/\n", th.TagTextDim, th.TagReset, apiURL)
		text += "        List all configured PLCs\n\n"
		text += fmt.Sprintf(" %sGET%s   %s/{plc}\n", th.TagTextDim, th.TagReset, apiURL)
		text += "        Get PLC details\n\n"
		text += fmt.Sprintf(" %sGET%s   %s/{plc}/programs\n", th.TagTextDim, th.TagReset, apiURL)
		text += "        List programs on PLC\n\n"
		text += fmt.Sprintf(" %sGET%s   %s/{plc}/tags\n", th.TagTextDim, th.TagReset, apiURL)
		text += "        Get all published tags\n\n"
		text += fmt.Sprintf(" %sGET%s   %s/{plc}/tags/{tag}\n", th.TagTextDim, th.TagReset, apiURL)
		text += "        Get specific tag value\n\n"
		text += fmt.Sprintf(" %sPOST%s  %s/{plc}/write\n", th.TagSuccess, th.TagReset, apiURL)
		text += "        Write tag value (writable tags only)\n"
		text += "        Body: {\"plc\": \"name\", \"tag\": \"tagname\", \"value\": <value>}\n\n"

		text += fmt.Sprintf(" %sTagPacks%s\n", th.TagAccent, th.TagReset)
		text += fmt.Sprintf(" %sGET%s   %s/tagpack\n", th.TagTextDim, th.TagReset, apiURL)
		text += "        List all TagPacks\n\n"
		text += fmt.Sprintf(" %sGET%s   %s/tagpack/{name}\n", th.TagTextDim, th.TagReset, apiURL)
		text += "        Get TagPack current values\n\n"
	} else {
		text += fmt.Sprintf(" %sREST API disabled%s\n\n", th.TagTextDim, th.TagReset)
	}

	if t.app.config.Web.UI.Enabled {
		text += fmt.Sprintf(" %sBrowser UI%s  %s/\n", th.TagAccent, th.TagReset, baseURL)
	} else {
		text += fmt.Sprintf(" %sBrowser UI disabled%s\n", th.TagTextDim, th.TagReset)
	}

	t.endpoints.SetText(text)
}

func (t *RESTTab) startServer() {
	if t.app.webServer == nil {
		t.app.setStatus("Web server not configured")
		return
	}
	if t.app.webServer.IsRunning() {
		t.app.setStatus("Web server already running")
		return
	}

	err := t.app.webServer.Start()
	if err != nil {
		t.app.setStatus(fmt.Sprintf("Web start failed: %v", err))
		return
	}

	t.app.LockConfig()
	t.app.config.Web.Enabled = true
	t.app.UnlockAndSaveConfig()
	t.Refresh()
	t.app.setStatus(fmt.Sprintf("Web server started on %s", t.app.webServer.Address()))
}

func (t *RESTTab) stopServer() {
	if t.app.webServer == nil {
		t.app.setStatus("Web server not configured")
		return
	}
	if !t.app.webServer.IsRunning() {
		t.app.setStatus("Web server not running")
		return
	}

	err := t.app.webServer.Stop()
	if err != nil {
		t.app.setStatus(fmt.Sprintf("Web stop failed: %v", err))
		return
	}

	t.app.LockConfig()
	t.app.config.Web.Enabled = false
	t.app.UnlockAndSaveConfig()
	t.Refresh()
	t.app.setStatus("Web server stopped")
}

// GetPrimitive returns the main primitive for this tab.
func (t *RESTTab) GetPrimitive() tview.Primitive {
	return t.flex
}

// GetFocusable returns the element that should receive focus.
func (t *RESTTab) GetFocusable() tview.Primitive {
	t.focusIndex = 4 // Start button - avoid input fields to keep hotkeys active
	return t.startBtn
}

// Refresh updates the display.
func (t *RESTTab) Refresh() {
	th := CurrentTheme
	running := t.app.webServer != nil && t.app.webServer.IsRunning()

	// Sync input fields from config if they differ (for multi-session sync)
	// Only update if the field doesn't have focus to avoid disrupting user input
	focused := t.app.app.GetFocus()
	if focused != t.hostInput && t.hostInput.GetText() != t.app.config.Web.Host {
		t.hostInput.SetText(t.app.config.Web.Host)
	}
	portStr := fmt.Sprintf("%d", t.app.config.Web.Port)
	if focused != t.portInput && t.portInput.GetText() != portStr {
		t.portInput.SetText(portStr)
	}

	// Sync checkboxes
	if focused != t.apiCheck && t.apiCheck.IsChecked() != t.app.config.Web.API.Enabled {
		t.apiCheck.SetChecked(t.app.config.Web.API.Enabled)
	}
	if focused != t.uiCheck && t.uiCheck.IsChecked() != t.app.config.Web.UI.Enabled {
		t.uiCheck.SetChecked(t.app.config.Web.UI.Enabled)
	}

	var status string
	if running {
		addr := t.app.webServer.Address()
		services := ""
		if t.app.config.Web.API.Enabled && t.app.config.Web.UI.Enabled {
			services = "API+UI"
		} else if t.app.config.Web.API.Enabled {
			services = "API only"
		} else if t.app.config.Web.UI.Enabled {
			services = "UI only"
		}
		status = fmt.Sprintf(" %s● Running%s on %s (%s)  %s│%s  %s?%s help  %sShift+Tab%s next tab",
			th.TagSuccess, th.TagReset, addr, services,
			th.TagTextDim, th.TagReset,
			th.TagAccent, th.TagReset,
			th.TagAccent, th.TagReset)
		// Start disabled (dimmed), Stop red (active)
		t.startBtn.SetBackgroundColor(th.Background)
		t.startBtn.SetLabelColor(th.TextDim)
		t.startBtn.SetBackgroundColorActivated(th.FieldBackground)
		t.startBtn.SetLabelColorActivated(th.Text)
		t.startBtn.SetStyle(tcell.StyleDefault.Foreground(th.TextDim).Background(th.Background).Dim(true))
		t.startBtn.SetActivatedStyle(tcell.StyleDefault.Foreground(th.Text).Background(th.FieldBackground).Underline(true))
		t.stopBtn.SetBackgroundColor(th.Error)
		t.stopBtn.SetLabelColor(tcell.ColorWhite)
		t.stopBtn.SetBackgroundColorActivated(th.Error)
		t.stopBtn.SetLabelColorActivated(tcell.ColorWhite)
		t.stopBtn.SetStyle(tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(th.Error))
		t.stopBtn.SetActivatedStyle(tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(th.Error).Bold(true))
	} else {
		status = fmt.Sprintf(" %s○ Stopped%s - Press Tab to reach Start/Stop  %s│%s  %s?%s help  %sShift+Tab%s next tab",
			th.TagError, th.TagReset,
			th.TagTextDim, th.TagReset,
			th.TagAccent, th.TagReset,
			th.TagAccent, th.TagReset)
		// Start green (active), Stop disabled (dimmed)
		t.startBtn.SetBackgroundColor(th.Success)
		t.startBtn.SetLabelColor(tcell.ColorWhite)
		t.startBtn.SetBackgroundColorActivated(th.Success)
		t.startBtn.SetLabelColorActivated(tcell.ColorWhite)
		t.startBtn.SetStyle(tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(th.Success))
		t.startBtn.SetActivatedStyle(tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(th.Success).Bold(true))
		t.stopBtn.SetBackgroundColor(th.Background)
		t.stopBtn.SetLabelColor(th.TextDim)
		t.stopBtn.SetBackgroundColorActivated(th.FieldBackground)
		t.stopBtn.SetLabelColorActivated(th.Text)
		t.stopBtn.SetStyle(tcell.StyleDefault.Foreground(th.TextDim).Background(th.Background).Dim(true))
		t.stopBtn.SetActivatedStyle(tcell.StyleDefault.Foreground(th.Text).Background(th.FieldBackground).Underline(true))
	}

	t.statusBar.SetText(status)
	t.updateEndpointsList()
}

// RefreshTheme updates theme-dependent UI elements.
func (t *RESTTab) RefreshTheme() {
	th := CurrentTheme
	t.formBox.SetBorderColor(th.Border).SetTitleColor(th.Accent)
	t.endpoints.SetBorderColor(th.Border).SetTitleColor(th.Accent)
	t.statusBar.SetTextColor(th.Text)
	t.endpoints.SetTextColor(th.Text)
	ApplyInputFieldTheme(t.hostInput)
	ApplyInputFieldTheme(t.portInput)
	ApplyButtonTheme(t.startBtn)
	ApplyButtonTheme(t.stopBtn)
	t.Refresh() // Update status text with new theme colors
}
