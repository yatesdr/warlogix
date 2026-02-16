package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"warlink/config"
	"warlink/engine"
	"warlink/rule"
)

// RulesTab handles the Rules configuration tab.
type RulesTab struct {
	app        *App
	flex       *tview.Flex
	table      *tview.Table
	tableFrame *tview.Frame
	condTable  *tview.Table
	info       *tview.TextView
	statusBar  *tview.TextView
	buttonBar  *tview.TextView

	selectedRule string
}

// NewRulesTab creates a new Rules tab.
func NewRulesTab(app *App) *RulesTab {
	t := &RulesTab{app: app}
	t.setupUI()
	t.Refresh()
	return t
}

func (t *RulesTab) setupUI() {
	// Button bar (themed)
	t.buttonBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
	t.updateButtonBar()

	// Rules table
	t.table = tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0)
	ApplyTableTheme(t.table)

	t.table.SetInputCapture(t.handleKeys)
	t.table.SetSelectionChangedFunc(t.onSelectionChanged)

	// Set up headers (themed)
	headers := []string{"", "Name", "Logic", "Conds", "Actions", "Fires", "Status"}
	for i, h := range headers {
		t.table.SetCell(0, i, tview.NewTableCell(h).
			SetTextColor(CurrentTheme.Accent).
			SetSelectable(false).
			SetAttributes(tcell.AttrBold))
	}

	t.tableFrame = tview.NewFrame(t.table).SetBorders(1, 0, 0, 0, 1, 1)
	t.tableFrame.SetBorder(true).SetTitle(" Rules ").SetBorderColor(CurrentTheme.Border).SetTitleColor(CurrentTheme.Accent)

	// Conditions table for selected rule
	t.condTable = tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false)
	ApplyTableTheme(t.condTable)
	t.condTable.SetBorder(true).SetTitle(" Conditions ").SetBorderColor(CurrentTheme.Border).SetTitleColor(CurrentTheme.Accent)
	t.condTable.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyTab || event.Key() == tcell.KeyBacktab {
			t.app.app.SetFocus(t.table)
			t.statusBar.SetText(fmt.Sprintf(" %d rules | Tab to conditions", len(t.app.config.Rules)))
			return nil
		}
		switch event.Rune() {
		case 'x':
			t.confirmRemoveCondition()
			return nil
		case 'a':
			t.showConditionForm(-1)
			return nil
		case 'e':
			row, _ := t.condTable.GetSelection()
			if row > 0 {
				t.showConditionForm(row - 1)
			}
			return nil
		}
		return event
	})

	// Info panel
	t.info = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetTextColor(CurrentTheme.Text)
	t.info.SetBorder(true).SetTitle(" Rule Details ").SetBorderColor(CurrentTheme.Border).SetTitleColor(CurrentTheme.Accent)

	// Right panel with conditions and info
	rightPanel := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(t.condTable, 0, 1, false).
		AddItem(t.info, 12, 0, false)

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

func (t *RulesTab) handleKeys(event *tcell.EventKey) *tcell.EventKey {
	switch event.Rune() {
	case 'a':
		t.showAddDialog()
		return nil
	case 'x':
		t.confirmRemoveRule()
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
		t.app.app.SetFocus(t.condTable)
		t.statusBar.SetText(" Conditions: a add | e edit | x remove | Tab back to rules")
		return nil
	}
	return event
}

func (t *RulesTab) getSelectedName() string {
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

func (t *RulesTab) onSelectionChanged(row, col int) {
	name := t.getSelectedName()
	if name == "" {
		return
	}
	t.selectedRule = name
	t.updateConditionsList()
	t.updateInfo(name)
}

func (t *RulesTab) updateConditionsList() {
	t.condTable.Clear()

	cfg := t.app.config.FindRule(t.selectedRule)
	if cfg == nil {
		return
	}

	// Set headers
	th := CurrentTheme
	condHeaders := []string{"NOT", "PLC", "Tag", "Op", "Value"}
	for i, h := range condHeaders {
		t.condTable.SetCell(0, i, tview.NewTableCell(h).
			SetTextColor(th.Accent).
			SetSelectable(false).
			SetAttributes(tcell.AttrBold))
	}

	for i, cond := range cfg.Conditions {
		row := i + 1
		notStr := ""
		if cond.Not {
			notStr = "NOT"
		}
		t.condTable.SetCell(row, 0, tview.NewTableCell(notStr).SetExpansion(0))
		t.condTable.SetCell(row, 1, tview.NewTableCell(cond.PLC).SetExpansion(1))
		t.condTable.SetCell(row, 2, tview.NewTableCell(cond.Tag).SetExpansion(1))
		t.condTable.SetCell(row, 3, tview.NewTableCell(cond.Operator).SetExpansion(0))
		t.condTable.SetCell(row, 4, tview.NewTableCell(fmt.Sprintf("%v", cond.Value)).SetExpansion(1))
	}
}

func (t *RulesTab) updateInfo(name string) {
	cfg := t.app.config.FindRule(name)
	if cfg == nil {
		t.info.SetText("")
		return
	}

	th := CurrentTheme

	logicMode := string(cfg.LogicMode)
	if logicMode == "" {
		logicMode = "and"
	}
	info := th.Label("Logic", strings.ToUpper(logicMode)) + "\n"

	if cfg.DebounceMS > 0 {
		info += th.Label("Debounce", fmt.Sprintf("%dms", cfg.DebounceMS)) + "\n"
	}
	if cfg.CooldownMS > 0 {
		info += th.Label("Cooldown", fmt.Sprintf("%dms", cfg.CooldownMS)) + "\n"
	}

	// Actions summary
	if len(cfg.Actions) > 0 {
		types := make([]string, 0, len(cfg.Actions))
		for _, a := range cfg.Actions {
			types = append(types, string(a.Type))
		}
		info += th.Label("Actions", fmt.Sprintf("%d: %s", len(cfg.Actions), strings.Join(types, ", "))) + "\n"
	}
	if len(cfg.ClearedActions) > 0 {
		types := make([]string, 0, len(cfg.ClearedActions))
		for _, a := range cfg.ClearedActions {
			types = append(types, string(a.Type))
		}
		info += th.Label("Cleared", fmt.Sprintf("%d: %s", len(cfg.ClearedActions), strings.Join(types, ", "))) + "\n"
	}

	// Get runtime status
	if t.app.ruleMgr != nil {
		status, err, count, lastFire := t.app.ruleMgr.GetRuleStatus(name)
		info += fmt.Sprintf("\n%sStatus:%s %s  %sFires:%s %d\n",
			th.TagAccent, th.TagReset, status.String(),
			th.TagAccent, th.TagReset, count)
		if !lastFire.IsZero() {
			info += fmt.Sprintf("%sLast:%s %s\n", th.TagAccent, th.TagReset, lastFire.Format("15:04:05"))
		}
		if err != nil {
			info += th.ErrorText("Error: "+err.Error()) + "\n"
		}
	}

	t.info.SetText(info)
}

// GetPrimitive returns the main primitive for this tab.
func (t *RulesTab) GetPrimitive() tview.Primitive {
	return t.flex
}

// GetFocusable returns the element that should receive focus.
func (t *RulesTab) GetFocusable() tview.Primitive {
	return t.table
}

// Refresh updates the display.
func (t *RulesTab) Refresh() {
	// Clear existing rows (keep header)
	for t.table.GetRowCount() > 1 {
		t.table.RemoveRow(1)
	}

	// Sort rules by name
	rules := make([]config.RuleConfig, len(t.app.config.Rules))
	copy(rules, t.app.config.Rules)
	sort.Slice(rules, func(i, j int) bool {
		return rules[i].Name < rules[j].Name
	})

	// Add rules to table
	for i, cfg := range rules {
		row := i + 1

		// Get runtime status
		var status rule.Status
		var count int64
		if t.app.ruleMgr != nil {
			status, _, count, _ = t.app.ruleMgr.GetRuleStatus(cfg.Name)
		}

		// Status indicator
		indicatorCell := tview.NewTableCell(GetStatusBullet()).SetExpansion(0)
		switch status {
		case rule.StatusArmed:
			indicatorCell.SetTextColor(IndicatorGreen)
		case rule.StatusFiring:
			indicatorCell.SetTextColor(tcell.ColorYellow)
		case rule.StatusWaitingClear, rule.StatusCooldown:
			indicatorCell.SetTextColor(tcell.ColorOrange)
		case rule.StatusError:
			indicatorCell.SetTextColor(IndicatorRed)
		default:
			indicatorCell.SetTextColor(IndicatorGray)
		}

		// Logic mode
		logicMode := string(cfg.LogicMode)
		if logicMode == "" {
			logicMode = "AND"
		} else {
			logicMode = strings.ToUpper(logicMode)
		}

		// Action types summary
		actionTypes := make([]string, 0, len(cfg.Actions))
		for _, a := range cfg.Actions {
			actionTypes = append(actionTypes, string(a.Type))
		}
		actionStr := strings.Join(actionTypes, ",")
		if actionStr == "" {
			actionStr = "-"
		}

		t.table.SetCell(row, 0, indicatorCell)
		t.table.SetCell(row, 1, tview.NewTableCell(cfg.Name).SetExpansion(1))
		t.table.SetCell(row, 2, tview.NewTableCell(logicMode).SetExpansion(0))
		t.table.SetCell(row, 3, tview.NewTableCell(fmt.Sprintf("%d", len(cfg.Conditions))).SetExpansion(0))
		t.table.SetCell(row, 4, tview.NewTableCell(actionStr).SetExpansion(1))
		t.table.SetCell(row, 5, tview.NewTableCell(fmt.Sprintf("%d", count)).SetExpansion(0))
		t.table.SetCell(row, 6, tview.NewTableCell(status.String()).SetExpansion(1))
	}

	// Update status bar
	armed := 0
	if t.app.ruleMgr != nil {
		for _, cfg := range rules {
			status, _, _, _ := t.app.ruleMgr.GetRuleStatus(cfg.Name)
			if status == rule.StatusArmed {
				armed++
			}
		}
	}
	t.statusBar.SetText(fmt.Sprintf(" %d rules, %d armed | Tab to switch to conditions", len(rules), armed))

	// Update conditions and info for selected
	if name := t.getSelectedName(); name != "" {
		t.selectedRule = name
		t.updateConditionsList()
		t.updateInfo(name)
	}
}

func (t *RulesTab) showAddDialog() {
	const pageName = "add-rule"

	// Get list of PLCs
	plcNames := make([]string, 0)
	for _, plc := range t.app.config.PLCs {
		plcNames = append(plcNames, plc.Name)
	}
	if len(plcNames) == 0 {
		t.app.showError("Error", "No PLCs configured. Add a PLC first.")
		return
	}

	form := tview.NewForm()
	ApplyFormTheme(form)
	form.SetBorder(true).SetTitle(" Add Rule ")

	form.AddInputField("Name:", "", 30, nil, nil)
	form.AddDropDown("Logic:", []string{"AND", "OR"}, 0, nil)

	// First condition
	form.AddDropDown("Cond PLC:", plcNames, 0, nil)

	selectedPLC := plcNames[0]
	tagOptions := t.app.GetEnabledTags(selectedPLC)
	if len(tagOptions) == 0 {
		tagOptions = []string{"(no tags configured)"}
	}

	var condTagDropdown *tview.DropDown
	condTagDropdown = tview.NewDropDown().SetLabel("Cond Tag:").SetOptions(tagOptions, nil)
	condTagDropdown.SetCurrentOption(0)

	form.GetFormItemByLabel("Cond PLC:").(*tview.DropDown).SetSelectedFunc(func(option string, index int) {
		selectedPLC = option
		newTags := t.app.GetEnabledTags(selectedPLC)
		if len(newTags) == 0 {
			newTags = []string{"(no tags)"}
		}
		if condTagDropdown != nil {
			condTagDropdown.SetOptions(newTags, nil)
			condTagDropdown.SetCurrentOption(0)
		}
	})
	form.AddFormItem(condTagDropdown)

	form.AddDropDown("Operator:", rule.ValidOperators(), 0, nil)
	form.AddInputField("Value:", "true", 15, nil, nil)
	form.AddCheckbox("NOT:", false, nil)

	// Action type
	form.AddDropDown("Action:", []string{"publish", "webhook", "writeback"}, 0, nil)
	form.AddInputField("Debounce (ms):", "100", 10, nil, nil)
	form.AddInputField("Cooldown (ms):", "0", 10, nil, nil)

	form.AddButton("Add", func() {
		name := form.GetFormItemByLabel("Name:").(*tview.InputField).GetText()
		logicIdx, _ := form.GetFormItemByLabel("Logic:").(*tview.DropDown).GetCurrentOption()
		plcIdx, _ := form.GetFormItemByLabel("Cond PLC:").(*tview.DropDown).GetCurrentOption()
		_, condTag := condTagDropdown.GetCurrentOption()
		opIdx, _ := form.GetFormItemByLabel("Operator:").(*tview.DropDown).GetCurrentOption()
		valueStr := form.GetFormItemByLabel("Value:").(*tview.InputField).GetText()
		notChecked := form.GetFormItemByLabel("NOT:").(*tview.Checkbox).IsChecked()
		actionIdx, _ := form.GetFormItemByLabel("Action:").(*tview.DropDown).GetCurrentOption()
		debounceStr := form.GetFormItemByLabel("Debounce (ms):").(*tview.InputField).GetText()
		cooldownStr := form.GetFormItemByLabel("Cooldown (ms):").(*tview.InputField).GetText()

		if name == "" || condTag == "(no tags configured)" || condTag == "(no tags)" {
			t.app.showError("Error", "Name and condition tag are required")
			return
		}

		logicModes := []config.RuleLogicMode{config.RuleLogicAND, config.RuleLogicOR}
		actionTypes := []config.RuleActionType{config.ActionPublish, config.ActionWebhook, config.ActionWriteback}

		value := parseValue(valueStr)

		var debounceMS int
		fmt.Sscanf(debounceStr, "%d", &debounceMS)
		var cooldownMS int
		fmt.Sscanf(cooldownStr, "%d", &cooldownMS)

		conditions := []config.RuleCondition{{
			PLC:      plcNames[plcIdx],
			Tag:      condTag,
			Operator: rule.ValidOperators()[opIdx],
			Value:    value,
			Not:      notChecked,
		}}

		actions := []config.RuleAction{{
			Type: actionTypes[actionIdx],
		}}

		if err := t.app.engine.CreateRule(engine.RuleCreateRequest{
			Name:       name,
			Enabled:    true,
			Conditions: conditions,
			LogicMode:  logicModes[logicIdx],
			DebounceMS: debounceMS,
			CooldownMS: cooldownMS,
			Actions:    actions,
		}); err != nil {
			t.app.showError("Error", fmt.Sprintf("Failed to create rule: %v", err))
			return
		}

		t.app.closeModal(pageName)
		t.Refresh()
		t.app.setStatus(fmt.Sprintf("Added rule: %s - use 'e' to configure actions and add more conditions", name))
	})

	form.AddButton("Cancel", func() {
		t.app.closeModal(pageName)
	})

	t.app.showFormModal(pageName, form, 65, 26, func() {
		t.app.closeModal(pageName)
	})
}

func (t *RulesTab) showEditDialog() {
	const pageName = "edit-rule"

	name := t.getSelectedName()
	if name == "" {
		return
	}

	cfg := t.app.config.FindRule(name)
	if cfg == nil {
		return
	}

	logicIdx := 0
	if cfg.LogicMode == config.RuleLogicOR {
		logicIdx = 1
	}

	form := tview.NewForm()
	ApplyFormTheme(form)
	form.SetBorder(true).SetTitle(fmt.Sprintf(" Edit Rule: %s ", name))

	form.AddDropDown("Logic:", []string{"AND", "OR"}, logicIdx, nil)
	form.AddInputField("Debounce (ms):", fmt.Sprintf("%d", cfg.DebounceMS), 10, nil, nil)
	form.AddInputField("Cooldown (ms):", fmt.Sprintf("%d", cfg.CooldownMS), 10, nil, nil)

	form.AddButton("Save", func() {
		newLogicIdx, _ := form.GetFormItemByLabel("Logic:").(*tview.DropDown).GetCurrentOption()
		debounceStr := form.GetFormItemByLabel("Debounce (ms):").(*tview.InputField).GetText()
		cooldownStr := form.GetFormItemByLabel("Cooldown (ms):").(*tview.InputField).GetText()

		logicModes := []config.RuleLogicMode{config.RuleLogicAND, config.RuleLogicOR}

		var debounceMS int
		fmt.Sscanf(debounceStr, "%d", &debounceMS)
		var cooldownMS int
		fmt.Sscanf(cooldownStr, "%d", &cooldownMS)

		if err := t.app.engine.UpdateRule(name, engine.RuleUpdateRequest{
			Enabled:        cfg.Enabled,
			Conditions:     cfg.Conditions,
			LogicMode:      logicModes[newLogicIdx],
			DebounceMS:     debounceMS,
			CooldownMS:     cooldownMS,
			Actions:        cfg.Actions,
			ClearedActions: cfg.ClearedActions,
		}); err != nil {
			t.app.showError("Error", fmt.Sprintf("Failed to update rule: %v", err))
			return
		}

		t.app.closeModal(pageName)
		t.Refresh()
		t.app.setStatus(fmt.Sprintf("Updated rule: %s", name))
	})

	form.AddButton("Cancel", func() {
		t.app.closeModal(pageName)
	})

	t.app.showFormModal(pageName, form, 55, 14, func() {
		t.app.closeModal(pageName)
	})
}

func (t *RulesTab) confirmRemoveRule() {
	name := t.getSelectedName()
	if name == "" {
		return
	}

	t.app.showConfirm("Remove Rule", fmt.Sprintf("Remove %s?", name), func() {
		if err := t.app.engine.DeleteRule(name); err != nil {
			t.app.showError("Error", err.Error())
			return
		}
		t.Refresh()
		t.app.setStatus(fmt.Sprintf("Removed rule: %s", name))
	})
}

func (t *RulesTab) confirmRemoveCondition() {
	if t.selectedRule == "" {
		return
	}
	cfg := t.app.config.FindRule(t.selectedRule)
	if cfg == nil {
		return
	}

	row, _ := t.condTable.GetSelection()
	idx := row - 1
	if idx < 0 || idx >= len(cfg.Conditions) {
		return
	}

	cond := cfg.Conditions[idx]
	label := fmt.Sprintf("%s.%s %s %v", cond.PLC, cond.Tag, cond.Operator, cond.Value)
	if cond.Not {
		label = "NOT " + label
	}

	t.app.showConfirm("Remove Condition", fmt.Sprintf("Remove condition: %s?", label), func() {
		cfg.Conditions = append(cfg.Conditions[:idx], cfg.Conditions[idx+1:]...)
		if err := t.app.engine.UpdateRule(t.selectedRule, engine.RuleUpdateRequest{
			Enabled:        cfg.Enabled,
			Conditions:     cfg.Conditions,
			LogicMode:      cfg.LogicMode,
			DebounceMS:     cfg.DebounceMS,
			CooldownMS:     cfg.CooldownMS,
			Actions:        cfg.Actions,
			ClearedActions: cfg.ClearedActions,
		}); err != nil {
			t.app.showError("Error", fmt.Sprintf("Failed to update rule: %v", err))
			return
		}
		t.Refresh()
		t.app.setStatus(fmt.Sprintf("Removed condition from %s", t.selectedRule))
		t.app.app.SetFocus(t.condTable)
	})
}

// showConditionForm shows a form to add (idx < 0) or edit (idx >= 0) a condition.
func (t *RulesTab) showConditionForm(idx int) {
	if t.selectedRule == "" {
		return
	}
	cfg := t.app.config.FindRule(t.selectedRule)
	if cfg == nil {
		return
	}

	plcNames := make([]string, 0, len(t.app.config.PLCs))
	for _, plc := range t.app.config.PLCs {
		plcNames = append(plcNames, plc.Name)
	}
	if len(plcNames) == 0 {
		t.app.showError("Error", "No PLCs configured.")
		return
	}

	editing := idx >= 0 && idx < len(cfg.Conditions)
	pageName := "cond-form"
	title := " Add Condition "
	if editing {
		title = " Edit Condition "
	}

	// Defaults for add
	plcIdx := 0
	opIdx := 0
	valueStr := "true"
	notVal := false

	if editing {
		cond := cfg.Conditions[idx]
		for i, name := range plcNames {
			if name == cond.PLC {
				plcIdx = i
				break
			}
		}
		ops := rule.ValidOperators()
		for i, op := range ops {
			if op == cond.Operator {
				opIdx = i
				break
			}
		}
		valueStr = fmt.Sprintf("%v", cond.Value)
		notVal = cond.Not
	}

	selectedPLC := plcNames[plcIdx]
	tagOptions := t.app.GetEnabledTags(selectedPLC)
	if len(tagOptions) == 0 {
		tagOptions = []string{"(no tags)"}
	}

	// Find initial tag index for edit mode
	tagIdx := 0
	if editing {
		for i, tag := range tagOptions {
			if tag == cfg.Conditions[idx].Tag {
				tagIdx = i
				break
			}
		}
	}

	form := tview.NewForm()
	ApplyFormTheme(form)
	form.SetBorder(true).SetTitle(title)

	form.AddDropDown("PLC:", plcNames, plcIdx, nil)

	condTagDropdown := tview.NewDropDown().SetLabel("Tag:").SetOptions(tagOptions, nil)
	condTagDropdown.SetCurrentOption(tagIdx)

	form.GetFormItemByLabel("PLC:").(*tview.DropDown).SetSelectedFunc(func(option string, index int) {
		selectedPLC = option
		newTags := t.app.GetEnabledTags(selectedPLC)
		if len(newTags) == 0 {
			newTags = []string{"(no tags)"}
		}
		condTagDropdown.SetOptions(newTags, nil)
		condTagDropdown.SetCurrentOption(0)
	})
	form.AddFormItem(condTagDropdown)

	form.AddDropDown("Operator:", rule.ValidOperators(), opIdx, nil)
	form.AddInputField("Value:", valueStr, 20, nil, nil)
	form.AddCheckbox("NOT:", notVal, nil)

	form.AddButton("Save", func() {
		savePlcIdx, _ := form.GetFormItemByLabel("PLC:").(*tview.DropDown).GetCurrentOption()
		_, condTag := condTagDropdown.GetCurrentOption()
		saveOpIdx, _ := form.GetFormItemByLabel("Operator:").(*tview.DropDown).GetCurrentOption()
		saveValue := form.GetFormItemByLabel("Value:").(*tview.InputField).GetText()
		saveNot := form.GetFormItemByLabel("NOT:").(*tview.Checkbox).IsChecked()

		if condTag == "(no tags)" || condTag == "(no tags configured)" {
			t.app.showErrorWithFocus("Error", "A valid tag is required.", form)
			return
		}

		cond := config.RuleCondition{
			PLC:      plcNames[savePlcIdx],
			Tag:      condTag,
			Operator: rule.ValidOperators()[saveOpIdx],
			Value:    parseValue(saveValue),
			Not:      saveNot,
		}

		if editing {
			cfg.Conditions[idx] = cond
		} else {
			cfg.Conditions = append(cfg.Conditions, cond)
		}

		if err := t.app.engine.UpdateRule(t.selectedRule, engine.RuleUpdateRequest{
			Enabled:        cfg.Enabled,
			Conditions:     cfg.Conditions,
			LogicMode:      cfg.LogicMode,
			DebounceMS:     cfg.DebounceMS,
			CooldownMS:     cfg.CooldownMS,
			Actions:        cfg.Actions,
			ClearedActions: cfg.ClearedActions,
		}); err != nil {
			t.app.showError("Error", fmt.Sprintf("Failed to update rule: %v", err))
			return
		}

		t.app.closeModal(pageName)
		t.Refresh()
		verb := "Added"
		if editing {
			verb = "Updated"
		}
		t.app.setStatus(fmt.Sprintf("%s condition on %s: %s.%s %s %v", verb, t.selectedRule, cond.PLC, cond.Tag, cond.Operator, cond.Value))
		t.app.app.SetFocus(t.condTable)
	})

	form.AddButton("Cancel", func() {
		t.app.closeModal(pageName)
		t.app.app.SetFocus(t.condTable)
	})

	t.app.showFormModal(pageName, form, 60, 18, func() {
		t.app.closeModal(pageName)
		t.app.app.SetFocus(t.condTable)
	})
}

func (t *RulesTab) toggleSelected() {
	name := t.getSelectedName()
	if name == "" {
		return
	}

	cfg := t.app.config.FindRule(name)
	if cfg == nil {
		return
	}

	if cfg.Enabled {
		t.app.engine.StopRule(name)
		t.Refresh()
		t.app.setStatus(fmt.Sprintf("Stopped rule: %s", name))
	} else {
		if err := t.app.engine.StartRule(name); err != nil {
			t.app.setStatus(fmt.Sprintf("Failed to start: %v", err))
			return
		}
		t.Refresh()
		t.app.setStatus(fmt.Sprintf("Started rule: %s", name))
	}
}

func (t *RulesTab) testSelected() {
	name := t.getSelectedName()
	if name == "" {
		return
	}

	t.app.setStatus(fmt.Sprintf("Test firing rule: %s - check Debug tab for results", name))

	go func() {
		err := t.app.engine.TestFireRule(name)

		go func() {
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

func (t *RulesTab) updateButtonBar() {
	th := CurrentTheme
	buttonText := " " + th.TagHotkey + "a" + th.TagActionText + "dd  " +
		th.TagHotkey + "x" + th.TagActionText + " remove  " +
		th.TagHotkey + "e" + th.TagActionText + "dit  " +
		th.TagHotkey + "Space" + th.TagActionText + " toggle  " +
		th.TagHotkey + "F" + th.TagActionText + "ire  " +
		th.TagActionText + "|  " +
		th.TagHotkey + "?" + th.TagActionText + " help " + th.TagReset
	t.buttonBar.SetText(buttonText)
}

// RefreshTheme updates theme-dependent UI elements.
func (t *RulesTab) RefreshTheme() {
	t.updateButtonBar()
	th := CurrentTheme
	t.tableFrame.SetBorderColor(th.Border).SetTitleColor(th.Accent)
	t.condTable.SetBorderColor(th.Border).SetTitleColor(th.Accent)
	t.info.SetBorderColor(th.Border).SetTitleColor(th.Accent)
	t.info.SetTextColor(th.Text)
	t.statusBar.SetTextColor(th.Text)
	ApplyTableTheme(t.table)
	ApplyTableTheme(t.condTable)
	// Update header colors
	for i := 0; i < t.table.GetColumnCount(); i++ {
		if cell := t.table.GetCell(0, i); cell != nil {
			cell.SetTextColor(th.Accent)
		}
	}
	for i := 0; i < t.condTable.GetColumnCount(); i++ {
		if cell := t.condTable.GetCell(0, i); cell != nil {
			cell.SetTextColor(th.Accent)
		}
	}
}

// parseValue converts a string to the appropriate Go type.
func parseValue(s string) interface{} {
	if s == "true" {
		return true
	}
	if s == "false" {
		return false
	}
	var intVal int
	if _, err := fmt.Sscanf(s, "%d", &intVal); err == nil {
		return intVal
	}
	var floatVal float64
	if _, err := fmt.Sscanf(s, "%f", &floatVal); err == nil {
		return floatVal
	}
	return s
}
