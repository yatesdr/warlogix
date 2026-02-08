package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"warlogix/ads"
	"warlogix/config"
	"warlogix/driver"
	"warlogix/logix"
	"warlogix/omron"
	"warlogix/plcman"
	"warlogix/s7"
)

// BrowserTab handles the tag browser tab.
type BrowserTab struct {
	app       *App
	flex      *tview.Flex
	plcSelect *tview.DropDown
	filter    *tview.InputField
	tree      *tview.TreeView
	treeFrame *tview.Frame
	details   *tview.TextView
	statusBar *tview.TextView
	buttonBar *tview.TextView

	selectedPLC          string
	lastPLCOptions       []string              // Track dropdown options to avoid unnecessary updates
	lastConnectionStatus plcman.ConnectionStatus // Track connection status to reload tags on connect
	updatingDropdown     bool                  // True when programmatically updating dropdown
	treeRoot          *tview.TreeNode
	tagNodes          map[string]*tview.TreeNode // Tag name -> tree node for quick lookup
	enabledTags       map[string]bool            // Tag name -> enabled for current PLC
	writableTags      map[string]bool            // Tag name -> writable for current PLC
	ignoredMembers    map[string]map[string]bool // Parent tag -> member name -> ignored for change detection
	filterText        string                     // Current filter text (lowercase)
}

// NewBrowserTab creates a new browser tab.
func NewBrowserTab(app *App) *BrowserTab {
	t := &BrowserTab{
		app:            app,
		tagNodes:       make(map[string]*tview.TreeNode),
		enabledTags:    make(map[string]bool),
		writableTags:   make(map[string]bool),
		ignoredMembers: make(map[string]map[string]bool),
	}
	t.setupUI()
	return t
}

func (t *BrowserTab) setupUI() {
	// Button bar
	t.buttonBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
	t.updateButtonBar()

	// PLC dropdown
	t.plcSelect = tview.NewDropDown().
		SetLabel("PLC: ").
		SetFieldWidth(20)
	ApplyDropDownTheme(t.plcSelect)
	t.plcSelect.SetSelectedFunc(func(text string, index int) {
		t.selectedPLC = text
		t.loadTags()
		t.updateButtonBar() // Update button bar based on PLC type (manual vs discovery)
		// Only change focus if this was a user selection, not programmatic
		if !t.updatingDropdown {
			t.app.app.SetFocus(t.tree)
		}
	})
	t.plcSelect.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			t.app.app.SetFocus(t.tree)
			return nil
		}
		return event
	})

	// Filter input
	t.filter = tview.NewInputField().
		SetLabel("Filter: ").
		SetFieldWidth(30)
	t.filter.SetChangedFunc(func(text string) {
		t.applyFilter(text)
	})
	t.filter.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape || event.Key() == tcell.KeyEnter {
			t.app.app.SetFocus(t.tree)
			return nil
		}
		return event
	})
	ApplyInputFieldTheme(t.filter)

	// Header row
	header := tview.NewFlex().
		AddItem(t.plcSelect, 30, 0, false).
		AddItem(nil, 2, 0, false).
		AddItem(t.filter, 40, 0, false).
		AddItem(nil, 0, 1, false)

	// Tree view for programs/tags
	t.treeRoot = tview.NewTreeNode("Tags").SetColor(CurrentTheme.Accent).
		SetSelectedTextStyle(tcell.StyleDefault.Foreground(CurrentTheme.SelectedText).Background(CurrentTheme.Accent))
	t.tree = tview.NewTreeView().
		SetRoot(t.treeRoot).
		SetCurrentNode(t.treeRoot)

	t.tree.SetSelectedFunc(t.onNodeSelected)
	t.tree.SetInputCapture(t.handleTreeKeys)

	t.treeFrame = tview.NewFrame(t.tree).SetBorders(0, 0, 0, 0, 0, 0)
	t.treeFrame.SetBorder(true).SetTitle(" Programs/Tags ").SetBorderColor(CurrentTheme.Border).SetTitleColor(CurrentTheme.Accent)

	// Details panel
	t.details = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetTextColor(CurrentTheme.Text)
	t.details.SetBorder(true).SetTitle(" Tag Details ").SetBorderColor(CurrentTheme.Border).SetTitleColor(CurrentTheme.Accent)
	t.details.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape || event.Key() == tcell.KeyTab {
			t.app.app.SetFocus(t.tree)
			return nil
		}
		return event
	})

	// Content area
	content := tview.NewFlex().
		AddItem(t.treeFrame, 0, 1, true).
		AddItem(t.details, 40, 0, false)

	// Status bar
	t.statusBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextColor(CurrentTheme.Text)

	// Main layout - buttonBar at top, outside frames
	t.flex = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(t.buttonBar, 1, 0, false).
		AddItem(header, 1, 0, false).
		AddItem(content, 0, 1, true).
		AddItem(t.statusBar, 1, 0, false)
}

func (t *BrowserTab) handleTreeKeys(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyEnter:
		node := t.tree.GetCurrentNode()
		if node != nil {
			t.onNodeSelected(node)
		}
		return nil
	case tcell.KeyTab:
		// Tab to details panel for scrolling
		t.app.app.SetFocus(t.details)
		return nil
	case tcell.KeyEscape:
		// Return focus to tree from filter/dropdown
		t.app.app.SetFocus(t.tree)
		return nil
	}

	switch event.Rune() {
	case ' ':
		// Toggle selection
		node := t.tree.GetCurrentNode()
		if node != nil {
			t.toggleNodeSelection(node)
		}
		return nil
	case '/':
		// Focus filter input
		t.app.app.SetFocus(t.filter)
		return nil
	case 'p':
		// Focus and open PLC dropdown
		t.app.app.SetFocus(t.plcSelect)
		// Simulate Enter key to open the dropdown (provide setFocus function)
		t.plcSelect.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(p tview.Primitive) {
			t.app.app.SetFocus(p)
		})
		return nil
	case 'c':
		// Clear filter
		t.filter.SetText("")
		t.applyFilter("")
		return nil
	case 'd':
		// Show detailed tag info
		node := t.tree.GetCurrentNode()
		if node != nil {
			t.showDetailedTagInfo(node)
		}
		return nil
	case 'w':
		// Toggle writable
		node := t.tree.GetCurrentNode()
		if node != nil {
			t.toggleNodeWritable(node)
		}
		return nil
	case 'W':
		// Write value to tag (only if writable)
		node := t.tree.GetCurrentNode()
		if node != nil {
			t.showWriteDialog(node)
		}
		return nil
	case 'i':
		// Toggle ignore for change detection (for UDT members)
		node := t.tree.GetCurrentNode()
		if node != nil {
			t.toggleNodeIgnore(node)
		}
		return nil
	case 'a':
		// Add manual tag (only for non-discovery PLCs)
		if t.isManualPLC() {
			t.showAddTagDialog()
		}
		return nil
	case 'e':
		// Edit manual tag (only for non-discovery PLCs)
		if t.isManualPLC() {
			node := t.tree.GetCurrentNode()
			if node != nil {
				t.showEditTagDialog(node)
			}
		}
		return nil
	case 'x':
		// Delete manual tag (only for non-discovery PLCs)
		if t.isManualPLC() {
			node := t.tree.GetCurrentNode()
			if node != nil {
				t.deleteManualTag(node)
			}
		}
		return nil
	case 's':
		// Configure services for tag
		node := t.tree.GetCurrentNode()
		if node != nil {
			t.showServicesDialog(node)
		}
		return nil
	}

	return event
}

// getTypeName returns the appropriate type name for a type code based on the current PLC family.
// Each PLC family uses its native type codes for accurate display.
func (t *BrowserTab) getTypeName(typeCode uint16) string {
	cfg := t.app.config.FindPLC(t.selectedPLC)
	if cfg != nil {
		switch cfg.GetFamily() {
		case config.FamilyS7:
			return s7.TypeName(typeCode)
		case config.FamilyBeckhoff:
			return ads.TypeName(typeCode)
		case config.FamilyOmron:
			return omron.TypeName(typeCode)
		}
	}
	return logix.TypeName(typeCode)
}

func (t *BrowserTab) onNodeSelected(node *tview.TreeNode) {
	ref := node.GetReference()
	if ref == nil {
		// It's a program/section node - expand/collapse, but not the root
		if node != t.treeRoot {
			node.SetExpanded(!node.IsExpanded())
		}
		return
	}

	// It's a tag node, show details
	tagInfo, ok := ref.(*driver.TagInfo)
	if !ok {
		return
	}

	// Check if this is a UDT that can be expanded
	if logix.IsStructure(tagInfo.TypeCode) {
		// If not yet expanded (no children), try to expand it
		if len(node.GetChildren()) == 0 {
			t.lazyExpandUDT(node, tagInfo)
			// lazyExpandUDT sets expanded=true, don't toggle
		} else {
			// Already has children, just toggle expand/collapse
			node.SetExpanded(!node.IsExpanded())
		}
	}

	t.showTagDetails(tagInfo)
}

// lazyExpandUDT expands a UDT node by fetching its template and adding member children.
// This is done lazily when the user first selects/expands the node.
func (t *BrowserTab) lazyExpandUDT(node *tview.TreeNode, tagInfo *driver.TagInfo) {
	plc := t.app.manager.GetPLC(t.selectedPLC)
	if plc == nil {
		DebugLog("lazyExpandUDT: no PLC for %s", tagInfo.Name)
		return
	}

	client := plc.GetLogixClient()
	if client == nil {
		DebugLog("lazyExpandUDT: no client for %s", tagInfo.Name)
		return
	}

	// Fetch template
	tmpl, err := client.GetTemplate(tagInfo.TypeCode)
	if err != nil {
		DebugLog("lazyExpandUDT: failed to get template for %s: %v", tagInfo.Name, err)
		return
	}

	// Determine base path - for arrays, we need to add [0] to access first element
	basePath := tagInfo.Name

	// Check multiple ways to detect arrays:
	// 1. Dimensions populated from discovery
	// 2. Array bit in type code (0x2000 or 0x4000 for 1D/2D)
	// 3. Tag name ends with "[]" (some PLCs return this notation)
	// 4. Use TagInfo.IsArray() which combines dimensions and type code check
	hasDims := len(tagInfo.Dimensions) > 0
	isArrayType := logix.IsArrayType(tagInfo.TypeCode)
	isArrayBitSet := logix.IsArray(tagInfo.TypeCode) // Check 0x2000 bit specifically
	nameHasBrackets := strings.HasSuffix(tagInfo.Name, "[]")
	isArray := hasDims || isArrayType || isArrayBitSet || nameHasBrackets

	DebugLog("lazyExpandUDT: %s - hasDims=%v, isArrayType=%v, isArrayBit=%v, nameBrackets=%v, typeCode=0x%04X, dims=%v",
		tagInfo.Name, hasDims, isArrayType, isArrayBitSet, nameHasBrackets, tagInfo.TypeCode, tagInfo.Dimensions)

	if isArray {
		// For array UDTs, show members of first element by default
		// Remove trailing [] if present, then add [0]
		basePath = strings.TrimSuffix(tagInfo.Name, "[]") + "[0]"
		DebugLog("lazyExpandUDT: %s is an array, using base path %s", tagInfo.Name, basePath)
	}

	// Add member nodes
	addedCount := 0
	for _, member := range tmpl.Members {
		if member.Hidden || member.Name == "" {
			continue
		}

		memberPath := basePath + "." + member.Name

		// Create synthetic TagInfo for member, including array dimensions from template
		dims := make([]uint32, len(member.ArrayDims))
		for i, d := range member.ArrayDims {
			dims[i] = uint32(d)
		}
		memberInfo := &driver.TagInfo{
			Name:       memberPath,
			TypeCode:   member.Type,
			Dimensions: dims,
		}

		enabled := t.enabledTags[memberPath]
		writable := t.writableTags[memberPath]
		memberNode := t.createTagNode(memberInfo, enabled, writable)

		node.AddChild(memberNode)
		t.tagNodes[memberPath] = memberNode
		addedCount++
	}

	DebugLog("lazyExpandUDT: expanded %s with %d visible members", tagInfo.Name, addedCount)

	// Force the node to be expanded to show children
	node.SetExpanded(true)
}

func (t *BrowserTab) toggleNodeSelection(node *tview.TreeNode) {
	ref := node.GetReference()
	if ref == nil {
		return
	}

	tagInfo, ok := ref.(*driver.TagInfo)
	if !ok {
		return
	}

	tagName := tagInfo.Name

	// Track previous state to detect enable transition
	wasEnabled := t.enabledTags[tagName]

	// Toggle enabled state
	t.enabledTags[tagName] = !t.enabledTags[tagName]
	enabled := t.enabledTags[tagName]
	writable := t.writableTags[tagName]

	// Update node text
	t.updateNodeText(node, tagInfo, enabled, writable)

	// Update config
	t.updateConfigTag(tagName, enabled, writable)

	// If tag was just enabled and it's a UDT, auto-populate ignore list with volatile members
	if enabled && !wasEnabled && logix.IsStructure(tagInfo.TypeCode) {
		t.autoPopulateIgnoreList(tagName, tagInfo.TypeCode)
	}

	// If tag was just enabled, force publish its current value immediately
	if enabled && !wasEnabled && t.selectedPLC != "" {
		go t.app.ForcePublishTag(t.selectedPLC, tagName)
	}

	// Update status
	t.updateStatus()
}

// autoPopulateIgnoreList automatically adds volatile members to the ignore list
// when a UDT tag is first enabled. Volatile members include timers, timestamps,
// and time-related types that change frequently and would cause unnecessary republishing.
func (t *BrowserTab) autoPopulateIgnoreList(tagName string, typeCode uint16) {
	plc := t.app.manager.GetPLC(t.selectedPLC)
	if plc == nil {
		return
	}

	client := plc.GetLogixClient()
	if client == nil {
		return
	}

	// Fetch the template for this UDT
	tmpl, err := client.GetTemplate(typeCode)
	if err != nil {
		DebugLog("autoPopulateIgnoreList: failed to get template for %s: %v", tagName, err)
		return
	}

	// Find volatile members
	var volatileMembers []string
	for _, member := range tmpl.Members {
		if member.Hidden || member.Name == "" {
			continue
		}

		// Check if member type is volatile (time-related)
		memberTypeName := logix.TypeName(member.Type)
		if logix.IsVolatileTypeName(memberTypeName) {
			volatileMembers = append(volatileMembers, member.Name)
		}

		// Also check if the template name indicates a volatile type (for nested UDTs)
		if member.IsStructure() {
			// Try to get the nested template name
			nestedTmpl, err := client.GetTemplate(member.Type)
			if err == nil && logix.IsVolatileTypeName(nestedTmpl.Name) {
				volatileMembers = append(volatileMembers, member.Name)
			}
		}
	}

	if len(volatileMembers) == 0 {
		return
	}

	// Add volatile members to the ignore list in config
	cfg := t.app.config.FindPLC(t.selectedPLC)
	if cfg == nil {
		return
	}

	for i := range cfg.Tags {
		if cfg.Tags[i].Name == tagName {
			// Add each volatile member to ignore list
			for _, memberName := range volatileMembers {
				cfg.Tags[i].AddIgnoreMember(memberName)
				// Update local tracking
				if t.ignoredMembers[tagName] == nil {
					t.ignoredMembers[tagName] = make(map[string]bool)
				}
				t.ignoredMembers[tagName][memberName] = true
			}
			break
		}
	}

	// Save config
	t.app.SaveConfig()

	// Notify user
	if len(volatileMembers) == 1 {
		t.app.setStatus(fmt.Sprintf("Auto-ignored volatile member: %s", volatileMembers[0]))
	} else {
		t.app.setStatus(fmt.Sprintf("Auto-ignored %d volatile members (timers, timestamps)", len(volatileMembers)))
	}
}

func (t *BrowserTab) toggleNodeWritable(node *tview.TreeNode) {
	ref := node.GetReference()
	if ref == nil {
		return
	}

	tagInfo, ok := ref.(*driver.TagInfo)
	if !ok {
		return
	}

	tagName := tagInfo.Name

	// Toggle writable state
	t.writableTags[tagName] = !t.writableTags[tagName]
	enabled := t.enabledTags[tagName]
	writable := t.writableTags[tagName]

	// Update node text
	t.updateNodeText(node, tagInfo, enabled, writable)

	// Update config
	t.updateConfigTag(tagName, enabled, writable)

	// Update status
	t.updateStatus()
}

// toggleNodeIgnore toggles the ignore status for a UDT member.
// This affects change detection - ignored members won't trigger republishing.
func (t *BrowserTab) toggleNodeIgnore(node *tview.TreeNode) {
	ref := node.GetReference()
	if ref == nil {
		return
	}

	tagInfo, ok := ref.(*driver.TagInfo)
	if !ok {
		return
	}

	tagPath := tagInfo.Name

	// Find the parent tag and member name
	parentTag, memberName := t.findParentTagAndMember(tagPath)
	if parentTag == "" || memberName == "" {
		// Not a UDT member or no parent tag configured
		t.app.setStatus("Cannot toggle ignore: tag is not a UDT member")
		return
	}

	cfg := t.app.config.FindPLC(t.selectedPLC)
	if cfg == nil {
		return
	}

	// Find the tag selection for the parent tag
	var tagSel *config.TagSelection
	for i := range cfg.Tags {
		if cfg.Tags[i].Name == parentTag {
			tagSel = &cfg.Tags[i]
			break
		}
	}

	if tagSel == nil {
		// Parent tag not in config - the UDT hasn't been enabled yet
		t.app.setStatus("Enable the parent tag first to configure ignore list")
		return
	}

	// Toggle ignore status
	wasIgnored := tagSel.ShouldIgnoreMember(memberName)
	if wasIgnored {
		tagSel.RemoveIgnoreMember(memberName)
	} else {
		tagSel.AddIgnoreMember(memberName)
	}

	// Update local tracking
	if t.ignoredMembers[parentTag] == nil {
		t.ignoredMembers[parentTag] = make(map[string]bool)
	}
	t.ignoredMembers[parentTag][memberName] = !wasIgnored

	// Save config
	t.app.SaveConfig()

	// Update node text
	enabled := t.enabledTags[tagPath]
	writable := t.writableTags[tagPath]
	t.updateNodeText(node, tagInfo, enabled, writable)

	// Status message
	if wasIgnored {
		t.app.setStatus(fmt.Sprintf("Changes to %s will now be published", memberName))
	} else {
		t.app.setStatus(fmt.Sprintf("Changes to %s will be ignored", memberName))
	}
}

// findParentTagAndMember finds the configured parent tag and the relative member path.
// For "Robot1.Position.Timestamp", if "Robot1" is configured, returns ("Robot1", "Position.Timestamp").
// Returns ("", "") if no parent tag is configured or if this is not a member path.
func (t *BrowserTab) findParentTagAndMember(tagPath string) (string, string) {
	// Check if this looks like a UDT member path (contains a dot)
	dotIdx := strings.Index(tagPath, ".")
	if dotIdx < 0 {
		return "", ""
	}

	cfg := t.app.config.FindPLC(t.selectedPLC)
	if cfg == nil {
		return "", ""
	}

	// Try progressively shorter prefixes to find the configured parent tag
	// For "Robot1.Position.Timestamp", try:
	//   1. "Robot1.Position" (member would be "Timestamp")
	//   2. "Robot1" (member would be "Position.Timestamp")
	parts := strings.Split(tagPath, ".")
	for i := len(parts) - 1; i > 0; i-- {
		candidate := strings.Join(parts[:i], ".")
		for _, sel := range cfg.Tags {
			if sel.Name == candidate {
				memberName := strings.Join(parts[i:], ".")
				return candidate, memberName
			}
		}
	}

	return "", ""
}

// isMemberIgnored checks if a member is in the ignore list for change detection.
func (t *BrowserTab) isMemberIgnored(tagPath string) bool {
	parentTag, memberName := t.findParentTagAndMember(tagPath)
	if parentTag == "" || memberName == "" {
		return false
	}

	// Check local cache first
	if ignored, ok := t.ignoredMembers[parentTag][memberName]; ok {
		return ignored
	}

	// Check config
	cfg := t.app.config.FindPLC(t.selectedPLC)
	if cfg == nil {
		return false
	}

	for _, sel := range cfg.Tags {
		if sel.Name == parentTag {
			return sel.ShouldIgnoreMember(memberName)
		}
	}

	return false
}

func (t *BrowserTab) updateNodeText(node *tview.TreeNode, tag *driver.TagInfo, enabled, writable bool) {
	checkbox := GetCheckboxUnchecked()
	if enabled {
		checkbox = GetCheckboxChecked()
	}

	th := CurrentTheme

	// Writable indicator
	writeIndicator := ""
	if writable {
		writeIndicator = th.TagWritable + "W" + th.TagReset + " "
	}

	// Ignore indicator (for UDT members)
	ignoreIndicator := ""
	if t.isMemberIgnored(tag.Name) {
		ignoreIndicator = th.TagError + "I" + th.TagReset + " "
	}

	// UDT expandable indicator
	udtIndicator := ""
	if logix.IsStructure(tag.TypeCode) {
		udtIndicator = th.TagAccent + GetTreeCollapsed() + th.TagReset
	}

	typeName := t.getTypeName(tag.TypeCode)
	shortName := tag.Name
	if idx := strings.LastIndex(tag.Name, "."); idx >= 0 {
		shortName = tag.Name[idx+1:]
	}

	// For address-based PLCs (S7, Omron FINS), show alias as primary name if set, with address in gray
	cfg := t.app.config.FindPLC(t.selectedPLC)
	if cfg != nil {
		family := cfg.GetFamily()
		if family == config.FamilyS7 || (family == config.FamilyOmron && cfg.IsOmronFINS()) {
			// Look up the alias for this tag
			for _, sel := range cfg.Tags {
				if sel.Name == tag.Name && sel.Alias != "" {
					// Show: checkbox [indicators] Alias  (address) type
					shortName = sel.Alias
					typeName = fmt.Sprintf("(%s) %s", tag.Name, typeName)
					break
				}
			}
		}
	}

	var text string
	if enabled {
		// Bold text for enabled items using [::b] inline formatting
		text = fmt.Sprintf("[::b]%s %s%s%s%s[::-]  %s%s%s", checkbox, udtIndicator, ignoreIndicator, writeIndicator, shortName, th.TagTextDim, typeName, th.TagReset)
		node.SetColor(th.Secondary) // Theme secondary color for publishing items
		// When selected/hovered: SelectedText on success color background
		node.SetSelectedTextStyle(tcell.StyleDefault.Foreground(th.SelectedText).Background(th.Success).Bold(true))
	} else {
		// Don't use inline color tags - let node.SetColor handle it
		// This allows proper color inversion when the node is selected
		text = fmt.Sprintf("%s %s%s%s%s  %s", checkbox, udtIndicator, ignoreIndicator, writeIndicator, shortName, typeName)
		node.SetColor(th.Text) // Theme text color for non-publishing items
		// When selected/hovered: SelectedText on dim background
		node.SetSelectedTextStyle(tcell.StyleDefault.Foreground(th.SelectedText).Background(th.TextDim))
	}
	node.SetText(text)
}

func (t *BrowserTab) updateConfigTag(tagName string, enabled, writable bool) {
	plc := t.app.config.FindPLC(t.selectedPLC)
	if plc == nil {
		return
	}

	// Find or create tag selection
	found := false
	for i := range plc.Tags {
		if plc.Tags[i].Name == tagName {
			plc.Tags[i].Enabled = enabled
			plc.Tags[i].Writable = writable
			found = true
			break
		}
	}

	if !found && (enabled || writable) {
		plc.Tags = append(plc.Tags, config.TagSelection{
			Name:     tagName,
			Enabled:  enabled,
			Writable: writable,
		})
	}

	t.app.SaveConfig()
}

func (t *BrowserTab) showWriteDialog(node *tview.TreeNode) {
	ref := node.GetReference()
	if ref == nil {
		return
	}

	tagInfo, ok := ref.(*driver.TagInfo)
	if !ok {
		return
	}

	tagName := tagInfo.Name

	// Check if tag is writable
	if !t.writableTags[tagName] {
		t.app.showError("Not Writable", "Tag must be marked writable first.\nPress 'w' to toggle writable flag.")
		return
	}

	// Get current value
	var currentValue string
	plc := t.app.manager.GetPLC(t.selectedPLC)
	if plc != nil {
		values := plc.GetValues()
		if val, ok := values[tagName]; ok && val != nil && val.Error == nil {
			currentValue = formatValue(val.GoValue())
		}
	}

	th := CurrentTheme
	pageName := "write-dialog"

	// Create write form
	form := tview.NewForm()
	ApplyFormTheme(form)
	form.SetBorder(true)
	form.SetTitle(fmt.Sprintf(" Write: %s ", tagName))
	form.SetTitleColor(th.Accent)
	form.SetBorderColor(th.Border)

	// Show current value as label, input for new value
	form.AddInputField("Current:", currentValue, 30, nil, nil)
	form.GetFormItemByLabel("Current:").(*tview.InputField).SetDisabled(true)
	form.AddInputField("New Value:", "", 30, nil, nil)

	closeDialog := func() {
		t.app.pages.RemovePage(pageName)
		t.app.pages.SwitchToPage("main")
		t.app.app.SetFocus(t.tree)
	}

	form.AddButton("Write", func() {
		newValue := form.GetFormItemByLabel("New Value:").(*tview.InputField).GetText()
		if newValue == "" {
			return
		}

		// Parse value - default to int64 for FINS WORD types
		var writeValue interface{}
		var parseErr error

		// Try parsing as integer first (handles hex with 0x prefix)
		var v int64
		v, parseErr = strconv.ParseInt(newValue, 0, 64)
		if parseErr != nil {
			// Try float
			var f float64
			f, parseErr = strconv.ParseFloat(newValue, 64)
			if parseErr != nil {
				t.app.setStatus(fmt.Sprintf("Invalid value: %s", newValue))
				return
			}
			writeValue = f
		} else {
			writeValue = v
		}

		// Capture values for goroutine before closing dialog
		plcName := t.selectedPLC
		writeVal := writeValue
		tagN := tagName
		app := t.app.app // Capture tview.Application for Draw()

		// Close dialog synchronously (safe - we're on main UI thread)
		t.app.pages.RemovePage(pageName)
		t.app.pages.SwitchToPage("main")
		t.app.app.SetFocus(t.tree)
		t.app.setStatus(fmt.Sprintf("Writing %v to %s...", writeVal, tagN))

		// Force a draw to ensure dialog is closed before goroutine runs
		app.Draw()

		// Perform write in background goroutine to not block UI
		go func() {
			// Perform the actual write (this may block on network I/O)
			err := t.app.manager.WriteTag(plcName, tagN, writeVal)

			// Update status - use Go channel pattern to avoid QueueUpdateDraw deadlock
			// The Draw() call is safe from goroutines
			if err != nil {
				t.app.setStatus(fmt.Sprintf("Write failed: %v", err))
			} else {
				t.app.setStatus(fmt.Sprintf("Wrote %v to %s", writeVal, tagN))
			}
			app.Draw()
		}()
	})

	form.AddButton("Cancel", closeDialog)
	form.SetCancelFunc(closeDialog)

	// Center the form in a modal
	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(form, 9, 1, true).
			AddItem(nil, 0, 1, false), 45, 1, true).
		AddItem(nil, 0, 1, false)

	t.app.pages.AddPage(pageName, modal, true, true)
	t.app.app.SetFocus(form)
}

func (t *BrowserTab) showTagDetails(tag *driver.TagInfo) {
	th := CurrentTheme
	var sb strings.Builder

	sb.WriteString(th.Label("Name", tag.Name) + "\n")
	sb.WriteString(th.Label("Type", t.getTypeName(tag.TypeCode)) + "\n")

	// Get current value if available
	plc := t.app.manager.GetPLC(t.selectedPLC)
	if plc != nil {
		values := plc.GetValues()
		if val, ok := values[tag.Name]; ok {
			if val.Error != nil {
				sb.WriteString(th.TagAccent + "Value:" + th.TagError + " " + val.Error.Error() + th.TagReset + "\n")
			} else {
				sb.WriteString(th.Label("Value", formatValue(val.GoValue())) + "\n")
			}
		}
	}

	// Show alias if set
	cfg := t.app.config.FindPLC(t.selectedPLC)
	if cfg != nil {
		for _, sel := range cfg.Tags {
			if sel.Name == tag.Name && sel.Alias != "" {
				sb.WriteString(th.Label("Alias", sel.Alias) + "\n")
				break
			}
		}
	}

	// Dimensions
	if len(tag.Dimensions) > 0 {
		dims := make([]string, len(tag.Dimensions))
		for i, d := range tag.Dimensions {
			dims[i] = fmt.Sprintf("%d", d)
		}
		sb.WriteString(th.Label("Dimensions", "["+strings.Join(dims, ",")+"]") + "\n")
	}

	enabled := t.enabledTags[tag.Name]
	writable := t.writableTags[tag.Name]
	if enabled {
		// Get enabled services for this tag
		services := []string{"REST", "MQTT", "Kafka", "Valkey"} // default all
		if cfg != nil {
			for _, sel := range cfg.Tags {
				if sel.Name == tag.Name {
					services = sel.GetEnabledServices()
					break
				}
			}
		}
		if len(services) == 0 {
			sb.WriteString("\n" + th.Dim(GetCheckboxChecked()+" Publishing disabled (no services)"))
		} else if len(services) == 4 {
			sb.WriteString("\n" + th.SuccessText(GetCheckboxChecked()+" Publishing to all services"))
		} else {
			sb.WriteString("\n" + th.SuccessText(GetCheckboxChecked()+" Publishing to "+strings.Join(services, ", ")))
		}
	} else {
		sb.WriteString("\n" + th.Dim(GetCheckboxUnchecked()+" Not publishing"))
	}

	if writable {
		sb.WriteString("\n" + th.ErrorText("W Writable"))
	} else {
		sb.WriteString("\n" + th.Dim("  Read-only"))
	}

	// Show ignore list for UDT tags
	if enabled && logix.IsStructure(tag.TypeCode) {
		if cfg != nil {
			for _, sel := range cfg.Tags {
				if sel.Name == tag.Name && len(sel.IgnoreChanges) > 0 {
					sb.WriteString("\n\n" + th.ErrorText("I Ignored for changes:"))
					for _, member := range sel.IgnoreChanges {
						sb.WriteString("\n  - " + member)
					}
					break
				}
			}
		}
	}

	// Show if this member is ignored (for UDT member nodes)
	if t.isMemberIgnored(tag.Name) {
		sb.WriteString("\n" + th.ErrorText("I Ignored for change detection"))
	}

	sb.WriteString("\n\n" + th.TagPrimary + "Space" + th.TagText + " toggle  " +
		th.TagPrimary + "w" + th.TagText + " writable  " +
		th.TagPrimary + "i" + th.TagText + " ignore  " +
		th.TagPrimary + "d" + th.TagText + " details" + th.TagReset)

	t.details.SetText(sb.String())
}

func (t *BrowserTab) showDetailedTagInfo(node *tview.TreeNode) {
	ref := node.GetReference()
	if ref == nil {
		return
	}

	tagInfo, ok := ref.(*driver.TagInfo)
	if !ok {
		return
	}

	th := CurrentTheme
	// Bold accent tag: insert ::b before the closing ]
	boldAccent := th.TagAccent[:len(th.TagAccent)-1] + "::b]"

	// Show immediate feedback
	var sb strings.Builder
	sb.WriteString(boldAccent + "Tag Information[-::-]\n")
	sb.WriteString("─────────────────────────────\n")
	sb.WriteString(th.Label("Name", tagInfo.Name) + "\n")
	sb.WriteString(fmt.Sprintf("%sType:%s %s (0x%04X)\n", th.TagAccent, th.TagReset, t.getTypeName(tagInfo.TypeCode), tagInfo.TypeCode))
	sb.WriteString(fmt.Sprintf("%sInstance:%s %d\n", th.TagAccent, th.TagReset, tagInfo.Instance))

	// Dimensions
	if len(tagInfo.Dimensions) > 0 {
		dims := make([]string, len(tagInfo.Dimensions))
		for i, d := range tagInfo.Dimensions {
			dims[i] = fmt.Sprintf("%d", d)
		}
		sb.WriteString(fmt.Sprintf("%sDimensions:%s [%s]\n", th.TagAccent, th.TagReset, strings.Join(dims, ", ")))
	} else {
		sb.WriteString(fmt.Sprintf("%sDimensions:%s scalar\n", th.TagAccent, th.TagReset))
	}


	sb.WriteString("\n" + boldAccent + "Live Value[-::-]\n")
	sb.WriteString("─────────────────────────────\n")
	sb.WriteString(th.Dim("Reading from PLC...") + "\n")
	t.details.SetText(sb.String())

	// Read from PLC in background goroutine
	plcName := t.selectedPLC
	tagName := tagInfo.Name
	tagTypeCode := tagInfo.TypeCode
	tagInstance := tagInfo.Instance
	tagDimensions := tagInfo.Dimensions

	// Get PLC family for type-specific handling
	var plcFamily config.PLCFamily
	if cfg := t.app.config.FindPLC(t.selectedPLC); cfg != nil {
		plcFamily = cfg.GetFamily()
	}

	go func() {
		th := CurrentTheme
		boldAccent := th.TagAccent[:len(th.TagAccent)-1] + "::b]"
		plc := t.app.manager.GetPLC(plcName)
		if plc == nil {
			t.app.QueueUpdateDraw(func() {
				t.details.SetText(sb.String() + "\n" + th.ErrorText("PLC not available") + "\n")
			})
			return
		}

		// For array types, show array info (dimensions come from tag list discovery)
		// Use family-specific type functions
		var arrayDebugInfo string
		var isArrayType, isStruct bool
		var baseType uint16
		var baseTypeName string
		var elemSize uint32

		switch plcFamily {
		case config.FamilyOmron:
			isArrayType = omron.IsArray(tagTypeCode)
			isStruct = false // Omron FINS doesn't have structured types like Logix
			baseType = omron.BaseType(tagTypeCode)
			baseTypeName = omron.TypeName(baseType)
			elemSize = uint32(omron.TypeSize(baseType))
		case config.FamilyS7:
			isArrayType = s7.IsArray(tagTypeCode)
			isStruct = false
			baseType = s7.BaseType(tagTypeCode)
			baseTypeName = s7.TypeName(baseType)
			elemSize = uint32(s7.TypeSize(baseType))
		case config.FamilyBeckhoff:
			isArrayType = ads.IsArray(tagTypeCode)
			isStruct = false
			baseType = ads.BaseType(tagTypeCode)
			baseTypeName = ads.TypeName(baseType)
			elemSize = uint32(ads.TypeSize(baseType))
		default:
			isArrayType = logix.IsArrayType(tagTypeCode)
			isStruct = logix.IsStructure(tagTypeCode)
			baseType = logix.BaseType(tagTypeCode)
			baseTypeName = logix.TypeName(baseType)
			if client := plc.GetLogixClient(); client != nil {
				elemSize = client.GetElementSize(tagTypeCode)
			} else {
				elemSize = uint32(logix.TypeSize(baseType))
			}
		}

		if isArrayType || isStruct {
			var debugSb strings.Builder
			if isArrayType {
				debugSb.WriteString("\n" + boldAccent + "Array Info[-::-]\n")
			} else {
				debugSb.WriteString("\n" + boldAccent + "Type Info[-::-]\n")
			}
			debugSb.WriteString("─────────────────────────────\n")
			debugSb.WriteString(fmt.Sprintf("%sBase Type:%s %s\n", th.TagAccent, th.TagReset, baseTypeName))

			// For Logix structures, show template ID
			if isStruct && (plcFamily == config.FamilyLogix || plcFamily == config.FamilyMicro800) {
				templateID := logix.TemplateID(tagTypeCode)
				debugSb.WriteString(fmt.Sprintf("%sTemplate ID:%s %d (0x%04X)\n", th.TagAccent, th.TagReset, templateID, templateID))
			}

			debugSb.WriteString(fmt.Sprintf("%sElement Size:%s %d bytes\n", th.TagAccent, th.TagReset, elemSize))
			arrayDebugInfo = debugSb.String()
		}

		// Calculate element count from dimensions
		var elemCount uint16 = 1
		for _, d := range tagDimensions {
			if d > 0 {
				elemCount *= uint16(d)
			}
		}

		// Try to read the tag directly, using count for arrays
		var val *plcman.TagValue
		var err error
		if elemCount > 1 {
			val, err = t.app.manager.ReadTagWithCount(plcName, tagName, elemCount)
		} else {
			val, err = t.app.manager.ReadTag(plcName, tagName)
		}

		t.app.QueueUpdateDraw(func() {
			var result strings.Builder

			// Rebuild header
			result.WriteString(boldAccent + "Tag Information[-::-]\n")
			result.WriteString("─────────────────────────────\n")
			result.WriteString(th.Label("Name", tagName) + "\n")
			result.WriteString(fmt.Sprintf("%sType:%s %s (0x%04X)\n", th.TagAccent, th.TagReset, t.getTypeName(tagTypeCode), tagTypeCode))
			result.WriteString(fmt.Sprintf("%sInstance:%s %d\n", th.TagAccent, th.TagReset, tagInstance))

			if len(tagDimensions) > 0 {
				dims := make([]string, len(tagDimensions))
				for i, d := range tagDimensions {
					dims[i] = fmt.Sprintf("%d", d)
				}
				result.WriteString(fmt.Sprintf("%sDimensions:%s [%s]\n", th.TagAccent, th.TagReset, strings.Join(dims, ", ")))
			} else {
				result.WriteString(fmt.Sprintf("%sDimensions:%s scalar\n", th.TagAccent, th.TagReset))
			}

			// Add array debug info if available
			if arrayDebugInfo != "" {
				result.WriteString(arrayDebugInfo)
			}

			result.WriteString("\n" + boldAccent + "Live Value[-::-]\n")
			result.WriteString("─────────────────────────────\n")

			if err != nil {
				result.WriteString(fmt.Sprintf("%sRead error:%s %v\n", th.TagError, th.TagReset, err))
				t.details.SetText(result.String())
				return
			}

			if val == nil {
				result.WriteString(th.Dim("No value returned") + "\n")
				t.details.SetText(result.String())
				return
			}

			if val.Error != nil {
				result.WriteString(fmt.Sprintf("%sTag error:%s %v\n", th.TagError, th.TagReset, val.Error))
				t.details.SetText(result.String())
				return
			}

			// Display value
			result.WriteString(th.Label("Value", formatValue(val.GoValue())) + "\n")
			result.WriteString(fmt.Sprintf("%sData Type:%s %s (0x%04X)\n", th.TagAccent, th.TagReset, t.getTypeName(val.DataType), val.DataType))
			result.WriteString(fmt.Sprintf("%sSize:%s %d bytes\n", th.TagAccent, th.TagReset, len(val.Bytes)))

			// Raw bytes hex dump
			result.WriteString("\n" + boldAccent + "Raw Bytes[-::-]\n")
			result.WriteString("─────────────────────────────\n")

			if len(val.Bytes) > 0 {
				// Hex dump with offset
				for i := 0; i < len(val.Bytes); i += 16 {
					// Offset
					result.WriteString(fmt.Sprintf("%s%04X:%s ", th.TagTextDim, i, th.TagReset))

					// Hex bytes
					for j := 0; j < 16; j++ {
						if i+j < len(val.Bytes) {
							result.WriteString(fmt.Sprintf("%02X ", val.Bytes[i+j]))
						} else {
							result.WriteString("   ")
						}
						if j == 7 {
							result.WriteString(" ")
						}
					}

					// ASCII representation
					result.WriteString(" " + th.TagTextDim + "|")
					for j := 0; j < 16 && i+j < len(val.Bytes); j++ {
						b := val.Bytes[i+j]
						if b >= 32 && b < 127 {
							result.WriteString(string(b))
						} else {
							result.WriteString(".")
						}
					}
					result.WriteString("|" + th.TagReset + "\n")

					// Limit display to prevent huge outputs
					if i >= 256 {
						result.WriteString(th.Dim(fmt.Sprintf("... (%d more bytes)", len(val.Bytes)-i-16)) + "\n")
						break
					}
				}
			} else {
				result.WriteString(th.Dim("No data") + "\n")
			}

			t.details.SetText(result.String())
		})
	}()
}

func (t *BrowserTab) updateStatus() {
	th := CurrentTheme
	count := 0
	for _, enabled := range t.enabledTags {
		if enabled {
			count++
		}
	}

	// Check PLC connection status
	statusPrefix := ""
	if t.selectedPLC != "" {
		plc := t.app.manager.GetPLC(t.selectedPLC)
		if plc == nil || plc.GetStatus() != plcman.StatusConnected {
			statusPrefix = th.ErrorText("OFFLINE") + " | "
		}
	}

	t.statusBar.SetText(fmt.Sprintf(" %s%d tags selected for publishing", statusPrefix, count))
}

func (t *BrowserTab) updateButtonBar() {
	th := CurrentTheme
	buttonText := " " + th.TagHotkey + "/" + th.TagActionText + " filter  " +
		th.TagHotkey + "c" + th.TagActionText + "lear  " +
		th.TagHotkey + "p" + th.TagActionText + "lc  " +
		th.TagHotkey + "Space" + th.TagActionText + " toggle  " +
		th.TagHotkey + "s" + th.TagActionText + "ervices  " +
		th.TagHotkey + "w" + th.TagActionText + "ritable  " +
		th.TagHotkey + "i" + th.TagActionText + "gnore  " +
		th.TagHotkey + "d" + th.TagActionText + "etails"

	// Add manual tag keys for non-discovery PLCs
	if t.isManualPLC() {
		buttonText += "  " + th.TagHotkey + "a" + th.TagActionText + "dd  " +
			th.TagHotkey + "e" + th.TagActionText + "dit  " +
			th.TagHotkey + "x" + th.TagActionText + " delete"
	}

	buttonText += "  " + th.TagActionText + "│  " +
		th.TagHotkey + "?" + th.TagActionText + " help " + th.TagReset
	t.buttonBar.SetText(buttonText)
}

// GetPrimitive returns the main primitive for this tab.
func (t *BrowserTab) GetPrimitive() tview.Primitive {
	return t.flex
}

// GetFocusable returns the element that should receive focus.
func (t *BrowserTab) GetFocusable() tview.Primitive {
	return t.tree
}

// RefreshTheme updates theme-dependent UI elements.
func (t *BrowserTab) RefreshTheme() {
	t.updateButtonBar()
	t.updateStatus()
	th := CurrentTheme
	t.treeFrame.SetBorderColor(th.Border).SetTitleColor(th.Accent)
	t.details.SetBorderColor(th.Border).SetTitleColor(th.Accent)
	t.details.SetTextColor(th.Text)
	t.statusBar.SetTextColor(th.Text)
	// Update form field colors
	ApplyDropDownTheme(t.plcSelect)
	ApplyInputFieldTheme(t.filter)
	ApplyTreeViewTheme(t.tree)
	// Update tree root color
	t.treeRoot.SetColor(th.Accent).SetSelectedTextStyle(tcell.StyleDefault.Foreground(th.SelectedText).Background(th.Accent))
	t.loadTags() // Refresh tree nodes with new theme colors
}

// Refresh updates the PLC dropdown and reloads tags.
func (t *BrowserTab) Refresh() {
	// Update PLC dropdown
	plcs := t.app.manager.ListPLCs()

	// Sort PLCs by name for consistent ordering
	sort.Slice(plcs, func(i, j int) bool {
		return plcs[i].Config.Name < plcs[j].Config.Name
	})

	options := make([]string, 0)
	selectedIdx := -1
	var selectedPLCStatus plcman.ConnectionStatus

	for _, plc := range plcs {
		// Show all configured PLCs in the dropdown
		if plc.Config.Name == t.selectedPLC {
			selectedIdx = len(options)
			selectedPLCStatus = plc.Status
		}
		options = append(options, plc.Config.Name)
	}

	// Check if the selected PLC's connection status changed to connected
	// This triggers a tag reload when PLC connects
	if t.selectedPLC != "" && selectedIdx >= 0 {
		if selectedPLCStatus == plcman.StatusConnected && t.lastConnectionStatus != plcman.StatusConnected {
			t.lastConnectionStatus = selectedPLCStatus
			t.loadTags()
			return
		}
		t.lastConnectionStatus = selectedPLCStatus
	}

	// Check if options have changed before updating
	optionsChanged := !stringSlicesEqual(t.lastPLCOptions, options)

	if optionsChanged {
		// Options changed, need to update dropdown
		t.lastPLCOptions = options

		// Mark that we're doing a programmatic update to avoid focus changes
		t.updatingDropdown = true

		// Use the callback version of SetOptions to preserve selection behavior
		t.plcSelect.SetOptions(options, func(text string, index int) {
			t.selectedPLC = text
			t.lastConnectionStatus = 0 // Reset so next Refresh detects status
			t.loadTags()
			t.updateButtonBar() // Update button bar based on PLC type
			// Only change focus if this was a user selection, not programmatic
			if !t.updatingDropdown {
				t.app.app.SetFocus(t.tree)
			}
		})

		if selectedIdx >= 0 {
			t.plcSelect.SetCurrentOption(selectedIdx)
		} else if len(options) > 0 && t.selectedPLC == "" {
			// Only auto-select if nothing was selected before
			t.plcSelect.SetCurrentOption(0)
			t.selectedPLC = options[0]
			t.lastConnectionStatus = 0 // Reset so next Refresh detects status
			t.loadTags()
			t.updateButtonBar() // Update button bar based on PLC type
		} else if len(options) == 0 {
			t.selectedPLC = ""
			t.lastConnectionStatus = 0
			t.clearTree()
		}

		t.updatingDropdown = false
	}
	// If options haven't changed, don't touch the dropdown at all
}

// stringSlicesEqual compares two string slices for equality.
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (t *BrowserTab) clearTree() {
	t.treeRoot.ClearChildren()
	t.tagNodes = make(map[string]*tview.TreeNode)
	t.enabledTags = make(map[string]bool)
	t.writableTags = make(map[string]bool)
	t.ignoredMembers = make(map[string]map[string]bool)
	t.details.SetText("")
	t.statusBar.SetText(" No PLC selected")
}

func (t *BrowserTab) loadTags() {
	// Set current node to root before clearing to prevent tview from
	// having a dangling reference to a destroyed node (which causes cursor jump)
	t.tree.SetCurrentNode(t.treeRoot)
	t.treeRoot.ClearChildren()
	t.tagNodes = make(map[string]*tview.TreeNode)
	t.enabledTags = make(map[string]bool)
	t.writableTags = make(map[string]bool)
	t.ignoredMembers = make(map[string]map[string]bool)

	if t.selectedPLC == "" {
		return
	}

	cfg := t.app.config.FindPLC(t.selectedPLC)
	if cfg == nil {
		return
	}

	isManual := !cfg.GetFamily().SupportsDiscovery()

	plc := t.app.manager.GetPLC(t.selectedPLC)

	var tags []driver.TagInfo
	var programs []string
	var values map[string]*plcman.TagValue

	if plc != nil {
		tags = plc.GetTags()
		programs = plc.GetPrograms()
		values = plc.GetValues()
	} else {
		values = make(map[string]*plcman.TagValue)
	}

	// For manual PLCs not yet connected, build tags from config using proper family parsing
	if isManual && len(tags) == 0 && len(cfg.Tags) > 0 {
		family := cfg.GetFamily()
		for _, sel := range cfg.Tags {
			var typeCode uint16
			var typeName string
			var dimensions []uint32
			var ok bool

			switch family {
			case config.FamilyS7:
				typeCode, ok = s7.TypeCodeFromName(sel.DataType)
				if !ok {
					typeCode = s7.TypeDInt
				}
				typeName = s7.TypeName(typeCode)
				if parsed, err := s7.ParseAddress(sel.Name); err == nil && parsed.Count > 1 {
					dimensions = []uint32{uint32(parsed.Count)}
					typeCode = s7.MakeArrayType(typeCode)
				}
			case config.FamilyOmron:
				typeCode, ok = omron.TypeCodeFromName(sel.DataType)
				if !ok {
					typeCode = omron.TypeWord
				}
				typeName = omron.TypeName(typeCode)
				if parsed, err := omron.ParseAddress(sel.Name); err == nil && parsed.Count > 1 {
					dimensions = []uint32{uint32(parsed.Count)}
					typeCode = omron.MakeArrayType(typeCode)
				}
			case config.FamilyBeckhoff:
				typeCode, ok = ads.TypeCodeFromName(sel.DataType)
				if !ok {
					typeCode = ads.TypeInt32
				}
				typeName = ads.TypeName(typeCode)
			default:
				typeCode, ok = logix.TypeCodeFromName(sel.DataType)
				if !ok {
					typeCode = logix.TypeDINT
				}
				typeName = logix.TypeName(typeCode)
			}

			tags = append(tags, driver.TagInfo{
				Name:       sel.Name,
				TypeCode:   typeCode,
				TypeName:   typeName,
				Dimensions: dimensions,
			})
		}
	}

	// Load enabled, writable, and ignore lists from config
	if cfg != nil {
		for _, sel := range cfg.Tags {
			t.enabledTags[sel.Name] = sel.Enabled
			t.writableTags[sel.Name] = sel.Writable
			// Load ignore list for this tag
			if len(sel.IgnoreChanges) > 0 {
				t.ignoredMembers[sel.Name] = make(map[string]bool)
				for _, member := range sel.IgnoreChanges {
					t.ignoredMembers[sel.Name][member] = true
				}
			}
		}
	}

	th := CurrentTheme

	// Show offline notice if PLC is not connected
	isOffline := plc == nil || plc.GetStatus() != plcman.StatusConnected
	if isOffline {
		offlineNode := tview.NewTreeNode(th.ErrorText("PLC OFFLINE") + " - Tags can still be configured").
			SetColor(th.Error).
			SetSelectable(false)
		t.treeRoot.AddChild(offlineNode)
	}

	// For manual PLCs, show a different tree structure
	if isManual {
		sectionName := "Manual Tags"
		sectionNode := tview.NewTreeNode(sectionName).
			SetColor(th.Accent).
			SetExpanded(true).
			SetSelectedTextStyle(tcell.StyleDefault.Foreground(th.SelectedText).Background(th.Accent))

		// Sort tags by name
		sort.Slice(tags, func(i, j int) bool {
			return tags[i].Name < tags[j].Name
		})

		for i := range tags {
			tag := &tags[i]
			// Skip tags that don't match filter
			if !t.matchesFilter(tag.Name) {
				continue
			}
			enabled := t.enabledTags[tag.Name]
			writable := t.writableTags[tag.Name]
			// Check for error
			var hasError bool
			if val, ok := values[tag.Name]; ok && val != nil && val.Error != nil {
				hasError = true
			}
			node := t.createTagNodeWithError(tag, enabled, writable, hasError)
			sectionNode.AddChild(node)
			t.tagNodes[tag.Name] = node
		}

		// Only add section node if it has matching tags
		if len(sectionNode.GetChildren()) > 0 {
			t.treeRoot.AddChild(sectionNode)
		}

		t.updateStatus()
		return
	}

	// Discovery-based PLCs: organize tags by program
	controllerTags := []driver.TagInfo{}
	programTags := make(map[string][]driver.TagInfo)

	// Build a set of known program prefixes for Beckhoff-style grouping
	programPrefixes := make(map[string]bool)
	for _, prog := range programs {
		programPrefixes[prog+"."] = true
	}

	for _, tag := range tags {
		if strings.HasPrefix(tag.Name, "Program:") {
			// Logix-style: "Program:MainProgram.tagname"
			rest := strings.TrimPrefix(tag.Name, "Program:")
			if idx := strings.Index(rest, "."); idx > 0 {
				progName := rest[:idx]
				programTags[progName] = append(programTags[progName], tag)
			}
		} else {
			// Check for Beckhoff-style: "MAIN.tagname", "GVL.globalvar"
			matched := false
			for prefix := range programPrefixes {
				if strings.HasPrefix(tag.Name, prefix) {
					progName := strings.TrimSuffix(prefix, ".")
					programTags[progName] = append(programTags[progName], tag)
					matched = true
					break
				}
			}
			if !matched {
				controllerTags = append(controllerTags, tag)
			}
		}
	}

	// Sort controller tags
	sort.Slice(controllerTags, func(i, j int) bool {
		return controllerTags[i].Name < controllerTags[j].Name
	})

	// Add controller tags
	if len(controllerTags) > 0 {
		controllerNode := tview.NewTreeNode("Controller").
			SetColor(th.Accent).
			SetExpanded(true).
			SetSelectedTextStyle(tcell.StyleDefault.Foreground(th.SelectedText).Background(th.Accent))

		for i := range controllerTags {
			tag := &controllerTags[i]
			// Skip tags that don't match filter
			if !t.matchesFilter(tag.Name) {
				continue
			}
			enabled := t.enabledTags[tag.Name]
			writable := t.writableTags[tag.Name]
			node := t.createTagNode(tag, enabled, writable)
			controllerNode.AddChild(node)
			t.tagNodes[tag.Name] = node
		}

		// Only add section node if it has matching tags
		if len(controllerNode.GetChildren()) > 0 {
			t.treeRoot.AddChild(controllerNode)
		}
	}

	// Sort programs
	sort.Strings(programs)

	// Add program tags
	for _, prog := range programs {
		tags := programTags[prog]
		if len(tags) == 0 {
			continue
		}

		sort.Slice(tags, func(i, j int) bool {
			return tags[i].Name < tags[j].Name
		})

		progNode := tview.NewTreeNode(prog).
			SetColor(th.Accent).
			SetExpanded(true).
			SetSelectedTextStyle(tcell.StyleDefault.Foreground(th.SelectedText).Background(th.Accent))

		for i := range tags {
			tag := &tags[i]
			// Skip tags that don't match filter
			if !t.matchesFilter(tag.Name) {
				continue
			}
			enabled := t.enabledTags[tag.Name]
			writable := t.writableTags[tag.Name]
			node := t.createTagNode(tag, enabled, writable)
			progNode.AddChild(node)
			t.tagNodes[tag.Name] = node
		}

		// Only add section node if it has matching tags
		if len(progNode.GetChildren()) > 0 {
			t.treeRoot.AddChild(progNode)
		}
	}

	t.updateStatus()
}

func (t *BrowserTab) createTagNode(tag *driver.TagInfo, enabled, writable bool) *tview.TreeNode {
	return t.createTagNodeWithError(tag, enabled, writable, false)
}

func (t *BrowserTab) createTagNodeWithError(tag *driver.TagInfo, enabled, writable, hasError bool) *tview.TreeNode {
	th := CurrentTheme
	checkbox := GetCheckboxUnchecked()
	if enabled {
		checkbox = GetCheckboxChecked()
	}

	// Writable indicator
	writeIndicator := ""
	if writable {
		writeIndicator = th.TagWritable + "W" + th.TagReset + " "
	}

	// Ignore indicator (for UDT members)
	ignoreIndicator := ""
	if t.isMemberIgnored(tag.Name) {
		ignoreIndicator = th.TagError + "I" + th.TagReset + " "
	}

	// Error indicator
	errorIndicator := ""
	if hasError {
		errorIndicator = th.TagError + "!" + th.TagReset + " "
	}

	// UDT expandable indicator
	udtIndicator := ""
	if logix.IsStructure(tag.TypeCode) {
		udtIndicator = th.TagAccent + GetTreeCollapsed() + th.TagReset
	}

	typeName := t.getTypeName(tag.TypeCode)
	shortName := tag.Name
	if idx := strings.LastIndex(tag.Name, "."); idx >= 0 {
		shortName = tag.Name[idx+1:]
	}

	// For address-based PLCs (S7, Omron FINS), show alias as primary name if set, with address in gray
	cfg := t.app.config.FindPLC(t.selectedPLC)
	if cfg != nil {
		family := cfg.GetFamily()
		if family == config.FamilyS7 || (family == config.FamilyOmron && cfg.IsOmronFINS()) {
			// Look up the alias for this tag
			for _, sel := range cfg.Tags {
				if sel.Name == tag.Name && sel.Alias != "" {
					// Show: checkbox [indicators] Alias  (address) type
					shortName = sel.Alias
					typeName = fmt.Sprintf("(%s) %s", tag.Name, typeName)
					break
				}
			}
		}
	}

	var text string
	if enabled {
		// Bold text for enabled items using [::b] inline formatting
		text = fmt.Sprintf("[::b]%s %s%s%s%s%s[::-]  %s%s%s", checkbox, udtIndicator, ignoreIndicator, errorIndicator, writeIndicator, shortName, th.TagTextDim, typeName, th.TagReset)
	} else {
		// Don't use inline color tags - let node.SetColor handle it
		// This allows proper color inversion when the node is selected
		text = fmt.Sprintf("%s %s%s%s%s%s  %s", checkbox, udtIndicator, ignoreIndicator, errorIndicator, writeIndicator, shortName, typeName)
	}

	node := tview.NewTreeNode(text).
		SetReference(tag).
		SetSelectable(true)

	if enabled {
		node.SetColor(th.Secondary) // Theme secondary color for publishing items
		// When selected/hovered: SelectedText on success color background
		node.SetSelectedTextStyle(tcell.StyleDefault.Foreground(th.SelectedText).Background(th.Success).Bold(true))
	} else {
		node.SetColor(th.Text) // Theme text color for non-publishing items
		// When selected/hovered: SelectedText on dim background
		node.SetSelectedTextStyle(tcell.StyleDefault.Foreground(th.SelectedText).Background(th.TextDim))
	}

	return node
}

func (t *BrowserTab) applyFilter(filterText string) {
	t.filterText = strings.ToLower(filterText)
	t.loadTags() // Rebuild tree with filter applied
}

// matchesFilter returns true if the tag name matches the current filter.
func (t *BrowserTab) matchesFilter(tagName string) bool {
	if t.filterText == "" {
		return true
	}
	return strings.Contains(strings.ToLower(tagName), t.filterText)
}

// isManualPLC returns true if the currently selected PLC is a non-discovery type.
func (t *BrowserTab) isManualPLC() bool {
	if t.selectedPLC == "" {
		return false
	}
	cfg := t.app.config.FindPLC(t.selectedPLC)
	if cfg == nil {
		return false
	}
	return !cfg.GetFamily().SupportsDiscovery()
}

// showServicesDialog shows a dialog to configure which services a tag publishes to.
func (t *BrowserTab) showServicesDialog(node *tview.TreeNode) {
	ref := node.GetReference()
	if ref == nil {
		return
	}

	tagInfo, ok := ref.(*driver.TagInfo)
	if !ok {
		return
	}

	tagName := tagInfo.Name

	// Check if tag is enabled for publishing
	if !t.enabledTags[tagName] {
		t.app.setStatus("Enable tag for publishing first (Space)")
		return
	}

	const pageName = "services"

	// Get current settings
	cfg := t.app.config.FindPLC(t.selectedPLC)
	if cfg == nil {
		return
	}

	// Find or initialize tag selection
	var sel *config.TagSelection
	for i := range cfg.Tags {
		if cfg.Tags[i].Name == tagName {
			sel = &cfg.Tags[i]
			break
		}
	}

	// If tag wasn't found in config, it should exist since it's enabled
	if sel == nil {
		t.app.setStatus("Tag configuration not found")
		return
	}

	// Current state (inverted: NoREST means REST is unchecked)
	restEnabled := !sel.NoREST
	mqttEnabled := !sel.NoMQTT
	kafkaEnabled := !sel.NoKafka
	valkeyEnabled := !sel.NoValkey

	form := tview.NewForm()
	ApplyFormTheme(form)

	// Truncate tag name for title if too long
	displayName := tagName
	if len(displayName) > 25 {
		displayName = displayName[:22] + "..."
	}
	form.SetBorder(true).SetTitle(" Services: " + displayName + " ")

	form.AddCheckbox("REST API", restEnabled, func(checked bool) {
		restEnabled = checked
	})
	form.AddCheckbox("MQTT", mqttEnabled, func(checked bool) {
		mqttEnabled = checked
	})
	form.AddCheckbox("Kafka", kafkaEnabled, func(checked bool) {
		kafkaEnabled = checked
	})
	form.AddCheckbox("Valkey", valkeyEnabled, func(checked bool) {
		valkeyEnabled = checked
	})

	form.AddButton("Save", func() {
		// Update config with inverted values
		sel.NoREST = !restEnabled
		sel.NoMQTT = !mqttEnabled
		sel.NoKafka = !kafkaEnabled
		sel.NoValkey = !valkeyEnabled

		t.app.SaveConfig()
		t.app.pages.RemovePage(pageName)
		t.app.app.SetFocus(t.tree)

		// Update details pane
		t.showTagDetails(tagInfo)

		// Build status message
		services := sel.GetEnabledServices()
		if len(services) == 4 {
			t.app.setStatus(fmt.Sprintf("%s: publishing to all services", tagName))
		} else if len(services) == 0 {
			t.app.setStatus(fmt.Sprintf("%s: publishing disabled (no services)", tagName))
		} else {
			t.app.setStatus(fmt.Sprintf("%s: publishing to %s", tagName, strings.Join(services, ", ")))
		}
	})

	form.AddButton("Cancel", func() {
		t.app.pages.RemovePage(pageName)
		t.app.app.SetFocus(t.tree)
	})

	form.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			t.app.pages.RemovePage(pageName)
			t.app.app.SetFocus(t.tree)
			return nil
		}
		return event
	})

	// Center the form
	flex := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(form, 15, 0, true).
			AddItem(nil, 0, 1, false), 45, 0, true).
		AddItem(nil, 0, 1, false)

	t.app.pages.AddPage(pageName, flex, true, true)
	t.app.app.SetFocus(form)
}

// showAddTagDialog shows a dialog to add a manual tag.
func (t *BrowserTab) showAddTagDialog() {
	const pageName = "addtag"

	form := tview.NewForm()
	ApplyFormTheme(form)
	form.SetBorder(true).SetTitle(" Add Manual Tag ")

	// Determine PLC family and use appropriate labels/types
	cfg := t.app.config.FindPLC(t.selectedPLC)
	family := config.FamilyLogix
	if cfg != nil {
		family = cfg.GetFamily()
	}

	// Address-based PLCs (S7, Omron FINS) show Alias first, then Address
	// Tag-based PLCs (Logix, Omron EIP) show Tag Name first, then Alias
	isAddressBased := family == config.FamilyS7 || (family == config.FamilyOmron && cfg.IsOmronFINS())

	var typeOptions []string
	var addressLabel string

	switch family {
	case config.FamilyS7:
		typeOptions = s7.SupportedTypeNames()
		addressLabel = "DB.Offset:"
	case config.FamilyOmron:
		typeOptions = omron.SupportedTypeNames()
		if cfg.IsOmronFINS() {
			addressLabel = "Address:"
		} else {
			addressLabel = "Tag Name:"
		}
	default:
		typeOptions = logix.SupportedTypeNames()
		addressLabel = "Tag Name:"
	}

	if isAddressBased {
		// Address-based: Alias first, then Type, then Offset/Address
		form.AddInputField("Alias:", "", 30, nil, nil)
		form.AddDropDown("Data Type:", typeOptions, 3, nil) // Default to DINT (index 3)
		form.AddInputField(addressLabel, "", 30, nil, nil)
		form.AddCheckbox("Writable:", false, nil)
	} else {
		// Tag-based: Tag Name, Type, Alias
		form.AddInputField(addressLabel, "", 30, nil, nil)
		form.AddDropDown("Data Type:", typeOptions, 3, nil) // Default to DINT (index 3)
		form.AddInputField("Alias:", "", 30, nil, nil)
		form.AddCheckbox("Writable:", false, nil)
	}

	form.AddButton("Add", func() {
		var tagName, alias string
		if isAddressBased {
			alias = form.GetFormItemByLabel("Alias:").(*tview.InputField).GetText()
			tagName = form.GetFormItemByLabel(addressLabel).(*tview.InputField).GetText()
		} else {
			tagName = form.GetFormItemByLabel(addressLabel).(*tview.InputField).GetText()
			alias = form.GetFormItemByLabel("Alias:").(*tview.InputField).GetText()
		}
		typeIdx, _ := form.GetFormItemByLabel("Data Type:").(*tview.DropDown).GetCurrentOption()
		writable := form.GetFormItemByLabel("Writable:").(*tview.Checkbox).IsChecked()

		if tagName == "" {
			t.app.showErrorWithFocus("Error", "Address is required", form)
			return
		}

		// Validate address format based on PLC family
		switch family {
		case config.FamilyS7:
			if err := s7.ValidateAddress(tagName); err != nil {
				t.app.showErrorWithFocus("Invalid Address", err.Error(), form)
				return
			}
		case config.FamilyOmron:
			if cfg.IsOmronFINS() {
				if err := omron.ValidateAddress(tagName); err != nil {
					t.app.showErrorWithFocus("Invalid Address", err.Error()+"\nExpected format: DM100, CIO50, HR10, etc.", form)
					return
				}
			}
		}

		// Check for duplicate
		cfg := t.app.config.FindPLC(t.selectedPLC)
		if cfg != nil {
			for _, tag := range cfg.Tags {
				if tag.Name == tagName {
					t.app.showErrorWithFocus("Error", "Tag already exists: "+tagName, form)
					return
				}
			}

			// Add the tag
			cfg.Tags = append(cfg.Tags, config.TagSelection{
				Name:     tagName,
				DataType: typeOptions[typeIdx],
				Alias:    alias,
				Enabled:  true,
				Writable: writable,
			})

			t.app.SaveConfig()

			// Refresh manual tags in manager
			t.app.manager.RefreshManualTags(t.selectedPLC)
		}

		t.app.closeModal(pageName)
		t.loadTags()
		t.app.setStatus(fmt.Sprintf("Added tag: %s", tagName))
	})

	form.AddButton("Cancel", func() {
		t.app.closeModal(pageName)
	})

	t.app.showFormModal(pageName, form, 50, 14, func() {
		t.app.closeModal(pageName)
	})
}

// showEditTagDialog shows a dialog to edit a manual tag.
func (t *BrowserTab) showEditTagDialog(node *tview.TreeNode) {
	const pageName = "edittag"

	ref := node.GetReference()
	if ref == nil {
		return
	}

	tagInfo, ok := ref.(*driver.TagInfo)
	if !ok {
		return
	}

	// Find the tag in config
	cfg := t.app.config.FindPLC(t.selectedPLC)
	if cfg == nil {
		return
	}

	var tagSel *config.TagSelection
	var tagIdx int
	for i := range cfg.Tags {
		if cfg.Tags[i].Name == tagInfo.Name {
			tagSel = &cfg.Tags[i]
			tagIdx = i
			break
		}
	}

	if tagSel == nil {
		return
	}

	form := tview.NewForm()
	ApplyFormTheme(form)
	form.SetBorder(true).SetTitle(" Edit Manual Tag ")

	// Determine PLC family and use appropriate labels/types
	family := cfg.GetFamily()
	isAddressBased := family == config.FamilyS7 || (family == config.FamilyOmron && cfg.IsOmronFINS())

	var typeOptions []string
	var addressLabel string

	switch family {
	case config.FamilyS7:
		typeOptions = s7.SupportedTypeNames()
		addressLabel = "DB.Offset:"
	case config.FamilyOmron:
		typeOptions = omron.SupportedTypeNames()
		if cfg.IsOmronFINS() {
			addressLabel = "Address:"
		} else {
			addressLabel = "Tag Name:"
		}
	default:
		typeOptions = logix.SupportedTypeNames()
		addressLabel = "Tag Name:"
	}

	selectedType := 3 // Default to DINT
	for i, opt := range typeOptions {
		if opt == tagSel.DataType {
			selectedType = i
			break
		}
	}

	if isAddressBased {
		// Address-based: Alias first, then Type, then Offset/Address
		form.AddInputField("Alias:", tagSel.Alias, 30, nil, nil)
		form.AddDropDown("Data Type:", typeOptions, selectedType, nil)
		form.AddInputField(addressLabel, tagSel.Name, 30, nil, nil)
		form.AddCheckbox("Writable:", tagSel.Writable, nil)
	} else {
		// Tag-based: Tag Name, Type, Alias
		form.AddInputField(addressLabel, tagSel.Name, 30, nil, nil)
		form.AddDropDown("Data Type:", typeOptions, selectedType, nil)
		form.AddInputField("Alias:", tagSel.Alias, 30, nil, nil)
		form.AddCheckbox("Writable:", tagSel.Writable, nil)
	}

	originalName := tagSel.Name

	form.AddButton("Save", func() {
		var tagName, alias string
		if isAddressBased {
			alias = form.GetFormItemByLabel("Alias:").(*tview.InputField).GetText()
			tagName = form.GetFormItemByLabel(addressLabel).(*tview.InputField).GetText()
		} else {
			tagName = form.GetFormItemByLabel(addressLabel).(*tview.InputField).GetText()
			alias = form.GetFormItemByLabel("Alias:").(*tview.InputField).GetText()
		}
		typeIdx, _ := form.GetFormItemByLabel("Data Type:").(*tview.DropDown).GetCurrentOption()
		writable := form.GetFormItemByLabel("Writable:").(*tview.Checkbox).IsChecked()

		if tagName == "" {
			t.app.showErrorWithFocus("Error", "Address is required", form)
			return
		}

		// Validate address format based on PLC family
		switch family {
		case config.FamilyS7:
			if err := s7.ValidateAddress(tagName); err != nil {
				t.app.showErrorWithFocus("Invalid Address", err.Error(), form)
				return
			}
		case config.FamilyOmron:
			if cfg.IsOmronFINS() {
				if err := omron.ValidateAddress(tagName); err != nil {
					t.app.showErrorWithFocus("Invalid Address", err.Error()+"\nExpected format: DM100, CIO50, HR10, etc.", form)
					return
				}
			}
		}

		// Check for duplicate if name changed
		if tagName != originalName {
			for _, tag := range cfg.Tags {
				if tag.Name == tagName {
					t.app.showErrorWithFocus("Error", "Tag already exists: "+tagName, form)
					return
				}
			}
		}

		// Update the tag
		cfg.Tags[tagIdx].Name = tagName
		cfg.Tags[tagIdx].DataType = typeOptions[typeIdx]
		cfg.Tags[tagIdx].Alias = alias
		cfg.Tags[tagIdx].Writable = writable

		t.app.SaveConfig()

		// Refresh manual tags in manager
		t.app.manager.RefreshManualTags(t.selectedPLC)

		t.app.closeModal(pageName)
		t.loadTags()
		t.app.setStatus(fmt.Sprintf("Updated tag: %s", tagName))
	})

	form.AddButton("Cancel", func() {
		t.app.closeModal(pageName)
	})

	t.app.showFormModal(pageName, form, 50, 14, func() {
		t.app.closeModal(pageName)
	})
}

// expandUDTMembers adds child nodes for UDT members to the given parent node.
// Returns the number of members added (including nested members that match filter).
func (t *BrowserTab) expandUDTMembers(parentNode *tview.TreeNode, basePath string, typeCode uint16, client *logix.Client, maxDepth int) int {
	if maxDepth <= 0 || client == nil {
		return 0
	}

	// Get template for this UDT
	tmpl, err := client.GetTemplate(typeCode)
	if err != nil {
		DebugLog("expandUDTMembers: failed to get template for %s (0x%04X): %v", basePath, typeCode, err)
		return 0
	}

	addedCount := 0

	for _, member := range tmpl.Members {
		if member.Hidden || member.Name == "" {
			continue
		}

		memberPath := basePath + "." + member.Name
		memberMatches := t.matchesFilter(memberPath)

		// Create a synthetic TagInfo for this member, including array dimensions
		dims := make([]uint32, len(member.ArrayDims))
		for i, d := range member.ArrayDims {
			dims[i] = uint32(d)
		}
		memberInfo := &driver.TagInfo{
			Name:       memberPath,
			TypeCode:   member.Type,
			Dimensions: dims,
		}

		enabled := t.enabledTags[memberPath]
		writable := t.writableTags[memberPath]
		memberNode := t.createTagNode(memberInfo, enabled, writable)

		// Check if this member is also a nested UDT
		if logix.IsStructure(member.Type) {
			// Recursively expand nested UDT
			nestedCount := t.expandUDTMembers(memberNode, memberPath, member.Type, client, maxDepth-1)
			if memberMatches || nestedCount > 0 {
				parentNode.AddChild(memberNode)
				t.tagNodes[memberPath] = memberNode
				addedCount++
			}
		} else if memberMatches {
			parentNode.AddChild(memberNode)
			t.tagNodes[memberPath] = memberNode
			addedCount++
		}
	}

	// Collapse UDT nodes by default (user can expand them)
	if addedCount > 0 {
		parentNode.SetExpanded(false)
	}

	return addedCount
}

// deleteManualTag deletes a manual tag after confirmation.
func (t *BrowserTab) deleteManualTag(node *tview.TreeNode) {
	ref := node.GetReference()
	if ref == nil {
		return
	}

	tagInfo, ok := ref.(*driver.TagInfo)
	if !ok {
		return
	}

	tagName := tagInfo.Name

	t.app.showConfirm("Delete Tag", fmt.Sprintf("Delete tag %s?", tagName), func() {
		cfg := t.app.config.FindPLC(t.selectedPLC)
		if cfg != nil {
			// Remove from config
			for i, tag := range cfg.Tags {
				if tag.Name == tagName {
					cfg.Tags = append(cfg.Tags[:i], cfg.Tags[i+1:]...)
					break
				}
			}
			t.app.SaveConfig()

			// Refresh manual tags in manager
			t.app.manager.RefreshManualTags(t.selectedPLC)
		}

		t.loadTags()
		t.app.setStatus(fmt.Sprintf("Deleted tag: %s", tagName))
	})
}

// formatValue formats a value for display, handling maps (UDTs) specially.
func formatValue(v interface{}) string {
	if v == nil {
		return "<nil>"
	}

	switch val := v.(type) {
	case map[string]interface{}:
		return formatMapValue(val, 0)
	case []interface{}:
		if len(val) == 0 {
			return "[]"
		}
		// For arrays, show first few elements
		var sb strings.Builder
		sb.WriteString("[")
		for i, elem := range val {
			if i > 0 {
				sb.WriteString(", ")
			}
			if i >= 5 {
				sb.WriteString(fmt.Sprintf("... (%d more)", len(val)-5))
				break
			}
			sb.WriteString(formatValue(elem))
		}
		sb.WriteString("]")
		return sb.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// formatMapValue formats a map value with optional indentation for nested display.
func formatMapValue(m map[string]interface{}, indent int) string {
	if len(m) == 0 {
		return "{}"
	}

	// Sort keys for consistent display
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	prefix := strings.Repeat("  ", indent)

	sb.WriteString("{\n")
	for i, k := range keys {
		v := m[k]
		sb.WriteString(prefix)
		sb.WriteString("  ")
		sb.WriteString(k)
		sb.WriteString(": ")

		// Handle nested maps
		if nested, ok := v.(map[string]interface{}); ok {
			sb.WriteString(formatMapValue(nested, indent+1))
		} else {
			sb.WriteString(formatValue(v))
		}

		if i < len(keys)-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}
	sb.WriteString(prefix)
	sb.WriteString("}")

	return sb.String()
}
