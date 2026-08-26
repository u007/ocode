//go:build !windows

package discovery

import (
	"os/exec"
	"syscall"
)

// detachFromControllingTTY puts cmd in its own session so it can never become
// the controlling terminal's foreground process group. Required for the
// interactive login-shell PATH probe (mlxPythonPath): "$SHELL -ilc" makes the
// shell run its normal interactive job-control startup, which calls
// tcsetpgrp to grab the tty for itself even though its stdin/stdout are
// redirected — see setProcGroup in internal/tool/proc_unix.go for the
// original instance of this bug (local-model server spawn) and
// reclaimTTYForeground in internal/tui/tty_foreground_unix.go for the
// consequence: once the probe process exits, the terminal's foreground pgrp
// is left pointing at nothing, and ocode's own next stdin read is delivered
// SIGTTIN instead — stopping the whole TUI job ("suspended (tty input)").
func detachFromControllingTTY(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
