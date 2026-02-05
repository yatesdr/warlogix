//go:build darwin

package tui

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/gdamore/tcell/v2"
	"golang.org/x/sys/unix"
)

// PTYTty wraps an *os.File (PTY) to implement tcell.Tty interface.
type PTYTty struct {
	file     *os.File
	saved    **unix.Termios
	sigwinch chan os.Signal
	stopChan chan struct{}
}

// NewPTYTty creates a new PTYTty wrapper around a PTY file.
func NewPTYTty(ptyFile *os.File) *PTYTty {
	return &PTYTty{
		file:     ptyFile,
		sigwinch: make(chan os.Signal, 1),
		stopChan: make(chan struct{}),
	}
}

// Start initializes the tty for use.
func (p *PTYTty) Start() error {
	// Get current terminal attributes
	termios, err := unix.IoctlGetTermios(int(p.file.Fd()), unix.TIOCGETA)
	if err != nil {
		return err
	}

	// Save the current state
	p.saved = &termios

	// Set raw mode
	rawTermios := *termios
	rawTermios.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	rawTermios.Oflag &^= unix.OPOST
	rawTermios.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	rawTermios.Cflag &^= unix.CSIZE | unix.PARENB
	rawTermios.Cflag |= unix.CS8
	rawTermios.Cc[unix.VMIN] = 1
	rawTermios.Cc[unix.VTIME] = 0

	if err := unix.IoctlSetTermios(int(p.file.Fd()), unix.TIOCSETA, &rawTermios); err != nil {
		return err
	}

	// Set up SIGWINCH handling
	signal.Notify(p.sigwinch, syscall.SIGWINCH)

	return nil
}

// Stop restores the tty to its original state.
func (p *PTYTty) Stop() error {
	signal.Stop(p.sigwinch)

	select {
	case <-p.stopChan:
		// Already closed
	default:
		close(p.stopChan)
	}

	if p.saved != nil {
		return unix.IoctlSetTermios(int(p.file.Fd()), unix.TIOCSETA, *p.saved)
	}
	return nil
}

// Drain discards any pending input.
func (p *PTYTty) Drain() error {
	// For PTY, we don't need to drain input in a special way
	return nil
}

// NotifyResize returns a channel that signals when the terminal is resized.
func (p *PTYTty) NotifyResize(cb func()) {
	go func() {
		for {
			select {
			case <-p.sigwinch:
				cb()
			case <-p.stopChan:
				return
			}
		}
	}()
}

// WindowSize returns the current window size.
func (p *PTYTty) WindowSize() (tcell.WindowSize, error) {
	ws, err := unix.IoctlGetWinsize(int(p.file.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		// Return a default size if we can't get the actual size
		return tcell.WindowSize{Width: 80, Height: 24}, nil
	}
	return tcell.WindowSize{
		Width:  int(ws.Col),
		Height: int(ws.Row),
	}, nil
}

// Read reads from the tty.
func (p *PTYTty) Read(b []byte) (int, error) {
	return p.file.Read(b)
}

// Write writes to the tty.
func (p *PTYTty) Write(b []byte) (int, error) {
	return p.file.Write(b)
}

// Close closes the tty.
func (p *PTYTty) Close() error {
	return p.file.Close()
}

// Verify PTYTty implements tcell.Tty
var _ tcell.Tty = (*PTYTty)(nil)
