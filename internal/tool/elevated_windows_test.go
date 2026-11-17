//go:build windows

package tool

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// With no interactive desktop, elevation must fail closed with a clear
// "unsupported on this host" error and never attempt to run the command.
func TestElevatedWindowsNoDesktopFailsClosed(t *testing.T) {
	t.Setenv("SESSIONNAME", "")
	cmd := exec.Command("cmd", "/C", "echo SHOULD_NOT_RUN")
	out, err := executeElevated(context.Background(), cmd, nil)
	if err == nil {
		t.Fatalf("expected unsupported error; out=%q", out)
	}
	if !strings.Contains(err.Error(), "not supported on this host") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPSQuote(t *testing.T) {
	if got := psQuote("a'b"); got != "'a''b'" {
		t.Fatalf("psQuote: got %q", got)
	}
}
