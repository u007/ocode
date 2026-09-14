//go:build !windows

package tui

import (
	"os"

	"golang.org/x/sys/unix"
)

// redirectStderr points fd 2 at f so writes that bypass os.Stderr (the Go
// runtime's panic and fatal-error output) land in the crash log too.
func redirectStderr(f *os.File) error {
	return unix.Dup2(int(f.Fd()), 2)
}
