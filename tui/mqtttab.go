package tui

import (
	"fmt"
	"strconv"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"warlogix/config"
	"warlogix/mqtt"
)

// MQTTTab handles the MQTT configuration tab.
type MQTTTab struct {
	app       *App
	flex      *tview.Flex
	table     *tview.Table
	tableBox  *tview.Flex
	info      *tview.TextView
	statusBar *tview.TextView
	buttonBar *tview.TextView
}

// NewMQTTTab creates a new MQTT tab.
func NewMQTTTab(app *App) *MQTTTab {
	t := &MQTTTab{app: app}
	t.setupUI()
	t.Refresh()
	return t
}

func (t *MQTTTab) setupUI() {
	// Button bar (themed) - at top, outside frame
	t.buttonBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
	t.updateButtonBar()

	// Broker table
	t.table = tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0)
	ApplyTableTheme(t.table)

	t.table.SetInputCapture(t.handleKeys)
	t.table.SetSelectedFunc(t.onSelect)

	// Set up headers (themed)
	headers := []string{"", "Name", "Broker", "Port", "TLS", "Root Topic", "Status"}
	for i, h := range headers {
		t.table.SetCell(0, i, tview.NewTableCell(h).
			SetTextColor(CurrentTheme.Accent).
			SetSelectable(false).
			SetAttributes(tcell.AttrBold))
	}

	// Table with frame
	t.tableBox = tview.NewFlex().SetDirection(tview.FlexRow)
	t.tableBox.SetBorder(true).SetTitle(" MQTT Brokers ").SetBorderColor(CurrentTheme.Border).SetTitleColor(CurrentTheme.Accent)
	t.tableBox.AddItem(t.table, 0, 1, true)

	// Info panel
	t.info = tview.NewTextView().
		SetDynamicColors(true).
		SetTextColor(CurrentTheme.Text)
	t.info.SetBorder(true).SetTitle(" Topic Structure ").SetBorderColor(CurrentTheme.Border).SetTitleColor(CurrentTheme.Accent)
	t.updateInfo()

	// Status bar
	t.statusBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextColor(CurrentTheme.Text)

	// Main layout - buttonBar at top, outside frames
	t.flex = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(t.buttonBar, 1, 0, false).
		AddItem(t.tableBox, 0, 1, true).
		AddItem(t.info, 8, 0, false).
		AddItem(t.statusBar, 1, 0, false)
}

func (t *MQTTTab) handleKeys(event *tcell.EventKey) *tcell.EventKey {
	switch event.Rune() {
	case 'a':
		t.showAddDialog()
		return nil
	case 'e':
		t.showEditDialog()
		return nil
	case 'x':
		t.removeSelected()
		return nil
	case 'c':
		t.connectSelected()
		return nil
	case 'C':
		t.disconnectSelected()
		return nil
	}
	return event
}

func (t *MQTTTab) onSelect(row, col int) {
	if row <= 0 {
		return
	}
	// Toggle connection on Enter
	pubs := t.app.mqttMgr.List()
	if row-1 >= len(pubs) {
		return
	}
	pub := pubs[row-1]
	if pub.IsRunning() {
		t.disconnectSelected()
	} else {
		t.connectSelected()
	}
}

func (t *MQTTTab) updateInfo() {
	th := CurrentTheme
	text := "\n"
	text += " " + th.TagAccent + "Topic Format:" + th.TagReset + "\n"
	text += "   {root_topic}/{plc_name}/tags/{tag_name}\n\n"
	text += " " + th.TagAccent + "Message Format:" + th.TagReset + "\n"
	text += "   {\"value\": <value>, \"type\": \"<type>\", \"timestamp\": \"<iso8601>\"}\n\n"
	text += " " + th.Dim("Tag messages are retained and only published on value change") + "\n"

	t.info.SetText(text)
}

func (t *MQTTTab) refreshTable() {
	// Clear existing rows (keep header)
	for t.table.GetRowCount() > 1 {
		t.table.RemoveRow(1)
	}

	pubs := t.app.mqttMgr.List()
	th := CurrentTheme
	for i, pub := range pubs {
		row := i + 1
		cfg := pub.Config()

		var indicator string
		if pub.IsRunning() {
			indicator = th.StatusConnected
		} else {
			indicator = th.StatusDisconnected
		}

		status := "Stopped"
		if pub.IsRunning() {
			status = "Connected"
		}

		tlsIndicator := th.Dim("No")
		if cfg.UseTLS {
			tlsIndicator = th.SuccessText("Yes")
		}

		t.table.SetCell(row, 0, tview.NewTableCell(indicator).SetExpansion(0))
		t.table.SetCell(row, 1, tview.NewTableCell(cfg.Name).SetExpansion(1))
		t.table.SetCell(row, 2, tview.NewTableCell(cfg.Broker).SetExpansion(1))
		t.table.SetCell(row, 3, tview.NewTableCell(fmt.Sprintf("%d", cfg.Port)).SetExpansion(0))
		t.table.SetCell(row, 4, tview.NewTableCell(tlsIndicator).SetExpansion(0))
		t.table.SetCell(row, 5, tview.NewTableCell(cfg.Selector).SetExpansion(1))
		t.table.SetCell(row, 6, tview.NewTableCell(status).SetExpansion(1))
	}
}

func (t *MQTTTab) showAddDialog() {
	const pageName = "add-mqtt"

	form := tview.NewForm()
	ApplyFormTheme(form)
	form.SetBorder(true).SetTitle(" Add MQTT Broker ")

	form.AddInputField("Name:", "", 20, nil, nil)
	form.AddInputField("Broker:", "localhost", 30, nil, nil)
	form.AddInputField("Port:", "1883", 8, acceptDigits, nil)
	form.AddInputField("Selector:", "factory", 20, nil, nil)
	form.AddInputField("Client ID:", "wargate", 20, nil, nil)
	form.AddInputField("Username:", "", 20, nil, nil)
	form.AddPasswordField("Password:", "", 20, '*', nil)
	form.AddCheckbox("Use TLS:", false, nil)
	form.AddCheckbox("Auto-connect:", false, nil)

	form.AddButton("Add", func() {
		name := form.GetFormItemByLabel("Name:").(*tview.InputField).GetText()
		broker := form.GetFormItemByLabel("Broker:").(*tview.InputField).GetText()
		portStr := form.GetFormItemByLabel("Port:").(*tview.InputField).GetText()
		rootTopic := form.GetFormItemByLabel("Selector:").(*tview.InputField).GetText()
		clientID := form.GetFormItemByLabel("Client ID:").(*tview.InputField).GetText()
		username := form.GetFormItemByLabel("Username:").(*tview.InputField).GetText()
		password := form.GetFormItemByLabel("Password:").(*tview.InputField).GetText()
		useTLS := form.GetFormItemByLabel("Use TLS:").(*tview.Checkbox).IsChecked()
		autoConnect := form.GetFormItemByLabel("Auto-connect:").(*tview.Checkbox).IsChecked()

		if name == "" || broker == "" {
			t.app.showError("Error", "Name and broker are required")
			return
		}

		port, _ := strconv.Atoi(portStr)
		if port <= 0 {
			port = 1883
		}

		cfg := config.MQTTConfig{
			Name:      name,
			Enabled:   autoConnect,
			Broker:    broker,
			Port:      port,
			Selector: rootTopic,
			ClientID:  clientID,
			Username:  username,
			Password:  password,
			UseTLS:    useTLS,
		}

		t.app.config.AddMQTT(cfg)
		t.app.SaveConfig()

		// Add to manager
		pub := mqtt.NewPublisher(&t.app.config.MQTT[len(t.app.config.MQTT)-1], t.app.config.Namespace)
		t.app.mqttMgr.Add(pub)

		if autoConnect {
			go func() {
				if err := pub.Start(); err == nil {
					t.app.ForcePublishAllValues()
				}
			}()
		}

		t.app.closeModal(pageName)
		t.Refresh()
		t.app.setStatus(fmt.Sprintf("Added MQTT broker: %s", name))
	})

	form.AddButton("Cancel", func() {
		t.app.closeModal(pageName)
	})

	t.app.showFormModal(pageName, form, 55, 24, func() {
		t.app.closeModal(pageName)
	})
}

func (t *MQTTTab) showEditDialog() {
	const pageName = "edit-mqtt"

	row, _ := t.table.GetSelection()
	if row <= 0 {
		return
	}

	pubs := t.app.mqttMgr.List()
	if row-1 >= len(pubs) {
		return
	}

	pub := pubs[row-1]
	cfg := pub.Config()
	originalName := cfg.Name

	form := tview.NewForm()
	ApplyFormTheme(form)
	form.SetBorder(true).SetTitle(" Edit MQTT Broker ")

	form.AddInputField("Name:", cfg.Name, 20, nil, nil)
	form.AddInputField("Broker:", cfg.Broker, 30, nil, nil)
	form.AddInputField("Port:", fmt.Sprintf("%d", cfg.Port), 8, acceptDigits, nil)
	form.AddInputField("Selector:", cfg.Selector, 20, nil, nil)
	form.AddInputField("Client ID:", cfg.ClientID, 20, nil, nil)
	form.AddInputField("Username:", cfg.Username, 20, nil, nil)
	form.AddPasswordField("Password:", cfg.Password, 20, '*', nil)
	form.AddCheckbox("Use TLS:", cfg.UseTLS, nil)
	form.AddCheckbox("Auto-connect:", cfg.Enabled, nil)

	form.AddButton("Save", func() {
		name := form.GetFormItemByLabel("Name:").(*tview.InputField).GetText()
		broker := form.GetFormItemByLabel("Broker:").(*tview.InputField).GetText()
		portStr := form.GetFormItemByLabel("Port:").(*tview.InputField).GetText()
		rootTopic := form.GetFormItemByLabel("Selector:").(*tview.InputField).GetText()
		clientID := form.GetFormItemByLabel("Client ID:").(*tview.InputField).GetText()
		username := form.GetFormItemByLabel("Username:").(*tview.InputField).GetText()
		password := form.GetFormItemByLabel("Password:").(*tview.InputField).GetText()
		useTLS := form.GetFormItemByLabel("Use TLS:").(*tview.Checkbox).IsChecked()
		autoConnect := form.GetFormItemByLabel("Auto-connect:").(*tview.Checkbox).IsChecked()

		if name == "" || broker == "" {
			t.app.showError("Error", "Name and broker are required")
			return
		}

		port, _ := strconv.Atoi(portStr)
		if port <= 0 {
			port = 1883
		}

		updated := config.MQTTConfig{
			Name:      name,
			Enabled:   autoConnect,
			Broker:    broker,
			Port:      port,
			Selector: rootTopic,
			ClientID:  clientID,
			Username:  username,
			Password:  password,
			UseTLS:    useTLS,
		}

		t.app.config.UpdateMQTT(originalName, updated)
		t.app.SaveConfig()

		// Close dialog immediately
		t.app.closeModal(pageName)
		t.app.setStatus(fmt.Sprintf("Updating MQTT broker: %s...", name))

		// Update manager in background to avoid blocking UI
		go func() {
			t.app.mqttMgr.Remove(originalName)
			newPub := mqtt.NewPublisher(t.app.config.FindMQTT(name), t.app.config.Namespace)
			t.app.mqttMgr.Add(newPub)

			if autoConnect {
				if err := newPub.Start(); err == nil {
					t.app.ForcePublishAllValues()
					DebugLogMQTT("Reconnected to %s after config update (topic: %s)", name, rootTopic)
				} else {
					DebugLogError("MQTT %s reconnect failed: %v", name, err)
				}
			}

			t.app.QueueUpdateDraw(func() {
				t.Refresh()
				t.app.setStatus(fmt.Sprintf("Updated MQTT broker: %s", name))
			})
		}()
		DebugLogMQTT("MQTT broker %s updated (broker: %s, topic: %s)", name, broker, rootTopic)
	})

	form.AddButton("Cancel", func() {
		t.app.closeModal(pageName)
	})

	t.app.showFormModal(pageName, form, 55, 24, func() {
		t.app.closeModal(pageName)
	})
}

func (t *MQTTTab) removeSelected() {
	row, _ := t.table.GetSelection()
	if row <= 0 {
		return
	}

	pubs := t.app.mqttMgr.List()
	if row-1 >= len(pubs) {
		return
	}

	pub := pubs[row-1]
	name := pub.Name()

	t.app.showConfirm("Remove MQTT Broker", fmt.Sprintf("Remove %s?", name), func() {
		// Run removal in background to avoid blocking UI (Stop() may block)
		go func() {
			t.app.mqttMgr.Remove(name)

			t.app.QueueUpdateDraw(func() {
				t.app.config.RemoveMQTT(name)
				t.app.SaveConfig()
				t.Refresh()
				t.app.setStatus(fmt.Sprintf("Removed MQTT broker: %s", name))
			})
		}()
	})
}

func (t *MQTTTab) connectSelected() {
	row, _ := t.table.GetSelection()
	if row <= 0 {
		return
	}

	pubs := t.app.mqttMgr.List()
	if row-1 >= len(pubs) {
		return
	}

	pub := pubs[row-1]
	if pub.IsRunning() {
		t.app.setStatus(fmt.Sprintf("MQTT %s already connected", pub.Name()))
		return
	}

	t.app.setStatus(fmt.Sprintf("Connecting to %s...", pub.Name()))

	go func() {
		pubName := pub.Name()
		pubAddr := pub.Address()
		pubTopic := pub.Config().Selector
		err := pub.Start()

		// Update UI immediately after connection attempt
		t.app.QueueUpdateDraw(func() {
			if err != nil {
				t.app.setStatus(fmt.Sprintf("MQTT connect failed: %v", err))
				DebugLogError("MQTT %s connection failed: %v", pubName, err)
			} else {
				if cfg := t.app.config.FindMQTT(pubName); cfg != nil {
					cfg.Enabled = true
					t.app.SaveConfig()
				}
				t.app.setStatus(fmt.Sprintf("MQTT connected to %s", pubAddr))
				DebugLogMQTT("Connected to %s (broker: %s, topic: %s)", pubName, pubAddr, pubTopic)
			}
			t.Refresh()
		})

		// Force publish in separate goroutine to not delay UI
		if err == nil {
			go t.app.ForcePublishAllValues()
		}
	}()
}

func (t *MQTTTab) disconnectSelected() {
	row, _ := t.table.GetSelection()
	if row <= 0 {
		return
	}

	pubs := t.app.mqttMgr.List()
	if row-1 >= len(pubs) {
		return
	}

	pub := pubs[row-1]
	if !pub.IsRunning() {
		t.app.setStatus(fmt.Sprintf("MQTT %s not connected", pub.Name()))
		return
	}

	pubName := pub.Name()
	t.app.setStatus(fmt.Sprintf("Disconnecting from %s...", pubName))

	// Run disconnect in background to avoid blocking UI
	go func() {
		pub.Stop()

		t.app.QueueUpdateDraw(func() {
			if cfg := t.app.config.FindMQTT(pubName); cfg != nil {
				cfg.Enabled = false
				t.app.SaveConfig()
			}
			t.Refresh()
			t.app.setStatus(fmt.Sprintf("MQTT disconnected from %s", pubName))
		})
	}()
}

// GetPrimitive returns the main primitive for this tab.
func (t *MQTTTab) GetPrimitive() tview.Primitive {
	return t.flex
}

// GetFocusable returns the element that should receive focus.
func (t *MQTTTab) GetFocusable() tview.Primitive {
	return t.table
}

// Refresh updates the display.
func (t *MQTTTab) Refresh() {
	pubs := t.app.mqttMgr.List()
	connectedCount := 0
	for _, pub := range pubs {
		if pub.IsRunning() {
			connectedCount++
		}
	}

	t.statusBar.SetText(fmt.Sprintf(" %d brokers configured, %d connected", len(pubs), connectedCount))
	t.refreshTable()
}

func (t *MQTTTab) updateButtonBar() {
	th := CurrentTheme
	buttonText := " " + th.TagHotkey + "a" + th.TagActionText + "dd  " +
		th.TagHotkey + "e" + th.TagActionText + "dit  " +
		th.TagHotkey + "x" + th.TagActionText + " remove  " +
		th.TagHotkey + "c" + th.TagActionText + "onnect  " +
		th.TagHotkey + "C" + th.TagActionText + " disconnect" + th.TagReset
	t.buttonBar.SetText(buttonText)
}

// RefreshTheme updates theme-dependent UI elements.
func (t *MQTTTab) RefreshTheme() {
	t.updateButtonBar()
	th := CurrentTheme
	t.tableBox.SetBorderColor(th.Border).SetTitleColor(th.Accent)
	t.info.SetBorderColor(th.Border).SetTitleColor(th.Accent)
	t.info.SetTextColor(th.Text)
	t.statusBar.SetTextColor(th.Text)
	ApplyTableTheme(t.table)
	// Update header colors
	for i := 0; i < t.table.GetColumnCount(); i++ {
		if cell := t.table.GetCell(0, i); cell != nil {
			cell.SetTextColor(th.Accent)
		}
	}
	// Regenerate info text with new theme colors
	t.updateInfo()
}

