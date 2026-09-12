//go:build windows

package computer

import (
	"github.com/u007/ocode/internal/tool"
)

// newPlatformDriver returns an unsupported-platform driver on windows;
// the real Windows driver is implemented in Part 08.
func newPlatformDriver(sup *tool.ProcessSupervisor) (tool.ComputerDriver, error) {
	return nil, ErrUnsupportedPlatform
}
