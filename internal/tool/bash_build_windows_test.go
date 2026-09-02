//go:build windows

package tool

import "testing"

// TestBuildBashCmdWindowsShape locks the Windows shape of the unified builder:
// `cmd /C <command>`, process group intentionally not set.
func TestBuildBashCmdWindowsShape(t *testing.T) {
	cmd := buildBashCmd(nil, "echo hi", "")
	if len(cmd.Args) != 3 || cmd.Args[0] != "cmd" || cmd.Args[1] != "/C" || cmd.Args[2] != "echo hi" {
		t.Fatalf("Args = %v, want [cmd /C echo hi]", cmd.Args)
	}
	cmd = buildBashCmd(nil, "echo hi", `C:\session\root`)
	if cmd.Dir != `C:\session\root` {
		t.Fatalf("Dir = %q, want C:\\session\\root", cmd.Dir)
	}
}