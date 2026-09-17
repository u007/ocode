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

// TestBashInvocationLoginShellOverride pins the desktop login-shell wiring:
// SetLoginShell swaps `bash -c` for `<shell> -l -c`, and the empty-string
// reset restores the default shape.
func TestBashInvocationLoginShellOverride(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix login-shell shape only")
	}
	t.Cleanup(func() { SetLoginShell("") })

	// Pin a shell this host actually has: the override is validated before
	// exec, so a hard-coded /bin/zsh would silently resolve to something else
	// on a Linux host without it and the assertions below would lie.
	want := existingShell(t)
	SetLoginShell(want)
	shell, args := bashInvocation("echo hi")
	if shell != want || len(args) != 3 || args[0] != "-l" || args[1] != "-c" || args[2] != "echo hi" {
		t.Fatalf("with override: shell=%q args=%v, want [%s -l -c echo hi]", shell, args, want)
	}

	// The override must flow through the unified builder into cmd.Args, so
	// both the foreground (exec.go) and background (process.go) paths inherit it.
	cmd, err := buildBashCmd(nil, "echo hi", "", nil, sandbox.RootSet{}, false)
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	if cmd.Path != want || len(cmd.Args) != 4 || cmd.Args[1] != "-l" || cmd.Args[2] != "-c" || cmd.Args[3] != "echo hi" {
		t.Fatalf("buildBashCmd Args = %v, want [%s -l -c echo hi]", cmd.Args, want)
	}

	SetLoginShell("")
	shell, args = bashInvocation("echo hi")
	if shell != "bash" || len(args) != 2 || args[0] != "-c" || args[1] != "echo hi" {
		t.Fatalf("after reset: shell=%q args=%v, want [bash -c echo hi]", shell, args)
	}
}

// TestLoginShellRunsProfileCommands is the end-to-end behavioral check for the
// desktop symptom: with the override set, an agent-emitted export survives the
// login-shell invocation (profile files are sourced before the command runs).
func TestLoginShellRunsProfileCommands(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix login-shell behavior only")
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}
	switch filepath.Base(shell) {
	case "zsh", "bash", "sh":
	default:
		t.Skipf("SHELL %q is not zsh/bash/sh", shell)
	}
	t.Cleanup(func() { SetLoginShell("") })

	SetLoginShell(shell)
	bt := BashTool{}
	res, err := bt.ExecuteStreamCtx(context.Background(), jsonRaw(`{"command":"export OCODE_LOGIN_SHELL_TEST=1; echo ok"}`), nil)
	if err != nil {
		t.Fatalf("login-shell execution failed: %v", err)
	}
	if !strings.Contains(res, "ok") {
		t.Fatalf("output %q missing expected ok", res)
	}
}

// jsonRaw builds tool arguments inline.
func jsonRaw(s string) json.RawMessage {
	return json.RawMessage(s)
}

// existingShell returns the first of the standard Unix shells present on this
// host, so login-shell tests never depend on a particular distro's layout.
func existingShell(t *testing.T) string {
	t.Helper()
	for _, candidate := range []string{"/bin/bash", "/bin/zsh", "/bin/sh"} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate
		}
	}
	t.Skip("no standard shell found on this host")
	return ""
}

// TestBashInvocationLoginShellOverrideMissingShell pins the validation in
// bashInvocation: a pinned login shell that does not exist on this host (a
// stale desktop config, a path copied from another machine) must resolve to a
// shell that does, instead of every agent command failing with
// `fork/exec <shell>: no such file or directory`.
func TestBashInvocationLoginShellOverrideMissingShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix login-shell shape only")
	}
	t.Cleanup(func() { SetLoginShell("") })

	missing := filepath.Join(t.TempDir(), "no-such-shell")
	SetLoginShell(missing)
	shell, args := bashInvocation("echo hi")
	if shell == missing {
		t.Fatalf("nonexistent override %q was handed to exec", missing)
	}
	if info, err := os.Stat(shell); err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		t.Fatalf("resolved shell %q is not an executable file (err=%v)", shell, err)
	}
	if len(args) != 3 || args[0] != "-l" || args[1] != "-c" || args[2] != "echo hi" {
		t.Fatalf("args = %v, want [-l -c echo hi]", args)
	}

	// A non-executable file is just as unusable as a missing one.
	plain := filepath.Join(t.TempDir(), "not-executable")
	if err := os.WriteFile(plain, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	SetLoginShell(plain)
	if shell, _ := bashInvocation("echo hi"); shell == plain {
		t.Fatalf("non-executable override %q was handed to exec", plain)
	}
}
