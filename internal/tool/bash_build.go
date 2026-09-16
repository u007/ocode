package tool

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"sync"

	"github.com/u007/ocode/internal/shell/sandbox"
)

// loginShellOverride pins the shell the bash tool runs commands through, as a
// login shell. Empty means the default non-login `bash -c`.
var (
	loginShellMu       sync.RWMutex
	loginShellOverride string
)

// SetLoginShell pins the shell the bash tool executes commands through:
// `exec.Command(shell, "-l", "-c", command)` instead of plain `bash -c`.
// The desktop app calls this once at boot: a Finder/Dock-launched .app
// inherits launchd's minimal PATH (/usr/bin:/bin:/usr/sbin:/sbin) and no
// $SHELL, so a non-login bash cannot find user toolchains initialized in
// profile files (homebrew, nvm, go — "go: command not found"). A login shell
// sources /etc/zprofile and ~/.zprofile (zsh) or /etc/profile and
// ~/.bash_profile (bash), restoring the user's PATH. An empty shell restores
// the default `bash -c` behavior — TUI and server modes launched from a
// shell never need it. Safe to call concurrently.
// See docs/superpowers/specs/2026-09-10-desktop-login-shell-env.md.
func SetLoginShell(shell string) {
	loginShellMu.Lock()
	loginShellOverride = shell
	loginShellMu.Unlock()
}

// loginShell returns the configured login-shell override, or "" when unset.
func loginShell() string {
	loginShellMu.RLock()
	defer loginShellMu.RUnlock()
	return loginShellOverride
}

// bashInvocation returns the platform shell argv for a bash-tool command:
// `<configured login shell> -l -c <command>` when SetLoginShell has pinned a
// shell (desktop), `bash -c <command>` on Unix otherwise, and
// `cmd /C <command>` on Windows.
func bashInvocation(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C", command}
	}
	if shell := loginShell(); shell != "" {
		return shell, []string{"-l", "-c", command}
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
