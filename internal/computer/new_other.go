//go:build !darwin && !windows && !linux

package computer

import (
	"github.com/u007/ocode/internal/tool"
)

// newPlatformDriver returns the unsupported-platform error for any
// OS not covered by the per-platform build-tagged files.
func newPlatformDriver(r commandRunner) (tool.ComputerDriver, error) {
	return nil, ErrUnsupportedPlatform
}
