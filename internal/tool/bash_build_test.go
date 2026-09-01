package tool

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/hooks"
	"github.com/u007/ocode/internal/shell/sandbox"
)

// TestBuildBashCmdUnixShape locks the Unix shape of the unified builder:
// `bash -c <command>` with a non-nil SysProcAttr (process group).
func TestBuildBashCmdUnixShape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shape only")
	}
	cmd, err := buildBashCmd(nil, "echo hi", "", nil, sandbox.RootSet{}, false)
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
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
	cmd, err := buildBashCmd(nil, "pwd", "/tmp/session-root", nil, sandbox.RootSet{}, false)
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	if cmd.Dir != "/tmp/session-root" {
		t.Fatalf("Dir = %q, want /tmp/session-root", cmd.Dir)
	}
	cmd, err = buildBashCmd(nil, "pwd", "", nil, sandbox.RootSet{}, false)
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
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
	cmd, err := buildBashCmd(nil, "true", "", nil, sandbox.RootSet{}, false)
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	ctxCmd, err := buildBashCmd(context.Background(), "true", "", nil, sandbox.RootSet{}, false)
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
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

// TestBuildBashCmdSkipsWhenInactive locks the inactive branch: plain cmd,
// nil error, wrapper never consulted.
func TestBuildBashCmdSkipsWhenInactive(t *testing.T) {
	w := &stubSandboxWrapper{available: false} // must not be consulted
	cmd, err := buildBashCmd(nil, "echo hi", "", w, sandbox.RootSet{WritableRoots: []string{"/tmp"}}, false)
	if err != nil {
		t.Fatalf("inactive build error: %v", err)
	}
	if len(cmd.Args) != 3 || cmd.Args[0] != "bash" {
		t.Fatalf("inactive cmd Args = %v, want plain bash -c", cmd.Args)
	}
	if w.wrapCalls != 0 {
		t.Fatalf("wrapper consulted %d times while inactive, want 0", w.wrapCalls)
	}
}

// TestBuildBashCmdWrapsWhenActive locks the active branch: available wrapper
// rewrites the cmd and receives the RootSet.
func TestBuildBashCmdWrapsWhenActive(t *testing.T) {
	wrapped := exec.Command("wrapped", "echo hi")
	w := &stubSandboxWrapper{available: true, wrapped: wrapped}
	roots := sandbox.RootSet{WritableRoots: []string{"/Users/test/project", "/tmp"}}
	cmd, err := buildBashCmd(nil, "echo hi", "", w, roots, true)
	if err != nil {
		t.Fatalf("active build error: %v", err)
	}
	if cmd != wrapped {
		t.Fatal("Wrap result not returned, want wrapped cmd")
	}
	if w.wrapCalls != 1 {
		t.Fatalf("wrapper consulted %d times, want 1", w.wrapCalls)
	}
	if len(w.gotRoots.WritableRoots) != 2 {
		t.Fatalf("wrapper received roots %v, want both writable roots", w.gotRoots.WritableRoots)
	}
}

// TestBuildBashCmdFailsClosedForeground locks fail-closed on the foreground
// path: active + unavailable backend ⇒ error and NO cmd (the caller must not
// run unconfined).
func TestBuildBashCmdFailsClosedForeground(t *testing.T) {
	w := &stubSandboxWrapper{available: false}
	cmd, err := buildBashCmd(nil, "echo hi", "", w, sandbox.RootSet{}, true)
	if err == nil {
		t.Fatal("active + unavailable backend must error (fail-closed)")
	}
	if cmd != nil {
		t.Fatalf("failed wrap returned a cmd %v, want nil", cmd.Args)
	}
}

// TestBuildBashCmdFailsClosedBackgroundNoRecord locks fail-closed on the
// background path through the registry: a wrap failure must not leave any
// ProcessRegistry record behind.
func TestBuildBashCmdFailsClosedBackgroundNoRecord(t *testing.T) {
	reg := NewProcessRegistry()
	w := &stubSandboxWrapper{available: false}
	p, err := reg.StartBackgroundSandbox("echo hi", "echo hi", "", w, sandbox.RootSet{}, true)
	if err == nil {
		t.Fatal("background wrap failure must error")
	}
	if p != nil {
		t.Fatalf("wrap failure returned a process record %s, want nil", p.ID)
	}
	if n := reg.Counter(); n != 0 {
		t.Fatalf("registry has %d records after failed wrap, want 0", n)
	}
}

// stubSandboxWrapper is a controllable Wrapper for fail-closed tests.
type stubSandboxWrapper struct {
	available      bool
	wrapped        *exec.Cmd
	wrapErr        error
	wrapCalls      int
	gotRoots       sandbox.RootSet
	availableCalls int
}

func (s *stubSandboxWrapper) Wrap(cmd *exec.Cmd, roots sandbox.RootSet) (*exec.Cmd, error) {
	s.wrapCalls++
	s.gotRoots = roots
	if s.wrapped != nil {
		return s.wrapped, s.wrapErr
	}
	return cmd, s.wrapErr
}

func (s *stubSandboxWrapper) Available() bool {
	s.availableCalls++
	return s.available
}

// jsonRaw builds tool arguments inline.
func jsonRaw(s string) json.RawMessage { return json.RawMessage(s) }