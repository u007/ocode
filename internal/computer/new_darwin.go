//go:build darwin

package computer

import (
	"github.com/u007/ocode/internal/tool"
)

// newPlatformDriver creates the macOS computer driver.
func newPlatformDriver(r commandRunner) (tool.ComputerDriver, error) {
	return &darwinDriver{r: r}, nil
}
