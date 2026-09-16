package remote

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ValidateTargetOS reports whether t's transport is usable on this OS
// (WSL targets require Windows). Exported wrapper of validateTargetOS for
// callers outside this package.
func ValidateTargetOS(t Target) error {
	return validateTargetOS(t.Kind, runtime.GOOS)
}

// ShellQuote quotes s as a single POSIX shell word.
func ShellQuote(s string) string { return shellQuote(s) }

// ShellQuotePath quotes a remote path for a shell command, translating a
// leading "~"/"~/" to "$HOME" expansion (see shellQuotePath).
func ShellQuotePath(p string) string { return shellQuotePath(p) }

// ExecCommand builds the local process that runs one non-interactive
// command on t (stdout/stderr capture is the caller's responsibility).
// It is the command-builder counterpart of ShellCommand (interactive):
//
//   - SSH: system ssh with BatchMode (fail fast instead of hanging the
//     caller on an invisible password prompt) plus ControlMaster
//     multiplexing (skipped on Windows where Win32-OpenSSH does not
//     support it) so a burst of git/file requests reuses one connection.
//   - WSL: wsl.exe -- sh -c (same shape as WSLTransport.Exec).
//
// Known residuals (deliberate, documented here so a future reader doesn't
// "fix" them by accident):
//   - ExecCommand multiplexes (ControlMaster/ControlPersist) while
//     SSHTransport.Exec opens a fresh connection per call — two
//     independent pools for the same target. Killing one mux master on
//     timeout can tear down in-flight requests sharing it.
//   - Neither path registers with the ProcessSupervisor, so a kill -9 on
//     ocode orphans the ssh child (same residual class as background bash
//     commands before the parent-monitor fix).
//
// It returns an error (never a nil cmd with nil error) for empty commands,
// invalid targets, or targets whose transport is unusable on this OS.
func ExecCommand(t Target, command string) (*exec.Cmd, error) {
	if strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("remote command is empty")
	}
	if err := ValidateTargetOS(t); err != nil {
		return nil, err
	}
	if err := t.Validate(); err != nil {
		return nil, fmt.Errorf("invalid remote target: %w", err)
	}
	switch t.Kind {
	case KindWSL:
		return exec.Command("wsl.exe", wslExecArgs(t.Distro, command)...), nil
	case KindSSH:
		if t.Host == "" {
			return nil, fmt.Errorf("SSH host is required")
		}
		args := []string{"-o", "BatchMode=yes"}
		if runtime.GOOS != "windows" {
			args = append(args,
				"-o", "ControlMaster=auto",
				"-o", "ControlPath="+sshControlSocket(t),
				"-o", "ControlPersist=600",
			)
		}
		args = append(args, t.SSHArgs()...)
		return exec.Command("ssh", append(args, command)...), nil
	default:
		return nil, fmt.Errorf("unsupported remote target kind")
	}
}

// sshControlSocket returns a stable, short ControlPath for the target so
// repeated non-interactive execs reuse one SSH master connection. The dir
// is created lazily (best-effort: ssh falls back to a plain connection if
// the socket can't be created). The socket name is a hash of the canonical
// target — keeps the path well under the ~104-char unix-socket limit even
// for long user@host strings.
func sshControlSocket(t Target) string {
	dir := filepath.Join(homeDir(), ".ocode", "ssh-mux")
	_ = os.MkdirAll(dir, 0o700)
	sum := sha256.Sum256([]byte(t.String()))
	return filepath.Join(dir, hex.EncodeToString(sum[:8]))
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "."
	}
	return home
}
