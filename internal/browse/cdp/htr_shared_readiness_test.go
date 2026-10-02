package cdp

// Readiness verification for the shared daemon, and the death policy.
//
// The contract this file pins:
//
//   - EnsureHTRServe must NOT block boot. It confirms only that the child it
//     just spawned is still alive (well under a second) and returns
//     Running/Owned; health is verified in the background.
//   - Verification is bounded. It stops at sharedVerifyBudget, complains once,
//     and leaves the daemon running: a slow-starting htrcli is not a dead one.
//   - When the daemon dies, ocode says so loudly and stops. No auto-restart and
//     no fallback to a private per-session daemon: either would recreate the
//     two-daemon split this whole change exists to remove, and the silent
//     fallback would hide the disconnect the user has to notice.

import (
	"bytes"
	"context"
	"encoding/base64"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// capturingLogger returns a logger writing into the returned buffer, so a test
// can assert on what was logged — including that nothing was.
func capturingLogger() (*log.Logger, *bytes.Buffer) {
	logs := &bytes.Buffer{}
	return log.New(logs, "", 0), logs
}

// captureHTRStatus installs a status sink and returns the slice it publishes
// into. It is how a test observes what the browser UI would be told, which is
// the only channel a post-EnsureHTRServe failure has.
//
// It also isolates lastHTRVerifyOutcome, the "have I already said this" record.
// That is process state on purpose — a second ensure in production must not
// re-warn — but it means a test that exercises the first-report path would be
// silenced by an earlier run of itself under -count=N. The previous value is
// restored on cleanup so tests still compose in file order.
//
// No mutex guards the returned slice: every test that reads it drives the
// verifier SYNCHRONOUSLY, so the publishing call has returned before the first
// read. A test that goes through launchSharedVerifyFn must join that goroutine
// (joinSharedVerify) instead.
func captureHTRStatus(t *testing.T) *[]string {
	t.Helper()
	prevSink := htrStatusFunc.Load()
	prevOutcome := lastHTRVerifyOutcome.Load()
	seen := []string{}
	SetStatusFunc(func(notice string) { seen = append(seen, notice) })
	lastHTRVerifyOutcome.Store(nil)
	t.Cleanup(func() {
		htrStatusFunc.Store(prevSink)
		lastHTRVerifyOutcome.Store(prevOutcome)
	})
	return &seen
}

// stubHTRExtensionDir writes the minimum a native-host manifest needs: a
// manifest.json carrying a stable base64 key, so the extension ID is derived
// rather than randomised. Without it ensureNativeHostManifest refuses, and
// EnsureHTRServe fails before it ever reaches the readiness path.
func stubHTRExtensionDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	manifest := `{"name":"htr-test","version":"0.0.1","manifest_version":3,"key":"` + key + `"}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write extension manifest: %v", err)
	}
	return dir
}

// stubServeBinary writes an executable standing in for `htrcli serve`: it stays
// alive and answers nothing. That is the interesting shape for readiness (a
// live process that never becomes healthy) and it keeps the test hermetic — no
// real daemon, no bound port, nothing to clean up but one short-lived child.
func stubServeBinary(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stub daemon is a POSIX shell script")
	}
	bin := filepath.Join(t.TempDir(), "htrcli")
	// Bounded lifetime so a leaked child can never outlive the run even if
	// cleanup is skipped: `sleep` with no argument would wait forever.
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ntrap 'exit 0' TERM INT\nsleep 120\n"), 0o755); err != nil {
		t.Fatalf("write stub htrcli: %v", err)
	}
	return bin
}

// joinSharedVerify redirects the background verifier so the test can count the
// launches and wait for them. It returns the launch count.
//
// Waiting matters: without it the verifier outlives the test body and the NEXT
// test's probe stubs would be swapped underneath it, which is a data race under
// -race — and a flake, not a failure. Counting matters just as much: nothing
// else can see that verification happens at all, so deleting the launch would
// leave the daemon permanently unverified and every test here still green.
//
// Call it AFTER installing probe stubs: cleanups run LIFO, so the join happens
// while the stubs are still in place, and the probe must report healthy by then
// or the join waits out the whole budget.
func joinSharedVerify(t *testing.T) *atomic.Int64 {
	t.Helper()
	orig := launchSharedVerifyFn
	launches := &atomic.Int64{}
	var mu sync.Mutex
	var pending []chan struct{}
	launchSharedVerifyFn = func(fn func()) {
		launches.Add(1)
		done := make(chan struct{})
		go func() { defer close(done); fn() }()
		mu.Lock()
		pending = append(pending, done)
		mu.Unlock()
	}
	t.Cleanup(func() {
		launchSharedVerifyFn = orig
		mu.Lock()
		var wait []chan struct{}
		wait = append(wait, pending...)
		mu.Unlock()
		for _, done := range wait {
			select {
			case <-done:
			case <-time.After(sharedVerifyBudget + 5*time.Second):
				t.Errorf("the background verifier never finished; it would race later tests")
			}
		}
	})
	return launches
}

// shutdownSupervisor stops a test supervisor and kills whatever it supervises.
func shutdownSupervisor(t *testing.T, sup *tool.ProcessSupervisor) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = sup.Shutdown(ctx)
	})
}

// TestReadinessReturnsBeforeFullBudget is the anti-stall guard: desktop boot
// calls StartBrowse synchronously, so a blocking wait here is added directly to
// startup time.
//
// The elapsed bound alone is not enough evidence, so this also pins the two
// properties that make a fast return correct rather than merely quick: the
// daemon must still be RUNNING afterwards (the previous implementation killed a
// daemon that had merely not answered yet), and the owner marker must already be
// written, because it is what authorises StopHTRServe — which now runs before
// the daemon has been proven healthy.
func TestReadinessReturnsBeforeFullBudget(t *testing.T) {
	isolateHTROwnerState(t)
	t.Setenv("HTR_PORT", "")
	// The daemon answers from the first probe the background verifier makes, so
	// the verifier retires promptly and joinSharedVerify never waits it out.
	origOwn, origForeign := htrHealthyForInstanceFn, htrHealthyForeignFn
	var healthy bool
	var mu sync.Mutex
	htrHealthyForInstanceFn = func(port int, socket, identity string) bool {
		mu.Lock()
		defer mu.Unlock()
		return healthy
	}
	htrHealthyForeignFn = func(port int, socket, token string) bool { return false }
	t.Cleanup(func() {
		htrHealthyForInstanceFn, htrHealthyForeignFn = origOwn, origForeign
	})
	launches := joinSharedVerify(t)

	sup := newTestSupervisor(t)
	shutdownSupervisor(t, sup)

	started := time.Now()
	st, err := EnsureHTRServe(sup, HTROptions{
		Enabled:      true,
		Port:         3845,
		ExtensionDir: stubHTRExtensionDir(t),
		CliPath:      stubServeBinary(t),
		Shared:       SharedDaemon{Mode: "shared", Port: 3845, Socket: "/tmp/x.sock", Token: "tok"},
	}, discardLogger())
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("EnsureHTRServe returned %v; a live-but-slow daemon must not fail the ensure", err)
	}
	if !st.Running || !st.Owned || !st.StartedByOcode {
		t.Fatalf("status running=%v owned=%v startedByOcode=%v, want true/true/true",
			st.Running, st.Owned, st.StartedByOcode)
	}
	// The pre-change implementation polled readiness 50x100ms and so burned ~5s
	// before returning. 2s leaves generous headroom for a loaded machine while
	// still failing on that loop.
	if elapsed > 2*time.Second {
		t.Errorf("EnsureHTRServe blocked for %s; it must not stall desktop boot", elapsed)
	}
	owner, ownerErr := readHTROwner()
	if ownerErr != nil {
		t.Fatalf("the owner marker must be recorded before health is verified: %v", ownerErr)
	}
	if !pidAlive(owner.PID) {
		t.Errorf("daemon pid %d was killed even though it is alive; a slow start is not a failed start", owner.PID)
	}
	if got := launches.Load(); got != 1 {
		t.Errorf("launched %d background verifiers, want exactly 1; verification is what makes the fast return safe", got)
	}
	// Let the background verifier observe a healthy daemon and retire, so the
	// join in cleanup is immediate.
	mu.Lock()
	healthy = true
	mu.Unlock()
}

// TestBackgroundVerifyReportsOneFailureAfterBudget pins the bounded part. A
// daemon that never answers must produce exactly one complaint inside the
// budget, must name the port, and must tell the UI why — without waiting for a
// second ensure to say it again.
func TestBackgroundVerifyReportsOneFailureAfterBudget(t *testing.T) {
	withStubbedProbes(t, false, false)
	notices := captureHTRStatus(t)
	lg, logs := capturingLogger()

	if verifySharedDaemon(3845, "/tmp/x.sock", "tok", lg, 300*time.Millisecond, 25*time.Millisecond) {
		t.Fatal("a daemon that never answers must not report ready")
	}
	if got := strings.Count(logs.String(), "\n"); got != 1 {
		t.Errorf("logged %d line(s) after one exhaustion, want exactly 1:\n%s", got, logs.String())
	}
	if !strings.Contains(logs.String(), "3845") {
		t.Errorf("the warning must name the port: %s", logs.String())
	}
	if len(*notices) != 1 || !strings.Contains((*notices)[0], "3845") {
		t.Errorf("the UI must be told once, with the port, why the daemon is unusable; got %q", *notices)
	}

	// A repeat exhaustion of the same daemon stays silent: the operator has
	// already been told, and re-warning on every ensure buries the first one.
	verifySharedDaemon(3845, "/tmp/x.sock", "tok", lg, 150*time.Millisecond, 25*time.Millisecond)
	if got := strings.Count(logs.String(), "\n"); got != 1 {
		t.Errorf("logged %d line(s) after a repeat exhaustion, want still 1:\n%s", got, logs.String())
	}
	if len(*notices) != 1 {
		t.Errorf("published %d notice(s) after a repeat exhaustion, want still 1: %q", len(*notices), *notices)
	}
}

// TestBackgroundVerifyStopsAtFirstHealthyProbe pins the other half: a daemon
// that comes up late is a success, not a failure.
//
// The probe turns healthy on its SECOND call, which is also what pins the
// production budget: a verifier that polled fewer than twice would report a
// failure for a daemon that is actually serving.
func TestBackgroundVerifyStopsAtFirstHealthyProbe(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	origOwn, origForeign := htrHealthyForInstanceFn, htrHealthyForeignFn
	htrHealthyForInstanceFn = func(port int, socket, identity string) bool {
		mu.Lock()
		defer mu.Unlock()
		calls++
		return calls >= 2
	}
	htrHealthyForeignFn = func(port int, socket, token string) bool { return false }
	t.Cleanup(func() { htrHealthyForInstanceFn, htrHealthyForeignFn = origOwn, origForeign })

	notices := captureHTRStatus(t)
	lg, logs := capturingLogger()

	begin := time.Now()
	verifySharedDaemonAsync(3845, "/tmp/x.sock", "tok", lg)
	if elapsed := time.Since(begin); elapsed > 2*time.Second {
		t.Errorf("the verifier took %s to notice a healthy daemon", elapsed)
	}
	if logs.Len() != 0 {
		t.Errorf("a daemon that became healthy must log nothing, got:\n%s", logs.String())
	}
	if len(*notices) != 0 {
		t.Errorf("a healthy daemon must not publish a failure notice, got %q", *notices)
	}
}

// TestReadinessFailsLoudlyWhenDaemonDiesDuringExec covers the failure branch of
// the BLOCKING half. A binary that exists but dies on the spot is the one
// failure that must be reported while the caller is still on the boot path — and
// the daemon's own words are the diagnosis, so the captured output must survive
// into the error.
func TestReadinessFailsLoudlyWhenDaemonDiesDuringExec(t *testing.T) {
	isolateHTROwnerState(t)
	t.Setenv("HTR_PORT", "")
	withStubbedProbes(t, false, false)
	launches := joinSharedVerify(t)

	sup := newTestSupervisor(t)
	shutdownSupervisor(t, sup)

	if runtime.GOOS == "windows" {
		t.Skip("the stub daemon is a POSIX shell script")
	}
	bin := filepath.Join(t.TempDir(), "htrcli")
	script := "#!/bin/sh\necho 'htrcli: cannot bind 127.0.0.1:3845' >&2\nexit 1\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub htrcli: %v", err)
	}

	started := time.Now()
	st, err := EnsureHTRServe(sup, HTROptions{
		Enabled:      true,
		Port:         3845,
		ExtensionDir: stubHTRExtensionDir(t),
		CliPath:      bin,
		Shared:       SharedDaemon{Mode: "shared", Port: 3845, Socket: "/tmp/x.sock", Token: "tok"},
	}, discardLogger())
	elapsed := time.Since(started)

	if err == nil {
		t.Fatalf("a daemon that dies during exec must fail the ensure, got %+v", st)
	}
	if elapsed > 2*time.Second {
		t.Errorf("the immediate-death failure took %s to surface; it must not stall boot", elapsed)
	}
	if !strings.Contains(err.Error(), "cannot bind") {
		t.Errorf("error %q must quote what the daemon printed — that is the diagnosis", err)
	}
	if _, ownerErr := readHTROwner(); ownerErr == nil {
		t.Error("a daemon that died during exec must not leave an owner marker behind")
	}
	// Nothing to verify: the daemon is already known dead, and a verifier for it
	// would only spend the whole budget re-discovering that.
	if got := launches.Load(); got != 0 {
		t.Errorf("launched %d verifiers for a daemon that died during exec, want 0", got)
	}
}

func TestSharedVerifyBudgetIsBounded(t *testing.T) {
	if sharedVerifyBudget <= 0 || sharedVerifyBudget > 30*time.Second {
		t.Errorf("sharedVerifyBudget = %s, want a positive value under 30s", sharedVerifyBudget)
	}
	if sharedVerifyPoll <= 0 || sharedVerifyPoll > sharedVerifyBudget {
		t.Errorf("sharedVerifyPoll = %s, want a positive value no larger than the budget", sharedVerifyPoll)
	}
}

// TestWatchHTRExitReportsDeathWithoutRestarting is the death policy: mark the
// record dead, say once which daemon died, retract the owner marker, and clear
// the host's cached status so the UI stops looking healthy. No auto-restart and
// no fallback daemon — recovery is the next explicit ensure.
func TestWatchHTRExitReportsDeathWithoutRestarting(t *testing.T) {
	isolateHTROwnerState(t)
	withStubbedProbes(t, false, false)

	bin := stubServeBinary(t)
	sup := newTestSupervisor(t)
	shutdownSupervisor(t, sup)

	cmd := newSharedServeCmd(bin, "tok", 3845, "/tmp/x.sock", "com.ocode.htrcontrol")
	rec, err := tool.StartSupervised(sup, cmd, tool.ProcessRegistration{
		ID: htrServeID, Name: "htrcli serve", Kind: tool.ProcessKindHTR, RetainOnShutdown: true,
	})
	if err != nil {
		t.Fatalf("start stub daemon: %v", err)
	}
	// The marker is both what StopHTRServe consults and where watchHTRExit
	// learns the port; watchHTRExit must retract it so a dead daemon is never
	// mistaken for one ocode may still stop.
	if err := htrWriteOwner("tok", 3845, rec.PID, "/tmp/x.sock", bin, rec.StartedAt, os.Getpid()); err != nil {
		t.Fatalf("write owner marker: %v", err)
	}

	notices := captureHTRStatus(t)
	lg, logs := capturingLogger()
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill stub daemon: %v", err)
	}

	watchHTRExit(cmd, sup, rec, "tok", lg)

	if got := strings.Count(logs.String(), "\n"); got != 1 {
		t.Errorf("logged %d line(s) on death, want exactly 1:\n%s", got, logs.String())
	}
	report := logs.String()
	if !strings.Contains(report, "3845") {
		t.Errorf("the death report must name the port, got %q", report)
	}
	if !strings.Contains(report, strconv.Itoa(rec.PID)) {
		t.Errorf("the death report must name the pid, got %q", report)
	}
	if len(*notices) != 1 || (*notices)[0] != "" {
		t.Errorf("death must clear the host's cached status with one empty publish, got %q", *notices)
	}
	dead, ok := sup.Lookup(htrServeID)
	if !ok {
		t.Fatal("the supervisor record must survive as the death record")
	}
	if dead.Status == tool.ProcRunning {
		t.Errorf("record status = %q, want a terminal status after death", dead.Status)
	}
	if _, err := readHTROwner(); err == nil {
		t.Error("the owner marker must be removed when the daemon dies")
	}
	// No auto-restart and no private fallback: either would register a process
	// under this supervisor, and there must be exactly the one that died.
	time.Sleep(200 * time.Millisecond)
	snap := sup.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("supervisor holds %d records, want exactly 1 (the daemon that died): %+v", len(snap), snap)
	}
	if snap[0].PID != rec.PID || snap[0].Status == tool.ProcRunning {
		t.Errorf("record = pid %d status %q, want the original pid %d left terminal",
			snap[0].PID, snap[0].Status, rec.PID)
	}
}
