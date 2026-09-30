package remote

import (
	"testing"
)

// ExecCommand builds the ssh argv for the whole non-interactive remote
// pipeline (git status/diff/mutations, file reads, shell probe). It must
// separate the target from the options with "--", so a host is always read as a
// destination. Validate/ParseTarget already reject a leading "-", but this is
// the independent second barrier at the point the argv is actually built.
func TestExecCommandSeparatesTargetWithDoubleDash(t *testing.T) {
	cmd, err := ExecCommand(Target{Kind: KindSSH, User: "u", Host: "h"}, "true")
	if err != nil {
		t.Fatal(err)
	}
	args := cmd.Args
	idx := -1
	for i, a := range args {
		if a == "--" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("ExecCommand args have no -- separator: %q", args)
	}
	// After "--" only the target and the remote command may appear.
	rest := args[idx+1:]
	if len(rest) != 2 || rest[0] != "u@h" || rest[1] != "true" {
		t.Fatalf("after -- = %q, want [u@h true]", rest)
	}
}

// The separator must not disturb the port flag: -p is an option, so it has to
// stay on the option side of "--" or ssh would read it as the hostname.
func TestExecCommandPortStaysBeforeSeparator(t *testing.T) {
	cmd, err := ExecCommand(Target{Kind: KindSSH, User: "u", Host: "h", Port: 2222}, "uname -sm")
	if err != nil {
		t.Fatal(err)
	}
	args := cmd.Args
	sep, port := -1, -1
	for i, a := range args {
		if a == "--" && sep < 0 {
			sep = i
		}
		if a == "-p" {
			port = i
		}
	}
	if sep < 0 || port < 0 {
		t.Fatalf("missing -- or -p: %q", args)
	}
	if port > sep {
		t.Fatalf("-p at %d must come before -- at %d: %q", port, sep, args)
	}
	if args[sep+1] != "u@h" {
		t.Fatalf("element after -- = %q, want the target", args[sep+1])
	}
}

// The mux ControlPath must be per user+host+PORT. ssh multiplexes onto an
// existing master without checking that it matches the requested destination,
// so a path shared between two ports means one port's commands run on the
// other's connection.
func TestSSHControlSocketSeparatesPortsAndUsers(t *testing.T) {
	base := Target{Kind: KindSSH, User: "u", Host: "h", Port: 22}
	if SSHControlSocketPath(base) == SSHControlSocketPath(Target{Kind: KindSSH, User: "u", Host: "h", Port: 2222}) {
		t.Fatal("port 22 and port 2222 share one ControlPath; a command aimed at one port would run on the other")
	}
	if SSHControlSocketPath(base) == SSHControlSocketPath(Target{Kind: KindSSH, User: "other", Host: "h", Port: 22}) {
		t.Fatal("two users on one host share one ControlPath")
	}
	// An UNSPECIFIED port must stay a distinct key from an explicit one, even
	// 22. With no -p, ssh resolves the port from ~/.ssh/config, which may remap
	// the host's default away from 22 entirely; treating "unspecified" as "22"
	// would then let a config-dialed connection share a master with an explicit
	// -p 22 command and run it on the wrong port. More keys is the safe
	// direction: the cost of a split is a duplicate master, not wrong data.
	if SSHControlSocketPath(Target{Kind: KindSSH, User: "u", Host: "h"}) ==
		SSHControlSocketPath(Target{Kind: KindSSH, User: "u", Host: "h", Port: 22}) {
		t.Fatal("an unspecified port collided with an explicit :22; ssh_config can remap the default")
	}
}

// The mux identity and the server package's registry/exec-pool key are derived
// in two different packages from the same three components. internal/remote
// cannot import internal/server, so the agreement is asserted from the server
// side (internal/server/remote_hosts_test.go, which calls
// remote.SSHControlSocketIdentity). This stub exists only to keep the remote
// package's own identity pinned to the documented shape.
func TestSSHControlSocketIdentityShape(t *testing.T) {
	if got, want := SSHControlSocketIdentity(Target{Kind: KindSSH, Host: "h"}), "h"; got != want {
		t.Errorf("no port: got %q, want %q", got, want)
	}
	if got, want := SSHControlSocketIdentity(Target{Kind: KindSSH, User: "u", Host: "h", Port: 2222}), "u@h:2222"; got != want {
		t.Errorf("with port: got %q, want %q", got, want)
	}
}
