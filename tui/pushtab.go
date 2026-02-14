package tui

import (
	"fmt"
	"sort"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"warlink/config"
	"warlink/engine"
	"warlink/push"
	"warlink/trigger"
)

// PushTab handles the Push webhook configuration tab.
type PushTab struct {
	app        *App
	flex       *tview.Flex
	table      *tview.Table
	tableFrame *tview.Frame
	condTable  *tview.Table
	info       *tview.TextView
	statusBar  *tview.TextView
	buttonBar  *tview.TextView

	selectedPush string
}

// NewPushTab creates a new Push tab.
func NewPushTab(app *App) *PushTab {
	t := &PushTab{app: app}
	t.setupUI()
	t.Refresh()
	return t
}

func (t *PushTab) setupUI() {
	// Button bar (themed)
	t.buttonBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
	t.updateButtonBar()

	// Push table
	t.table = tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0)
	ApplyTableTheme(t.table)

	t.table.SetInputCapture(t.handleKeys)
	t.table.SetSelectionChangedFunc(t.onSelectionChanged)

	// Set up headers (themed)
	headers := []string{"", "Name", "Conditions", "URL", "Method", "Sends", "Status"}
	for i, h := range headers {
		t.table.SetCell(0, i, tview.NewTableCell(h).
			SetTextColor(CurrentTheme.Accent).
			SetSelectable(false).
			SetAttributes(tcell.AttrBold))
	}

	t.tableFrame = tview.NewFrame(t.table).SetBorders(1, 0, 0, 0, 1, 1)
	t.tableFrame.SetBorder(true).SetTitle(" Push Webhooks ").SetBorderColor(CurrentTheme.Border).SetTitleColor(CurrentTheme.Accent)

	// Conditions table for selected push
	t.condTable = tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false)
	ApplyTableTheme(t.condTable)
	t.condTable.SetBorder(true).SetTitle(" Conditions ").SetBorderColor(CurrentTheme.Border).SetTitleColor(CurrentTheme.Accent)
	t.condTable.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
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
	t.info.SetBorder(true).SetTitle(" Push Details ").SetBorderColor(CurrentTheme.Border).SetTitleColor(CurrentTheme.Accent)

	// Right panel with conditions and info
	rightPanel := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(t.condTable, 0, 1, false).
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

func (t *PushTab) handleKeys(event *tcell.EventKey) *tcell.EventKey {
	switch event.Rune() {
	case 'a':
		t.showAddDialog()
		return nil
	case 'x':
		t.confirmRemovePush()
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
		return nil
	}
	return event
}

func (t *PushTab) getSelectedName() string {
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

func (t *PushTab) onSelectionChanged(row, col int) {
	name := t.getSelectedName()
	if name == "" {
		return
	}
	t.selectedPush = name
	t.updateConditionsList()
	t.updateInfo(name)
}

func (t *PushTab) updateConditionsList() {
	t.condTable.Clear()

	cfg := t.app.config.FindPush(t.selectedPush)
	if cfg == nil {
		return
	}

	// Set headers
	th := CurrentTheme
	condHeaders := []string{"PLC", "Tag", "Op", "Value"}
	for i, h := range condHeaders {
		t.condTable.SetCell(0, i, tview.NewTableCell(h).
			SetTextColor(th.Accent).
			SetSelectable(false).
			SetAttributes(tcell.AttrBold))
	}

	for i, cond := range cfg.Conditions {
		row := i + 1
		t.condTable.SetCell(row, 0, tview.NewTableCell(cond.PLC).SetExpansion(1))
		t.condTable.SetCell(row, 1, tview.NewTableCell(cond.Tag).SetExpansion(1))
		t.condTable.SetCell(row, 2, tview.NewTableCell(cond.Operator).SetExpansion(0))
		t.condTable.SetCell(row, 3, tview.NewTableCell(fmt.Sprintf("%v", cond.Value)).SetExpansion(1))
	}
}

func (t *PushTab) updateInfo(name string) {
	cfg := t.app.config.FindPush(name)
	if cfg == nil {
		t.info.SetText("")
		return
	}

	th := CurrentTheme
	info := th.Label("URL", cfg.URL) + "\n"
	info += th.Label("Method", cfg.Method) + "\n"

	ct := cfg.ContentType
	if ct == "" {
		ct = "application/json"
	}
	info += th.Label("Content-Type", ct) + "\n"

	if cfg.Auth.Type != "" {
		info += th.Label("Auth", string(cfg.Auth.Type)) + "\n"
	}

	cooldown := cfg.CooldownMin
	if cooldown == 0 {
		cooldown = 15 * time.Minute
	}
	info += th.Label("Cooldown", cooldown.String())
	if cfg.CooldownPerCond {
		info += " (per-condition)"
	}
	info += "\n"

	if cfg.Body != "" {
		body := cfg.Body
		if len(body) > 60 {
			body = body[:57] + "..."
		}
		info += th.Label("Body", body) + "\n"
	}

	// Get runtime status
	if t.app.pushMgr != nil {
		status, err, count, lastSend, lastCode := t.app.pushMgr.GetPushStatus(name)
		info += fmt.Sprintf("\n%sStatus:%s %s  %sSends:%s %d\n",
			th.TagAccent, th.TagReset, status.String(),
			th.TagAccent, th.TagReset, count)
		if lastCode > 0 {
			info += fmt.Sprintf("%sLast HTTP:%s %d\n", th.TagAccent, th.TagReset, lastCode)
		}
		if !lastSend.IsZero() {
			info += fmt.Sprintf("%sLast Send:%s %s\n", th.TagAccent, th.TagReset, lastSend.Format("15:04:05"))
		}
		if err != nil {
			info += th.ErrorText("Error: "+err.Error()) + "\n"
		}
	}

	t.info.SetText(info)
}

// GetPrimitive returns the main primitive for this tab.
func (t *PushTab) GetPrimitive() tview.Primitive {
	return t.flex
}

// GetFocusable returns the element that should receive focus.
func (t *PushTab) GetFocusable() tview.Primitive {
	return t.table
}

// Refresh updates the display.
func (t *PushTab) Refresh() {
	// Clear existing rows (keep header)
	for t.table.GetRowCount() > 1 {
		t.table.RemoveRow(1)
	}

	// Sort pushes by name
	pushes := make([]config.PushConfig, len(t.app.config.Pushes))
	copy(pushes, t.app.config.Pushes)
	sort.Slice(pushes, func(i, j int) bool {
		return pushes[i].Name < pushes[j].Name
	})

	// Add pushes to table
	for i, cfg := range pushes {
		row := i + 1

		// Get runtime status
		var status push.Status
		var count int64
		if t.app.pushMgr != nil {
			status, _, count, _, _ = t.app.pushMgr.GetPushStatus(cfg.Name)
		}

		// Status indicator
		indicatorCell := tview.NewTableCell(GetStatusBullet()).SetExpansion(0)
		switch status {
		case push.StatusArmed:
			indicatorCell.SetTextColor(IndicatorGreen)
		case push.StatusFiring:
			indicatorCell.SetTextColor(tcell.ColorYellow)
		case push.StatusWaitingClear, push.StatusMinInterval:
			indicatorCell.SetTextColor(tcell.ColorOrange)
		case push.StatusError:
			indicatorCell.SetTextColor(IndicatorRed)
		default:
			indicatorCell.SetTextColor(IndicatorGray)
		}

		// Conditions summary
		condStr := fmt.Sprintf("%d", len(cfg.Conditions))

		// Method
		method := cfg.Method
		if method == "" {
			method = "POST"
		}

		// URL (truncated)
		urlStr := cfg.URL
		if len(urlStr) > 40 {
			urlStr = urlStr[:37] + "..."
		}

		t.table.SetCell(row, 0, indicatorCell)
		t.table.SetCell(row, 1, tview.NewTableCell(cfg.Name).SetExpansion(1))
		t.table.SetCell(row, 2, tview.NewTableCell(condStr).SetExpansion(0))
		t.table.SetCell(row, 3, tview.NewTableCell(urlStr).SetExpansion(2))
		t.table.SetCell(row, 4, tview.NewTableCell(method).SetExpansion(0))
		t.table.SetCell(row, 5, tview.NewTableCell(fmt.Sprintf("%d", count)).SetExpansion(0))
		t.table.SetCell(row, 6, tview.NewTableCell(status.String()).SetExpansion(1))
	}

	// Update status bar
	armed := 0
	if t.app.pushMgr != nil {
		for _, cfg := range pushes {
			status, _, _, _, _ := t.app.pushMgr.GetPushStatus(cfg.Name)
			if status == push.StatusArmed {
				armed++
			}
		}
	}
	t.statusBar.SetText(fmt.Sprintf(" %d pushes, %d armed | Tab to switch to conditions", len(pushes), armed))

	// Update conditions and info for selected
	if name := t.getSelectedName(); name != "" {
		t.selectedPush = name
		t.updateConditionsList()
		t.updateInfo(name)
	}
}

func (t *PushTab) showAddDialog() {
	const pageName = "add-push"

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
	form.SetBorder(true).SetTitle(" Add Push Webhook ")

	form.AddInputField("Name:", "", 30, nil, nil)
	form.AddInputField("URL:", "", 50, nil, nil)
	form.AddDropDown("Method:", []string{"POST", "GET", "PUT", "PATCH"}, 0, nil)
	form.AddInputField("Content Type:", "application/json", 30, nil, nil)
	form.AddInputField("Body:", "", 50, nil, nil)
	form.AddDropDown("Auth:", []string{"None", "Bearer", "Basic", "JWT", "Custom Header"}, 0, nil)
	form.AddInputField("Token/Password:", "", 30, nil, nil)
	form.AddInputField("Cooldown:", "15m", 15, nil, nil)
	form.AddCheckbox("Per-Condition:", false, nil)
	form.AddInputField("Timeout:", "30s", 15, nil, nil)
	form.AddCheckbox("Enabled:", true, nil)

	form.AddButton("Add", func() {
		name := form.GetFormItemByLabel("Name:").(*tview.InputField).GetText()
		urlStr := form.GetFormItemByLabel("URL:").(*tview.InputField).GetText()
		methodIdx, _ := form.GetFormItemByLabel("Method:").(*tview.DropDown).GetCurrentOption()
		contentType := form.GetFormItemByLabel("Content Type:").(*tview.InputField).GetText()
		body := form.GetFormItemByLabel("Body:").(*tview.InputField).GetText()
		authIdx, _ := form.GetFormItemByLabel("Auth:").(*tview.DropDown).GetCurrentOption()
		tokenPwd := form.GetFormItemByLabel("Token/Password:").(*tview.InputField).GetText()
		cooldownStr := form.GetFormItemByLabel("Cooldown:").(*tview.InputField).GetText()
		perCond := form.GetFormItemByLabel("Per-Condition:").(*tview.Checkbox).IsChecked()
		timeoutStr := form.GetFormItemByLabel("Timeout:").(*tview.InputField).GetText()
		enabled := form.GetFormItemByLabel("Enabled:").(*tview.Checkbox).IsChecked()

		if name == "" || urlStr == "" {
			t.app.showError("Error", "Name and URL are required")
			return
		}

		methods := []string{"POST", "GET", "PUT", "PATCH"}
		authTypes := []config.PushAuthType{"", config.PushAuthBearer, config.PushAuthBasic, config.PushAuthJWT, config.PushAuthCustomHeader}

		var cooldown time.Duration
		if d, err := time.ParseDuration(cooldownStr); err == nil {
			cooldown = d
		} else {
			cooldown = 15 * time.Minute
		}
		var timeout time.Duration
		if d, err := time.ParseDuration(timeoutStr); err == nil {
			timeout = d
		} else {
			timeout = 30 * time.Second
		}

		authCfg := config.PushAuthConfig{Type: authTypes[authIdx]}
		switch authCfg.Type {
		case config.PushAuthBearer, config.PushAuthJWT:
			authCfg.Token = tokenPwd
		case config.PushAuthBasic:
			authCfg.Password = tokenPwd
		case config.PushAuthCustomHeader:
			authCfg.HeaderValue = tokenPwd
		}

		if err := t.app.engine.CreatePush(engine.PushCreateRequest{
			Name:            name,
			Enabled:         enabled,
			Conditions:      []config.PushCondition{},
			URL:             urlStr,
			Method:          methods[methodIdx],
			ContentType:     contentType,
			Body:            body,
			Auth:            authCfg,
			CooldownMin:     cooldown,
			CooldownPerCond: perCond,
			Timeout:         timeout,
		}); err != nil {
			t.app.showError("Error", fmt.Sprintf("Failed to create push: %v", err))
			return
		}

		t.app.closeModal(pageName)
		t.Refresh()
		t.app.setStatus(fmt.Sprintf("Added push: %s - add conditions via edit", name))
	})

	form.AddButton("Cancel", func() {
		t.app.closeModal(pageName)
	})

	t.app.showFormModal(pageName, form, 65, 28, func() {
		t.app.closeModal(pageName)
	})
}

func (t *PushTab) showEditDialog() {
	const pageName = "edit-push"

	name := t.getSelectedName()
	if name == "" {
		return
	}

	cfg := t.app.config.FindPush(name)
	if cfg == nil {
		return
	}

	// Get list of PLCs for condition editing
	plcNames := make([]string, 0)
	for _, plc := range t.app.config.PLCs {
		plcNames = append(plcNames, plc.Name)
	}

	methodIdx := 0
	methods := []string{"POST", "GET", "PUT", "PATCH"}
	for i, m := range methods {
		if m == cfg.Method {
			methodIdx = i
			break
		}
	}

	authTypes := []config.PushAuthType{"", config.PushAuthBearer, config.PushAuthBasic, config.PushAuthJWT, config.PushAuthCustomHeader}
	authIdx := 0
	for i, a := range authTypes {
		if a == cfg.Auth.Type {
			authIdx = i
			break
		}
	}

	tokenPwd := cfg.Auth.Token
	if cfg.Auth.Type == config.PushAuthBasic {
		tokenPwd = cfg.Auth.Password
	} else if cfg.Auth.Type == config.PushAuthCustomHeader {
		tokenPwd = cfg.Auth.HeaderValue
	}

	cooldownStr := cfg.CooldownMin.String()
	if cfg.CooldownMin == 0 {
		cooldownStr = "15m"
	}
	timeoutStr := cfg.Timeout.String()
	if cfg.Timeout == 0 {
		timeoutStr = "30s"
	}

	form := tview.NewForm()
	ApplyFormTheme(form)
	form.SetBorder(true).SetTitle(" Edit Push Webhook ")

	form.AddInputField("URL:", cfg.URL, 50, nil, nil)
	form.AddDropDown("Method:", methods, methodIdx, nil)
	form.AddInputField("Content Type:", cfg.ContentType, 30, nil, nil)
	form.AddInputField("Body:", cfg.Body, 50, nil, nil)
	form.AddDropDown("Auth:", []string{"None", "Bearer", "Basic", "JWT", "Custom Header"}, authIdx, nil)
	form.AddInputField("Token/Password:", tokenPwd, 30, nil, nil)
	form.AddInputField("Cooldown:", cooldownStr, 15, nil, nil)
	form.AddCheckbox("Per-Condition:", cfg.CooldownPerCond, nil)
	form.AddInputField("Timeout:", timeoutStr, 15, nil, nil)
	form.AddCheckbox("Enabled:", cfg.Enabled, nil)

	// Conditions section - show current conditions summary
	condSummary := fmt.Sprintf("%d condition(s)", len(cfg.Conditions))
	form.AddInputField("Conditions:", condSummary, 30, nil, nil)

	// Add condition button fields
	if len(plcNames) > 0 {
		form.AddDropDown("+ Cond PLC:", plcNames, 0, nil)

		// Get tags for first PLC
		selectedPLC := plcNames[0]
		tagOptions := t.app.GetEnabledTags(selectedPLC)
		if len(tagOptions) == 0 {
			tagOptions = []string{"(no tags)"}
		}

		var condTagDropdown *tview.DropDown
		condTagDropdown = tview.NewDropDown().SetLabel("+ Cond Tag:").SetOptions(tagOptions, nil)
		condTagDropdown.SetCurrentOption(0)

		form.GetFormItemByLabel("+ Cond PLC:").(*tview.DropDown).SetSelectedFunc(func(option string, index int) {
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

		form.AddDropDown("+ Cond Op:", trigger.ValidOperators(), 0, nil)
		form.AddInputField("+ Cond Value:", "true", 15, nil, nil)

		form.AddButton("Add Cond", func() {
			plcIdx, _ := form.GetFormItemByLabel("+ Cond PLC:").(*tview.DropDown).GetCurrentOption()
			_, condTag := condTagDropdown.GetCurrentOption()
			opIdx, _ := form.GetFormItemByLabel("+ Cond Op:").(*tview.DropDown).GetCurrentOption()
			condValue := form.GetFormItemByLabel("+ Cond Value:").(*tview.InputField).GetText()

			if condTag == "(no tags)" {
				return
			}

			var value interface{} = condValue
			if condValue == "true" {
				value = true
			} else if condValue == "false" {
				value = false
			} else {
				var intVal int
				if _, err := fmt.Sscanf(condValue, "%d", &intVal); err == nil {
					value = intVal
				} else {
					var floatVal float64
					if _, err := fmt.Sscanf(condValue, "%f", &floatVal); err == nil {
						value = floatVal
					}
				}
			}

			cond := config.PushCondition{
				PLC:      plcNames[plcIdx],
				Tag:      condTag,
				Operator: trigger.ValidOperators()[opIdx],
				Value:    value,
			}

			cfg.Conditions = append(cfg.Conditions, cond)
			// Update conditions display
			condInput := form.GetFormItemByLabel("Conditions:").(*tview.InputField)
			condInput.SetText(fmt.Sprintf("%d condition(s)", len(cfg.Conditions)))
			t.updateConditionsList()
			t.app.setStatus(fmt.Sprintf("Added condition: %s.%s %s %v", cond.PLC, cond.Tag, cond.Operator, cond.Value))
		})
	}

	form.AddButton("Save", func() {
		urlStr := form.GetFormItemByLabel("URL:").(*tview.InputField).GetText()
		newMethodIdx, _ := form.GetFormItemByLabel("Method:").(*tview.DropDown).GetCurrentOption()
		contentType := form.GetFormItemByLabel("Content Type:").(*tview.InputField).GetText()
		body := form.GetFormItemByLabel("Body:").(*tview.InputField).GetText()
		newAuthIdx, _ := form.GetFormItemByLabel("Auth:").(*tview.DropDown).GetCurrentOption()
		newTokenPwd := form.GetFormItemByLabel("Token/Password:").(*tview.InputField).GetText()
		newCooldownStr := form.GetFormItemByLabel("Cooldown:").(*tview.InputField).GetText()
		perCond := form.GetFormItemByLabel("Per-Condition:").(*tview.Checkbox).IsChecked()
		newTimeoutStr := form.GetFormItemByLabel("Timeout:").(*tview.InputField).GetText()
		enabled := form.GetFormItemByLabel("Enabled:").(*tview.Checkbox).IsChecked()

		if urlStr == "" {
			t.app.showError("Error", "URL is required")
			return
		}

		var cooldown time.Duration
		if d, err := time.ParseDuration(newCooldownStr); err == nil {
			cooldown = d
		}
		var timeout time.Duration
		if d, err := time.ParseDuration(newTimeoutStr); err == nil {
			timeout = d
		}

		newAuthCfg := config.PushAuthConfig{Type: authTypes[newAuthIdx]}
		switch newAuthCfg.Type {
		case config.PushAuthBearer, config.PushAuthJWT:
			newAuthCfg.Token = newTokenPwd
		case config.PushAuthBasic:
			newAuthCfg.Password = newTokenPwd
			newAuthCfg.Username = cfg.Auth.Username // Preserve existing username
		case config.PushAuthCustomHeader:
			newAuthCfg.HeaderName = cfg.Auth.HeaderName // Preserve existing header name
			newAuthCfg.HeaderValue = newTokenPwd
		}

		if err := t.app.engine.UpdatePush(name, engine.PushUpdateRequest{
			Enabled:         enabled,
			Conditions:      cfg.Conditions,
			URL:             urlStr,
			Method:          methods[newMethodIdx],
			ContentType:     contentType,
			Headers:         cfg.Headers,
			Body:            body,
			Auth:            newAuthCfg,
			CooldownMin:     cooldown,
			CooldownPerCond: perCond,
			Timeout:         timeout,
		}); err != nil {
			t.app.showError("Error", fmt.Sprintf("Failed to update push: %v", err))
			return
		}

		t.app.closeModal(pageName)
		t.Refresh()
		t.app.setStatus(fmt.Sprintf("Updated push: %s", name))
	})

	form.AddButton("Cancel", func() {
		t.app.closeModal(pageName)
	})

	t.app.showFormModal(pageName, form, 70, 34, func() {
		t.app.closeModal(pageName)
	})
}

func (t *PushTab) confirmRemovePush() {
	name := t.getSelectedName()
	if name == "" {
		return
	}

	t.app.showConfirm("Remove Push", fmt.Sprintf("Remove %s?", name), func() {
		if err := t.app.engine.DeletePush(name); err != nil {
			t.app.showError("Error", err.Error())
			return
		}
		t.Refresh()
		t.app.setStatus(fmt.Sprintf("Removed push: %s", name))
	})
}

func (t *PushTab) toggleSelected() {
	name := t.getSelectedName()
	if name == "" {
		return
	}

	cfg := t.app.config.FindPush(name)
	if cfg == nil {
		return
	}

	if cfg.Enabled {
		t.app.engine.StopPush(name)
		t.Refresh()
		t.app.setStatus(fmt.Sprintf("Stopped push: %s", name))
	} else {
		if err := t.app.engine.StartPush(name); err != nil {
			t.app.setStatus(fmt.Sprintf("Failed to start: %v", err))
			return
		}
		t.Refresh()
		t.app.setStatus(fmt.Sprintf("Started push: %s", name))
	}
}

func (t *PushTab) testSelected() {
	name := t.getSelectedName()
	if name == "" {
		return
	}

	t.app.setStatus(fmt.Sprintf("Test firing push: %s - check Debug tab for results", name))

	go func() {
		err := t.app.engine.TestFirePush(name)

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

func (t *PushTab) updateButtonBar() {
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
func (t *PushTab) RefreshTheme() {
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

