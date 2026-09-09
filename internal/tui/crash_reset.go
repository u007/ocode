package tui

import (
	"os"

	"golang.org/x/term"

	"github.com/u007/ocode/internal/crashguard"
)

// installCrashTerminalReset registers a crashguard hook that returns the
// terminal to a usable state when an ocode-owned goroutine panics. Bubbletea
// cannot run its own restore on that path (the process dies before it gets
// control), so mouse tracking and the alt-screen would otherwise stay enabled
// in the user's shell. State is captured before bubbletea enters raw mode so
// the restore returns the tty to what it was at launch.
func installCrashTerminalReset() {
	fd := int(os.Stdin.Fd())
	saved, err := term.GetState(fd)
	if err != nil {
		// Not a real tty (Run already rejects this earlier); nothing to restore.
		saved = nil
	}
	crashguard.SetOnPanic(func() {
		// ?1003l/?1006l/?1002l/?1000l: all mouse modes off; ?1049l: leave alt-screen;
		// ?25h: cursor visible; ?2004l: bracketed paste off; ?1004l: focus events off.
		os.Stdout.WriteString("\x1b[?1003l\x1b[?1006l\x1b[?1002l\x1b[?1000l\x1b[?1004l\x1b[?2004l\x1b[?25h\x1b[?1049l")
		if saved != nil {
			_ = term.Restore(fd, saved)
		}
	})
}
