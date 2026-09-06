package remotecli

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseArgs(t *testing.T) {
	target, path, noSync, web, err := parseArgs([]string{"user@host", "/proj", "--no-sync"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.String() != "user@host" || path != "/proj" || !noSync || web {
		t.Errorf("got target=%q path=%q noSync=%v web=%v", target.String(), path, noSync, web)
	}

	target, path, noSync, web, err = parseArgs([]string{"host"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.String() != "host" || path != "" || noSync || web {
		t.Errorf("got target=%q path=%q noSync=%v web=%v", target.String(), path, noSync, web)
	}
}

func TestParseArgsErrors(t *testing.T) {
	cases := [][]string{
		{},
		{"--no-sync"}, // no target
		{"host", "path", "extra"},
		{"--unknown-flag", "host"},
	}
	for _, args := range cases {
		if _, _, _, _, err := parseArgs(args); err == nil {
			t.Errorf("parseArgs(%v): expected error", args)
		}
	}
}

func TestParseArgsWebFlag(t *testing.T) {
	target, path, noSync, web, err := parseArgs([]string{"--web", "host", "/proj"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !web {
		t.Error("expected web=true")
	}
	if target.Host != "host" || path != "/proj" || noSync {
		t.Errorf("got target=%+v path=%q noSync=%v", target, path, noSync)
	}
}

func TestParseArgsWebFlagAnyPosition(t *testing.T) {
	_, _, _, web, err := parseArgs([]string{"host", "--web"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !web {
		t.Error("expected web=true regardless of flag position")
	}
}

func TestRunRejectsExplicitPathWithWebFlag(t *testing.T) {
	// The check fires before any network/SSH activity (right after
	// parseArgs, before path defaulting or remote.ConnectWeb), so this never
	// touches the network.
	err := Run([]string{"--web", "host", "/proj"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "does not yet support selecting a specific project path") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunAllowsWebFlagWithoutExplicitPath(t *testing.T) {
	// nosuchhost.invalid (RFC 2606 reserved TLD, guaranteed never to
	// resolve) makes Run fail fast on the "reachable" prepare stage instead
	// of reaching a real host — this test only needs to confirm the new
	// path guard does not fire when no path was given; a real connection
	// failure past that point is expected and fine.
	err := Run([]string{"--web", "nosuchhost.invalid"})
	if err == nil {
		t.Fatal("expected an error (no such remote host), just not the path-rejection one")
	}
	if strings.Contains(err.Error(), "does not yet support selecting a specific project path") {
		t.Errorf("--web without an explicit path must not be rejected by the path guard: %v", err)
	}
}

func TestRunReceiveConfigRejectsMalformedFrame(t *testing.T) {
	var out bytes.Buffer
	err := runReceiveConfig(strings.NewReader("not json"), &out)
	if err == nil {
		t.Fatal("expected error for malformed frame")
	}
	if !strings.HasPrefix(out.String(), "ERROR") {
		t.Errorf("expected an ERROR line, got: %q", out.String())
	}
}

func TestRunReceiveConfigRejectsEmptyStdin(t *testing.T) {
	var out bytes.Buffer
	err := runReceiveConfig(strings.NewReader(""), &out)
	if err == nil {
		t.Fatal("expected error for empty stdin")
	}
	if !strings.HasPrefix(out.String(), "ERROR") {
		t.Errorf("expected an ERROR line, got: %q", out.String())
	}
}
