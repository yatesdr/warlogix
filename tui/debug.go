package tui

import (
	"fmt"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// DebugTab displays debug log messages.
type DebugTab struct {
	app       *App
	flex      *tview.Flex
	logView   *tview.TextView
	statusBar *tview.TextView
	messages  []string
	mu        sync.Mutex
	maxLines  int
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
	// Log view
	t.logView = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true)
	t.logView.SetBorder(true).SetTitle(" Debug Log ")

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
		SetText(" [yellow]c[white] clear  [yellow]g[white] top  [yellow]G[white] bottom  [yellow]↑↓[white] scroll  [gray]│[white]  [yellow]?[white] help  [yellow]Shift+Tab[white] next tab")

	// Main layout
	t.flex = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(t.logView, 0, 1, true).
		AddItem(t.statusBar, 1, 0, false)
}

// Log adds a message to the debug log.
// This is safe to call from any goroutine.
// Uses TryLock to avoid blocking - messages may be dropped if contended.
func (t *DebugTab) Log(format string, args ...interface{}) {
	// Use TryLock to avoid blocking callers (especially fire() which can block UI)
	if !t.mu.TryLock() {
		return // Drop the message rather than block
	}
	defer t.mu.Unlock()

	timestamp := time.Now().Format("15:04:05.000")
	msg := fmt.Sprintf("[gray]%s[-] %s", timestamp, fmt.Sprintf(format, args...))
	t.messages = append(t.messages, msg)

	// Trim if too many messages
	if len(t.messages) > t.maxLines {
		t.messages = t.messages[len(t.messages)-t.maxLines:]
	}

	// Don't update UI here - let Refresh() handle it to avoid threading issues
}

// LogError adds an error message to the debug log.
func (t *DebugTab) LogError(format string, args ...interface{}) {
	t.Log("[red]ERROR:[-] "+format, args...)
}

// LogInfo adds an info message to the debug log.
func (t *DebugTab) LogInfo(format string, args ...interface{}) {
	t.Log("[blue]INFO:[-] "+format, args...)
}

// LogMQTT adds an MQTT-related message to the debug log.
func (t *DebugTab) LogMQTT(format string, args ...interface{}) {
	t.Log("[green]MQTT:[-] "+format, args...)
}

// LogValkey adds a Valkey-related message to the debug log.
func (t *DebugTab) LogValkey(format string, args ...interface{}) {
	t.Log("[yellow]VALKEY:[-] "+format, args...)
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
	defer t.mu.Unlock()
	t.messages = make([]string, 0)
	t.logView.SetText("")
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
	t.Log("[cyan]KAFKA:[-] "+format, args...)
}

// DebugLogKafka logs a Kafka message to the debug tab if it exists.
func DebugLogKafka(format string, args ...interface{}) {
	if debugLogger != nil {
		debugLogger.LogKafka(format, args...)
	}
}
