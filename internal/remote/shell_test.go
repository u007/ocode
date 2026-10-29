package remote

import (
	"reflect"
	"strings"
	"testing"
)

// keepaliveArgs is the expected keepalive block every long-lived SSH process
// must carry; see sshKeepaliveArgs for the rationale.
var keepaliveArgs = []string{
	"-o", "ServerAliveInterval=15",
	"-o", "ServerAliveCountMax=3",
	"-o", "ConnectTimeout=15",
}

func TestShellCommandSSH(t *testing.T) {
	cmd := ShellCommand(Target{Kind: KindSSH, User: "u", Host: "h"}, "~/proj dir", "")
	want := append(append([]string{"ssh", "-t"}, keepaliveArgs...), "--", "u@h", launchScriptWithCd("~/proj dir", ""))
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("args = %q, want %q", cmd.Args, want)
	}
	launch := cmd.Args[len(cmd.Args)-1]
	// The remote shell must be chosen on the remote: a stale remote $SHELL
	// (set but not executable) has to fall through, which `${SHELL:-/bin/sh}`
	// could not do.
	if strings.Contains(launch, "${SHELL:-") {
		t.Fatalf("launch still trusts an unset-only $SHELL expansion: %q", launch)
	}
	if !strings.Contains(launch, `[ -x "$c" ]`) {
		t.Fatalf("launch does not validate executability: %q", launch)
	}
}

func TestShellCommandSSHAbsolutePath(t *testing.T) {
	cmd := ShellCommand(Target{Kind: KindSSH, Host: "h"}, "/srv/it's", "")
	want := append(append([]string{"ssh", "-t"}, keepaliveArgs...), "--", "h", launchScriptWithCd("/srv/it's", ""))
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("args = %q, want %q", cmd.Args, want)
	}
}

func TestShellCommandSSHPort(t *testing.T) {
	cmd := ShellCommand(Target{Kind: KindSSH, User: "u", Host: "h", Port: 2222}, "/srv/app", "")
	want := append(append([]string{"ssh", "-t"}, keepaliveArgs...), "-p", "2222", "--", "u@h", launchScriptWithCd("/srv/app", ""))
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("args = %q, want %q", cmd.Args, want)
	}
}

// TestShellCommandSSHHasKeepalive pins the fix for a remote SSH terminal that
// never disconnected or reconnected: without ServerAlive* the pty stays open
// on a silently-dead tunnel, so the session never exits and the client never
// reconnects.
func TestShellCommandSSHHasKeepalive(t *testing.T) {
	cmd := ShellCommand(Target{Kind: KindSSH, Host: "h"}, "/srv/app", "")
	joined := strings.Join(cmd.Args, " ")
	for _, opt := range []string{"ServerAliveInterval=15", "ServerAliveCountMax=3", "ConnectTimeout=15"} {
		if !strings.Contains(joined, opt) {
			t.Fatalf("interactive ssh missing %s: %q", opt, cmd.Args)
		}
	}
}

func TestShellCommandWSL(t *testing.T) {
	cmd := ShellCommand(Target{Kind: KindWSL, Distro: "Ubuntu"}, "~/proj", "")
	want := []string{"wsl.exe", "-d", "Ubuntu", "--cd", "~/proj"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("args = %q, want %q", cmd.Args, want)
	}
	cmd = ShellCommand(Target{Kind: KindWSL}, "/home/x", "")
	want = []string{"wsl.exe", "--cd", "/home/x"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("args = %q, want %q", cmd.Args, want)
	}
}

func TestShellCommandRejectsEmptyPathOrHost(t *testing.T) {
	if cmd := ShellCommand(Target{Kind: KindSSH, Host: "h"}, "", ""); cmd != nil {
		t.Fatalf("empty path: got %q, want nil", cmd.Args)
	}
	if cmd := ShellCommand(Target{Kind: KindWSL, Distro: "Ubuntu"}, "", ""); cmd != nil {
		t.Fatalf("empty WSL path: got %q, want nil", cmd.Args)
	}
	if cmd := ShellCommand(Target{Kind: KindSSH, User: "u"}, "/srv/x", ""); cmd != nil {
		t.Fatalf("empty SSH host: got %q, want nil", cmd.Args)
	}
	// Empty WSL distro selects the default distro and stays valid.
	if cmd := ShellCommand(Target{Kind: KindWSL}, "/home/x", ""); cmd == nil {
		t.Fatal("empty WSL distro with non-empty path: got nil, want a command")
	}
}

func TestShellCommandWithMuxNameAttachesTmuxSession(t *testing.T) {
	name := TerminalMuxName("u@h", "/srv/app", "term-1")
	cmd := ShellCommand(Target{Kind: KindSSH, User: "u", Host: "h"}, "/srv/app", name)
	last := cmd.Args[len(cmd.Args)-1]
	if !strings.Contains(last, "tmux new-session -A -s ") && strings.Contains(last, name) {
		t.Fatalf("launcher must attach-or-create the named tmux session, got %q", last)
	}
	// A host without tmux must still get the plain login shell.
	if !strings.Contains(last, "command -v tmux") || !strings.Contains(last, "exec \"$c\" -l") {
		t.Fatalf("launcher must fall through to the login shell when tmux is absent, got %q", last)
	}
}

func TestTerminalMuxNameStablePerTab(t *testing.T) {
	a := TerminalMuxName("h", "/p", "t1")
	if a != TerminalMuxName("h", "/p", "t1") {
		t.Fatal("same tab must map to the same tmux session")
	}
	if a == TerminalMuxName("h", "/p", "t2") || a == TerminalMuxName("h", "/q", "t1") || a == TerminalMuxName("g", "/p", "t1") {
		t.Fatal("different tab, path or host must get a different session")
	}
}
