package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"warlogix/config"
	"warlogix/trigger"
)

// TriggersTab handles the Triggers configuration tab.
type TriggersTab struct {
	app       *App
	flex      *tview.Flex
	table     *tview.Table
	dataTable *tview.Table
	info      *tview.TextView
	statusBar *tview.TextView

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
	// Button bar
	buttons := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter).
		SetText(" [yellow]a[white]dd  [yellow]e[white]dit  [yellow]r[white]emove  [yellow]t[white] add tag  [yellow]x[white] remove tag  [yellow]s[white]tart  [yellow]S[white]top  [yellow]T[white]est  [gray]│[white]  [yellow]?[white] help ")

	// Triggers table
	t.table = tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0)

	t.table.SetInputCapture(t.handleKeys)
	t.table.SetSelectionChangedFunc(t.onSelectionChanged)

	// Set up headers
	headers := []string{"", "Name", "PLC", "Trigger", "Condition", "Fires", "Status"}
	for i, h := range headers {
		t.table.SetCell(0, i, tview.NewTableCell(h).
			SetTextColor(tcell.ColorYellow).
			SetSelectable(false).
			SetAttributes(tcell.AttrBold))
	}

	tableFrame := tview.NewFrame(t.table).SetBorders(1, 0, 0, 0, 1, 1)
	tableFrame.SetBorder(true).SetTitle(" Event Triggers ")

	// Data tags table for selected trigger
	t.dataTable = tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false)
	t.dataTable.SetBorder(true).SetTitle(" Data Tags ")
	t.dataTable.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Rune() {
		case 'x':
			t.removeSelectedDataTag()
			return nil
		case 't':
			t.showAddDataTagDialog()
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
		SetScrollable(true)
	t.info.SetBorder(true).SetTitle(" Trigger Details ")

	// Right panel with data tags and info
	rightPanel := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(t.dataTable, 0, 1, false).
		AddItem(t.info, 10, 0, false)

	// Content area
	content := tview.NewFlex().
		AddItem(tableFrame, 0, 2, true).
		AddItem(rightPanel, 0, 1, false)

	// Status bar
	t.statusBar = tview.NewTextView().
		SetDynamicColors(true)

	// Main layout
	t.flex = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(buttons, 1, 0, false).
		AddItem(content, 0, 1, true).
		AddItem(t.statusBar, 1, 0, false)
}

func (t *TriggersTab) handleKeys(event *tcell.EventKey) *tcell.EventKey {
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
	case 't':
		t.showAddDataTagDialog()
		return nil
	case 'x':
		t.removeSelectedDataTag()
		return nil
	case 's':
		t.startSelected()
		return nil
	case 'S':
		t.stopSelected()
		return nil
	case 'T':
		t.testSelected()
		return nil
	}
	if event.Key() == tcell.KeyTab {
		if t.dataTable.GetRowCount() > 0 {
			t.app.app.SetFocus(t.dataTable)
			return nil
		}
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

	for i, tag := range cfg.Tags {
		t.dataTable.SetCell(i, 0, tview.NewTableCell(tag).SetExpansion(1))
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

	info := fmt.Sprintf("[yellow]Trigger:[white] %s %v\n", cfg.Condition.Operator, cfg.Condition.Value)
	if cfg.AckTag != "" {
		info += fmt.Sprintf("[yellow]Ack:[white] %s (1=ok, -1=err)\n", cfg.AckTag)
	}
	info += fmt.Sprintf("[yellow]Kafka:[white] %s\n", cfg.KafkaCluster)
	info += fmt.Sprintf("[yellow]Topic:[white] %s\n", cfg.Topic)

	// Get runtime status
	status, err, count, lastFire := t.app.triggerMgr.GetTriggerStatus(name)
	info += fmt.Sprintf("\n[yellow]Status:[white] %s  [yellow]Fires:[white] %d\n", status.String(), count)
	if !lastFire.IsZero() {
		info += fmt.Sprintf("[yellow]Last:[white] %s\n", lastFire.Format("15:04:05"))
	}
	if err != nil {
		info += fmt.Sprintf("[red]Error: %s[-]\n", err.Error())
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

		// Status indicator
		indicator := StatusIndicatorDisconnected
		switch status {
		case trigger.StatusArmed:
			indicator = StatusIndicatorConnected
		case trigger.StatusFiring:
			indicator = StatusIndicatorConnecting
		case trigger.StatusCooldown:
			indicator = "[blue]●[-]"
		case trigger.StatusError:
			indicator = StatusIndicatorError
		}

		// Condition string
		condStr := fmt.Sprintf("%s %v", cfg.Condition.Operator, cfg.Condition.Value)

		t.table.SetCell(row, 0, tview.NewTableCell(indicator).SetExpansion(0))
		t.table.SetCell(row, 1, tview.NewTableCell(cfg.Name).SetExpansion(1))
		t.table.SetCell(row, 2, tview.NewTableCell(cfg.PLC).SetExpansion(1))
		t.table.SetCell(row, 3, tview.NewTableCell(cfg.TriggerTag).SetExpansion(1))
		t.table.SetCell(row, 4, tview.NewTableCell(condStr).SetExpansion(1))
		t.table.SetCell(row, 5, tview.NewTableCell(fmt.Sprintf("%d", count)).SetExpansion(0))
		t.table.SetCell(row, 6, tview.NewTableCell(status.String()).SetExpansion(1))
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

// showTagPicker shows a filterable tag picker dialog and calls onSelect with the chosen tag.
func (t *TriggersTab) showTagPicker(title string, plcName string, onSelect func(tagName string)) {
	const pageName = "tag-picker"

	// Get PLC and its tags
	plc := t.app.manager.GetPLC(plcName)
	if plc == nil {
		t.app.showError("Error", "PLC not found or not connected")
		return
	}

	tags := plc.GetTags()
	if len(tags) == 0 {
		t.app.showError("Error", "No tags available. Connect to PLC first.")
		return
	}

	// Create filter input
	filter := tview.NewInputField().
		SetLabel("Filter: ").
		SetFieldWidth(30)

	// Create list of tags
	list := tview.NewList().
		SetHighlightFullLine(true)

	// Populate list
	populateList := func(filterText string) {
		list.Clear()
		filterLower := strings.ToLower(filterText)
		for _, tag := range tags {
			if filterText == "" || strings.Contains(strings.ToLower(tag.Name), filterLower) {
				tagName := tag.Name
				list.AddItem(tagName, "", 0, func() {
					t.app.closeModal(pageName)
					onSelect(tagName)
				})
			}
		}
	}
	populateList("")

	filter.SetChangedFunc(func(text string) {
		populateList(text)
	})

	filter.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyDown, tcell.KeyTab:
			t.app.app.SetFocus(list)
			return nil
		case tcell.KeyEscape:
			t.app.closeModal(pageName)
			return nil
		case tcell.KeyEnter:
			// Select first item if any
			if list.GetItemCount() > 0 {
				list.SetCurrentItem(0)
				t.app.app.SetFocus(list)
			}
			return nil
		}
		return event
	})

	list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			t.app.closeModal(pageName)
			return nil
		case tcell.KeyUp:
			if list.GetCurrentItem() == 0 {
				t.app.app.SetFocus(filter)
				return nil
			}
		}
		if event.Rune() == '/' {
			t.app.app.SetFocus(filter)
			return nil
		}
		return event
	})

	// Layout
	content := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(filter, 1, 0, true).
		AddItem(list, 0, 1, false)
	content.SetBorder(true).SetTitle(" " + title + " (/ to filter, Enter to select) ")

	t.app.showCenteredModal(pageName, content, 60, 20)
	t.app.app.SetFocus(filter)
}

// getRepublishedTags returns the list of tag names enabled for republishing on a PLC.
func (t *TriggersTab) getRepublishedTags(plcName string) []string {
	plcCfg := t.app.config.FindPLC(plcName)
	if plcCfg == nil {
		return nil
	}
	var tags []string
	for _, tag := range plcCfg.Tags {
		if tag.Enabled {
			tags = append(tags, tag.Name)
		}
	}
	return tags
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

	// Get list of Kafka clusters (optional)
	kafkaNames := []string{"(None)"}
	for _, k := range t.app.config.Kafka {
		kafkaNames = append(kafkaNames, k.Name)
	}

	form := tview.NewForm()
	form.SetBorder(true).SetTitle(" Add Trigger ")

	// Get initial tags for first PLC
	selectedPLC := plcNames[0]
	tagOptions := t.getRepublishedTags(selectedPLC)
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
		newTags := t.getRepublishedTags(selectedPLC)
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
	form.AddInputField("Topic:", "", 30, nil, nil)

	form.AddButton("Add", func() {
		name := form.GetFormItemByLabel("Name:").(*tview.InputField).GetText()
		plcIdx, _ := form.GetFormItemByLabel("PLC:").(*tview.DropDown).GetCurrentOption()
		triggerTagIdx, triggerTag := triggerTagDropdown.GetCurrentOption()
		opIdx, _ := form.GetFormItemByLabel("Operator:").(*tview.DropDown).GetCurrentOption()
		valueStr := form.GetFormItemByLabel("Value:").(*tview.InputField).GetText()
		ackTagIdx, ackTag := ackTagDropdown.GetCurrentOption()
		kafkaIdx, _ := form.GetFormItemByLabel("Kafka:").(*tview.DropDown).GetCurrentOption()
		topic := form.GetFormItemByLabel("Topic:").(*tview.InputField).GetText()

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

		// Get Kafka cluster name (empty if "(None)" selected)
		kafkaCluster := ""
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
			Tags:         []string{}, // Data tags added separately
			KafkaCluster: kafkaCluster,
			Topic:        topic,
			Metadata:     make(map[string]string),
		}

		t.app.config.AddTrigger(cfg)
		t.app.SaveConfig()

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

	// Get lists
	plcNames := make([]string, 0)
	plcIdx := 0
	for i, plc := range t.app.config.PLCs {
		plcNames = append(plcNames, plc.Name)
		if plc.Name == cfg.PLC {
			plcIdx = i
		}
	}

	kafkaNames := []string{"(None)"}
	kafkaIdx := 0 // Default to "(None)"
	for i, k := range t.app.config.Kafka {
		kafkaNames = append(kafkaNames, k.Name)
		if k.Name == cfg.KafkaCluster {
			kafkaIdx = i + 1 // +1 because "(None)" is at index 0
		}
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
	tagOptions := t.getRepublishedTags(selectedPLC)
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
	form.SetBorder(true).SetTitle(" Edit Trigger ")

	form.AddInputField("Name:", cfg.Name, 30, nil, nil)

	// Declare dropdowns for updating when PLC changes
	var triggerTagDropdown, ackTagDropdown *tview.DropDown

	form.AddDropDown("PLC:", plcNames, plcIdx, func(option string, index int) {
		selectedPLC = option
		newTags := t.getRepublishedTags(selectedPLC)
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

	form.AddDropDown("Kafka:", kafkaNames, kafkaIdx, nil)
	form.AddInputField("Topic:", cfg.Topic, 30, nil, nil)

	originalName := cfg.Name

	form.AddButton("Save", func() {
		newName := form.GetFormItemByLabel("Name:").(*tview.InputField).GetText()
		newPlcIdx, _ := form.GetFormItemByLabel("PLC:").(*tview.DropDown).GetCurrentOption()
		triggerTagIdx, triggerTag := triggerTagDropdown.GetCurrentOption()
		newOpIdx, _ := form.GetFormItemByLabel("Operator:").(*tview.DropDown).GetCurrentOption()
		valueStr := form.GetFormItemByLabel("Value:").(*tview.InputField).GetText()
		ackTagIdx, ackTag := ackTagDropdown.GetCurrentOption()
		newKafkaIdx, _ := form.GetFormItemByLabel("Kafka:").(*tview.DropDown).GetCurrentOption()
		topic := form.GetFormItemByLabel("Topic:").(*tview.InputField).GetText()

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

		// Get Kafka cluster name (empty if "(None)" selected)
		kafkaCluster := ""
		if newKafkaIdx > 0 {
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
			Tags:         cfg.Tags, // Keep existing data tags
			KafkaCluster: kafkaCluster,
			Topic:        topic,
			Metadata:     cfg.Metadata,
		}

		t.app.config.UpdateTrigger(originalName, updated)
		t.app.SaveConfig()

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

	t.app.showFormModal(pageName, form, 65, 24, func() {
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

	t.showTagPicker("Add Data Tag to "+name, cfg.PLC, func(tag string) {
		// Check for duplicate
		for _, existing := range cfg.Tags {
			if existing == tag {
				t.app.setStatus("Tag already in list: " + tag)
				return
			}
		}

		// Add tag to config
		cfg.Tags = append(cfg.Tags, tag)
		t.app.SaveConfig()

		// Update trigger manager
		t.app.triggerMgr.UpdateTrigger(cfg)

		t.updateDataTagsList()
		t.app.setStatus(fmt.Sprintf("Added data tag: %s", tag))
	})
}

func (t *TriggersTab) removeSelectedDataTag() {
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

	// Remove from config
	cfg.Tags = append(cfg.Tags[:idx], cfg.Tags[idx+1:]...)
	t.app.SaveConfig()

	// Update trigger manager
	t.app.triggerMgr.UpdateTrigger(cfg)

	t.updateDataTagsList()
	t.app.setStatus(fmt.Sprintf("Removed data tag: %s", tagName))
}

func (t *TriggersTab) removeSelected() {
	name := t.getSelectedName()
	if name == "" {
		return
	}

	t.app.showConfirm("Remove Trigger", fmt.Sprintf("Remove %s?", name), func() {
		t.app.triggerMgr.StopTrigger(name)
		t.app.triggerMgr.RemoveTrigger(name)
		t.app.config.RemoveTrigger(name)
		t.app.SaveConfig()
		t.Refresh()
		t.app.setStatus(fmt.Sprintf("Removed trigger: %s", name))
	})
}

func (t *TriggersTab) startSelected() {
	name := t.getSelectedName()
	if name == "" {
		return
	}

	cfg := t.app.config.FindTrigger(name)
	if cfg != nil {
		cfg.Enabled = true
		t.app.SaveConfig()
	}

	if err := t.app.triggerMgr.StartTrigger(name); err != nil {
		t.app.setStatus(fmt.Sprintf("Failed to start: %v", err))
		return
	}

	t.Refresh()
	t.app.setStatus(fmt.Sprintf("Started trigger: %s", name))
}

func (t *TriggersTab) stopSelected() {
	name := t.getSelectedName()
	if name == "" {
		return
	}

	cfg := t.app.config.FindTrigger(name)
	if cfg != nil {
		cfg.Enabled = false
		t.app.SaveConfig()
	}

	if err := t.app.triggerMgr.StopTrigger(name); err != nil {
		t.app.setStatus(fmt.Sprintf("Failed to stop: %v", err))
		return
	}

	t.Refresh()
	t.app.setStatus(fmt.Sprintf("Stopped trigger: %s", name))
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
