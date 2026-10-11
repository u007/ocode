//go:build !windows

package tui

import (
	"os"
	"syscall"
)

// terminationSignals is the set watchProgramSignals asks the runtime to
// deliver. SIGHUP is included so a dropped terminal quits through bubbletea
// (which restores the tty) instead of the default silent kill.
func terminationSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGCONT}
}

// isResumeSignal reports whether sig is SIGCONT, which is logged only, as
// evidence of a stop/resume cycle, and must not trigger a cleanup request.
func isResumeSignal(sig os.Signal) bool { return sig == syscall.SIGCONT }
