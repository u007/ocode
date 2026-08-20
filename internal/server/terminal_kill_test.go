//go:build !windows

package server

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// wantSignalTests skips the signal-delivery tests unless explicitly opted in.
// The terminal path signals a process group rooted at a session-leader child
// (pty.Start sets Setsid), exactly the shape that some sandboxed exec runners
// intercept so kill() reports success yet the child survives. On a normal
// desktop host SIGTERM/SIGKILL to such a group works, so developers can run:
//
//	OCODE_TEST_TERMINAL_SIGNALS=1 go test ./internal/server/ -run TerminateProcessTree
func wantSignalTests(t *testing.T) {
	t.Helper()
	if os.Getenv("OCODE_TEST_TERMINAL_SIGNALS") == "" {
		t.Skip("set OCODE_TEST_TERMINAL_SIGNALS=1 to run (needs signal delivery to session-leader children, unavailable in some sandboxes)")
	}
}

// TestTerminateProcessTreeSigtermExits verifies that a process group which
// honors SIGTERM is reaped quickly (well within the grace window) and is fully
// gone afterwards.
func TestTerminateProcessTreeSigtermExits(t *testing.T) {
	wantSignalTests(t)
	// Shell that exits cleanly on SIGTERM (deterministic, unlike `sleep`).
	cmd := exec.Command("sh", "-c", "trap 'exit 0' TERM; while true; do sleep 0.5; done")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start shell: %v", err)
	}
	pid := cmd.Process.Pid

	start := time.Now()
	terminateProcessTree(pid, 2*time.Second)
	elapsed := time.Since(start)

	if _, err := cmd.Process.Wait(); err != nil {
		t.Logf("shell wait result: %v", err)
	}
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("terminate returned too late: %s (expected < 1.5s)", elapsed)
	}
	if err := syscall.Kill(-pid, 0); err != syscall.ESRCH {
		t.Fatalf("process group %d still alive after terminate (err=%v)", pid, err)
	}
}

// TestTerminateProcessTreeSigkillEscalation verifies that a process group which
// ignores SIGTERM is force-killed after the grace window elapses.
func TestTerminateProcessTreeSigkillEscalation(t *testing.T) {
	wantSignalTests(t)
	// bash that ignores TERM and loops forever.
	cmd := exec.Command("bash", "-c", "trap '' TERM; while true; do sleep 0.2; done")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Skipf("bash unavailable, skipping: %v", err)
	}
	pid := cmd.Process.Pid

	start := time.Now()
	terminateProcessTree(pid, 600*time.Millisecond)
	elapsed := time.Since(start)

	if _, err := cmd.Process.Wait(); err != nil {
		t.Logf("bash wait result: %v", err)
	}
	if elapsed < 500*time.Millisecond {
		t.Fatalf("terminate escalated too early: %s", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("terminate took too long: %s", elapsed)
	}
	if err := syscall.Kill(-pid, 0); err != syscall.ESRCH {
		t.Fatalf("process group %d still alive after SIGKILL (err=%v)", pid, err)
	}
}

// TestTerminateProcessTreeDeadPID verifies the already-gone path returns
// immediately without hanging or panicking.
func TestTerminateProcessTreeDeadPID(t *testing.T) {
	start := time.Now()
	terminateProcessTree(9_999_999, 2*time.Second) // no such process group
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("terminate on dead pid took too long: %s", elapsed)
	}
}

func TestTerminalGraceDuration(t *testing.T) {
	// No deadline -> max grace.
	if g := terminalGraceDuration(context.Background()); g != 2*time.Second {
		t.Fatalf("no-deadline grace = %s, want 2s", g)
	}
	// Tight deadline -> half the remaining budget (with timing slack).
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	g := terminalGraceDuration(ctx)
	if g < 40*time.Millisecond || g > 60*time.Millisecond {
		t.Fatalf("tight-deadline grace = %s, want ~50ms", g)
	}
}
