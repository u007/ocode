package tui

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/u007/ocode/internal/browse/cdp"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/server"
	"github.com/u007/ocode/internal/tool"
)

// htrDebugPrefix is the message prefix every shared-HTR debug line carries.
// Tests match on it so an unrelated entry appended by another goroutine in this
// package (the log is process-global) cannot be mistaken for the ensure's.
const htrDebugPrefix = "htr: shared daemon"

// errHTRSentinelForTest is a stand-in ensure failure. It is a distinct value
// (not a literal string) so the log assertion proves the error itself reached
// the log rather than any text that happens to look like it.
var errHTRSentinelForTest = errors.New("htrcli daemon probe exploded")

// isolateHTROwnState points HOME and every data-dir override at a fresh temp
// directory.
//
// HOME is not optional here: paths.OcodeGlobalDataDir is HOME-derived on darwin,
// so a test that set only XDG_DATA_HOME would read and write the developer's
// real ~/.local/share/opencode and ~/.htrcli. LOCALAPPDATA is included so the
// same test is hermetic on Windows.
func isolateHTROwnState(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "localappdata"))
	// htrcli's own port override would silently redirect the health probe away
	// from the resolved coordinates.
	t.Setenv("HTR_PORT", "")
	t.Setenv("HTRCLI_PATH", "")
	t.Setenv("OCODE_HTRCLI_PATH", "")
}

// stubServerEnsure replaces the server call the real ensure makes, for the
// duration of the test. It returns the stub so a test can assert the seam was
// consulted — the restore is registered as a cleanup, so an override can never
// leak into the next test even if this one fails.
func stubServerEnsure(t *testing.T, fn func(*tool.ProcessSupervisor, config.BrowserConfig, *log.Logger) (cdp.HTRStatus, error)) {
	t.Helper()
	prev := ensureSharedHTRDaemonFnDefault
	ensureSharedHTRDaemonFnDefault = fn
	t.Cleanup(func() { ensureSharedHTRDaemonFnDefault = prev })
}

// waitForSharedHTREmbedry blocks until the ensure has appended its one debug
// line and returns it.
//
// Waiting on the log rather than on the stub is deliberate: the log append is
// the last thing the ensure does, so observing it means the ensure finished.
// That keeps a late append from one test out of the next test's window, and it
// means a test which asserts on log content never races the goroutine writing it.
func waitForSharedHTREmbedry(t *testing.T, since uint64) DebugEntry {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		entries, cursor := DebugLog.SnapshotSince(since)
		for _, e := range entries {
			if strings.HasPrefix(e.Message, htrDebugPrefix) {
				return e
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no %q debug entry appeared within 10s; the real ensure never reported an outcome", htrDebugPrefix)
		}
		since = cursor
		time.Sleep(5 * time.Millisecond)
	}
}

// TestEnsureSharedHTRDaemonAsyncInvokesTheEnsureOffThread pins the two halves of
// "fire-and-forget": the work is started at all, and the caller returns without
// waiting for it.
//
// The seam blocks until the test releases it, so the second assertion can only
// pass if the ensure ran on another goroutine. Mutant: calling ensure()
// directly instead of through crashguard.Go.
func TestEnsureSharedHTRDaemonAsyncInvokesTheEnsureOffThread(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	m := &model{ensureSharedHTRDaemonFn: func() {
		close(entered)
		<-release
	}}
	t.Cleanup(func() { close(release) })

	returned := make(chan struct{})
	go func() {
		m.ensureSharedHTRDaemonAsync()
		close(returned)
	}()

	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the ensure was never invoked; the TUI startup hook must start the work")
	}

	select {
	case <-returned:
	case <-time.After(10 * time.Second):
		t.Fatal("ensureSharedHTRDaemonAsync blocked until the ensure finished; startup must never wait on a daemon")
	}
}

// TestEnsureSharedHTRDaemonAsyncRunsAtMostOncePerModel pins the guard that stops
// repeated startups in one process from re-entering the ensure.
//
// Mutants: dropping `if m.htrEnsured { return }`, and dropping the
// `m.htrEnsured = true` assignment. The first is killed by the re-entry check;
// the second by the synchronous flag assertion, which needs no timing at all.
func TestEnsureSharedHTRDaemonAsyncRunsAtMostOncePerModel(t *testing.T) {
	var calls atomic.Int32
	entered := make(chan struct{}, 4)
	m := &model{ensureSharedHTRDaemonFn: func() {
		calls.Add(1)
		entered <- struct{}{}
	}}

	m.ensureSharedHTRDaemonAsync()
	// The guard is set on the caller's goroutine before anything is spawned, so
	// it is readable the instant the call returns — no waiting, no flakiness.
	if !m.htrEnsured {
		t.Fatal("htrEnsured must be set by the time ensureSharedHTRDaemonAsync returns")
	}
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the first ensure never ran")
	}

	m.ensureSharedHTRDaemonAsync()
	m.ensureSharedHTRDaemonAsync()

	// A re-entrant ensure would land here. The bound is generous on purpose: this
	// is the only timing assumption in the file, and being too generous can only
	// hide a regression, never manufacture a failure.
	select {
	case <-entered:
		t.Fatalf("ensure was re-entered (%d invocations); the one-shot guard did not hold", calls.Load())
	case <-time.After(2 * time.Second):
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("ensure ran %d times, want exactly 1", n)
	}
}

// TestEnsureSharedHTRDaemonPassesTheModelsSupervisorToTheServer pins that the
// real ensure hands the model's own supervisor to the server package. A nil or
// freshly-built supervisor would make cdp.EnsureHTRServe adopt but never spawn,
// which is exactly the bug this task exists to fix.
//
// Mutants: reading a different supervisor, and never consulting the server seam
// at all (the override would never be observed).
func TestEnsureSharedHTRDaemonPassesTheModelsSupervisorToTheServer(t *testing.T) {
	isolateHTROwnState(t)
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{GracePeriod: time.Millisecond})
	t.Cleanup(func() {
		// t.Context() is already cancelled by the time cleanups run, so the
		// supervisor would never see its grace period.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = sup.Shutdown(ctx)
	})

	seen := make(chan *tool.ProcessSupervisor, 1)
	stubServerEnsure(t, func(got *tool.ProcessSupervisor, _ config.BrowserConfig, _ *log.Logger) (cdp.HTRStatus, error) {
		seen <- got
		return cdp.HTRStatus{}, server.ErrHTRDisabled
	})

	m := &model{supervisor: sup}
	cursor := DebugLog.Cursor()
	m.ensureSharedHTRDaemonAsync()

	select {
	case got := <-seen:
		if got != sup {
			t.Fatalf("server ensure received supervisor %p, want the model's own %p", got, sup)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the real ensure never reached the server seam")
	}
	waitForSharedHTREmbedry(t, cursor)
}

// TestEnsureSharedHTRDaemonLogsTheReusedDaemonStatus pins what a successful
// ensure reports. StartedByOcode is the field the whole provenance rule rests
// on, so a line that omitted it would make the daemon's ownership unreadable
// from the log.
//
// Mutants: dropping the success line, and logging only the address.
func TestEnsureSharedHTRDaemonLogsTheReusedDaemonStatus(t *testing.T) {
	isolateHTROwnState(t)
	stubServerEnsure(t, func(*tool.ProcessSupervisor, config.BrowserConfig, *log.Logger) (cdp.HTRStatus, error) {
		return cdp.HTRStatus{Addr: "127.0.0.1:45999", Running: true, StartedByOcode: false}, nil
	})

	cursor := DebugLog.Cursor()
	(&model{}).ensureSharedHTRDaemonAsync()
	entry := waitForSharedHTREmbedry(t, cursor)

	if entry.Kind != DebugKindSession {
		t.Errorf("kind = %q, want %q: a healthy daemon is a normal startup fact, not an error", entry.Kind, DebugKindSession)
	}
	for _, want := range []string{"127.0.0.1:45999", "running=true", "startedByOcode=false"} {
		if !strings.Contains(entry.Message, want) {
			t.Errorf("message %q does not report %q; the log is the only record of the daemon's provenance", entry.Message, want)
		}
	}
}

// TestEnsureSharedHTRDaemonLogsAFailureAsAnError pins that a genuine failure is
// recorded at ERROR severity with the cause, and never as a success.
//
// Mutants: swallowing the error silently, and reporting a failure under the
// informational kind.
func TestEnsureSharedHTRDaemonLogsAFailureAsAnError(t *testing.T) {
	isolateHTROwnState(t)
	stubServerEnsure(t, func(*tool.ProcessSupervisor, config.BrowserConfig, *log.Logger) (cdp.HTRStatus, error) {
		return cdp.HTRStatus{}, errHTRSentinelForTest
	})

	cursor := DebugLog.Cursor()
	(&model{}).ensureSharedHTRDaemonAsync()
	entry := waitForSharedHTREmbedry(t, cursor)

	if entry.Kind != DebugKindError {
		t.Errorf("kind = %q, want %q: a failure that was tried for must be visible as one", entry.Kind, DebugKindError)
	}
	if !strings.Contains(entry.Message, errHTRSentinelForTest.Error()) {
		t.Errorf("message %q does not carry the cause %q; the log is the only place it appears", entry.Message, errHTRSentinelForTest)
	}
}

// TestEnsureSharedHTRDaemonLogsDisabledAsInformational pins the one outcome that
// must not look like a failure: HTR switched off in config is an expected
// setting, and an ERROR entry would land on the log tab of every launch of every
// such session.
//
// Mutant: dropping the errors.Is branch, so the disabled case is logged as an
// error like any other failure.
func TestEnsureSharedHTRDaemonLogsDisabledAsInformational(t *testing.T) {
	isolateHTROwnState(t)
	stubServerEnsure(t, func(*tool.ProcessSupervisor, config.BrowserConfig, *log.Logger) (cdp.HTRStatus, error) {
		return cdp.HTRStatus{}, server.ErrHTRDisabled
	})

	cursor := DebugLog.Cursor()
	(&model{}).ensureSharedHTRDaemonAsync()
	entry := waitForSharedHTREmbedry(t, cursor)

	if entry.Kind != DebugKindSession {
		t.Errorf("kind = %q, want %q: HTR being switched off is configuration, not a failure", entry.Kind, DebugKindSession)
	}
	if !strings.Contains(entry.Message, "disabled") {
		t.Errorf("message %q does not say why nothing was started", entry.Message)
	}
}

// TestEnsureSharedHTRDaemonAsyncSpawnsThroughCrashguard pins the repo rule that
// every goroutine in internal/tui goes through crashguard.
//
// It is checked on the syntax tree rather than at runtime because there is no
// runtime difference to observe: crashguard.Recover re-panics after running the
// terminal-reset hook, so a guarded panic and an unguarded one both end the
// process. What differs is only which one runs the hook, and the only way to see
// that is to read the call site.
//
// Mutants: `go ensure()` in place of crashguard.Go, and removing the spawn.
func TestEnsureSharedHTRDaemonAsyncSpawnsThroughCrashguard(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "model.go", nil, 0)
	if err != nil {
		t.Fatalf("parse model.go: %v", err)
	}

	var target *ast.FuncDecl
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Name.Name != "ensureSharedHTRDaemonAsync" {
			continue
		}
		if target != nil {
			t.Fatal("model.go declares ensureSharedHTRDaemonAsync more than once")
		}
		target = fn
	}
	if target == nil {
		t.Fatal("model.go no longer declares (*model).ensureSharedHTRDaemonAsync")
	}

	guarded, bare := 0, 0
	ast.Inspect(target.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.GoStmt:
			bare++
		case *ast.CallExpr:
			sel, ok := node.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if ok && pkg.Name == "crashguard" && sel.Sel.Name == "Go" {
				guarded++
			}
		}
		return true
	})

	if bare > 0 {
		t.Errorf("ensureSharedHTRDaemonAsync contains %d bare `go` statement(s); every goroutine in internal/tui must go through crashguard.Go so a panic cannot leave the alt-screen enabled", bare)
	}
	if guarded != 1 {
		t.Errorf("found %d crashguard.Go call(s) in ensureSharedHTRDaemonAsync, want exactly 1", guarded)
	}
}
