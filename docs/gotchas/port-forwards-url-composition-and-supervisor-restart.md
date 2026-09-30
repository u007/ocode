---
type: Gotcha
title: 'Port forwards Disable/Enable: URL composed past query, supervisor retained-terminal collision, and dead-forward liveness/restart monitor'
description: 'Three defects broke the Port forwards panel: a URL helper returned a query-terminated string that callers appended path segments onto (the port landed inside the project param); the process supervisor retained terminal records, blocking stable-ID restart; and — added 2026-09-28 — a dead `ssh -N -L` child stayed reported live forever because nobody performed its Wait, so Disable→Enable could not revive it. Documents the forwardProcess reaper + SetOnExit hook, the portMapWatchdog restart policy (15s→60s exponential backoff, 30s settle window, give-up after 8 failures, event-driven wake + 10s safety tick), and the deliberate limitation that "live" only means the ssh child is running, not that the service behind the forward answers. Includes the test blind spot where widget API mocks can never catch malformed URLs. Added 2026-09-30: the first list request and server/desktop boot no longer block on opening persisted forwards (background, panic-safe `remote.RunAsync`), and the Ports capability probe `isPortMapsAvailable` fails closed — a failed probe hides the button instead of rendering a broken one.'
resource: "web/src/api/client.ts; internal/tool/process_supervisor.go; internal/remote/portmap.go; internal/server/portmap_watchdog.go; internal/server/handler_portmaps.go; internal/server/server.go; internal/desktop/boot.go; internal/desktop/portmaps.go"
tags:
  - port-forwards
  - url-composition
  - process-supervisor
  - web-api-client
  - liveness
  - watchdog
  - restart-monitor
  - capability-probe
  - background-autostart
  - gotcha
timestamp: 2026-09-30T03:53:25Z
---
# Port forwards Disable/Enable: URL composed past query, supervisor retained-terminal collision, and dead-forward liveness/restart monitor

Fixed 2026-09-16 (§1–§3), 2026-09-28 (§4), and 2026-09-30 (§5). Symptom of the original pair: toggling a
forward in the **Port forwards** panel answered
`host/project_path is not a remote project registered with this server`. The desktop
remote-workspace family (`/api/desktop/portmaps*`, no query) was unaffected — only the
project-scoped family hit both bugs. §4 covers a later, independent defect: when an
`ssh -N -L` child died on its own, the panel kept showing a live forward with nothing
listening, and Disable → Enable could not revive it. §5 covers the request/boot flow: the first
list and server/desktop boot no longer block on opening persisted forwards, and the Ports
capability probe fails closed.

## KEY LESSONS / GENERAL RULES

1. **Never append a path segment to a helper that returns a full URL-with-query.** If a
   helper builds `/api/resource?key=val`, appending `/${id}/disable` produces
   `/api/resource?key=val/3510/disable` — the segment lands *inside* the last query value,
   not in the path. The fix is to give the helper an explicit `suffix` parameter inserted
   *before* the query string.

2. **A terminal supervised record with a stable ID cannot be restarted by default.** The
   process supervisor retains terminal (exited/killed/failed) records, so `Start` with the
   same ID hits "already registered". Any manager that deliberately reuses an ID across
   Stop→Start **or crash→auto-restart** cycles must opt in to
   `ProcessRegistration.ReplaceTerminal` — since §4 the reaper marks the record terminal on
   *unexpected* exits too, and the restart monitor re-opens forwards under that same stable
   ID.

3. **Widget tests that mock the API module assert call arguments, not the real fetch URL.**
   If the API module is `vi.mock`-ed, no test ever constructs the actual URL string, so
   composition bugs pass silently. URL-level tests must stub `global.fetch` and assert the
   requested URL/method against the real client code.

4. **A `live` map entry written by `Start` and deleted only by `Stop` is a claim nobody
   verifies.** If the child can die without `Stop` being called — and an `ssh` tunnel always
   can: network drop, laptop sleep/wake, remote host reboot — then whoever *starts* the
   child must also *reap* it. Otherwise every reader of that map (`IsLive`, and `Start`'s
   already-running short-circuit) believes a lie forever. Give each child its own reaper
   that performs the `Wait`, marks the supervisor record terminal with a generation-aware
   (PID-qualified) call, and removes the live entry under an identity check.

5. **A successful open is not a recovery.** A restart policy that clears its failure count
   on a successful start is defeated by a forward that opens and dies immediately: every
   attempt "succeeds", so it is re-opened forever and never gives up. Count a recovery only
   when the child outlives a settle window (or is observed live on a later pass), and let
   the failure count — not the open — drive exponential backoff toward an explicit give-up
   that only the user's own Enable/Add clears.

6. **A side-effect open on a latency path belongs on a background goroutine — and that
   goroutine must recover.** The first list request for a project and server/desktop boot both
   double as the auto-start trigger for persisted forwards, and every open runs a bounded
   readiness probe (~5s when the tunnel cannot come up). Answer from persisted state
   immediately (`live: false` until the open lands) and run the open through a helper that
   recovers+logs panics — an unrecovered panic on ANY goroutine terminates the whole process,
   so a background forward open must never be able to take the app down.

7. **A capability probe must fail closed.** If a button is gated on "does the server have this
   route?", returning true for "anything but 404" means a 500 renders a button whose every
   action then fails. Gate on "did the server hand me a usable list?": 404, 400, 5xx, transport
   failure, and a 200 whose body is not an array are all "unavailable".

## 1. URL composition — suffix appended past the query

`portMapsPath(target)` in `web/src/api/client.ts` returned a URL that already contained
a query string (`/api/portmaps?host=…&project=~/www/app`). `removePortMap` and
`setPortMapEnabled` then concatenated `/${remotePort}` / `/${remotePort}/enable|disable`
*after* it, producing:

```
/api/portmaps?host=…&project=~/www/app/3510/disable
```

The server reads `project` as the full value `~/www/app/3510/disable` and 400s; the POST
even lands on the *add* route, not the enable route. List and Add were correct (no suffix),
so the panel rendered and a forward could be added — only Disable/Enable/Remove broke.

**Fix:** `portMapsPath(target, suffix)` now takes the trailing path segment so it is
inserted **before** the query:

```ts
// BEFORE (broken):
portMapsPath(target) + `/${port}/disable`   // → …?project=~/app/3510/disable

// AFTER (fixed):
portMapsPath(target, `/${port}/disable`)    // → …/3510/disable?project=~/app
```

**Where the seam is:** the helper is `portMapsPath(target, suffix)` in
`web/src/api/client.ts`, called by `removePortMap` and `setPortMapEnabled` (symbol anchors
only: that file was under concurrent edit when §4 was written and its line numbers had
already drifted by >1000 since this page was created — re-grep rather than trust an old
line). The five server routes are registered in `internal/server/server.go:548-552`
(`GET /api/portmaps`, `POST /api/portmaps`, `DELETE /api/portmaps/{port}`,
`POST /api/portmaps/{port}/enable|disable`); the handlers live in
`internal/server/handler_portmaps.go`.

## 2. Process supervisor — terminal record blocks stable-ID restart

`ForwardManager` in `internal/remote/portmap.go` registers each forward under a stable
supervisor ID `remote-portmap-<port>` (`forwardRegistrationID`,
`internal/remote/portmap.go:56`). Ending the forward leaves that record terminal, so the
next `Start` for the same port hits `process "remote-portmap-3510" already registered` —
surfaced as 502 `enabled, but failed to open now`. (At the time of the original bug,
`ForwardManager.Stop` marked the record itself. Since 2026-09-28, `Stop`
(`internal/remote/portmap.go:228-241`) only kills the child and blocks on the reaper's `done`
channel, and the reaper (`internal/remote/portmap.go:124`) does the marking via
`MarkKilledPID`/`MarkExitedPID` — see §4. Either way the record is terminal before the next
`Start` runs, so this section's conclusion is unchanged.)

Pre-existing: also reachable via `internal/desktop/portmaps.go` and `ocode remote --web`
`/port disable` → `/port enable`. Only exposed by the new panel because forwards are now
toggled frequently.

**Fix:** `ProcessRegistration.ReplaceTerminal` (new, opt-in). When set and the existing
record for the ID is **terminal**, `StartSupervised` replaces it instead of failing; a
still-running record is never replaced. The forward registration opts in
(`internal/remote/portmap.go:188-199`):

```go
tool.ProcessRegistration{
    ID:               forwardRegistrationID(pm.RemotePort),
    ReplaceTerminal:  true,   // allow Disable → Enable restart
    ...
}
```

**Where the seam is:** the replacement logic is `internal/tool/process_supervisor.go:213-234`
(`StartSupervised` registration fallback). The `canReplace` predicate is:

```go
canReplace := isTerminal && (
    reg.ReplaceTerminal ||
    reg.ID == "browse-chrome" ||
    reg.Kind == ProcessKindBrowser || recSnap.Kind == ProcessKindBrowser ||
    reg.Kind == ProcessKindHTR    || recSnap.Kind == ProcessKindHTR,
)
```

Generic `proc-N` collisions still error — only deliberately stable IDs opt in.

## 3. Test blind spot — widget mocks vs URL-level assertions

`web/src/components/Layout/PortMapsWidget.test.tsx` mocks `../../api/client` entirely, so
it asserts call *arguments* (e.g. "called with port 3510") but never constructs the actual
fetch URL. The composition bug was invisible to this test.

**Fix:** `web/src/api/client.portmaps.test.ts` stubs `global.fetch` and asserts the real
URL/method for list/add/remove/enable/disable with and without a target. This catches
composition bugs because the test exercises the real `portMapsPath` code path. Mirror this
pattern for new API endpoints that build URLs with query + path parameters.

## 4. Forward liveness — the manager owns Wait, the monitor owns the restart (2026-09-28)

Two further defects, independent of §1–§3. Line anchors in this section were verified
against the working tree on 2026-09-28.

### The bug: nobody performed the child's Wait

`ForwardManager.live` was written by `Start` and deleted only by `Stop`. The supervisor's
`waitFn` for these children is a deliberate no-op — *"The manager owns Wait; a no-op keeps
the supervisor from racing it"* (`internal/tool/process_supervisor.go:176-177`) — and
nothing ever performed that Wait. So when an `ssh` child died on its own (network drop,
laptop sleep/wake, remote host reboot):

- `IsLive` — a bare map lookup — answered `true` forever;
- `Start` short-circuited on the same stale entry (`internal/remote/portmap.go:177-180`)
  and returned nil **without opening anything**;
- Disable → Enable could not revive it: `Stop` killed nothing (no process), and `Start`
  "succeeded" on the stale entry, so the probe was never even reached.

Net effect: the panel showed a live forward with nothing listening on the local port.

### Fix A — a reaper owns each child (`internal/remote/portmap.go`)

- `forwardProcess` (`internal/remote/portmap.go:38`) holds the `exec.Cmd`, start time, a
  `done` channel, and a `stopped` flag that distinguishes a requested teardown from an
  unexpected exit.
- `Start` (`internal/remote/portmap.go:209-211`) registers the live entry and starts the reaper
  goroutine **before** the bounded readiness probe (`internal/remote/portmap.go:211-213`),
  so a child that exits during the probe is never recorded as live; the probe-failure path
  kills and blocks on `done` instead of racing the reaper
  (`internal/remote/portmap.go:213-222`).
- `reap` (`internal/remote/portmap.go:124`) performs the `Wait`, marks the supervisor record
  terminal with the PID-qualified `MarkExitedPID`/`MarkKilledPID` — generation-aware, so a
  stale reaper cannot clobber a newer record for the same stable ID — removes the live entry
  under an identity check `cur == fp` (`internal/remote/portmap.go:139`, so a re-open during
  the reap keeps the newer child's entry), closes `done`, and only then fires `SetOnExit`.
- `Stop` (`internal/remote/portmap.go:228-241`) kills and blocks on the reaper's `done` channel
  instead of calling `Wait` itself: two `Wait`s on one process race and the second never
  returns.
- `SetOnExit` (`internal/remote/portmap.go:103`) is the new exported hook —
  `func(remotePort, exitCode int, uptime time.Duration)` — fired from the reaper after the
  live entry is cleared, so the callback may safely call back into the manager. `uptime` is
  what lets a caller tell a forward that was healthy from one that flapped on start.

`IsLive` (`internal/remote/portmap.go:112`) is now a liveness signal rather than "Start was
called and did not error", because the reaper removes the entry the moment the child exits.

### Fix B — the restart monitor (`internal/server/portmap_watchdog.go`)

Detection alone only clears the lie; something has to re-open the forward. One
`portMapPolicy` per remote project, held by its `portMapEntry`
(`internal/server/handler_portmaps.go:54`), decides when that is allowed:

- **Backoff / give-up** (constants at `internal/server/portmap_watchdog.go:25-43`):
  exponential from `portMapBaseBackoff` 15s doubling to `portMapMaxBackoff` 60s, giving up
  after `portMapMaxFailures` 8 consecutive failures (~6 minutes of trying — enough to cover
  a host reboot, bounded for a host that is genuinely off); `portMapSettleWindow` 30s;
  `portMapWatchInterval` 10s.
- **A successful open does NOT clear the failure count** (`tryStart`,
  `internal/server/portmap_watchdog.go:156-182`): on success it only sets
  `nextTry = now + portMapSettleWindow`. A forward counts as recovered solely when a child
  outlives the settle window (`noteExit` with `uptime >= portMapSettleWindow`,
  `internal/server/portmap_watchdog.go:107-121`) or is observed live on a pass
  (`noteHealthy`). Otherwise a forward that opens and dies immediately would look healthy on
  every attempt and be re-opened forever.
- **Event-driven, ticker as safety net.** The `SetOnExit` hook installed in
  `portMapRegistry.entry` (`internal/server/handler_portmaps.go:88-91`) records the exit and
  nudges `wakeMonitor` (`internal/server/portmap_watchdog.go:260`) — a non-blocking send
  into the buffered-by-one `wake` channel (`internal/server/handler_portmaps.go:41`), since
  the sender is a reaper goroutine that must never block. `portMapWatchdogLoop`
  (`internal/server/portmap_watchdog.go:188`) waits on `wake` and on the 10s ticker, then
  runs `portMapWatchdogPass` (`internal/server/portmap_watchdog.go:208`) →
  `monitorPortMaps` (`internal/server/portmap_watchdog.go:220`), which reconciles each
  project's *persisted* forwards against `IsLive`: disabled → `forget`, live →
  `noteHealthy`, dead + enabled → `policy.tryStart`. Every restart, every failure, and the
  give-up are logged (`port forwards: monitor restarted|failed|gave up …`), and a store-read
  failure is logged too rather than swallowed.
- **The user's own actions are the escape hatch:** Enable and Add clear the give-up
  (`entry.policy.reset`, `internal/server/handler_portmaps.go:206` and `:269` — the
  deliberate "try it now"); Disable and Remove forget the port
  (`entry.policy.forget`, `internal/server/handler_portmaps.go:229` and `:265`) so a
  disabled forward is never revived by the watchdog.
- **Where it runs:** started in `Serve` right beside the idle-agent evictor —
  `internal/server/server.go:1557-1558` (`go s.handler.evictIdleLoop(stop)` /
  `go s.handler.portMapWatchdogLoop(stop)`), stopped by the same `stop` channel.

### Deliberately out of scope

- **The service *behind* a forward.** `waitForTunnelReady` only dials
  `127.0.0.1:localPort` (`internal/remote/connect.go:380`), and ssh's local listener accepts
  whether or not the remote end is reachable — so "live" means "our ssh child is running",
  **not** "the remote app answers". A dead backend behind a healthy forward is invisible to
  this monitor.
- **The panel.** `PortMapsWidget` was deliberately left load-on-open (it refetches only when
  the widget opens or the active project changes — no polling), so the panel reflects
  reality on the next open, not continuously.

## 5. First list and boot no longer block on opening forwards; the capability probe fails closed (2026-09-30)

Three changes to the port-map request/boot flow. Line anchors in this section were
verified against the working tree on 2026-09-30.

### The old flow — an inline open on the latency path

- `HandleListPortMaps` (`internal/server/handler_portmaps.go:173`) doubles as the
  auto-start trigger: the first list for a project ran `autoStartPortMaps`
  (`internal/server/handler_portmaps.go:147`) inline behind `entry.autoStartOnce`
  (`internal/server/handler_portmaps.go:59`). Every `fm.Start` runs the bounded readiness
  probe `waitForTunnelReady` (`internal/remote/connect.go:380` — `tunnelReadyAttempts` 25 ×
  `tunnelReadyInterval` 200ms, `internal/remote/connect.go:371-374`, ~5s), so a forward
  whose tunnel could not come up held the list response for ~5s.
- `startRemoteServer` (`internal/desktop/boot.go`) ran the same loop before `net.Listen`,
  so a single dead forward delayed the desktop window appearing by ~5s. (That
  remote-workspace mode is only reachable via a hand-written
  `~/.local/share/ocode/workspace.json`; no UI writes one — a latent fix, not a reported
  symptom.)

### Fix — answer from persisted state, open in the background via `remote.RunAsync`

- `HandleListPortMaps` still arms `entry.autoStartOnce` on the first list
  (`internal/server/handler_portmaps.go:179-183`) — `sync.Once.Do` returns as soon as the
  goroutine is spawned, so only one auto-start ever runs per project per process — but the
  open now goes through `remote.RunAsync`. The list answers immediately from persisted
  state; rows report `live: false` until the open lands, which is exactly how a disabled
  or not-yet-opened forward already rendered.
- **`remote.RunAsync(what string, fn func())`** (`internal/remote/portmap.go:29`) runs
  `fn` on its own goroutine and recovers+logs any panic with `debug.Stack()`. The recover
  is load-bearing, not padding: an unrecovered panic on ANY goroutine terminates the whole
  process, so a background forward open that panicked would take down the app and every
  session in it. `what` names the operation in the log line.
- Desktop: the loop was extracted to `(*portMapsHandler).autoStartEnabled()`
  (`internal/desktop/portmaps.go:62` — its doc comment says it is deliberately NOT fast and
  that latency-path callers must run it through `RunAsync`) and invoked as
  `remote.RunAsync("desktop port maps auto-start", pmHandler.autoStartEnabled)`
  (`internal/desktop/boot.go:380`), off the critical path before `net.Listen`.
- **Deliberately still synchronous:** the user-initiated Add and Enable paths. Their 502
  carries `saved, but failed to open now: …` / `enabled, but failed to open now: …`
  (`internal/server/handler_portmaps.go:220`, `:324`) to the panel, which is worth the
  wait.

### The capability probe fails closed — `isPortMapsAvailable` (`web/src/api/client.ts:3154`)

The Ports button gates on "did the server hand me a list?". The old probe returned `true`
for anything that was not a 404, so a 500 rendered the button whose every action then
failed. It now returns `Array.isArray(maps)` on success and `false` on every failure — 404
(route absent), 400 (server refuses this project: unregistered host or WSL target), 5xx,
transport error, and a 200 whose body is not an array (SPA fallback/proxy). The catch
carries an `// intentionally not logged:` comment: the probe runs on every project
switch, and the panel's own list call surfaces failures readably.

### Observable consequence

- The Ports button can now appear a moment before its forwards report live: the first list
  answers from persisted state immediately, so rows may show `enabled` while the
  background open is still inside its readiness probe. `PortMapsWidget` therefore
  re-fetches every `LIVE_POLL_INTERVAL_MS` (1.2s) while any **enabled** forward is not
  live, up to `LIVE_POLL_MAX_ATTEMPTS` (12, ~14s — forwards open one after another, and a
  dead one costs ~5s), resetting the budget on each dialog open / project change. The
  decision is the pure `shouldPollForLive` in `web/src/lib/portMapsLivePoll.ts`, so the
  budget is testable without waiting it out. A **disabled** forward is never polled for:
  the user asked for it to be down, so re-fetching could never converge — and the finite
  budget means a host that never comes back settles on a truthful "enabled, not live" row
  rather than polling forever.
- A failed probe hides the button rather than showing a broken one whose every click
  errors.

## Regression tests

- `web/src/api/client.portmaps.test.ts` — URL/method assertions for all portmap operations,
  plus the `isPortMapsAvailable` describe (fails closed on 404/400/500/transport/non-array).
- `web/src/lib/portMapsLivePoll.test.ts` — `pendingLiveForwards` / `shouldPollForLive`: a
  disabled row is never pending, and the attempt budget is finite.
- `web/src/components/Layout/PortMapsWidget.test.tsx` "polling while a forward is still
  opening" — the panel re-fetches until an enabled forward reports live, and does NOT
  re-fetch when nothing is pending or the row is disabled.
- `internal/server/handler_portmaps_test.go:TestPortMapsListDoesNotBlockOnAutoStart` — the
  list answers immediately; a second registered project (the canary, which takes the same
  `reg.mu`) is unaffected. `TestPortMapsAutoStartRunsAfterListResponds` — the open still
  happens, and the forward reaches live.
- `internal/remote/portmap_test.go:TestRunAsyncRecoversAndLogsPanic`,
  `TestRunAsyncReturnsBeforeFnFinishes` — the background runner absorbs a panic and is
  genuinely asynchronous.
- `internal/desktop/portmaps_test.go:TestAutoStartEnabledOpensOnlyEnabledForwards` — the
  extracted loop still opens only enabled forwards (the disabled row's local port is bound
  on purpose, or the assertion would pass either way),
  `TestAutoStartEnabledToleratesNilStore`.
- `internal/remote/portmap_test.go:TestForwardManagerRestartAfterStop` — fake `ssh` + open
  local port drive Start → Stop → Start; fails with the collision when `ReplaceTerminal`
  is removed.
- `internal/tool/process_test.go:TestStartSupervised_ReplaceTerminalAllowsRestartOfStableID`
  — opt-in replaces terminal records, default still collides, running records are never
  replaced.

§4 coverage (all green on 2026-09-28 via `go test ./internal/remote -run ForwardManager`
and `go test ./internal/server -run PortMap`):

- `internal/remote/portmap_test.go:TestForwardManagerReportsDeadAfterChildExits` — a child
  that exits on its own stops being reported live; the reaper, not `Stop`, clears `live`.
- `internal/remote/portmap_test.go:TestForwardManagerMarksSupervisorRecordTerminalOnUnexpectedExit`
  and `TestForwardManagerRestartAfterUnexpectedExit` — an unexpected exit marks the stable
  ID terminal and the same ID can start again.
- `internal/remote/portmap_test.go:TestForwardManagerStopAfterUnexpectedExitDoesNotHang` —
  `Stop` on an already-reaped forward returns.
- `internal/server/portmap_watchdog_test.go:TestPortMapEntryWakesMonitorWhenForwardDies` —
  end-to-end: a fake `ssh` exits on its own → `IsLive` false **and** the wake channel
  signalled.
- `internal/server/portmap_watchdog_test.go:TestPortMapPolicyGivesUpOnARepeatedlyFlappingForward`
  — pins Fix B's key subtlety: `Start` succeeds every time but the child dies inside the
  settle window, and the policy still reaches give-up (a "clear on successful open"
  implementation flaps forever).
- `internal/server/portmap_watchdog_test.go:TestPortMapPolicyBacksOffAfterAFlappingChild`,
  `TestPortMapPolicyBacksOffAfterAFailedOpen`, `TestPortMapPolicyGivesUpAfterRepeatedFailures`,
  `TestPortMapPolicyForgetsDisabledForwards`, `TestPortMapPolicyHealthyForwardClearsFailures`,
  `TestPortMapWatchdogPassRestartsOnlyEnabledDeadForwards`,
  `TestPortMapWatchdogPassSkipsLiveForwards`, `TestPortMapEnableResetsGiveUp`.

§5 coverage (green on 2026-09-30):

- `internal/server/handler_portmaps_test.go:TestPortMapsListDoesNotBlockOnAutoStart` — a
  project whose tunnel can never come up still gets an immediate list; a second registered
  project (whose list takes the same `reg.mu`) must answer immediately too, pinning that
  the registry lock is not held across the open.
- `internal/server/handler_portmaps_test.go:TestPortMapsAutoStartRunsAfterListResponds` —
  the response lands before the open, and the forward still reaches live afterwards.
- `internal/remote/portmap_test.go:TestRunAsyncRecoversAndLogsPanic` — the panic is
  recovered and logged with value + stack (the process survives).
- `internal/remote/portmap_test.go:TestRunAsyncReturnsBeforeFnFinishes` — RunAsync returns
  before fn completes: it is genuinely asynchronous.
- `internal/desktop/portmaps_test.go:TestAutoStartEnabledOpensOnlyEnabledForwards` — the
  extracted loop opens every persisted enabled forward and nothing else.
- `internal/desktop/portmaps_test.go:TestAutoStartEnabledToleratesNilStore` — a store that
  failed to open is a no-op, not a boot crash.
- `web/src/api/client.portmaps.test.ts` (new `isPortMapsAvailable` describe) — available
  on a JSON list; unavailable on 404, 400, 500, transport failure, and a non-array 200.

§1–§3 items were verified to fail with their fix reverted; the §4 items were run green
against the current tree (the feature landed with them).