# Part 3 — Entry points: TUI, HTTP API, Settings UI (Tasks 6–7)

Self-contained. You do not need to read the other parts. Execute Task 6, then 7.

**Spec:** `.opencode/plans/2026-10-01-htr-shared-daemon-spec.md`, sections 4 and 5.

## Constraints that apply to every task in this part

- Every goroutine in `internal/tui` goes through `crashguard.Go`. Never a bare `go func()` — a panic there kills the process before Bubble Tea can leave the alt-screen.
- Never hold `Handler.mu` across a daemon spawn or health check. `htrBrowserConfig()` takes and releases the lock; everything after it is outside.
- Never call `os.Getwd()` in a server handler.
- Diagnostics via `emitDebug` / the package logger. Never `fmt.Print*` to stdout/stderr — the TUI runs in the alt-screen.
- Never emit the token value over the wire. Report provenance (`token_source`), not the secret.
- AdoptOnly must never be a silent no-op: `POST …/htr/start` refuses with the reason.

## Symbols

**Existing** (verified 2026-10-01): `server.StartBrowse`, `server.LoadBrowseOptions`, `server.resolveManagedHTROptions`, `Handler.htrBrowserConfig`, `Handler.htrStatus`, `Handler.startManagedHTR`, `Handler.stopManagedHTR`, `Handler.HandleGetHTRStatus`, `Handler.HandleStartHTR`, `Handler.HandleStopHTR`, `Handler.HandleListHTRTabs` in `internal/server/`; the seams `ensureHTRServeFn`, `stopHTRServeFn`, `htrDaemonStatusFn`, `listHTRTabsFn`, `htrOptionsFn` in `internal/server/handler_config.go`; the only TUI call site `server.StartBrowse` inside the `/rc` path in `internal/tui/model.go`; `web/src/components/Settings/BrowserForm.tsx`.

**New in this part:** `Handler.htrSharedSnapshot`, `daemon_pid` in the status response, `shared`/`adopt_only`/`effective_port`/`effective_socket`/`token_source`/`config_path` in the browser-config response.

---

### Task 6: A plain TUI session starts the shared daemon

**Files:**
- Modify: `internal/tui/model.go` (startup path — add beside the existing `/rc` browse call)
- Test: `internal/tui/htr_startup_test.go` (create)

**Interfaces:**
- Consumes: `server.LoadBrowseOptions`, `server.resolveManagedHTROptions` (both exported? — if `resolveManagedHTROptions` is unexported, add an exported `server.EnsureSharedHTRDaemon(sup, cfg)` wrapper in `internal/server/htr.go` and call that instead), `cdp.EnsureHTRServe`.
- Produces: `func (m *Model) ensureSharedHTRDaemon()` called once from the TUI startup path.

- [ ] **Step 1: Add the exported wrapper**

In `internal/server/htr.go` add:

```go
// EnsureSharedHTRDaemon resolves and ensures the shared daemon for callers
// outside the browse server (notably the TUI, which has no browse origin of its
// own). It is fire-and-forget safe: failures are returned, never fatal.
func EnsureSharedHTRDaemon(sup *tool.ProcessSupervisor, browser config.BrowserConfig, lg *log.Logger) (cdp.HTRStatus, error) {
	opts, notice := resolveManagedHTROptions(browser)
	if notice != "" {
		return cdp.HTRStatus{}, errors.New(notice)
	}
	if !opts.Enabled {
		return cdp.HTRStatus{}, errors.New("HTR is disabled")
	}
	return cdp.EnsureHTRServe(sup, opts, lg)
}
```

- [ ] **Step 2: Write the failing test**

`internal/tui/htr_startup_test.go`:

```go
package tui

import "testing"

// The TUI must reach the ensure through crashguard, never a bare goroutine.
// This pins the call site's existence and its fire-and-forget shape without
// starting a daemon.
func TestEnsureSharedHTRDaemonIsFireAndForget(t *testing.T) {
	var m Model
	// Seam defined in Step 4; overridden here so no daemon is started.
	m.ensureSharedHTRDaemonFn = func() { t.Log("ensure invoked") }
	done := make(chan struct{})
	m.ensureSharedHTRDaemonAsync(done)
	select {
	case <-done:
	default:
		t.Fatal("ensure must be asynchronous so TUI startup never blocks on a daemon")
	}
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestEnsureSharedHTRDaemon -v`
Expected: FAIL to compile — `ensureSharedHTRDaemonFn` undefined.

- [ ] **Step 4: Implement the TUI hook**

Add two fields to `Model` — `htrEnsured bool` and the test seam
`ensureSharedHTRDaemonFn func()` (nil in production, which falls through to the
real implementation below) — and:

```go
// ensureSharedHTRDaemonAsync starts the shared HTR daemon so the user's own
// browser extension has something to attach to. It runs through crashguard and
// never blocks startup: a slow or unreachable daemon must not delay the first
// prompt. Failures land in the debug log, not the frame.
func (m *Model) ensureSharedHTRDaemonAsync(done chan struct{}) {
	if done != nil {
		defer close(done)
	}
	if m.htrEnsured {
		return
	}
	m.htrEnsured = true
	ensure := m.ensureSharedHTRDaemonFn
	if ensure == nil {
		ensure = func() {
			browser := config.DefaultBrowserConfig()
			if ocfg, err := config.LoadOcodeConfigCopy(); err == nil && ocfg != nil {
				browser = ocfg.Browser
			}
			st, err := server.EnsureSharedHTRDaemon(m.procSup, browser, log.Default())
			if err != nil {
				m.emitDebugf("htr: shared daemon not started: %v", err)
				return
			}
			m.emitDebugf("htr: shared daemon %s running=%v startedByOcode=%v", st.Addr, st.Running, st.StartedByOcode)
		}
	}
	crashguard.Go(ensure)
}
```

Call it once from the TUI's startup path (the same place the model finishes
initialising), not from the `/rc` branch — `/rc` already goes through
`StartBrowse`, and a second ensure would be redundant. Guard with `m.htrEnsured`
so repeated startups in one process do not re-enter.

- [ ] **Step 5: Run tests, then commit**

Run: `go test ./internal/tui/ -run 'HTR|Shared' -v` then `go build ./...`
Expected: PASS / clean.

```bash
git add internal/server/htr.go internal/tui/model.go internal/tui/htr_startup_test.go
git commit -m "feat(tui): start the shared HTR daemon from a plain TUI session"
```

---

### Task 7: Settings API and the Settings UI

**Files:**
- Modify: `internal/server/handler_config.go` (`htrStatus`, `htrStatusResponse`, `HandleGetBrowserConfig`, `HandleStartHTR`, `HandleStopHTR`)
- Modify: `web/src/api/client.ts` (`HtrStatus`, browser-config types)
- Modify: `web/src/components/Settings/BrowserForm.tsx`
- Test: `internal/server/handler_htr_shared_test.go` (create), `web/src/components/Settings/BrowserForm.htrShared.test.tsx` (create)

**Interfaces:**
- Consumes: `SharedDaemon` (Task 1), `shouldStopSharedDaemon` semantics (Task 4), the seams `ensureHTRServeFn` / `stopHTRServeFn` / `htrOptionsFn`.
- Produces: status fields `mode`, `adopt_only`, `token_source`, `config_path`, `daemon_pid`, `started_by_ocode`; browser-config fields `htr_shared`, `htr_token_set`, `effective_port`, `effective_socket`.

- [ ] **Step 1: Write the failing Go test**

```go
package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStartHTRRefusesInAdoptOnlyWithReason(t *testing.T) {
	h := testConfigHandler(t)
	htrOptionsFn = func(config.BrowserConfig) (cdp.HTROptions, string) {
		return cdp.HTROptions{Enabled: true, Shared: cdp.SharedDaemon{Mode: "shared", AdoptOnly: true, ConfigPath: "/nope/.htrcli/config.json"}}, ""
	}
	t.Cleanup(func() { htrOptionsFn = resolveManagedHTROptions })

	rec := httptest.NewRecorder()
	h.HandleStartHTR(rec, httptest.NewRequest("POST", "/api/config/ocode/htr/start", nil))
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["adopt_only"] != true {
		t.Errorf("adopt_only = %v, want true", body["adopt_only"])
	}
	notice, _ := body["notice"].(string)
	if !strings.Contains(notice, "htrcli serve") {
		t.Errorf("notice %q must tell the user to start `htrcli serve` themselves", notice)
	}
}

func TestStopHTRRefusesForeignDaemonWithReason(t *testing.T) {
	h := testConfigHandler(t)
	stopHTRServeFn = func(*tool.ProcessSupervisor, int, *log.Logger) (cdp.HTRStatus, error) {
		return cdp.HTRStatus{Running: true, Addr: "127.0.0.1:3845"}, nil
	}
	t.Cleanup(func() { stopHTRServeFn = cdp.StopHTRServe })

	rec := httptest.NewRecorder()
	h.HandleStopHTR(rec, httptest.NewRequest("POST", "/api/config/ocode/htr/stop", nil))
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["stopped"] != false {
		t.Errorf("stopped = %v, want false", body["stopped"])
	}
	if reason, _ := body["reason"].(string); reason == "" {
		t.Error("a refused stop must carry a reason")
	}
}
```

Both tests use the package's existing `testConfigHandler(t)` constructor and the
existing `stubHTRSeams(t)` helper from `handler_config_test.go`; do not add a
second handler constructor.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/server/ -run 'TestStartHTRRefuses|TestStopHTRRefuses' -v`
Expected: FAIL — `adopt_only`/`stopped` absent from the response.

- [ ] **Step 3: Extend the status payload**

Add the fields to `htrStatusResponse` and populate them from the resolved
`SharedDaemon`: `Mode`, `AdoptOnly`, `TokenSource`, `ConfigPath`, plus
`DaemonPID` read from the owner marker and `StartedByOcode`. `stopManagedHTR`
must report `stopped:false` plus a `reason` when `shouldStopSharedDaemon` says
no — the refusal is the contract, not an error path.

- [ ] **Step 4: Extend `HandleGetBrowserConfig`**

Add `htr_shared`, `htr_token_set`, `effective_port`, `effective_socket`. The two
effective values come from the resolved `SharedDaemon`, so a legacy `htr_port:
3846` shows as `3845` rather than being echoed back and confusing the user.

- [ ] **Step 5: Write the failing UI test**

`web/src/components/Settings/BrowserForm.htrShared.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { BrowserForm } from "./BrowserForm";

const getBrowserConfig = vi.fn();
const getHtrStatus = vi.fn();

describe("BrowserForm shared HTR mode", () => {
  beforeEach(() => {
    getBrowserConfig.mockReset();
    getHtrStatus.mockReset();
  });

  it("shows the effective port with its provenance and greys the legacy field", async () => {
    getBrowserConfig.mockResolvedValue({
      chrome_path: "",
      idle_timeout_minutes: 10,
      screencast_quality: 95,
      htr_enabled: true,
      htr_shared: true,
      htr_token_set: false,
      htr_port: 3846, // legacy value that shared mode ignores
      effective_port: 3845,
      effective_socket: "/Users/me/.htrcli/daemon.sock",
      token_source: "htrcli-config",
      config_path: "/Users/me/.htrcli/config.json",
      htr_socket_path: "",
      htr_native_host_name: "com.ocode.htrcontrol",
      no_sandbox: true,
    });
    getHtrStatus.mockResolvedValue({
      enabled: true, running: true, managed: false, addr: "127.0.0.1:3845",
      port: 3845, socket: "/Users/me/.htrcli/daemon.sock", binary: "",
      mode: "shared", adopt_only: false, token_source: "htrcli-config",
      config_path: "/Users/me/.htrcli/config.json", daemon_pid: 4242,
      started_by_ocode: true, notice: "",
    });

    render(<BrowserForm />);
    await waitFor(() => expect(screen.getByTestId("htr-effective")).toBeTruthy());
    const eff = screen.getByTestId("htr-effective").textContent ?? "";
    expect(eff).toContain("3845");
    expect(eff).toContain("htrcli"); // provenance, not a bare number
    expect(screen.getByTestId("htr-port").hasAttribute("disabled")).toBe(true);
  });

  it("disables the stop button when ocode did not start the daemon", async () => {
    getBrowserConfig.mockResolvedValue({ htr_enabled: true, htr_shared: true, htr_native_host_name: "com.ocode.htrcontrol" });
    getHtrStatus.mockResolvedValue({
      enabled: true, running: true, addr: "127.0.0.1:3845", port: 3845,
      mode: "shared", started_by_ocode: false, notice: "",
    });
    render(<BrowserForm />);
    await waitFor(() => expect(screen.getByTestId("htr-stop")).toBeTruthy());
    expect(screen.getByTestId("htr-stop").hasAttribute("disabled")).toBe(true);
  });
});
```

Match the existing `BrowserForm` test file's mocking style (it already mocks the
api module — patch the exported `api` object, not named imports).

- [ ] **Step 6: Run to verify it fails**

Run: `cd web && npx vitest run src/components/Settings/BrowserForm.htrShared.test.tsx`
Expected: FAIL — no `htr-effective` test id.

- [ ] **Step 7: Implement the UI**

In `BrowserForm.tsx`:
- add a **Shared daemon** row showing `effective_port` / `effective_socket` with the `token_source` and `config_path` that produced them, under `data-testid="htr-effective"`;
- render the existing `htr_port` and `htr_socket_path` inputs `disabled` when `htr_shared` is true, with a hint "legacy private-daemon mode only";
- disable the stop button unless `started_by_ocode`;
- surface `notice` verbatim when `adopt_only` is true.

> **The two new keys are config-file-only — render them as read-only provenance,
> never as inputs.** `config.SaveOcodeHTRConfig` takes
> `(enabled, extensionPath, cliPath, port, runtimeOverrides...)` and has no
> parameter for `HTRShared` or `HTRToken`, so neither key can be written through
> this API. Both are read correctly by `LoadBrowseOptions` and `StartBrowse`, and
> nothing clobbers them: `SaveOcodeHTRConfig` and `SaveOcodeBrowserConfig` mutate
> only their named fields under `withOcodeConfigLock` rather than rewriting the
> `browser` section, so a hand-edited `htr_shared: false` survives an unrelated
> save in this form.
>
> Showing `htr_shared` and whether a `htr_token` is set is fine; offering an input
> that silently fails to persist is not. Making them editable means adding
> parameters to `SaveOcodeHTRConfig` — out of scope for this task, and it must
> keep `*bool` on the wire so an explicit `false` stays expressible.

Update the `HtrStatus` and browser-config types in `web/src/api/client.ts` to
match the Go payloads exactly (field names snake_case on the wire).

- [ ] **Step 8: Run tests, then commit**

Run: `go test ./internal/server/ -run 'HTR|Browser' && cd web && npx vitest run src/components/Settings/`
Expected: PASS.

```bash
git add internal/server/handler_config.go internal/server/handler_htr_shared_test.go \
        web/src/api/client.ts web/src/components/Settings/BrowserForm.tsx \
        web/src/components/Settings/BrowserForm.htrShared.test.tsx
git commit -m "feat(htr): show shared-daemon provenance in the settings API and UI"
```
