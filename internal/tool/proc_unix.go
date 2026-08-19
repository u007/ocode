//go:build !windows

package tool

import (
	"os"
	"os/exec"
	"syscall"
)

// setProcGroup places the command in its own process group so the whole
// group can be signalled together.
func setProcGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup force-kills the process group rooted at proc.
func killProcessGroup(proc *os.Process) error {
	return syscall.Kill(-proc.Pid, syscall.SIGKILL)
}

// terminateProcess sends SIGTERM to the process, or to its whole group
// when it owns one.
func terminateProcess(proc *os.Process, ownsGroup bool) error {
	if ownsGroup {
		return syscall.Kill(-proc.Pid, syscall.SIGTERM)
	}
	return proc.Signal(syscall.SIGTERM)
}

// forceKillProcess sends SIGKILL to the process, or to its whole group
// when it owns one.
func forceKillProcess(proc *os.Process, ownsGroup bool) error {
	if ownsGroup {
		return syscall.Kill(-proc.Pid, syscall.SIGKILL)
	}
	return proc.Kill()
}
