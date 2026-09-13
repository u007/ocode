//go:build linux

package computer

import (
	"github.com/u007/ocode/internal/tool"
)

// newPlatformDriver creates the Linux computer driver
// (xdotool/scrot on X11, ydotool/grim on Wayland).
func newPlatformDriver(r commandRunner, _ *tool.ProcessSupervisor) (tool.ComputerDriver, error) {
	return newLinuxDriver(r, nil)
}
