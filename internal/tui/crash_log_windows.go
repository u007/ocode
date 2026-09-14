//go:build windows

package tui

import "os"

// redirectStderr swaps os.Stderr only; the Windows runtime writes panics to
// the process's stderr handle, which os.Stderr reassignment does not move.
func redirectStderr(f *os.File) error {
	os.Stderr = f
	return nil
}
