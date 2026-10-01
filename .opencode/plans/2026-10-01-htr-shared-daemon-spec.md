---
title: Shared HTR Daemon Design
status: approved; section 3 pending
date: 2026-10-01
---

# Shared HTR Daemon

ocode runs a single `htrcli serve` daemon that both its embedded browser and the
user's own browser extension attach to, instead of a private daemon that no
external extension can reach.

> **On line anchors.** Every `file.go:NNN` reference below was verified by grep
> against the working tree on **2026-10-01**. Line numbers are deliberately omitted
> from the body: this repo edits `internal/browse/cdp/htr.go` often, and a stale
> anchor silently rots into a lie. Cite the symbol instead — `EnsureHTRServe`,
> `StopHTRServe`, `withHTRStartLock`, `activeHTRLeases`, `htrHealthyForInstance`,
> `htrHealthyForeign` (new), `processMatchesOwner`, `validateHTRNativeHostName`,
> `ensureNativeHostManifest`, `rewriteNativeHostName`, `extensionRuntimeDirName`,
> `launchChromeWithOptions` — and re-derive the file if you need to look one up.

## Problem

ocode supervises its own `htrcli serve` with a namespaced native-messaging host,
its own socket, and a per-launch random bearer identity. A user-installed HTR
NControl extension talks to a completely separate standalone daemon on port 3845
via `com.htrcontrol.host`. The two never meet:

- ocode is forbidden from touching `com.htrcontrol.host`
  (`validateHTRNativeHostName` in `internal/browse/cdp/htr.go`, `ensureNativeHostManifest` in `internal/browse/cdp/native_host.go`)
- `DefaultHTRPort = 3846` exists specifically "to prevent ocode from attaching
  to or stopping a user's existing daemon" (`internal/browse/cdp/htr.go`)
- ocode's embedded Chromium gets `HTR_SOCKET_PATH` injected into its environment
  (`internal/browse/cdp/launch.go`), so its extension preload relays to
  ocode's socket, never the shared one

Consequence: the user's browser extension shows "not connected" whenever ocode
is running, and the only way to fix it is to run `htrcli serve` by hand.

Additionally, a plain TUI session never starts any daemon at all. HTR is only
reached through `StartBrowse` (`internal/server/server.go`), whose only TUI
call site is `/rc` (`internal/tui/model.go`).

## Scope of the daemon today

Per-process, not per-session: `EnsureHTRServe` is called once per ocode server
process (`internal/server/server.go`), and cross-process leases
(`activeHTRLeases`, `internal/browse/cdp/htr.go`) keep one daemon alive while
any ocode process holds a live lease. Spawning already goes through the shared
supervisor helper — `tool.StartSupervised` with
`ProcessRegistration{ID: "htr-serve", Kind: tool.ProcessKindHTR,
RetainOnShutdown: true}` — serialised by the existing cross-process
`withHTRStartLock` (`internal/browse/cdp/htr.go`, lock file
`<globalDataDir>/htr/start.lock`). This design reuses all of that and adds no new
spawn path.

## Decisions

Settled with the user before designing; do not re-litigate without asking.

1. **One shared daemon**, not two. ocode's private 3846 daemon goes away as the
   default.
2. **Configuration source is htrcli's own config**, `~/.htrcli/config.json`, so
   the token is never stored twice.
3. **Fallback is adopt-only.** When ocode cannot prove it may start a daemon, it
   probes and never spawns.
4. **Stop on exit only for a daemon this process spawned.** Daemons from another
   ocode server, another desktop instance, or any external run are left alone.
5. **The user's extension drops its connection whenever ocode closes**, when
   ocode is the daemon's starter. Intended, and it will read as a regression the
   first time it happens.
6. The extension preload keeps ocode's namespaced native host, so origin
   isolation and the user's `com.htrcontrol.host` file are untouched.

## 1. Where the daemon's coordinates come from

One new boolean, `browser.htr_shared` (default `true`). Two explicit modes with
no silent precedence:

- `htr_shared: true` — the daemon is whatever `~/.htrcli/config.json` describes.
- `htr_shared: false` — today's private daemon, unchanged (3846, ocode-managed
  socket, random identity). This is the rollback path.

Resolution under `htr_shared`, in order:

| Coordinate | Source | Fallback |
|---|---|---|
| port | port of `server` in `~/.htrcli/config.json`, only if loopback | `3845` |
| socket | `~/.htrcli/daemon.sock` | Windows: `127.0.0.1:3847` |
| token | `browser.htr_token` if set, else `token` in htrcli's config | empty means AdoptOnly |
| binary | `htrcli_path` in htrcli's config, then `HTRCLI_PATH` / `OCODE_HTRCLI_PATH`, then `PATH`, then ocode's embedded copy | embedded |

A non-loopback `server` is treated as unreadable-for-startup: ocode will not
spawn a daemon it cannot reach locally.

**AdoptOnly** is entered whenever ocode cannot prove it may start a daemon —
missing file, unparseable, non-JSON, or empty token. In AdoptOnly ocode probes,
never spawns, and reports why.

htrcli's config is parsed as JSON only. Viper also accepts TOML and YAML; a user
who converted their config gets AdoptOnly and a notice, rather than ocode
growing a TOML parser.

**Migration hazard.** Existing configs carry `htr_port: 3846`, which under
shared mode is ignored (it applies only to `htr_shared: false`). Left silent that
reads as a bug, so the Settings HTR section renders effective coordinates *with
provenance* (`port 3845 (from ~/.htrcli/config.json)`) and greys the legacy
fields with a "legacy private-daemon mode only" note.

## 2. Lifecycle, ownership, stop

Resolution yields a descriptor (port, socket, token, binary, mode).
`EnsureHTRServe` then resolves one of four states:

| State | Condition | Action |
|---|---|---|
| Own-alive | owner marker's identity authenticates on `/api/health` | reuse, no spawn |
| Foreign-alive | any token (config, or another instance's marker) authenticates | reuse, write no owner marker |
| Start | nobody answers and mode allows spawning | spawn under the start lock |
| Absent | nobody answers and mode is AdoptOnly | no spawn; notice explains why |

Two probes are required. The existing `htrHealthyForInstance`
(`internal/browse/cdp/htr.go`) requires `managed && identity == marker`,
which stays correct for a daemon we started. Adoption needs a new relaxed
`htrHealthyForeign(port, socket, token)` requiring only `service == "htrcli"`, a
port match, a socket match and a 200 on an authenticated probe — `managed` is
not required, because a daemon the user started reports `managed: false`.

**We still set `HTR_MANAGED_ID`.** htrcli echoes it into `/api/health` as
`managed` and `identity` (`apiHandler` in htrcli's `internal/host/server.go`) and reads nothing
else from it. Setting it to the shared token keeps the existing strict probe
valid for a daemon we started, at zero cost.

**We never pass `HTR_BEARER_TOKEN`.** htrcli resolves its own token —
`HTR_BEARER_TOKEN`, then `HTR_BEARER_TOKEN_FILE`, then viper `token`. A
tokenless spawn therefore reads the same `~/.htrcli/config.json` we did, so
ocode and htrcli cannot disagree about the bearer. *Verified live before this
design was written: a `htrcli serve` started with no token env reported its
bearer as the token from `~/.htrcli/config.json` and accepted it.*

**Spawn mechanics are unchanged.** Spawn only inside `withHTRStartLock`, only
via `tool.StartSupervised` with the existing `htr-serve` registration. No raw
`exec.Command`, no shell. Every probe keeps a bounded deadline (the existing 2s
client timeout), and every wait loop is bounded rather than open-ended.

**Ownership.** `ocode-owner.json` gains `started_by_pid` next to `owner_pid`.
They are usually identical and diverge exactly when ocode adopts a daemon
another instance started — the case the stop rule must not touch. Foreign-alive
daemons get no marker at all, so there is nothing to stop later. Stale-process
safety is unchanged: `processMatchesOwner` (`internal/browse/cdp/htr.go`)
matches pid *and* start token, so pid reuse cannot cause a wrong kill or a wrong
adoption.

**Stop rule.** Stop on exit only if this process spawned the daemon:

```
owner.StartedByPID == os.Getpid()  &&  processMatchesOwner(owner)  &&  no live lease
```

The lease clause is the one addition to the user's rule: if the TUI spawned the
daemon while the desktop app is running, the survivor's live lease keeps it
alive, and it dies with the last ocode process. Single-instance behaviour is
unchanged.

**Readiness must not block startup.** Desktop boot calls `StartBrowse`
synchronously (`internal/desktop/boot.go`), so a long blocking wait would
stall boot. Therefore:

- blocking: spawn the process and confirm it is alive (~1s, existing path)
- background: continue health verification for up to 15s; failure sets the
  existing notice and never blocks boot or TUI start

Both the background verify and the TUI trigger run through `crashguard.Go`, never
a bare goroutine.

## 3. What happens when the daemon dies mid-session — PENDING USER CONFIRMATION

**This section is not yet approved.** Everything else in this spec is settled;
the policy below is a recommendation awaiting the user's answer, and no other
section changes whichever way it goes.

Recommendation — fail loudly, and do not paper over it:

- the existing `watchHTRExit` path marks the daemon gone and the status flips to
  stopped; the reason reaches the user through the existing notice plus the
  Settings status line
- **no mid-session auto-restart**, and **no fallback to a private per-session
  daemon** — a silent fallback would recreate the two-daemon split this design
  removes and would hide the disconnect the user needs to notice
- recovery is the next explicit ensure: the next ocode start, or the Settings
  start button

This is the one place where reasonable people could differ, so it is called out
for confirmation rather than assumed: if you would rather ocode transparently
restart the shared daemon in place, say so and the design changes.

## 4. Native host, preload, what becomes shared

ocode's namespaced host stays. The rewrite of `com.htrcontrol.host` to
`com.ocode.htrcontrol` (`rewriteNativeHostName` in
`internal/browse/cdp/htr_assets.go`) and the
`com.ocode.htrcontrol.json` manifest are unchanged, so the user's
`com.htrcontrol.host.json` is never read, rewritten or removed. Because the
extracted directory is named from a hash of the host name
(`extensionRuntimeDirName` in `internal/browse/cdp/htr_assets.go`), ocode reuses its existing extraction
and no new extension ID appears.

Under shared mode ocode stops injecting `HTR_SOCKET_PATH` into the embedded
Chromium (the env block in `launchChromeWithOptions`,
`internal/browse/cdp/launch.go`), so the preload's relay falls back
to htrcli's default `~/.htrcli/daemon.sock` — the same socket the user's browser
relay dials. Two relays, one daemon. The relay needs no token of its own: the
daemon hands it the bearer in the socket handshake (`htrcli
internal/host/bridge.go`), which is why an external extension has always worked
with no ocode involvement.

`HTR_NATIVE_HOST_NAME` (`internal/browse/cdp/launch.go`) is dropped in shared
mode too. It has no consumer in htrcli, and an extension cannot read process
environment. It is inert; dropping it is cleanup, not a fix.

**Isolation across projects and sessions.** A shared daemon is not shared session
state: ocode issues exactly one request against it, a read-only `GET /api/tabs`
in `HandleListHTRTabs`, and never a command. ocode's own browser tabs live in the
embedded Chromium's ephemeral profile behind a per-`Manager` CDP connection, so
there is nothing to partition and nothing to clean up per session. No new
session-id-keyed map or journal is introduced, so `/reset-id` needs no change.

**One tab list becomes visible.** The daemon aggregates tabs from the embedded
Chromium and the user's browser. The Settings "list tabs" button will show mixed
tabs; that is honest and safe, because nothing can act on them.

**Optional add-on.** ocode writes its native-host manifest only for the browser
it selected, which orphans `com.ocode.htrcontrol.json` in other families — a
stale copy pointing at the 0.8.113 binary sits in Google Chrome's
NativeMessagingHosts. Proposal: prune `com.ocode.*` manifests from the other
known families when writing, never `com.htrcontrol.host`.

## 5. Entry points, config, Settings UI

Two new fields, threaded through the existing `LoadBrowseOptions` →
`BrowseOptions` → `resolveManagedHTROptions` → `cdp.HTROptions` path:

```
browser.htr_shared  bool    // default true; false = private daemon (rollback)
browser.htr_token   string  // escape hatch; "" = read htrcli's config
```

Desktop (`internal/desktop/boot.go`), `serve`
(`internal/server/server.go`) and `/rc` (`internal/tui/model.go`)
already flow through `StartBrowse`; no call-site changes. The plain TUI session
is new: it fires the ensure at startup through `crashguard.Go`, reported via
`emitDebug`, since the TUI has no HTR UI.

Eager is deliberate. Lazy has no trigger — a TUI session that never opens a
browser would never start the daemon, which is the behaviour being fixed.

**Server locking.** `POST /api/config/ocode/htr/start` resolves the descriptor
and performs the spawn *outside* `Handler.mu`; only the resulting status is
written back under the lock. No daemon startup, health wait or process spawn
happens while `Handler.mu` is held, in any mode.

`GET /api/config/ocode/htr` gains `mode`, `config_path`, `token_source`,
`adopt_only`, `effective_port`, `effective_socket` and `started_by_ocode`.
`POST …/htr/start` refuses with an explanation in AdoptOnly rather than silently
no-op'ing. `POST …/htr/stop` stops only when `started_by_pid` is our own pid and
otherwise returns `{"stopped": false, "reason": "started by another process"}`.

`BrowserForm.tsx` shows mode, effective coordinates with provenance, and greys
`htr_port` / `htr_socket_path` under shared mode.

## 6. Failure modes, tests, migration, docs

Degradation ladder — every failure keeps ocode usable and none silently disables
HTR:

| Failure | Behaviour |
|---|---|
| config missing / non-JSON / no token | AdoptOnly; probe only; notice names the file and the fix |
| `server` is non-loopback | AdoptOnly plus notice |
| port busy, daemon rejects our token | no spawn; notice names the port and the token mismatch |
| daemon never healthy | background verify times out at 15s; notice; boot already returned |
| daemon dies mid-session | loud status + notice; no auto-restart, no private fallback (section 3) |
| spawn binary missing | existing `ResolveHTRCliBinary` error surfaces verbatim |
| `htr_enabled: false` | daemon untouched; status shows `disabled` |

Tests:

- `resolveSharedDaemon` table test over full config, missing file, non-JSON,
  empty token, non-loopback `server`, `htr_token` override winning, port fallback
  to 3845, and the Windows socket. Pins section 1 exactly.
- four lifecycle states with fake probes: own-alive reuses without spawning;
  foreign-alive reuses and writes no owner marker; start asserts the child env
  contains `HTR_PORT`, `HTR_SOCKET_PATH` and `HTR_MANAGED_ID` and **does not
  contain `HTR_BEARER_TOKEN`**; AdoptOnly never spawns.
- stop rule: `StartedByPID == self` stops; a foreign pid is refused with a
  reason; a dead pid removes the marker without killing; one live lease from a
  second process prevents the stop.
- spawn goes through `StartSupervised` under `withHTRStartLock` — asserted, not
  assumed.
- `POST …/htr/start` completes with `Handler.mu` unheld during the spawn.
- regression, unchanged: ocode never modifies `com.htrcontrol.host`
  (`internal/browse/cdp/htr_test.go`), and the `com.htrcontrol.host` name is
  rejected.
- plain TUI ensures through a `crashguard` goroutine, never a bare `go func()`.
- `htr_shared` and `htr_token` survive config save and load.
- mutation checks on the two load-bearing assertions — injecting
  `HTR_BEARER_TOKEN`, and dropping `StartedByPID` from the stop condition — each
  verified to compile before being called caught, per
  `docs/gotchas/mutation-check-mutants-must-compile.md`.

No migration: `htr_shared` defaults to `true`, and an existing `htr_port: 3846` is
ignored and surfaced in the UI rather than acted on. `htr_shared: false` restores
today's behaviour exactly.

Documentation to update, all of it a consequence of a behaviour change:

- config reference for `browser.htr_shared` and `browser.htr_token` (these are
  `ocodeconfig.json` keys, not environment variables, so no `.env.example` entry)
- `skills/ocode-web/SKILL.md`, the embedded-browser settings docs, `CHANGES.md`
- the `DefaultHTRPort = 3846` comment (`internal/browse/cdp/htr.go`) and the
  "never touches `com.htrcontrol.host`" comments now describe only the legacy
  mode and must be corrected — a stale comment here is how the private daemon
  gets re-added
- runtime state stays under `paths.GlobalDataDir()`; diagnostics go to
  `paths.LogsDir()`, never a raw stderr write

## Open risks

- The Firefox extension only attaches when its side panel is open and remote
  control is enabled, so "connected" may need one manual toggle after a restart
  regardless of this work.
- Two daemons are gone, but two extension *clients* remain on one daemon; tab
  IDs from both appear in one list.
- The heaviest remaining assumption is a Firefox extension cold-start attaching
  to a daemon spawned with no token env. The token-resolution half is already
  proven live; the browser-side half needs a manual check.
- **Deferred, tracked in TODO.md:** the orphaned `com.ocode.htrcontrol.json` in
  browser families ocode no longer manages (section 4 add-on).
