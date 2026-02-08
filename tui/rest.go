package tui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// RESTTab handles the REST API configuration tab.
type RESTTab struct {
	app        *App
	flex       *tview.Flex
	formBox    *tview.Flex
	hostInput  *tview.InputField
	portInput  *tview.InputField
	startBtn   *tview.Button
	stopBtn    *tview.Button
	endpoints  *tview.TextView
	statusBar  *tview.TextView
	focusables []tview.Primitive
	focusIndex int
}

// NewRESTTab creates a new REST tab.
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
		SetText(t.app.config.REST.Host).
		SetFieldWidth(15).
		SetChangedFunc(func(text string) {
			t.app.config.REST.Host = text
			t.app.SaveConfig()
			t.updateEndpointsList()
		})

	// Port input
	t.portInput = tview.NewInputField().
		SetLabel("Port: ").
		SetText(fmt.Sprintf("%d", t.app.config.REST.Port)).
		SetFieldWidth(6).
		SetAcceptanceFunc(tview.InputFieldInteger).
		SetChangedFunc(func(text string) {
			var port int
			fmt.Sscanf(text, "%d", &port)
			if port > 0 && port < 65536 {
				t.app.config.REST.Port = port
				t.app.SaveConfig()
				t.updateEndpointsList()
			}
		})

	// Apply theme to input fields
	ApplyInputFieldTheme(t.hostInput)
	ApplyInputFieldTheme(t.portInput)

	// Buttons
	t.startBtn = tview.NewButton("Start").SetSelectedFunc(t.startServer)
	t.stopBtn = tview.NewButton("Stop").SetSelectedFunc(t.stopServer)
	ApplyButtonTheme(t.startBtn)
	ApplyButtonTheme(t.stopBtn)

	// Track focusable elements for Tab navigation
	t.focusables = []tview.Primitive{t.hostInput, t.portInput, t.startBtn, t.stopBtn}
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
		}
	}

	// Horizontal row with inputs and buttons (with spacing)
	inputRow := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(t.hostInput, 22, 0, true).
		AddItem(nil, 2, 0, false). // spacer
		AddItem(t.portInput, 14, 0, false).
		AddItem(nil, 3, 0, false). // spacer
		AddItem(t.startBtn, 9, 0, false).
		AddItem(nil, 2, 0, false). // spacer
		AddItem(t.stopBtn, 8, 0, false).
		AddItem(nil, 0, 1, false)  // fill remaining space

	// Status bar showing running state
	t.statusBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextColor(CurrentTheme.Text)

	// Combine status and inputs in config box
	t.formBox = tview.NewFlex().SetDirection(tview.FlexRow)
	t.formBox.SetBorder(true).SetTitle(" REST API Configuration ").SetBorderColor(CurrentTheme.Border).SetTitleColor(CurrentTheme.Accent)
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
	baseURL := fmt.Sprintf("http://%s:%d", t.app.config.REST.Host, t.app.config.REST.Port)

	text := "\n"
	text += fmt.Sprintf(" %sPLCs%s\n", th.TagAccent, th.TagReset)
	text += fmt.Sprintf(" %sGET%s   %s/\n", th.TagTextDim, th.TagReset, baseURL)
	text += "        List all configured PLCs\n\n"
	text += fmt.Sprintf(" %sGET%s   %s/{plc}\n", th.TagTextDim, th.TagReset, baseURL)
	text += "        Get PLC details\n\n"
	text += fmt.Sprintf(" %sGET%s   %s/{plc}/programs\n", th.TagTextDim, th.TagReset, baseURL)
	text += "        List programs on PLC\n\n"
	text += fmt.Sprintf(" %sGET%s   %s/{plc}/tags\n", th.TagTextDim, th.TagReset, baseURL)
	text += "        Get all published tags\n\n"
	text += fmt.Sprintf(" %sGET%s   %s/{plc}/tags/{tag}\n", th.TagTextDim, th.TagReset, baseURL)
	text += "        Get specific tag value\n\n"
	text += fmt.Sprintf(" %sPOST%s  %s/{plc}/write\n", th.TagSuccess, th.TagReset, baseURL)
	text += "        Write tag value (writable tags only)\n"
	text += "        Body: {\"plc\": \"name\", \"tag\": \"tagname\", \"value\": <value>}\n\n"

	text += fmt.Sprintf(" %sTagPacks%s\n", th.TagAccent, th.TagReset)
	text += fmt.Sprintf(" %sGET%s   %s/tagpack\n", th.TagTextDim, th.TagReset, baseURL)
	text += "        List all TagPacks\n\n"
	text += fmt.Sprintf(" %sGET%s   %s/tagpack/{name}\n", th.TagTextDim, th.TagReset, baseURL)
	text += "        Get TagPack current values\n"

	t.endpoints.SetText(text)
}

func (t *RESTTab) startServer() {
	if t.app.apiServer.IsRunning() {
		t.app.setStatus("REST server already running")
		return
	}

	err := t.app.apiServer.Start()
	if err != nil {
		t.app.setStatus(fmt.Sprintf("REST start failed: %v", err))
		return
	}

	t.app.config.REST.Enabled = true
	t.app.SaveConfig()
	t.Refresh()
	t.app.setStatus(fmt.Sprintf("REST server started on %s", t.app.apiServer.Address()))
}

func (t *RESTTab) stopServer() {
	if !t.app.apiServer.IsRunning() {
		t.app.setStatus("REST server not running")
		return
	}

	err := t.app.apiServer.Stop()
	if err != nil {
		t.app.setStatus(fmt.Sprintf("REST stop failed: %v", err))
		return
	}

	t.app.config.REST.Enabled = false
	t.app.SaveConfig()
	t.Refresh()
	t.app.setStatus("REST server stopped")
}

// GetPrimitive returns the main primitive for this tab.
func (t *RESTTab) GetPrimitive() tview.Primitive {
	return t.flex
}

// GetFocusable returns the element that should receive focus.
func (t *RESTTab) GetFocusable() tview.Primitive {
	t.focusIndex = 0
	return t.hostInput
}

// Refresh updates the display.
func (t *RESTTab) Refresh() {
	th := CurrentTheme
	running := t.app.apiServer.IsRunning()

	var status string
	if running {
		status = fmt.Sprintf(" %s● Running%s on %s  %s│%s  %s?%s help  %sShift+Tab%s next tab",
			th.TagSuccess, th.TagReset, t.app.apiServer.Address(),
			th.TagTextDim, th.TagReset,
			th.TagAccent, th.TagReset,
			th.TagAccent, th.TagReset)
		// Start disabled (dimmed), Stop red (active)
		t.startBtn.SetStyle(tcell.StyleDefault.Foreground(th.TextDim).Background(th.Background).Dim(true))
		t.startBtn.SetActivatedStyle(tcell.StyleDefault.Foreground(th.Text).Background(th.FieldBackground).Underline(true))
		t.stopBtn.SetStyle(tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(th.Error))
		t.stopBtn.SetActivatedStyle(tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(th.Error).Bold(true))
	} else {
		status = fmt.Sprintf(" %s○ Stopped%s - Press Tab to reach Start/Stop  %s│%s  %s?%s help  %sShift+Tab%s next tab",
			th.TagError, th.TagReset,
			th.TagTextDim, th.TagReset,
			th.TagAccent, th.TagReset,
			th.TagAccent, th.TagReset)
		// Start green (active), Stop disabled (dimmed)
		t.startBtn.SetStyle(tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(th.Success))
		t.startBtn.SetActivatedStyle(tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(th.Success).Bold(true))
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
