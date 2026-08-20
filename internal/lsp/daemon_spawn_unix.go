//go:build !windows

package lsp

import (
	"os/exec"
	"syscall"
)

// detachProcAttr places the spawned daemon in its own process group (the
// same convention internal/server and internal/tool use for background
// children) so it outlives the spawning ocode process's exit or Ctrl-C
// without being signalled along with it.
func detachProcAttr(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
