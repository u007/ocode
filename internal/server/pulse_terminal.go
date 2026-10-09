//go:build !windows

package server

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// pulseTerminalTail returns the project key and the tail of a terminal's
// on-disk output history. It mirrors GET /api/terminal/{id}/history: the same
// access gate, the same project trust boundary, the same log. Only the last
// terminalHistoryMaxPage bytes are read.
func (h *Handler) pulseTerminalTail(terminalID string) (project, text string, err error) {
	if !h.terminalAccessAllowed() {
		return "", "", errors.New("terminal access requires server authentication or a loopback bind address")
	}
	if strings.HasPrefix(terminalID, "anon-") {
		return "", "", fmt.Errorf("terminal %q has no history", terminalID)
	}
	project, err = h.pulseTerminalProject(terminalID)
	if err != nil {
		return "", "", err
	}

	// Pin the snapshot end with a one-byte read, then read the trailing window.
	_, end, err := readTerminalHistoryRangeAt(project, terminalID, 0, 1, nil)
	if err != nil {
		return "", "", fmt.Errorf("terminal %q history: %w", terminalID, err)
	}
	offset := end - terminalHistoryMaxPage
	if offset < 0 {
		offset = 0
	}
	data, _, err := readTerminalHistoryRangeAt(project, terminalID, offset, terminalHistoryMaxPage, &end)
	if err != nil {
		return "", "", fmt.Errorf("terminal %q history: %w", terminalID, err)
	}
	text = string(data)
	if offset > 0 {
		// The window starts mid-stream: drop the partial first line, which may
		// also begin inside a multi-byte character.
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = text[i+1:]
		}
	}
	if !utf8.ValidString(text) {
		text = strings.ToValidUTF8(text, "�")
	}
	return project, text, nil
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
