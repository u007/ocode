//go:build !windows

package server

import (
	"log"
	"syscall"
	"time"
)

// terminateProcessTree gracefully kills the process group rooted at pid (the pty
// shell created by pty.Start is a session leader, so its pgid equals its pid).
// It sends SIGTERM to the whole group (and to the direct child as a fallback for
// environments that restrict group signals), waits up to grace for it to exit,
// then escalates to SIGKILL. This lets a foreground command flush/clean up before
// the desktop app exits, instead of being slaughtered mid-write.
func terminateProcessTree(pid int, grace time.Duration) {
	terminateProcessTreeSignal(pid)

	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
			return // group leader exited
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Grace window elapsed: force-kill the group and the direct child.
	terminateProcessTreeKill(pid)
}

// terminateProcessTreeSignal sends SIGTERM to the whole process group rooted
// at pid (plus the direct child as a sandbox fallback). Shutdown calls this
// first and then waits on the session's done channel — which only closes after
// the read loop has drained final output and synced history — instead of
// polling the pid. terminateProcessTree above keeps the old poll-based shape
// for detach-TTL and explicit-close paths.
func terminateProcessTreeSignal(pid int) {
	if pid <= 0 {
		return
	}
	// Negative pid targets the process group, so the shell and its children
	// (the actual running command) all receive the signal.
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		if err != syscall.ESRCH {
			log.Printf("terminal: SIGTERM to process group %d failed: %v", pid, err)
		}
	}
	// Fallback for sandboxes/runtimes that filter group-directed signals: signal
	// the direct child too. Harmless if the group signal already covered it.
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
		log.Printf("terminal: SIGTERM to pid %d failed: %v", pid, err)
	}
}

// terminateProcessTreeKill force-kills the process group rooted at pid. It is
// the escalation half of terminateProcessTree, split out so shutdown can wait
// on session completion first and only escalate when the shutdown context
// expires.
func terminateProcessTreeKill(pid int) {
	if pid <= 0 {
		return
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
		log.Printf("terminal: SIGKILL to process group %d failed: %v", pid, err)
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
		log.Printf("terminal: SIGKILL to pid %d failed: %v", pid, err)
	}
}
