package ssh

import (
	"io"
	"sync"

	"github.com/gdamore/tcell/v2"
	gossh "golang.org/x/crypto/ssh"
)

// SSHChannelTty wraps an SSH channel to implement tcell.Tty interface.
// This allows tcell to use an SSH channel as a terminal.
type SSHChannelTty struct {
	channel  gossh.Channel
	term     string
	width    int
	height   int
	mu       sync.RWMutex
	resizeCb func()
	resizeMu sync.Mutex
	stopped  bool
}

// NewSSHChannelTty creates a new SSHChannelTty wrapping the given SSH channel.
func NewSSHChannelTty(channel gossh.Channel, term string, initialWidth, initialHeight int) *SSHChannelTty {
	// Default to xterm-256color if no term specified
	if term == "" {
		term = "xterm-256color"
	}
	return &SSHChannelTty{
		channel: channel,
		term:    term,
		width:   initialWidth,
		height:  initialHeight,
	}
}

// Term returns the terminal type.
func (t *SSHChannelTty) Term() string {
	return t.term
}

// Start initializes the tty for use.
// For SSH channels, the terminal is already in raw mode, so this is a no-op.
func (t *SSHChannelTty) Start() error {
	return nil
}

// Stop signals that the tty should stop.
// It sets a flag that causes Read() to return EOF on the next call.
// The channel is NOT closed here - it stays open so screen.Fini() can
// send terminal restore sequences.
func (t *SSHChannelTty) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopped = true
	return nil
}

// Drain discards any pending input.
// For SSH channels, we don't need special draining.
func (t *SSHChannelTty) Drain() error {
	return nil
}

// NotifyResize registers a callback to be called when the terminal is resized.
func (t *SSHChannelTty) NotifyResize(cb func()) {
	t.resizeMu.Lock()
	t.resizeCb = cb
	t.resizeMu.Unlock()
}

// WindowSize returns the current window size.
func (t *SSHChannelTty) WindowSize() (tcell.WindowSize, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return tcell.WindowSize{
		Width:  t.width,
		Height: t.height,
	}, nil
}

// SetWindowSize updates the window size and triggers the resize callback.
// This is called by the SSH server when it receives a window-change request.
func (t *SSHChannelTty) SetWindowSize(width, height int) {
	t.mu.Lock()
	t.width = width
	t.height = height
	t.mu.Unlock()

	t.resizeMu.Lock()
	cb := t.resizeCb
	t.resizeMu.Unlock()

	if cb != nil {
		cb()
	}
}

// Read reads from the SSH channel.
func (t *SSHChannelTty) Read(b []byte) (int, error) {
	// Check if stopped before reading
	t.mu.RLock()
	stopped := t.stopped
	t.mu.RUnlock()
	if stopped {
		return 0, io.EOF
	}
	n, err := t.channel.Read(b)
	// If the channel was closed while reading, return EOF
	if err != nil {
		t.mu.RLock()
		stopped = t.stopped
		t.mu.RUnlock()
		if stopped {
			return 0, io.EOF
		}
	}
	return n, err
}

// Write writes to the SSH channel.
func (t *SSHChannelTty) Write(b []byte) (int, error) {
	return t.channel.Write(b)
}

// Close closes the SSH channel.
func (t *SSHChannelTty) Close() error {
	t.Stop()
	return t.channel.Close()
}

// Stopped returns true if the tty has been stopped.
func (t *SSHChannelTty) Stopped() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.stopped
}

// Verify SSHChannelTty implements tcell.Tty
var _ tcell.Tty = (*SSHChannelTty)(nil)

// Verify SSHChannelTty implements io.ReadWriteCloser
var _ io.ReadWriteCloser = (*SSHChannelTty)(nil)
