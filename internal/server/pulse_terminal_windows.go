//go:build windows

package server

import "errors"

// pulseTerminalTail: the interactive terminal (and its history log) does not
// exist on Windows, so there is nothing to read.
func (h *Handler) pulseTerminalTail(terminalID string) (string, string, error) {
	return "", "", errors.New("terminals are not supported on Windows")
}
