package server

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

func TestProcessSupervisorNonNil(t *testing.T) {
	s := New("127.0.0.1:0", "", "", nil)
	if s.ProcessSupervisor() == nil {
		t.Fatal("expected ProcessSupervisor() to be non-nil after New")
	}
}

func TestProcessSupervisorShutdownOnServerShutdown(t *testing.T) {
	s := New("127.0.0.1:0", "", "", nil)
	ps := s.ProcessSupervisor()

	cmd := exec.Command("/bin/sleep", "30")
	if _, err := tool.StartSupervised(ps, cmd, tool.ProcessRegistration{ID: "browser-1", Name: "chrome"}); err != nil {
		t.Fatalf("StartSupervised() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	// The child must have been terminated by the supervisor during Shutdown.
	snap := ps.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("snapshot len = %d, want 1", len(snap))
	}
	if snap[0].Status == tool.ProcRunning {
		t.Fatalf("expected child not running after shutdown, got status %q", snap[0].Status)
	}
	// The manager owns Wait — reap the terminated child.
	_ = cmd.Wait()

	// The supervisor is shut down, so new registrations are refused.
	if _, err := ps.Register(tool.ProcessRegistration{ID: "browser-2", Name: "chrome"}); err != tool.ErrProcessSupervisorClosed {
		t.Fatalf("Register() error = %v, want ErrProcessSupervisorClosed", err)
	}
}
