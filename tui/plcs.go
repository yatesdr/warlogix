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
	app        *App
	flex       *tview.Flex
	table      *tview.Table
	tableFrame *tview.Frame
	statusBar  *tview.TextView
	buttons    *tview.Flex
	buttonBar  *tview.TextView
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
	ApplyTableTheme(t.table)

	t.table.SetSelectedFunc(t.onSelect)
	t.table.SetInputCapture(t.handleKeys)

	// Set up headers (themed)
	headers := []string{"", "Name", "Address", "Family", "Status", "Product"}
	for i, h := range headers {
		t.table.SetCell(0, i, tview.NewTableCell(h).
			SetTextColor(CurrentTheme.Accent).
			SetSelectable(false).
			SetAttributes(tcell.AttrBold))
	}

	// Create button bar as a single text view (themed)
	t.buttonBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
	t.updateButtonBar()
	t.buttons = tview.NewFlex().AddItem(t.buttonBar, 0, 1, false)

	// Create status bar
	t.statusBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextColor(CurrentTheme.Text)

	// Create frame around table
	t.tableFrame = tview.NewFrame(t.table).
		SetBorders(1, 0, 0, 0, 1, 1)
	t.tableFrame.SetBorder(true).SetTitle(" PLCs ").SetBorderColor(CurrentTheme.Border).SetTitleColor(CurrentTheme.Accent)

	// Assemble layout
	t.flex = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(t.buttons, 1, 0, false).
		AddItem(t.tableFrame, 0, 1, true).
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
	case 'x':
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

// updateButtonBar updates the button bar text with current theme colors.
func (t *PLCsTab) updateButtonBar() {
	th := CurrentTheme
	buttonText := " " + th.TagHotkey + "d" + th.TagActionText + "iscover  " +
		th.TagHotkey + "a" + th.TagActionText + "dd  " +
		th.TagHotkey + "e" + th.TagActionText + "dit  " +
		th.TagHotkey + "x" + th.TagActionText + " remove  " +
		th.TagHotkey + "c" + th.TagActionText + "onnect  dis" +
		th.TagHotkey + "C" + th.TagActionText + "onnect  " +
		th.TagHotkey + "i" + th.TagActionText + "nfo  " +
		th.TagActionText + "│  " +
		th.TagHotkey + "?" + th.TagActionText + " help  " +
		th.TagHotkey + "Shift+Tab" + th.TagActionText + " next tab "
	t.buttonBar.SetText(buttonText)
}

// RefreshTheme updates the tab's UI elements to match the current theme.
func (t *PLCsTab) RefreshTheme() {
	t.updateButtonBar()
	th := CurrentTheme
	t.tableFrame.SetBorderColor(th.Border).SetTitleColor(th.Accent)
	t.statusBar.SetTextColor(th.Text)
	ApplyTableTheme(t.table)
	// Update header colors
	for i := 0; i < t.table.GetColumnCount(); i++ {
		if cell := t.table.GetCell(0, i); cell != nil {
			cell.SetTextColor(th.Accent)
		}
	}
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

		// Status indicator - use fixed colors (theme-independent)
		indicatorCell := tview.NewTableCell("●").SetExpansion(0)
		switch plc.GetStatus() {
		case plcman.StatusConnected:
			indicatorCell.SetTextColor(IndicatorGreen)
		case plcman.StatusConnecting:
			indicatorCell.SetTextColor(tcell.ColorYellow)
		case plcman.StatusError:
			indicatorCell.SetTextColor(IndicatorRed)
		default:
			indicatorCell.SetTextColor(IndicatorGray)
		}

		// Product name
		productName := ""
		if info := plc.GetDeviceInfo(); info != nil {
			productName = info.Model
		}

		t.table.SetCell(row, 0, indicatorCell)
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
	const pageName = "discovery"

	list := tview.NewList()
	list.SetBorder(true).SetTitle(" Discovered Devices ")

	for _, dev := range devices {
		ip := dev.IP.String()
		text := fmt.Sprintf("%s - %s", ip, dev.ProductName)
		list.AddItem(text, "", 0, nil)
	}

	list.AddItem("Cancel", "", 'c', nil)

	list.SetSelectedFunc(func(index int, mainText, secondaryText string, shortcut rune) {
		t.app.closeModal(pageName)
		if index < len(devices) {
			dev := devices[index]
			t.showAddDialogWithDevice(&dev)
		}
	})

	list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			t.app.closeModal(pageName)
			return nil
		}
		return event
	})

	t.app.showCenteredModal(pageName, list, 60, 15)
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
	protocol    int    // 0=fins (default), 1=eip - used for Omron PLCs
	pollRateMs  string // Poll rate in milliseconds (250-10000, empty = use global)
	autoConnect bool
	healthCheck bool // Publish health status
}

var familyOptions = []string{"logix", "micro800", "s7", "beckhoff", "omron"}
var omronProtocolOptions = []string{"fins", "eip"}

func (t *PLCsTab) showAddDialogWithDevice(dev *logix.DeviceInfo) {
	state := &plcFormState{
		family:      0,     // Default to logix
		slot:        "0",
		amsPort:     "851",
		pollRateMs:  "1000", // Default 1000ms
		autoConnect: true,   // Default to enabled
		healthCheck: true,   // Default to enabled
	}

	if dev != nil {
		state.address = dev.IP.String()
		state.name = dev.ProductName
	}

	t.buildAddForm(state)
}

func (t *PLCsTab) buildAddForm(state *plcFormState) {
	const pageName = "add"

	// Remove existing form if present
	t.app.pages.RemovePage(pageName)

	form := tview.NewForm()
	ApplyFormTheme(form)
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
	case config.FamilyLogix, config.FamilyMicro800:
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
	case config.FamilyOmron:
		form.AddDropDown("Protocol:", omronProtocolOptions, state.protocol, nil)
		// Slot field only needed for FINS (EIP doesn't use slot)
		if state.protocol == 0 {
			form.AddInputField("Slot:", state.slot, 5, func(text string, lastChar rune) bool {
				_, err := strconv.Atoi(text)
				return err == nil || text == ""
			}, nil)
		}
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
	form.AddCheckbox("Health check:", state.healthCheck, nil)

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

		healthCheck := state.healthCheck
		protocol := ""
		if family == config.FamilyOmron {
			protocol = omronProtocolOptions[state.protocol]
		}
		cfg := config.PLCConfig{
			Name:               state.name,
			Address:            state.address,
			Slot:               byte(slot),
			Family:             family,
			Protocol:           protocol,
			Enabled:            state.autoConnect,
			HealthCheckEnabled: &healthCheck,
			PollRate:           pollRate,
			AmsNetId:           state.amsNetId,
			AmsPort:            uint16(amsPort),
		}

		t.app.config.AddPLC(cfg)
		t.app.SaveConfig()
		if addedCfg := t.app.config.FindPLC(state.name); addedCfg != nil {
			t.app.manager.AddPLC(addedCfg)
		}
		t.app.UpdateMQTTPLCNames()

		t.app.closeModal(pageName)
		t.Refresh()
		t.app.setStatus(fmt.Sprintf("Added PLC: %s", state.name))
	})

	form.AddButton("Cancel", func() {
		t.app.closeModal(pageName)
	})

	// Calculate form height based on number of fields
	formHeight := 19 // Base height for common fields + poll rate + health check + buttons
	if family == config.FamilyBeckhoff {
		formHeight = 21 // Extra fields for Beckhoff
	}

	t.app.showFormModal(pageName, form, 55, formHeight, func() {
		t.app.closeModal(pageName)
	})

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
	if item := form.GetFormItemByLabel("Protocol:"); item != nil {
		state.protocol, _ = item.(*tview.DropDown).GetCurrentOption()
	}
	if item := form.GetFormItemByLabel("Poll Rate (ms):"); item != nil {
		state.pollRateMs = item.(*tview.InputField).GetText()
	}
	if item := form.GetFormItemByLabel("Auto-connect:"); item != nil {
		state.autoConnect = item.(*tview.Checkbox).IsChecked()
	}
	if item := form.GetFormItemByLabel("Health check:"); item != nil {
		state.healthCheck = item.(*tview.Checkbox).IsChecked()
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

	// Determine protocol index (0=fins, 1=eip)
	protocolIndex := 0
	if cfg.IsOmronEIP() {
		protocolIndex = 1
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
			protocol:    protocolIndex,
			pollRateMs:  pollRateMs,
			autoConnect: cfg.Enabled,
			healthCheck: cfg.IsHealthCheckEnabled(),
		},
		originalName: cfg.Name,
		tags:         cfg.Tags,
	}

	t.buildEditForm(state)
}

func (t *PLCsTab) buildEditForm(state *editFormState) {
	const pageName = "edit"

	// Remove existing form if present
	t.app.pages.RemovePage(pageName)

	form := tview.NewForm()
	ApplyFormTheme(form)
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
	case config.FamilyLogix, config.FamilyMicro800:
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
	case config.FamilyOmron:
		form.AddDropDown("Protocol:", omronProtocolOptions, state.protocol, nil)
		// Slot field only needed for FINS (EIP doesn't use slot)
		if state.protocol == 0 {
			form.AddInputField("Slot:", state.slot, 5, func(text string, lastChar rune) bool {
				_, err := strconv.Atoi(text)
				return err == nil || text == ""
			}, nil)
		}
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
	form.AddCheckbox("Health check:", state.healthCheck, nil)

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

		healthCheck := state.healthCheck
		protocol := ""
		if family == config.FamilyOmron {
			protocol = omronProtocolOptions[state.protocol]
		}
		updated := config.PLCConfig{
			Name:               state.name,
			Address:            state.address,
			Slot:               byte(slot),
			Family:             family,
			Protocol:           protocol,
			Enabled:            state.autoConnect,
			HealthCheckEnabled: &healthCheck,
			PollRate:           pollRate,
			Tags:               state.tags,
			AmsNetId:           state.amsNetId,
			AmsPort:            uint16(amsPort),
		}

		t.app.config.UpdatePLC(state.originalName, updated)
		t.app.SaveConfig()

		// Close dialog first
		t.app.closeModal(pageName)
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
		t.app.closeModal(pageName)
	})

	// Calculate form height based on number of fields
	formHeight := 19 // Base height for common fields + poll rate + health check + buttons
	if family == config.FamilyBeckhoff {
		formHeight = 21 // Extra fields for Beckhoff
	}

	t.app.showFormModal(pageName, form, 55, formHeight, func() {
		t.app.closeModal(pageName)
	})

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
	if item := form.GetFormItemByLabel("Health check:"); item != nil {
		state.healthCheck = item.(*tview.Checkbox).IsChecked()
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
	const pageName = "info"

	name := t.getSelectedPLCName()
	if name == "" {
		return
	}

	plc := t.app.manager.GetPLC(name)
	if plc == nil {
		return
	}
	deviceInfo := plc.GetDeviceInfo()

	// Build info text (themed)
	th := CurrentTheme
	info := th.Label("Name", plc.Config.Name) + "\n"
	info += th.Label("Address", plc.Config.Address) + "\n"
	info += fmt.Sprintf("%sSlot:%s %d\n", th.TagAccent, th.TagReset, plc.Config.Slot)
	info += th.Label("Status", plc.GetStatus().String()) + "\n"
	info += th.Label("Mode", plc.GetConnectionMode()) + "\n"

	if err := plc.GetError(); err != nil {
		info += fmt.Sprintf("%sError:%s %s\n", th.TagAccent, th.TagError, err.Error())
	}

	if deviceInfo != nil {
		info += fmt.Sprintf("\n%s── Device Info ──%s\n", th.TagPrimary, th.TagReset)
		info += th.Label("Model", deviceInfo.Model) + "\n"
		info += th.Label("Vendor", deviceInfo.Vendor) + "\n"
		info += th.Label("Version", deviceInfo.Version) + "\n"
		if deviceInfo.SerialNumber != "" {
			info += th.Label("Serial", deviceInfo.SerialNumber) + "\n"
		}
		if deviceInfo.Description != "" {
			info += th.Label("Type", deviceInfo.Description) + "\n"
		}
	} else {
		info += "\n" + th.Dim("Connect to view device info")
	}

	// Show tag count if connected
	tags := plc.GetTags()
	programs := plc.GetPrograms()
	if len(tags) > 0 || len(programs) > 0 {
		info += fmt.Sprintf("\n%sPrograms:%s %d\n", th.TagAccent, th.TagReset, len(programs))
		info += fmt.Sprintf("%sTags:%s %d\n", th.TagAccent, th.TagReset, len(tags))
	}

	textView := tview.NewTextView().
		SetDynamicColors(true).
		SetText(info)
	textView.SetBorder(true).SetTitle(" PLC Info ")

	textView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape || event.Key() == tcell.KeyEnter || event.Rune() == 'i' {
			t.app.closeModal(pageName)
			return nil
		}
		return event
	})

	t.app.showCenteredModal(pageName, textView, 55, 20)
}
