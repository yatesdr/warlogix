//go:build windows

package tui

import (
	"errors"
	"os"

	"github.com/gdamore/tcell/v2"
)

// PTYTty is a stub for Windows which doesn't support Unix PTY.
// SSH daemon mode is not supported on Windows.
type PTYTty struct {
	file *os.File
}

// NewPTYTty creates a new PTYTty wrapper. On Windows, this returns a stub.
func NewPTYTty(ptyFile *os.File) *PTYTty {
	return &PTYTty{file: ptyFile}
}

// Start is not supported on Windows.
func (p *PTYTty) Start() error {
	return errors.New("PTY not supported on Windows")
}

// Stop is a no-op on Windows.
func (p *PTYTty) Stop() error {
	return nil
}

// Drain is a no-op on Windows.
func (p *PTYTty) Drain() error {
	return nil
}

// NotifyResize is a no-op on Windows.
func (p *PTYTty) NotifyResize(cb func()) {
}

// WindowSize returns a default size on Windows.
func (p *PTYTty) WindowSize() (tcell.WindowSize, error) {
	return tcell.WindowSize{Width: 80, Height: 24}, nil
}

// Read reads from the file.
func (p *PTYTty) Read(b []byte) (int, error) {
	if p.file == nil {
		return 0, errors.New("PTY not supported on Windows")
	}
	return p.file.Read(b)
}

// Write writes to the file.
func (p *PTYTty) Write(b []byte) (int, error) {
	if p.file == nil {
		return 0, errors.New("PTY not supported on Windows")
	}
	return p.file.Write(b)
}

// Close closes the file.
func (p *PTYTty) Close() error {
	if p.file == nil {
		return nil
	}
	return p.file.Close()
}

// Verify PTYTty implements tcell.Tty
var _ tcell.Tty = (*PTYTty)(nil)
