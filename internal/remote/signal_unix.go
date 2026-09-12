//go:build !windows

package remote

import (
	"os"
	"syscall"
)

// processAlive reports whether p still exists. Signal 0 performs error
// checking without delivering a signal: nil means the process exists, ESRCH
// ("no such process") means it exited.
func processAlive(p *os.Process) bool {
	return p.Signal(syscall.Signal(0)) == nil
}
