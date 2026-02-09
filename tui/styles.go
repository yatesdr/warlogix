// Package tui provides the text user interface for Wargate.
package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"warlink/config"
)

// Theme defines the complete color scheme for the UI.
// It provides both tcell.Color values for direct API usage and
// pre-computed inline tag strings for tview formatted text.
type Theme struct {
	Name string

	// Core colors (tcell.Color for SetColor, SetTextColor, etc.)
	Primary    tcell.Color // Headers, selected items, links
	Secondary  tcell.Color // Secondary elements
	Accent     tcell.Color // Keyboard shortcuts, highlights
	Text       tcell.Color // Normal text
	TextDim    tcell.Color // Disabled/secondary text
	Error      tcell.Color // Errors, offline status
	Success    tcell.Color // Connected, success states
	Warning    tcell.Color // Warnings, pending states
	Border     tcell.Color // Box borders
	Background tcell.Color // Background (usually default)
	Hotkey     tcell.Color // Hotkey letters in action bars
	ActionText tcell.Color // Action bar text (non-hotkey)

	// Form field colors
	FieldBackground    tcell.Color // Background for input fields
	FieldText          tcell.Color // Text in input fields
	DropdownBackground tcell.Color // Background for dropdown when open
	DropdownSelected   tcell.Color // Selected item in dropdown

	// Special indicators
	Writable     tcell.Color // Writable tag indicator
	FormLabel    tcell.Color // Labels in forms/modals (should be bright/readable)
	ButtonText   tcell.Color // Text on buttons (should contrast with FieldBackground)
	SelectedText tcell.Color // Text color when item is selected (on Accent background)

	// Inline tag strings (for tview formatted text)
	// Format: "[#RRGGBB]" or "[colorname]"
	TagPrimary    string
	TagSecondary  string
	TagAccent     string
	TagText       string
	TagTextDim    string
	TagError      string
	TagSuccess    string
	TagWarning    string
	TagHotkey     string
	TagActionText string
	TagWritable   string
	TagReset      string // Always "[-]"

	// Pre-built status indicators
	StatusConnected    string
	StatusDisconnected string
	StatusConnecting   string
	StatusError        string
}

// colorToTag converts a tcell.Color to a tview inline color tag.
func colorToTag(c tcell.Color) string {
	// For named colors, use the hex value for consistency
	r, g, b := c.RGB()
	return fmt.Sprintf("[#%02X%02X%02X]", r, g, b)
}

// colorToHex converts a tcell.Color to a hex string (without brackets).
func colorToHex(c tcell.Color) string {
	r, g, b := c.RGB()
	return fmt.Sprintf("#%02X%02X%02X", r, g, b)
}

// NewTheme creates a theme with computed tag strings from the provided colors.
func NewTheme(name string, primary, secondary, accent, text, textDim, err, success, warning, border, bg, hotkey, actionText tcell.Color) *Theme {
	// Derive field colors from the theme palette
	// Field background: use a darker version of accent for visibility
	fieldBg := tcell.NewHexColor(0x333344) // Dark blue-gray as default
	fieldText := text                       // Same as normal text
	dropdownBg := tcell.NewHexColor(0x222233)
	dropdownSel := accent // Use accent color for selected items

	t := &Theme{
		Name:               name,
		Primary:            primary,
		Secondary:          secondary,
		Accent:             accent,
		Text:               text,
		TextDim:            textDim,
		Error:              err,
		Success:            success,
		Warning:            warning,
		Border:             border,
		Background:         bg,
		Hotkey:             hotkey,
		ActionText:         actionText,
		FieldBackground:    fieldBg,
		FieldText:          fieldText,
		DropdownBackground: dropdownBg,
		DropdownSelected:   dropdownSel,
		Writable:           warning, // Default to warning color, themes can override
		FormLabel:          primary, // Default to primary for bright labels
		ButtonText:         tcell.ColorBlack, // Default to black for contrast on light backgrounds
		SelectedText:       tcell.ColorBlack, // Default to black for selected item text
		TagReset:           "[-]",
	}

	// Compute inline tags from colors
	t.TagPrimary = colorToTag(primary)
	t.TagSecondary = colorToTag(secondary)
	t.TagAccent = colorToTag(accent)
	t.TagText = colorToTag(text)
	t.TagTextDim = colorToTag(textDim)
	t.TagError = colorToTag(err)
	t.TagSuccess = colorToTag(success)
	t.TagWarning = colorToTag(warning)
	t.TagHotkey = colorToTag(hotkey)
	t.TagActionText = colorToTag(actionText)
	t.TagWritable = colorToTag(warning) // Default, will be updated if Writable is set

	// Build status indicators
	t.updateStatusIndicators()

	return t
}

// updateStatusIndicators rebuilds status indicators based on ASCII mode.
func (t *Theme) updateStatusIndicators() {
	if ASCIIModeEnabled {
		// ASCII fallbacks for terminals without Unicode support
		t.StatusConnected = t.TagSuccess + "[*]" + t.TagReset
		t.StatusDisconnected = t.TagTextDim + "[ ]" + t.TagReset
		t.StatusConnecting = t.TagWarning + "[~]" + t.TagReset
		t.StatusError = t.TagError + "[!]" + t.TagReset
	} else {
		// Unicode indicators
		t.StatusConnected = t.TagSuccess + "●" + t.TagReset
		t.StatusDisconnected = t.TagTextDim + "○" + t.TagReset
		t.StatusConnecting = t.TagWarning + "◐" + t.TagReset
		t.StatusError = t.TagError + "●" + t.TagReset
	}
}

// NewThemeWithFieldColors creates a theme with explicit field colors.
func NewThemeWithFieldColors(name string, primary, secondary, accent, text, textDim, err, success, warning, border, bg, hotkey, actionText, fieldBg, fieldText, dropdownBg, dropdownSel tcell.Color) *Theme {
	t := NewTheme(name, primary, secondary, accent, text, textDim, err, success, warning, border, bg, hotkey, actionText)
	t.FieldBackground = fieldBg
	t.FieldText = fieldText
	t.DropdownBackground = dropdownBg
	t.DropdownSelected = dropdownSel
	return t
}

// Helper methods for common formatting patterns

// Label formats a label with its value: "Label: value"
func (t *Theme) Label(label, value string) string {
	return t.TagAccent + label + ":" + t.TagReset + " " + value
}

// Shortcut formats a keyboard shortcut: "a" in accent color
func (t *Theme) Shortcut(key string) string {
	return t.TagAccent + key + t.TagReset
}

// ShortcutLabel formats a shortcut with its action: "a add"
func (t *Theme) ShortcutLabel(key, action string) string {
	return t.TagHotkey + key + t.TagActionText + action + t.TagReset
}

// ErrorText formats error text
func (t *Theme) ErrorText(msg string) string {
	return t.TagError + msg + t.TagReset
}

// SuccessText formats success text
func (t *Theme) SuccessText(msg string) string {
	return t.TagSuccess + msg + t.TagReset
}

// Dim formats dimmed/disabled text
func (t *Theme) Dim(msg string) string {
	return t.TagTextDim + msg + t.TagReset
}

// Predefined themes

// ThemeDefault - High-compatibility theme using basic ANSI colors
// Works with any terminal including TTY over SSH
var ThemeDefault = func() *Theme {
	t := NewTheme(
		"default",
		tcell.ColorWhite,   // Primary: White (headers)
		tcell.ColorGreen,   // Secondary: Green (enabled/publishing items)
		tcell.ColorTeal,    // Accent: Teal (selected tab, selector)
		tcell.ColorSilver,  // Text: Silver (non-publishing items)
		tcell.ColorGray,    // TextDim: Dark gray (disabled, unselected tabs)
		tcell.ColorRed,     // Error: Red
		tcell.ColorGreen,   // Success: Green
		tcell.ColorYellow,  // Warning: Yellow
		tcell.ColorSilver,  // Border: Silver
		tcell.ColorDefault, // Background: Terminal default
		tcell.ColorYellow,  // Hotkey: Yellow
		tcell.ColorSilver,  // ActionText: Silver
	)
	t.FieldBackground = tcell.ColorGray      // Dark gray for input fields
	t.FieldText = tcell.ColorWhite           // White text in fields
	t.DropdownBackground = tcell.ColorGray   // Dark gray for dropdown list
	t.DropdownSelected = tcell.ColorSilver   // Silver for selected item
	t.Writable = tcell.ColorRed              // Red for writable indicator
	t.TagWritable = colorToTag(t.Writable)
	t.FormLabel = tcell.ColorWhite           // White labels in forms
	t.ButtonText = tcell.ColorWhite          // White text on buttons
	t.SelectedText = tcell.ColorWhite        // White text on teal selection for contrast
	return t
}()

// ThemeRetro - Classic CRT terminal with phosphor green and black
var ThemeRetro = func() *Theme {
	t := NewTheme(
		"retro",
		tcell.NewHexColor(0x33FF33), // Primary: Bright green (tabs)
		tcell.NewHexColor(0x88FF88), // Secondary: Very bright green (enabled/publishing items)
		tcell.NewHexColor(0x33FF33), // Accent: Bright green (selector)
		tcell.NewHexColor(0x00BB00), // Text: Medium green (unselected items)
		tcell.NewHexColor(0x006600), // TextDim: Dim green (disabled)
		tcell.NewHexColor(0xFF3333), // Error: Red
		tcell.NewHexColor(0x33FF33), // Success: Bright green
		tcell.NewHexColor(0xFFAA00), // Warning: Amber
		tcell.NewHexColor(0x00AA00), // Border: Medium green
		tcell.ColorDefault,          // Background
		tcell.NewHexColor(0x33FF33), // Hotkey: Bright green
		tcell.NewHexColor(0x00AA00), // ActionText: Medium green
	)
	t.FieldBackground = tcell.NewHexColor(0x002200)    // Very dark green
	t.FieldText = tcell.NewHexColor(0x33FF33)          // Bright green
	t.DropdownBackground = tcell.NewHexColor(0x001100) // Near black green
	t.DropdownSelected = tcell.NewHexColor(0x33FF33)   // Bright green
	t.Writable = tcell.NewHexColor(0x33FF33)           // Bright green for writable
	t.TagWritable = colorToTag(t.Writable)
	t.ButtonText = tcell.NewHexColor(0x33FF33)         // Bright green on dark buttons
	return t
}()

// ThemeMono - Blue IBM terminal aesthetic
var ThemeMono = func() *Theme {
	t := NewTheme(
		"mono",
		tcell.NewHexColor(0x5B9BD5), // Primary: IBM blue (tabs)
		tcell.NewHexColor(0x00BFFF), // Secondary: Deep sky blue (publishing items)
		tcell.NewHexColor(0x87CEEB), // Accent: Sky blue (section headers)
		tcell.NewHexColor(0xADD8E6), // Text: Light blue (non-publishing items)
		tcell.NewHexColor(0x4682B4), // TextDim: Steel blue
		tcell.NewHexColor(0xFF6B6B), // Error: Coral red
		tcell.NewHexColor(0x00BFFF), // Success: Deep sky blue
		tcell.NewHexColor(0xFFD966), // Warning: Gold
		tcell.NewHexColor(0x5B9BD5), // Border: IBM blue
		tcell.ColorDefault,          // Background
		tcell.NewHexColor(0x87CEEB), // Hotkey: Sky blue
		tcell.NewHexColor(0x6CA0C0), // ActionText: Medium blue
	)
	t.FieldBackground = tcell.NewHexColor(0x1A2A3A)    // Dark blue
	t.FieldText = tcell.NewHexColor(0xADD8E6)          // Light blue
	t.DropdownBackground = tcell.NewHexColor(0x0F1A25) // Darker blue
	t.DropdownSelected = tcell.NewHexColor(0x5B9BD5)   // IBM blue
	t.Writable = tcell.NewHexColor(0x87CEEB)           // Sky blue for writable
	t.TagWritable = colorToTag(t.Writable)
	return t
}()

// ThemeAmber - Warm amber CRT with orange accents
var ThemeAmber = func() *Theme {
	t := NewTheme(
		"amber",
		tcell.NewHexColor(0xFFBF00), // Primary: Amber (tabs)
		tcell.NewHexColor(0xFFD700), // Secondary: Gold (publishing items)
		tcell.NewHexColor(0xFF8C00), // Accent: Dark orange (section headers)
		tcell.NewHexColor(0xFFE4B5), // Text: Moccasin/pale amber (non-publishing items)
		tcell.NewHexColor(0xB8860B), // TextDim: Dark goldenrod
		tcell.NewHexColor(0xFF4444), // Error: Red
		tcell.NewHexColor(0xFFD700), // Success: Gold
		tcell.NewHexColor(0xFF8C00), // Warning: Dark orange
		tcell.NewHexColor(0xFFBF00), // Border: Amber
		tcell.ColorDefault,          // Background
		tcell.NewHexColor(0xFF8C00), // Hotkey: Dark orange
		tcell.NewHexColor(0xDAA520), // ActionText: Goldenrod
	)
	t.FieldBackground = tcell.NewHexColor(0x2A1A0A)    // Dark brown
	t.FieldText = tcell.NewHexColor(0xFFE4B5)          // Moccasin
	t.DropdownBackground = tcell.NewHexColor(0x1A0F05) // Darker brown
	t.DropdownSelected = tcell.NewHexColor(0xFFBF00)   // Amber
	t.Writable = tcell.NewHexColor(0xFFD700)           // Gold for writable
	t.TagWritable = colorToTag(t.Writable)
	return t
}()

// ThemeHighContrast is designed for accessibility
var ThemeHighContrast = func() *Theme {
	t := NewTheme(
		"highcontrast",
		tcell.NewHexColor(0xFFFF00), // Primary: Yellow (tabs, headers)
		tcell.NewHexColor(0x00FF00), // Secondary: Bright Green (publishing items)
		tcell.NewHexColor(0xFFFF00), // Accent: Yellow (selector)
		tcell.NewHexColor(0xFFFFFF), // Text: White (non-publishing items)
		tcell.NewHexColor(0x888888), // TextDim: Gray (disabled)
		tcell.NewHexColor(0xFF0000), // Error: Bright Red
		tcell.NewHexColor(0x00FF00), // Success: Bright Green
		tcell.NewHexColor(0xFFFF00), // Warning: Yellow
		tcell.NewHexColor(0xFFFFFF), // Border: White
		tcell.ColorDefault,          // Background
		tcell.NewHexColor(0x00FFFF), // Hotkey: Cyan
		tcell.NewHexColor(0xFFFFFF), // ActionText: White
	)
	t.FieldBackground = tcell.NewHexColor(0x000000)    // Black
	t.FieldText = tcell.NewHexColor(0xFFFFFF)          // White
	t.DropdownBackground = tcell.NewHexColor(0x000000) // Black
	t.DropdownSelected = tcell.NewHexColor(0xFFFF00)   // Yellow
	t.Writable = tcell.NewHexColor(0x00FFFF)           // Cyan for writable
	t.TagWritable = colorToTag(t.Writable)
	t.FormLabel = tcell.NewHexColor(0xFFFF00)          // Yellow labels
	t.ButtonText = tcell.NewHexColor(0xFFFFFF)         // White on black buttons
	return t
}()

// School Themes

// ThemeVanderbilt - Vanderbilt University (Gold and Black)
// Official Vanderbilt Gold is a muted, brownish gold
var ThemeVanderbilt = func() *Theme {
	t := NewTheme(
		"vanderbilt",
		tcell.NewHexColor(0xB3A369), // Primary: Vanderbilt Gold (tabs)
		tcell.NewHexColor(0x44DD66), // Secondary: Green (enabled/publishing items)
		tcell.NewHexColor(0xB3A369), // Accent: Vanderbilt Gold (selector)
		tcell.NewHexColor(0xFFFFFF), // Text: White (non-publishing items)
		tcell.NewHexColor(0x888888), // TextDim: Gray (disabled)
		tcell.NewHexColor(0xFF4444), // Error: Red
		tcell.NewHexColor(0x44DD66), // Success: Green
		tcell.NewHexColor(0xB3A369), // Warning: Vanderbilt Gold
		tcell.NewHexColor(0xB3A369), // Border: Vanderbilt Gold
		tcell.ColorDefault,          // Background
		tcell.NewHexColor(0xD4C88E), // Hotkey: Light gold
		tcell.NewHexColor(0xCCCCCC), // ActionText: Light gray
	)
	t.FieldBackground = tcell.NewHexColor(0x4A4530)    // Lighter gold-brown
	t.FieldText = tcell.NewHexColor(0xFFFFFF)          // White
	t.DropdownBackground = tcell.NewHexColor(0x3A3520) // Medium gold-brown
	t.DropdownSelected = tcell.NewHexColor(0xB3A369)   // Vanderbilt Gold
	t.Writable = tcell.NewHexColor(0xD4C88E)           // Light gold for writable
	t.TagWritable = colorToTag(t.Writable)
	t.FormLabel = tcell.NewHexColor(0xFFFFFF)          // White labels
	t.ButtonText = tcell.NewHexColor(0xFFFFFF)         // White on dark buttons
	return t
}()

// ThemeHarvard - Harvard University (Official colors: Crimson, Black, Gray, Gold)
var ThemeHarvard = func() *Theme {
	t := NewTheme(
		"harvard",
		tcell.NewHexColor(0xA41034), // Primary: Harvard Crimson (tabs)
		tcell.NewHexColor(0x44DD66), // Secondary: Green (publishing/enabled items)
		tcell.NewHexColor(0xB6B6B6), // Accent: Light gray (selector)
		tcell.NewHexColor(0xB6B6B6), // Text: Light gray (non-publishing items)
		tcell.NewHexColor(0x808284), // TextDim: Harvard Gray (disabled)
		tcell.NewHexColor(0xFF5555), // Error: Light red
		tcell.NewHexColor(0x44DD66), // Success: Green
		tcell.NewHexColor(0xB6B6B6), // Warning: Light gray
		tcell.NewHexColor(0xA41034), // Border: Crimson
		tcell.ColorDefault,          // Background
		tcell.NewHexColor(0xC9B037), // Hotkey: Harvard Gold
		tcell.NewHexColor(0x808284), // ActionText: Harvard Gray
	)
	t.FieldBackground = tcell.NewHexColor(0x404244)    // Dark gray (darker than Harvard Gray)
	t.FieldText = tcell.NewHexColor(0xB6B6B6)          // Light gray
	t.DropdownBackground = tcell.NewHexColor(0x303234) // Darker gray
	t.DropdownSelected = tcell.NewHexColor(0xB6B6B6)   // Light gray
	t.Writable = tcell.NewHexColor(0x44DD66)           // Green for writable
	t.TagWritable = colorToTag(t.Writable)
	t.FormLabel = tcell.NewHexColor(0xB6B6B6)          // Light gray labels
	t.ButtonText = tcell.NewHexColor(0xB6B6B6)         // Light gray on buttons
	t.SelectedText = tcell.NewHexColor(0x000000)       // Black text on gray selection
	return t
}()

// ThemeLSU - Louisiana State University (Purple and Gold)
var ThemeLSU = func() *Theme {
	t := NewTheme(
		"lsu",
		tcell.NewHexColor(0x6B3FA0), // Primary: Brighter LSU Purple (tabs)
		tcell.NewHexColor(0xFDD023), // Secondary: LSU Gold (publishing items)
		tcell.NewHexColor(0xFDD023), // Accent: LSU Gold (selector)
		tcell.NewHexColor(0xFFFFFF), // Text: White (non-publishing items)
		tcell.NewHexColor(0x888888), // TextDim: Gray (disabled)
		tcell.NewHexColor(0xFF4444), // Error: Red
		tcell.NewHexColor(0xFDD023), // Success: LSU Gold
		tcell.NewHexColor(0xFDD023), // Warning: LSU Gold
		tcell.NewHexColor(0x6B3FA0), // Border: Brighter Purple
		tcell.ColorDefault,          // Background
		tcell.NewHexColor(0xFDD023), // Hotkey: LSU Gold
		tcell.NewHexColor(0xCCCCCC), // ActionText: Light gray
	)
	t.FieldBackground = tcell.NewHexColor(0x2D1850)    // Lighter dark purple
	t.FieldText = tcell.NewHexColor(0xFFFFFF)          // White
	t.DropdownBackground = tcell.NewHexColor(0x1A0A30) // Dark purple
	t.DropdownSelected = tcell.NewHexColor(0x6B3FA0)   // Brighter Purple
	t.Writable = tcell.NewHexColor(0xFDD023)           // LSU Gold for writable
	t.TagWritable = colorToTag(t.Writable)
	t.FormLabel = tcell.NewHexColor(0xFFFFFF)          // White labels
	t.ButtonText = tcell.NewHexColor(0xFFFFFF)         // White on dark buttons
	return t
}()

// Sports Team Themes

// ThemeRedWings - Detroit Red Wings (Red and White)
var ThemeRedWings = func() *Theme {
	t := NewTheme(
		"redwings",
		tcell.NewHexColor(0xCE1126), // Primary: Red Wings Red (tabs)
		tcell.NewHexColor(0x44DD66), // Secondary: Green (enabled/publishing items)
		tcell.NewHexColor(0xD0D0D0), // Accent: Silver (selector)
		tcell.NewHexColor(0xE0E0E0), // Text: Soft white (non-publishing items)
		tcell.NewHexColor(0x707070), // TextDim: Medium gray (disabled)
		tcell.NewHexColor(0xFF6666), // Error: Lighter red (distinct from theme red)
		tcell.NewHexColor(0x44DD66), // Success: Green
		tcell.NewHexColor(0xFFCC00), // Warning: Gold
		tcell.NewHexColor(0xCE1126), // Border: Red Wings Red
		tcell.ColorDefault,          // Background
		tcell.NewHexColor(0xFFFFFF), // Hotkey: Bright white
		tcell.NewHexColor(0xA0A0A0), // ActionText: Gray
	)
	t.FieldBackground = tcell.NewHexColor(0x383838)    // Neutral dark gray
	t.FieldText = tcell.NewHexColor(0xE0E0E0)          // Soft white
	t.DropdownBackground = tcell.NewHexColor(0x282828) // Darker gray
	t.DropdownSelected = tcell.NewHexColor(0xD0D0D0)   // Silver
	t.Writable = tcell.NewHexColor(0xCE1126)           // Red Wings Red for writable
	t.TagWritable = colorToTag(t.Writable)
	t.FormLabel = tcell.NewHexColor(0xE0E0E0)          // Soft white labels
	t.ButtonText = tcell.NewHexColor(0xE0E0E0)         // Soft white on buttons
	t.SelectedText = tcell.NewHexColor(0x000000)       // Black text on silver selection
	return t
}()

// ThemeLions - Detroit Lions (Honolulu Blue and Silver)
var ThemeLions = func() *Theme {
	t := NewTheme(
		"lions",
		tcell.NewHexColor(0x0099DD), // Primary: Brighter Honolulu Blue (tabs)
		tcell.NewHexColor(0x44DD66), // Secondary: Green (enabled/publishing items)
		tcell.NewHexColor(0xB0B7BC), // Accent: Silver (selector)
		tcell.NewHexColor(0xE8F4FF), // Text: Pale blue-white (non-publishing items)
		tcell.NewHexColor(0x7090A0), // TextDim: Blue-gray (disabled)
		tcell.NewHexColor(0xFF4444), // Error: Red
		tcell.NewHexColor(0x44DD66), // Success: Green
		tcell.NewHexColor(0xFFCC00), // Warning: Gold
		tcell.NewHexColor(0x0099DD), // Border: Brighter Blue
		tcell.ColorDefault,          // Background
		tcell.NewHexColor(0xB0B7BC), // Hotkey: Silver
		tcell.NewHexColor(0xC0D0DD), // ActionText: Blue-tinted gray
	)
	t.FieldBackground = tcell.NewHexColor(0x404040)    // Dark gray
	t.FieldText = tcell.NewHexColor(0xE8F4FF)          // Pale blue-white
	t.DropdownBackground = tcell.NewHexColor(0x303030) // Darker gray
	t.DropdownSelected = tcell.NewHexColor(0xB0B7BC)   // Silver
	t.Writable = tcell.NewHexColor(0xB0B7BC)           // Silver for writable
	t.TagWritable = colorToTag(t.Writable)
	t.FormLabel = tcell.NewHexColor(0xE8F4FF)          // Pale blue-white labels
	t.ButtonText = tcell.NewHexColor(0xE8F4FF)         // Pale blue-white on buttons
	t.SelectedText = tcell.NewHexColor(0x000000)       // Black text on silver selection
	return t
}()

// ThemeSpartans - Michigan State Spartans (Green and White)
var ThemeSpartans = func() *Theme {
	t := NewTheme(
		"spartans",
		tcell.NewHexColor(0x2E8B57), // Primary: Brighter Spartan Green (tabs)
		tcell.NewHexColor(0x44DD66), // Secondary: Bright green (enabled/publishing items)
		tcell.NewHexColor(0xFFFFFF), // Accent: White (selector)
		tcell.NewHexColor(0xE0E0E0), // Text: Soft white (non-publishing items)
		tcell.NewHexColor(0x888888), // TextDim: Gray (disabled)
		tcell.NewHexColor(0xFF4444), // Error: Red
		tcell.NewHexColor(0x44DD66), // Success: Bright green
		tcell.NewHexColor(0xFFCC00), // Warning: Gold
		tcell.NewHexColor(0x2E8B57), // Border: Brighter Spartan Green
		tcell.ColorDefault,          // Background
		tcell.NewHexColor(0xFFFFFF), // Hotkey: White
		tcell.NewHexColor(0xCCCCCC), // ActionText: Light gray
	)
	t.FieldBackground = tcell.NewHexColor(0x0A2A1A)    // Dark green
	t.FieldText = tcell.NewHexColor(0xFFFFFF)          // White
	t.DropdownBackground = tcell.NewHexColor(0x051510) // Darker green
	t.DropdownSelected = tcell.NewHexColor(0x2E8B57)   // Brighter Spartan Green
	t.Writable = tcell.NewHexColor(0xFFFFFF)           // White for writable
	t.TagWritable = colorToTag(t.Writable)
	t.FormLabel = tcell.NewHexColor(0xFFFFFF)          // White labels
	t.ButtonText = tcell.NewHexColor(0xFFFFFF)         // White on dark buttons
	return t
}()

// ThemeTigers - Detroit Tigers (Navy Blue and Orange)
var ThemeTigers = func() *Theme {
	t := NewTheme(
		"tigers",
		tcell.NewHexColor(0x1E3A5F), // Primary: Brighter Navy (tabs)
		tcell.NewHexColor(0xFF6633), // Secondary: Brighter Orange (publishing items)
		tcell.NewHexColor(0xFF6633), // Accent: Brighter Orange (selector)
		tcell.NewHexColor(0xFFFFFF), // Text: White (non-publishing items)
		tcell.NewHexColor(0x888888), // TextDim: Gray (disabled)
		tcell.NewHexColor(0xFF4444), // Error: Red
		tcell.NewHexColor(0xFF6633), // Success: Brighter Orange
		tcell.NewHexColor(0xFFAA66), // Warning: Light orange
		tcell.NewHexColor(0x1E3A5F), // Border: Brighter Navy
		tcell.ColorDefault,          // Background
		tcell.NewHexColor(0xFF6633), // Hotkey: Brighter Orange
		tcell.NewHexColor(0xCCCCCC), // ActionText: Light gray
	)
	t.FieldBackground = tcell.NewHexColor(0x152535)    // Lighter navy
	t.FieldText = tcell.NewHexColor(0xFFFFFF)          // White
	t.DropdownBackground = tcell.NewHexColor(0x0A1520) // Dark navy
	t.DropdownSelected = tcell.NewHexColor(0xFF6633)   // Brighter Orange
	t.Writable = tcell.NewHexColor(0xFF6633)           // Brighter Orange for writable
	t.TagWritable = colorToTag(t.Writable)
	t.FormLabel = tcell.NewHexColor(0xFFFFFF)          // White labels
	t.ButtonText = tcell.NewHexColor(0xFFFFFF)         // White on dark buttons
	return t
}()

// AvailableThemes lists all built-in themes
var AvailableThemes = map[string]*Theme{
	"default":      ThemeDefault,
	"retro":        ThemeRetro,
	"mono":         ThemeMono,
	"amber":        ThemeAmber,
	"highcontrast": ThemeHighContrast,
	"vanderbilt":   ThemeVanderbilt,
	"harvard":      ThemeHarvard,
	"lsu":          ThemeLSU,
	"redwings":     ThemeRedWings,
	"lions":        ThemeLions,
	"spartans":     ThemeSpartans,
	"tigers":       ThemeTigers,
}

// ThemeOrder defines the order for cycling through themes
var ThemeOrder = []string{"default", "retro", "mono", "amber", "highcontrast", "vanderbilt", "harvard", "lsu", "redwings", "lions", "spartans", "tigers"}

// CurrentTheme is the active theme used by all UI components
var CurrentTheme = ThemeDefault

// currentThemeIndex tracks position in ThemeOrder for cycling
var currentThemeIndex = 0

func init() {
	// Apply form styles for default theme at startup
	ApplyFormStyles()
}

// SetTheme changes the current theme by name. Returns false if theme not found.
func SetTheme(name string) bool {
	if theme, ok := AvailableThemes[name]; ok {
		CurrentTheme = theme
		// Update index to match
		for i, n := range ThemeOrder {
			if n == name {
				currentThemeIndex = i
				break
			}
		}
		ApplyFormStyles()
		return true
	}
	return false
}

// NextTheme cycles to the next theme and returns its name.
func NextTheme() string {
	currentThemeIndex = (currentThemeIndex + 1) % len(ThemeOrder)
	name := ThemeOrder[currentThemeIndex]
	CurrentTheme = AvailableThemes[name]
	ApplyFormStyles()
	return name
}

// GetThemeName returns the current theme name.
func GetThemeName() string {
	return ThemeOrder[currentThemeIndex]
}

// ApplyFormStyles updates tview's global styles to match the current theme.
// This affects input fields, dropdowns, and other form elements.
func ApplyFormStyles() {
	th := CurrentTheme

	// Set tview's global styles for form fields
	tview.Styles.PrimitiveBackgroundColor = tcell.ColorDefault
	tview.Styles.ContrastBackgroundColor = th.FieldBackground
	tview.Styles.MoreContrastBackgroundColor = th.DropdownBackground
	tview.Styles.PrimaryTextColor = th.FormLabel
	tview.Styles.SecondaryTextColor = th.TextDim
	tview.Styles.TertiaryTextColor = th.Accent
	tview.Styles.InverseTextColor = tcell.ColorBlack
	tview.Styles.ContrastSecondaryTextColor = th.FieldText
	tview.Styles.BorderColor = th.Border
	tview.Styles.TitleColor = th.Accent
	tview.Styles.GraphicsColor = th.TextDim
}

// ApplyInputFieldTheme applies the current theme colors to an InputField.
// Call this in RefreshTheme() for each input field.
func ApplyInputFieldTheme(field *tview.InputField) {
	th := CurrentTheme
	field.SetFieldBackgroundColor(th.FieldBackground)
	field.SetFieldTextColor(th.FieldText)
	field.SetLabelColor(th.FormLabel)
}

// ApplyDropDownTheme applies the current theme colors to a DropDown.
// Call this in RefreshTheme() for each dropdown.
func ApplyDropDownTheme(dropdown *tview.DropDown) {
	th := CurrentTheme
	dropdown.SetFieldBackgroundColor(th.FieldBackground)
	dropdown.SetFieldTextColor(th.FieldText)
	dropdown.SetLabelColor(th.FormLabel)
	// SetListStyles takes (unselected style, selected style)
	// Use field text color on dark background for unselected, SelectedText on accent for selected
	unselected := tcell.StyleDefault.Background(th.DropdownBackground).Foreground(th.FieldText)
	selected := tcell.StyleDefault.Background(th.DropdownSelected).Foreground(th.SelectedText)
	dropdown.SetListStyles(unselected, selected)
}

// ApplyTreeViewTheme applies the current theme colors to a TreeView.
// Call this in RefreshTheme() for each tree view.
func ApplyTreeViewTheme(tree *tview.TreeView) {
	th := CurrentTheme
	tree.SetGraphicsColor(th.TextDim)
}

// ApplyTableTheme applies the current theme colors to a Table.
// Call this in RefreshTheme() for each table.
func ApplyTableTheme(table *tview.Table) {
	th := CurrentTheme
	// Set selected row style: SelectedText on accent background for good contrast
	table.SetSelectedStyle(tcell.StyleDefault.Foreground(th.SelectedText).Background(th.Accent))
}

// ApplyButtonTheme applies the current theme colors to a Button.
// Call this in RefreshTheme() for each button.
func ApplyButtonTheme(button *tview.Button) {
	th := CurrentTheme
	button.SetBackgroundColor(th.FieldBackground)
	button.SetLabelColor(th.ButtonText)
	button.SetLabelColorActivated(th.SelectedText)
	button.SetBackgroundColorActivated(th.Accent)
	// Use SetStyle for full control over button appearance
	button.SetStyle(tcell.StyleDefault.Foreground(th.ButtonText).Background(th.FieldBackground))
	button.SetActivatedStyle(tcell.StyleDefault.Foreground(th.SelectedText).Background(th.Accent))
}

// ApplyFormTheme applies the current theme colors to a Form.
// Call this after creating a form to ensure consistent styling.
func ApplyFormTheme(form *tview.Form) {
	th := CurrentTheme
	form.SetBackgroundColor(tcell.ColorDefault)
	form.SetBorderColor(th.Border)
	form.SetTitleColor(th.Accent)
	form.SetLabelColor(th.FormLabel)
	form.SetFieldBackgroundColor(th.FieldBackground)
	form.SetFieldTextColor(th.FieldText)
	form.SetButtonBackgroundColor(th.FieldBackground)
	form.SetButtonTextColor(th.ButtonText)
	form.SetButtonActivatedStyle(tcell.StyleDefault.Foreground(th.SelectedText).Background(th.Accent))
}

// ApplyListTheme applies the current theme colors to a List.
// Call this in RefreshTheme() for each list.
func ApplyListTheme(list *tview.List) {
	th := CurrentTheme
	list.SetMainTextColor(th.Text)
	list.SetSecondaryTextColor(th.TextDim)
	list.SetSelectedTextColor(th.SelectedText)
	list.SetSelectedBackgroundColor(th.Accent)
}

// ApplyModalTheme applies the current theme to a modal dialog.
func ApplyModalTheme(modal *tview.Modal) {
	th := CurrentTheme
	modal.SetBackgroundColor(th.Background)
	modal.SetTextColor(th.Text)
	modal.SetButtonBackgroundColor(th.FieldBackground)
	modal.SetButtonTextColor(th.ButtonText) // Use ButtonText for proper contrast on FieldBackground
	modal.SetButtonStyle(tcell.StyleDefault.Foreground(th.ButtonText).Background(th.FieldBackground))
	modal.SetButtonActivatedStyle(tcell.StyleDefault.Foreground(th.SelectedText).Background(th.Accent))
	modal.SetBorderColor(th.Border)
	modal.SetBorder(true)
}

// Legacy color variables - now derived from CurrentTheme for backwards compatibility
// These are kept for any code that directly references them
var (
	ColorPrimary    = CurrentTheme.Primary
	ColorSecondary  = CurrentTheme.Secondary
	ColorAccent     = CurrentTheme.Accent
	ColorError      = CurrentTheme.Error
	ColorDisabled   = CurrentTheme.TextDim
	ColorConnected  = CurrentTheme.Success
	ColorDisconnect = CurrentTheme.TextDim
	ColorBackground = CurrentTheme.Background
	ColorText       = CurrentTheme.Text
	ColorSelected   = CurrentTheme.Primary
)

// Legacy status indicators - now derived from CurrentTheme
// Note: These are initialized once at startup; use CurrentTheme.Status* for dynamic theming
var (
	StatusIndicatorConnected    = CurrentTheme.StatusConnected
	StatusIndicatorDisconnected = CurrentTheme.StatusDisconnected
	StatusIndicatorConnecting   = CurrentTheme.StatusConnecting
	StatusIndicatorError        = CurrentTheme.StatusError
)

// TagDisplayInfo contains display information for a tag.
type TagDisplayInfo struct {
	IsEnabled bool   // Whether the tag is enabled/monitored in Browser
	Alias     string // Tag alias if set
}

// FormatTagDisplay checks if a tag is enabled in Browser and returns its info.
// If plcTags is nil, assumes the tag is not enabled.
func FormatTagDisplay(tagName string, plcTags []config.TagSelection) TagDisplayInfo {
	info := TagDisplayInfo{
		IsEnabled: false,
		Alias:     "",
	}

	// Look up the tag in the PLC's tag selections
	if plcTags != nil {
		for _, sel := range plcTags {
			if sel.Name == tagName {
				info.IsEnabled = sel.Enabled
				info.Alias = sel.Alias
				break
			}
		}
	}

	return info
}

// Fixed indicator colors (theme-independent)
var (
	IndicatorGreen = tcell.ColorGreen
	IndicatorRed   = tcell.ColorRed
	IndicatorGray  = tcell.ColorGray
)

// Box drawing characters (Unicode defaults)
const (
	BoxHorizontal  = "─"
	BoxVertical    = "│"
	BoxTopLeft     = "┌"
	BoxTopRight    = "┐"
	BoxBottomLeft  = "└"
	BoxBottomRight = "┘"
	BoxCross       = "┼"
	BoxTeeRight    = "├"
	BoxTeeLeft     = "┤"
	BoxTeeDown     = "┬"
	BoxTeeUp       = "┴"
)

// ASCIIModeEnabled tracks whether ASCII mode is active for terminals without Unicode support.
var ASCIIModeEnabled = false

// DetectASCIIMode checks the environment to determine if ASCII mode should be enabled.
// Returns true if the locale appears to not support UTF-8.
func DetectASCIIMode() bool {
	// Check common locale environment variables
	lang := os.Getenv("LANG")
	lcAll := os.Getenv("LC_ALL")
	lcCtype := os.Getenv("LC_CTYPE")

	// Check each variable for UTF-8 support
	for _, loc := range []string{lcAll, lcCtype, lang} {
		if loc == "" {
			continue
		}
		// If any locale variable contains UTF-8/utf8, we have Unicode support
		locLower := strings.ToLower(loc)
		if strings.Contains(locLower, "utf-8") || strings.Contains(locLower, "utf8") {
			return false // Unicode supported
		}
	}

	// If LANG is "C" or "POSIX" or empty, ASCII mode is needed
	if lang == "" || lang == "C" || lang == "POSIX" {
		return true
	}

	// Default to Unicode (most modern systems support it)
	return false
}

// AutoDetectAndEnableASCIIMode checks the environment and enables ASCII mode if needed.
// Returns true if ASCII mode was enabled.
func AutoDetectAndEnableASCIIMode() bool {
	if DetectASCIIMode() {
		EnableASCIIMode()
		return true
	}
	return false
}

// EnableASCIIMode switches tview to use ASCII box-drawing characters.
// This is useful for terminals that don't render Unicode properly (e.g., SSH with limited locale settings).
func EnableASCIIMode() {
	ASCIIModeEnabled = true
	// Set tview's border characters to ASCII equivalents
	tview.Borders.Horizontal = '-'
	tview.Borders.Vertical = '|'
	tview.Borders.TopLeft = '+'
	tview.Borders.TopRight = '+'
	tview.Borders.BottomLeft = '+'
	tview.Borders.BottomRight = '+'
	tview.Borders.LeftT = '+'
	tview.Borders.RightT = '+'
	tview.Borders.TopT = '+'
	tview.Borders.BottomT = '+'
	tview.Borders.Cross = '+'
	tview.Borders.HorizontalFocus = '='
	tview.Borders.VerticalFocus = '|'
	tview.Borders.TopLeftFocus = '+'
	tview.Borders.TopRightFocus = '+'
	tview.Borders.BottomLeftFocus = '+'
	tview.Borders.BottomRightFocus = '+'
	// Update status indicators in current theme
	CurrentTheme.updateStatusIndicators()
}

// DisableASCIIMode restores tview's default Unicode box-drawing characters.
func DisableASCIIMode() {
	ASCIIModeEnabled = false
	// Restore tview's default Unicode borders
	tview.Borders.Horizontal = '─'
	tview.Borders.Vertical = '│'
	tview.Borders.TopLeft = '┌'
	tview.Borders.TopRight = '┐'
	tview.Borders.BottomLeft = '└'
	tview.Borders.BottomRight = '┘'
	tview.Borders.LeftT = '├'
	tview.Borders.RightT = '┤'
	tview.Borders.TopT = '┬'
	tview.Borders.BottomT = '┴'
	tview.Borders.Cross = '┼'
	tview.Borders.HorizontalFocus = '━'
	tview.Borders.VerticalFocus = '┃'
	tview.Borders.TopLeftFocus = '┏'
	tview.Borders.TopRightFocus = '┓'
	tview.Borders.BottomLeftFocus = '┗'
	tview.Borders.BottomRightFocus = '┛'
	// Update status indicators in current theme
	CurrentTheme.updateStatusIndicators()
}

// Tree characters (Unicode defaults - use GetTree* functions for ASCII-aware versions)
const (
	TreeBranch     = "├── "
	TreeLastBranch = "└── "
	TreeVertical   = "│   "
	TreeSpace      = "    "
	TreeExpanded   = "▼ "
	TreeCollapsed  = "▶ "
)

// Checkbox characters (Unicode defaults - use GetCheckbox* functions for ASCII-aware versions)
const (
	CheckboxChecked   = "☑"
	CheckboxUnchecked = "☐"
)

// GetTreeBranch returns the appropriate tree branch character for the current mode.
func GetTreeBranch() string {
	if ASCIIModeEnabled {
		return "+-- "
	}
	return TreeBranch
}

// GetTreeLastBranch returns the appropriate last tree branch character for the current mode.
func GetTreeLastBranch() string {
	if ASCIIModeEnabled {
		return "`-- "
	}
	return TreeLastBranch
}

// GetTreeVertical returns the appropriate tree vertical character for the current mode.
func GetTreeVertical() string {
	if ASCIIModeEnabled {
		return "|   "
	}
	return TreeVertical
}

// GetTreeExpanded returns the appropriate expanded indicator for the current mode.
func GetTreeExpanded() string {
	if ASCIIModeEnabled {
		return "v "
	}
	return TreeExpanded
}

// GetTreeCollapsed returns the appropriate collapsed indicator for the current mode.
func GetTreeCollapsed() string {
	if ASCIIModeEnabled {
		return "> "
	}
	return TreeCollapsed
}

// GetCheckboxChecked returns the appropriate checked checkbox for the current mode.
func GetCheckboxChecked() string {
	if ASCIIModeEnabled {
		// Use parentheses instead of brackets to avoid tview interpreting
		// them as color formatting tags (tview uses [color] syntax)
		return "(X)"
	}
	return CheckboxChecked
}

// GetCheckboxUnchecked returns the appropriate unchecked checkbox for the current mode.
func GetCheckboxUnchecked() string {
	if ASCIIModeEnabled {
		// Use parentheses instead of brackets to avoid tview interpreting
		// them as color formatting tags (tview uses [color] syntax)
		return "( )"
	}
	return CheckboxUnchecked
}

// GetStatusBullet returns the appropriate status bullet indicator for the current mode.
func GetStatusBullet() string {
	if ASCIIModeEnabled {
		return "*"
	}
	return "●"
}

// Tab labels
const (
	TabPLCs     = "PLCs"
	TabBrowser  = "Republisher"
	TabPacks    = "TagPacks"
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

// helpTextBase is the shared help text, with %s placeholder for quit action
const helpTextBase = `
 Keyboard Shortcuts
 ──────────────────────────────────────

 Navigation
   Shift+Tab    Cycle tabs
   P/B/T/G      PLCs / Browser / TagPacks / triGgers
   E/M/V/K/D    rEst / Mqtt / Valkey / Kafka / Debug
   Tab          Move between fields
   Enter        Select / Activate
   Space        Toggle checkbox
   Escape       Close dialog / Back
   ?            Show this help

 PLCs Tab
   d            Discover PLCs
   a            Add PLC
   e            Edit selected
   x            Remove selected
   c            Connect
   C            Disconnect
   i            Show PLC info

 Tag Browser Tab
   /            Focus filter
   c            Clear filter
   p            Focus PLC dropdown
   Space        Toggle tag publishing
   w            Toggle tag writable
   W            Write value to tag
   d            Show tag details
   a            Add manual tag (Micro800/S7/Omron)
   e            Edit manual tag (Micro800/S7/Omron)
   x            Remove manual tag (Micro800/S7/Omron)
   Escape       Return to tree

 MQTT / Valkey / Kafka Tabs
   a            Add broker/server/cluster
   e            Edit selected
   x            Remove selected
   c            Connect
   C            Disconnect

 Triggers Tab
   a            Add (trigger or tag, context-sensitive)
   x            Remove (trigger or tag, context-sensitive)
   e            Edit selected trigger
   s            Start trigger
   S            Stop trigger
   F            Fire trigger (test)

 Application
   N            Configure namespace
   F6           Cycle themes
   Q            %s
`

// GetHelpText returns the help text with the appropriate quit action
func GetHelpText(daemonMode bool) string {
	if daemonMode {
		return fmt.Sprintf(helpTextBase, "Disconnect")
	}
	return fmt.Sprintf(helpTextBase, "Quit")
}
