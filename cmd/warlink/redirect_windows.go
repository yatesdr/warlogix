//go:build windows

package main

import "os"

// redirectStderr is a no-op on Windows where dup2 is not available.
func redirectStderr(f *os.File) {}
