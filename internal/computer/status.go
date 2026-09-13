package computer

import (
	"runtime"

	"github.com/u007/ocode/internal/config"
)

// StatusLines returns user-facing computer-use status lines.
func StatusLines(cfg config.ComputerUseConfig) []string {
	status := "disabled"
	if cfg.Enabled {
		status = "enabled"
	}
	lines := []string{
		"Computer use: " + status,
		"Backend: " + backendName(),
	}
	if runtime.GOOS == "darwin" {
		lines = append(lines, "Grant Screen Recording and Accessibility to your terminal or ocode-desktop under System Settings → Privacy & Security.")
	}
	return lines
}
