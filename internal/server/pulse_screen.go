package server

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/vt"
	"github.com/u007/ocode/internal/crashguard"
)

// pulseTerminalScreenBytes is how much of the newest history a screen replays.
// It is the window the dashboard card replays too, which reproduces a full
// Claude Code repaint (64 KiB did not).
const pulseTerminalScreenBytes = 256 << 10

// pulseTerminalScreenPageBytes is the page in which a window's first ESC is
// searched. It mirrors the dashboard card's default restore page (pageSize in
// restoreTerminalHistory), which neither Pulse nor the panel overrides, so both
// starts agree.
const pulseTerminalScreenPageBytes = 64 << 10

// renderPulseScreen replays raw pty output into a screen of cols x rows and
// returns the visible rows as plain text, trailing blank rows dropped. The
// screen is what a terminal shows right now: cursor moves and in-place redraws
// are applied, so a full-screen program reads as its current frame, not as a
// stream of lines. Wide characters come out whole, because String() already
// skips their continuation cells.
func renderPulseScreen(raw string, cols, rows int) ([]string, error) {
	e := vt.NewEmulator(cols, rows)
	// The emulator writes replies to terminal queries (DSR ESC[6n, DA ESC[c,
	// which Claude Code sends) into a pipe. Nothing else reads it, so a Write
	// would block on the first query. Drain it on a goroutine. The pipe's writer
	// is closed directly to end that drain. Emulator.Close would also write an
	// unguarded flag its Read checks, which the race detector flags.
	replies, ok := e.InputPipe().(interface{ CloseWithError(error) error })
	if !ok {
		return nil, fmt.Errorf("x/vt reply pipe cannot be closed, so its reader would leak")
	}
	drained := make(chan struct{})
	crashguard.Go(func() {
		defer close(drained)
		_, _ = io.Copy(io.Discard, e)
	})
	defer func() {
		_ = replies.CloseWithError(io.EOF)
		<-drained
	}()
	// x/vt's ED 3 handler (handlers.go, case 3) clears the visible screen as well as
	// the scrollback, where xterm clears only the scrollback. The screen never shows
	// scrollback, so dropping the sequence leaves the visible screen as xterm draws it.
	if _, err := e.Write([]byte(strings.ReplaceAll(raw, "\x1b[3J", ""))); err != nil {
		return nil, fmt.Errorf("replay terminal output: %w", err)
	}
	lines := strings.Split(e.String(), "\n")
	for i, line := range lines {
		lines[i] = truncateRunes(strings.TrimRightFunc(line, unicode.IsSpace), pulseTerminalLineRuneBudget)
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines, nil
}

// pulseTerminalScreenTool is terminal_screen: the current screen of one live
// terminal, laid out at the pty's own size. The dashboard row shows the same
// screen. A capped result keeps the bottom rows, where a prompt sits.
func (h *Handler) pulseTerminalScreenTool(raw json.RawMessage) (string, error) {
	var args struct {
		TerminalID string `json:"terminal_id"`
		Host       string `json:"host"`
	}
	if err := decodePulseArgs(raw, &args); err != nil {
		return "", err
	}
	if args.TerminalID == "" {
		return "", fmt.Errorf("terminal_id is required")
	}
	project, cols, rows, history, err := h.pulseTerminalScreenText(args.TerminalID, args.Host)
	if err != nil {
		return "", err
	}
	lines, err := renderPulseScreen(history, cols, rows)
	if err != nil {
		return "", err
	}
	return pulseCapped(len(lines), func(n int, truncated bool) any {
		return struct {
			TerminalID string   `json:"terminal_id"`
			Host       string   `json:"host,omitempty"`
			Project    string   `json:"project"`
			Cols       int      `json:"cols"`
			Rows       int      `json:"rows"`
			Lines      []string `json:"lines"`
			Truncated  bool     `json:"truncated,omitempty"`
		}{args.TerminalID, args.Host, project, cols, rows, append([]string{}, lines[len(lines)-n:]...), truncated}
	})
}
