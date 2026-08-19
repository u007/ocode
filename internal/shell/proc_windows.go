//go:build windows

package shell

import "os/exec"

// setProcGroup is a no-op on Windows; process groups are POSIX-only here.
func setProcGroup(_ *exec.Cmd) {}
