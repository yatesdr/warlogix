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
	width    int
	height   int
	mu       sync.RWMutex
	resizeCb func()
	resizeMu sync.Mutex
	stopChan chan struct{}
	stopped  bool
}

// NewSSHChannelTty creates a new SSHChannelTty wrapping the given SSH channel.
func NewSSHChannelTty(channel gossh.Channel, initialWidth, initialHeight int) *SSHChannelTty {
	return &SSHChannelTty{
		channel:  channel,
		width:    initialWidth,
		height:   initialHeight,
		stopChan: make(chan struct{}),
	}
}

// Start initializes the tty for use.
// For SSH channels, the terminal is already in raw mode, so this is a no-op.
func (t *SSHChannelTty) Start() error {
	return nil
}

// Stop signals that the tty should stop.
func (t *SSHChannelTty) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.stopped {
		t.stopped = true
		close(t.stopChan)
	}
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
	return t.channel.Read(b)
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

// StopChan returns a channel that is closed when the tty is stopped.
func (t *SSHChannelTty) StopChan() <-chan struct{} {
	return t.stopChan
}

// Verify SSHChannelTty implements tcell.Tty
var _ tcell.Tty = (*SSHChannelTty)(nil)

// Verify SSHChannelTty implements io.ReadWriteCloser
var _ io.ReadWriteCloser = (*SSHChannelTty)(nil)
