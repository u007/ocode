//go:build !windows

package server

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
)

// pulseTerminalScreenText returns what terminal_screen replays for one live
// terminal: its project, the pty size the browser last reported, and the newest
// pulseTerminalScreenBytes of its log, pinned to one snapshot. The same access
// gate and local-project boundary as terminal_read apply. A terminal that is not
// live has no pty size, so it has no screen.
func (h *Handler) pulseTerminalScreenText(terminalID string) (string, int, int, string, error) {
	if !h.terminalAccessAllowed() {
		return "", 0, 0, "", errors.New("terminal access requires server authentication or a loopback bind address")
	}
	if strings.HasPrefix(terminalID, "anon-") {
		return "", 0, 0, "", fmt.Errorf("terminal %q has no screen: %w", terminalID, errTerminalHistoryMissing)
	}
	project, err := h.pulseTerminalProject(terminalID)
	if err != nil {
		return "", 0, 0, "", err
	}
	entry, live := h.terminalProcs.snapshot()[terminalID]
	if !live {
		return "", 0, 0, "", fmt.Errorf("terminal %q is not live, so it has no screen", terminalID)
	}
	if entry.Cols == 0 || entry.Rows == 0 {
		return "", 0, 0, "", fmt.Errorf("terminal %q has no pty size yet; open the terminal in a browser", terminalID)
	}

	// Pin the snapshot end with a one-byte read, then read the newest window.
	_, end, err := readTerminalHistoryRangeAt(project, terminalID, 0, 1, nil)
	if err != nil {
		return "", 0, 0, "", fmt.Errorf("terminal %q history: %w", terminalID, err)
	}
	offset := max(end-pulseTerminalScreenBytes, 0)
	data, err := readPulseWindow(project, terminalID, offset, end-offset, end)
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
	return project, int(entry.Cols), int(entry.Rows), string(data), nil
}
