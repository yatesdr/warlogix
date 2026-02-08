package tui

import (
	"fmt"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"warlogix/logging"
)

// DebugTab displays debug log messages.
type DebugTab struct {
	app        *App
	flex       *tview.Flex
	logView    *tview.TextView
	statusBar  *tview.TextView
	buttonBar  *tview.TextView
	messages   []string
	mu         sync.Mutex
	maxLines   int
	fileLogger *logging.FileLogger
}

// Global debug logger instance
var debugLogger *DebugTab

// NewDebugTab creates a new debug tab.
func NewDebugTab(app *App) *DebugTab {
	t := &DebugTab{
		app:      app,
		maxLines: 1000,
		messages: make([]string, 0),
	}
	t.setupUI()
	debugLogger = t
	return t
}

func (t *DebugTab) setupUI() {
	// Button bar
	t.buttonBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
	t.updateButtonBar()

	// Log view
	t.logView = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetTextColor(CurrentTheme.Text)
	t.logView.SetBorder(true).SetTitle(" Debug Log ").SetBorderColor(CurrentTheme.Border).SetTitleColor(CurrentTheme.Accent)

	// Auto-scroll to bottom
	t.logView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Rune() {
		case 'c', 'C':
			t.Clear()
			return nil
		case 'G':
			// Go to end
			t.logView.ScrollToEnd()
			return nil
		case 'g':
			// Go to beginning
			t.logView.ScrollToBeginning()
			return nil
		}
		return event
	})

	// Status bar
	t.statusBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextColor(CurrentTheme.Text)
	t.updateStatusBar()

	// Main layout - buttonBar at top, outside frames
	t.flex = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(t.buttonBar, 1, 0, false).
		AddItem(t.logView, 0, 1, true).
		AddItem(t.statusBar, 1, 0, false)
}

// Log adds a message to the debug log.
// This is safe to call from any goroutine.
// Uses TryLock to avoid blocking - messages may be dropped if contended.
func (t *DebugTab) Log(format string, args ...interface{}) {
	formattedMsg := fmt.Sprintf(format, args...)

	// Write to file logger if configured (always, even if TryLock fails)
	if t.fileLogger != nil {
		// Strip tview color tags for file output
		t.fileLogger.Log("%s", stripColorTags(formattedMsg))
	}

	// Use TryLock to avoid blocking callers (especially fire() which can block UI)
	if !t.mu.TryLock() {
		return // Drop the message rather than block
	}
	defer t.mu.Unlock()

	timestamp := time.Now().Format("15:04:05.000")
	msg := fmt.Sprintf("%s%s%s %s", CurrentTheme.TagTextDim, timestamp, CurrentTheme.TagReset, formattedMsg)
	t.messages = append(t.messages, msg)

	// Trim if too many messages
	if len(t.messages) > t.maxLines {
		t.messages = t.messages[len(t.messages)-t.maxLines:]
	}

	// Don't update UI here - let Refresh() handle it to avoid threading issues
}

// SetFileLogger sets a file logger for writing debug messages to disk.
// Messages are written to the file in addition to the debug buffer.
func (t *DebugTab) SetFileLogger(logger *logging.FileLogger) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.fileLogger = logger
}

// stripColorTags removes tview color tags like [red], [green], [-], etc.
func stripColorTags(s string) string {
	result := make([]byte, 0, len(s))
	inTag := false
	for i := 0; i < len(s); i++ {
		if s[i] == '[' {
			inTag = true
			continue
		}
		if s[i] == ']' && inTag {
			inTag = false
			continue
		}
		if !inTag {
			result = append(result, s[i])
		}
	}
	return string(result)
}

// LogError adds an error message to the debug log.
func (t *DebugTab) LogError(format string, args ...interface{}) {
	th := CurrentTheme
	t.Log(th.TagError+"ERROR:"+th.TagReset+" "+format, args...)
}

// LogInfo adds an info message to the debug log.
func (t *DebugTab) LogInfo(format string, args ...interface{}) {
	th := CurrentTheme
	t.Log(th.TagPrimary+"INFO:"+th.TagReset+" "+format, args...)
}

// LogMQTT adds an MQTT-related message to the debug log.
func (t *DebugTab) LogMQTT(format string, args ...interface{}) {
	th := CurrentTheme
	t.Log(th.TagSuccess+"MQTT:"+th.TagReset+" "+format, args...)
}

// LogValkey adds a Valkey-related message to the debug log.
func (t *DebugTab) LogValkey(format string, args ...interface{}) {
	th := CurrentTheme
	t.Log(th.TagAccent+"VALKEY:"+th.TagReset+" "+format, args...)
}

func (t *DebugTab) buildText() string {
	result := ""
	for _, msg := range t.messages {
		result += msg + "\n"
	}
	return result
}

// Clear clears the debug log.
func (t *DebugTab) Clear() {
	t.mu.Lock()
	t.messages = make([]string, 0)
	t.logView.SetText("")
	t.mu.Unlock()
	t.updateStatusBar()
}

// GetPrimitive returns the main primitive for this tab.
func (t *DebugTab) GetPrimitive() tview.Primitive {
	return t.flex
}

// GetFocusable returns the element that should receive focus.
func (t *DebugTab) GetFocusable() tview.Primitive {
	return t.logView
}

// Refresh updates the debug tab.
// Must be called from QueueUpdateDraw or main goroutine.
// Uses TryLock to avoid blocking the UI thread.
func (t *DebugTab) Refresh() {
	// Use TryLock to avoid blocking UI if Log() is being called
	if !t.mu.TryLock() {
		return // Skip this refresh cycle
	}
	text := t.buildText()
	msgCount := len(t.messages)
	t.mu.Unlock()

	// Only update if there are messages (avoid unnecessary redraws)
	if msgCount > 0 {
		t.logView.SetText(text)
		t.logView.ScrollToEnd()
	}
	t.statusBar.SetText(fmt.Sprintf(" %d log lines (max %d)", msgCount, t.maxLines))
}

// DebugLog logs a message to the debug tab if it exists.
func DebugLog(format string, args ...interface{}) {
	if debugLogger != nil {
		debugLogger.Log(format, args...)
	}
}

// DebugLogMQTT logs an MQTT message to the debug tab if it exists.
func DebugLogMQTT(format string, args ...interface{}) {
	if debugLogger != nil {
		debugLogger.LogMQTT(format, args...)
	}
}

// DebugLogError logs an error to the debug tab if it exists.
func DebugLogError(format string, args ...interface{}) {
	if debugLogger != nil {
		debugLogger.LogError(format, args...)
	}
}

// DebugLogValkey logs a Valkey message to the debug tab if it exists.
func DebugLogValkey(format string, args ...interface{}) {
	if debugLogger != nil {
		debugLogger.LogValkey(format, args...)
	}
}

// LogKafka adds a Kafka-related message to the debug log.
func (t *DebugTab) LogKafka(format string, args ...interface{}) {
	th := CurrentTheme
	t.Log(th.TagAccent+"KAFKA:"+th.TagReset+" "+format, args...)
}

// DebugLogKafka logs a Kafka message to the debug tab if it exists.
func DebugLogKafka(format string, args ...interface{}) {
	if debugLogger != nil {
		debugLogger.LogKafka(format, args...)
	}
}

// LogSSH adds an SSH-related message to the debug log.
func (t *DebugTab) LogSSH(format string, args ...interface{}) {
	th := CurrentTheme
	t.Log(th.TagSecondary+"SSH:"+th.TagReset+" "+format, args...)
}

// DebugLogSSH logs an SSH message to the debug tab if it exists.
func DebugLogSSH(format string, args ...interface{}) {
	if debugLogger != nil {
		debugLogger.LogSSH(format, args...)
	}
}

// LogLogix adds a Logix-related message to the debug log.
func (t *DebugTab) LogLogix(format string, args ...interface{}) {
	th := CurrentTheme
	t.Log(th.TagSecondary+"Logix:"+th.TagReset+" "+format, args...)
}

// DebugLogLogix logs a Logix message to the debug tab if it exists.
func DebugLogLogix(format string, args ...interface{}) {
	if debugLogger != nil {
		debugLogger.LogLogix(format, args...)
	}
}

// SetDebugFileLogger sets a file logger for the global debug logger.
func SetDebugFileLogger(logger *logging.FileLogger) {
	if debugLogger != nil {
		debugLogger.SetFileLogger(logger)
	}
}

func (t *DebugTab) updateStatusBar() {
	t.mu.Lock()
	lineCount := len(t.messages)
	t.mu.Unlock()
	t.statusBar.SetText(fmt.Sprintf(" %d log lines (max %d)", lineCount, t.maxLines))
}

func (t *DebugTab) updateButtonBar() {
	th := CurrentTheme
	buttonText := " " + th.TagHotkey + "c" + th.TagActionText + "lear  " +
		th.TagHotkey + "g" + th.TagActionText + " top  " +
		th.TagHotkey + "G" + th.TagActionText + " bottom  " +
		th.TagHotkey + "↑↓" + th.TagActionText + " scroll  " +
		th.TagActionText + "│  " +
		th.TagHotkey + "?" + th.TagActionText + " help  " +
		th.TagHotkey + "Shift+Tab" + th.TagActionText + " next tab " + th.TagReset
	t.buttonBar.SetText(buttonText)
}

// RefreshTheme updates theme-dependent UI elements.
func (t *DebugTab) RefreshTheme() {
	t.updateButtonBar()
	t.updateStatusBar()
	th := CurrentTheme
	t.logView.SetBorderColor(th.Border).SetTitleColor(th.Accent)
	t.logView.SetTextColor(th.Text)
	t.statusBar.SetTextColor(th.Text)
}
