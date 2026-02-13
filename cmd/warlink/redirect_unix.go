//go:build !windows

package main

import (
	"os"
	"syscall"
)

// redirectStderr redirects stderr to the given file using dup2.
func redirectStderr(f *os.File) {
	syscall.Dup2(int(f.Fd()), int(os.Stderr.Fd()))
}
