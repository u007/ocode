//go:build windows

package tool

import (
	"testing"

	"github.com/u007/ocode/internal/shell/sandbox"
)

// TestBuildBashCmdWindowsShape locks the Windows shape of the unified builder:
// `cmd /C <command>`, process group intentionally not set.
func TestBuildBashCmdWindowsShape(t *testing.T) {
	cmd, err := buildBashCmd(nil, "echo hi", "", nil, sandbox.RootSet{}, false)
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	if len(cmd.Args) != 3 || cmd.Args[0] != "cmd" || cmd.Args[1] != "/C" || cmd.Args[2] != "echo hi" {
		t.Fatalf("Args = %v, want [cmd /C echo hi]", cmd.Args)
	}
	cmd, err = buildBashCmd(nil, "echo hi", `C:\session\root`, nil, sandbox.RootSet{}, false)
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	if cmd.Dir != `C:\session\root` {
		t.Fatalf("Dir = %q, want C:\\session\\root", cmd.Dir)
	}
}
