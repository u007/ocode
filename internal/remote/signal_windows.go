//go:build windows

package remote

import (
	"os"
	"syscall"
)

// stillActive is the exit code GetExitCodeProcess reports for a running
// process (STILL_ACTIVE).
const stillActive = 259

// processAlive reports whether p still exists. Windows has no signal 0
// (os.Process.Signal only accepts Kill/Interrupt), so query the exit code
// through a process handle instead.
func processAlive(p *os.Process) bool {
	h, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(p.Pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}
