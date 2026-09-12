//go:build linux

package computer

import (
	"github.com/u007/ocode/internal/tool"
)

// newPlatformDriver returns an unsupported-platform driver on linux;
// the real Linux driver is implemented in Part 09.
func newPlatformDriver(sup *tool.ProcessSupervisor) (tool.ComputerDriver, error) {
	return nil, ErrUnsupportedPlatform
}
