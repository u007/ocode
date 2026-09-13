//go:build darwin

package computer

import (
	_ "embed"

	"github.com/u007/ocode/internal/tool"
)

//go:embed cgevent_darwin.js
var cgeventJS []byte

// darwinHelper is the process-wide JXA helper file (see helperScript).
var darwinHelper = &helperScript{name: "ocode-cgevent-%d.js", data: cgeventJS}

// newPlatformDriver creates the macOS computer driver.
func newPlatformDriver(r commandRunner, sup *tool.ProcessSupervisor) (tool.ComputerDriver, error) {
	script, err := darwinHelper.ensure(sup)
	if err != nil {
		return nil, err
	}
	return &darwinDriver{r: r, script: script}, nil
}
