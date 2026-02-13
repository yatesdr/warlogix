//go:build !windows

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// redirectStderr redirects stderr to the given file using dup2.
// Uses x/sys/unix which handles arm64 (dup3) transparently.
func redirectStderr(f *os.File) {
	unix.Dup2(int(f.Fd()), int(os.Stderr.Fd()))
}
