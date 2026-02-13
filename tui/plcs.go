package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"warlink/config"
	"warlink/driver"
	"warlink/logging"
	"warlink/plcman"
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
		indicatorCell := tview.NewTableCell(GetStatusBullet()).SetExpansion(0)
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

		// Product name (escape brackets to avoid tview style tag interpretation)
		productName := ""
		if info := plc.GetDeviceInfo(); info != nil {
			productName = escapeTviewText(info.Model)
		}

		t.table.SetCell(row, 0, indicatorCell)
		t.table.SetCell(row, 1, tview.NewTableCell(escapeTviewText(plc.Config.Name)).SetExpansion(1))
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

// escapeTviewText removes characters that tview interprets as style tags
func escapeTviewText(s string) string {
	// Remove square brackets entirely - tview interprets them as style tags
	s = strings.ReplaceAll(s, "[", "")
	s = strings.ReplaceAll(s, "]", "")
	// Also remove any control characters
	var result strings.Builder
	for _, r := range s {
		if r >= 32 && r < 127 {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// buildDetailsString creates a summary of extra device info for display
func buildDetailsString(dev driver.DiscoveredDevice) string {
	var parts []string

	// Protocol-specific important details first
	switch dev.Family {
	case config.FamilyBeckhoff:
		// AMS Net ID is critical for Beckhoff
		if amsNetId := dev.Extra["amsNetId"]; amsNetId != "" {
			parts = append(parts, "AMS:"+amsNetId)
		}
		// Route status is important
		if hasRoute := dev.Extra["hasRoute"]; hasRoute == "false" {
			parts = append(parts, "NO ROUTE")
		}
		// Hostname if available
		if hostname := dev.Extra["hostname"]; hostname != "" {
			parts = append(parts, hostname)
		}
		// TwinCAT version
		if tcVer := dev.Extra["tcVersion"]; tcVer != "" {
			parts = append(parts, "TC"+tcVer)
		}
	case config.FamilyS7:
		// Rack/Slot is important for S7
		rack := dev.Extra["rack"]
		slot := dev.Extra["slot"]
		if rack != "" || slot != "" {
			parts = append(parts, fmt.Sprintf("Rack:%s Slot:%s", rack, slot))
		}
	case config.FamilyLogix, config.FamilyMicro800:
		// Serial and revision for Allen-Bradley
		if serial := dev.Extra["serial"]; serial != "" && serial != "0" {
			parts = append(parts, "SN:"+serial)
		}
		if rev := dev.Extra["revision"]; rev != "" {
			parts = append(parts, "Rev:"+rev)
		}
	case config.FamilyOmron:
		// Node for FINS
		if node := dev.Extra["node"]; node != "" {
			parts = append(parts, "Node:"+node)
		}
		// Serial and revision if available (for EIP)
		if serial := dev.Extra["serial"]; serial != "" && serial != "0" {
			parts = append(parts, "SN:"+serial)
		}
		if rev := dev.Extra["revision"]; rev != "" {
			parts = append(parts, "Rev:"+rev)
		}
	}

	result := strings.Join(parts, ", ")
	return escapeTviewText(result)
}

// discoveredDevicesCache holds cached discovered devices
var discoveredDevicesCache []driver.DiscoveredDevice
var discoveredDevicesCacheMu sync.Mutex

// discoveryInProgress tracks whether discovery is currently running
var discoveryInProgress bool
var discoveryInProgressMu sync.Mutex

func (t *PLCsTab) discover() {
	// Don't start new discovery if modal is already open
	if t.app.isModalOpen() {
		t.app.setStatus("Close current dialog first")
		return
	}

	// Get all local subnets for port scanning (S7, ADS, FINS)
	subnets := driver.GetLocalSubnets()

	t.app.setStatus("Scanning network (EIP, S7, ADS, FINS)...")

	// Mark discovery as in progress
	discoveryInProgressMu.Lock()
	discoveryInProgress = true
	discoveryInProgressMu.Unlock()

	// Show modal immediately
	t.showDiscoveryModal()

	// Run discovery in background, updating cache as devices are found
	go func() {
		defer func() {
			discoveryInProgressMu.Lock()
			discoveryInProgress = false
			discoveryInProgressMu.Unlock()
		}()

		logging.DebugLog("tui", "Discovery: found %d local subnets: %v", len(subnets), subnets)

		// Scan all subnets in parallel
		var wg sync.WaitGroup

		addToCache := func(devices []driver.DiscoveredDevice) {
			discoveredDevicesCacheMu.Lock()
			for _, dev := range devices {
				key := dev.Key()
				found := false
				for _, cached := range discoveredDevicesCache {
					if cached.Key() == key {
						found = true
						break
					}
				}
				if !found {
					discoveredDevicesCache = append(discoveredDevicesCache, dev)
				}
			}
			discoveredDevicesCacheMu.Unlock()
		}

		for _, cidr := range subnets {
			wg.Add(1)
			go func(cidr string) {
				defer wg.Done()
				logging.DebugLog("tui", "Discovery: scanning subnet %s", cidr)
				devices := driver.DiscoverAll("255.255.255.255", cidr, 500*time.Millisecond, 50)
				logging.DebugLog("tui", "Discovery: subnet %s returned %d devices", cidr, len(devices))
				addToCache(devices)
			}(cidr)
		}

		// If no subnets found, still do EIP broadcast
		if len(subnets) == 0 {
			logging.DebugLog("tui", "Discovery: no subnets, doing EIP-only")
			devices := driver.DiscoverEIPOnly("255.255.255.255", 3*time.Second)
			addToCache(devices)
		}

		wg.Wait()

		discoveredDevicesCacheMu.Lock()
		count := len(discoveredDevicesCache)
		discoveredDevicesCacheMu.Unlock()

		logging.DebugLog("tui", "Discovery: complete, total %d devices", count)

		t.app.QueueUpdateDraw(func() {
			t.app.setStatus(fmt.Sprintf("Discovery complete - %d device(s) found", count))
		})
	}()
}

func (t *PLCsTab) showDiscoveryModal() {
	const pageName = "discovery"

	DebugLog("Discovery modal: showDiscoveryModal called")

	th := CurrentTheme

	// Create the main container
	flex := tview.NewFlex().SetDirection(tview.FlexRow)
	flex.SetBorder(true)
	flex.SetBorderColor(th.Border).SetTitleColor(th.Accent)
	flex.SetBackgroundColor(th.Background)
	flex.SetTitle(" Discovering Devices... ")

	// Filter input (hidden by default)
	filterInput := tview.NewInputField()
	filterInput.SetLabel(" Filter: ")
	filterInput.SetFieldWidth(40)
	filterInput.SetLabelColor(th.Text)
	filterInput.SetFieldBackgroundColor(th.FieldBackground)
	filterInput.SetFieldTextColor(th.FieldText)
	filterInput.SetBackgroundColor(th.Background)

	// Track filter visibility and current filter
	filterVisible := false
	currentFilter := ""

	// Create the table
	table := tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0)
	ApplyTableTheme(table)

	// Set up headers
	headers := []string{"IP Address", "Driver", "Protocol", "Device Identifier", "Details"}
	for i, h := range headers {
		table.SetCell(0, i, tview.NewTableCell(h).
			SetTextColor(th.Accent).
			SetSelectable(false).
			SetAttributes(tcell.AttrBold))
	}

	// Track filtered indices to map selection back to original devices
	var filteredIndices []int

	// Function to populate table based on filter
	populateTable := func() {
		// Clear existing rows (keep header)
		for table.GetRowCount() > 1 {
			table.RemoveRow(1)
		}
		filteredIndices = nil

		filter := strings.ToLower(currentFilter)

		// Get current cached devices
		discoveredDevicesCacheMu.Lock()
		devices := make([]driver.DiscoveredDevice, len(discoveredDevicesCache))
		copy(devices, discoveredDevicesCache)
		discoveredDevicesCacheMu.Unlock()

		DebugLog("Discovery modal: populateTable called with %d devices", len(devices))

		for i, dev := range devices {
			ip := escapeTviewText(dev.IP.String())
			driverName := escapeTviewText(string(dev.Family))
			protocol := escapeTviewText(dev.Protocol)
			deviceId := escapeTviewText(dev.ProductName)
			details := buildDetailsString(dev)

			DebugLog("Discovery modal: row %d - raw: IP=%q Protocol=%q Family=%q ProductName=%q",
				i, dev.IP.String(), dev.Protocol, dev.Family, dev.ProductName)
			DebugLog("Discovery modal: row %d - escaped: ip=%q driver=%q protocol=%q deviceId=%q details=%q",
				i, ip, driverName, protocol, deviceId, details)

			// Apply filter
			if filter != "" {
				searchText := strings.ToLower(ip + driverName + protocol + deviceId + dev.Vendor + details)
				if !strings.Contains(searchText, filter) {
					continue
				}
			}

			filteredIndices = append(filteredIndices, i)
			row := len(filteredIndices) // 1-indexed because of header

			table.SetCell(row, 0, tview.NewTableCell(ip).SetExpansion(1))
			table.SetCell(row, 1, tview.NewTableCell(driverName).SetExpansion(1))
			table.SetCell(row, 2, tview.NewTableCell(protocol).SetExpansion(1))
			table.SetCell(row, 3, tview.NewTableCell(deviceId).SetExpansion(2))
			table.SetCell(row, 4, tview.NewTableCell(details).SetExpansion(2))
		}

		// Update title with count and scanning status
		total := len(devices)
		discoveryInProgressMu.Lock()
		scanning := discoveryInProgress
		discoveryInProgressMu.Unlock()

		if filter != "" {
			if scanning {
				flex.SetTitle(fmt.Sprintf(" Scanning... (%d/%d) ", len(filteredIndices), total))
			} else {
				flex.SetTitle(fmt.Sprintf(" Discovered Devices (%d/%d) ", len(filteredIndices), total))
			}
		} else if scanning {
			if total == 0 {
				flex.SetTitle(" Scanning... ")
			} else {
				flex.SetTitle(fmt.Sprintf(" Scanning... (%d found) ", total))
			}
		} else {
			flex.SetTitle(fmt.Sprintf(" Discovered Devices (%d) ", total))
		}
	}

	// Initial population from cache
	populateTable()

	// Set up periodic refresh while discovery is running
	stopRefresh := make(chan struct{})
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopRefresh:
				return
			case <-ticker.C:
				discoveryInProgressMu.Lock()
				scanning := discoveryInProgress
				discoveryInProgressMu.Unlock()

				t.app.QueueUpdateDraw(func() {
					populateTable()
				})

				// Stop refreshing once discovery is complete
				if !scanning {
					return
				}
			}
		}
	}()

	// Filter input change handler
	filterInput.SetChangedFunc(func(text string) {
		currentFilter = text
		populateTable()
	})

	// Rescan button
	rescanBtn := tview.NewButton("Rescan")
	ApplyButtonTheme(rescanBtn)

	// Close button
	closeBtn := tview.NewButton("Close")
	ApplyButtonTheme(closeBtn)

	// Clear cache button
	clearBtn := tview.NewButton("Clear")
	ApplyButtonTheme(clearBtn)

	closeModal := func() {
		close(stopRefresh)
		t.app.closeModal(pageName)
	}

	closeBtn.SetSelectedFunc(closeModal)

	clearBtn.SetSelectedFunc(func() {
		discoveredDevicesCacheMu.Lock()
		discoveredDevicesCache = nil
		discoveredDevicesCacheMu.Unlock()
		populateTable()
		t.app.setStatus("Discovery cache cleared")
	})

	// Rescan function - starts a new scan without closing modal
	startRescan := func() {
		// Check if already scanning
		discoveryInProgressMu.Lock()
		alreadyScanning := discoveryInProgress
		discoveryInProgressMu.Unlock()
		if alreadyScanning {
			t.app.setStatus("Scan already in progress...")
			return
		}

		// Clear cache and start new scan
		discoveredDevicesCacheMu.Lock()
		discoveredDevicesCache = nil
		discoveredDevicesCacheMu.Unlock()
		populateTable()

		// Mark as scanning
		discoveryInProgressMu.Lock()
		discoveryInProgress = true
		discoveryInProgressMu.Unlock()

		t.app.setStatus("Rescanning network...")

		// Run discovery in background
		go func() {
			defer func() {
				discoveryInProgressMu.Lock()
				discoveryInProgress = false
				discoveryInProgressMu.Unlock()
			}()

			subnets := driver.GetLocalSubnets()
			logging.DebugLog("tui", "Rescan: found %d local subnets: %v", len(subnets), subnets)

			var wg sync.WaitGroup
			addToCache := func(devices []driver.DiscoveredDevice) {
				discoveredDevicesCacheMu.Lock()
				for _, dev := range devices {
					key := dev.Key()
					found := false
					for _, cached := range discoveredDevicesCache {
						if cached.Key() == key {
							found = true
							break
						}
					}
					if !found {
						discoveredDevicesCache = append(discoveredDevicesCache, dev)
					}
				}
				discoveredDevicesCacheMu.Unlock()
			}

			for _, cidr := range subnets {
				wg.Add(1)
				go func(cidr string) {
					defer wg.Done()
					devices := driver.DiscoverAll("255.255.255.255", cidr, 500*time.Millisecond, 50)
					addToCache(devices)
				}(cidr)
			}

			if len(subnets) == 0 {
				devices := driver.DiscoverEIPOnly("255.255.255.255", 3*time.Second)
				addToCache(devices)
			}

			wg.Wait()

			discoveredDevicesCacheMu.Lock()
			count := len(discoveredDevicesCache)
			discoveredDevicesCacheMu.Unlock()

			t.app.QueueUpdateDraw(func() {
				t.app.setStatus(fmt.Sprintf("Rescan complete - %d device(s) found", count))
			})
		}()
	}

	rescanBtn.SetSelectedFunc(startRescan)

	// Button container
	buttonFlex := tview.NewFlex().SetDirection(tview.FlexColumn)
	buttonFlex.SetBackgroundColor(th.Background)
	buttonFlex.AddItem(nil, 0, 1, false)
	buttonFlex.AddItem(rescanBtn, 10, 0, false)
	buttonFlex.AddItem(nil, 2, 0, false)
	buttonFlex.AddItem(clearBtn, 9, 0, false)
	buttonFlex.AddItem(nil, 2, 0, false)
	buttonFlex.AddItem(closeBtn, 9, 0, false)
	buttonFlex.AddItem(nil, 0, 1, false)

	// Help text
	helpText := tview.NewTextView()
	helpText.SetText(" /: Filter  r: Rescan  c: Clear  Enter: Add  Esc: Close")
	helpText.SetTextColor(th.TextDim)
	helpText.SetBackgroundColor(th.Background)
	helpText.SetTextAlign(tview.AlignCenter)

	// Build layout
	flex.AddItem(table, 0, 1, true)
	flex.AddItem(helpText, 1, 0, false)
	flex.AddItem(buttonFlex, 1, 0, false)

	// Handle table selection
	table.SetSelectedFunc(func(row, col int) {
		if row <= 0 || row-1 >= len(filteredIndices) {
			return
		}
		originalIndex := filteredIndices[row-1]
		discoveredDevicesCacheMu.Lock()
		if originalIndex < len(discoveredDevicesCache) {
			dev := discoveredDevicesCache[originalIndex]
			discoveredDevicesCacheMu.Unlock()
			closeModal()
			t.showAddDialogWithDevice(&dev)
			return
		}
		discoveredDevicesCacheMu.Unlock()
	})

	// Input capture for the table
	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			if filterVisible {
				// Hide filter and clear it
				filterVisible = false
				filterInput.SetText("")
				currentFilter = ""
				populateTable()
				flex.Clear()
				flex.AddItem(table, 0, 1, true)
				flex.AddItem(helpText, 1, 0, false)
				flex.AddItem(buttonFlex, 1, 0, false)
				t.app.app.SetFocus(table)
				return nil
			}
			closeModal()
			return nil
		case tcell.KeyTab:
			t.app.app.SetFocus(rescanBtn)
			return nil
		case tcell.KeyRune:
			switch event.Rune() {
			case '/':
				if !filterVisible {
					filterVisible = true
					// Insert filter at top
					flex.Clear()
					flex.AddItem(filterInput, 1, 0, true)
					flex.AddItem(table, 0, 1, false)
					flex.AddItem(helpText, 1, 0, false)
					flex.AddItem(buttonFlex, 1, 0, false)
					t.app.app.SetFocus(filterInput)
				}
				return nil
			case 'c', 'C':
				// Clear cache
				discoveredDevicesCacheMu.Lock()
				discoveredDevicesCache = nil
				discoveredDevicesCacheMu.Unlock()
				populateTable()
				t.app.setStatus("Discovery cache cleared")
				return nil
			case 'r', 'R':
				// Rescan
				startRescan()
				return nil
			}
		}
		return event
	})

	// Input capture for the filter
	filterInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			// Hide filter and clear it
			filterVisible = false
			filterInput.SetText("")
			currentFilter = ""
			populateTable()
			flex.Clear()
			flex.AddItem(table, 0, 1, true)
			flex.AddItem(helpText, 1, 0, false)
			flex.AddItem(buttonFlex, 1, 0, false)
			t.app.app.SetFocus(table)
			return nil
		case tcell.KeyEnter, tcell.KeyDown:
			t.app.app.SetFocus(table)
			return nil
		}
		return event
	})

	// Input capture for buttons
	rescanBtn.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			closeModal()
			return nil
		case tcell.KeyTab:
			t.app.app.SetFocus(clearBtn)
			return nil
		case tcell.KeyBacktab:
			t.app.app.SetFocus(table)
			return nil
		}
		return event
	})
	clearBtn.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			closeModal()
			return nil
		case tcell.KeyTab:
			t.app.app.SetFocus(closeBtn)
			return nil
		case tcell.KeyBacktab:
			t.app.app.SetFocus(rescanBtn)
			return nil
		}
		return event
	})
	closeBtn.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			closeModal()
			return nil
		case tcell.KeyTab:
			t.app.app.SetFocus(table)
			return nil
		case tcell.KeyBacktab:
			t.app.app.SetFocus(clearBtn)
			return nil
		}
		return event
	})

	// Show modal (wider to accommodate Details column)
	t.app.showCenteredModal(pageName, flex, 110, 22)
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
	timeoutMs   string // Connection timeout in milliseconds (empty = driver default)
	autoConnect  bool
	discoverTags bool // Auto-discover tags on connect
	healthCheck  bool // Publish health status

	// Omron FINS-specific settings
	finsNode    string // Destination node (typically last octet of PLC IP)
	finsNetwork string // Network number (usually 0)
	finsUnit    string // Unit number (usually 0)
}

var familyOptions = []string{"logix", "micro800", "s7", "beckhoff", "omron"}
var omronProtocolOptions = []string{"fins", "eip"}

func (t *PLCsTab) showAddDialogWithDevice(dev *driver.DiscoveredDevice) {
	state := &plcFormState{
		family:       0,     // Default to logix
		slot:         "0",
		amsPort:      "851",
		pollRateMs:   "1000", // Default 1000ms
		timeoutMs:    "5000", // Default 5000ms
		autoConnect:  true,   // Default to enabled
		discoverTags: true,   // Default to enabled
		healthCheck:  true,   // Default to enabled
	}

	if dev != nil {
		state.address = dev.IP.String()
		state.name = dev.ProductName

		// Set family based on discovered protocol
		switch dev.Family {
		case config.FamilyLogix:
			state.family = 0
		case config.FamilyMicro800:
			state.family = 1
		case config.FamilyS7:
			state.family = 2
			// Set slot from discovery if available
			if slot, ok := dev.Extra["slot"]; ok {
				state.slot = slot
			}
		case config.FamilyBeckhoff:
			state.family = 3
			// Set AMS Net ID from discovery if available
			if amsNetId, ok := dev.Extra["amsNetId"]; ok {
				state.amsNetId = amsNetId
			}
		case config.FamilyOmron:
			state.family = 4
			// Set protocol based on discovery
			if dev.Protocol == "EIP" {
				state.protocol = 1 // eip
			} else {
				state.protocol = 0 // fins
				if node, ok := dev.Extra["node"]; ok {
					state.finsNode = node
				}
			}
		}
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
		form.AddDropDown("Protocol:", omronProtocolOptions, state.protocol, func(option string, index int) {
			if !initialized {
				return
			}
			// Save current values before rebuilding when protocol changes
			t.saveAddFormState(form, state, family)
			state.protocol = index
			// Rebuild form with new protocol
			t.buildAddForm(state)
		})
		// FINS-specific fields (not needed for EIP)
		if state.protocol == 0 {
			form.AddInputField("Node:", state.finsNode, 5, func(text string, lastChar rune) bool {
				_, err := strconv.Atoi(text)
				return err == nil || text == ""
			}, nil)
			form.AddInputField("Network:", state.finsNetwork, 5, func(text string, lastChar rune) bool {
				_, err := strconv.Atoi(text)
				return err == nil || text == ""
			}, nil)
			form.AddInputField("Unit:", state.finsUnit, 5, func(text string, lastChar rune) bool {
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

	// Timeout field (common to all families)
	form.AddInputField("Timeout (ms):", state.timeoutMs, 10, func(text string, lastChar rune) bool {
		if text == "" {
			return true // Allow empty for "driver default"
		}
		_, err := strconv.Atoi(text)
		return err == nil
	}, nil)

	form.AddCheckbox("Auto-connect:", state.autoConnect, nil)

	// Show "Discover tags" checkbox for families that support discovery
	canDiscover := family.SupportsDiscovery() || (family == config.FamilyOmron && state.protocol == 1)
	if canDiscover {
		form.AddCheckbox("Discover tags:", state.discoverTags, nil)
	}

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

		// Parse timeout (0 means use driver default)
		var timeout time.Duration
		if state.timeoutMs != "" {
			timeoutMs, _ := strconv.Atoi(state.timeoutMs)
			if timeoutMs > 0 {
				timeout = time.Duration(timeoutMs) * time.Millisecond
			}
		}

		healthCheck := state.healthCheck
		protocol := ""
		if family == config.FamilyOmron {
			protocol = omronProtocolOptions[state.protocol]
		}

		// Parse Omron FINS settings
		finsNode, _ := strconv.Atoi(state.finsNode)
		finsNetwork, _ := strconv.Atoi(state.finsNetwork)
		finsUnit, _ := strconv.Atoi(state.finsUnit)

		// Only set DiscoverTags explicitly if user disabled it (non-default)
		var discoverTags *bool
		if canDiscover && !state.discoverTags {
			f := false
			discoverTags = &f
		}

		cfg := config.PLCConfig{
			Name:               state.name,
			Address:            state.address,
			Slot:               byte(slot),
			Family:             family,
			Protocol:           protocol,
			Enabled:            state.autoConnect,
			DiscoverTags:       discoverTags,
			HealthCheckEnabled: &healthCheck,
			PollRate:           pollRate,
			Timeout:            timeout,
			AmsNetId:           state.amsNetId,
			AmsPort:            uint16(amsPort),
			// Omron FINS settings
			FinsNode:    byte(finsNode),
			FinsNetwork: byte(finsNetwork),
			FinsUnit:    byte(finsUnit),
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
	formHeight := 21 // Base height for common fields + poll rate + timeout + health check + buttons
	if canDiscover {
		formHeight += 2 // "Discover tags" checkbox
	}
	switch family {
	case config.FamilyBeckhoff:
		formHeight += 2 // Extra fields for Beckhoff (AMS Net ID, AMS Port)
	case config.FamilyOmron:
		if state.protocol == 0 { // FINS protocol
			formHeight += 4 // Extra fields for FINS (Protocol, Node, Network, Unit)
		}
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
	if item := form.GetFormItemByLabel("Timeout (ms):"); item != nil {
		state.timeoutMs = item.(*tview.InputField).GetText()
	}
	if item := form.GetFormItemByLabel("Auto-connect:"); item != nil {
		state.autoConnect = item.(*tview.Checkbox).IsChecked()
	}
	if item := form.GetFormItemByLabel("Discover tags:"); item != nil {
		state.discoverTags = item.(*tview.Checkbox).IsChecked()
	}
	if item := form.GetFormItemByLabel("Health check:"); item != nil {
		state.healthCheck = item.(*tview.Checkbox).IsChecked()
	}
	// Omron FINS fields
	if item := form.GetFormItemByLabel("Node:"); item != nil {
		state.finsNode = item.(*tview.InputField).GetText()
	}
	if item := form.GetFormItemByLabel("Network:"); item != nil {
		state.finsNetwork = item.(*tview.InputField).GetText()
	}
	if item := form.GetFormItemByLabel("Unit:"); item != nil {
		state.finsUnit = item.(*tview.InputField).GetText()
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

	// Convert timeout to milliseconds string (default 5000 if not configured)
	timeoutMs := "5000"
	if cfg.Timeout > 0 {
		timeoutMs = strconv.Itoa(int(cfg.Timeout.Milliseconds()))
	}

	state := &editFormState{
		plcFormState: plcFormState{
			family:       selectedFamily,
			name:         cfg.Name,
			address:      cfg.Address,
			slot:         strconv.Itoa(int(cfg.Slot)),
			amsNetId:     cfg.AmsNetId,
			amsPort:      amsPort,
			protocol:     protocolIndex,
			pollRateMs:   pollRateMs,
			timeoutMs:    timeoutMs,
			autoConnect:  cfg.Enabled,
			discoverTags: cfg.SupportsDiscovery(),
			healthCheck:  cfg.IsHealthCheckEnabled(),
			// Omron FINS settings
			finsNode:    strconv.Itoa(int(cfg.FinsNode)),
			finsNetwork: strconv.Itoa(int(cfg.FinsNetwork)),
			finsUnit:    strconv.Itoa(int(cfg.FinsUnit)),
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
		form.AddDropDown("Protocol:", omronProtocolOptions, state.protocol, func(option string, index int) {
			if !initialized {
				return
			}
			// Save current values before rebuilding when protocol changes
			t.saveEditFormState(form, state, family)
			state.protocol = index
			// Rebuild form with new protocol
			t.buildEditForm(state)
		})
		// FINS-specific fields (not needed for EIP)
		if state.protocol == 0 {
			form.AddInputField("Node:", state.finsNode, 5, func(text string, lastChar rune) bool {
				_, err := strconv.Atoi(text)
				return err == nil || text == ""
			}, nil)
			form.AddInputField("Network:", state.finsNetwork, 5, func(text string, lastChar rune) bool {
				_, err := strconv.Atoi(text)
				return err == nil || text == ""
			}, nil)
			form.AddInputField("Unit:", state.finsUnit, 5, func(text string, lastChar rune) bool {
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

	// Timeout field (common to all families)
	form.AddInputField("Timeout (ms):", state.timeoutMs, 10, func(text string, lastChar rune) bool {
		if text == "" {
			return true // Allow empty for "driver default"
		}
		_, err := strconv.Atoi(text)
		return err == nil
	}, nil)

	form.AddCheckbox("Auto-connect:", state.autoConnect, nil)

	// Show "Discover tags" checkbox for families that support discovery
	canDiscover := family.SupportsDiscovery() || (family == config.FamilyOmron && state.protocol == 1)
	if canDiscover {
		form.AddCheckbox("Discover tags:", state.discoverTags, nil)
	}

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

		// Parse timeout (0 means use driver default)
		var timeout time.Duration
		if state.timeoutMs != "" {
			timeoutMs, _ := strconv.Atoi(state.timeoutMs)
			if timeoutMs > 0 {
				timeout = time.Duration(timeoutMs) * time.Millisecond
			}
		}

		healthCheck := state.healthCheck
		protocol := ""
		if family == config.FamilyOmron {
			protocol = omronProtocolOptions[state.protocol]
		}

		// Parse Omron FINS settings
		finsNode, _ := strconv.Atoi(state.finsNode)
		finsNetwork, _ := strconv.Atoi(state.finsNetwork)
		finsUnit, _ := strconv.Atoi(state.finsUnit)

		// Only set DiscoverTags explicitly if user disabled it (non-default)
		var discoverTags *bool
		if canDiscover && !state.discoverTags {
			f := false
			discoverTags = &f
		}

		updated := config.PLCConfig{
			Name:               state.name,
			Address:            state.address,
			Slot:               byte(slot),
			Family:             family,
			Protocol:           protocol,
			Enabled:            state.autoConnect,
			DiscoverTags:       discoverTags,
			HealthCheckEnabled: &healthCheck,
			PollRate:           pollRate,
			Timeout:            timeout,
			Tags:               state.tags,
			AmsNetId:           state.amsNetId,
			AmsPort:            uint16(amsPort),
			// Omron FINS settings
			FinsNode:    byte(finsNode),
			FinsNetwork: byte(finsNetwork),
			FinsUnit:    byte(finsUnit),
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
	formHeight := 21 // Base height for common fields + poll rate + timeout + health check + buttons
	if canDiscover {
		formHeight += 2 // "Discover tags" checkbox
	}
	switch family {
	case config.FamilyBeckhoff:
		formHeight += 2 // Extra fields for Beckhoff (AMS Net ID, AMS Port)
	case config.FamilyOmron:
		if state.protocol == 0 { // FINS protocol
			formHeight += 4 // Extra fields for FINS (Protocol, Node, Network, Unit)
		}
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
	if item := form.GetFormItemByLabel("Timeout (ms):"); item != nil {
		state.timeoutMs = item.(*tview.InputField).GetText()
	}
	if item := form.GetFormItemByLabel("Auto-connect:"); item != nil {
		state.autoConnect = item.(*tview.Checkbox).IsChecked()
	}
	if item := form.GetFormItemByLabel("Discover tags:"); item != nil {
		state.discoverTags = item.(*tview.Checkbox).IsChecked()
	}
	if item := form.GetFormItemByLabel("Health check:"); item != nil {
		state.healthCheck = item.(*tview.Checkbox).IsChecked()
	}
	// Omron FINS fields
	if item := form.GetFormItemByLabel("Node:"); item != nil {
		state.finsNode = item.(*tview.InputField).GetText()
	}
	if item := form.GetFormItemByLabel("Network:"); item != nil {
		state.finsNetwork = item.(*tview.InputField).GetText()
	}
	if item := form.GetFormItemByLabel("Unit:"); item != nil {
		state.finsUnit = item.(*tview.InputField).GetText()
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

	// Show family-specific settings
	switch plc.Config.Family {
	case config.FamilyOmron:
		info += th.Label("Protocol", plc.Config.GetProtocol()) + "\n"
		if plc.Config.IsOmronFINS() {
			info += fmt.Sprintf("%sNode:%s %d\n", th.TagAccent, th.TagReset, plc.Config.FinsNode)
			info += fmt.Sprintf("%sNetwork:%s %d\n", th.TagAccent, th.TagReset, plc.Config.FinsNetwork)
			info += fmt.Sprintf("%sUnit:%s %d\n", th.TagAccent, th.TagReset, plc.Config.FinsUnit)
		}
	case config.FamilyBeckhoff:
		info += th.Label("AMS Net ID", plc.Config.AmsNetId) + "\n"
		info += fmt.Sprintf("%sAMS Port:%s %d\n", th.TagAccent, th.TagReset, plc.Config.AmsPort)
	default:
		info += fmt.Sprintf("%sSlot:%s %d\n", th.TagAccent, th.TagReset, plc.Config.Slot)
	}

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

	if plc.AllowManualTags() && len(tags) == 0 {
		info += "\n" + th.Dim("No tags -- press 'a' in tag browser to add tags manually")
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
