package remote

import (
	"context"
	"os/exec"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

// TestSSHTransport_SharedSupervisor_NoIDCollision reproduces the server-side
// remote-connect failure `process "remote-exec-2" already registered`.
//
// The server hands every RemoteWorkspace the same process supervisor
// (internal/server/remote_hosts.go), and the supervisor retains terminal
// records forever. The transport's exec IDs used to restart at remote-exec-1
// per transport instance, so a reconnect — or two transports connecting
// different hosts in parallel — collided on the same index. Because
// BinaryExists swallows its exec error, the reported collision was the SECOND
// exec (DetectPlatform), i.e. remote-exec-2.
func TestSSHTransport_SharedSupervisor_NoIDCollision(t *testing.T) {
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	t.Cleanup(func() { _ = sup.Shutdown(context.Background()) })

	first := NewSSHTransport(Target{}, sup)
	second := NewSSHTransport(Target{}, sup)

	// First transport consumes exec #1 and #2 — the shape of ensureBinary
	// (BinaryExists then DetectPlatform) — leaving two retained records.
	for i := 1; i <= 2; i++ {
		if err := first.run(exec.Command("true"), "exec"); err != nil {
			t.Fatalf("first transport exec %d: %v", i, err)
		}
	}

	// A second transport on the same supervisor (a reconnect, or a parallel
	// connect to another host) must be able to run its own execs.
	for i := 1; i <= 2; i++ {
		if err := second.run(exec.Command("true"), "exec"); err != nil {
			t.Fatalf("second transport exec %d collided: %v", i, err)
		}
	}
}

// TestWSLTransport_SharedSupervisor_NoIDCollision is the WSL counterpart of the
// SSH test above (the two transports keep parallel nextID implementations).
func TestWSLTransport_SharedSupervisor_NoIDCollision(t *testing.T) {
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	t.Cleanup(func() { _ = sup.Shutdown(context.Background()) })

	first := NewWSLTransport("", sup)
	second := NewWSLTransport("", sup)

	for i := 1; i <= 2; i++ {
		if err := first.run(exec.Command("true"), "exec"); err != nil {
			t.Fatalf("first transport exec %d: %v", i, err)
		}
	}
	for i := 1; i <= 2; i++ {
		if err := second.run(exec.Command("true"), "exec"); err != nil {
			t.Fatalf("second transport exec %d collided: %v", i, err)
		}
	}
}
