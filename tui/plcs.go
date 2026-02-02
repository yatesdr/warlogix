package tui

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"warlogix/config"
	"warlogix/logix"
	"warlogix/plcman"
)

// PLCsTab handles the PLCs management tab.
type PLCsTab struct {
	app       *App
	flex      *tview.Flex
	table     *tview.Table
	statusBar *tview.TextView
	buttons   *tview.Flex
}

// NewPLCsTab creates a new PLCs tab.
func NewPLCsTab(app *App) *PLCsTab {
	t := &PLCsTab{app: app}
	t.setupUI()
	t.Refresh()
	return t
}

func (t *PLCsTab) setupUI() {
	// Create table for PLC list
	t.table = tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0)

	t.table.SetSelectedFunc(t.onSelect)
	t.table.SetInputCapture(t.handleKeys)

	// Set up headers
	headers := []string{"", "Name", "Address", "Status", "Product"}
	for i, h := range headers {
		t.table.SetCell(0, i, tview.NewTableCell(h).
			SetTextColor(tcell.ColorYellow).
			SetSelectable(false).
			SetAttributes(tcell.AttrBold))
	}

	// Create button bar as a single text view
	buttonBar := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter).
		SetText(" [yellow]d[white]iscover  [yellow]a[white]dd  [yellow]e[white]dit  [yellow]r[white]emove  [yellow]c[white]onnect  dis[yellow]C[white]onnect  [yellow]i[white]nfo ")
	t.buttons = tview.NewFlex().AddItem(buttonBar, 0, 1, false)

	// Create status bar
	t.statusBar = tview.NewTextView().
		SetDynamicColors(true)

	// Create frame around table
	tableFrame := tview.NewFrame(t.table).
		SetBorders(1, 0, 0, 0, 1, 1)
	tableFrame.SetBorder(true).SetTitle(" PLCs ")

	// Assemble layout
	t.flex = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(t.buttons, 1, 0, false).
		AddItem(tableFrame, 0, 1, true).
		AddItem(t.statusBar, 1, 0, false)
}

func (t *PLCsTab) handleKeys(event *tcell.EventKey) *tcell.EventKey {
	switch event.Rune() {
	case 'd':
		t.discover()
		return nil
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
	case 'i':
		t.showInfoDialog()
		return nil
	}
	return event
}

func (t *PLCsTab) onSelect(row, col int) {
	if row <= 0 {
		return
	}
	// Double-click/Enter toggles connection
	plcs := t.app.manager.ListPLCs()
	if row-1 >= len(plcs) {
		return
	}
	plc := plcs[row-1]
	name := plc.Config.Name
	if plc.GetStatus() == plcman.StatusConnected {
		// Disable auto-connect and disconnect in background
		if cfg := t.app.config.FindPLC(name); cfg != nil {
			cfg.Enabled = false
			t.app.SaveConfig()
		}
		go t.app.manager.Disconnect(name)
	} else {
		// Enable auto-connect and connect in background
		if cfg := t.app.config.FindPLC(name); cfg != nil {
			cfg.Enabled = true
			t.app.SaveConfig()
		}
		go t.app.manager.Connect(name)
	}
}

// GetPrimitive returns the main primitive for this tab.
func (t *PLCsTab) GetPrimitive() tview.Primitive {
	return t.flex
}

// GetFocusable returns the element that should receive focus.
func (t *PLCsTab) GetFocusable() tview.Primitive {
	return t.table
}

// Refresh updates the display.
func (t *PLCsTab) Refresh() {
	// Clear existing rows (keep header)
	for t.table.GetRowCount() > 1 {
		t.table.RemoveRow(1)
	}

	// Add PLCs
	plcs := t.app.manager.ListPLCs()
	for i, plc := range plcs {
		row := i + 1

		// Status indicator
		var indicator string
		switch plc.GetStatus() {
		case plcman.StatusConnected:
			indicator = StatusIndicatorConnected
		case plcman.StatusConnecting:
			indicator = StatusIndicatorConnecting
		case plcman.StatusError:
			indicator = StatusIndicatorError
		default:
			indicator = StatusIndicatorDisconnected
		}

		// Product name
		productName := ""
		if identity := plc.GetIdentity(); identity != nil {
			productName = identity.ProductName
		}

		t.table.SetCell(row, 0, tview.NewTableCell(indicator).SetExpansion(0))
		t.table.SetCell(row, 1, tview.NewTableCell(plc.Config.Name).SetExpansion(1))
		t.table.SetCell(row, 2, tview.NewTableCell(plc.Config.Address).SetExpansion(1))
		t.table.SetCell(row, 3, tview.NewTableCell(plc.GetStatus().String()).SetExpansion(1))
		t.table.SetCell(row, 4, tview.NewTableCell(productName).SetExpansion(1))
	}

	// Update column widths
	t.table.SetCell(0, 0, tview.NewTableCell("").SetSelectable(false))

	// Update status
	connected := 0
	for _, plc := range plcs {
		if plc.GetStatus() == plcman.StatusConnected {
			connected++
		}
	}

	// Show poll stats
	stats := t.app.manager.GetPollStats()
	statusText := fmt.Sprintf(" %d PLCs, %d connected", len(plcs), connected)
	if !stats.LastPollTime.IsZero() {
		statusText += fmt.Sprintf(" | Poll: %d tags, %d changes", stats.TagsPolled, stats.ChangesFound)
		if stats.LastError != nil {
			statusText += fmt.Sprintf(" [red](err: %v)[-]", stats.LastError)
		}
	}
	t.statusBar.SetText(statusText)
}

func (t *PLCsTab) discover() {
	t.app.setStatus("Discovering PLCs...")

	go func() {
		devices, err := logix.Discover("255.255.255.255", 3*time.Second)

		t.app.QueueUpdateDraw(func() {
			if err != nil {
				t.app.setStatus(fmt.Sprintf("Discovery error: %v", err))
				return
			}

			if len(devices) == 0 {
				t.app.setStatus("No PLCs discovered")
				return
			}

			t.app.setStatus(fmt.Sprintf("Found %d device(s)", len(devices)))
			t.showDiscoveryResults(devices)
		})
	}()
}

func (t *PLCsTab) showDiscoveryResults(devices []logix.DeviceInfo) {
	list := tview.NewList()
	list.SetBorder(true).SetTitle(" Discovered Devices ")

	for _, dev := range devices {
		ip := dev.IP.String()
		text := fmt.Sprintf("%s - %s", ip, dev.ProductName)
		list.AddItem(text, "", 0, nil)
	}

	list.AddItem("Cancel", "", 'c', nil)

	list.SetSelectedFunc(func(index int, mainText, secondaryText string, shortcut rune) {
		t.app.pages.RemovePage("discovery")
		if index < len(devices) {
			dev := devices[index]
			t.showAddDialogWithDevice(&dev)
		}
		t.app.focusCurrentTab()
	})

	list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			t.app.pages.RemovePage("discovery")
			t.app.focusCurrentTab()
			return nil
		}
		return event
	})

	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(list, 15, 1, true).
			AddItem(nil, 0, 1, false), 60, 1, true).
		AddItem(nil, 0, 1, false)

	t.app.pages.AddPage("discovery", modal, true, true)
	t.app.app.SetFocus(list)
}

func (t *PLCsTab) showAddDialog() {
	t.showAddDialogWithDevice(nil)
}

func (t *PLCsTab) showAddDialogWithDevice(dev *logix.DeviceInfo) {
	form := tview.NewForm()
	form.SetBorder(true).SetTitle(" Add PLC ")

	defaultName := ""
	defaultAddr := ""
	defaultSlot := "0"

	if dev != nil {
		defaultAddr = dev.IP.String()
		defaultName = dev.ProductName
	}

	form.AddInputField("Name:", defaultName, 30, nil, nil)
	form.AddInputField("Address:", defaultAddr, 30, nil, nil)
	form.AddInputField("Slot:", defaultSlot, 5, func(text string, lastChar rune) bool {
		_, err := strconv.Atoi(text)
		return err == nil || text == ""
	}, nil)
	form.AddCheckbox("Auto-connect:", false, nil)

	form.AddButton("Add", func() {
		name := form.GetFormItemByLabel("Name:").(*tview.InputField).GetText()
		addr := form.GetFormItemByLabel("Address:").(*tview.InputField).GetText()
		slotStr := form.GetFormItemByLabel("Slot:").(*tview.InputField).GetText()
		autoConnect := form.GetFormItemByLabel("Auto-connect:").(*tview.Checkbox).IsChecked()

		if name == "" || addr == "" {
			t.app.showError("Error", "Name and address are required")
			return
		}

		slot, _ := strconv.Atoi(slotStr)

		cfg := config.PLCConfig{
			Name:    name,
			Address: addr,
			Slot:    byte(slot),
			Enabled: autoConnect,
		}

		t.app.config.AddPLC(cfg)
		t.app.manager.AddPLC(&t.app.config.PLCs[len(t.app.config.PLCs)-1])
		t.app.SaveConfig()

		t.app.pages.RemovePage("add")
		t.Refresh()
		t.app.focusCurrentTab()
		t.app.setStatus(fmt.Sprintf("Added PLC: %s", name))
	})

	form.AddButton("Cancel", func() {
		t.app.pages.RemovePage("add")
		t.app.focusCurrentTab()
	})

	form.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			t.app.pages.RemovePage("add")
			t.app.focusCurrentTab()
			return nil
		}
		return event
	})

	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(form, 13, 1, true).
			AddItem(nil, 0, 1, false), 50, 1, true).
		AddItem(nil, 0, 1, false)

	t.app.pages.AddPage("add", modal, true, true)
	t.app.app.SetFocus(form)
}

func (t *PLCsTab) showEditDialog() {
	row, _ := t.table.GetSelection()
	if row <= 0 {
		return
	}

	plcs := t.app.manager.ListPLCs()
	if row-1 >= len(plcs) {
		return
	}

	plc := plcs[row-1]
	cfg := plc.Config

	form := tview.NewForm()
	form.SetBorder(true).SetTitle(" Edit PLC ")

	form.AddInputField("Name:", cfg.Name, 30, nil, nil)
	form.AddInputField("Address:", cfg.Address, 30, nil, nil)
	form.AddInputField("Slot:", strconv.Itoa(int(cfg.Slot)), 5, func(text string, lastChar rune) bool {
		_, err := strconv.Atoi(text)
		return err == nil || text == ""
	}, nil)
	form.AddCheckbox("Auto-connect:", cfg.Enabled, nil)

	originalName := cfg.Name

	form.AddButton("Save", func() {
		name := form.GetFormItemByLabel("Name:").(*tview.InputField).GetText()
		addr := form.GetFormItemByLabel("Address:").(*tview.InputField).GetText()
		slotStr := form.GetFormItemByLabel("Slot:").(*tview.InputField).GetText()
		autoConnect := form.GetFormItemByLabel("Auto-connect:").(*tview.Checkbox).IsChecked()

		if name == "" || addr == "" {
			t.app.showError("Error", "Name and address are required")
			return
		}

		slot, _ := strconv.Atoi(slotStr)

		updated := config.PLCConfig{
			Name:    name,
			Address: addr,
			Slot:    byte(slot),
			Enabled: autoConnect,
			Tags:    cfg.Tags,
		}

		t.app.config.UpdatePLC(originalName, updated)
		t.app.SaveConfig()

		// Close dialog first
		t.app.pages.RemovePage("edit")
		t.app.focusCurrentTab()
		t.app.setStatus(fmt.Sprintf("Updating PLC: %s...", name))

		// Update manager in background to avoid blocking UI
		go func() {
			t.app.manager.Disconnect(originalName)
			t.app.manager.RemovePLC(originalName)
			if updatedCfg := t.app.config.FindPLC(name); updatedCfg != nil {
				t.app.manager.AddPLC(updatedCfg)
				// Reconnect if auto-connect is enabled
				if updatedCfg.Enabled {
					t.app.manager.Connect(name)
				}
			}
			t.app.QueueUpdateDraw(func() {
				t.Refresh()
				t.app.setStatus(fmt.Sprintf("Updated PLC: %s", name))
			})
		}()
	})

	form.AddButton("Cancel", func() {
		t.app.pages.RemovePage("edit")
		t.app.focusCurrentTab()
	})

	form.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			t.app.pages.RemovePage("edit")
			t.app.focusCurrentTab()
			return nil
		}
		return event
	})

	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(form, 13, 1, true).
			AddItem(nil, 0, 1, false), 50, 1, true).
		AddItem(nil, 0, 1, false)

	t.app.pages.AddPage("edit", modal, true, true)
	t.app.app.SetFocus(form)
}

func (t *PLCsTab) removeSelected() {
	row, _ := t.table.GetSelection()
	if row <= 0 {
		return
	}

	plcs := t.app.manager.ListPLCs()
	if row-1 >= len(plcs) {
		return
	}

	plc := plcs[row-1]
	name := plc.Config.Name

	t.app.showConfirm("Remove PLC", fmt.Sprintf("Remove %s?", name), func() {
		t.app.config.RemovePLC(name)
		t.app.SaveConfig()
		t.app.setStatus(fmt.Sprintf("Removing PLC: %s...", name))

		// Update manager in background to avoid blocking UI
		go func() {
			t.app.manager.Disconnect(name)
			t.app.manager.RemovePLC(name)
			t.app.QueueUpdateDraw(func() {
				t.Refresh()
				t.app.setStatus(fmt.Sprintf("Removed PLC: %s", name))
			})
		}()
	})
}

func (t *PLCsTab) connectSelected() {
	row, _ := t.table.GetSelection()
	if row <= 0 {
		return
	}

	plcs := t.app.manager.ListPLCs()
	if row-1 >= len(plcs) {
		return
	}

	plc := plcs[row-1]
	name := plc.Config.Name
	t.app.setStatus(fmt.Sprintf("Connecting to %s...", name))

	// Enable auto-connect so it stays connected
	if cfg := t.app.config.FindPLC(name); cfg != nil {
		cfg.Enabled = true
		t.app.SaveConfig()
	}

	go func() {
		err := t.app.manager.Connect(name)
		t.app.QueueUpdateDraw(func() {
			if err != nil {
				t.app.setStatus(fmt.Sprintf("Connection failed: %v", err))
			} else {
				tags := plc.GetTags()
				t.app.setStatus(fmt.Sprintf("Connected to %s - %d tags discovered", name, len(tags)))
			}
			t.Refresh()
		})
	}()
}

func (t *PLCsTab) disconnectSelected() {
	row, _ := t.table.GetSelection()
	if row <= 0 {
		return
	}

	plcs := t.app.manager.ListPLCs()
	if row-1 >= len(plcs) {
		return
	}

	plc := plcs[row-1]
	name := plc.Config.Name
	t.app.setStatus(fmt.Sprintf("Disconnecting from %s...", name))

	// Disable auto-connect to prevent auto-reconnect
	if cfg := t.app.config.FindPLC(name); cfg != nil {
		cfg.Enabled = false
		t.app.SaveConfig()
	}

	go func() {
		t.app.manager.Disconnect(name)
		t.app.QueueUpdateDraw(func() {
			t.Refresh()
			t.app.setStatus(fmt.Sprintf("Disconnected from %s", name))
		})
	}()
}

func (t *PLCsTab) showInfoDialog() {
	row, _ := t.table.GetSelection()
	if row <= 0 {
		return
	}

	plcs := t.app.manager.ListPLCs()
	if row-1 >= len(plcs) {
		return
	}

	plc := plcs[row-1]
	identity := plc.GetIdentity()

	// Build info text
	info := fmt.Sprintf("[yellow]Name:[white] %s\n", plc.Config.Name)
	info += fmt.Sprintf("[yellow]Address:[white] %s\n", plc.Config.Address)
	info += fmt.Sprintf("[yellow]Slot:[white] %d\n", plc.Config.Slot)
	info += fmt.Sprintf("[yellow]Status:[white] %s\n", plc.GetStatus().String())

	if err := plc.GetError(); err != nil {
		info += fmt.Sprintf("[yellow]Error:[red] %s\n", err.Error())
	}

	if identity != nil {
		info += "\n[blue]── Device Identity ──[-]\n"
		info += fmt.Sprintf("[yellow]Product:[white] %s\n", identity.ProductName)
		info += fmt.Sprintf("[yellow]Vendor:[white] %s\n", identity.VendorName())
		info += fmt.Sprintf("[yellow]Type:[white] %s\n", identity.DeviceTypeName())
		info += fmt.Sprintf("[yellow]Revision:[white] %s\n", identity.Revision)
		info += fmt.Sprintf("[yellow]Serial:[white] %d\n", identity.Serial)
	} else {
		info += "\n[gray]Connect to view device identity[-]"
	}

	// Show tag count if connected
	tags := plc.GetTags()
	programs := plc.GetPrograms()
	if len(tags) > 0 || len(programs) > 0 {
		info += fmt.Sprintf("\n[yellow]Programs:[white] %d\n", len(programs))
		info += fmt.Sprintf("[yellow]Tags:[white] %d\n", len(tags))
	}

	textView := tview.NewTextView().
		SetDynamicColors(true).
		SetText(info)
	textView.SetBorder(true).SetTitle(" PLC Info ")

	textView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape || event.Key() == tcell.KeyEnter || event.Rune() == 'i' {
			t.app.pages.RemovePage("info")
			t.app.focusCurrentTab()
			return nil
		}
		return event
	})

	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(textView, 18, 1, true).
			AddItem(nil, 0, 1, false), 50, 1, true).
		AddItem(nil, 0, 1, false)

	t.app.pages.AddPage("info", modal, true, true)
	t.app.app.SetFocus(textView)
}
