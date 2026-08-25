package discovery

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// mlxPythonPathOnce/mlxPythonPathVal cache the resolved PATH for the process
// lifetime — a login-shell probe is comparatively expensive and PATH does not
// change while ocode is running.
var (
	mlxPythonPathOnce sync.Once
	mlxPythonPathVal  string
)

// mlxPythonPath returns a PATH string suitable for invoking python3 for the
// MLX local-model backend. A desktop app launched from Finder/Dock inherits
// launchd's bare PATH (/usr/bin:/bin:/usr/sbin:/sbin — no shell rc file is
// ever sourced), so a bare "python3" there resolves to the Xcode Command Line
// Tools stub instead of whatever python3 the user actually installed mlx_lm /
// huggingface_hub into (Homebrew, pyenv, the python.org Framework build,
// ...). That stub launches "successfully" and exits almost immediately on
// ModuleNotFoundError, which used to be indistinguishable from a slow cold
// load — see waitForChatHealth's liveness check for why that looked like a
// 15-minute hang instead of an instant, diagnosable failure.
//
// Resolved once per process by asking the user's own login shell for its
// PATH ($SHELL -ilc), which is where those installs actually live, and
// falling back to the process's own (possibly bare) PATH if that probe fails
// or times out. A marker isolates the PATH line from any MOTD/interactive
// noise the shell may print ahead of it.
func mlxPythonPath() string {
	mlxPythonPathOnce.Do(func() {
		mlxPythonPathVal = os.Getenv("PATH")
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/zsh"
		}
		shell = strings.TrimSpace(shell)
		// Harden $SHELL: reject shell metacharacters and non-executable paths
		// so a poisoned env or rc file cannot redirect this probe to an
		// arbitrary binary. Fall back to the process PATH if invalid.
		if strings.ContainsAny(shell, " \t\n;|&$`'\"") {
			return
		}
		if fi, err := os.Stat(shell); err != nil || fi.IsDir() || fi.Mode()&0o111 == 0 {
			if lp, lookErr := exec.LookPath(shell); lookErr != nil || lp == "" {
				return
			} else {
				shell = lp
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		const marker = "__OCODE_PATH__"
		out, err := exec.CommandContext(ctx, shell, "-ilc", "echo "+marker+"; printf '%s' \"$PATH\"").Output()
		if err != nil {
			return
		}
		// Use LastIndex so an rc file that echoes the marker itself cannot
		// poison the PATH value with early garbage.
		idx := strings.LastIndex(string(out), marker)
		if idx < 0 {
			return
		}
		shellPath := strings.TrimSpace(string(out)[idx+len(marker):])
		if shellPath == "" {
			return
		}
		mlxPythonPathVal = shellPath
	})
	return mlxPythonPathVal
}

// mlxPythonPATH is a deprecated alias for mlxPythonPath (mixedCaps). Kept
// for one release to avoid churn while call sites migrate; new code should
// call mlxPythonPath.
func mlxPythonPATH() string { return mlxPythonPath() }

// mlxPythonBinaryQuoted returns mlxPythonBinary() shell-quoted for safe use
// in a `bash -c` command line. Centralizes the Finder/Dock launchd PATH fix so
// spawnMLXChatServer and spawnMLXServer share one call site and one comment.
func mlxPythonBinaryQuoted() string { return shellQuote(mlxPythonBinary()) }

// mlxPythonBinary resolves an absolute path to python3 by searching
// mlxPythonPath() directly. This is required — not cosmetic — for a direct
// (non-shell) exec.Command("python3", ...): Go resolves a bare argv[0] via
// exec.LookPath against the calling process's own os.Getenv("PATH") at the
// moment exec.Command is called, before cmd.Env is ever consulted, so setting
// cmd.Env cannot change which binary gets found. Falls back to the bare name
// "python3" if no candidate exists on mlxPythonPath(), so the resulting error
// still names the binary that was tried instead of an empty path.
func mlxPythonBinary() string {
	for _, dir := range strings.Split(mlxPythonPath(), string(os.PathListSeparator)) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, "python3")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate
		}
	}
	return "python3"
}
