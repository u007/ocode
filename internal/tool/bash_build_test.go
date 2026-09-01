package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/hooks"
)

// TestBuildBashCmdUnixShape locks the Unix shape of the unified builder:
// `bash -c <command>` with a non-nil SysProcAttr (process group).
func TestBuildBashCmdUnixShape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shape only")
	}
	cmd := buildBashCmd(nil, "echo hi", "")
	if len(cmd.Args) != 3 || cmd.Args[0] != "bash" || cmd.Args[1] != "-c" || cmd.Args[2] != "echo hi" {
		t.Fatalf("Args = %v, want [bash -c echo hi]", cmd.Args)
	}
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr nil, want process-group setup")
	}
}

// TestBuildBashCmdSetsDir locks the session-workdir wiring: a non-empty dir
// lands in cmd.Dir; an empty one leaves it untouched (inherit process cwd).
func TestBuildBashCmdSetsDir(t *testing.T) {
	cmd := buildBashCmd(nil, "pwd", "/tmp/session-root")
	if cmd.Dir != "/tmp/session-root" {
		t.Fatalf("Dir = %q, want /tmp/session-root", cmd.Dir)
	}
	cmd = buildBashCmd(nil, "pwd", "")
	if cmd.Dir != "" {
		t.Fatalf("Dir = %q, want empty (inherit cwd)", cmd.Dir)
	}
}

// TestBuildBashCmdNilCtxMatchesPlainCommand verifies the nil-ctx path (the
// background launch) uses exec.Command semantics — the child must not be tied
// to a caller context.
func TestBuildBashCmdNilCtxMatchesPlainCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix semantics check")
	}
	cmd := buildBashCmd(nil, "true", "")
	ctxCmd := buildBashCmd(context.Background(), "true", "")
	if cmd.Path == "" || cmd.Path != ctxCmd.Path {
		t.Fatalf("plain cmd path %q != background ctx cmd path %q", cmd.Path, ctxCmd.Path)
	}
}

// TestBashUsesSessionWorkdirNotProcessCwd is the load-bearing session-workdir
// test: with the session project root differing from os.Getwd(), a foreground
// `pwd` resolves to the session root, and the background process hook receives
// the session root as its cwd — not the process cwd.
func TestBashUsesSessionWorkdirNotProcessCwd(t *testing.T) {
	sessionRoot := t.TempDir()
	ctx := WithWorkDir(context.Background(), sessionRoot)

	// Foreground path: pwd must print the session root.
	bt := BashTool{}
	res, err := bt.ExecuteStreamCtx(ctx, jsonRaw(`{"command":"pwd"}`), nil)
	if err != nil {
		t.Fatalf("foreground pwd failed: %v", err)
	}
	out := strings.TrimSpace(res)
	outResolved, err := filepath.EvalSymlinks(out)
	if err != nil {
		outResolved = filepath.Clean(out)
	}
	rootResolved, err := filepath.EvalSymlinks(sessionRoot)
	if err != nil {
		rootResolved = filepath.Clean(sessionRoot)
	}
	if outResolved != rootResolved {
		t.Fatalf("foreground pwd printed %q (resolved %q), want session root %q (resolved %q)", out, outResolved, sessionRoot, rootResolved)
	}

	// Background path: the RunShellEnv hook must see the session root as cwd.
	var hookCwd string
	ph := hooks.New()
	ph.RegisterShellEnv(func(cwd string) map[string]string {
		hookCwd = cwd
		return map[string]string{"SHELL_CWD": cwd}
	})
	SetHookPipeline(ph)
	t.Cleanup(func() { SetHookPipeline(nil) })

	reg := NewProcessRegistry()
	bt = BashTool{Procs: reg}
	if _, err := bt.ExecuteStreamCtx(ctx, jsonRaw(`{"command":"echo hi","run_in_background":true}`), nil); err != nil {
		t.Fatalf("background launch failed: %v", err)
	}
	reg.KillAll()
	gotResolved, err := filepath.EvalSymlinks(hookCwd)
	if err != nil {
		gotResolved = filepath.Clean(hookCwd)
	}
	if gotResolved != rootResolved {
		t.Fatalf("background hook cwd = %q (resolved %q), want session root (resolved %q)", hookCwd, gotResolved, rootResolved)
	}
	if _, err := os.Getwd(); err != nil {
		t.Fatal(err)
	}
}

// jsonRaw builds tool arguments inline.
func jsonRaw(s string) json.RawMessage { return json.RawMessage(s) }