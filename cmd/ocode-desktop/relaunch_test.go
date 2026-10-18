package main

import (
	"strings"
	"testing"
	"time"
)

// TestRelaunchHelperArgvPassesPathsOutOfBand pins the contract that made the
// original restart helper unsafe to test: the pid, bundle, timeout and log path
// must travel as ARGV, never interpolated into the shell script. Go's %q is a Go
// string literal, not shell-safe, so a bundle path containing `$` or a backtick
// would expand or command-substitute inside the script's double quotes.
func TestRelaunchHelperArgvPassesPathsOutOfBand(t *testing.T) {
	// Deliberately hostile: both $ and a backtick, which %q would not escape.
	const bundle = "/Applications/ocode $HOME `id`.app"
	const logPath = "/tmp/ocode desktop $x.log"

	argv := relaunchHelperArgv(4242, bundle, 20*time.Second, logPath)

	if len(argv) != 7 {
		t.Fatalf("argv has %d elements, want 7: %#v", len(argv), argv)
	}
	if argv[0] != "-c" || argv[2] != "sh" {
		t.Fatalf("argv[0]/argv[2] = %q/%q, want -c/sh", argv[0], argv[2])
	}
	if argv[3] != "4242" {
		t.Fatalf("argv[3] (pid) = %q, want 4242", argv[3])
	}
	if argv[4] != bundle {
		t.Fatalf("argv[4] (bundle) = %q, want %q", argv[4], bundle)
	}
	if argv[5] != "20" {
		t.Fatalf("argv[5] (timeout seconds) = %q, want 20", argv[5])
	}
	if argv[6] != logPath {
		t.Fatalf("argv[6] (log) = %q, want %q", argv[6], logPath)
	}

	script := argv[1]
	// The backgrounded subshell is what reparents the worker to launchd, so it
	// is never a descendant of the app it signals.
	if !strings.Contains(script, ") &") {
		t.Fatalf("script must background a subshell (double-fork):\n%s", script)
	}
	// Nothing that varies is interpolated into the script text.
	if strings.Contains(script, bundle) || strings.Contains(script, logPath) {
		t.Fatal("paths must not be interpolated into the script; pass them as argv")
	}
	if strings.Contains(script, "%q") {
		t.Fatalf("script contains a %%q verb; paths must not be Go-quoted into the shell")
	}
	// The worker reads every input from a positional parameter.
	for _, want := range []string{`"$1"`, `"$2"`, `"$3"`, `"$4"`} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing positional parameter %s:\n%s", want, script)
		}
	}
}
