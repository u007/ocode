package remote

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
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
		// SSHArgs supplies the "--" separator immediately before the destination.
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
	// Hash the CONNECTION identity, not Target.String(): the mux socket must be
	// per host AND port AND user. ssh reuses an existing master without
	// verifying it matches the requested destination, so two ports sharing one
	// ControlPath means a command aimed at port 2222 is executed on whichever
	// port opened the master first. Keep this in step with
	// server.remoteConnectionKey, which keys the registry and exec pool by the
	// same three components; a test pins that they agree.
	sum := sha256.Sum256([]byte(sshConnectionIdentity(t)))
	return filepath.Join(dir, hex.EncodeToString(sum[:8]))
}

// sshConnectionIdentity is the mux-socket identity for t: the same
// user+host+port string the server package's registry and exec pool key on.
// It lives here (not in internal/server) because this package must not import
// server. SSHControlSocketIdentity exposes it for the cross-package test that
// proves the two agree.
func sshConnectionIdentity(t Target) string {
	if t.Port <= 0 {
		return t.String()
	}
	return t.String() + ":" + strconv.Itoa(t.Port)
}

// SSHControlSocketIdentity returns the connection identity used for the mux
// socket, so internal/server can assert its own remoteConnectionKey agrees.
func SSHControlSocketIdentity(t Target) string { return sshConnectionIdentity(t) }

// SSHControlSocketPath returns the ControlPath ssh will use for t.
func SSHControlSocketPath(t Target) string { return sshControlSocket(t) }

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "."
	}
	return home
}
