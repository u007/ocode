// Package shell provides a small, cross-platform helper for running a shell
// command non-interactively, capturing combined stdout+stderr, and reporting
// the exit code with a clear error for non-exit failures. It exists so the
// TUI agent loop and the server-side /api/shell handler can share one
// implementation of "spawn bash, capture output" — the two callers have
// nearly identical requirements (timeout, Setpgid for cleanup, combined
// stdout/stderr, exit code extraction) and previously duplicated the code.
package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"syscall"
	"time"
)

// DefaultTimeout is the upper bound for a single shell invocation. Callers
// can override it with RunWithTimeout.
const DefaultTimeout = 600 * time.Second

// Result captures the outcome of a Run invocation. Output holds combined
// stdout+stderr (the helper pipes both to the same buffer, mirroring the
// historical TUI behavior). ExitCode is 0 on success and the process exit
// code on a non-zero exit. Err is non-nil only for non-exit failures
// (start failure, timeout, kill); a non-zero exit code does NOT set Err —
// callers should branch on ExitCode instead.
type Result struct {
	Output   string
	ExitCode int
	Err      error
}

// Build returns a configured *exec.Cmd that runs command (via `bash -c` on
// Unix or `cmd /C` on Windows) with the given working directory and the
// Unix-only Setpgid cleanup hook. The caller is responsible for calling
// cmd.Start / cmd.Run / cmd.Wait and for releasing any process-group
// resources. The timeout is supplied via the context. This is the same
// machinery Run uses internally; it's exposed for callers (notably the
// TUI agent loop) that need to register the cmd with a process supervisor
// before running it.
func Build(ctx context.Context, command string, dir string) *exec.Cmd {
	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		c = exec.CommandContext(ctx, "bash", "-c", command)
	}
	if dir != "" {
		c.Dir = dir
	}
	if runtime.GOOS != "windows" {
		c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	return c
}

// Run executes command (passed to `bash -c` on Unix or `cmd /C` on Windows)
// with the given working directory and the package's DefaultTimeout. The
// returned Result is always populated; the helper never panics on a
// non-zero exit. dir may be "" to inherit the calling process's working
// directory.
func Run(command string, dir string) Result {
	return RunWithTimeout(command, dir, DefaultTimeout)
}

// RunWithTimeout is Run with an explicit timeout. A timeout produces a
// Result with Err set to a descriptive error and ExitCode 1 (the process
// is killed via the context).
func RunWithTimeout(command string, dir string, timeout time.Duration) Result {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	c := Build(ctx, command, dir)

	var buf bytes.Buffer
	c.Stdout = &buf
	c.Stderr = &buf

	err := c.Run()
	out := buf.String()

	res := Result{Output: out}
	if err == nil {
		return res
	}

	// Context-deadline / cancelled: surface as a clear error, not a
	// confusing exec.ExitError.
	if ctx.Err() == context.DeadlineExceeded {
		res.ExitCode = 1
		res.Err = fmt.Errorf("command timed out after %s", timeout)
		return res
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		// Deliberately leave res.Err nil: a non-zero exit is already
		// represented by ExitCode, and callers that want to render
		// "exit status N" do so themselves. Returning err.Error() here
		// duplicates that string in the user-facing output.
		return res
	}

	res.ExitCode = 1
	res.Err = err
	return res
}
