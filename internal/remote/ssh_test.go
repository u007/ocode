package remote

import (
	"reflect"
	"strings"
	"testing"
)

// TestSSHTransportExecHasFailFastArgs pins the fix for a remote SSH connect
// that could hang the whole request goroutine: SSHTransport.Exec (the transport
// behind reachability, platform detect, provisioning, server discovery, and
// credential sync) used to invoke bare `ssh <target> <command>`. Without
// BatchMode, ssh reads a password/passphrase from /dev/tty; when ocode's server
// was started from a terminal that tty exists, so ssh blocks on an invisible
// prompt with no way to fail. ConnectTimeout bounds an unreachable host too.
func TestSSHTransportExecHasFailFastArgs(t *testing.T) {
	tr := NewSSHTransport(Target{Kind: KindSSH, User: "u", Host: "h"}, nil)
	got := tr.commandArgs("true")
	want := []string{
		"-o", "BatchMode=yes",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "ConnectTimeout=15",
		"--", "u@h", "true",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commandArgs = %q, want %q", got, want)
	}
}

func TestSSHTransportExecArgsIncludePort(t *testing.T) {
	tr := NewSSHTransport(Target{Kind: KindSSH, User: "u", Host: "h", Port: 2222}, nil)
	got := tr.commandArgs("uname -sm")
	want := []string{
		"-o", "BatchMode=yes",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "ConnectTimeout=15",
		"-p", "2222", "--", "u@h", "uname -sm",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commandArgs = %q, want %q", got, want)
	}
}

// TestSSHTransportExecStdinSharesFailFastArgs guards against Exec and ExecStdin
// drifting: both build their argument list from commandArgs.
func TestSSHTransportExecStdinSharesFailFastArgs(t *testing.T) {
	tr := NewSSHTransport(Target{Kind: KindSSH, Host: "h"}, nil)
	got := tr.commandArgs("cat >/tmp/x")
	joined := strings.Join(got, " ")
	for _, opt := range []string{"BatchMode=yes", "ConnectTimeout=15", "ServerAliveInterval=15", "ServerAliveCountMax=3"} {
		if !strings.Contains(joined, opt) {
			t.Fatalf("Exec/ExecStdin ssh args missing %s: %q", opt, got)
		}
	}
}

// TestScpFailFastArgs pins the upload path: a cross-compiled binary upload must
// not block on a passphrase prompt, and the scp port flag is -P (not ssh's -p).
func TestScpFailFastArgs(t *testing.T) {
	got := scpFailFastArgs(Target{Kind: KindSSH, User: "u", Host: "h", Port: 2222})
	want := []string{
		"-o", "BatchMode=yes",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "ConnectTimeout=15",
		"-P", "2222",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scpFailFastArgs = %q, want %q", got, want)
	}
	if strings.Contains(strings.Join(got, " "), "-p 2222") {
		t.Fatalf("scp used ssh's -p instead of scp's -P: %q", got)
	}
}

func TestScpFailFastArgsOmitsDefaultPort(t *testing.T) {
	got := scpFailFastArgs(Target{Kind: KindSSH, Host: "h"})
	if strings.Contains(strings.Join(got, " "), "-P") {
		t.Fatalf("default-port scp must not carry -P: %q", got)
	}
	if !strings.Contains(strings.Join(got, " "), "BatchMode=yes") {
		t.Fatalf("scp missing BatchMode: %q", got)
	}
}

// TestExecInteractiveKeepsPrompting ensures the fail-fast block is NOT applied
// to the interactive path: `ocode remote` must still be able to prompt for a
// passphrase at a real terminal.
func TestExecInteractiveKeepsPrompting(t *testing.T) {
	tr := NewSSHTransport(Target{Kind: KindSSH, User: "u", Host: "h"}, nil)
	args := append([]string{"-t"}, tr.Target.SSHArgs()...)
	if strings.Contains(strings.Join(args, " "), "BatchMode") {
		t.Fatalf("interactive ssh must not force BatchMode: %q", args)
	}
}

// Every ssh argv must carry a "--" immediately before the target, so a host is
// read as a destination and never as an option. This is the second barrier
// behind Validate/ParseTarget: verified against real ssh, an option-shaped
// token is executed (ProxyCommand runs through a shell), while the same token
// after "--" is rejected as an invalid hostname.
func TestSSHArgsAlwaysSeparateTargetWithDoubleDash(t *testing.T) {
	tr := NewSSHTransport(Target{Kind: KindSSH, User: "u", Host: "h"}, nil)
	args := tr.commandArgs("true")
	idx := -1
	for i, a := range args {
		if a == "--" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("commandArgs has no -- separator: %q", args)
	}
	// Everything after -- must be target + command only, never an option.
	for _, a := range args[idx+1:] {
		if strings.HasPrefix(a, "-") && a != "--" {
			t.Fatalf("option %q appears after the -- separator: %q", a, args)
		}
	}
}
