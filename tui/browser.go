package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"warlogix/config"
	"warlogix/logix"
	"warlogix/plcman"
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

	selectedPLC  string
	treeRoot     *tview.TreeNode
	tagNodes     map[string]*tview.TreeNode // Tag name -> tree node for quick lookup
	enabledTags  map[string]bool            // Tag name -> enabled for current PLC
	writableTags map[string]bool            // Tag name -> writable for current PLC
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
		t.app.app.SetFocus(t.tree)
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
	}

	return event
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

	t.showTagDetails(tagInfo)
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

	// Toggle enabled state
	t.enabledTags[tagName] = !t.enabledTags[tagName]
	enabled := t.enabledTags[tagName]
	writable := t.writableTags[tagName]

	// Update node text
	t.updateNodeText(node, tagInfo, enabled, writable)

	// Update config
	t.updateConfigTag(tagName, enabled, writable)

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

	typeName := tag.TypeName()
	shortName := tag.Name
	if idx := strings.LastIndex(tag.Name, "."); idx >= 0 {
		shortName = tag.Name[idx+1:]
	}

	var text string
	if enabled {
		text = fmt.Sprintf("%s %s%s  [gray]%s[-]", checkbox, writeIndicator, shortName, typeName)
		node.SetColor(tcell.ColorWhite)
	} else {
		text = fmt.Sprintf("[gray]%s %s%s  %s[-]", checkbox, writeIndicator, shortName, typeName)
		node.SetColor(tcell.ColorGray)
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
	sb.WriteString("[yellow]Type:[-] " + tag.TypeName() + "\n")

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
		sb.WriteString("\n[green]" + CheckboxChecked + " Publishing to REST/MQTT[-]")
	} else {
		sb.WriteString("\n[gray]" + CheckboxUnchecked + " Not publishing[-]")
	}

	if writable {
		sb.WriteString("\n[red]W Writable via MQTT[-]")
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
	sb.WriteString(fmt.Sprintf("[yellow]Type:[-] %s (0x%04X)\n", tagInfo.TypeName(), tagInfo.TypeCode))
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

	go func() {
		plc := t.app.manager.GetPLC(plcName)
		if plc == nil {
			t.app.QueueUpdateDraw(func() {
				t.details.SetText(sb.String() + "\n[red]PLC not available[-]\n")
			})
			return
		}

		// Try to read the tag directly
		val, err := t.app.manager.ReadTag(plcName, tagName)

		t.app.QueueUpdateDraw(func() {
			var result strings.Builder

			// Rebuild header
			result.WriteString("[yellow::b]Tag Information[-::-]\n")
			result.WriteString("─────────────────────────────\n")
			result.WriteString(fmt.Sprintf("[yellow]Name:[-] %s\n", tagName))
			result.WriteString(fmt.Sprintf("[yellow]Type:[-] %s (0x%04X)\n", tagInfo.TypeName(), tagInfo.TypeCode))
			result.WriteString(fmt.Sprintf("[yellow]Instance:[-] %d\n", tagInfo.Instance))

			if len(tagInfo.Dimensions) > 0 {
				dims := make([]string, len(tagInfo.Dimensions))
				for i, d := range tagInfo.Dimensions {
					dims[i] = fmt.Sprintf("%d", d)
				}
				result.WriteString(fmt.Sprintf("[yellow]Dimensions:[-] [%s]\n", strings.Join(dims, ", ")))
			} else {
				result.WriteString("[yellow]Dimensions:[-] scalar\n")
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
			result.WriteString(fmt.Sprintf("[yellow]Data Type:[-] %s (0x%04X)\n", val.TypeName(), val.DataType))
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
	t.statusBar.SetText(fmt.Sprintf(" %d tags selected | [yellow]/[white] filter  [yellow]c[white] clear  [yellow]p[white] PLC  [yellow]Space[white] toggle  [yellow]w[white] writable  [yellow]d[white] details", count))
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

	options := make([]string, 0)
	selectedIdx := -1

	for i, plc := range plcs {
		if plc.GetStatus() == plcman.StatusConnected {
			options = append(options, plc.Config.Name)
			if plc.Config.Name == t.selectedPLC {
				selectedIdx = i
			}
		}
	}

	t.plcSelect.SetOptions(options, nil)

	if selectedIdx >= 0 {
		t.plcSelect.SetCurrentOption(selectedIdx)
	} else if len(options) > 0 {
		t.plcSelect.SetCurrentOption(0)
		t.selectedPLC = options[0]
		t.loadTags()
	} else {
		t.selectedPLC = ""
		t.clearTree()
	}
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

	plc := t.app.manager.GetPLC(t.selectedPLC)
	if plc == nil {
		return
	}

	tags := plc.GetTags()
	programs := plc.GetPrograms()

	// Load enabled and writable tags from config
	cfg := t.app.config.FindPLC(t.selectedPLC)
	if cfg != nil {
		for _, sel := range cfg.Tags {
			t.enabledTags[sel.Name] = sel.Enabled
			t.writableTags[sel.Name] = sel.Writable
		}
	}

	// Organize tags by program
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
		t.treeRoot.AddChild(controllerNode)

		for i := range controllerTags {
			tag := &controllerTags[i]
			enabled := t.enabledTags[tag.Name]
			writable := t.writableTags[tag.Name]
			node := t.createTagNode(tag, enabled, writable)
			controllerNode.AddChild(node)
			t.tagNodes[tag.Name] = node
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
		t.treeRoot.AddChild(progNode)

		for i := range tags {
			tag := &tags[i]
			enabled := t.enabledTags[tag.Name]
			writable := t.writableTags[tag.Name]
			node := t.createTagNode(tag, enabled, writable)
			progNode.AddChild(node)
			t.tagNodes[tag.Name] = node
		}
	}

	t.updateStatus()
}

func (t *BrowserTab) createTagNode(tag *logix.TagInfo, enabled, writable bool) *tview.TreeNode {
	checkbox := CheckboxUnchecked
	if enabled {
		checkbox = CheckboxChecked
	}

	// Writable indicator
	writeIndicator := ""
	if writable {
		writeIndicator = "[red]W[-] "
	}

	typeName := tag.TypeName()
	shortName := tag.Name
	if idx := strings.LastIndex(tag.Name, "."); idx >= 0 {
		shortName = tag.Name[idx+1:]
	}

	var text string
	if enabled {
		text = fmt.Sprintf("%s %s%s  [gray]%s[-]", checkbox, writeIndicator, shortName, typeName)
	} else {
		text = fmt.Sprintf("[gray]%s %s%s  %s[-]", checkbox, writeIndicator, shortName, typeName)
	}

	node := tview.NewTreeNode(text).
		SetReference(tag).
		SetSelectable(true)

	if enabled {
		node.SetColor(tcell.ColorWhite)
	} else {
		node.SetColor(tcell.ColorGray)
	}

	return node
}

func (t *BrowserTab) applyFilter(filterText string) {
	filterText = strings.ToLower(filterText)

	// Walk through all nodes and show/hide based on filter
	for name, node := range t.tagNodes {
		if filterText == "" || strings.Contains(strings.ToLower(name), filterText) {
			node.SetColor(tcell.ColorWhite)
		} else {
			node.SetColor(tcell.ColorGray)
		}
	}
}
