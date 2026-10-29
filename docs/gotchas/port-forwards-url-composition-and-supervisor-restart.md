---
type: Gotcha
title: 'Port forwards Disable/Enable: URL composed past query, supervisor retained-terminal collision, and dead-forward liveness/restart monitor'
description: 'Three defects broke the Port forwards panel: a URL helper returned a query-terminated string that callers appended path segments onto (the port landed inside the project param); the process supervisor retained terminal records, blocking stable-ID restart; and — added 2026-09-28 — a dead `ssh -N -L` child stayed reported live forever because nobody performed its Wait, so Disable→Enable could not revive it. Documents the forwardProcess reaper + SetOnExit hook, the portMapWatchdog restart policy (15s→60s exponential backoff, 30s settle window, give-up after 8 failures, event-driven wake + 10s safety tick), and the deliberate limitation that "live" only means the ssh child is running, not that the service behind the forward answers. Includes the test blind spot where widget API mocks can never catch malformed URLs. Added 2026-09-30: the first list request and server/desktop boot no longer block on opening persisted forwards (background, panic-safe `remote.RunAsync`), and the Ports capability probe `isPortMapsAvailable` fails closed — a failed probe hides the button instead of rendering a broken one. Added 2026-10-09 (updated 2026-10-21): reverse (`-R`) forwards — the web Ports panel add form now supports the `Reverse` checkbox (`-L` unticked = this machine reaches the remote; ticked = `-R`, remote reaches this machine''s local port), sends `"reverse": true`, and shows a security warning; the direction also resets after a successful add. The CLI `/port status` and `/port add -R` labels previously had the arrow inverted (`remote <- localhost` read as localhost→remote) and were fixed so traffic-flow is source-left. Loopback-pinned argv, readiness as an estimate, a `WaitDelay` on the ssh stderr pipe, and a single exit claim (`forwardProcess.phase`) so the monitor hook fires only for exits after a successful open, and an exit is reported once.'
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
timestamp: 2026-10-09T06:20:23Z
---
# Port forwards Disable/Enable: URL composed past query, supervisor retained-terminal collision, and dead-forward liveness/restart monitor

Fixed 2026-09-16 (§1–§3), 2026-09-28 (§4), and 2026-09-30 (§5). §6 (2026-10-09) adds reverse `-R` forwards. Symptom of the original pair: toggling a
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
line). The five server routes are registered in `internal/server/server.go:698-702`
(`GET /api/portmaps`, `POST /api/portmaps`, `DELETE /api/portmaps/{port}`,
`POST /api/portmaps/{port}/enable|disable`); the handlers live in
`internal/server/handler_portmaps.go`.

## 2. Process supervisor — terminal record blocks stable-ID restart

`ForwardManager` in `internal/remote/portmap.go` registers each forward under a stable
supervisor ID `remote-portmap-<port>` (`forwardRegistrationID`,
`internal/remote/portmap.go:121`). Ending the forward leaves that record terminal, so the
next `Start` for the same port hits `process "remote-portmap-3510" already registered` —
surfaced as 502 `enabled, but failed to open now`. (At the time of the original bug,
`ForwardManager.Stop` marked the record itself. Since 2026-09-28, `Stop`
(`internal/remote/portmap.go:327-343`) only kills the child and blocks on the reaper's `done`
channel, and the reaper (`internal/remote/portmap.go:166`) does the marking via
`MarkKilledPID`/`MarkExitedPID` — see §4. Either way the record is terminal before the next
`Start` runs, so this section's conclusion is unchanged.)

Pre-existing: also reachable via `internal/desktop/portmaps.go` and `ocode remote --web`
`/port disable` → `/port enable`. Only exposed by the new panel because forwards are now
toggled frequently.

**Fix:** `ProcessRegistration.ReplaceTerminal` (new, opt-in). When set and the existing
record for the ID is **terminal**, `StartSupervised` replaces it instead of failing; a
still-running record is never replaced. The forward registration opts in
(`internal/remote/portmap.go:257-269`):

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
- `Start` short-circuited on the same stale entry (`internal/remote/portmap.go:222-226`)
  and returned nil **without opening anything**;
- Disable → Enable could not revive it: `Stop` killed nothing (no process), and `Start`
  "succeeded" on the stale entry, so the probe was never even reached.

Net effect: the panel showed a live forward with nothing listening on the local port.

### Fix A — a reaper owns each child (`internal/remote/portmap.go`)

- `forwardProcess` (`internal/remote/portmap.go:69`) holds the `exec.Cmd`, start time, a
  `done` channel, and a `stopped` flag that distinguishes a requested teardown from an
  unexpected exit.
- `Start` (`internal/remote/portmap.go:276-280`) registers the live entry and starts the reaper
  goroutine **before** the bounded readiness probe (`internal/remote/portmap.go:282-287`),
  so a child that exits during the probe is never recorded as live; the probe-failure path
  kills and blocks on `done` instead of racing the reaper
  (`internal/remote/portmap.go:277-294`).
- `reap` (`internal/remote/portmap.go:166`) performs the `Wait`, marks the supervisor record
  terminal with the PID-qualified `MarkExitedPID`/`MarkKilledPID` — generation-aware, so a
  stale reaper cannot clobber a newer record for the same stable ID — removes the live entry
  under an identity check `cur == fp` (`internal/remote/portmap.go:182`, so a re-open during
  the reap keeps the newer child's entry), closes `done`, and only then fires `SetOnExit`.
- `Stop` (`internal/remote/portmap.go:327-343`) kills and blocks on the reaper's `done` channel
  instead of calling `Wait` itself: two `Wait`s on one process race and the second never
  returns.
- `SetOnExit` (`internal/remote/portmap.go:139`) is the new exported hook —
  `func(remotePort, exitCode int, uptime time.Duration)` — fired from the reaper after the
  live entry is cleared, so the callback may safely call back into the manager. `uptime` is
  what lets a caller tell a forward that was healthy from one that flapped on start.

`IsLive` (`internal/remote/portmap.go:154`) is now a liveness signal rather than "Start was
called and did not error", because the reaper removes the entry the moment the child exits.

### Fix B — the restart monitor (`internal/server/portmap_watchdog.go`)

Detection alone only clears the lie; something has to re-open the forward. One
`portMapPolicy` per remote project, held by its `portMapEntry`
(`internal/server/handler_portmaps.go:67`), decides when that is allowed:

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
  `portMapRegistry.entry` (`internal/server/handler_portmaps.go:90-111`; `SetOnExit` at `:108-111`) records the exit and
  nudges `wakeMonitor` (`internal/server/portmap_watchdog.go:331`) — a non-blocking send
  into the buffered-by-one `wake` channel (`internal/server/handler_portmaps.go:48`), since
  the sender is a reaper goroutine that must never block. `portMapWatchdogLoop`
  (`internal/server/portmap_watchdog.go:260`) waits on `wake` and on the 10s ticker, then
  runs `portMapWatchdogPass` (`internal/server/portmap_watchdog.go:280`) →
  `monitorPortMaps` (`internal/server/portmap_watchdog.go:292`), which reconciles each
  project's *persisted* forwards against `IsLive`: disabled → `forget`, live →
  `noteHealthy`, dead + enabled → `policy.tryStart`. Every restart, every failure, and the
  give-up are logged (`port forwards: monitor restarted|failed|gave up …`), and a store-read
  failure is logged too rather than swallowed. A project removed from the store
  (`projects.ErrRemoteProjectNotFound`) is handled once: its live ssh children are stopped
  (`ForwardManager.StopAll`), its registry entry is dropped (`portMapRegistry.drop`), and one
  `port forwards: remote project … was removed` line is logged. It is not re-logged every tick.
- **The user's own actions are the escape hatch:** Enable and Add clear the give-up
  (`entry.policy.reset`, `internal/server/handler_portmaps.go:447` (reset, Add) and `:545` (reset, Enable), forget via `entry.policy.forget` at `:499` (Remove) and `:541` (Disable) — the
  deliberate "try it now"); Disable and Remove forget the port
  (`entry.policy.forget`, `internal/server/handler_portmaps.go:541`) so a
  disabled forward is never revived by the watchdog.
- **Where it runs:** started in `Serve` right beside the idle-agent evictor —
  `internal/server/server.go:2074-2075` (`go s.handler.evictIdleLoop(stop)` /
  `go s.handler.portMapWatchdogLoop(stop)`), stopped by the same `stop` channel.

### Deliberately out of scope

- **The service *behind* a forward.** `waitForTunnelReady` only dials
  `127.0.0.1:localPort` (`internal/remote/connect.go:402`), and ssh's local listener accepts
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

- `HandleListPortMaps` (`internal/server/handler_portmaps.go:394`) doubles as the
  auto-start trigger: the first list for a project ran `autoStartPortMaps`
  (`internal/server/handler_portmaps.go:279`) inline behind `entry.autoStartOnce`
  (`internal/server/handler_portmaps.go:71`). Every `fm.Start` runs the bounded readiness
  probe `waitForTunnelReady` (`internal/remote/connect.go:394` — `tunnelReadyAttempts` 25 ×
  `tunnelReadyInterval` 200ms, `internal/remote/connect.go:385-388`, ~5s), so a forward
  whose tunnel could not come up held the list response for ~5s.
- `startRemoteServer` (`internal/desktop/boot.go`) ran the same loop before `net.Listen`,
  so a single dead forward delayed the desktop window appearing by ~5s. (That
  remote-workspace mode is only reachable via a hand-written
  `~/.local/share/ocode/workspace.json`; no UI writes one — a latent fix, not a reported
  symptom.)

### Fix — answer from persisted state, open in the background via `remote.RunAsync`

- `HandleListPortMaps` still arms `entry.autoStartOnce` on the first list
  (`internal/server/handler_portmaps.go:400` — the `portMapAutoStartOnce` call inside `HandleListPortMaps`) — `sync.Once.Do` returns as soon as the
  goroutine is spawned, so only one auto-start ever runs per project per process — but the
  open now goes through `remote.RunAsync`. The list answers immediately from persisted
  state; rows report `live: false` until the open lands, which is exactly how a disabled
  or not-yet-opened forward already rendered.
- **`remote.RunAsync(what string, fn func())`** (`internal/remote/portmap.go:32`) runs
  `fn` on its own goroutine and recovers+logs any panic with `debug.Stack()`. The recover
  is load-bearing, not padding: an unrecovered panic on ANY goroutine terminates the whole
  process, so a background forward open that panicked would take down the app and every
  session in it. `what` names the operation in the log line.
- Desktop: the loop was extracted to `(*portMapsHandler).autoStartEnabled()`
  (`internal/desktop/portmaps.go:65` — its doc comment says it is deliberately NOT fast and
  that latency-path callers must run it through `RunAsync`) and invoked as
  `remote.RunAsync("desktop port maps auto-start", pmHandler.autoStartEnabled)`
  (`internal/desktop/boot.go:444`), off the critical path before `net.Listen`.
- **Deliberately still synchronous:** the user-initiated Add and Enable paths. Their 502
  carries `saved, but failed to open now: …` / `enabled, but failed to open now: …`
  (`internal/server/handler_portmaps.go:449` — `"saved, but failed to open now"`; `:553` — `"enabled, but failed to open now"`) to the panel, which is worth the
  wait.

### The capability probe fails closed — `isPortMapsAvailable` (`web/src/api/client.ts:4259`)

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


## 5b. Boot sweep — every saved remote project is opened at server start (2026-10-21)

Before the sweep, a persisted, enabled SSH port forward was only ever opened by the **first**
`GET /api/portmaps?host=…&project=…` list request (`HandleListPortMaps`,
`internal/server/handler_portmaps.go:394`). The web Ports panel (`PortMapsWidget`) issues
that request only for the **active** project (`isPortMapsAvailable` → `api.listPortMaps` with
a `remoteForwardTarget` derived from the active project). The result: a saved remote project
that was not the active one at launch came up with its rows persisted but its tunnels down,
and the user had to click the project to make it connect. The watchdog could not compensate —
it visits only registry entries (`portMapWatchdogPass` → `monitorPortMaps`), so a project that
was never listed had no registry entry at all.

`Serve()` (`internal/server/server.go:2203`) now calls `s.handler.portMapAutoStartAll()`
(`internal/server/handler_portmaps.go:345`) at boot. It iterates the full saved project list
(`h.projects.List()`), skips local (`Host == ""`) and WSL (`target.Kind != remote.KindSSH`)
projects, carries the saved SSH port explicitly (`Project.RemotePort` into
`target.Port` when `target.Port == 0`; `Target.String()` is port-free), skips projects with
no persisted forwards (`len(p.PortMaps) == 0` — the watchdog visits every entry each tick, so
an entry for a forward-less project would be pure overhead), creates the registry entry (`h.portMaps.entry`),
and starts the persisted enabled forwards through the shared guard.

The sweep and the first list share **one** guard: `portMapAutoStartOnce`
(`internal/server/handler_portmaps.go:311`). It wraps `entry.autoStartOnce` (a `sync.Once`)
— so a project already swept at boot is never opened a second time by a later list request.
The open still runs in the background (`remote.RunAsync`) and never blocks the list response;
the row reports `live: false` until the open lands (`HandleListPortMaps:305` →
`portMapAutoStartOnce(entry, ref, true)`).

The **ephemeral reverse (`-R`) htrcli forward** (`openHTRReverseForward`) is **deliberately**
**not** opened by the sweep: `portMapAutoStartOnce` passes `withHTR = false` (`portMapAutoStartAll`:286 — the `withHTR = false` argument);
the list handler passes `withHTR = true` (`HandleListPortMaps`:305). The reverse forward is a
relay for the project the user is actually working in; opening it for every saved remote
project at boot would add one extra reverse ssh child per project, each exposing a local port
(`:<htrPort>`) to the remote host's loopback. The reverse forward has no `Once` guard
(`withHTR` is independent of `autoStartOnce`), so a boot sweep that consumed `autoStartOnce`
does not prevent the list from opening it. See "Host-scoped htrcli forward" below.

Only SSH targets are swept (`remote.KindSSH`). A project with no persisted forwards (`len(p.PortMaps) == 0`)
gets no registry entry — the watchdog visits every entry each tick, so a forward-less entry
would be pure overhead. The sweep carries the saved SSH port explicitly
(`Project.RemotePort` → `target.Port` when `target.Port == 0`; `Target.String()` is
port-free), matching the same rule `remoteWorkFor` applies when resolving a request's host.
Without it, a forward saved for a non-default SSH port (`p.RemotePort > 0`) would dial port 22.

The new file `internal/server/handler_portmaps_boot_test.go` covers the sweep: boot opens
only persisted enabled forwards for SSH remote projects (`portMapAutoStartAll`), skips WSL,
skips empty-forward projects, carries the saved port (`Project.RemotePort`), never opens the
reverse (`withHTR` false), shares `autoStartOnce` with the list handler (no double open), and
creates the registry entry so the watchdog can monitor from start.

## 6. Reverse (`-R`) forwards: the remote reaches the desktop (2026-10-09)

**Symptom it fixes:** a forward could only expose a remote service on the desktop (`-L`).
Nothing made a desktop service reachable from the remote host, so server processes there
could not use the desktop's htrcli relay (`:3845`) or Chrome CDP (`:9222`).

### Shape

- **Direction is a field, not a separate table.** `projects.PortMap.Reverse`
  (`json:"reverse,omitempty"`) and `remote.ProjectPortMap.Reverse`. The zero value is `-L`,
  so existing `projects.json` bytes are unchanged and no migration runs.
- **Argv:** `forwardArgs` (`internal/remote/portmap.go`) emits `-N`, the keepalive block,
  `-o ExitOnForwardFailure=yes`, and `-R 127.0.0.1:<remote>:127.0.0.1:<local>`. Both ends name
  loopback explicitly, so the remote listener stays off the network even on a host whose
  sshd allows `GatewayPorts`. There is no bind-address knob.
- **Keyed by remote port everywhere.** The remote port is one namespace on the remote host,
  so a `-R` and an `-L` on the same remote port are refused: `ErrPortMapDirection`, mapped
  to 409 by both handlers. Re-adding the same direction updates the local port in place.
- **Readiness is an estimate.** The web form's direction toggle does not change this. For `-L` the probe dials the LOCAL side, `127.0.0.1:localPort` (`waitForTunnelReady`), which ssh's own listener accepts whether or not the remote end is reachable. For `-R` dialing the local port proves nothing either, because the
  desktop service is already listening. A reverse forward is ready when its child is still
  alive after the same probe budget `waitForTunnelReady` uses. `ExitOnForwardFailure` turns a
  refused remote bind into an early exit, which fails fast and returns ssh's stderr. A remote
  listener that never accepts still reads live; `IsLive` and the reaper track later deaths.
- **SSH only.** `ForwardManager.Start` refuses a reverse forward on a WSL target before any
  process is spawned. WSL shares the Windows loopback, so there is nothing to reverse to.

### Stderr needs a wait delay, and the monitor hook needs the ready gate

- **The forward's stderr is a pipe, so `cmd.WaitDelay` is mandatory** (`forwardWaitDelay`, 2s).
  Capturing stderr is what lets a failed open report ssh's reason, but a `ProxyCommand` helper
  (nc, cloudflared, aws ssm) inherits that pipe and can outlive a killed ssh. Without the bound,
  `Wait` never returns: the reaper never runs, a dead forward reads live forever (the §4
  defect), and `Stop` blocks. This applies to `-L` too, because stderr is captured for every
  forward. The regression tests need a fixture that forks a background `sleep` **before** the
  kill or exit; otherwise the grandchild does not exist yet and the test passes without holding
  the pipe.
- **`onExit` fires only after a successful open, and each exit is claimed once.**
  `forwardProcess.phase` moves to ready when readiness passes (`markReady`), and the reaper
  claims the exit with a swap (`markExited`). An exit before readiness wins the claim, so
  `Start`'s error is its only report, and `tryStart`'s failure count sees it once. An exit
  after readiness goes only to `onExit`. A refused `-R` bind exits ssh during readiness. Without
  the claim the same exit also reached `noteExit`, so one failure counted twice. The same
  compare-and-swap closes the window where an exit lands just before `Start` returns.

### Rules

1. **Every persisted-to-runtime conversion goes through `projects.PortMap.Runtime()`.**
   Hand-building `remote.ProjectPortMap{...}` from a persisted entry silently restarts a `-R`
   as an `-L` on reconnect, enable, or a watchdog retry. Six call sites now share the helper.
2. **Direction is part of a forward's identity.** Any new path that reopens, lists, or
   removes persisted forwards must carry it.

### Surfaces

- API: `POST /api/portmaps` (project-scoped) and `POST /api/desktop/portmaps` take
  `"reverse": true`. List responses carry `reverse`.
- CLI: `/port add -R <remotePort>[:<localPort>]`. `/port status` shows forward rows as
  `localhost:L -> remote:N` and reverse (`-R`) rows as `remote:N -> localhost:M (reverse)`;
  `/port add -R` success reports `added: remote:N -> localhost:M (reverse)` and a plain
  `/port add` reports `added: localhost:L -> remote:N`. (Previously the CLI status line
  and web label drew the reverse arrow inverted — `remote:N <- localhost:M` — which reads
  as localhost→remote, the opposite of the `-R` traffic direction; the `-L` status line was
  also inverted to `remote:N -> localhost:M`. All four are now source-on-left.)
- Web panel: the `PortMapsWidget` (`PortMapsWidget.tsx`) now supports direction selection.
  Unticked (`reverse = false`) = `-L` (this machine reaches the remote's port); ticked (`reverse = true`)
  = `-R` (the remote reaches this machine's local port). The checkbox (`data-testid="portmap-reverse-toggle"`) sends `"reverse": true` in the `POST /api/portmaps` body (`web/src/api/client.ts`). After a successful add the checkbox resets to unticked so a second forward is not silently inverted. When ticked, a security warning (`data-testid="portmap-reverse-warning"`) is shown at creation time: anything that can reach the remote host's loopback (including other local users on a shared box) can then reach this machine's local port; port `9222` (Chrome CDP) hands full browser control — remove the entry when done. Row labels show `remote:N → localhost:M (reverse)` for reverse and `localhost:L → remote:N` for forward.

### Automatic htrcli forward (2026-10-14)

When `browser.htr_enabled` is on and the local htrcli daemon answers, the **list-request**
autostart (`portMapsHandler.autoStartEnabled` on desktop, `HandleListPortMaps` on the server)
also opens `-R <htrPort>:127.0.0.1:<htrPort>` through `server.StartHTRReverseForward` (under its
own `htrOnce` guard), so `htrcli` on the SSH remote drives the local browser. The boot sweep
(`portMapAutoStartAll`) **deliberately skips** the reverse forward (`withHTR` is `false`): opening
it for every saved remote project would add one reverse ssh child per project, each exposing a
local port to the remote host's loopback. Only the list request — which means the user is
looking at this project — opens it. It is ephemeral (not in `projects.json`, not in the
panel), is skipped when the user has a persisted forward on that remote port, and is a no-op on
WSL (logged). It is re-evaluated only on autostart, so start the local daemon first, then
reconnect. A remote htrcli configured for a different port is not remapped.

**Remote daemon collision (2026-10-21).** If the remote host runs its own htrcli daemon on the
same port, sshd cannot bind the `-R` listener (`remote port forwarding failed for listen port
3845`), `ExitOnForwardFailure` kills the ssh child, and remote `htrcli tabs list` talks to the
remote's browserless daemon ("No tabs connected"). `StartBrowse` therefore forces HTR off when
`srv.remoteMode` (`ocode serve --remote`) so the remote server never spawns that daemon.
Not covered: a TUI/`run` session on the remote with `browser.htr_enabled` still goes through
`EnsureSharedHTRDaemon`; set `htr_enabled: false` in the remote's `ocodeconfig.json`.

**Kill-and-retry prompt (2026-10-29).** `ForwardManager.Start` wraps a refused `-R` bind in
`*remote.PortBusyError` (message unchanged). `openHTRReverse` then probes the remote once
(`remote.FindPortHolder`: `ss -ltnp`, else `lsof`, plus `ps -o args=`) under a 5s deadline
(`htrHolderProbeTimeout`; a timed-out probe records no holder and is logged) and caches the holder
on the `htrHost`; `GET /api/portmaps/htr` returns it as `holder{pid,command}` and the Ports row shows
"Kill and retry" behind a confirm dialog. A live forward drops the cached holder
(`dropHolderIfLive`, from both status and the auto-start skip). `POST /api/portmaps/htr/reclaim {pid}`
refuses unless `pid` is the cached holder, re-verifies on the remote that it still holds the port
(`KillPortHolder`), sends SIGTERM only (never -9, never pid <= 1), then reopens the forward. Every
ssh exec in the probe and kill runs under a deadline context (`BoundedTransport.ExecContext`): the
probe's own 5s, the reclaim's request-scoped `portReclaimTimeout` (10s). A hung ssh is killed at
the deadline and cannot pin the request. A socket shared by a master and its workers lists all of
them: the kill signals every listener, after checking none is pid <= 1. Only the ssh user's own
processes are visible, so a port held by another user shows no holder and no kill button.

**Same prompt for `-L` rows (local holder).** A squatter on the LOCAL port answers the readiness
dial, so a refused `ssh -L` bind can surface either as the "did not become ready" error or as
"exited while opening"; `Start` reads ssh's stderr on both paths (`localBindRefused`:
"Address already in use" / "cannot listen to port") and returns `*remote.LocalPortBusyError`
carrying the holder (`FindLocalPortHolder`: lsof/ss + cmdline, local, no ssh; bounded by a 5s
`listenerProbeTimeout`). `ForwardManager`
remembers each port's last `Start` error (`StartError`, cleared by a successful Start or Stop), so
`portMapViews` can add `holder` to a down `-L` row without probing per list. `POST
/api/portmaps/{port}/reclaim {pid}` requires `pid` to be that reported holder, re-verifies it
(`KillLocalPortHolder`), SIGTERMs every listener on the port (never pid <= 1 or ocode's own pid; if
any listener is one of those, nothing is signalled), waits for the port to free under
`portReclaimTimeout`, then reopens. Unlike
`tunnel_reap.go` this is not limited to orphaned ocode tunnels: the confirm dialog shows the full
command instead, because the squatter may be the user's own dev server.

### Security

A reverse forward makes the desktop's local port reachable by everything on the remote host's
loopback, including other local users on a shared box. Port `9222` is Chrome CDP, which gives
full control of the browser. The web panel now surfaces this warning in the add form (the
`Reverse` checkbox arm shows the amber `portmap-reverse-warning` banner) so it is visible at
the point of creation, not only after the row exists. Forward only what is needed, and remove
the entry when done.

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

### Host-scoped htrcli forward (2026-10-22)

The remote port is a **host-wide** resource, but the forward used to live in the per-project
`ForwardManager`, guarded by a per-project `htrOnce`. Two failures followed: a second project on
the same host collided with the first (`process "remote-portmap-<host>-3845" already registered`,
or sshd's `remote port forwarding failed`), and a single failed open (stale remote listener after
a desktop restart, host briefly unreachable) was never retried, leaving the relay unmapped.

- `portMapRegistry.htr` holds one `htrHost` (own `ForwardManager`, `opening` CAS, `lastErr`,
  `lastAttempt`) per SSH host, shared by every project on it (`internal/server/htr_reverse.go`).
- The list request calls `autoStartHTRReverse`: skipped when `browser.htr_remote_autostart` is
  off or the forward is live, throttled to one attempt per `htrAutoRetryInterval` (15 s) per host,
  retried on every later list until it is live.
- Ports dialog row ("htrcli browser relay") + endpoints `GET /api/portmaps/htr`,
  `POST /api/portmaps/htr/start` (explicit, ignores the throttle and the setting, surfaces the ssh
  error) and `POST /api/portmaps/htr/autostart` (`{"enabled":bool}`, persists the global
  `browser.htr_remote_autostart`, default true).
