package computer

import (
	"errors"

	"github.com/u007/ocode/internal/tool"
)

var ErrSupervisorRequired = errors.New("computer: process supervisor required")
var ErrUnsupportedPlatform = errors.New("computer: unsupported platform")

// New creates a platform computer driver. It requires a non-nil
// process supervisor; the supervisor is used to register every
// subprocess the driver spawns (screenshot, etc.) so they survive
// ocode shutdown and appear in session snapshots.
func New(sup *tool.ProcessSupervisor) (tool.ComputerDriver, error) {
	if sup == nil {
		return nil, ErrSupervisorRequired
	}
	return newPlatformDriver(&execRunner{sup: sup})
}
