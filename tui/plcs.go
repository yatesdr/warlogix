package tui

import (
	"fmt"
	"sort"
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
	headers := []string{"", "Name", "Address", "Family", "Status", "Product"}
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
		SetText(" [yellow]d[white]iscover  [yellow]a[white]dd  [yellow]e[white]dit  [yellow]r[white]emove  [yellow]c[white]onnect  dis[yellow]C[white]onnect  [yellow]i[white]nfo  [gray]│[white]  [yellow]?[white] help  [yellow]Shift+Tab[white] next tab ")
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

// getSelectedPLCName returns the name of the currently selected PLC from the table.
func (t *PLCsTab) getSelectedPLCName() string {
	row, _ := t.table.GetSelection()
	if row <= 0 {
		return ""
	}
	cell := t.table.GetCell(row, 1) // Column 1 is the Name column
	if cell == nil {
		return ""
	}
	return cell.Text
}

func (t *PLCsTab) onSelect(row, col int) {
	if row <= 0 {
		return
	}
	// Get PLC name from the table cell, not by index
	name := t.table.GetCell(row, 1).Text
	if name == "" {
		return
	}
	plc := t.app.manager.GetPLC(name)
	if plc == nil {
		return
	}
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

	// Sort PLCs by name for consistent ordering
	sort.Slice(plcs, func(i, j int) bool {
		return plcs[i].Config.Name < plcs[j].Config.Name
	})

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
		t.table.SetCell(row, 3, tview.NewTableCell(plc.Config.GetFamily().String()).SetExpansion(1))
		t.table.SetCell(row, 4, tview.NewTableCell(plc.GetStatus().String()).SetExpansion(1))
		t.table.SetCell(row, 5, tview.NewTableCell(productName).SetExpansion(1))
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

// plcFormState holds the current state of the PLC form for rebuilding
type plcFormState struct {
	family      int
	name        string
	address     string
	slot        string
	amsNetId    string
	amsPort     string
	pollRateMs  string // Poll rate in milliseconds (250-10000, empty = use global)
	autoConnect bool
}

var familyOptions = []string{"logix", "micro800", "s7", "beckhoff", "omron"}

func (t *PLCsTab) showAddDialogWithDevice(dev *logix.DeviceInfo) {
	state := &plcFormState{
		family:     0, // Default to logix
		slot:       "0",
		amsPort:    "851",
		pollRateMs: "1000", // Default 1000ms
	}

	if dev != nil {
		state.address = dev.IP.String()
		state.name = dev.ProductName
	}

	t.buildAddForm(state)
}

func (t *PLCsTab) buildAddForm(state *plcFormState) {
	// Remove existing form if present
	t.app.pages.RemovePage("add")

	form := tview.NewForm()
	form.SetBorder(true).SetTitle(" Add PLC ")

	family := config.PLCFamily(familyOptions[state.family])

	// Guard to prevent callback during initial setup
	initialized := false

	// Family is always first
	form.AddDropDown("Family:", familyOptions, state.family, func(option string, index int) {
		if !initialized {
			return
		}
		// Save current values before rebuilding
		t.saveAddFormState(form, state, family)
		state.family = index
		// Rebuild form with new family
		t.buildAddForm(state)
	})

	// Common fields
	form.AddInputField("Name:", state.name, 30, nil, nil)
	form.AddInputField("Address:", state.address, 30, nil, nil)

	// Family-specific fields
	switch family {
	case config.FamilyLogix, config.FamilyMicro800, config.FamilyOmron:
		form.AddInputField("Slot:", state.slot, 5, func(text string, lastChar rune) bool {
			_, err := strconv.Atoi(text)
			return err == nil || text == ""
		}, nil)
	case config.FamilyS7:
		form.AddInputField("Slot:", state.slot, 5, func(text string, lastChar rune) bool {
			_, err := strconv.Atoi(text)
			return err == nil || text == ""
		}, nil)
	case config.FamilyBeckhoff:
		form.AddInputField("AMS Net ID:", state.amsNetId, 25, nil, nil)
		form.AddInputField("AMS Port:", state.amsPort, 8, func(text string, lastChar rune) bool {
			_, err := strconv.Atoi(text)
			return err == nil || text == ""
		}, nil)
	}

	// Poll rate field (common to all families)
	form.AddInputField("Poll Rate (ms):", state.pollRateMs, 10, func(text string, lastChar rune) bool {
		if text == "" {
			return true // Allow empty for "use global default"
		}
		_, err := strconv.Atoi(text)
		return err == nil
	}, nil)

	form.AddCheckbox("Auto-connect:", state.autoConnect, nil)

	form.AddButton("Add", func() {
		t.saveAddFormState(form, state, family)

		if state.name == "" || state.address == "" {
			t.app.showError("Error", "Name and address are required")
			return
		}

		slot, _ := strconv.Atoi(state.slot)
		amsPort, _ := strconv.Atoi(state.amsPort)

		// Parse poll rate (0 means use global default)
		var pollRate time.Duration
		if state.pollRateMs != "" {
			pollMs, _ := strconv.Atoi(state.pollRateMs)
			if pollMs > 0 {
				// Clamp to valid range
				if pollMs < 250 {
					pollMs = 250
				} else if pollMs > 10000 {
					pollMs = 10000
				}
				pollRate = time.Duration(pollMs) * time.Millisecond
			}
		}

		cfg := config.PLCConfig{
			Name:     state.name,
			Address:  state.address,
			Slot:     byte(slot),
			Family:   family,
			Enabled:  state.autoConnect,
			PollRate: pollRate,
			AmsNetId: state.amsNetId,
			AmsPort:  uint16(amsPort),
		}

		t.app.config.AddPLC(cfg)
		t.app.SaveConfig()
		if addedCfg := t.app.config.FindPLC(state.name); addedCfg != nil {
			t.app.manager.AddPLC(addedCfg)
		}
		t.app.UpdateMQTTPLCNames()

		t.app.pages.RemovePage("add")
		t.Refresh()
		t.app.focusCurrentTab()
		t.app.setStatus(fmt.Sprintf("Added PLC: %s", state.name))
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

	// Calculate form height based on number of fields
	formHeight := 17 // Base height for common fields + poll rate + buttons
	if family == config.FamilyBeckhoff {
		formHeight = 19 // Extra fields for Beckhoff
	}

	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(form, formHeight, 1, true).
			AddItem(nil, 0, 1, false), 55, 1, true).
		AddItem(nil, 0, 1, false)

	t.app.pages.AddPage("add", modal, true, true)
	t.app.app.SetFocus(form)

	// Enable dropdown callback after form is displayed
	initialized = true
}

func (t *PLCsTab) saveAddFormState(form *tview.Form, state *plcFormState, family config.PLCFamily) {
	if item := form.GetFormItemByLabel("Name:"); item != nil {
		state.name = item.(*tview.InputField).GetText()
	}
	if item := form.GetFormItemByLabel("Address:"); item != nil {
		state.address = item.(*tview.InputField).GetText()
	}
	if item := form.GetFormItemByLabel("Slot:"); item != nil {
		state.slot = item.(*tview.InputField).GetText()
	}
	if item := form.GetFormItemByLabel("AMS Net ID:"); item != nil {
		state.amsNetId = item.(*tview.InputField).GetText()
	}
	if item := form.GetFormItemByLabel("AMS Port:"); item != nil {
		state.amsPort = item.(*tview.InputField).GetText()
	}
	if item := form.GetFormItemByLabel("Poll Rate (ms):"); item != nil {
		state.pollRateMs = item.(*tview.InputField).GetText()
	}
	if item := form.GetFormItemByLabel("Auto-connect:"); item != nil {
		state.autoConnect = item.(*tview.Checkbox).IsChecked()
	}
}

// editFormState extends plcFormState with edit-specific fields
type editFormState struct {
	plcFormState
	originalName string
	tags         []config.TagSelection
}

func (t *PLCsTab) showEditDialog() {
	name := t.getSelectedPLCName()
	if name == "" {
		return
	}

	plc := t.app.manager.GetPLC(name)
	if plc == nil {
		return
	}
	cfg := plc.Config

	// Find family index
	selectedFamily := 0
	for i, opt := range familyOptions {
		if config.PLCFamily(opt) == cfg.Family || (cfg.Family == "" && opt == "logix") {
			selectedFamily = i
			break
		}
	}

	// Set default AMS port if not configured
	amsPort := "851"
	if cfg.AmsPort > 0 {
		amsPort = strconv.Itoa(int(cfg.AmsPort))
	}

	// Convert poll rate to milliseconds string (default 1000 if not configured)
	pollRateMs := "1000"
	if cfg.PollRate > 0 {
		pollRateMs = strconv.Itoa(int(cfg.PollRate.Milliseconds()))
	}

	state := &editFormState{
		plcFormState: plcFormState{
			family:      selectedFamily,
			name:        cfg.Name,
			address:     cfg.Address,
			slot:        strconv.Itoa(int(cfg.Slot)),
			amsNetId:    cfg.AmsNetId,
			amsPort:     amsPort,
			pollRateMs:  pollRateMs,
			autoConnect: cfg.Enabled,
		},
		originalName: cfg.Name,
		tags:         cfg.Tags,
	}

	t.buildEditForm(state)
}

func (t *PLCsTab) buildEditForm(state *editFormState) {
	// Remove existing form if present
	t.app.pages.RemovePage("edit")

	form := tview.NewForm()
	form.SetBorder(true).SetTitle(" Edit PLC ")

	family := config.PLCFamily(familyOptions[state.family])

	// Guard to prevent callback during initial setup
	initialized := false

	// Family is always first
	form.AddDropDown("Family:", familyOptions, state.family, func(option string, index int) {
		if !initialized {
			return
		}
		// Save current values before rebuilding
		t.saveEditFormState(form, state, family)
		state.family = index
		// Rebuild form with new family
		t.buildEditForm(state)
	})

	// Common fields
	form.AddInputField("Name:", state.name, 30, nil, nil)
	form.AddInputField("Address:", state.address, 30, nil, nil)

	// Family-specific fields
	switch family {
	case config.FamilyLogix, config.FamilyMicro800, config.FamilyOmron:
		form.AddInputField("Slot:", state.slot, 5, func(text string, lastChar rune) bool {
			_, err := strconv.Atoi(text)
			return err == nil || text == ""
		}, nil)
	case config.FamilyS7:
		form.AddInputField("Slot:", state.slot, 5, func(text string, lastChar rune) bool {
			_, err := strconv.Atoi(text)
			return err == nil || text == ""
		}, nil)
	case config.FamilyBeckhoff:
		form.AddInputField("AMS Net ID:", state.amsNetId, 25, nil, nil)
		form.AddInputField("AMS Port:", state.amsPort, 8, func(text string, lastChar rune) bool {
			_, err := strconv.Atoi(text)
			return err == nil || text == ""
		}, nil)
	}

	// Poll rate field (common to all families)
	form.AddInputField("Poll Rate (ms):", state.pollRateMs, 10, func(text string, lastChar rune) bool {
		if text == "" {
			return true // Allow empty for "use global default"
		}
		_, err := strconv.Atoi(text)
		return err == nil
	}, nil)

	form.AddCheckbox("Auto-connect:", state.autoConnect, nil)

	form.AddButton("Save", func() {
		t.saveEditFormState(form, state, family)

		if state.name == "" || state.address == "" {
			t.app.showError("Error", "Name and address are required")
			return
		}

		slot, _ := strconv.Atoi(state.slot)
		amsPort, _ := strconv.Atoi(state.amsPort)

		// Parse poll rate (0 means use global default)
		var pollRate time.Duration
		if state.pollRateMs != "" {
			pollMs, _ := strconv.Atoi(state.pollRateMs)
			if pollMs > 0 {
				// Clamp to valid range
				if pollMs < 250 {
					pollMs = 250
				} else if pollMs > 10000 {
					pollMs = 10000
				}
				pollRate = time.Duration(pollMs) * time.Millisecond
			}
		}

		updated := config.PLCConfig{
			Name:     state.name,
			Address:  state.address,
			Slot:     byte(slot),
			Family:   family,
			Enabled:  state.autoConnect,
			PollRate: pollRate,
			Tags:     state.tags,
			AmsNetId: state.amsNetId,
			AmsPort:  uint16(amsPort),
		}

		t.app.config.UpdatePLC(state.originalName, updated)
		t.app.SaveConfig()

		// Close dialog first
		t.app.pages.RemovePage("edit")
		t.app.focusCurrentTab()
		t.app.setStatus(fmt.Sprintf("Updating PLC: %s...", state.name))

		// Update manager in background to avoid blocking UI
		originalName := state.originalName
		newName := state.name
		go func() {
			t.app.manager.Disconnect(originalName)
			t.app.manager.RemovePLC(originalName)
			if updatedCfg := t.app.config.FindPLC(newName); updatedCfg != nil {
				t.app.manager.AddPLC(updatedCfg)
				if originalName != newName {
					t.app.UpdateMQTTPLCNames()
				}
				if updatedCfg.Enabled {
					t.app.manager.Connect(newName)
				}
			}
			t.app.QueueUpdateDraw(func() {
				t.Refresh()
				t.app.setStatus(fmt.Sprintf("Updated PLC: %s", newName))
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

	// Calculate form height based on number of fields
	formHeight := 17 // Base height for common fields + poll rate + buttons
	if family == config.FamilyBeckhoff {
		formHeight = 19 // Extra fields for Beckhoff
	}

	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(form, formHeight, 1, true).
			AddItem(nil, 0, 1, false), 55, 1, true).
		AddItem(nil, 0, 1, false)

	t.app.pages.AddPage("edit", modal, true, true)
	t.app.app.SetFocus(form)

	// Enable dropdown callback after form is displayed
	initialized = true
}

func (t *PLCsTab) saveEditFormState(form *tview.Form, state *editFormState, family config.PLCFamily) {
	if item := form.GetFormItemByLabel("Name:"); item != nil {
		state.name = item.(*tview.InputField).GetText()
	}
	if item := form.GetFormItemByLabel("Address:"); item != nil {
		state.address = item.(*tview.InputField).GetText()
	}
	if item := form.GetFormItemByLabel("Slot:"); item != nil {
		state.slot = item.(*tview.InputField).GetText()
	}
	if item := form.GetFormItemByLabel("AMS Net ID:"); item != nil {
		state.amsNetId = item.(*tview.InputField).GetText()
	}
	if item := form.GetFormItemByLabel("AMS Port:"); item != nil {
		state.amsPort = item.(*tview.InputField).GetText()
	}
	if item := form.GetFormItemByLabel("Poll Rate (ms):"); item != nil {
		state.pollRateMs = item.(*tview.InputField).GetText()
	}
	if item := form.GetFormItemByLabel("Auto-connect:"); item != nil {
		state.autoConnect = item.(*tview.Checkbox).IsChecked()
	}
}

func (t *PLCsTab) removeSelected() {
	name := t.getSelectedPLCName()
	if name == "" {
		return
	}

	t.app.showConfirm("Remove PLC", fmt.Sprintf("Remove %s?", name), func() {
		t.app.config.RemovePLC(name)
		t.app.SaveConfig()
		t.app.UpdateMQTTPLCNames()
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
	name := t.getSelectedPLCName()
	if name == "" {
		return
	}

	plc := t.app.manager.GetPLC(name)
	if plc == nil {
		return
	}
	t.app.setStatus(fmt.Sprintf("Connecting to %s...", name))

	// Enable auto-connect so it stays connected
	if cfg := t.app.config.FindPLC(name); cfg != nil {
		cfg.Enabled = true
		t.app.SaveConfig()
	}

	// Connect runs in background - manager will log success/failure
	t.app.manager.Connect(name)
}

func (t *PLCsTab) disconnectSelected() {
	name := t.getSelectedPLCName()
	if name == "" {
		return
	}
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
	name := t.getSelectedPLCName()
	if name == "" {
		return
	}

	plc := t.app.manager.GetPLC(name)
	if plc == nil {
		return
	}
	identity := plc.GetIdentity()

	// Build info text
	info := fmt.Sprintf("[yellow]Name:[white] %s\n", plc.Config.Name)
	info += fmt.Sprintf("[yellow]Address:[white] %s\n", plc.Config.Address)
	info += fmt.Sprintf("[yellow]Slot:[white] %d\n", plc.Config.Slot)
	info += fmt.Sprintf("[yellow]Status:[white] %s\n", plc.GetStatus().String())
	info += fmt.Sprintf("[yellow]Mode:[white] %s\n", plc.GetConnectionMode())

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
			AddItem(textView, 20, 1, true).
			AddItem(nil, 0, 1, false), 55, 1, true).
		AddItem(nil, 0, 1, false)

	t.app.pages.AddPage("info", modal, true, true)
	t.app.app.SetFocus(textView)
}
