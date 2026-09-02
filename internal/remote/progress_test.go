package remote

import (
	"errors"
	"strings"
	"testing"
)

func TestProgressPlainNonTTY(t *testing.T) {
	var buf strings.Builder
	p := NewProgress(&buf, "Connecting to devbox…")

	p.Start("reachable", "ssh reachable")
	p.Done("")

	p.Start("platform", "platform detect")
	p.Done("linux/arm64")

	p.Start("multiplex", "checking for tmux/screen")
	p.Warn("tmux/screen not found on remote — this session will not survive a dropped connection")

	p.Start("launch", "launching remote TUI")
	p.Fail(errors.New("ssh: connection reset"), "check your network")

	out := buf.String()
	for _, want := range []string{
		"Connecting to devbox",
		"ssh reachable",
		"linux/arm64",
		"tmux/screen not found",
		"ssh: connection reset",
		"check your network",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("progress output missing %q, got:\n%s", want, out)
		}
	}
}

func TestProgressNeverSwallowsFailureDetail(t *testing.T) {
	var buf strings.Builder
	p := NewProgress(&buf, "")
	p.Start("build", "building")
	underlying := "go: cannot find package \"foo\" in any of:\n\t/usr/lib/go/src/foo"
	p.Fail(errors.New(underlying), "")

	if !strings.Contains(buf.String(), underlying) {
		t.Errorf("expected verbatim underlying error in output, got:\n%s", buf.String())
	}
}
