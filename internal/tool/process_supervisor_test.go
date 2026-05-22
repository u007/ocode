package tool

import (
	"context"
	"testing"
	"time"
)

func TestProcessSupervisorShutdownIsIdempotent(t *testing.T) {
	t.Parallel()

	s := NewProcessSupervisor(ProcessSupervisorOptions{GracePeriod: 10 * time.Millisecond})

	var callbackCalls int
	if err := s.RegisterShutdownCallback(func() { callbackCalls++ }); err != nil {
		t.Fatalf("RegisterShutdownCallback() error = %v", err)
	}

	gracefulDone := make(chan struct{})
	forceDone := make(chan struct{})
	forceReleased := make(chan struct{})
	waitCalls := 0
	gracefulCalls := 0
	forceCalls := 0

	_, err := s.Register(ProcessRegistration{
		ID:                    "proc-1",
		Name:                  "sleep",
		Command:               "sleep 30",
		Kind:                  ProcessKindBackgroundBash,
		AllowGracefulShutdown: true,
		waitFn: func() error {
			waitCalls++
			<-forceReleased
			return nil
		},
		gracefulFn: func() error {
			gracefulCalls++
			close(gracefulDone)
			return nil
		},
		forceFn: func() error {
			forceCalls++
			close(forceDone)
			close(forceReleased)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("first Shutdown() error = %v", err)
	}
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("second Shutdown() error = %v", err)
	}

	select {
	case <-gracefulDone:
	default:
		t.Fatal("expected graceful termination attempt")
	}
	select {
	case <-forceDone:
	default:
		t.Fatal("expected force kill fallback")
	}
	if callbackCalls != 1 {
		t.Fatalf("shutdown callback calls = %d, want 1", callbackCalls)
	}
	if waitCalls != 1 {
		t.Fatalf("wait calls = %d, want 1", waitCalls)
	}
	if gracefulCalls != 1 {
		t.Fatalf("graceful calls = %d, want 1", gracefulCalls)
	}
	if forceCalls != 1 {
		t.Fatalf("force calls = %d, want 1", forceCalls)
	}

	record, ok := s.Lookup("proc-1")
	if !ok {
		t.Fatal("expected retained process record")
	}
	if record.Status != ProcKilled {
		t.Fatalf("status = %q, want %q", record.Status, ProcKilled)
	}
	if record.EndedAt.IsZero() {
		t.Fatal("expected terminal timestamp after shutdown")
	}
}

func TestProcessSupervisorShutdownSkipsFinishedChildren(t *testing.T) {
	t.Parallel()

	s := NewProcessSupervisor(ProcessSupervisorOptions{GracePeriod: 10 * time.Millisecond})

	gracefulCalls := 0
	forceCalls := 0
	_, err := s.Register(ProcessRegistration{
		ID:                    "proc-1",
		Name:                  "echo",
		Command:               "echo done",
		Kind:                  ProcessKindBackgroundBash,
		AllowGracefulShutdown: true,
		gracefulFn: func() error {
			gracefulCalls++
			return nil
		},
		forceFn: func() error {
			forceCalls++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if !s.MarkExited("proc-1", 0) {
		t.Fatal("MarkExited() = false, want true")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	if gracefulCalls != 0 {
		t.Fatalf("graceful calls = %d, want 0", gracefulCalls)
	}
	if forceCalls != 0 {
		t.Fatalf("force calls = %d, want 0", forceCalls)
	}

	record, ok := s.Lookup("proc-1")
	if !ok {
		t.Fatal("expected retained finished record")
	}
	if record.Status != ProcExited {
		t.Fatalf("status = %q, want %q", record.Status, ProcExited)
	}
	if record.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", record.ExitCode)
	}
	if !record.Retained {
		t.Fatal("expected finished record to remain retained")
	}
}

func TestProcessSupervisorShutdownForceKillsRunningChild(t *testing.T) {
	t.Parallel()

	s := NewProcessSupervisor(ProcessSupervisorOptions{GracePeriod: 10 * time.Millisecond})

	forceReleased := make(chan struct{})
	gracefulCalls := 0
	forceCalls := 0

	_, err := s.Register(ProcessRegistration{
		ID:                    "proc-1",
		Name:                  "sleep",
		Command:               "sleep 30",
		Kind:                  ProcessKindInteractiveShell,
		AllowGracefulShutdown: true,
		waitFn: func() error {
			<-forceReleased
			return nil
		},
		gracefulFn: func() error {
			gracefulCalls++
			return nil
		},
		forceFn: func() error {
			forceCalls++
			close(forceReleased)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	if gracefulCalls != 1 {
		t.Fatalf("graceful calls = %d, want 1", gracefulCalls)
	}
	if forceCalls != 1 {
		t.Fatalf("force calls = %d, want 1", forceCalls)
	}

	record, ok := s.Lookup("proc-1")
	if !ok {
		t.Fatal("expected retained process record")
	}
	if record.Status != ProcKilled {
		t.Fatalf("status = %q, want %q", record.Status, ProcKilled)
	}
	if record.EndedAt.IsZero() {
		t.Fatal("expected ended timestamp after force kill")
	}
}

func TestProcessSupervisorRetainsFinishedRecordsForSession(t *testing.T) {
	t.Parallel()

	s := NewProcessSupervisor(ProcessSupervisorOptions{GracePeriod: 10 * time.Millisecond})

	_, err := s.Register(ProcessRegistration{
		ID:      "proc-1",
		Name:    "echo",
		Command: "echo retained",
		Kind:    ProcessKindBackgroundBash,
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if !s.MarkExited("proc-1", 0) {
		t.Fatal("MarkExited() = false, want true")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	snapshot := s.Snapshot()
	if len(snapshot) != 1 {
		t.Fatalf("snapshot len = %d, want 1", len(snapshot))
	}
	if snapshot[0].ID != "proc-1" {
		t.Fatalf("snapshot id = %q, want %q", snapshot[0].ID, "proc-1")
	}
	if snapshot[0].Status != ProcExited {
		t.Fatalf("status = %q, want %q", snapshot[0].Status, ProcExited)
	}
	if !snapshot[0].Retained {
		t.Fatal("expected terminal record retention for full session")
	}
}
