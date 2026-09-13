//go:build windows

package computer

import (
	"github.com/u007/ocode/internal/tool"
)

// newPlatformDriver creates the Windows computer driver.
func newPlatformDriver(r commandRunner, sup *tool.ProcessSupervisor) (tool.ComputerDriver, error) {
	return newWindowsDriver(r, sup)
}
