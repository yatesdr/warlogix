package tui

import (
	"fmt"

	"github.com/rivo/tview"
)

// RESTTab handles the REST API configuration tab.
type RESTTab struct {
	app       *App
	flex      *tview.Flex
	form      *tview.Form
	endpoints *tview.TextView
	statusBar *tview.TextView
}

// NewRESTTab creates a new REST tab.
func NewRESTTab(app *App) *RESTTab {
	t := &RESTTab{app: app}
	t.setupUI()
	t.Refresh()
	return t
}

func (t *RESTTab) setupUI() {
	// Form for REST configuration
	t.form = tview.NewForm()
	t.form.AddInputField("Host:", t.app.config.REST.Host, 20, nil, func(text string) {
		t.app.config.REST.Host = text
		t.app.SaveConfig()
		t.updateEndpointsList()
	})
	t.form.AddInputField("Port:", fmt.Sprintf("%d", t.app.config.REST.Port), 10, acceptDigits, func(text string) {
		var port int
		fmt.Sscanf(text, "%d", &port)
		if port > 0 && port < 65536 {
			t.app.config.REST.Port = port
			t.app.SaveConfig()
			t.updateEndpointsList()
		}
	})
	t.form.AddButton("Start", t.startServer)
	t.form.AddButton("Stop", t.stopServer)

	formBox := tview.NewFlex().SetDirection(tview.FlexRow)
	formBox.SetBorder(true).SetTitle(" REST API Configuration ")
	formBox.AddItem(t.form, 5, 0, true)

	// Endpoints display
	t.endpoints = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true)
	t.endpoints.SetBorder(true).SetTitle(" Available Endpoints ")

	// Status bar
	t.statusBar = tview.NewTextView().
		SetDynamicColors(true)

	// Main layout
	t.flex = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(formBox, 7, 0, true).
		AddItem(t.endpoints, 0, 1, false).
		AddItem(t.statusBar, 1, 0, false)
}

func (t *RESTTab) updateEndpointsList() {
	baseURL := fmt.Sprintf("http://%s:%d", t.app.config.REST.Host, t.app.config.REST.Port)

	text := "\n"
	text += fmt.Sprintf(" [yellow]GET[white]  %s/\n", baseURL)
	text += "       List all configured PLCs\n\n"
	text += fmt.Sprintf(" [yellow]GET[white]  %s/{plc}\n", baseURL)
	text += "       Get PLC details\n\n"
	text += fmt.Sprintf(" [yellow]GET[white]  %s/{plc}/programs\n", baseURL)
	text += "       List programs on PLC\n\n"
	text += fmt.Sprintf(" [yellow]GET[white]  %s/{plc}/tags\n", baseURL)
	text += "       Get all published tags\n\n"
	text += fmt.Sprintf(" [yellow]GET[white]  %s/{plc}/tags/{tag}\n", baseURL)
	text += "       Get specific tag value\n"

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
	return t.form
}

// Refresh updates the display.
func (t *RESTTab) Refresh() {
	running := t.app.apiServer.IsRunning()

	var status string
	if running {
		status = fmt.Sprintf("[green]● Running[white] on %s", t.app.apiServer.Address())
	} else {
		status = "[gray]○ Stopped"
	}

	t.statusBar.SetText(" Status: " + status)
	t.updateEndpointsList()
}
