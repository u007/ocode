//go:build !windows

package tool

import (
	"os/exec"
	"testing"
	"time"
)

// TestStartSupervisedRegistersBrowserRecord lives here because it asserts on
// cmd.SysProcAttr.Setsid, which does not exist on Windows. Keeping it in the
// untagged process_supervisor_test.go broke the Windows build of this package's
// tests (and with it every test in the package).
func TestStartSupervisedRegistersBrowserRecord(t *testing.T) {
	t.Parallel()

	s := NewProcessSupervisor(ProcessSupervisorOptions{GracePeriod: 10 * time.Millisecond})

	cmd := exec.Command("/bin/sleep", "30")
	reg := ProcessRegistration{
		ID:                    "browser-1",
		Name:                  "chrome",
		Command:               "/bin/sleep 30",
		Kind:                  ProcessKindBrowser,
		AllowGracefulShutdown: true,
	}

	rec, err := StartSupervised(s, cmd, reg)
	if err != nil {
		t.Fatalf("StartSupervised() error = %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})

	if rec.Kind != ProcessKindBrowser {
		t.Fatalf("rec.Kind = %q, want %q", rec.Kind, ProcessKindBrowser)
	}
	if rec.PID <= 0 {
		t.Fatalf("rec.PID = %d, want > 0", rec.PID)
	}
	if !rec.OwnsProcessGroup {
		t.Fatal("expected OwnsProcessGroup to be true")
	}
	if cmd.SysProcAttr == nil {
		t.Fatal("expected SysProcAttr to be set")
	}
	if !cmd.SysProcAttr.Setsid {
		t.Fatalf("expected Setsid process group setup, got %#v", cmd.SysProcAttr)
	}

	// The record must be visible in Snapshot().
	snap := s.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("snapshot len = %d, want 1", len(snap))
	}
	if snap[0].ID != "browser-1" || snap[0].PID != rec.PID {
		t.Fatalf("unexpected snapshot record: %+v", snap[0])
	}
}
