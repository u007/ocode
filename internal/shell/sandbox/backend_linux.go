//go:build linux

package sandbox

import "os"

// prodLinuxProbes returns the real availability probes for the Linux backend:
// Landlock via kernel syscalls, bubblewrap via the trusted absolute path.
func prodLinuxProbes() linuxBackendProbes {
	return linuxBackendProbes{
		landlockUsable: landlockUsable,
		bwrapUsable:    bwrapUsable,
		executable:     os.Executable,
	}
}

// newWrapper selects the real Linux backend (Landlock preferred, bwrap
// fallback).
func newWrapper() Wrapper { return newLinuxWrapper(prodLinuxProbes()) }
