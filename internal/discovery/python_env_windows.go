//go:build windows

package discovery

import "os/exec"

// detachFromControllingTTY is a no-op on Windows; process groups and
// controlling-terminal hijacking via tcsetpgrp are POSIX-only concerns.
func detachFromControllingTTY(_ *exec.Cmd) {}
