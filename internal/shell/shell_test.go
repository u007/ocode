package shell

import (
	"strings"
	"testing"
	"time"
)

func TestRun_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("requires bash")
	}
	res := Run("echo hello", "")
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", res.ExitCode)
	}
	if got := strings.TrimSpace(res.Output); got != "hello" {
		t.Fatalf("output = %q, want %q", got, "hello")
	}
}

func TestRun_NonZeroExit(t *testing.T) {
	if testing.Short() {
		t.Skip("requires bash")
	}
	res := Run("exit 7", "")
	if res.Err != nil {
		t.Fatalf("non-zero exit should not set Err, got: %v", res.Err)
	}
	if res.ExitCode != 7 {
		t.Fatalf("exit code = %d, want 7", res.ExitCode)
	}
}

func TestRun_StartFailure(t *testing.T) {
	// An invalid working directory makes exec.Cmd.Start fail (no such
	// file / not a directory), which is a non-exit error: Run must
	// surface it via Result.Err, not via ExitCode.
	res := RunWithTimeout("echo hi", "/nonexistent-cwd-xyz", 5*time.Second)
	if res.Err == nil {
		t.Fatalf("expected error for invalid working dir, got nil")
	}
}

func TestRunWithTimeout_TimesOut(t *testing.T) {
	if testing.Short() {
		t.Skip("requires bash")
	}
	res := RunWithTimeout("sleep 10", "", 200*time.Millisecond)
	if res.Err == nil {
		t.Fatalf("expected timeout error, got nil (exit=%d)", res.ExitCode)
	}
	if !strings.Contains(res.Err.Error(), "timed out") {
		t.Fatalf("error = %v, want it to mention 'timed out'", res.Err)
	}
}

func TestRun_StderrCaptured(t *testing.T) {
	if testing.Short() {
		t.Skip("requires bash")
	}
	res := Run("echo err 1>&2", "")
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", res.ExitCode)
	}
	if !strings.Contains(res.Output, "err") {
		t.Fatalf("output = %q, want it to contain 'err'", res.Output)
	}
}
