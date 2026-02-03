// Package tui provides the text user interface for Wargate.
package tui

import "github.com/gdamore/tcell/v2"

// Color scheme
var (
	ColorPrimary    = tcell.ColorBlue
	ColorSecondary  = tcell.ColorGreen
	ColorAccent     = tcell.ColorYellow
	ColorError      = tcell.ColorRed
	ColorDisabled   = tcell.ColorGray
	ColorConnected  = tcell.ColorGreen
	ColorDisconnect = tcell.ColorGray
	ColorBackground = tcell.ColorDefault
	ColorText       = tcell.ColorWhite
	ColorSelected   = tcell.ColorBlue
)

// Status indicator strings
const (
	StatusIndicatorConnected    = "[green]●[-]"
	StatusIndicatorDisconnected = "[gray]○[-]"
	StatusIndicatorConnecting   = "[yellow]◐[-]"
	StatusIndicatorError        = "[red]●[-]"
)

// Box drawing characters
const (
	BoxHorizontal = "─"
	BoxVertical   = "│"
	BoxTopLeft    = "┌"
	BoxTopRight   = "┐"
	BoxBottomLeft = "└"
	BoxBottomRight = "┘"
	BoxCross      = "┼"
	BoxTeeRight   = "├"
	BoxTeeLeft    = "┤"
	BoxTeeDown    = "┬"
	BoxTeeUp      = "┴"
)

// Tree characters
const (
	TreeBranch    = "├── "
	TreeLastBranch = "└── "
	TreeVertical  = "│   "
	TreeSpace     = "    "
	TreeExpanded  = "▼ "
	TreeCollapsed = "▶ "
)

// Checkbox characters
const (
	CheckboxChecked   = "☑"
	CheckboxUnchecked = "☐"
)

// Tab labels
const (
	TabPLCs     = "PLCs"
	TabBrowser  = "Tag Browser"
	TabREST     = "REST"
	TabMQTT     = "MQTT"
	TabValkey   = "Valkey"
	TabKafka    = "Kafka"
	TabTriggers = "Triggers"
	TabDebug    = "Debug"
)

// acceptDigits is a validation function for numeric input fields.
func acceptDigits(text string, lastChar rune) bool {
	if text == "" {
		return true
	}
	for _, c := range text {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// Help text
const HelpText = `
 Keyboard Shortcuts
 ──────────────────────────────────────

 Navigation
   Shift+Tab    Switch program tabs
   Tab          Move between fields
   Enter        Select / Activate
   Space        Toggle checkbox
   Escape       Close dialog / Back
   ?            Show this help

 PLCs Tab
   d            Discover PLCs
   a            Add PLC
   e            Edit selected
   r            Remove selected
   c            Connect
   C            Disconnect
   i            Show PLC info

 Tag Browser Tab
   /            Focus filter
   c            Clear filter
   p            Focus PLC dropdown
   Space        Toggle tag publishing
   w            Toggle tag writable
   d            Show tag details
   a            Add manual tag (Micro800/S7/Omron)
   e            Edit manual tag (Micro800/S7/Omron)
   x            Delete manual tag (Micro800/S7/Omron)
   Escape       Return to tree

 MQTT / Valkey / Kafka Tabs
   a            Add broker/server/cluster
   e            Edit selected
   r            Remove selected
   c            Connect
   C            Disconnect

 Triggers Tab
   a            Add trigger
   e            Edit selected
   r            Remove selected
   s            Start trigger
   S            Stop trigger
   T            Test fire trigger
   R            Reset trigger from error

 Application
   Q            Quit
`
