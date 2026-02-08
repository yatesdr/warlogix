package tui

import (
	"fmt"
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

// ShowTagPicker displays a modal to select a tag from any PLC.
// Shows all discovered top-level tags (strike-through if not enabled) plus
// any enabled UDT members from the config.
// excludeTags is a list of "PLC:Tag" strings to exclude from the list.
// onSelect is called with the selected PLC and tag name.
func (a *App) ShowTagPicker(title string, excludeTags []PLCTag, onSelect func(plc, tag string)) {
	const pageName = "tag-picker"

	// Build exclusion set
	excluded := make(map[string]bool)
	for _, et := range excludeTags {
		excluded[et.PLC+":"+et.Tag] = true
	}

	// Build list of tags from all PLCs
	type tagEntry struct {
		plc      string
		tag      string
		display  string
		enabled  bool // true if enabled in Browser for polling
		isUDT    bool // true if tag is a UDT member (has dots in name)
		udtDepth int  // nesting depth (number of dots)
	}
	var allTags []tagEntry

	plcs := a.manager.ListPLCs()
	for _, plc := range plcs {
		// Build set of enabled tags for this PLC
		enabledTags := make(map[string]bool)
		for _, sel := range plc.Config.Tags {
			if sel.Enabled {
				enabledTags[sel.Name] = true
			}
		}

		// Track which tags we've added to avoid duplicates
		added := make(map[string]bool)

		// Add all discovered top-level tags (for PLCs that support discovery)
		tags := plc.GetTags()
		for _, tag := range tags {
			key := plc.Config.Name + ":" + tag.Name
			if excluded[key] {
				continue
			}
			added[tag.Name] = true

			allTags = append(allTags, tagEntry{
				plc:      plc.Config.Name,
				tag:      tag.Name,
				display:  fmt.Sprintf("%s:%s", plc.Config.Name, tag.Name),
				enabled:  enabledTags[tag.Name],
				isUDT:    false,
				udtDepth: 0,
			})
		}

		// Add config-defined tags not already added from discovery
		// This includes: manual PLC tags, UDT members, and any tags defined before connection
		for _, sel := range plc.Config.Tags {
			// Skip if already added from discovery
			if added[sel.Name] {
				continue
			}
			key := plc.Config.Name + ":" + sel.Name
			if excluded[key] {
				continue
			}
			added[sel.Name] = true

			// Detect UDT members by presence of dots in tag name
			dotCount := strings.Count(sel.Name, ".")

			allTags = append(allTags, tagEntry{
				plc:      plc.Config.Name,
				tag:      sel.Name,
				display:  fmt.Sprintf("%s:%s", plc.Config.Name, sel.Name),
				enabled:  sel.Enabled,
				isUDT:    dotCount > 0,
				udtDepth: dotCount,
			})
		}
	}

	// Sort by display name
	sort.Slice(allTags, func(i, j int) bool {
		return allTags[i].display < allTags[j].display
	})

	if len(allTags) == 0 {
		a.showError("No Tags", "No tags available. Connect to a PLC first.")
		return
	}

	// Create filter input
	filter := tview.NewInputField().
		SetLabel("Filter: ").
		SetFieldWidth(40)
	ApplyInputFieldTheme(filter)

	// Create list
	list := tview.NewList().
		SetHighlightFullLine(true)
	ApplyListTheme(list)

	// Populate list function
	// Filter logic: if filter contains ":", split into PLC filter (left) and tag filter (right)
	// Both parts are matched independently: "logix:data" matches PLC containing "logix" AND tag containing "data"
	populateList := func(filterText string) {
		list.Clear()
		filterLower := strings.ToLower(filterText)

		var plcFilter, tagFilter string
		if idx := strings.Index(filterLower, ":"); idx != -1 {
			plcFilter = strings.TrimSpace(filterLower[:idx])
			tagFilter = strings.TrimSpace(filterLower[idx+1:])
		} else {
			// No colon - match either PLC or tag name
			plcFilter = filterLower
			tagFilter = filterLower
		}

		for _, entry := range allTags {
			plcLower := strings.ToLower(entry.plc)
			tagLower := strings.ToLower(entry.tag)

			var match bool
			if strings.Contains(filterText, ":") {
				// Both PLC and tag must match their respective filters
				plcMatch := plcFilter == "" || strings.Contains(plcLower, plcFilter)
				tagMatch := tagFilter == "" || strings.Contains(tagLower, tagFilter)
				match = plcMatch && tagMatch
			} else {
				// No colon - match if filter appears in either PLC or tag
				match = filterText == "" ||
					strings.Contains(plcLower, filterLower) ||
					strings.Contains(tagLower, filterLower)
			}

			if match {
				plcName := entry.plc
				tagName := entry.tag
				displayText := entry.display

				// Add UDT member indicator with depth visualization
				if entry.isUDT {
					// Show nesting depth: ⊳ for depth 1, ⊳⊳ for depth 2, etc.
					prefix := strings.Repeat("⊳", entry.udtDepth) + " "
					displayText = "[::d]" + prefix + "[::-]" + entry.display
				}

				// Strike-through for tags not enabled in Browser
				if !entry.enabled {
					displayText = "[::s]" + displayText + "[::-]"
				}

				list.AddItem(displayText, "", 0, func() {
					a.closeModal(pageName)
					onSelect(plcName, tagName)
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
			a.app.SetFocus(list)
			return nil
		case tcell.KeyEscape:
			a.closeModal(pageName)
			return nil
		case tcell.KeyEnter:
			// Select first item if any
			if list.GetItemCount() > 0 {
				list.SetCurrentItem(0)
				a.app.SetFocus(list)
			}
			return nil
		}
		return event
	})

	list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			a.closeModal(pageName)
			return nil
		case tcell.KeyUp:
			if list.GetCurrentItem() == 0 {
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

	// Layout
	content := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(filter, 1, 0, true).
		AddItem(list, 0, 1, false)
	content.SetBorder(true).SetTitle(" " + title + " (/ to filter, Enter to select) ")
	content.SetBorderColor(CurrentTheme.Border).SetTitleColor(CurrentTheme.Accent)

	a.showCenteredModal(pageName, content, 70, 20)
	a.app.SetFocus(filter)
}
