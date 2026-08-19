//go:build windows

package server

import "os/exec"

// oNoFollow is 0 on Windows: os.OpenFile via CreateFile does not follow
// file symlinks the way POSIX open does, so no extra flag is needed.
const oNoFollow = 0

// setProcGroup is a no-op on Windows; process groups are POSIX-only here.
func setProcGroup(_ *exec.Cmd) {}
