//go:build !windows

package server

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// pulseTerminalWindow returns the project key and one window of a terminal's
// on-disk output history: its newest pulseTerminalWindowBytes, or with head its
// oldest. moreBefore and moreAfter say whether history exists beyond the window
// on that side. It mirrors GET /api/terminal/{id}/history: the same access gate,
// the same project trust boundary, the same log.
func (h *Handler) pulseTerminalWindow(terminalID string, head bool) (project, text string, moreBefore, moreAfter bool, err error) {
	if !h.terminalAccessAllowed() {
		return "", "", false, false, errors.New("terminal access requires server authentication or a loopback bind address")
	}
	if strings.HasPrefix(terminalID, "anon-") {
		return "", "", false, false, fmt.Errorf("terminal %q has no history", terminalID)
	}
	project, err = h.pulseTerminalProject(terminalID)
	if err != nil {
		return "", "", false, false, err
	}

	// Pin the snapshot end with a one-byte read, then read the chosen window.
	_, end, err := readTerminalHistoryRangeAt(project, terminalID, 0, 1, nil)
	if err != nil {
		return "", "", false, false, fmt.Errorf("terminal %q history: %w", terminalID, err)
	}
	var offset, length int64
	if head {
		length = min(end, pulseTerminalWindowBytes)
		moreAfter = end > length
	} else {
		offset = max(end-pulseTerminalWindowBytes, 0)
		length = end - offset
		moreBefore = offset > 0
	}
	data, err := readPulseWindow(project, terminalID, offset, length, end)
	if err != nil {
		return "", "", false, false, fmt.Errorf("terminal %q history: %w", terminalID, err)
	}
	text = string(data)
	if !head && offset > 0 {
		// The window starts mid-stream: drop the partial first line, which may
		// also begin inside a multi-byte character.
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = text[i+1:]
		}
	}
	if head && moreAfter {
		// The window ends mid-stream: drop the partial last line.
		if i := strings.LastIndexByte(text, '\n'); i >= 0 {
			text = text[:i+1]
		}
	}
	if !utf8.ValidString(text) {
		text = strings.ToValidUTF8(text, "�")
	}
	return project, text, moreBefore, moreAfter, nil
}

// readPulseWindow reads [offset, offset+length) of a terminal's log, pinned to
// the snapshot end. The history reader returns at most terminalHistoryMaxPage
// per call, so a window wider than that is read in chunks.
func readPulseWindow(project, terminalID string, offset, length, end int64) ([]byte, error) {
	out := make([]byte, 0, length)
	for int64(len(out)) < length {
		chunk := min(length-int64(len(out)), terminalHistoryMaxPage)
		data, _, err := readTerminalHistoryRangeAt(project, terminalID, offset+int64(len(out)), chunk, &end)
		if err != nil {
			return nil, err
		}
		if len(data) == 0 {
			break
		}
		out = append(out, data...)
	}
	return out, nil
}

// pulseTerminalProject resolves the project a terminal belongs to: the live
// session when there is one, else the open-tab store. Remote terminals are not
// readable here (their history lives on the remote host), and the project must
// be a root this server serves.
func (h *Handler) pulseTerminalProject(terminalID string) (string, error) {
	project := ""
	if sess := h.terminalSessions.lookup(terminalID); sess != nil {
		project = sess.project
	} else if h.termTabsStore != nil {
		all := h.termTabsStore.All()
		keys := make([]string, 0, len(all))
		for k := range all {
			keys = append(keys, k)
		}
		sort.Strings(keys)
	search:
		for _, k := range keys {
			for _, t := range all[k].Terminals {
				if t.ID == terminalID {
					project = k
					break search
				}
			}
		}
	}
	if project == "" {
		return "", fmt.Errorf("terminal %q not found (see terminal_tabs for open terminals)", terminalID)
	}
	for _, root := range h.allowedProjectRoots() {
		if project == root {
			return project, nil
		}
	}
	return "", fmt.Errorf("terminal %q belongs to %q, which is not a local project of this server", terminalID, project)
}
