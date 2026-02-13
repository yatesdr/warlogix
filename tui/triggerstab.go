package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"warlink/config"
	"warlink/trigger"
)

// TriggersTab handles the Triggers configuration tab.
type TriggersTab struct {
	app        *App
	flex       *tview.Flex
	table      *tview.Table
	tableFrame *tview.Frame
	dataTable  *tview.Table
	info       *tview.TextView
	statusBar  *tview.TextView
	buttonBar  *tview.TextView

	selectedTrigger string
}

// NewTriggersTab creates a new Triggers tab.
func NewTriggersTab(app *App) *TriggersTab {
	t := &TriggersTab{app: app}
	t.setupUI()
	t.Refresh()
	return t
}

func (t *TriggersTab) setupUI() {
	// Button bar (themed)
	t.buttonBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
	t.updateButtonBar()

	// Triggers table
	t.table = tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0)
	ApplyTableTheme(t.table)

	t.table.SetInputCapture(t.handleKeys)
	t.table.SetSelectionChangedFunc(t.onSelectionChanged)

	// Set up headers (themed)
	headers := []string{"", "Name", "PLC", "Trigger", "Condition", "Pack", "Fires", "Status"}
	for i, h := range headers {
		t.table.SetCell(0, i, tview.NewTableCell(h).
			SetTextColor(CurrentTheme.Accent).
			SetSelectable(false).
			SetAttributes(tcell.AttrBold))
	}

	t.tableFrame = tview.NewFrame(t.table).SetBorders(1, 0, 0, 0, 1, 1)
	t.tableFrame.SetBorder(true).SetTitle(" Event Triggers ").SetBorderColor(CurrentTheme.Border).SetTitleColor(CurrentTheme.Accent)

	// Data tags table for selected trigger
	t.dataTable = tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false)
	ApplyTableTheme(t.dataTable)
	t.dataTable.SetBorder(true).SetTitle(" Data Tags/Packs ").SetBorderColor(CurrentTheme.Border).SetTitleColor(CurrentTheme.Accent)
	t.dataTable.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Rune() {
		case 'a':
			t.showAddDataTagDialog()
			return nil
		case 'x':
			t.confirmRemoveDataTag()
			return nil
		}
		if event.Key() == tcell.KeyTab || event.Key() == tcell.KeyBacktab {
			t.app.app.SetFocus(t.table)
			return nil
		}
		return event
	})

	// Info panel
	t.info = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetTextColor(CurrentTheme.Text)
	t.info.SetBorder(true).SetTitle(" Trigger Details ").SetBorderColor(CurrentTheme.Border).SetTitleColor(CurrentTheme.Accent)

	// Right panel with data tags and info
	rightPanel := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(t.dataTable, 0, 1, false).
		AddItem(t.info, 10, 0, false)

	// Content area
	content := tview.NewFlex().
		AddItem(t.tableFrame, 0, 2, true).
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

func (t *TriggersTab) handleKeys(event *tcell.EventKey) *tcell.EventKey {
	switch event.Rune() {
	case 'a':
		t.showAddDialog()
		return nil
	case 'x':
		t.confirmRemoveTrigger()
		return nil
	case 'e':
		t.showEditDialog()
		return nil
	case ' ':
		t.toggleSelected()
		return nil
	case 'F':
		t.testSelected()
		return nil
	}
	if event.Key() == tcell.KeyTab {
		t.app.app.SetFocus(t.dataTable)
		return nil
	}
	return event
}

func (t *TriggersTab) getSelectedName() string {
	row, _ := t.table.GetSelection()
	if row <= 0 {
		return ""
	}
	cell := t.table.GetCell(row, 1)
	if cell == nil {
		return ""
	}
	return cell.Text
}

func (t *TriggersTab) onSelectionChanged(row, col int) {
	name := t.getSelectedName()
	if name == "" {
		return
	}
	t.selectedTrigger = name
	t.updateDataTagsList()
	t.updateInfo(name)
}

func (t *TriggersTab) updateDataTagsList() {
	t.dataTable.Clear()

	cfg := t.app.config.FindTrigger(t.selectedTrigger)
	if cfg == nil {
		return
	}

	// Get PLC config for tag lookup
	var plcTags []config.TagSelection
	if plcCfg := t.app.config.FindPLC(cfg.PLC); plcCfg != nil {
		plcTags = plcCfg.Tags
	}

	th := CurrentTheme
	for i, tag := range cfg.Tags {
		var displayName string
		var isEnabled bool

		// Check if it's a pack reference
		if strings.HasPrefix(tag, "pack:") {
			packName := strings.TrimPrefix(tag, "pack:")
			if packCfg := t.app.config.FindTagPack(packName); packCfg != nil {
				isEnabled = packCfg.Enabled
			}
			displayName = packName + " (TagPack)"
		} else {
			// It's a tag - use shared helper
			tagInfo := FormatTagDisplay(tag, plcTags)
			isEnabled = tagInfo.IsEnabled
			displayName = tag
			if tagInfo.Alias != "" {
				displayName = tagInfo.Alias + " (" + tag + ")"
			}
		}

		cell := tview.NewTableCell(displayName).SetExpansion(1)
		if !isEnabled {
			cell.SetTextColor(th.TextDim).SetAttributes(tcell.AttrStrikeThrough)
		}
		t.dataTable.SetCell(i, 0, cell)
	}

	// Restore selection if valid
	if t.dataTable.GetRowCount() > 0 {
		row, _ := t.dataTable.GetSelection()
		if row >= t.dataTable.GetRowCount() {
			t.dataTable.Select(t.dataTable.GetRowCount()-1, 0)
		}
	}
}

func (t *TriggersTab) updateInfo(name string) {
	cfg := t.app.config.FindTrigger(name)
	if cfg == nil {
		t.info.SetText("")
		return
	}

	th := CurrentTheme
	info := fmt.Sprintf("%sTrigger:%s %s %v\n", th.TagAccent, th.TagReset, cfg.Condition.Operator, cfg.Condition.Value)
	if cfg.AckTag != "" {
		info += fmt.Sprintf("%sAck:%s %s (1=ok, -1=err)\n", th.TagAccent, th.TagReset, cfg.AckTag)
	}
	info += th.Label("Kafka", cfg.KafkaCluster) + "\n"
	if cfg.Selector != "" {
		info += th.Label("Selector", cfg.Selector) + "\n"
	}

	// Get runtime status
	status, err, count, lastFire := t.app.triggerMgr.GetTriggerStatus(name)
	info += fmt.Sprintf("\n%sStatus:%s %s  %sFires:%s %d\n", th.TagAccent, th.TagReset, status.String(), th.TagAccent, th.TagReset, count)
	if !lastFire.IsZero() {
		info += fmt.Sprintf("%sLast:%s %s\n", th.TagAccent, th.TagReset, lastFire.Format("15:04:05"))
	}
	if err != nil {
		info += th.ErrorText("Error: "+err.Error()) + "\n"
	}

	t.info.SetText(info)
}

// GetPrimitive returns the main primitive for this tab.
func (t *TriggersTab) GetPrimitive() tview.Primitive {
	return t.flex
}

// GetFocusable returns the element that should receive focus.
func (t *TriggersTab) GetFocusable() tview.Primitive {
	return t.table
}

// Refresh updates the display.
func (t *TriggersTab) Refresh() {
	// Clear existing rows (keep header)
	for t.table.GetRowCount() > 1 {
		t.table.RemoveRow(1)
	}

	// Sort triggers by name
	triggers := make([]config.TriggerConfig, len(t.app.config.Triggers))
	copy(triggers, t.app.config.Triggers)
	sort.Slice(triggers, func(i, j int) bool {
		return triggers[i].Name < triggers[j].Name
	})

	// Add triggers to table
	for i, cfg := range triggers {
		row := i + 1

		// Get runtime status
		status, _, count, _ := t.app.triggerMgr.GetTriggerStatus(cfg.Name)

		// Status indicator - use fixed colors (theme-independent)
		indicatorCell := tview.NewTableCell(GetStatusBullet()).SetExpansion(0)
		switch status {
		case trigger.StatusArmed:
			indicatorCell.SetTextColor(IndicatorGreen)
		case trigger.StatusFiring:
			indicatorCell.SetTextColor(tcell.ColorYellow)
		case trigger.StatusCooldown:
			indicatorCell.SetTextColor(tcell.ColorOrange)
		case trigger.StatusError:
			indicatorCell.SetTextColor(IndicatorRed)
		default:
			indicatorCell.SetTextColor(IndicatorGray)
		}

		// Condition string
		condStr := fmt.Sprintf("%s %v", cfg.Condition.Operator, cfg.Condition.Value)

		// Pack name (or "-" if none)
		packName := cfg.PublishPack
		if packName == "" {
			packName = "-"
		}

		t.table.SetCell(row, 0, indicatorCell)
		t.table.SetCell(row, 1, tview.NewTableCell(cfg.Name).SetExpansion(1))
		t.table.SetCell(row, 2, tview.NewTableCell(cfg.PLC).SetExpansion(1))
		t.table.SetCell(row, 3, tview.NewTableCell(cfg.TriggerTag).SetExpansion(1))
		t.table.SetCell(row, 4, tview.NewTableCell(condStr).SetExpansion(1))
		t.table.SetCell(row, 5, tview.NewTableCell(packName).SetExpansion(1))
		t.table.SetCell(row, 6, tview.NewTableCell(fmt.Sprintf("%d", count)).SetExpansion(0))
		t.table.SetCell(row, 7, tview.NewTableCell(status.String()).SetExpansion(1))
	}

	// Update status bar
	armed := 0
	for _, cfg := range triggers {
		status, _, _, _ := t.app.triggerMgr.GetTriggerStatus(cfg.Name)
		if status == trigger.StatusArmed {
			armed++
		}
	}
	t.statusBar.SetText(fmt.Sprintf(" %d triggers, %d armed | Tab to switch to data tags list", len(triggers), armed))

	// Update data tags list and info for selected
	if name := t.getSelectedName(); name != "" {
		t.selectedTrigger = name
		t.updateDataTagsList()
		t.updateInfo(name)
	}
}

func (t *TriggersTab) showAddDialog() {
	const pageName = "add-trigger"

	// Get list of PLCs
	plcNames := make([]string, 0)
	for _, plc := range t.app.config.PLCs {
		plcNames = append(plcNames, plc.Name)
	}
	if len(plcNames) == 0 {
		t.app.showError("Error", "No PLCs configured. Add a PLC first.")
		return
	}

	// Get list of Kafka clusters: "All" (default), then individual clusters
	kafkaNames := []string{"All"}
	for _, k := range t.app.config.Kafka {
		kafkaNames = append(kafkaNames, k.Name)
	}

	form := tview.NewForm()
	ApplyFormTheme(form)
	form.SetBorder(true).SetTitle(" Add Trigger ")

	// Get initial tags for first PLC
	selectedPLC := plcNames[0]
	tagOptions := t.app.GetEnabledTags(selectedPLC)
	if len(tagOptions) == 0 {
		tagOptions = []string{"(no tags configured)"}
	}
	// Add "(None)" option for optional ack tag
	ackOptions := append([]string{"(None)"}, tagOptions...)

	form.AddInputField("Name:", "", 30, nil, nil)

	// PLC dropdown - update tag dropdowns when changed
	var triggerTagDropdown, ackTagDropdown *tview.DropDown

	form.AddDropDown("PLC:", plcNames, 0, func(option string, index int) {
		selectedPLC = option
		newTags := t.app.GetEnabledTags(selectedPLC)
		if len(newTags) == 0 {
			newTags = []string{"(no tags configured)"}
		}
		newAckOptions := append([]string{"(None)"}, newTags...)

		// Update tag dropdowns
		if triggerTagDropdown != nil {
			triggerTagDropdown.SetOptions(newTags, nil)
			triggerTagDropdown.SetCurrentOption(0)
		}
		if ackTagDropdown != nil {
			ackTagDropdown.SetOptions(newAckOptions, nil)
			ackTagDropdown.SetCurrentOption(0)
		}
	})

	// Trigger tag dropdown
	triggerTagDropdown = tview.NewDropDown().SetLabel("Trigger Tag:").SetOptions(tagOptions, nil)
	triggerTagDropdown.SetCurrentOption(0)
	form.AddFormItem(triggerTagDropdown)

	form.AddDropDown("Operator:", trigger.ValidOperators(), 0, nil)
	form.AddInputField("Value:", "true", 15, nil, nil)

	// Ack tag dropdown (optional) - writes 1 on success, -1 on error
	ackTagDropdown = tview.NewDropDown().SetLabel("Ack Tag (1=ok,-1=err):").SetOptions(ackOptions, nil)
	ackTagDropdown.SetCurrentOption(0)
	form.AddFormItem(ackTagDropdown)

	form.AddDropDown("Kafka:", kafkaNames, 0, nil)
	form.AddInputField("Selector:", "", 30, nil, nil)

	form.AddButton("Add", func() {
		name := form.GetFormItemByLabel("Name:").(*tview.InputField).GetText()
		plcIdx, _ := form.GetFormItemByLabel("PLC:").(*tview.DropDown).GetCurrentOption()
		triggerTagIdx, triggerTag := triggerTagDropdown.GetCurrentOption()
		opIdx, _ := form.GetFormItemByLabel("Operator:").(*tview.DropDown).GetCurrentOption()
		valueStr := form.GetFormItemByLabel("Value:").(*tview.InputField).GetText()
		ackTagIdx, ackTag := ackTagDropdown.GetCurrentOption()
		kafkaIdx, _ := form.GetFormItemByLabel("Kafka:").(*tview.DropDown).GetCurrentOption()
		selector := form.GetFormItemByLabel("Selector:").(*tview.InputField).GetText()

		// Validate trigger tag
		if name == "" || triggerTag == "(no tags configured)" {
			t.app.showError("Error", "Name and trigger tag are required")
			return
		}
		_ = triggerTagIdx // used for validation above

		// Handle "(None)" selection for ack tag
		if ackTagIdx == 0 {
			ackTag = ""
		}

		// Get Kafka cluster name ("all" if "All" selected, otherwise specific name)
		kafkaCluster := "all"
		if kafkaIdx > 0 {
			kafkaCluster = kafkaNames[kafkaIdx]
		}

		// Parse value
		var value interface{} = valueStr
		if valueStr == "true" {
			value = true
		} else if valueStr == "false" {
			value = false
		} else {
			var intVal int
			if _, err := fmt.Sscanf(valueStr, "%d", &intVal); err == nil {
				value = intVal
			} else {
				var floatVal float64
				if _, err := fmt.Sscanf(valueStr, "%f", &floatVal); err == nil {
					value = floatVal
				}
			}
		}

		cfg := config.TriggerConfig{
			Name:       name,
			Enabled:    true,
			PLC:        plcNames[plcIdx],
			TriggerTag: triggerTag,
			Condition: config.TriggerCondition{
				Operator: trigger.ValidOperators()[opIdx],
				Value:    value,
			},
			AckTag:       ackTag,
			DebounceMS:   100,
			Tags:         []string{}, // Data tags/packs added separately
			MQTTBroker:   "all",      // Publish to all MQTT brokers by default
			KafkaCluster: kafkaCluster,
			Selector:     selector,
			Metadata:     make(map[string]string),
		}

		t.app.LockConfig()
		t.app.config.AddTrigger(cfg)
		t.app.UnlockAndSaveConfig()

		if err := t.app.triggerMgr.AddTrigger(&cfg); err != nil {
			t.app.showError("Error", fmt.Sprintf("Failed to add trigger: %v", err))
			return
		}

		t.app.triggerMgr.StartTrigger(name)

		t.app.closeModal(pageName)
		t.Refresh()
		t.app.setStatus(fmt.Sprintf("Added trigger: %s - use 't' to add data tags", name))
	})

	form.AddButton("Cancel", func() {
		t.app.closeModal(pageName)
	})

	t.app.showFormModal(pageName, form, 65, 24, func() {
		t.app.closeModal(pageName)
	})
}

func (t *TriggersTab) showEditDialog() {
	const pageName = "edit-trigger"

	name := t.getSelectedName()
	if name == "" {
		return
	}

	cfg := t.app.config.FindTrigger(name)
	if cfg == nil {
		return
	}

	// Get PLC list
	plcNames := make([]string, 0)
	plcIdx := 0
	for i, plc := range t.app.config.PLCs {
		plcNames = append(plcNames, plc.Name)
		if plc.Name == cfg.PLC {
			plcIdx = i
		}
	}

	// Get MQTT broker list: "All", "None", individual brokers
	mqttNames := []string{"All", "None"}
	mqttIdx := 0 // Default to "All"
	for i, m := range t.app.config.MQTT {
		mqttNames = append(mqttNames, m.Name)
		if m.Name == cfg.MQTTBroker {
			mqttIdx = i + 2
		}
	}
	if cfg.MQTTBroker == "" || strings.EqualFold(cfg.MQTTBroker, "all") {
		mqttIdx = 0
	} else if strings.EqualFold(cfg.MQTTBroker, "none") {
		mqttIdx = 1
	}

	// Get Kafka cluster list: "All", "None", individual clusters
	kafkaNames := []string{"All", "None"}
	kafkaIdx := 0 // Default to "All"
	for i, k := range t.app.config.Kafka {
		kafkaNames = append(kafkaNames, k.Name)
		if k.Name == cfg.KafkaCluster {
			kafkaIdx = i + 2
		}
	}
	if cfg.KafkaCluster == "" || strings.EqualFold(cfg.KafkaCluster, "all") {
		kafkaIdx = 0
	} else if strings.EqualFold(cfg.KafkaCluster, "none") {
		kafkaIdx = 1
	}

	opIdx := 0
	for i, op := range trigger.ValidOperators() {
		if op == cfg.Condition.Operator {
			opIdx = i
			break
		}
	}

	// Get republished tags for current PLC
	selectedPLC := cfg.PLC
	tagOptions := t.app.GetEnabledTags(selectedPLC)
	if len(tagOptions) == 0 {
		tagOptions = []string{"(no tags configured)"}
	}
	ackOptions := append([]string{"(None)"}, tagOptions...)

	// Find current tag indices
	triggerTagIdx := 0
	for i, tag := range tagOptions {
		if tag == cfg.TriggerTag {
			triggerTagIdx = i
			break
		}
	}
	ackTagIdx := 0
	for i, tag := range ackOptions {
		if tag == cfg.AckTag {
			ackTagIdx = i
			break
		}
	}

	form := tview.NewForm()
	ApplyFormTheme(form)
	form.SetBorder(true).SetTitle(" Edit Trigger ")

	form.AddInputField("Name:", cfg.Name, 30, nil, nil)

	// Declare dropdowns for updating when PLC changes
	var triggerTagDropdown, ackTagDropdown *tview.DropDown

	form.AddDropDown("PLC:", plcNames, plcIdx, func(option string, index int) {
		selectedPLC = option
		newTags := t.app.GetEnabledTags(selectedPLC)
		if len(newTags) == 0 {
			newTags = []string{"(no tags configured)"}
		}
		newAckOptions := append([]string{"(None)"}, newTags...)

		if triggerTagDropdown != nil {
			triggerTagDropdown.SetOptions(newTags, nil)
			triggerTagDropdown.SetCurrentOption(0)
		}
		if ackTagDropdown != nil {
			ackTagDropdown.SetOptions(newAckOptions, nil)
			ackTagDropdown.SetCurrentOption(0)
		}
	})

	// Trigger tag dropdown
	triggerTagDropdown = tview.NewDropDown().SetLabel("Trigger Tag:").SetOptions(tagOptions, nil)
	triggerTagDropdown.SetCurrentOption(triggerTagIdx)
	form.AddFormItem(triggerTagDropdown)

	form.AddDropDown("Operator:", trigger.ValidOperators(), opIdx, nil)
	form.AddInputField("Value:", fmt.Sprintf("%v", cfg.Condition.Value), 15, nil, nil)

	// Ack tag dropdown (optional) - writes 1 on success, -1 on error
	ackTagDropdown = tview.NewDropDown().SetLabel("Ack Tag (1=ok,-1=err):").SetOptions(ackOptions, nil)
	ackTagDropdown.SetCurrentOption(ackTagIdx)
	form.AddFormItem(ackTagDropdown)

	// Service configuration
	form.AddDropDown("MQTT:", mqttNames, mqttIdx, nil)
	form.AddDropDown("Kafka:", kafkaNames, kafkaIdx, nil)
	form.AddInputField("Selector:", cfg.Selector, 30, nil, nil)

	originalName := cfg.Name

	form.AddButton("Save", func() {
		newName := form.GetFormItemByLabel("Name:").(*tview.InputField).GetText()
		newPlcIdx, _ := form.GetFormItemByLabel("PLC:").(*tview.DropDown).GetCurrentOption()
		triggerTagIdx, triggerTag := triggerTagDropdown.GetCurrentOption()
		newOpIdx, _ := form.GetFormItemByLabel("Operator:").(*tview.DropDown).GetCurrentOption()
		valueStr := form.GetFormItemByLabel("Value:").(*tview.InputField).GetText()
		ackTagIdx, ackTag := ackTagDropdown.GetCurrentOption()
		newMqttIdx, _ := form.GetFormItemByLabel("MQTT:").(*tview.DropDown).GetCurrentOption()
		newKafkaIdx, _ := form.GetFormItemByLabel("Kafka:").(*tview.DropDown).GetCurrentOption()
		selector := form.GetFormItemByLabel("Selector:").(*tview.InputField).GetText()

		// Validate trigger tag
		if newName == "" || triggerTag == "(no tags configured)" {
			t.app.showError("Error", "Name and trigger tag are required")
			return
		}
		_ = triggerTagIdx // used for validation above

		// Handle "(None)" selection for ack tag
		if ackTagIdx == 0 {
			ackTag = ""
		}

		// Get MQTT broker name
		mqttBroker := "all"
		if newMqttIdx == 1 {
			mqttBroker = "none"
		} else if newMqttIdx > 1 {
			mqttBroker = mqttNames[newMqttIdx]
		}

		// Get Kafka cluster name
		kafkaCluster := "all"
		if newKafkaIdx == 1 {
			kafkaCluster = "none"
		} else if newKafkaIdx > 1 {
			kafkaCluster = kafkaNames[newKafkaIdx]
		}

		// Parse value
		var value interface{} = valueStr
		if valueStr == "true" {
			value = true
		} else if valueStr == "false" {
			value = false
		} else {
			var intVal int
			if _, err := fmt.Sscanf(valueStr, "%d", &intVal); err == nil {
				value = intVal
			} else {
				var floatVal float64
				if _, err := fmt.Sscanf(valueStr, "%f", &floatVal); err == nil {
					value = floatVal
				}
			}
		}

		updated := config.TriggerConfig{
			Name:       newName,
			Enabled:    cfg.Enabled,
			PLC:        plcNames[newPlcIdx],
			TriggerTag: triggerTag,
			Condition: config.TriggerCondition{
				Operator: trigger.ValidOperators()[newOpIdx],
				Value:    value,
			},
			AckTag:       ackTag,
			DebounceMS:   cfg.DebounceMS,
			Tags:         cfg.Tags, // Keep existing data tags/packs
			MQTTBroker:   mqttBroker,
			KafkaCluster: kafkaCluster,
			Selector:     selector,
			Metadata:     cfg.Metadata,
		}

		t.app.LockConfig()
		t.app.config.UpdateTrigger(originalName, updated)
		t.app.UnlockAndSaveConfig()

		t.app.triggerMgr.RemoveTrigger(originalName)
		if err := t.app.triggerMgr.AddTrigger(&updated); err != nil {
			t.app.showError("Error", fmt.Sprintf("Failed to update trigger: %v", err))
			return
		}

		if updated.Enabled {
			t.app.triggerMgr.StartTrigger(newName)
		}

		t.app.closeModal(pageName)
		t.Refresh()
		t.app.setStatus(fmt.Sprintf("Updated trigger: %s", newName))
	})

	form.AddButton("Cancel", func() {
		t.app.closeModal(pageName)
	})

	t.app.showFormModal(pageName, form, 65, 26, func() {
		t.app.closeModal(pageName)
	})
}

func (t *TriggersTab) showAddDataTagDialog() {
	name := t.getSelectedName()
	if name == "" {
		t.app.setStatus("Select a trigger first")
		return
	}

	cfg := t.app.config.FindTrigger(name)
	if cfg == nil {
		return
	}

	// Build exclusion lists from existing tags in trigger
	var excludeTags []PLCTag
	var excludePacks []string
	for _, tag := range cfg.Tags {
		if strings.HasPrefix(tag, "pack:") {
			excludePacks = append(excludePacks, strings.TrimPrefix(tag, "pack:"))
		} else {
			excludeTags = append(excludeTags, PLCTag{PLC: cfg.PLC, Tag: tag})
		}
	}

	t.app.ShowTagPickerWithOptions(TagPickerOptions{
		Title:        "Add Data Tag to " + name,
		PLCFilter:    cfg.PLC,
		IncludePacks: true,
		ExcludeTags:  excludeTags,
		ExcludePacks: excludePacks,
		OnSelectTag: func(plc, tag string) {
			t.app.LockConfig()
			cfg.Tags = append(cfg.Tags, tag)
			t.app.UnlockAndSaveConfig()
			t.app.triggerMgr.UpdateTrigger(cfg)
			t.updateDataTagsList()
			t.app.setStatus(fmt.Sprintf("Added: %s", tag))
			t.app.app.SetFocus(t.dataTable)
		},
		OnSelectPack: func(packName string) {
			t.app.LockConfig()
			cfg.Tags = append(cfg.Tags, "pack:"+packName)
			t.app.UnlockAndSaveConfig()
			t.app.triggerMgr.UpdateTrigger(cfg)
			t.updateDataTagsList()
			t.app.setStatus(fmt.Sprintf("Added pack: %s", packName))
			t.app.app.SetFocus(t.dataTable)
		},
	})
}

func (t *TriggersTab) confirmRemoveDataTag() {
	name := t.getSelectedName()
	if name == "" {
		return
	}

	cfg := t.app.config.FindTrigger(name)
	if cfg == nil || len(cfg.Tags) == 0 {
		return
	}

	idx, _ := t.dataTable.GetSelection()
	if idx < 0 || idx >= len(cfg.Tags) {
		return
	}

	tagName := cfg.Tags[idx]
	displayName := tagName
	if strings.HasPrefix(tagName, "pack:") {
		displayName = "[pack] " + strings.TrimPrefix(tagName, "pack:")
	}

	t.app.showConfirm("Remove Tag", fmt.Sprintf("Remove %s?", displayName), func() {
		// Remove from config
		t.app.LockConfig()
		cfg.Tags = append(cfg.Tags[:idx], cfg.Tags[idx+1:]...)
		t.app.UnlockAndSaveConfig()

		// Update trigger manager
		t.app.triggerMgr.UpdateTrigger(cfg)

		t.updateDataTagsList()
		t.app.setStatus(fmt.Sprintf("Removed: %s", displayName))
	})
}

func (t *TriggersTab) confirmRemoveTrigger() {
	name := t.getSelectedName()
	if name == "" {
		return
	}

	t.app.showConfirm("Remove Trigger", fmt.Sprintf("Remove %s?", name), func() {
		t.app.triggerMgr.StopTrigger(name)
		t.app.triggerMgr.RemoveTrigger(name)
		t.app.LockConfig()
		t.app.config.RemoveTrigger(name)
		t.app.UnlockAndSaveConfig()
		t.Refresh()
		t.app.setStatus(fmt.Sprintf("Removed trigger: %s", name))
	})
}

func (t *TriggersTab) toggleSelected() {
	name := t.getSelectedName()
	if name == "" {
		return
	}

	cfg := t.app.config.FindTrigger(name)
	if cfg == nil {
		return
	}

	if cfg.Enabled {
		// Stop the trigger
		t.app.LockConfig()
		cfg.Enabled = false
		t.app.UnlockAndSaveConfig()
		if err := t.app.triggerMgr.StopTrigger(name); err != nil {
			t.app.setStatus(fmt.Sprintf("Failed to stop: %v", err))
			return
		}
		t.Refresh()
		t.app.setStatus(fmt.Sprintf("Stopped trigger: %s", name))
	} else {
		// Start the trigger
		t.app.LockConfig()
		cfg.Enabled = true
		t.app.UnlockAndSaveConfig()
		if err := t.app.triggerMgr.StartTrigger(name); err != nil {
			t.app.setStatus(fmt.Sprintf("Failed to start: %v", err))
			return
		}
		t.Refresh()
		t.app.setStatus(fmt.Sprintf("Started trigger: %s", name))
	}
}

func (t *TriggersTab) testSelected() {
	name := t.getSelectedName()
	if name == "" {
		return
	}

	t.app.setStatus(fmt.Sprintf("Test firing trigger: %s - check Debug tab for results", name))

	// Run completely in background - fire and forget
	// Results will appear in the Debug tab log
	go func() {
		err := t.app.triggerMgr.TestFireTrigger(name)

		// Schedule UI update for later (don't block on it)
		go func() {
			// Small delay to let the fire complete logging
			time.Sleep(100 * time.Millisecond)
			t.app.QueueUpdateDraw(func() {
				if err != nil {
					t.app.setStatus(fmt.Sprintf("Test fire error: %v", err))
				} else {
					t.app.setStatus(fmt.Sprintf("Test fire complete: %s", name))
				}
				t.Refresh()
			})
		}()
	}()
}

func (t *TriggersTab) updateButtonBar() {
	th := CurrentTheme
	buttonText := " " + th.TagHotkey + "a" + th.TagActionText + "dd  " +
		th.TagHotkey + "x" + th.TagActionText + " remove  " +
		th.TagHotkey + "e" + th.TagActionText + "dit  " +
		th.TagHotkey + "Space" + th.TagActionText + " toggle  " +
		th.TagHotkey + "F" + th.TagActionText + "ire  " +
		th.TagActionText + "│  " +
		th.TagHotkey + "?" + th.TagActionText + " help " + th.TagReset
	t.buttonBar.SetText(buttonText)
}

// RefreshTheme updates theme-dependent UI elements.
func (t *TriggersTab) RefreshTheme() {
	t.updateButtonBar()
	th := CurrentTheme
	t.tableFrame.SetBorderColor(th.Border).SetTitleColor(th.Accent)
	t.dataTable.SetBorderColor(th.Border).SetTitleColor(th.Accent)
	t.info.SetBorderColor(th.Border).SetTitleColor(th.Accent)
	t.info.SetTextColor(th.Text)
	t.statusBar.SetTextColor(th.Text)
	ApplyTableTheme(t.table)
	ApplyTableTheme(t.dataTable)
	// Update header colors
	for i := 0; i < t.table.GetColumnCount(); i++ {
		if cell := t.table.GetCell(0, i); cell != nil {
			cell.SetTextColor(th.Accent)
		}
	}
}
