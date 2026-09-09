package crashguard

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestGoRunsHookThenRePanics runs the guarded goroutine in a child process
// (the re-panic is fatal) and asserts the hook fired before the crash.
func TestGoRunsHookThenRePanics(t *testing.T) {
	if os.Getenv("CRASHGUARD_CHILD") == "1" {
		SetOnPanic(func() { os.Stdout.WriteString("HOOK-RAN\n") })
		done := make(chan struct{})
		Go(func() {
			defer close(done)
			panic("boom")
		})
		<-done
		select {} // re-panic terminates the process before this blocks forever
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestGoRunsHookThenRePanics")
	cmd.Env = append(os.Environ(), "CRASHGUARD_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected child to crash, got success:\n%s", out)
	}
	s := string(out)
	if !strings.Contains(s, "HOOK-RAN") {
		t.Fatalf("hook did not run before re-panic:\n%s", s)
	}
	if !strings.Contains(s, "goroutine panic: boom") {
		t.Fatalf("stack not printed:\n%s", s)
	}
	if strings.Index(s, "HOOK-RAN") > strings.Index(s, "goroutine panic") {
		t.Fatalf("hook must run before the trace:\n%s", s)
	}
}
