//go:build !windows

package shell

import (
	"os/exec"
	"syscall"
)

// setProcGroup places the command in its own process group so the whole
// group can be signalled together.
func setProcGroup(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
