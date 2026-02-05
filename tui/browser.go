package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"warlogix/ads"
	"warlogix/config"
	"warlogix/logix"
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
	details   *tview.TextView
	statusBar *tview.TextView

	selectedPLC       string
	lastPLCOptions    []string // Track dropdown options to avoid unnecessary updates
	updatingDropdown  bool     // True when programmatically updating dropdown
	treeRoot          *tview.TreeNode
	tagNodes          map[string]*tview.TreeNode // Tag name -> tree node for quick lookup
	enabledTags       map[string]bool            // Tag name -> enabled for current PLC
	writableTags      map[string]bool            // Tag name -> writable for current PLC
	filterText        string                     // Current filter text (lowercase)
}

// NewBrowserTab creates a new browser tab.
func NewBrowserTab(app *App) *BrowserTab {
	t := &BrowserTab{
		app:          app,
		tagNodes:     make(map[string]*tview.TreeNode),
		enabledTags:  make(map[string]bool),
		writableTags: make(map[string]bool),
	}
	t.setupUI()
	return t
}

func (t *BrowserTab) setupUI() {
	// PLC dropdown
	t.plcSelect = tview.NewDropDown().
		SetLabel("PLC: ").
		SetFieldWidth(20)
	t.plcSelect.SetSelectedFunc(func(text string, index int) {
		t.selectedPLC = text
		t.loadTags()
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

	// Header row
	header := tview.NewFlex().
		AddItem(t.plcSelect, 30, 0, false).
		AddItem(nil, 2, 0, false).
		AddItem(t.filter, 40, 0, false).
		AddItem(nil, 0, 1, false)

	// Tree view for programs/tags
	t.treeRoot = tview.NewTreeNode("Tags").SetColor(tcell.ColorYellow)
	t.tree = tview.NewTreeView().
		SetRoot(t.treeRoot).
		SetCurrentNode(t.treeRoot)

	t.tree.SetSelectedFunc(t.onNodeSelected)
	t.tree.SetInputCapture(t.handleTreeKeys)

	treeFrame := tview.NewFrame(t.tree).SetBorders(0, 0, 0, 0, 0, 0)
	treeFrame.SetBorder(true).SetTitle(" Programs/Tags ")

	// Details panel
	t.details = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true)
	t.details.SetBorder(true).SetTitle(" Tag Details ")
	t.details.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape || event.Key() == tcell.KeyTab {
			t.app.app.SetFocus(t.tree)
			return nil
		}
		return event
	})

	// Content area
	content := tview.NewFlex().
		AddItem(treeFrame, 0, 1, true).
		AddItem(t.details, 40, 0, false)

	// Status bar
	t.statusBar = tview.NewTextView().
		SetDynamicColors(true)

	// Main layout
	t.flex = tview.NewFlex().
		SetDirection(tview.FlexRow).
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
		// Focus PLC dropdown
		t.app.app.SetFocus(t.plcSelect)
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
		}
	}
	return logix.TypeName(typeCode)
}

func (t *BrowserTab) onNodeSelected(node *tview.TreeNode) {
	ref := node.GetReference()
	if ref == nil {
		// It's a program node, expand/collapse
		node.SetExpanded(!node.IsExpanded())
		return
	}

	// It's a tag node, show details
	tagInfo, ok := ref.(*logix.TagInfo)
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
func (t *BrowserTab) lazyExpandUDT(node *tview.TreeNode, tagInfo *logix.TagInfo) {
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

		// Create synthetic TagInfo for member
		memberInfo := &logix.TagInfo{
			Name:     memberPath,
			TypeCode: member.Type,
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

	tagInfo, ok := ref.(*logix.TagInfo)
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

	// If tag was just enabled, force publish its current value immediately
	if enabled && !wasEnabled && t.selectedPLC != "" {
		go t.app.ForcePublishTag(t.selectedPLC, tagName)
	}

	// Update status
	t.updateStatus()
}

func (t *BrowserTab) toggleNodeWritable(node *tview.TreeNode) {
	ref := node.GetReference()
	if ref == nil {
		return
	}

	tagInfo, ok := ref.(*logix.TagInfo)
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

func (t *BrowserTab) updateNodeText(node *tview.TreeNode, tag *logix.TagInfo, enabled, writable bool) {
	checkbox := CheckboxUnchecked
	if enabled {
		checkbox = CheckboxChecked
	}

	// Writable indicator
	writeIndicator := ""
	if writable {
		writeIndicator = "[red]W[-] "
	}

	// UDT expandable indicator
	udtIndicator := ""
	if logix.IsStructure(tag.TypeCode) {
		udtIndicator = "[yellow]▶[-] "
	}

	typeName := t.getTypeName(tag.TypeCode)
	shortName := tag.Name
	if idx := strings.LastIndex(tag.Name, "."); idx >= 0 {
		shortName = tag.Name[idx+1:]
	}

	// For S7 PLCs, show alias as primary name if set, with address in gray
	cfg := t.app.config.FindPLC(t.selectedPLC)
	if cfg != nil && cfg.GetFamily() == config.FamilyS7 {
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

	var text string
	if enabled {
		text = fmt.Sprintf("%s %s%s%s  [gray]%s[-]", checkbox, udtIndicator, writeIndicator, shortName, typeName)
		node.SetColor(tcell.ColorWhite)
	} else {
		// Don't use inline color tags - let node.SetColor handle it
		// This allows proper color inversion when the node is selected
		text = fmt.Sprintf("%s %s%s%s  %s", checkbox, udtIndicator, writeIndicator, shortName, typeName)
		node.SetColor(tcell.ColorDarkGray)
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

func (t *BrowserTab) showTagDetails(tag *logix.TagInfo) {
	var sb strings.Builder

	sb.WriteString("[yellow]Name:[-] " + tag.Name + "\n")
	sb.WriteString("[yellow]Type:[-] " + t.getTypeName(tag.TypeCode) + "\n")

	// Get current value if available
	plc := t.app.manager.GetPLC(t.selectedPLC)
	if plc != nil {
		values := plc.GetValues()
		if val, ok := values[tag.Name]; ok {
			if val.Error != nil {
				sb.WriteString("[yellow]Value:[-] [red]" + val.Error.Error() + "[-]\n")
			} else {
				sb.WriteString(fmt.Sprintf("[yellow]Value:[-] %v\n", val.GoValue()))
			}
		}
	}

	// Show alias if set
	cfg := t.app.config.FindPLC(t.selectedPLC)
	if cfg != nil {
		for _, sel := range cfg.Tags {
			if sel.Name == tag.Name && sel.Alias != "" {
				sb.WriteString("[yellow]Alias:[-] " + sel.Alias + "\n")
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
		sb.WriteString("[yellow]Dimensions:[-] [" + strings.Join(dims, ",") + "]\n")
	}

	enabled := t.enabledTags[tag.Name]
	writable := t.writableTags[tag.Name]
	if enabled {
		sb.WriteString("\n[green]" + CheckboxChecked + " Publishing to REST/MQTT/Valkey[-]")
	} else {
		sb.WriteString("\n[gray]" + CheckboxUnchecked + " Not publishing[-]")
	}

	if writable {
		sb.WriteString("\n[red]W Writable[-]")
	} else {
		sb.WriteString("\n[gray]  Read-only[-]")
	}

	sb.WriteString("\n\n[blue]Space[white] toggle  [blue]w[white] writable  [blue]d[white] details[-]")

	t.details.SetText(sb.String())
}

func (t *BrowserTab) showDetailedTagInfo(node *tview.TreeNode) {
	ref := node.GetReference()
	if ref == nil {
		return
	}

	tagInfo, ok := ref.(*logix.TagInfo)
	if !ok {
		return
	}

	// Show immediate feedback
	var sb strings.Builder
	sb.WriteString("[yellow::b]Tag Information[-::-]\n")
	sb.WriteString("─────────────────────────────\n")
	sb.WriteString(fmt.Sprintf("[yellow]Name:[-] %s\n", tagInfo.Name))
	sb.WriteString(fmt.Sprintf("[yellow]Type:[-] %s (0x%04X)\n", t.getTypeName(tagInfo.TypeCode), tagInfo.TypeCode))
	sb.WriteString(fmt.Sprintf("[yellow]Instance:[-] %d\n", tagInfo.Instance))

	// Dimensions
	if len(tagInfo.Dimensions) > 0 {
		dims := make([]string, len(tagInfo.Dimensions))
		for i, d := range tagInfo.Dimensions {
			dims[i] = fmt.Sprintf("%d", d)
		}
		sb.WriteString(fmt.Sprintf("[yellow]Dimensions:[-] [%s]\n", strings.Join(dims, ", ")))
	} else {
		sb.WriteString("[yellow]Dimensions:[-] scalar\n")
	}


	sb.WriteString("\n[yellow::b]Live Value[-::-]\n")
	sb.WriteString("─────────────────────────────\n")
	sb.WriteString("[gray]Reading from PLC...[-]\n")
	t.details.SetText(sb.String())

	// Read from PLC in background goroutine
	plcName := t.selectedPLC
	tagName := tagInfo.Name
	tagTypeCode := tagInfo.TypeCode
	tagInstance := tagInfo.Instance
	tagDimensions := tagInfo.Dimensions

	go func() {
		plc := t.app.manager.GetPLC(plcName)
		if plc == nil {
			t.app.QueueUpdateDraw(func() {
				t.details.SetText(sb.String() + "\n[red]PLC not available[-]\n")
			})
			return
		}

		// For array types, show array info (dimensions come from tag list discovery)
		var arrayDebugInfo string
		isArrayType := logix.IsArrayType(tagTypeCode)
		isStruct := logix.IsStructure(tagTypeCode)
		if isArrayType || isStruct {
			var debugSb strings.Builder
			if isArrayType {
				debugSb.WriteString("\n[yellow::b]Array Info[-::-]\n")
			} else {
				debugSb.WriteString("\n[yellow::b]Type Info[-::-]\n")
			}
			debugSb.WriteString("─────────────────────────────\n")
			baseType := logix.BaseType(tagTypeCode)
			debugSb.WriteString(fmt.Sprintf("[yellow]Base Type:[-] %s\n", logix.TypeName(baseType)))

			// For structures, show template ID
			if isStruct {
				templateID := logix.TemplateID(tagTypeCode)
				debugSb.WriteString(fmt.Sprintf("[yellow]Template ID:[-] %d (0x%04X)\n", templateID, templateID))
			}

			// Get element size (handles both atomic and structure types)
			var elemSize uint32
			if client := plc.GetLogixClient(); client != nil {
				elemSize = client.GetElementSize(tagTypeCode)
			} else {
				elemSize = uint32(logix.TypeSize(baseType))
			}
			debugSb.WriteString(fmt.Sprintf("[yellow]Element Size:[-] %d bytes\n", elemSize))
			arrayDebugInfo = debugSb.String()
		}

		// Try to read the tag directly
		val, err := t.app.manager.ReadTag(plcName, tagName)

		t.app.QueueUpdateDraw(func() {
			var result strings.Builder

			// Rebuild header
			result.WriteString("[yellow::b]Tag Information[-::-]\n")
			result.WriteString("─────────────────────────────\n")
			result.WriteString(fmt.Sprintf("[yellow]Name:[-] %s\n", tagName))
			result.WriteString(fmt.Sprintf("[yellow]Type:[-] %s (0x%04X)\n", t.getTypeName(tagTypeCode), tagTypeCode))
			result.WriteString(fmt.Sprintf("[yellow]Instance:[-] %d\n", tagInstance))

			if len(tagDimensions) > 0 {
				dims := make([]string, len(tagDimensions))
				for i, d := range tagDimensions {
					dims[i] = fmt.Sprintf("%d", d)
				}
				result.WriteString(fmt.Sprintf("[yellow]Dimensions:[-] [%s]\n", strings.Join(dims, ", ")))
			} else {
				result.WriteString("[yellow]Dimensions:[-] scalar\n")
			}

			// Add array debug info if available
			if arrayDebugInfo != "" {
				result.WriteString(arrayDebugInfo)
			}

			result.WriteString("\n[yellow::b]Live Value[-::-]\n")
			result.WriteString("─────────────────────────────\n")

			if err != nil {
				result.WriteString(fmt.Sprintf("[red]Read error:[-] %v\n", err))
				t.details.SetText(result.String())
				return
			}

			if val == nil {
				result.WriteString("[gray]No value returned[-]\n")
				t.details.SetText(result.String())
				return
			}

			if val.Error != nil {
				result.WriteString(fmt.Sprintf("[red]Tag error:[-] %v\n", val.Error))
				t.details.SetText(result.String())
				return
			}

			// Display value
			result.WriteString(fmt.Sprintf("[yellow]Value:[-] %v\n", val.GoValue()))
			result.WriteString(fmt.Sprintf("[yellow]Data Type:[-] %s (0x%04X)\n", t.getTypeName(val.DataType), val.DataType))
			result.WriteString(fmt.Sprintf("[yellow]Size:[-] %d bytes\n", len(val.Bytes)))

			// Raw bytes hex dump
			result.WriteString("\n[yellow::b]Raw Bytes[-::-]\n")
			result.WriteString("─────────────────────────────\n")

			if len(val.Bytes) > 0 {
				// Hex dump with offset
				for i := 0; i < len(val.Bytes); i += 16 {
					// Offset
					result.WriteString(fmt.Sprintf("[gray]%04X:[-] ", i))

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
					result.WriteString(" [gray]|")
					for j := 0; j < 16 && i+j < len(val.Bytes); j++ {
						b := val.Bytes[i+j]
						if b >= 32 && b < 127 {
							result.WriteString(string(b))
						} else {
							result.WriteString(".")
						}
					}
					result.WriteString("|[-]\n")

					// Limit display to prevent huge outputs
					if i >= 256 {
						result.WriteString(fmt.Sprintf("[gray]... (%d more bytes)[-]\n", len(val.Bytes)-i-16))
						break
					}
				}
			} else {
				result.WriteString("[gray]No data[-]\n")
			}

			t.details.SetText(result.String())
		})
	}()
}

func (t *BrowserTab) updateStatus() {
	count := 0
	for _, enabled := range t.enabledTags {
		if enabled {
			count++
		}
	}
	baseStatus := fmt.Sprintf(" %d tags selected | [yellow]/[white] filter  [yellow]c[white] clear  [yellow]p[white] PLC  [yellow]Space[white] toggle  [yellow]w[white] writable  [yellow]d[white] details", count)

	// Add manual tag keys for non-discovery PLCs
	if t.isManualPLC() {
		baseStatus += "  [yellow]a[white] add  [yellow]e[white] edit  [yellow]x[white] delete"
	}

	baseStatus += "  [gray]│[white]  [yellow]?[white] help"
	t.statusBar.SetText(baseStatus)
}

// GetPrimitive returns the main primitive for this tab.
func (t *BrowserTab) GetPrimitive() tview.Primitive {
	return t.flex
}

// GetFocusable returns the element that should receive focus.
func (t *BrowserTab) GetFocusable() tview.Primitive {
	return t.tree
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

	for _, plc := range plcs {
		// Show connected PLCs, or manual PLCs (even if not connected, so tags can be configured)
		isManual := !plc.Config.GetFamily().SupportsDiscovery()
		if plc.GetStatus() == plcman.StatusConnected || isManual {
			if plc.Config.Name == t.selectedPLC {
				selectedIdx = len(options) // Index in options, not in plcs
			}
			options = append(options, plc.Config.Name)
		}
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
			t.loadTags()
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
			t.loadTags()
		} else if len(options) == 0 {
			t.selectedPLC = ""
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
	t.details.SetText("")
	t.statusBar.SetText(" No PLC selected")
}

func (t *BrowserTab) loadTags() {
	t.treeRoot.ClearChildren()
	t.tagNodes = make(map[string]*tview.TreeNode)
	t.enabledTags = make(map[string]bool)
	t.writableTags = make(map[string]bool)

	if t.selectedPLC == "" {
		return
	}

	cfg := t.app.config.FindPLC(t.selectedPLC)
	if cfg == nil {
		return
	}

	isManual := !cfg.GetFamily().SupportsDiscovery()

	plc := t.app.manager.GetPLC(t.selectedPLC)

	var tags []logix.TagInfo
	var programs []string
	var values map[string]*plcman.TagValue

	if plc != nil {
		tags = plc.GetTags()
		programs = plc.GetPrograms()
		values = plc.GetValues()
	} else {
		values = make(map[string]*plcman.TagValue)
	}

	// For manual PLCs not yet connected, build tags from config
	if isManual && len(tags) == 0 && len(cfg.Tags) > 0 {
		for _, sel := range cfg.Tags {
			typeCode, ok := logix.TypeCodeFromName(sel.DataType)
			if !ok {
				typeCode = logix.TypeDINT
			}
			tags = append(tags, logix.TagInfo{
				Name:     sel.Name,
				TypeCode: typeCode,
			})
		}
	}

	// Load enabled and writable tags from config
	if cfg != nil {
		for _, sel := range cfg.Tags {
			t.enabledTags[sel.Name] = sel.Enabled
			t.writableTags[sel.Name] = sel.Writable
		}
	}

	// For manual PLCs, show a different tree structure
	if isManual {
		sectionName := "Manual Tags"
		sectionNode := tview.NewTreeNode(sectionName).
			SetColor(tcell.ColorBlue).
			SetExpanded(true)

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
	controllerTags := []logix.TagInfo{}
	programTags := make(map[string][]logix.TagInfo)

	for _, tag := range tags {
		if strings.HasPrefix(tag.Name, "Program:") {
			// Extract program name
			rest := strings.TrimPrefix(tag.Name, "Program:")
			if idx := strings.Index(rest, "."); idx > 0 {
				progName := rest[:idx]
				programTags[progName] = append(programTags[progName], tag)
			}
		} else {
			controllerTags = append(controllerTags, tag)
		}
	}

	// Sort controller tags
	sort.Slice(controllerTags, func(i, j int) bool {
		return controllerTags[i].Name < controllerTags[j].Name
	})

	// Add controller tags
	if len(controllerTags) > 0 {
		controllerNode := tview.NewTreeNode("Controller").
			SetColor(tcell.ColorBlue).
			SetExpanded(true)

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
			SetColor(tcell.ColorBlue).
			SetExpanded(true)

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

func (t *BrowserTab) createTagNode(tag *logix.TagInfo, enabled, writable bool) *tview.TreeNode {
	return t.createTagNodeWithError(tag, enabled, writable, false)
}

func (t *BrowserTab) createTagNodeWithError(tag *logix.TagInfo, enabled, writable, hasError bool) *tview.TreeNode {
	checkbox := CheckboxUnchecked
	if enabled {
		checkbox = CheckboxChecked
	}

	// Writable indicator
	writeIndicator := ""
	if writable {
		writeIndicator = "[red]W[-] "
	}

	// Error indicator
	errorIndicator := ""
	if hasError {
		errorIndicator = "[red]![-] "
	}

	// UDT expandable indicator
	udtIndicator := ""
	if logix.IsStructure(tag.TypeCode) {
		udtIndicator = "[yellow]▶[-] "
	}

	typeName := t.getTypeName(tag.TypeCode)
	shortName := tag.Name
	if idx := strings.LastIndex(tag.Name, "."); idx >= 0 {
		shortName = tag.Name[idx+1:]
	}

	// For S7 PLCs, show alias as primary name if set, with address in gray
	cfg := t.app.config.FindPLC(t.selectedPLC)
	if cfg != nil && cfg.GetFamily() == config.FamilyS7 {
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

	var text string
	if enabled {
		text = fmt.Sprintf("%s %s%s%s%s  [gray]%s[-]", checkbox, udtIndicator, errorIndicator, writeIndicator, shortName, typeName)
	} else {
		// Don't use inline color tags - let node.SetColor handle it
		// This allows proper color inversion when the node is selected
		text = fmt.Sprintf("%s %s%s%s%s  %s", checkbox, udtIndicator, errorIndicator, writeIndicator, shortName, typeName)
	}

	node := tview.NewTreeNode(text).
		SetReference(tag).
		SetSelectable(true)

	if enabled {
		node.SetColor(tcell.ColorWhite)
	} else {
		node.SetColor(tcell.ColorDarkGray)
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

// showAddTagDialog shows a dialog to add a manual tag.
func (t *BrowserTab) showAddTagDialog() {
	form := tview.NewForm()
	form.SetBorder(true).SetTitle(" Add Manual Tag ")

	// Use "Address:" label and S7 types for S7 PLCs
	cfg := t.app.config.FindPLC(t.selectedPLC)
	addressLabel := "Tag Name:"
	var typeOptions []string
	isS7 := cfg != nil && cfg.GetFamily() == config.FamilyS7
	if isS7 {
		addressLabel = "Address:"
		typeOptions = s7.SupportedTypeNames()
		// S7: Alias first, then Type, then Address (more intuitive)
		form.AddInputField("Name:", "", 30, nil, nil)
		form.AddDropDown("Data Type:", typeOptions, 3, nil) // Default to DINT (index 3)
		form.AddInputField(addressLabel, "", 30, nil, nil)
		form.AddCheckbox("Writable:", false, nil)
	} else {
		typeOptions = logix.SupportedTypeNames()
		// Logix: Tag Name, Type, Alias
		form.AddInputField(addressLabel, "", 30, nil, nil)
		form.AddDropDown("Data Type:", typeOptions, 3, nil) // Default to DINT (index 3)
		form.AddInputField("Alias:", "", 30, nil, nil)
		form.AddCheckbox("Writable:", false, nil)
	}

	form.AddButton("Add", func() {
		var tagName, alias string
		if isS7 {
			alias = form.GetFormItemByLabel("Name:").(*tview.InputField).GetText()
			tagName = form.GetFormItemByLabel(addressLabel).(*tview.InputField).GetText()
		} else {
			tagName = form.GetFormItemByLabel(addressLabel).(*tview.InputField).GetText()
			alias = form.GetFormItemByLabel("Alias:").(*tview.InputField).GetText()
		}
		typeIdx, _ := form.GetFormItemByLabel("Data Type:").(*tview.DropDown).GetCurrentOption()
		writable := form.GetFormItemByLabel("Writable:").(*tview.Checkbox).IsChecked()

		if tagName == "" {
			if isS7 {
				t.app.showErrorWithFocus("Error", "Address is required", form)
			} else {
				t.app.showErrorWithFocus("Error", "Tag name is required", form)
			}
			return
		}

		// Validate S7 address format
		if isS7 {
			if err := s7.ValidateAddress(tagName); err != nil {
				t.app.showErrorWithFocus("Invalid Address", err.Error(), form)
				return
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

		t.app.pages.RemovePage("addtag")
		t.loadTags()
		t.app.focusCurrentTab()
		t.app.setStatus(fmt.Sprintf("Added tag: %s", tagName))
	})

	form.AddButton("Cancel", func() {
		t.app.pages.RemovePage("addtag")
		t.app.focusCurrentTab()
	})

	form.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			t.app.pages.RemovePage("addtag")
			t.app.focusCurrentTab()
			return nil
		}
		return event
	})

	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(form, 14, 1, true).
			AddItem(nil, 0, 1, false), 50, 1, true).
		AddItem(nil, 0, 1, false)

	t.app.pages.AddPage("addtag", modal, true, true)
	t.app.app.SetFocus(form)
}

// showEditTagDialog shows a dialog to edit a manual tag.
func (t *BrowserTab) showEditTagDialog(node *tview.TreeNode) {
	ref := node.GetReference()
	if ref == nil {
		return
	}

	tagInfo, ok := ref.(*logix.TagInfo)
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
	form.SetBorder(true).SetTitle(" Edit Manual Tag ")

	// Use S7 types for S7 PLCs
	isS7 := cfg.GetFamily() == config.FamilyS7
	var typeOptions []string
	if isS7 {
		typeOptions = s7.SupportedTypeNames()
	} else {
		typeOptions = logix.SupportedTypeNames()
	}

	selectedType := 3 // Default to DINT
	for i, opt := range typeOptions {
		if opt == tagSel.DataType {
			selectedType = i
			break
		}
	}

	// Use "Address:" label for S7 PLCs, "Tag Name:" for others
	addressLabel := "Tag Name:"
	if isS7 {
		addressLabel = "Address:"
		// S7: Name first, then Type, then Address (more intuitive)
		form.AddInputField("Name:", tagSel.Alias, 30, nil, nil)
		form.AddDropDown("Data Type:", typeOptions, selectedType, nil)
		form.AddInputField(addressLabel, tagSel.Name, 30, nil, nil)
		form.AddCheckbox("Writable:", tagSel.Writable, nil)
	} else {
		// Logix: Tag Name, Type, Alias
		form.AddInputField(addressLabel, tagSel.Name, 30, nil, nil)
		form.AddDropDown("Data Type:", typeOptions, selectedType, nil)
		form.AddInputField("Alias:", tagSel.Alias, 30, nil, nil)
		form.AddCheckbox("Writable:", tagSel.Writable, nil)
	}

	originalName := tagSel.Name

	form.AddButton("Save", func() {
		var tagName, alias string
		if isS7 {
			alias = form.GetFormItemByLabel("Name:").(*tview.InputField).GetText()
			tagName = form.GetFormItemByLabel(addressLabel).(*tview.InputField).GetText()
		} else {
			tagName = form.GetFormItemByLabel(addressLabel).(*tview.InputField).GetText()
			alias = form.GetFormItemByLabel("Alias:").(*tview.InputField).GetText()
		}
		typeIdx, _ := form.GetFormItemByLabel("Data Type:").(*tview.DropDown).GetCurrentOption()
		writable := form.GetFormItemByLabel("Writable:").(*tview.Checkbox).IsChecked()

		if tagName == "" {
			if isS7 {
				t.app.showErrorWithFocus("Error", "Address is required", form)
			} else {
				t.app.showErrorWithFocus("Error", "Tag name is required", form)
			}
			return
		}

		// Validate S7 address format
		if isS7 {
			if err := s7.ValidateAddress(tagName); err != nil {
				t.app.showErrorWithFocus("Invalid Address", err.Error(), form)
				return
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

		t.app.pages.RemovePage("edittag")
		t.loadTags()
		t.app.focusCurrentTab()
		t.app.setStatus(fmt.Sprintf("Updated tag: %s", tagName))
	})

	form.AddButton("Cancel", func() {
		t.app.pages.RemovePage("edittag")
		t.app.focusCurrentTab()
	})

	form.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			t.app.pages.RemovePage("edittag")
			t.app.focusCurrentTab()
			return nil
		}
		return event
	})

	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(form, 14, 1, true).
			AddItem(nil, 0, 1, false), 50, 1, true).
		AddItem(nil, 0, 1, false)

	t.app.pages.AddPage("edittag", modal, true, true)
	t.app.app.SetFocus(form)
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

		// Create a synthetic TagInfo for this member
		memberInfo := &logix.TagInfo{
			Name:     memberPath,
			TypeCode: member.Type,
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

	tagInfo, ok := ref.(*logix.TagInfo)
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
