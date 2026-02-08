package tui

import (
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// PLCTag represents a tag with its PLC name.
type PLCTag struct {
	PLC string
	Tag string
}

// TagPickerOptions configures the tag picker behavior.
type TagPickerOptions struct {
	Title        string            // Dialog title
	ExcludeTags  []PLCTag          // Tags to exclude from the list
	ExcludePacks []string          // Pack names to exclude
	IncludePacks bool              // If true, include TagPacks in the list
	PLCFilter    string            // If set, only show tags from this PLC
	OnSelectTag  func(plc, tag string) // Called when a tag is selected
	OnSelectPack func(packName string) // Called when a pack is selected (if IncludePacks)
}

// pickerEntry represents an item in the tag picker.
type pickerEntry struct {
	isPack   bool
	plc      string // empty for packs
	tag      string // pack name if isPack
	display  string // formatted display text
	enabled  bool   // true if enabled/active
	isUDT    bool
	udtDepth int
}

// ShowTagPickerWithOptions displays a modal to select a tag or pack.
// Uses a Table for proper strikethrough rendering.
func (a *App) ShowTagPickerWithOptions(opts TagPickerOptions) {
	const pageName = "tag-picker"

	// Build exclusion sets
	excludedTags := make(map[string]bool)
	for _, et := range opts.ExcludeTags {
		excludedTags[et.PLC+":"+et.Tag] = true
	}
	excludedPacks := make(map[string]bool)
	for _, p := range opts.ExcludePacks {
		excludedPacks[p] = true
	}

	// Build list of entries
	var allEntries []pickerEntry

	// Add packs first if requested
	if opts.IncludePacks {
		for _, pack := range a.config.TagPacks {
			if excludedPacks[pack.Name] {
				continue
			}
			allEntries = append(allEntries, pickerEntry{
				isPack:  true,
				tag:     pack.Name,
				display: pack.Name + " (TagPack)",
				enabled: pack.Enabled,
			})
		}
	}

	// Add tags from PLCs
	plcs := a.manager.ListPLCs()
	for _, plc := range plcs {
		// Filter by PLC if specified
		if opts.PLCFilter != "" && plc.Config.Name != opts.PLCFilter {
			continue
		}

		// Build set of enabled tags for this PLC
		enabledTags := make(map[string]bool)
		for _, sel := range plc.Config.Tags {
			if sel.Enabled {
				enabledTags[sel.Name] = true
			}
		}

		// Track which tags we've added to avoid duplicates
		added := make(map[string]bool)

		// Add all discovered top-level tags
		tags := plc.GetTags()
		for _, tag := range tags {
			key := plc.Config.Name + ":" + tag.Name
			if excludedTags[key] {
				continue
			}
			added[tag.Name] = true

			display := tag.Name
			if opts.PLCFilter == "" {
				display = plc.Config.Name + ":" + tag.Name
			}

			allEntries = append(allEntries, pickerEntry{
				plc:     plc.Config.Name,
				tag:     tag.Name,
				display: display,
				enabled: enabledTags[tag.Name],
			})
		}

		// Add config-defined tags not already added from discovery
		for _, sel := range plc.Config.Tags {
			if added[sel.Name] {
				continue
			}
			key := plc.Config.Name + ":" + sel.Name
			if excludedTags[key] {
				continue
			}
			added[sel.Name] = true

			dotCount := strings.Count(sel.Name, ".")
			display := sel.Name
			if opts.PLCFilter == "" {
				display = plc.Config.Name + ":" + sel.Name
			}

			allEntries = append(allEntries, pickerEntry{
				plc:      plc.Config.Name,
				tag:      sel.Name,
				display:  display,
				enabled:  sel.Enabled,
				isUDT:    dotCount > 0,
				udtDepth: dotCount,
			})
		}
	}

	// Sort entries: packs first, then tags by display name
	sort.Slice(allEntries, func(i, j int) bool {
		if allEntries[i].isPack != allEntries[j].isPack {
			return allEntries[i].isPack // packs first
		}
		return allEntries[i].display < allEntries[j].display
	})

	if len(allEntries) == 0 {
		a.showError("No Items", "No tags available. Connect to a PLC first.")
		return
	}

	// Create filter input
	filter := tview.NewInputField().
		SetLabel("Filter: ").
		SetFieldWidth(40)
	ApplyInputFieldTheme(filter)

	// Create table for items (better strikethrough support than List)
	table := tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false)
	ApplyTableTheme(table)

	// Track filtered entries for selection callback
	var filteredEntries []pickerEntry

	// Populate table function
	populateTable := func(filterText string) {
		table.Clear()
		filteredEntries = nil
		filterLower := strings.ToLower(filterText)

		row := 0
		for _, entry := range allEntries {
			// Filter matching
			displayLower := strings.ToLower(entry.display)
			packMatch := entry.isPack && strings.Contains("pack", filterLower)
			if filterText != "" && !strings.Contains(displayLower, filterLower) && !packMatch {
				continue
			}

			filteredEntries = append(filteredEntries, entry)

			// Build display text
			displayText := entry.display

			// Add UDT indicator
			if entry.isUDT {
				prefix := strings.Repeat("⊳", entry.udtDepth) + " "
				displayText = prefix + entry.display
			}

			cell := tview.NewTableCell(displayText).
				SetExpansion(1).
				SetReference(&filteredEntries[len(filteredEntries)-1])

			// Use strikethrough + dim color for disabled items (dim for terminals without strikethrough)
			if !entry.enabled {
				cell.SetTextColor(CurrentTheme.TextDim).SetAttributes(tcell.AttrStrikeThrough)
			} else if entry.isUDT {
				cell.SetTextColor(CurrentTheme.TextDim)
			}

			table.SetCell(row, 0, cell)
			row++
		}

		if row > 0 {
			table.Select(0, 0)
		}
	}
	populateTable("")

	// Handle selection
	selectCurrent := func() {
		row, _ := table.GetSelection()
		if row < 0 || row >= len(filteredEntries) {
			return
		}
		entry := filteredEntries[row]
		a.closeModal(pageName)
		if entry.isPack && opts.OnSelectPack != nil {
			opts.OnSelectPack(entry.tag)
		} else if !entry.isPack && opts.OnSelectTag != nil {
			opts.OnSelectTag(entry.plc, entry.tag)
		}
	}

	filter.SetChangedFunc(func(text string) {
		populateTable(text)
	})

	filter.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyDown, tcell.KeyTab:
			a.app.SetFocus(table)
			return nil
		case tcell.KeyEscape:
			a.closeModal(pageName)
			return nil
		case tcell.KeyEnter:
			if table.GetRowCount() > 0 {
				table.Select(0, 0)
				a.app.SetFocus(table)
			}
			return nil
		}
		return event
	})

	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			a.closeModal(pageName)
			return nil
		case tcell.KeyEnter:
			selectCurrent()
			return nil
		case tcell.KeyUp:
			row, _ := table.GetSelection()
			if row == 0 {
				a.app.SetFocus(filter)
				return nil
			}
		}
		if event.Rune() == '/' {
			a.app.SetFocus(filter)
			return nil
		}
		return event
	})

	table.SetSelectedFunc(func(row, col int) {
		selectCurrent()
	})

	// Layout
	content := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(filter, 1, 0, true).
		AddItem(table, 0, 1, false)
	content.SetBorder(true).SetTitle(" " + opts.Title + " (/ to filter, Enter to select) ")
	content.SetBorderColor(CurrentTheme.Border).SetTitleColor(CurrentTheme.Accent)

	a.showCenteredModal(pageName, content, 70, 20)
	a.app.SetFocus(filter)
}

// ShowTagPicker displays a modal to select a tag from any PLC.
// This is a convenience wrapper around ShowTagPickerWithOptions for backwards compatibility.
func (a *App) ShowTagPicker(title string, excludeTags []PLCTag, onSelect func(plc, tag string)) {
	a.ShowTagPickerWithOptions(TagPickerOptions{
		Title:       title,
		ExcludeTags: excludeTags,
		OnSelectTag: onSelect,
	})
}

// GetEnabledTags returns the list of tag names enabled for republishing on a PLC.
func (a *App) GetEnabledTags(plcName string) []string {
	plcCfg := a.config.FindPLC(plcName)
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

