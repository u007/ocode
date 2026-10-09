//go:build windows

package server

import "errors"

// pulseTerminalWindow: the interactive terminal (and its history log) does not
// exist on Windows, so there is nothing to read.
func (h *Handler) pulseTerminalWindow(terminalID string, head bool) (string, string, bool, bool, error) {
	return "", "", false, false, errors.New("terminals are not supported on Windows")
}
