package tui

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"github.com/mattn/go-isatty"
)

// RunOptions controls startup behavior of the TUI. Fields are zero-value
// safe: the empty string for PermissionMode leaves the loaded config value
// untouched.
type RunOptions struct {
	SessionID      string
	Continue       bool
	YOLO           bool
	PermissionMode string // "" | "auto" | "off"
	Effort         string // "" leaves the persisted thinking budget untouched; otherwise off|low|med|high|xhigh|max
	WebFS          fs.FS  // Embedded web assets for /rc command
}

// errNoTTY is returned when stdin/stdout aren't attached to a terminal.
// Bubbletea itself detects this too, but only after opening /dev/tty
// directly and failing with an opaque internal error ("could not open TTY:
// open /dev/tty: device not configured") that gives the user no indication
// the TUI simply can't run in their current context (piped output, a
// script, cron/CI, or another tool's non-interactive shell).
var errNoTTY = fmt.Errorf("no interactive terminal detected; run ocode from a terminal app (Terminal, iTerm2, etc.), or use `ocode run <prompt>` for non-interactive/headless use")

func Run(opts RunOptions) error {
	if !isatty.IsTerminal(os.Stdin.Fd()) || !isatty.IsTerminal(os.Stdout.Fd()) {
		fmt.Fprintf(os.Stderr, "ocode: %v\n", errNoTTY)
		return errNoTTY
	}

	// Redirect the standard library logger into the debug panel before anything
	// runs. Once bubbletea enters the alt-screen, any stray log/os.Stderr write
	// paints over the frame and corrupts it; routing log here keeps those
	// messages visible without bleeding onto the screen.
	log.SetFlags(0)
	log.SetOutput(debugLogWriter{})

	m := newModel(opts)

	reclaimTTYForeground()

	// If an explicitly requested session (-session / -continue) failed to
	// load, abort before the TUI starts. Continuing here would silently open
	// a fresh session and, on the next save, write a placeholder file that
	// shadows the missing ID in the session picker.
	if m.sessionLoadErr != nil {
		fmt.Fprintf(os.Stderr, "ocode: cannot resume session: %v\n", m.sessionLoadErr)
		return m.sessionLoadErr
	}

	if m.config != nil {
		if err := validateStartupEditorConfig(&m.config.Ocode); err != nil {
			fmt.Fprintf(os.Stderr, "ocode: %v\n", err)
			return err
		}
	}

	p := tea.NewProgram(m, tea.WithFilter(newInputFilter()))
	var finalModel tea.Model
	defer func() {
		if finalModel == nil {
			return
		}
		cleanupProgramModel(finalModel)
	}()
	stopSignals := watchProgramSignals(p)
	defer stopSignals()
	finalModel, err := p.Run()
	if err != nil {
		return err
	}
	switch m := finalModel.(type) {
	case model:
		fmt.Fprint(os.Stdout, exitResumeSummary(m.sessionID))
	case *model:
		fmt.Fprint(os.Stdout, exitResumeSummary(m.sessionID))
	}
	return nil
}

func watchProgramSignals(p *tea.Program) func() {
	sigCh := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		for {
			select {
			case <-done:
				return
			case <-sigCh:
				p.Send(cleanupRequestMsg{})
			}
		}
	}()
	return func() {
		signal.Stop(sigCh)
		close(done)
	}
}

func cleanupProgramModel(m tea.Model) {
	switch m := m.(type) {
	case model:
		m.cleanupCurrentSession()
	case *model:
		m.cleanupCurrentSession()
	}
}

func exitResumeSummary(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	return fmt.Sprintf("Session ID: %s\nResume with: ocode -session %s\n", sessionID, sessionID)
}
