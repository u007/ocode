//go:build !windows

package tool

import (
	"os"
	"os/exec"
	"syscall"
)

// setProcGroup places the command in its own session (and, as a consequence,
// its own process group headed by its own PID) so the whole group can be
// signalled together via -pid.
//
// Setsid rather than plain Setpgid: these children's stdio is always piped
// or redirected, never the real controlling terminal, but Setpgid alone
// leaves them in the SAME session as ocode — any child (or a grandchild,
// e.g. a spawned local-model server) can still open("/dev/tty") and get a
// live handle on the real terminal. A local-model server child was
// confirmed doing exactly that and issuing TIOCSPGRP against it, which
// knocked ocode's own process group out of the terminal's foreground and
// suspended/corrupted the whole shell session (SIGTTOU/SIGTTIN, "error on
// TTY read: Input/output error") — confirmed root cause via
// internal/tui/tty_foreground_unix.go's debugTTYState instrumentation:
// tty_fg_pgrp drifted away from ocode's own pgrp during local-model
// auto-start with plain Setpgid, and no longer drifts at all with Setsid.
// Setsid makes the child a new session leader with no controlling terminal
// at all, so open("/dev/tty") fails outright and this class of hijack is
// structurally impossible rather than something ocode has to detect and
// recover from.
func setProcGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
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
