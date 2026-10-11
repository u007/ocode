//go:build !windows

package tui

import (
	"os"
	"syscall"
	"testing"
)

// TestUnixSignalsWatchedAndResumeIsLogOnly pins the Unix contract that the
// platform split must preserve: SIGTERM, SIGHUP and SIGCONT are all watched,
// and SIGCONT is the one signal classified as resume-only (logged, never a
// cleanup request). If a future refactor drops SIGCONT from the watched set —
// or makes isResumeSignal return false for it — the stop/resume evidence the
// crash log records is lost, silently.
func TestUnixSignalsWatchedAndResumeIsLogOnly(t *testing.T) {
	watched := map[os.Signal]bool{}
	for _, sig := range terminationSignals() {
		watched[sig] = true
	}
	for _, want := range []os.Signal{syscall.SIGTERM, syscall.SIGHUP, syscall.SIGCONT} {
		if !watched[want] {
			t.Errorf("terminationSignals() = %v, must watch %v", terminationSignals(), want)
		}
	}
	if !isResumeSignal(syscall.SIGCONT) {
		t.Error("isResumeSignal(SIGCONT) = false; SIGCONT must be logged only, never a cleanup request")
	}
	for _, sig := range []os.Signal{syscall.SIGTERM, syscall.SIGHUP} {
		if isResumeSignal(sig) {
			t.Errorf("isResumeSignal(%v) = true; only SIGCONT is resume-only", sig)
		}
	}
}
