//go:build windows

package server

import (
	"errors"
)

// pulseTerminalWindow: the interactive terminal (and its history log) does not
// exist on Windows, so there is nothing to read.
func (h *Handler) pulseTerminalWindow(terminalID string, head bool) (string, string, bool, bool, bool, error) {
	return "", "", false, false, false, errors.New("terminals are not supported on Windows")
}

// pulseTerminalScreenText: no pty exists on Windows, so there is no screen to replay.
func (h *Handler) pulseTerminalScreenText(terminalID string) (string, int, int, string, error) {
	return "", 0, 0, "", errors.New("terminals are not supported on Windows")
}
