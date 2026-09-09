package remote

import (
	"reflect"
	"testing"
)

func TestShellCommandSSH(t *testing.T) {
	cmd := ShellCommand(Target{Kind: KindSSH, User: "u", Host: "h"}, "~/proj dir")
	want := []string{"ssh", "-t", "u@h", `cd "$HOME/proj dir" && exec "${SHELL:-/bin/sh}" -l`}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("args = %q, want %q", cmd.Args, want)
	}
}

func TestShellCommandSSHAbsolutePath(t *testing.T) {
	cmd := ShellCommand(Target{Kind: KindSSH, Host: "h"}, "/srv/it's")
	want := []string{"ssh", "-t", "h", `cd '/srv/it'\''s' && exec "${SHELL:-/bin/sh}" -l`}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("args = %q, want %q", cmd.Args, want)
	}
}

func TestShellCommandWSL(t *testing.T) {
	cmd := ShellCommand(Target{Kind: KindWSL, Distro: "Ubuntu"}, "~/proj")
	want := []string{"wsl.exe", "-d", "Ubuntu", "--cd", "~/proj"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("args = %q, want %q", cmd.Args, want)
	}
	cmd = ShellCommand(Target{Kind: KindWSL}, "/home/x")
	want = []string{"wsl.exe", "--cd", "/home/x"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("args = %q, want %q", cmd.Args, want)
	}
}

func TestShellCommandRejectsEmptyPathOrHost(t *testing.T) {
	if cmd := ShellCommand(Target{Kind: KindSSH, Host: "h"}, ""); cmd != nil {
		t.Fatalf("empty path: got %q, want nil", cmd.Args)
	}
	if cmd := ShellCommand(Target{Kind: KindWSL, Distro: "Ubuntu"}, ""); cmd != nil {
		t.Fatalf("empty WSL path: got %q, want nil", cmd.Args)
	}
	if cmd := ShellCommand(Target{Kind: KindSSH, User: "u"}, "/srv/x"); cmd != nil {
		t.Fatalf("empty SSH host: got %q, want nil", cmd.Args)
	}
	// Empty WSL distro selects the default distro and stays valid.
	if cmd := ShellCommand(Target{Kind: KindWSL}, "/home/x"); cmd == nil {
		t.Fatal("empty WSL distro with non-empty path: got nil, want a command")
	}
}
