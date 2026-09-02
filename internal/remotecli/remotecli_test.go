package remotecli

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseArgs(t *testing.T) {
	target, path, noSync, err := parseArgs([]string{"user@host", "/proj", "--no-sync"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.String() != "user@host" || path != "/proj" || !noSync {
		t.Errorf("got target=%q path=%q noSync=%v", target.String(), path, noSync)
	}

	target, path, noSync, err = parseArgs([]string{"host"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.String() != "host" || path != "" || noSync {
		t.Errorf("got target=%q path=%q noSync=%v", target.String(), path, noSync)
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
		if _, _, _, err := parseArgs(args); err == nil {
			t.Errorf("parseArgs(%v): expected error", args)
		}
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
