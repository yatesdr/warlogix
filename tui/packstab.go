package tui

import (
	"fmt"
	"sort"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"warlink/config"
	"warlink/kafka"
)

// PacksTab handles the Tag Packs configuration tab.
type PacksTab struct {
	app         *App
	flex        *tview.Flex
	packTable   *tview.Table
	packFrame   *tview.Frame
	memberTable *tview.Table
	info        *tview.TextView
	statusBar   *tview.TextView
	buttonBar   *tview.TextView

	selectedPack string
}

// NewPacksTab creates a new Packs tab.
func NewPacksTab(app *App) *PacksTab {
	t := &PacksTab{app: app}
	t.setupUI()
	t.Refresh()
	return t
}

func (t *PacksTab) setupUI() {
	// Button bar
	t.buttonBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
	t.updateButtonBar()

	// Packs table
	t.packTable = tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0)
	ApplyTableTheme(t.packTable)

	t.packTable.SetInputCapture(t.handleKeys)
	t.packTable.SetSelectionChangedFunc(t.onSelectionChanged)

	// Set up headers
	headers := []string{"", "Name", "Members", "MQTT", "Kafka", "Valkey"}
	for i, h := range headers {
		t.packTable.SetCell(0, i, tview.NewTableCell(h).
			SetTextColor(CurrentTheme.Accent).
			SetSelectable(false).
			SetAttributes(tcell.AttrBold))
	}

	t.packFrame = tview.NewFrame(t.packTable).SetBorders(1, 0, 0, 0, 1, 1)
	t.packFrame.SetBorder(true).SetTitle(" Tag Packs ").SetBorderColor(CurrentTheme.Border).SetTitleColor(CurrentTheme.Accent)

	// Member table
	t.memberTable = tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0)
	ApplyTableTheme(t.memberTable)
	t.memberTable.SetBorder(true).SetTitle(" Members ").SetBorderColor(CurrentTheme.Border).SetTitleColor(CurrentTheme.Accent)

	// Set up member headers
	memberHeaders := []string{"PLC", "Tag", ""}
	for i, h := range memberHeaders {
		t.memberTable.SetCell(0, i, tview.NewTableCell(h).
			SetTextColor(CurrentTheme.Accent).
			SetSelectable(false).
			SetAttributes(tcell.AttrBold))
	}

	t.memberTable.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Rune() {
		case 'a':
			t.showAddTagDialog()
			return nil
		case 'x':
			t.removeSelectedMember()
			return nil
		case 'i':
			t.toggleMemberIgnore()
			return nil
		}
		if event.Key() == tcell.KeyTab || event.Key() == tcell.KeyBacktab {
			t.app.app.SetFocus(t.packTable)
			return nil
		}
		return event
	})

	// Info panel
	t.info = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetTextColor(CurrentTheme.Text)
	t.info.SetBorder(true).SetTitle(" Pack Details ").SetBorderColor(CurrentTheme.Border).SetTitleColor(CurrentTheme.Accent)

	// Right panel
	rightPanel := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(t.memberTable, 0, 1, false).
		AddItem(t.info, 8, 0, false)

	// Content area
	content := tview.NewFlex().
		AddItem(t.packFrame, 0, 2, true).
		AddItem(rightPanel, 0, 1, false)

	// Status bar
	t.statusBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextColor(CurrentTheme.Text)

	// Main layout
	t.flex = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(t.buttonBar, 1, 0, false).
		AddItem(content, 0, 1, true).
		AddItem(t.statusBar, 1, 0, false)
}

func (t *PacksTab) handleKeys(event *tcell.EventKey) *tcell.EventKey {
	switch event.Rune() {
	case 'a':
		t.showCreateDialog()
		return nil
	case 'x':
		t.removeSelected()
		return nil
	case ' ':
		t.toggleEnabled()
		return nil
	case 'e':
		t.showEditDialog()
		return nil
	}
	if event.Key() == tcell.KeyTab {
		t.app.app.SetFocus(t.memberTable)
		if t.memberTable.GetRowCount() > 1 {
			t.memberTable.Select(1, 0)
		}
		return nil
	}
	return event
}

func (t *PacksTab) getSelectedName() string {
	row, _ := t.packTable.GetSelection()
	if row <= 0 {
		return ""
	}
	cell := t.packTable.GetCell(row, 1)
	if cell == nil {
		return ""
	}
	return cell.Text
}

func (t *PacksTab) onSelectionChanged(row, col int) {
	name := t.getSelectedName()
	if name == "" {
		return
	}
	t.selectedPack = name
	t.updateMemberList()
	t.updateInfo(name)
}

func (t *PacksTab) updateMemberList() {
	// Clear existing rows (keep header)
	for t.memberTable.GetRowCount() > 1 {
		t.memberTable.RemoveRow(1)
	}

	cfg := t.app.config.FindTagPack(t.selectedPack)
	if cfg == nil {
		return
	}

	th := CurrentTheme
	for i, member := range cfg.Members {
		row := i + 1

		// Get PLC config for tag lookup
		var plcTags []config.TagSelection
		if plcCfg := t.app.config.FindPLC(member.PLC); plcCfg != nil {
			plcTags = plcCfg.Tags
		}

		// Use shared helper to format tag display
		tagInfo := FormatTagDisplay(member.Tag, plcTags)

		// Show "I" indicator if this member is ignored for change detection
		ignoreStr := ""
		if member.IgnoreChanges {
			ignoreStr = th.TagError + "I" + th.TagReset
		}

		// Build display text with alias if present
		tagDisplay := member.Tag
		if tagInfo.Alias != "" {
			tagDisplay = tagInfo.Alias + " (" + member.Tag + ")"
		}

		// Create cells - use SetAttributes for strikethrough and dim color for terminals without strikethrough support
		plcCell := tview.NewTableCell(member.PLC).SetExpansion(1)
		tagCell := tview.NewTableCell(tagDisplay).SetExpansion(1)
		if !tagInfo.IsEnabled {
			plcCell.SetTextColor(th.TextDim).SetAttributes(tcell.AttrStrikeThrough)
			tagCell.SetTextColor(th.TextDim).SetAttributes(tcell.AttrStrikeThrough)
		}

		t.memberTable.SetCell(row, 0, plcCell)
		t.memberTable.SetCell(row, 1, tagCell)
		t.memberTable.SetCell(row, 2, tview.NewTableCell(ignoreStr).SetExpansion(0))
	}
}

func (t *PacksTab) updateInfo(name string) {
	cfg := t.app.config.FindTagPack(name)
	if cfg == nil {
		t.info.SetText("")
		return
	}

	th := CurrentTheme
	info := th.Label("Name", cfg.Name) + "\n"
	info += th.Label("Enabled", fmt.Sprintf("%v", cfg.Enabled)) + "\n\n"

	// Check service connectivity
	mqttConnected := false
	for _, pub := range t.app.mqttMgr.List() {
		if pub.IsRunning() {
			mqttConnected = true
			break
		}
	}
	valkeyConnected := false
	for _, pub := range t.app.valkeyMgr.List() {
		if pub.IsRunning() {
			valkeyConnected = true
			break
		}
	}
	kafkaConnected := false
	for _, cfgK := range t.app.config.Kafka {
		if producer := t.app.kafkaMgr.GetProducer(cfgK.Name); producer != nil {
			if producer.GetStatus() == kafka.StatusConnected {
				kafkaConnected = true
				break
			}
		}
	}

	// Service indicators: green=active, red=enabled but not connected, gray=disabled (fixed colors)
	var mqttIndicator, kafkaIndicator, valkeyIndicator string
	if !cfg.MQTTEnabled {
		mqttIndicator = "[gray]●[-]"
	} else if mqttConnected {
		mqttIndicator = "[green]●[-]"
	} else {
		mqttIndicator = "[red]●[-]"
	}

	if !cfg.KafkaEnabled {
		kafkaIndicator = "[gray]●[-]"
	} else if kafkaConnected {
		kafkaIndicator = "[green]●[-]"
	} else {
		kafkaIndicator = "[red]●[-]"
	}

	if !cfg.ValkeyEnabled {
		valkeyIndicator = "[gray]●[-]"
	} else if valkeyConnected {
		valkeyIndicator = "[green]●[-]"
	} else {
		valkeyIndicator = "[red]●[-]"
	}

	info += fmt.Sprintf("MQTT: %s  Kafka: %s  Valkey: %s", mqttIndicator, kafkaIndicator, valkeyIndicator)

	t.info.SetText(info)
}

// GetPrimitive returns the main primitive for this tab.
func (t *PacksTab) GetPrimitive() tview.Primitive {
	return t.flex
}

// GetFocusable returns the element that should receive focus.
func (t *PacksTab) GetFocusable() tview.Primitive {
	return t.packTable
}

// Refresh updates the display.
func (t *PacksTab) Refresh() {
	// Clear existing rows (keep header)
	for t.packTable.GetRowCount() > 1 {
		t.packTable.RemoveRow(1)
	}

	// Build sorted index into config slice (avoids copying structs)
	indices := make([]int, len(t.app.config.TagPacks))
	for i := range indices {
		indices[i] = i
	}
	sort.Slice(indices, func(i, j int) bool {
		return t.app.config.TagPacks[indices[i]].Name < t.app.config.TagPacks[indices[j]].Name
	})

	// Check service connectivity once for all packs
	mqttConnected := false
	for _, pub := range t.app.mqttMgr.List() {
		if pub.IsRunning() {
			mqttConnected = true
			break
		}
	}
	valkeyConnected := false
	for _, pub := range t.app.valkeyMgr.List() {
		if pub.IsRunning() {
			valkeyConnected = true
			break
		}
	}
	kafkaConnected := false
	for _, cfg := range t.app.config.Kafka {
		if producer := t.app.kafkaMgr.GetProducer(cfg.Name); producer != nil {
			if producer.GetStatus() == kafka.StatusConnected {
				kafkaConnected = true
				break
			}
		}
	}

	// Add packs to table
	for i, idx := range indices {
		cfg := &t.app.config.TagPacks[idx]
		row := i + 1

		// Status indicator - use fixed colors (theme-independent)
		indicatorCell := tview.NewTableCell(GetStatusBullet()).SetExpansion(0)
		if cfg.Enabled {
			indicatorCell.SetTextColor(IndicatorGreen)
		} else {
			indicatorCell.SetTextColor(IndicatorGray)
		}

		// Service indicators: green=active, red=enabled but not connected, gray=disabled
		mqttCell := tview.NewTableCell(GetStatusBullet()).SetExpansion(0)
		if !cfg.MQTTEnabled {
			mqttCell.SetTextColor(IndicatorGray)
		} else if mqttConnected {
			mqttCell.SetTextColor(IndicatorGreen)
		} else {
			mqttCell.SetTextColor(IndicatorRed)
		}

		kafkaCell := tview.NewTableCell(GetStatusBullet()).SetExpansion(0)
		if !cfg.KafkaEnabled {
			kafkaCell.SetTextColor(IndicatorGray)
		} else if kafkaConnected {
			kafkaCell.SetTextColor(IndicatorGreen)
		} else {
			kafkaCell.SetTextColor(IndicatorRed)
		}

		valkeyCell := tview.NewTableCell(GetStatusBullet()).SetExpansion(0)
		if !cfg.ValkeyEnabled {
			valkeyCell.SetTextColor(IndicatorGray)
		} else if valkeyConnected {
			valkeyCell.SetTextColor(IndicatorGreen)
		} else {
			valkeyCell.SetTextColor(IndicatorRed)
		}

		t.packTable.SetCell(row, 0, indicatorCell)
		t.packTable.SetCell(row, 1, tview.NewTableCell(cfg.Name).SetExpansion(1))
		t.packTable.SetCell(row, 2, tview.NewTableCell(fmt.Sprintf("%d", len(cfg.Members))).SetExpansion(0))
		t.packTable.SetCell(row, 3, mqttCell)
		t.packTable.SetCell(row, 4, kafkaCell)
		t.packTable.SetCell(row, 5, valkeyCell)
	}

	// Update status bar
	enabled := 0
	for i := range t.app.config.TagPacks {
		if t.app.config.TagPacks[i].Enabled {
			enabled++
		}
	}
	t.statusBar.SetText(fmt.Sprintf(" %d packs, %d enabled | Tab to switch to members, 'i' to toggle ignore", len(t.app.config.TagPacks), enabled))

	// Update member list and info for selected
	if name := t.getSelectedName(); name != "" {
		t.selectedPack = name
		t.updateMemberList()
		t.updateInfo(name)
	}
}

func (t *PacksTab) showCreateDialog() {
	const pageName = "create-pack"

	form := tview.NewForm()
	ApplyFormTheme(form)
	form.SetBorder(true).SetTitle(" Create Tag Pack ")

	form.AddInputField("Name:", "", 30, nil, nil)
	form.AddCheckbox("MQTT:", true, nil)
	form.AddCheckbox("Kafka:", true, nil)
	form.AddCheckbox("Valkey:", true, nil)

	form.AddButton("Create", func() {
		name := form.GetFormItemByLabel("Name:").(*tview.InputField).GetText()
		mqttEnabled := form.GetFormItemByLabel("MQTT:").(*tview.Checkbox).IsChecked()
		kafkaEnabled := form.GetFormItemByLabel("Kafka:").(*tview.Checkbox).IsChecked()
		valkeyEnabled := form.GetFormItemByLabel("Valkey:").(*tview.Checkbox).IsChecked()

		if name == "" {
			t.app.showError("Error", "Name is required")
			return
		}

		if t.app.config.FindTagPack(name) != nil {
			t.app.showError("Error", "A pack with this name already exists")
			return
		}

		cfg := config.TagPackConfig{
			Name:          name,
			Enabled:       true,
			MQTTEnabled:   mqttEnabled,
			KafkaEnabled:  kafkaEnabled,
			ValkeyEnabled: valkeyEnabled,
			Members:       []config.TagPackMember{},
		}

		t.app.config.AddTagPack(cfg)
		t.app.SaveConfig()

		// Reload pack manager
		if t.app.packMgr != nil {
			t.app.packMgr.Reload()
		}

		t.app.closeModal(pageName)
		t.Refresh()
		t.app.setStatus(fmt.Sprintf("Created pack: %s - use 'a' to add tags", name))
	})

	form.AddButton("Cancel", func() {
		t.app.closeModal(pageName)
	})

	t.app.showFormModal(pageName, form, 50, 16, func() {
		t.app.closeModal(pageName)
	})
}

func (t *PacksTab) showAddTagDialog() {
	name := t.getSelectedName()
	if name == "" {
		t.app.setStatus("Select a pack first")
		return
	}

	cfg := t.app.config.FindTagPack(name)
	if cfg == nil {
		return
	}

	// Build exclusion list
	var excluded []PLCTag
	for _, member := range cfg.Members {
		excluded = append(excluded, PLCTag{PLC: member.PLC, Tag: member.Tag})
	}

	t.app.ShowTagPicker("Add Tag to "+name, excluded, func(plc, tag string) {
		// Check for duplicate (should be filtered by picker, but double-check)
		for _, existing := range cfg.Members {
			if existing.PLC == plc && existing.Tag == tag {
				t.app.setStatus("Tag already in pack")
				return
			}
		}

		// Add member (changes trigger publish by default, IgnoreChanges=false)
		cfg.Members = append(cfg.Members, config.TagPackMember{
			PLC:           plc,
			Tag:           tag,
			IgnoreChanges: false,
		})
		t.app.SaveConfig()

		if t.app.packMgr != nil {
			t.app.packMgr.Reload()
		}

		t.updateMemberList()
		t.Refresh()
		t.app.setStatus(fmt.Sprintf("Added %s:%s to pack", plc, tag))
		t.app.app.SetFocus(t.memberTable)
	})
}

func (t *PacksTab) removeSelectedMember() {
	name := t.getSelectedName()
	if name == "" {
		return
	}

	cfg := t.app.config.FindTagPack(name)
	if cfg == nil || len(cfg.Members) == 0 {
		return
	}

	row, _ := t.memberTable.GetSelection()
	idx := row - 1 // Account for header
	if idx < 0 || idx >= len(cfg.Members) {
		return
	}

	member := cfg.Members[idx]

	t.app.showConfirm("Remove Tag", fmt.Sprintf("Remove %s:%s from pack?", member.PLC, member.Tag), func() {
		// Remove member
		cfg.Members = append(cfg.Members[:idx], cfg.Members[idx+1:]...)
		t.app.SaveConfig()

		if t.app.packMgr != nil {
			t.app.packMgr.Reload()
		}

		t.updateMemberList()
		t.Refresh()
		t.app.setStatus(fmt.Sprintf("Removed %s:%s from pack", member.PLC, member.Tag))
	})
}

func (t *PacksTab) toggleMemberIgnore() {
	name := t.getSelectedName()
	if name == "" {
		return
	}

	cfg := t.app.config.FindTagPack(name)
	if cfg == nil || len(cfg.Members) == 0 {
		return
	}

	row, _ := t.memberTable.GetSelection()
	idx := row - 1
	if idx < 0 || idx >= len(cfg.Members) {
		return
	}

	cfg.Members[idx].IgnoreChanges = !cfg.Members[idx].IgnoreChanges
	t.app.SaveConfig()

	if t.app.packMgr != nil {
		t.app.packMgr.Reload()
	}

	t.updateMemberList()
	if cfg.Members[idx].IgnoreChanges {
		t.app.setStatus(fmt.Sprintf("Changes to %s:%s will be ignored", cfg.Members[idx].PLC, cfg.Members[idx].Tag))
	} else {
		t.app.setStatus(fmt.Sprintf("Changes to %s:%s will trigger publish", cfg.Members[idx].PLC, cfg.Members[idx].Tag))
	}
}

func (t *PacksTab) removeSelected() {
	name := t.getSelectedName()
	if name == "" {
		return
	}

	t.app.showConfirm("Remove Pack", fmt.Sprintf("Remove pack '%s'?", name), func() {
		t.app.config.RemoveTagPack(name)
		t.app.SaveConfig()

		if t.app.packMgr != nil {
			t.app.packMgr.Reload()
		}

		t.Refresh()
		t.app.setStatus(fmt.Sprintf("Removed pack: %s", name))
	})
}

func (t *PacksTab) toggleEnabled() {
	name := t.getSelectedName()
	if name == "" {
		return
	}

	cfg := t.app.config.FindTagPack(name)
	if cfg == nil {
		return
	}

	wasEnabled := cfg.Enabled
	cfg.Enabled = !cfg.Enabled
	t.app.SaveConfig()

	if t.app.packMgr != nil {
		t.app.packMgr.Reload()

		// If pack was just enabled, publish immediately for testing/validation
		if cfg.Enabled && !wasEnabled {
			t.app.packMgr.PublishPackImmediate(name)
		}
	}

	t.Refresh()
	status := "disabled"
	if cfg.Enabled {
		status = "enabled (published)"
	}
	t.app.setStatus(fmt.Sprintf("Pack %s %s", name, status))
}

func (t *PacksTab) showEditDialog() {
	const pageName = "edit-pack"

	name := t.getSelectedName()
	if name == "" {
		return
	}

	cfg := t.app.config.FindTagPack(name)
	if cfg == nil {
		return
	}

	form := tview.NewForm()
	ApplyFormTheme(form)
	form.SetBorder(true).SetTitle(" Edit Tag Pack ")

	form.AddInputField("Name:", cfg.Name, 30, nil, nil)
	form.AddCheckbox("MQTT:", cfg.MQTTEnabled, nil)
	form.AddCheckbox("Kafka:", cfg.KafkaEnabled, nil)
	form.AddCheckbox("Valkey:", cfg.ValkeyEnabled, nil)

	form.AddButton("Save", func() {
		newName := form.GetFormItemByLabel("Name:").(*tview.InputField).GetText()
		mqttEnabled := form.GetFormItemByLabel("MQTT:").(*tview.Checkbox).IsChecked()
		kafkaEnabled := form.GetFormItemByLabel("Kafka:").(*tview.Checkbox).IsChecked()
		valkeyEnabled := form.GetFormItemByLabel("Valkey:").(*tview.Checkbox).IsChecked()

		if newName == "" {
			t.app.showError("Error", "Name is required")
			return
		}

		if newName != name && t.app.config.FindTagPack(newName) != nil {
			t.app.showError("Error", "A pack with this name already exists")
			return
		}

		cfg.Name = newName
		cfg.MQTTEnabled = mqttEnabled
		cfg.KafkaEnabled = kafkaEnabled
		cfg.ValkeyEnabled = valkeyEnabled

		t.app.SaveConfig()

		if t.app.packMgr != nil {
			t.app.packMgr.Reload()
		}

		t.app.closeModal(pageName)
		t.Refresh()
		t.app.setStatus(fmt.Sprintf("Updated pack: %s", newName))
	})

	form.AddButton("Cancel", func() {
		t.app.closeModal(pageName)
	})

	t.app.showFormModal(pageName, form, 50, 16, func() {
		t.app.closeModal(pageName)
	})
}

func (t *PacksTab) updateButtonBar() {
	th := CurrentTheme
	// Pack table keys | Member table keys | help
	buttonText := " " +
		th.TagHotkey + "a" + th.TagActionText + "dd  " +
		th.TagHotkey + "x" + th.TagActionText + " remove  " +
		th.TagHotkey + "Space" + th.TagActionText + " enable  " +
		th.TagHotkey + "e" + th.TagActionText + "dit  " +
		th.TagHotkey + "i" + th.TagActionText + "gnore  " +
		th.TagActionText + "│  " +
		th.TagHotkey + "?" + th.TagActionText + " help " + th.TagReset
	t.buttonBar.SetText(buttonText)
}

// RefreshTheme updates theme-dependent UI elements.
func (t *PacksTab) RefreshTheme() {
	t.updateButtonBar()
	th := CurrentTheme
	t.packFrame.SetBorderColor(th.Border).SetTitleColor(th.Accent)
	t.memberTable.SetBorderColor(th.Border).SetTitleColor(th.Accent)
	t.info.SetBorderColor(th.Border).SetTitleColor(th.Accent)
	t.info.SetTextColor(th.Text)
	t.statusBar.SetTextColor(th.Text)
	ApplyTableTheme(t.packTable)
	ApplyTableTheme(t.memberTable)
	// Update header colors
	for i := 0; i < t.packTable.GetColumnCount(); i++ {
		if cell := t.packTable.GetCell(0, i); cell != nil {
			cell.SetTextColor(th.Accent)
		}
	}
	for i := 0; i < t.memberTable.GetColumnCount(); i++ {
		if cell := t.memberTable.GetCell(0, i); cell != nil {
			cell.SetTextColor(th.Accent)
		}
	}
}
