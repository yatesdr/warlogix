package tui

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"warlogix/config"
)

// ValkeyTab handles the Valkey configuration tab.
type ValkeyTab struct {
	app       *App
	flex      *tview.Flex
	table     *tview.Table
	info      *tview.TextView
	statusBar *tview.TextView
}

// NewValkeyTab creates a new Valkey tab.
func NewValkeyTab(app *App) *ValkeyTab {
	t := &ValkeyTab{app: app}
	t.setupUI()
	t.Refresh()
	return t
}

func (t *ValkeyTab) setupUI() {
	// Button bar
	buttons := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter).
		SetText(" [yellow]a[white]dd  [yellow]e[white]dit  [yellow]r[white]emove  [yellow]c[white]onnect  dis[yellow]C[white]onnect  [gray]│[white]  [yellow]?[white] help  [yellow]Shift+Tab[white] next tab ")

	// Server table
	t.table = tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0)

	t.table.SetInputCapture(t.handleKeys)
	t.table.SetSelectedFunc(t.onSelect)

	// Set up headers
	headers := []string{"", "Name", "Address", "TLS", "Factory", "Status"}
	for i, h := range headers {
		t.table.SetCell(0, i, tview.NewTableCell(h).
			SetTextColor(tcell.ColorYellow).
			SetSelectable(false).
			SetAttributes(tcell.AttrBold))
	}

	tableBox := tview.NewFlex().SetDirection(tview.FlexRow)
	tableBox.SetBorder(true).SetTitle(" Valkey Servers ")
	tableBox.AddItem(buttons, 1, 0, false)
	tableBox.AddItem(t.table, 0, 1, true)

	// Info panel
	t.info = tview.NewTextView().
		SetDynamicColors(true)
	t.info.SetBorder(true).SetTitle(" Key Structure ")
	t.updateInfo()

	// Status bar
	t.statusBar = tview.NewTextView().
		SetDynamicColors(true)

	// Main layout
	t.flex = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(tableBox, 0, 1, true).
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
	case 'r':
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
	text := "\n"
	text += " [yellow]Key Format:[white]\n"
	text += "   {factory}/{plc}/tags/{tag}\n\n"
	text += " [yellow]Value Format:[white]\n"
	text += "   {\"value\": <value>, \"type\": \"<type>\", \"writable\": bool, \"timestamp\": \"<iso8601>\"}\n\n"
	text += " [yellow]Pub/Sub Channels:[white]\n"
	text += "   {factory}/{plc}/changes - per-PLC changes\n"
	text += "   {factory}/_all/changes  - all changes\n\n"
	text += " [gray]Keys are set with optional TTL for stale detection[-]\n"

	t.info.SetText(text)
}

func (t *ValkeyTab) refreshTable() {
	// Clear existing rows (keep header)
	for t.table.GetRowCount() > 1 {
		t.table.RemoveRow(1)
	}

	pubs := t.app.valkeyMgr.List()
	for i, pub := range pubs {
		row := i + 1
		cfg := pub.Config()

		var indicator string
		if pub.IsRunning() {
			indicator = "[green]●[-]"
		} else {
			indicator = "[gray]○[-]"
		}

		status := "Stopped"
		if pub.IsRunning() {
			status = "Connected"
		}

		tlsIndicator := "[gray]No[-]"
		if cfg.UseTLS {
			tlsIndicator = "[green]Yes[-]"
		}

		t.table.SetCell(row, 0, tview.NewTableCell(indicator).SetExpansion(0))
		t.table.SetCell(row, 1, tview.NewTableCell(cfg.Name).SetExpansion(1))
		t.table.SetCell(row, 2, tview.NewTableCell(cfg.Address).SetExpansion(1))
		t.table.SetCell(row, 3, tview.NewTableCell(tlsIndicator).SetExpansion(0))
		t.table.SetCell(row, 4, tview.NewTableCell(cfg.Factory).SetExpansion(1))
		t.table.SetCell(row, 5, tview.NewTableCell(status).SetExpansion(1))
	}
}

func (t *ValkeyTab) showAddDialog() {
	const pageName = "add-valkey"

	form := tview.NewForm()
	form.SetBorder(true).SetTitle(" Add Valkey Server ")

	form.AddInputField("Name:", "", 20, nil, nil)
	form.AddInputField("Address:", "localhost:6379", 30, nil, nil)
	form.AddInputField("Password:", "", 20, nil, nil)
	form.AddInputField("Database:", "0", 5, acceptDigits, nil)
	form.AddInputField("Factory:", "factory", 20, nil, nil)
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
		factory := form.GetFormItemByLabel("Factory:").(*tview.InputField).GetText()
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
			Factory:         factory,
			UseTLS:          useTLS,
			KeyTTL:          secondsToDuration(ttl),
			PublishChanges:  publishChanges,
			EnableWriteback: enableWriteback,
		}

		t.app.config.AddValkey(cfg)
		t.app.SaveConfig()

		// Add to manager
		pub := t.app.valkeyMgr.Add(&t.app.config.Valkey[len(t.app.config.Valkey)-1])

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

	t.app.showFormModal(pageName, form, 55, 21, func() {
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
	form.SetBorder(true).SetTitle(" Edit Valkey Server ")

	form.AddInputField("Name:", cfg.Name, 20, nil, nil)
	form.AddInputField("Address:", cfg.Address, 30, nil, nil)
	form.AddInputField("Password:", cfg.Password, 20, nil, nil)
	form.AddInputField("Database:", fmt.Sprintf("%d", cfg.Database), 5, acceptDigits, nil)
	form.AddInputField("Factory:", cfg.Factory, 20, nil, nil)
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
		factory := form.GetFormItemByLabel("Factory:").(*tview.InputField).GetText()
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
			Factory:         factory,
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
		DebugLogValkey("Valkey server %s updated (address: %s, factory: %s)", name, address, factory)

		// Update manager in background to avoid blocking UI
		go func() {
			t.app.valkeyMgr.Remove(originalName)
			newPub := t.app.valkeyMgr.Add(t.app.config.FindValkey(name))

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

	t.app.showFormModal(pageName, form, 55, 21, func() {
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
