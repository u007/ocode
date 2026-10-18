//go:build windows

package server

import (
	"encoding/json"
	"errors"
)

// pulseTerminalNewTool is unavailable on Windows: the pty bridge is built on
// github.com/creack/pty, which is Unix-only. The tool stays registered (see
// pulseWriteTools and TestPulseToolsShape) so the Windows build keeps the same
// surface and the model gets an explicit refusal instead of a missing tool.
func (h *Handler) pulseTerminalNewTool(raw json.RawMessage) (string, error) {
	return "", errors.New("interactive terminal is not supported on Windows")
}
