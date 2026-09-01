package tool

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"

	"github.com/u007/ocode/internal/shell/sandbox"
)

// bashInvocation returns the platform shell argv for a bash-tool command:
// `bash -c <command>` on Unix, `cmd /C <command>` on Windows.
func bashInvocation(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C", command}
	}
	return "bash", []string{"-c", command}
}

// buildBashCmd is the single bash-command construction site for both the
// foreground (exec.go) and background (process.go) paths. It encapsulates the
// GOOS branch, the session-workdir wiring, and — when active — the sandbox
// wrap.
//
// A non-nil ctx is honored via exec.CommandContext so a foreground timeout can
// kill the child (and, via setProcGroup, its whole process group); a nil ctx —
// the background launch — uses plain exec.Command semantics so the child is
// NOT tied to a caller context that may be cancelled mid-flight.
//
// dir sets cmd.Dir when non-empty (the session project root), so relative
// commands resolve against the session, not the process working directory.
//
// Sandbox semantics (fail-closed): when active, the command MUST be confined.
// If the backend is not Available(), an error is returned and cmd is nil — the
// caller must never run the plain command unconfined. When inactive, the cmd
// is returned plain and the wrapper is never consulted.
func buildBashCmd(ctx context.Context, command, dir string, w sandbox.Wrapper, roots sandbox.RootSet, active bool) (*exec.Cmd, error) {
	shell, args := bashInvocation(command)
	var cmd *exec.Cmd
	if ctx != nil {
		cmd = exec.CommandContext(ctx, shell, args...)
	} else {
		cmd = exec.Command(shell, args...)
	}
	if runtime.GOOS != "windows" {
		setProcGroup(cmd)
	}
	if dir != "" {
		cmd.Dir = dir
	}
	if !active {
		return cmd, nil
	}
	if w == nil || !w.Available() {
		return nil, fmt.Errorf("sandbox mode active but backend unavailable: refusing to run unconfined command %q", command)
	}
	return w.Wrap(cmd, roots)
}