//go:build !windows

package server

import (
	"os/exec"
	"syscall"
)

// oNoFollow rejects opens whose final path component is a symlink.
const oNoFollow = syscall.O_NOFOLLOW

// setProcGroup places the command in its own process group so the whole
// group can be signalled together.
func setProcGroup(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
