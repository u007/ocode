# Part 04 — Chrome manager (`internal/browse/cdp/manager.go`, `launch.go`, `target.go`)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Checkbox steps. High-level by policy (no code in plans). TDD per task; commit per task.

**Goal:** Own the single headless Chrome process (discovery, launch through the supervisor, crash/idle lifecycle) and one browser context + page target per `stateKey`, exposing navigation, screencast, input, and telemetry to the browse WebSocket layer.

**Spec:** `docs/superpowers/specs/2026-08-31-browser-chrome-cdp-design.md` § Chrome process, § Target per stateKey, § Transport (server side of screencast/input), § Error handling.

**Global constraints:** `--remote-debugging-pipe` only; `--no-sandbox` forbidden; spawn only via `tool.StartSupervised`; loopback blocked; Windows → "not supported" error; no new deps.

## Interfaces consumed (from Parts 01–03)

- `tool.StartSupervised(sup, cmd, reg) (ProcessRecord, error)`, `tool.ProcessKindBrowser`, `sup.RegisterShutdownCallback(fn)`, `sup.MarkExited(id, code)`.
- `cdp.NewConn(r, w) *Conn`; `Call(ctx, sessionID, method, params, result)`; `Subscribe(sessionID, method) (ch, cancel)`; `Done()`; `Close()`.
- `cdp.NewEgressProxy(dialer) (*EgressProxy, error)`; `ProxyServerURL()`; `Close()`.

## Interfaces produced (used by Part 05)

- `ManagerOptions{ ChromePath string; IdleTimeout time.Duration; Supervisor *tool.ProcessSupervisor; Dialer *net.Dialer; EmitNav func(NavEvent); Log *log.Logger }` — `NavEvent` here is a `cdp`-local struct `{StateKey, URL string; Status int; Error string}` (Part 05 maps it to `browse.NavEvent{Mode:"chrome"}`; avoids the import cycle).
- `NewManager(opts ManagerOptions) *Manager`
- `(*Manager).Attach(ctx, stateKey string, sink FrameSink) (*Target, error)` — lazily launches Chrome, creates context+target for the key if absent, replaces any previous sink, (re)starts the screencast. Error kinds: `ErrChromeNotFound`, `ErrUnsupportedPlatform`, launch failure.
- `(*Manager).Revoke(stateKey string)` — closes target + disposes context; no-op if absent.
- `(*Manager).Close(ctx) error` — `Browser.close`, wait for exit (bounded by ctx), close proxy, remove temp dir.
- `FrameSink` interface: `Frame(width, height uint32, jpeg []byte)`; `Console(ConsoleEvent{Level string; Args []string; TS int64})`; `Network(NetworkEvent{Method, URL string; Status int; DurationMs int64; TS int64; Blocked string})`; `Error(msg string)`.
- `(*Target).Navigate(ctx, url) error` (rejects non-`http(s)` schemes with `ErrBadScheme`), `Back(ctx)`, `Forward(ctx)`, `Reload(ctx)`, `Resize(ctx, w, h int, dpr float64)`, `Mouse(ctx, MouseEvent)`, `Key(ctx, KeyEvent)`, `Detach()` (stops screencast, drops sink, keeps target alive).
- `MouseEvent{Kind string /* move|down|up|wheel */; X, Y float64; Button string; ClickCount int; DeltaX, DeltaY float64; Modifiers int}`; `KeyEvent{Kind string /* down|up|char */; Key, Code, Text string; Modifiers int}`.
- `FindChrome(configured string) (string, error)` — discovery.

## Test infrastructure for this part

A **stub CDP server** (`internal/browse/cdp/stubchrome_test.go`): speaks the pipe framing over `io.Pipe`s, answers the method set in the spec with canned results (`Target.createBrowserContext → {browserContextId}`, `Target.createTarget → {targetId}`, `Target.attachToTarget → {sessionId}`, everything else → `{}`), records every call, and lets tests inject events (`Page.screencastFrame`, `Runtime.consoleAPICalled`, `Network.responseReceived`, `Network.loadingFailed`, `Target.targetCrashed`, `Target.attachedToTarget`). The manager must accept an injectable launcher (`launch func(ctx) (*Conn, exited <-chan int, cleanup func(), err error)`) so tests bypass the real process; the production launcher is the only place that touches `exec`.

---

### Task 1: Binary discovery

**Files:**
- Create: `internal/browse/cdp/launch.go` (+ `launch_windows.go` / `launch_unix.go` if build tags are needed for `ExtraFiles`)
- Test: `internal/browse/cdp/launch_test.go`

- [ ] Step 1: Write failing tests: (a) configured path that exists (create a temp executable file) → returned as-is; (b) configured path missing → `ErrChromeNotFound` wrapping the path; (c) `OCODE_CHROME_PATH` env used when config empty (`t.Setenv`); (d) with both empty, candidates are probed in the documented order — inject a `statFn`/`lookPathFn` so the test can assert the order without real binaries; (e) on `GOOS == windows` → `ErrUnsupportedPlatform` regardless (tested via an injected `goos` variable).
- [ ] Step 2: Run → fails.
- [ ] Step 3: Implement with the macOS `.app` paths and Linux `$PATH` names from the spec.
- [ ] Step 4: Run → pass.
- [ ] Step 5: Commit `feat(browse/cdp): chrome binary discovery`.

---

### Task 2: Launcher (real process, pipe wiring)

**Files:** `launch.go`; test `launch_test.go` (one test gated on `OCODE_CHROME_PATH`).

- [ ] Step 1: Write a failing test (skipped without `OCODE_CHROME_PATH`): launching produces a `Conn` on which `Browser.getVersion` succeeds within 10 s; `Close` makes the process exit and the temp `--user-data-dir` disappear; the supervisor snapshot shows kind `browser`.
- [ ] Step 2: Run → skipped locally without the env var; with it, fails.
- [ ] Step 3: Implement: `os.Pipe()` ×2 → `cmd.ExtraFiles = [readEnd(for fd3), writeEnd(for fd4)]`; flags exactly as in the spec (with the temp dir); stderr piped to the logger at debug level; start through `tool.StartSupervised` with `ProcessRegistration{ID:"browse-chrome", Name:"headless chrome", Kind: ProcessKindBrowser, AllowGracefulShutdown:true}`; a goroutine owns `cmd.Wait`, closes the `exited` channel with the code and calls `sup.MarkExited`; register `Browser.close` via `sup.RegisterShutdownCallback` (bounded by a 3 s ctx). Handshake: `Browser.getVersion` with a 10 s timeout → on failure kill and return `launch failed: …`. Close parent copies of the child ends after `Start`.
- [ ] Step 4: Run (with the env var set locally) → pass. Confirm no `--no-sandbox`, no `--remote-debugging-port` anywhere (`grep -rn` in the package as part of the test).
- [ ] Step 5: Commit `feat(browse/cdp): headless chrome launcher via supervisor`.

---

### Task 3: Manager — lazy launch, context + target per key, revoke

**Files:**
- Create: `internal/browse/cdp/manager.go`, `internal/browse/cdp/target.go`, `internal/browse/cdp/stubchrome_test.go`
- Test: `internal/browse/cdp/manager_test.go`

- [ ] Step 1: Write failing tests (stub Chrome): (a) `NewManager` does not launch; first `Attach` launches once, calls `Target.createBrowserContext` with `proxyServer` = the egress proxy URL and `proxyBypassList:"<-loopback>"`, then `createTarget {url:"about:blank", browserContextId}`, `attachToTarget {flatten:true}`, `Target.setAutoAttach`, `Page.enable`, `Runtime.enable`, `Network.enable`; (b) second `Attach` for the same key creates **no** new context/target and replaces the sink (old sink receives no further frames); (c) two keys → two contexts; (d) `Revoke` → `closeTarget` + `disposeBrowserContext`; `Revoke` on unknown key is a no-op; (e) `Attach` when `FindChrome` fails → `ErrChromeNotFound` and `EmitNav` called with the "chrome not found — set browser.chrome_path" error; `Log` gets one line only across repeated attempts.
- [ ] Step 2: Run → fails.
- [ ] Step 3: Implement `Manager` (mutex, `targets map[string]*Target`, `launchOnce`-style state that resets after exit) and `Target` skeleton.
- [ ] Step 4: Run `-race` → pass.
- [ ] Step 5: Commit `feat(browse/cdp): manager with per-stateKey targets`.

---

### Task 4: Navigation + nav events

**Files:** `target.go`, `manager_test.go`.

- [ ] Step 1: Write failing tests: (a) `Navigate("https://example.com/")` → `Page.navigate` on the target session and an immediate `EmitNav{Status:0}`; (b) stub emits `Network.responseReceived {type:"Document", frameId:<main>, response.status:200, url}` → `EmitNav{URL, Status:200}`; (c) `Network.loadingFailed` for the main document with `errorText:"net::ERR_NAME_NOT_RESOLVED"` → `EmitNav{Status:0, Error:"…NAME_NOT_RESOLVED"}`; with `errorText` containing `ERR_TUNNEL_CONNECTION_FAILED`/`ERR_PROXY_CONNECTION_FAILED` → error text `"… not reachable from Chrome mode — open externally"`; (d) `Navigate("file:///etc/passwd")`, `chrome://settings`, `javascript:1`, `data:text/html,…` → `ErrBadScheme`, no CDP call; (e) `Page.frameNavigated` (main frame) to a non-`http(s)` URL → manager navigates to `about:blank` and emits a nav error; (f) `Back`/`Forward` use `Page.getNavigationHistory` + `navigateToHistoryEntry`; `Reload` → `Page.reload`; (g) `Page.navigatedWithinDocument` → `EmitNav{URL, Status:200}` (SPA route change).
- [ ] Step 2: Run → fails.
- [ ] Step 3: Implement; main-frame id learned from `Page.getFrameTree` or the first `frameNavigated` with no `parentId`.
- [ ] Step 4: Run → pass.
- [ ] Step 5: Commit `feat(browse/cdp): navigation + server-authoritative nav events`.

---

### Task 5: Screencast, resize, input

**Files:** `target.go`, `manager_test.go`.

- [ ] Step 1: Write failing tests: (a) `Attach` calls `Page.stopScreencast` then `Page.startScreencast {format:"jpeg", quality:70, maxWidth:1280, maxHeight:800, everyNthFrame:1}` (defaults until first `Resize`); (b) stub emits `Page.screencastFrame {data:<base64>, metadata:{deviceWidth, deviceHeight}, sessionId: 7}` → sink `Frame(w, h, decodedBytes)` then `Page.screencastFrameAck {sessionId:7}` — ack must happen **after** the sink call returns; (c) `Resize(1000, 600, 2)` → `Emulation.setDeviceMetricsOverride {width:1000, height:600, deviceScaleFactor:2, mobile:false}` then stop/start screencast with `maxWidth:2000, maxHeight:1200`; (d) `Mouse{Kind:"down", X, Y, Button:"left", ClickCount:1}` → `Input.dispatchMouseEvent {type:"mousePressed", …}`; `wheel` → `type:"mouseWheel"` with deltas; `move` → `mouseMoved`; (e) `Key{Kind:"down", Key:"a", Code:"KeyA", Text:"a"}` → `Input.dispatchKeyEvent {type:"keyDown", …}`; `char` → `type:"char"`; (f) `Detach` stops the screencast and later frames are not delivered.
- [ ] Step 2: Run → fails.
- [ ] Step 3: Implement; base64-decode frames in the manager so the sink gets raw JPEG.
- [ ] Step 4: Run `-race` → pass.
- [ ] Step 5: Commit `feat(browse/cdp): screencast + input forwarding`.

---

### Task 6: Console/network telemetry, auto-attach, crash & idle lifecycle

**Files:** `target.go`, `manager.go`, `manager_test.go`.

- [ ] Step 1: Write failing tests: (a) `Runtime.consoleAPICalled {type:"warning", args:[{value:"x"},{description:"Object"}]}` → sink `Console{Level:"warn", Args:["x","Object"]}` (map `warning→warn`, `log/info/error/debug` pass-through, others → `log`); `Runtime.exceptionThrown` → `Console{Level:"error", Args:[exception text + url:line]}`; (b) `Network.requestWillBeSent` + `responseReceived` pair → sink `Network{Method, URL, Status, DurationMs ≥ 0}`; `loadingFailed` with a tunnel/proxy error → `Network{Status:0, Blocked:"private address"}`; (c) `Target.attachedToTarget {sessionId:"W1", waitingForDebugger:true}` → `Runtime.runIfWaitingForDebugger` on `W1`; (d) `Target.targetCrashed` for a key → sink `Error("target crashed")`, `EmitNav{Error:"target crashed"}`, and the next `Attach` for that key creates a fresh target; (e) Chrome exit (`exited` channel fires) with two open keys → both sinks get `Error("chrome exited")`, both nav errors emitted, `MarkExited` called once, and the next `Attach` relaunches (launcher called twice in total); (f) idle reaper: with `IdleTimeout` = 50 ms in the test, after the last `Revoke` the launcher's `cleanup` runs and `Browser.close` was called; a later `Attach` relaunches.
- [ ] Step 2: Run → fails.
- [ ] Step 3: Implement; reaper is a timer reset on target-count changes, not a polling loop.
- [ ] Step 4: Run `-race` → pass.
- [ ] Step 5: Commit `feat(browse/cdp): telemetry, auto-attach, crash + idle lifecycle`.

## Verification for the part

- `go test -race ./internal/browse/cdp/` green; `OCODE_CHROME_PATH="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" go test ./internal/browse/cdp/ -run Launch` green on macOS.
- Files: `conn.go`, `egress.go`, `launch.go`, `manager.go`, `target.go` each ≤ ~300 lines.
