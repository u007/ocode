//go:build windows

package tool

import (
	"context"
	"encoding/json"
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

// The elevation flag must route Windows calls to executeElevated (native UAC
// elevation) even though no password is collected (NeedsPassword=false). With
// no interactive desktop the native branch yields the clear unsupported error;
// without the flag the call would silently run on the ordinary path instead.
func TestElevationFlagRoutesWindowsToNativeElevation(t *testing.T) {
	t.Setenv("SESSIONNAME", "")
	ctx := WithElevationFlag(WithElevationSecret(context.Background(), func() []byte { return nil }), true)
	args, _ := json.Marshal(map[string]string{"command": "whoami"})
	res, _ := (BashTool{}).ExecuteStreamCtx(ctx, args, nil)
	if !strings.Contains(res, "not supported on this host") {
		t.Fatalf("flag did not route to the native-elevation branch: %q", res)
	}
}
