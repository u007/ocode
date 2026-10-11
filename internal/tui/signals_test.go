package tui

import (
	"os"
	"testing"
)

// TestTerminationSignalsAlwaysIncludeInterrupt pins the invariant that every
// platform watches os.Interrupt. It is what drives the cleanup request, and
// dropping it would let a hard kill skip bubbletea's tty restore, leaving the
// alt-screen and mouse tracking enabled in the user's shell. This is the
// reason signals_windows.go keeps os.Interrupt even though SIGTERM, SIGHUP
// and SIGCONT do not exist there.
func TestTerminationSignalsAlwaysIncludeInterrupt(t *testing.T) {
	sigs := terminationSignals()
	if len(sigs) == 0 {
		t.Fatal("terminationSignals() is empty; no signal would ever reach the watcher")
	}
	for _, sig := range sigs {
		if sig == os.Interrupt {
			return
		}
	}
	t.Fatalf("terminationSignals() = %v, must include os.Interrupt", sigs)
}

// TestInterruptIsNotTreatedAsResume guards the cleanup path: isResumeSignal
// returns true for the log-only SIGCONT, and a true result makes the watcher
// `continue` without ever sending cleanupRequestMsg. If an interrupt were
// ever classified as a resume signal, Ctrl+C would stop quitting gracefully.
func TestInterruptIsNotTreatedAsResume(t *testing.T) {
	if isResumeSignal(os.Interrupt) {
		t.Fatal("isResumeSignal(os.Interrupt) = true; interrupt must trigger cleanup, not be skipped")
	}
}
