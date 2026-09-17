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
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// unixShellFallbacks is the resolution order used when neither the caller's
// preferred override nor $SHELL names a usable shell. bash comes first (the
// documented default for agent and `!` commands), then zsh (the macOS user
// default this codebase historically assumed), then sh (the POSIX baseline
// that exists on essentially every Unix).
var unixShellFallbacks = []string{
	"/bin/bash", "/usr/bin/bash",
	"/bin/zsh", "/usr/bin/zsh",
	"/bin/sh", "/usr/bin/sh",
}

// IsExecutable reports whether path names an existing, non-directory file with
// at least one execute bit. It is the guard every shell-resolution path applies
// before returning a candidate: without it, a shell value that is stale,
// misspelled, or copied from another machine reaches exec and fails with the
// opaque `fork/exec /bin/zsh: no such file or directory`.
func IsExecutable(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}

// SystemShells returns the login shells registered in /etc/shells that actually
// exist and are executable, in file order. It is best-effort: a missing or
// unreadable file yields an empty slice rather than an error, so callers
// degrade to their own fallbacks.
func SystemShells() []string {
	data, err := os.ReadFile("/etc/shells")
	if err != nil {
		return nil
	}
	var shells []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if IsExecutable(line) {
			shells = append(shells, line)
		}
	}
	return shells
}

// usableShell reports whether name can be handed to exec: an absolute path must
// point at an executable file, and a bare name must resolve through PATH.
func usableShell(name string) bool {
	if name == "" {
		return false
	}
	if filepath.IsAbs(name) {
		return IsExecutable(name)
	}
	_, err := exec.LookPath(name)
	return err == nil
}

// Resolve returns a usable Unix shell path, preferring preferred, then $SHELL,
// then the common system locations, then the first executable entry in
// /etc/shells, and finally the bare name "sh".
//
// Every absolute candidate must exist and be executable before it is returned,
// so a value that is stale or came from another machine can never reach exec —
// the failure mode this function exists to prevent is
// `fork/exec /bin/zsh: no such file or directory` on a host whose configured
// shell value names a binary the host does not have. The bare "sh" last resort
// is resolved through PATH at exec time, which strictly beats a guaranteed
// ENOENT.
//
// Callers on Windows have their own shells and never reach this function (see
// Build, config.DefaultTerminalShell, and tool.bashInvocation).
func Resolve(preferred string) string {
	for _, candidate := range []string{preferred, os.Getenv("SHELL")} {
		if usableShell(candidate) {
			return candidate
		}
	}
	for _, candidate := range unixShellFallbacks {
		if usableShell(candidate) {
			return candidate
		}
	}
	if shells := SystemShells(); len(shells) > 0 {
		return shells[0]
	}
	return "sh"
}

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
		// Resolve (never a raw $SHELL): a shell value that is stale or was
		// inherited from another machine must not reach exec, or the command
		// fails with an opaque `fork/exec <shell>: no such file or directory`
		// instead of running under an available shell.
		c = exec.CommandContext(ctx, Resolve(""), "-l", "-c", command)
	}
	if dir != "" {
		c.Dir = dir
	}
	if runtime.GOOS != "windows" {
		setProcGroup(c)
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
