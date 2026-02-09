package tui

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"warlink/config"
)

// ValkeyTab handles the Valkey configuration tab.
type ValkeyTab struct {
	app       *App
	flex      *tview.Flex
	table     *tview.Table
	tableBox  *tview.Flex
	info      *tview.TextView
	statusBar *tview.TextView
	buttonBar *tview.TextView
}

// NewValkeyTab creates a new Valkey tab.
func NewValkeyTab(app *App) *ValkeyTab {
	t := &ValkeyTab{app: app}
	t.setupUI()
	t.Refresh()
	return t
}

func (t *ValkeyTab) setupUI() {
	// Button bar (themed) - at top, outside frame
	t.buttonBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
	t.updateButtonBar()

	// Server table
	t.table = tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0)
	ApplyTableTheme(t.table)

	t.table.SetInputCapture(t.handleKeys)
	t.table.SetSelectedFunc(t.onSelect)

	// Set up headers (themed)
	headers := []string{"", "Name", "Address", "TLS", "Selector", "Status"}
	for i, h := range headers {
		t.table.SetCell(0, i, tview.NewTableCell(h).
			SetTextColor(CurrentTheme.Accent).
			SetSelectable(false).
			SetAttributes(tcell.AttrBold))
	}

	// Table with frame
	t.tableBox = tview.NewFlex().SetDirection(tview.FlexRow)
	t.tableBox.SetBorder(true).SetTitle(" Valkey Servers ").SetBorderColor(CurrentTheme.Border).SetTitleColor(CurrentTheme.Accent)
	t.tableBox.AddItem(t.table, 0, 1, true)

	// Info panel
	t.info = tview.NewTextView().
		SetDynamicColors(true).
		SetTextColor(CurrentTheme.Text)
	t.info.SetBorder(true).SetTitle(" Key Structure ").SetBorderColor(CurrentTheme.Border).SetTitleColor(CurrentTheme.Accent)
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
		AddItem(t.info, 10, 0, false).
		AddItem(t.statusBar, 1, 0, false)
}

func (t *ValkeyTab) handleKeys(event *tcell.EventKey) *tcell.EventKey {
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

func (t *ValkeyTab) onSelect(row, col int) {
	if row <= 0 {
		return
	}
	// Toggle connection on Enter
	pubs := t.app.valkeyMgr.List()
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

func (t *ValkeyTab) updateInfo() {
	th := CurrentTheme
	text := "\n"
	text += " " + th.TagAccent + "Key Format:" + th.TagReset + "\n"
	text += "   {namespace}[:{selector}]:{plc}:tags:{tag}\n\n"
	text += " " + th.TagAccent + "Value Format:" + th.TagReset + "\n"
	text += "   {\"value\": <value>, \"type\": \"<type>\", \"writable\": bool, \"timestamp\": \"<iso8601>\"}\n\n"
	text += " " + th.TagAccent + "Pub/Sub Channels:" + th.TagReset + "\n"
	text += "   {namespace}[:{selector}]:{plc}:changes - per-PLC changes\n"
	text += "   {namespace}[:{selector}]:_all:changes  - all changes\n\n"
	text += " " + th.Dim("Keys are set with optional TTL for stale detection") + "\n"

	t.info.SetText(text)
}

func (t *ValkeyTab) refreshTable() {
	// Clear existing rows (keep header)
	for t.table.GetRowCount() > 1 {
		t.table.RemoveRow(1)
	}

	pubs := t.app.valkeyMgr.List()
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
		t.table.SetCell(row, 2, tview.NewTableCell(cfg.Address).SetExpansion(1))
		t.table.SetCell(row, 3, tview.NewTableCell(tlsIndicator).SetExpansion(0))
		t.table.SetCell(row, 4, tview.NewTableCell(cfg.Selector).SetExpansion(1))
		t.table.SetCell(row, 5, tview.NewTableCell(status).SetExpansion(1))
	}
}

func (t *ValkeyTab) showAddDialog() {
	const pageName = "add-valkey"

	form := tview.NewForm()
	ApplyFormTheme(form)
	form.SetBorder(true).SetTitle(" Add Valkey Server ")

	form.AddInputField("Name:", "", 20, nil, nil)
	form.AddInputField("Address:", "localhost:6379", 30, nil, nil)
	form.AddInputField("Password:", "", 20, nil, nil)
	form.AddInputField("Database:", "0", 5, acceptDigits, nil)
	form.AddInputField("Selector:", "factory", 20, nil, nil)
	form.AddInputField("Key TTL (sec):", "0", 8, acceptDigits, nil)
	form.AddCheckbox("Use TLS:", false, nil)
	form.AddCheckbox("Publish Changes:", true, nil)
	form.AddCheckbox("Enable Writeback:", false, nil)
	form.AddCheckbox("Auto-connect:", false, nil)

	form.AddButton("Add", func() {
		name := form.GetFormItemByLabel("Name:").(*tview.InputField).GetText()
		address := form.GetFormItemByLabel("Address:").(*tview.InputField).GetText()
		password := form.GetFormItemByLabel("Password:").(*tview.InputField).GetText()
		dbStr := form.GetFormItemByLabel("Database:").(*tview.InputField).GetText()
		selector := form.GetFormItemByLabel("Selector:").(*tview.InputField).GetText()
		ttlStr := form.GetFormItemByLabel("Key TTL (sec):").(*tview.InputField).GetText()
		useTLS := form.GetFormItemByLabel("Use TLS:").(*tview.Checkbox).IsChecked()
		publishChanges := form.GetFormItemByLabel("Publish Changes:").(*tview.Checkbox).IsChecked()
		enableWriteback := form.GetFormItemByLabel("Enable Writeback:").(*tview.Checkbox).IsChecked()
		autoConnect := form.GetFormItemByLabel("Auto-connect:").(*tview.Checkbox).IsChecked()

		if name == "" || address == "" {
			t.app.showError("Error", "Name and address are required")
			return
		}

		db, _ := strconv.Atoi(dbStr)
		ttl, _ := strconv.Atoi(ttlStr)

		cfg := config.ValkeyConfig{
			Name:            name,
			Enabled:         autoConnect,
			Address:         address,
			Password:        password,
			Database:        db,
			Selector:         selector,
			UseTLS:          useTLS,
			KeyTTL:          secondsToDuration(ttl),
			PublishChanges:  publishChanges,
			EnableWriteback: enableWriteback,
		}

		t.app.config.AddValkey(cfg)
		t.app.SaveConfig()

		// Add to manager
		pub := t.app.valkeyMgr.Add(&t.app.config.Valkey[len(t.app.config.Valkey)-1], t.app.config.Namespace)

		t.app.closeModal(pageName)
		t.Refresh()
		t.app.setStatus(fmt.Sprintf("Added Valkey server: %s", name))

		if autoConnect {
			go func() {
				err := pub.Start()
				t.app.QueueUpdateDraw(func() {
					if err != nil {
						t.app.setStatus(fmt.Sprintf("Valkey %s connect failed: %v", name, err))
					} else {
						t.app.setStatus(fmt.Sprintf("Valkey connected to %s", pub.Address()))
					}
					t.Refresh()
				})
			}()
		}
	})

	form.AddButton("Cancel", func() {
		t.app.closeModal(pageName)
	})

	t.app.showFormModal(pageName, form, 55, 26, func() {
		t.app.closeModal(pageName)
	})
}

func (t *ValkeyTab) showEditDialog() {
	const pageName = "edit-valkey"

	row, _ := t.table.GetSelection()
	if row <= 0 {
		return
	}

	pubs := t.app.valkeyMgr.List()
	if row-1 >= len(pubs) {
		return
	}

	pub := pubs[row-1]
	cfg := pub.Config()
	originalName := cfg.Name

	form := tview.NewForm()
	ApplyFormTheme(form)
	form.SetBorder(true).SetTitle(" Edit Valkey Server ")

	form.AddInputField("Name:", cfg.Name, 20, nil, nil)
	form.AddInputField("Address:", cfg.Address, 30, nil, nil)
	form.AddInputField("Password:", cfg.Password, 20, nil, nil)
	form.AddInputField("Database:", fmt.Sprintf("%d", cfg.Database), 5, acceptDigits, nil)
	form.AddInputField("Selector:", cfg.Selector, 20, nil, nil)
	form.AddInputField("Key TTL (sec):", fmt.Sprintf("%d", int(cfg.KeyTTL.Seconds())), 8, acceptDigits, nil)
	form.AddCheckbox("Use TLS:", cfg.UseTLS, nil)
	form.AddCheckbox("Publish Changes:", cfg.PublishChanges, nil)
	form.AddCheckbox("Enable Writeback:", cfg.EnableWriteback, nil)
	form.AddCheckbox("Auto-connect:", cfg.Enabled, nil)

	form.AddButton("Save", func() {
		name := form.GetFormItemByLabel("Name:").(*tview.InputField).GetText()
		address := form.GetFormItemByLabel("Address:").(*tview.InputField).GetText()
		password := form.GetFormItemByLabel("Password:").(*tview.InputField).GetText()
		dbStr := form.GetFormItemByLabel("Database:").(*tview.InputField).GetText()
		selector := form.GetFormItemByLabel("Selector:").(*tview.InputField).GetText()
		ttlStr := form.GetFormItemByLabel("Key TTL (sec):").(*tview.InputField).GetText()
		useTLS := form.GetFormItemByLabel("Use TLS:").(*tview.Checkbox).IsChecked()
		publishChanges := form.GetFormItemByLabel("Publish Changes:").(*tview.Checkbox).IsChecked()
		enableWriteback := form.GetFormItemByLabel("Enable Writeback:").(*tview.Checkbox).IsChecked()
		autoConnect := form.GetFormItemByLabel("Auto-connect:").(*tview.Checkbox).IsChecked()

		if name == "" || address == "" {
			t.app.showError("Error", "Name and address are required")
			return
		}

		db, _ := strconv.Atoi(dbStr)
		ttl, _ := strconv.Atoi(ttlStr)

		updated := config.ValkeyConfig{
			Name:            name,
			Enabled:         autoConnect,
			Address:         address,
			Password:        password,
			Database:        db,
			Selector:         selector,
			UseTLS:          useTLS,
			KeyTTL:          secondsToDuration(ttl),
			PublishChanges:  publishChanges,
			EnableWriteback: enableWriteback,
		}

		t.app.config.UpdateValkey(originalName, updated)
		t.app.SaveConfig()

		// Close dialog immediately
		t.app.closeModal(pageName)
		t.app.setStatus(fmt.Sprintf("Updating Valkey server: %s...", name))
		DebugLogValkey("Valkey server %s updated (address: %s, selector: %s)", name, address, selector)

		// Update manager in background to avoid blocking UI
		go func() {
			t.app.valkeyMgr.Remove(originalName)
			newPub := t.app.valkeyMgr.Add(t.app.config.FindValkey(name), t.app.config.Namespace)

			if autoConnect {
				err := newPub.Start()
				t.app.QueueUpdateDraw(func() {
					if err != nil {
						t.app.setStatus(fmt.Sprintf("Valkey %s reconnect failed: %v", name, err))
					} else {
						t.app.setStatus(fmt.Sprintf("Valkey reconnected to %s", newPub.Address()))
					}
					t.Refresh()
				})
			} else {
				t.app.QueueUpdateDraw(func() {
					t.Refresh()
					t.app.setStatus(fmt.Sprintf("Updated Valkey server: %s", name))
				})
			}
		}()
	})

	form.AddButton("Cancel", func() {
		t.app.closeModal(pageName)
	})

	t.app.showFormModal(pageName, form, 55, 26, func() {
		t.app.closeModal(pageName)
	})
}

func (t *ValkeyTab) removeSelected() {
	row, _ := t.table.GetSelection()
	if row <= 0 {
		return
	}

	pubs := t.app.valkeyMgr.List()
	if row-1 >= len(pubs) {
		return
	}

	pub := pubs[row-1]
	name := pub.Config().Name

	t.app.showConfirm("Remove Valkey Server", fmt.Sprintf("Remove %s?", name), func() {
		// Run removal in background to avoid blocking UI (Stop() may block)
		go func() {
			t.app.valkeyMgr.Remove(name)

			t.app.QueueUpdateDraw(func() {
				t.app.config.RemoveValkey(name)
				t.app.SaveConfig()
				t.Refresh()
				t.app.setStatus(fmt.Sprintf("Removed Valkey server: %s", name))
			})
		}()
	})
}

func (t *ValkeyTab) connectSelected() {
	row, _ := t.table.GetSelection()
	if row <= 0 {
		return
	}

	pubs := t.app.valkeyMgr.List()
	if row-1 >= len(pubs) {
		return
	}

	pub := pubs[row-1]
	if pub.IsRunning() {
		t.app.setStatus(fmt.Sprintf("Valkey %s already connected", pub.Config().Name))
		t.Refresh() // Refresh table to show current state
		return
	}

	t.app.setStatus(fmt.Sprintf("Connecting to %s...", pub.Config().Name))

	go func() {
		pubName := pub.Config().Name
		err := pub.Start()
		t.app.QueueUpdateDraw(func() {
			if err != nil {
				t.app.setStatus(fmt.Sprintf("Valkey connect failed: %v", err))
			} else {
				if cfg := t.app.config.FindValkey(pubName); cfg != nil {
					cfg.Enabled = true
					t.app.SaveConfig()
				}
				t.app.setStatus(fmt.Sprintf("Valkey connected to %s", pub.Address()))
			}
			t.Refresh()
		})
	}()
}

func (t *ValkeyTab) disconnectSelected() {
	row, _ := t.table.GetSelection()
	if row <= 0 {
		return
	}

	pubs := t.app.valkeyMgr.List()
	if row-1 >= len(pubs) {
		return
	}

	pub := pubs[row-1]
	if !pub.IsRunning() {
		t.app.setStatus(fmt.Sprintf("Valkey %s not connected", pub.Config().Name))
		return
	}

	pubName := pub.Config().Name
	t.app.setStatus(fmt.Sprintf("Disconnecting from %s...", pubName))

	// Run disconnect in background to avoid blocking UI
	go func() {
		pub.Stop()

		t.app.QueueUpdateDraw(func() {
			if cfg := t.app.config.FindValkey(pubName); cfg != nil {
				cfg.Enabled = false
				t.app.SaveConfig()
			}
			t.Refresh()
			t.app.setStatus(fmt.Sprintf("Valkey disconnected from %s", pubName))
		})
	}()
}

// GetPrimitive returns the main primitive for this tab.
func (t *ValkeyTab) GetPrimitive() tview.Primitive {
	return t.flex
}

// GetFocusable returns the element that should receive focus.
func (t *ValkeyTab) GetFocusable() tview.Primitive {
	return t.table
}

// Refresh updates the display.
func (t *ValkeyTab) Refresh() {
	pubs := t.app.valkeyMgr.List()
	connectedCount := 0
	for _, pub := range pubs {
		if pub.IsRunning() {
			connectedCount++
		}
	}

	t.statusBar.SetText(fmt.Sprintf(" %d servers configured, %d connected", len(pubs), connectedCount))
	t.refreshTable()
}

// secondsToDuration converts seconds to time.Duration.
func secondsToDuration(seconds int) time.Duration {
	return time.Duration(seconds) * time.Second
}

func (t *ValkeyTab) updateButtonBar() {
	th := CurrentTheme
	buttonText := " " + th.TagHotkey + "a" + th.TagActionText + "dd  " +
		th.TagHotkey + "e" + th.TagActionText + "dit  " +
		th.TagHotkey + "x" + th.TagActionText + " remove  " +
		th.TagHotkey + "c" + th.TagActionText + "onnect  dis" +
		th.TagHotkey + "C" + th.TagActionText + "onnect  " +
		th.TagActionText + "│  " +
		th.TagHotkey + "?" + th.TagActionText + " help  " +
		th.TagHotkey + "Shift+Tab" + th.TagActionText + " next tab " + th.TagReset
	t.buttonBar.SetText(buttonText)
}

// RefreshTheme updates theme-dependent UI elements.
func (t *ValkeyTab) RefreshTheme() {
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
