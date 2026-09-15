package remote

import (
	"testing"

	"github.com/u007/ocode/internal/tool"
)

func TestForwardRegistrationIDDistinctFromTunnelID(t *testing.T) {
	// StartTunnel keys its own registration on fmt.Sprintf("remote-tunnel-%d",
	// apiPort); ForwardManager must never collide with that ID space for the
	// same numeric port, or ProcessSupervisor.Register would reject the
	// second registration as a duplicate.
	if got := forwardRegistrationID(4096); got == "remote-tunnel-4096" {
		t.Fatalf("forwardRegistrationID(4096) = %q, collides with StartTunnel's ID space", got)
	}
}

func TestForwardManagerIsLiveFalseBeforeStart(t *testing.T) {
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	fm := NewForwardManager(sup, Target{Kind: KindSSH, Host: "example.invalid"})
	if fm.IsLive(3000) {
		t.Fatal("IsLive(3000) = true before any Start call")
	}
}

func TestForwardManagerStopNonLiveIsNoop(t *testing.T) {
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	fm := NewForwardManager(sup, Target{Kind: KindSSH, Host: "example.invalid"})
	if err := fm.Stop(3000); err != nil {
		t.Fatalf("Stop on a never-started port returned an error: %v", err)
	}
}
