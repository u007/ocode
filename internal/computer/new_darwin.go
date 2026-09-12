//go:build darwin

package computer

import (
	"github.com/u007/ocode/internal/tool"
)

// newPlatformDriver returns an unsupported-platform driver on darwin;
// the real macOS driver is implemented in Part 07.
func newPlatformDriver(sup *tool.ProcessSupervisor) (tool.ComputerDriver, error) {
	return nil, ErrUnsupportedPlatform
}
