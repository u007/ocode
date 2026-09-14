//go:build !windows

package lsp

import (
	"os/exec"
	"syscall"
)

// detachProcAttr places the spawned daemon in its own session (and so its
// own process group) so it outlives the spawning ocode process's exit or
// Ctrl-C without being signalled along with it. Setsid rather than Setpgid,
// matching internal/tool/proc_unix.go: a Setpgid child stays in ocode's
// terminal session and can still open /dev/tty, so a job-control step in
// the daemon or the language server it spawns could move the terminal's
// foreground group away from the TUI. A new session has no controlling
// terminal, which rules that out structurally.
func detachProcAttr(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
