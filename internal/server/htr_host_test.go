package server

import (
	"testing"

	"github.com/u007/ocode/internal/remote"
	"github.com/u007/ocode/internal/tool"
)

// The remote port is a host-wide resource, so two projects on one SSH host
// must share one reverse-forward state; a per-project manager made the second
// project's `ssh -R` collide with the first's.
func TestHTRHostIsSharedAcrossProjectsOnOneHost(t *testing.T) {
	reg := newPortMapRegistry(tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{}))
	a, err := remote.ParseTarget("user@devbox")
	if err != nil {
		t.Fatal(err)
	}
	b, err := remote.ParseTarget("user@devbox")
	if err != nil {
		t.Fatal(err)
	}
	other, err := remote.ParseTarget("user@other")
	if err != nil {
		t.Fatal(err)
	}
	if reg.htrHost(a) != reg.htrHost(b) {
		t.Fatal("two projects on user@devbox got different htr reverse-forward state")
	}
	if reg.htrHost(a) == reg.htrHost(other) {
		t.Fatal("different hosts share htr reverse-forward state")
	}
}
