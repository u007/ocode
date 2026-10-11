# Part 2 — Daemon lifecycle, ownership, stop rule (Tasks 3–5)

Self-contained. You do not need to read the other parts. Execute Task 3, then 4,
then 5.

**Spec:** `.opencode/plans/2026-10-01-htr-shared-daemon-spec.md`, sections 2, 3 and 6.

## Constraints that apply to every task in this part

- Spawn **only** via `tool.StartSupervised` inside `withHTRStartLock`, using the existing `htrServeID` registration with `RetainOnShutdown: true`. No raw `exec.Command`, no shell.
- **Never** set `HTR_BEARER_TOKEN` on the child. htrcli resolves its own token. There is a test pinning its absence.
- Set `HTR_MANAGED_ID` to the **shared token**, never a fresh random value, so the existing strict probe stays valid for a daemon we started.
- Every probe keeps the existing 2s client timeout; every wait loop is bounded.
- Daemon output must be **captured, not discarded**. Correcting an earlier note in this
  plan: a nil `cmd.Stdout` makes `os/exec` connect the descriptor to `os.DevNull`, so
  the daemon was never inheriting the TUI's terminal and was never an alt-screen
  corruption risk. The real defect was that daemon diagnostics were silently thrown
  away, which is exactly what CLAUDE.md says to capture instead. The invariant
  stands — neither stream may be an `*os.File`, since an `*os.File` is connected
  directly to that file — but the reason is now correct. See Task 3 Step 4.
- Ownership and lease files live under `paths.GlobalDataDir()`; diagnostics go to the package logger.
- Fail loudly: no mid-session auto-restart and no fallback to a private per-session daemon.

## Symbols

**Existing** (verified 2026-10-01): `EnsureHTRServe`, `StopHTRServe`, `HTRDaemonStatus`, `HTRStatus`, `htrHealthyForInstance`, `activeHTRLeases`, `processMatchesOwner`, `htrOwner`, `htrWriteOwner`, `readHTROwner`, `withHTRStartLock`, `watchHTRExit` in `internal/browse/cdp/htr.go`; `tool.StartSupervised` in `internal/tool/process_supervisor.go`.

**New in this part:** `htrHealthyForeign`, `htrOwner.StartedByPID`, `HTRStatus.StartedByOcode`.

---

### Task 3: Relaxed probe, adoption, and captured daemon output

**Files:**
- Modify: `internal/browse/cdp/htr.go` (`EnsureHTRServe`, spawn env, output wiring)
- Test: `internal/browse/cdp/htr_shared_lifecycle_test.go` (create)

**Interfaces:**
- Consumes: `HTROptions.Shared` (`SharedDaemon`, Task 1), `htrHealthyForInstance`, `withHTRStartLock`, `tool.StartSupervised`.
- Produces: `func htrHealthyForeign(port int, socket, token string) bool`, and package vars `htrHealthyForInstanceFn` / `htrHealthyForeignFn` so tests can stub both probes without a real port.

- [ ] **Step 1: Write the failing tests**

Create `internal/browse/cdp/htr_shared_lifecycle_test.go`:

```go
package cdp

import (
	"os"
	"strings"
	"testing"
)

func withStubbedProbes(t *testing.T, own, foreign bool) {
	t.Helper()
	origOwn, origForeign := htrHealthyForInstanceFn, htrHealthyForeignFn
	htrHealthyForInstanceFn = func(port int, socket, identity string) bool { return own }
	htrHealthyForeignFn = func(port int, socket, token string) bool { return foreign }
	t.Cleanup(func() { htrHealthyForInstanceFn, htrHealthyForeignFn = origOwn, origForeign })
}

func TestOwnAliveReusesWithoutSpawning(t *testing.T) {
	withStubbedProbes(t, true, false)
	st, err := EnsureHTRServe(nil, HTROptions{
		Enabled: true, Port: 3845,
		Shared: SharedDaemon{Mode: "shared", Port: 3845, Socket: "/tmp/x.sock", Token: "tok"},
	}, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !st.Running {
		t.Fatal("expected Running")
	}
	if st.StartedByOcode {
		t.Error("an adopted/alive daemon must not be reported as started by ocode")
	}
}

func TestForeignAliveIsAdoptedWithoutSpawnAndWithoutMarker(t *testing.T) {
	withStubbedProbes(t, false, true)
	home := t.TempDir()
	t.Setenv("XDG_DATA_HOME", home)

	st, err := EnsureHTRServe(nil, HTROptions{
		Enabled: true, Port: 3845,
		Shared: SharedDaemon{Mode: "shared", Port: 3845, Socket: "/tmp/x.sock", Token: "tok"},
	}, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !st.Running || st.StartedByOcode {
		t.Fatalf("running=%v startedByOcode=%v, want true/false", st.Running, st.StartedByOcode)
	}
	if owner, err := readHTROwner(); err == nil && owner.PID > 0 {
		t.Error("a foreign daemon must not get an ocode owner marker")
	}
}

func TestAdoptOnlyNeverSpawns(t *testing.T) {
	withStubbedProbes(t, false, false)
	_, err := EnsureHTRServe(nil, HTROptions{
		Enabled: true, Port: 3845,
		Shared: SharedDaemon{Mode: "shared", AdoptOnly: true, Port: 3845, Socket: "/tmp/x.sock"},
	}, discardLogger())
	if err == nil {
		t.Fatal("AdoptOnly with no daemon must return an error rather than pretending")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "adopt") &&
		!strings.Contains(strings.ToLower(err.Error()), "htrcli serve") {
		t.Errorf("error %q must tell the user to start `htrcli serve` themselves", err)
	}
}

func TestSharedSpawnEnvOmitsBearerToken(t *testing.T) {
	env := sharedServeEnv("tok", 3845, "/tmp/x.sock", "com.ocode.htrcontrol")
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "HTR_BEARER_TOKEN=") {
		t.Errorf("shared spawn must not pass HTR_BEARER_TOKEN; htrcli resolves its own:\n%s", joined)
	}
	if !strings.Contains(joined, "HTR_MANAGED_ID=tok") {
		t.Errorf("shared spawn must set HTR_MANAGED_ID to the shared token:\n%s", joined)
	}
	if !strings.Contains(joined, "HTR_PORT=3845") || !strings.Contains(joined, "HTR_SOCKET_PATH=/tmp/x.sock") {
		t.Errorf("shared spawn missing port/socket:\n%s", joined)
	}
	if strings.Contains(joined, "HTR_NATIVE_HOST_NAME=com.htrcontrol.host") {
		t.Error("must never point at the user's standalone host name")
	}
}

func TestDaemonOutputIsCapturedNotInherited(t *testing.T) {
	cmd := newSharedServeCmd("/nonexistent/htrcli", "tok", 3845, "/tmp/x.sock", "com.ocode.htrcontrol")
	if cmd.Stdout == nil || cmd.Stderr == nil {
		t.Fatal("daemon stdout/stderr must be captured, not discarded")
	}
	if f, ok := cmd.Stdout.(*os.File); ok {
		t.Fatalf("daemon stdout must not be an *os.File (os/exec connects those directly); got %v", f)
	}
}
```

Add a small `discardLogger()` helper in the same file returning `log.New(io.Discard, "", 0)`.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/browse/cdp/ -run 'TestOwnAlive|TestForeignAlive|TestAdoptOnly|TestSharedSpawnEnv|TestDaemonOutput' -v`
Expected: FAIL to compile — `htrHealthyForeignFn`, `sharedServeEnv`, `newSharedServeCmd`, `HTRStatus.StartedByOcode` undefined.

- [ ] **Step 3: Add the relaxed probe**

In `internal/browse/cdp/htr.go`, next to `htrHealthyForInstance`:

```go
// htrHealthyForeign probes a daemon ocode did not start. It is deliberately
// laxer than htrHealthyForInstance: a daemon the user started reports
// managed:false, because HTR_MANAGED_ID is only set by ocode's own spawn.
// Requiring service+port+socket+authentication is enough to adopt it safely.
func htrHealthyForeign(port int, socket, token string) bool {
	p, err := NormalizeHTRPort(port)
	if err != nil || strings.TrimSpace(token) == "" {
		return false
	}
	req, err := http.NewRequest(http.MethodGet, "http://"+htrAddr(p)+"/api/health", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false
	}
	var health htrHealthResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&health); err != nil {
		return false
	}
	h := health.Data
	return health.OK && h.Service == "htrcli" && h.Port == p && h.Socket == socket
}

var (
	htrHealthyForInstanceFn = htrHealthyForInstance
	htrHealthyForeignFn     = htrHealthyForeign
)
```

Add `StartedByOcode bool` to `HTRStatus`.

- [ ] **Step 4: Extract the spawn into helpers that capture output**

```go
// sharedServeEnv builds the child environment. It deliberately omits
// HTR_BEARER_TOKEN: htrcli resolves its own token from its config, so passing
// one here would create a second source of truth that can silently disagree.
func sharedServeEnv(token string, port int, socket, nativeHost string) []string {
	return append(os.Environ(),
		"HTR_PORT="+strconv.Itoa(port),
		"HTRCLI_NO_TRAY=1",
		"HTR_SOCKET_PATH="+socket,
		"HTR_NATIVE_HOST_NAME="+nativeHost,
		"HTR_MANAGED_ID="+token,
	)
}

// newSharedServeCmd builds the daemon command with its output captured. The
// A nil stream would send diagnostics to os.DevNull and lose them; an uncaptured
// pipe would also eventually block the child. Note os/exec already serialises
// writes when both streams are the same comparable writer, so one shared buffer
// is not a data race — the mutex is belt and braces, and the cap is the real
// requirement here, because the daemon outlives the call that built the sink.
func newSharedServeCmd(bin, token string, port int, socket, nativeHost string) *exec.Cmd {
	cmd := exec.Command(bin, "serve", "--no-tray")
	cmd.Env = sharedServeEnv(token, port, socket, nativeHost)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	return cmd
}
```

If the supervisor already tees child output into the debug log, use that sink
instead of a local buffer; the requirement is that `cmd.Stdout`/`cmd.Stderr` are
never `*os.File`.

- [ ] **Step 5: Wire the four states into `EnsureHTRServe`**

Order the resolution exactly as follows, before any spawn:

1. `shared := opts.Shared`; if `shared.Mode == "private"` keep today's code path unchanged.
2. If `htrHealthyForInstanceFn(port, socket, ownerIdentity)` → own-alive: reuse, `StartedByOcode: false`.
3. Else if `!shared.AdoptOnly && htrHealthyForeignFn(port, socket, shared.Token)` → foreign-alive: reuse, write **no** owner marker, `StartedByOcode: false`.
4. Else if `shared.AdoptOnly` → return an error naming `htrcli serve` and the config path.
5. Else spawn under `withHTRStartLock` with `newSharedServeCmd`, and inside the lock re-check both probes so two ocode processes racing on the same port cannot both spawn.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/browse/cdp/ -v`
Expected: PASS. The pre-existing native-host tests must still pass untouched.

- [ ] **Step 7: Run with the race detector**

Run: `go test -race ./internal/browse/cdp/`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/browse/cdp/htr.go internal/browse/cdp/htr_shared_lifecycle_test.go
git commit -m "feat(htr): adopt a foreign shared daemon and capture daemon output"
```

---

### Task 4: Ownership provenance and the stop rule

**Files:**
- Modify: `internal/browse/cdp/htr.go` (`htrOwner`, `htrWriteOwner`, `StopHTRServe`)
- Test: `internal/browse/cdp/htr_shared_stop_test.go` (create)

**Interfaces:**
- Consumes: `htrOwner`, `readHTROwner`, `processMatchesOwner`, `activeHTRLeases`, `StopHTRServe`.
- Produces: `htrOwner.StartedByPID int` (`json:"started_by_pid,omitempty"`).

- [ ] **Step 1: Write the failing tests**

```go
package cdp

import "testing"

func writeTestOwner(t *testing.T, startedBy int) htrOwner {
	t.Helper()
	o := htrOwner{Identity: "tok", PID: os.Getpid(), OwnerPID: startedBy, StartedByPID: startedBy, Port: 3845, Socket: "/tmp/x.sock"}
	if err := htrWriteOwner(o.Identity, o.Port, o.PID, o.Socket, "/bin/htrcli", nowForTest()); err != nil {
		t.Fatal(err)
	}
	return o
}

func TestStopRuleStopsOwnDaemon(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	stopped, err := shouldStopSharedDaemon(readOwnerForTest(t, os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	if !stopped {
		t.Error("a daemon this process spawned must be stopped on exit — that is the designed behaviour")
	}
}

func TestStopRuleRefusesForeignDaemon(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	owner := readOwnerForTest(t, os.Getpid()+1) // started by another process
	stopped, err := shouldStopSharedDaemon(owner)
	if err != nil {
		t.Fatal(err)
	}
	if stopped {
		t.Error("a daemon started by another ocode instance or an external run must never be stopped")
	}
}

func TestStopRuleDefersToLiveLease(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	// Simulate a second live ocode process holding a lease.
	acquireForeignLease(t, 3845, "/tmp/x.sock", "tok")
	stopped, err := shouldStopSharedDaemon(readOwnerForTest(t, os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	if stopped {
		t.Error("with another ocode instance holding a live lease, the daemon must survive this one exiting")
	}
}

func TestStopRuleUsesStartedByPIDNotOwnerPID(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	owner := readOwnerForTest(t, os.Getpid())
	owner.StartedByPID = os.Getpid() + 1 // adopted from another instance
	owner.OwnerPID = os.Getpid()         // but we rewrote the marker
	writeOwnerForTest(t, owner)
	stopped, err := shouldStopSharedDaemon(owner)
	if err != nil {
		t.Fatal(err)
	}
	if stopped {
		t.Error("provenance must come from StartedByPID; adopting rewrites OwnerPID and must not grant stop rights")
	}
}
```

Add the small helpers the tests use (`readOwnerForTest`, `writeOwnerForTest`,
`acquireForeignLease`, `nowForTest`) in the same file, plus package vars so the
liveness checks are stubbable — otherwise the recorded daemon PID is not
alive and the predicate correctly returns false:

```go
var (
	pidAliveFn       = pidAlive
	processMatchesFn = processMatchesOwner
)
```

Each stop-rule test stubs both to report a live, matching daemon.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/browse/cdp/ -run TestStopRule -v`
Expected: FAIL to compile — `shouldStopSharedDaemon`, `htrOwner.StartedByPID` undefined.

- [ ] **Step 3: Add provenance**

Add `StartedByPID int \`json:"started_by_pid,omitempty"\`` to `htrOwner`. Extend `htrWriteOwner` with a `startedByPID` parameter (update its single existing caller). `OwnerPID` keeps meaning "which ocode process last wrote this marker"; `StartedByPID` means "which process actually spawned the daemon".

- [ ] **Step 4: Implement the predicate and use it in `StopHTRServe`**

```go
// shouldStopSharedDaemon decides whether this process may terminate the
// recorded daemon. Three conditions, all required: we are the process that
// spawned it, the process is still the one we recorded (pid + start token), and
// no other ocode instance holds a live lease.
func shouldStopSharedDaemon(owner htrOwner) (bool, error) {
	if owner.PID <= 0 {
		return false, nil
	}
	if owner.StartedByPID != os.Getpid() {
		return false, nil
	}
	if !pidAlive(owner.PID) {
		return false, nil // marker cleanup is the caller's job
	}
	if !processMatchesOwner(owner) {
		return false, fmt.Errorf("managed htr daemon pid %d could not be verified; refusing to stop it", owner.PID)
	}
	active, err := activeHTRLeases(owner.Port, owner.Socket, owner.Identity)
	if err != nil {
		return false, err
	}
	return !active, nil
}
```

In `StopHTRServe`, call it first; on `false, nil` return `HTRStatus{Running: pidAlive(owner.PID), ...}` with no error and **no** kill. Keep the existing "could not be verified" error path.

- [ ] **Step 5: Document the ocode-crash orphan explicitly**

Add a comment on `htrOwner` stating: if ocode is SIGKILLed the daemon survives as an orphan and the next ocode run **adopts** it without stop rights, so an orphan can outlive every ocode process. That is intended (it is indistinguishable from a daemon the user started themselves, and the stop rule forbids killing those). Surface the pid in the Settings status so the user can end it deliberately — that is Task 7's `daemon_pid` field. Do not add auto-reaping.

- [ ] **Step 6: Run tests, then commit**

Run: `go test -race ./internal/browse/cdp/`
Expected: PASS.

```bash
git add internal/browse/cdp/htr.go internal/browse/cdp/htr_shared_stop_test.go
git commit -m "feat(htr): stop only a daemon this process spawned"
```

---

### Task 5: Non-blocking readiness and fail-loudly daemon death

**Files:**
- Modify: `internal/browse/cdp/htr.go` (readiness loop), `internal/server/server.go` (`StartBrowse` notice path)
- Test: `internal/browse/cdp/htr_shared_readiness_test.go` (create)

**Interfaces:**
- Consumes: `HTRStatus`, `watchHTRExit`, the `sharedServeEnv` helpers from Task 3.
- Produces: `func verifySharedDaemonAsync(port int, socket, token string, lg *log.Logger)` and `const sharedVerifyBudget = 15 * time.Second`.

- [ ] **Step 1: Write the failing test**

```go
package cdp

import (
	"testing"
	"time"
)

func TestReadinessReturnsBeforeFullBudget(t *testing.T) {
	withStubbedProbes(t, false, false) // never healthy
	// A real supervisor is required: with sup == nil EnsureHTRServe returns
	// before the readiness loop, and this test would pass vacuously.
	sup := newTestSupervisor(t)
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	start := time.Now()
	st, err := EnsureHTRServe(sup, HTROptions{
		Enabled: true, Port: 3845,
		Shared: SharedDaemon{Mode: "shared", Port: 3845, Socket: "/tmp/x.sock", Token: "tok", Binary: "/nonexistent/htrcli"},
	}, discardLogger())
	elapsed := time.Since(start)
	if err == nil && st.Running {
		t.Fatal("expected a not-running result when nothing ever becomes healthy")
	}
	if err == nil || !strings.Contains(err.Error(), "htrcli") {
		t.Errorf("error = %v, want one naming the missing binary rather than a hang", err)
	}
	if elapsed > 3*time.Second {
		t.Errorf("readiness blocked for %s; it must not stall boot", elapsed)
	}
}

func TestSharedVerifyBudgetIsBounded(t *testing.T) {
	if sharedVerifyBudget <= 0 || sharedVerifyBudget > 30*time.Second {
		t.Errorf("sharedVerifyBudget = %s, want a positive value under 30s", sharedVerifyBudget)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/browse/cdp/ -run 'TestReadiness|TestSharedVerifyBudget' -v`
Expected: FAIL — `sharedVerifyBudget` undefined, and the first test times out or blocks far past 3s.

- [ ] **Step 3: Split blocking from background verification**

Replace the existing bounded 50×100ms wait with:

- a **blocking** confirmation that the spawned process is alive (well under one second), returning a `Running: true, Owned: true` status immediately;
- `verifySharedDaemonAsync` launched via `crashguard.Go`, polling both probes every 250ms up to `sharedVerifyBudget`, then logging a single warning and updating the status. It must never touch `Handler.mu`.

```go
const sharedVerifyBudget = 15 * time.Second

func verifySharedDaemonAsync(port int, socket, token string, lg *log.Logger) {
	if lg == nil {
		lg = log.Default()
	}
	deadline := time.Now().Add(sharedVerifyBudget)
	for time.Now().Before(deadline) {
		if htrHealthyForInstanceFn(port, socket, token) || htrHealthyForeignFn(port, socket, token) {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	lg.Printf("htr: shared daemon on port %d did not become healthy within %s; run `htrcli serve` manually or check %s", port, sharedVerifyBudget, htrAddr(port))
}
```

- [ ] **Step 4: Fail loudly when the daemon dies mid-session**

Keep `watchHTRExit` as the single death detector. On exit it must mark the supervisor record dead and emit one `log.Printf` naming the port. Add **no** restart and **no** private-daemon fallback. If `internal/server` caches a status string for the UI, clear it so the Settings row shows `Stopped` — do not synthesise a healthy-looking status.

- [ ] **Step 5: Run tests, then commit**

Run: `go test -race ./internal/browse/cdp/ ./internal/server/ -run 'HTR|Browse|Browser'`
Expected: PASS.

```bash
git add internal/browse/cdp/htr.go internal/server/server.go internal/browse/cdp/htr_shared_readiness_test.go
git commit -m "feat(htr): verify shared-daemon readiness without blocking boot"
```
