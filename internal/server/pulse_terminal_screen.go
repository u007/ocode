//go:build !windows

package server

import (
	"bytes"
	"fmt"
)

// pulseTerminalScreenText returns what terminal_screen replays for one live
// terminal: its project, the pty size the browser last reported, and the newest
// pulseTerminalScreenBytes of its log, pinned to one snapshot. The same access
// gate and local-project boundary as terminal_read apply. A terminal that is not
// live has no pty size, so it has no screen.
func (h *Handler) pulseTerminalScreenText(terminalID, host string) (string, int, int, string, error) {
	project, read, err := h.pulseTerminalSource(terminalID, host)
	if err != nil {
		return "", 0, 0, "", err
	}
	var cols, rows uint16
	if host != "" {
		live, lerr := h.pulseRemoteLive(host)
		if lerr != nil {
			return "", 0, 0, "", lerr
		}
		found := false
		for _, r := range live {
			if r.ID == terminalID {
				cols, rows, found = r.Cols, r.Rows, true
				break
			}
		}
		if !found {
			return "", 0, 0, "", fmt.Errorf("terminal %q is not live on %s, so it has no screen", terminalID, host)
		}
	} else {
		entry, live := h.terminalProcs.snapshot()[terminalID]
		if !live {
			return "", 0, 0, "", fmt.Errorf("terminal %q is not live, so it has no screen", terminalID)
		}
		cols, rows = entry.Cols, entry.Rows
	}
	if cols == 0 || rows == 0 {
		return "", 0, 0, "", fmt.Errorf("terminal %q has no pty size yet; open the terminal in a browser", terminalID)
	}

	// Pin the snapshot end with a one-byte read, then read the newest window.
	_, end, err := read(0, 1, nil)
	if err != nil {
		return "", 0, 0, "", fmt.Errorf("terminal %q history: %w", terminalID, err)
	}
	offset := max(end-pulseTerminalScreenBytes, 0)
	data, err := readPulseWindow(read, offset, end-offset, end)
	if err != nil {
		return "", 0, 0, "", fmt.Errorf("terminal %q history: %w", terminalID, err)
	}
	// A window that starts mid-sequence would replay the sequence's tail as text.
	// Start at the first ESC in its first page, the rule the dashboard card applies
	// (restoreTerminalHistory); a page with no ESC is kept whole.
	if offset > 0 {
		page := data[:min(len(data), pulseTerminalScreenPageBytes)]
		if i := bytes.IndexByte(page, 0x1b); i >= 0 {
			data = data[i:]
		}
	}
	return project, int(cols), int(rows), string(data), nil
}
