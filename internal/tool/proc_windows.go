//go:build windows

package tool

import (
	"os"
	"os/exec"
)

// setProcGroup is a no-op on Windows; process groups are POSIX-only here.
func setProcGroup(_ *exec.Cmd) {}

// killProcessGroup force-kills the process; Windows has no POSIX process
// groups, so only the direct process is killed.
func killProcessGroup(proc *os.Process) error {
	return proc.Kill()
}

// terminateProcess kills the process; Windows has no SIGTERM delivery.
func terminateProcess(proc *os.Process, _ bool) error {
	return proc.Kill()
}

// forceKillProcess kills the process.
func forceKillProcess(proc *os.Process, _ bool) error {
	return proc.Kill()
}
