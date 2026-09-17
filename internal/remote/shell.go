package remote

import "os/exec"

// ShellCommand builds the local process that gives an interactive login
// shell on target with its cwd set to path — what the web UI's terminal tab
// pty-starts for a remote project instead of a local shell. Like the rest
// of this package it drives the system ssh/wsl.exe binaries so ssh_config,
// agent forwarding and ProxyJump apply unchanged. The caller owns stdio
// (typically a pty), so nothing is attached here.
//
// SSH: `cd` runs on the remote so "~" is expanded there (shellQuotePath's
// $HOME translation); the login shell is whatever the remote account uses.
// WSL: wsl.exe's own --cd flag understands Linux paths including "~", and
// its default action with no command is the distro's login shell.
//
// The caller must pass a registered non-empty host/path: path must be
// non-empty, and SSH targets must carry a non-empty Host (ParseTarget
// guarantees this; an empty Distro on WSL targets means the default distro
// and is valid). An empty path would build a broken cd command (SSH) or an
// empty --cd flag (WSL), and an empty SSH host would leave ssh with a
// missing target, so invalid input returns nil instead of a broken command.
func ShellCommand(t Target, path string) *exec.Cmd {
	if path == "" {
		return nil
	}
	if t.Kind == KindSSH && t.Host == "" {
		return nil
	}
	if t.Kind == KindWSL {
		args := []string{}
		if t.Distro != "" {
			args = append(args, "-d", t.Distro)
		}
		args = append(args, "--cd", path)
		return exec.Command("wsl.exe", args...)
	}
	remoteCmd := launchScriptWithCd(path)
	args := append([]string{"-t"}, sshKeepaliveArgs()...)
	args = append(args, t.SSHArgs()...)
	return exec.Command("ssh", append(args, remoteCmd)...)
}

// sshKeepaliveArgs bounds how long a long-lived SSH process (an interactive
// terminal, the remote-workspace tunnel, a port forward) can sit on a
// silently-dead transport. Without these options ssh never notices a dropped
// tunnel: TCP keepalive defaults to a ~2h idle timeout, and the terminal
// session's WebSocket ping only tests the browser↔local-server hop, which
// stays healthy while the SSH connection is gone. The user then sees a frozen
// terminal that never disconnects or reconnects. With ServerAliveInterval=15
// and ServerAliveCountMax=3, an unresponsive peer makes ssh exit within ~45s,
// which closes the pty, ends the session, and lets the client auto-reconnect.
// ConnectTimeout bounds a (re)connect attempt against an unreachable host so
// it fails fast instead of hanging on the OS-level TCP timeout.
func sshKeepaliveArgs() []string {
	return []string{
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "ConnectTimeout=15",
	}
}
