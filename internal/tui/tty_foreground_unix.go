//go:build !windows

package tui

import (
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"
)

// reclaimTTYForeground restores this process's own process group as the
// controlling terminal's foreground process group, and makes SIGTTOU and
// SIGTTIN non-fatal for the rest of the run.
//
// Defense-in-depth, not the primary fix: two confirmed causes of ocode's
// terminal getting hijacked mid-run were a local-model server child spawned
// with Setpgid (see internal/tool/proc_unix.go's setProcGroup, now Setsid)
// and the interactive login-shell PATH probe (see
// internal/discovery/python_env.go's detachFromControllingTTY, now also
// Setsid) — both reassigned the terminal's foreground process group to
// themselves via TIOCSPGRP as part of their own job-control setup, and both
// now structurally cannot touch /dev/tty at all. Kept here in case some
// other spawned process not covered by those two fixes ever does the same:
// left alone, a background process group changing terminal attributes
// (tcsetattr, e.g. bubbletea entering raw mode) is sent SIGTTOU, and a
// background process group reading from the terminal (bubbletea's input
// loop) is sent SIGTTIN — either one stops the whole ocode job
// ("suspended (tty output)" / "suspended (tty input)"), which is what
// upgrades a hijack into a hard crash on the next tty read. signal.Ignore,
// not signal.Notify, is required here — POSIX only lets tcsetattr/read
// succeed from a background pgrp when the corresponding signal is ignored
// outright; a caught handler still interrupts the syscall (EINTR), which
// bubbletea does not retry.
func reclaimTTYForeground() {
	signal.Ignore(syscall.SIGTTOU, syscall.SIGTTIN)
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return
	}
	defer f.Close()
	pgrp, err := syscall.Getpgid(0)
	if err != nil {
		return
	}
	_ = unix.IoctlSetInt(int(f.Fd()), unix.TIOCSPGRP, pgrp)
}
