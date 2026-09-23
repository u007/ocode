---
type: Design
title: Embedded HTR Extension and Managed Daemon Design
description: Design for embedding the HTR NControl extension and a platform-matched htrcli daemon in ocode's managed Chrome flow, including the HTTP lifecycle API exposed from Settings > Browser.
tags:
  - design-spec
  - browser
  - htr
  - htrcli
  - automation
  - companion-daemon
timestamp: 2026-09-23T09:58:42Z
---
# Embedded HTR Extension and Managed Daemon Design

**Date:** 2026-09-09
**Status:** Approved for implementation

## Goal

Embed the HTR NControl extension and a platform-matched `htrcli` daemon in ocode's managed Chrome flow without changing the user's existing Chrome installation, profile, extension set, or standalone HTR daemon.

## Defaults and configuration

HTR is enabled by default for ocode-managed Chrome only. External/user Chrome is never modified or controlled by this integration. Setting `browser.htr.enabled: false` restores managed Chrome without HTR.

The integration is parameterized through browser options and config:

- `enabled` (default `true`)
- `extension_path` (optional development override; empty uses the embedded extension)
- `htrcli_path` (optional development override; empty uses the embedded platform binary)
- `daemon_port` (default `3846`, separate from standalone htrcli's `3845`)
- `socket_path` (optional; empty uses an ocode-managed socket)
- `native_host_name` (default `com.ocode.htrcontrol`)

## Build and packaging

`make install` and `make desktop-app` depend on a preparation target that locates `HTRCLI_ROOT` (default `../how-to-recorder/htrcli`) and `HTR_EXTENSION_DIR` (default `../how-to-recorder/build`), validates the extension manifest, builds `htrcli` for the target OS/architecture, and embeds both assets in the resulting ocode artifact. Missing source inputs produce an actionable build error.

The embedded extension uses a dedicated native host name and stable identity. Existing HTR builds retain `com.htrcontrol.host`; ocode's embedded build uses `com.ocode.htrcontrol` and does not overwrite the existing host registration.

## Runtime isolation

At runtime ocode extracts assets under its global data directory in a versioned HTR runtime directory. Installation is serialized across ocode processes, uses unique temporary paths plus atomic replacement, and records the embedded archive digest so partial or stale assets are repaired. Managed Chrome receives a dedicated profile, the extracted extension, and environment/options identifying the managed daemon socket. Native-host registration is namespaced and is created/updated only for the ocode host; existing registrations are preserved.

## Daemon lifecycle

The bundled daemon is launched with explicit port, socket, managed identity, and managed-lease options and with the tray disabled. Ocode processes acquire leases and refresh heartbeats through the shared lock using atomic writes. Health responses must prove the htrcli service, managed identity, port, and socket before a process is reused or terminated. The owner record also validates PID, executable, and process-start token before cleanup. A daemon waiter marks crashed processes exited so a later startup can replace the terminal supervisor record. The daemon remains alive while any lease is valid, expires crashed-process leases, and shuts itself down after the final lease expires. Normal ocode shutdown releases its lease. This is shared across multiple ocode and desktop processes and does not terminate an independently launched standalone htrcli daemon.

## Managed HTR HTTP API and Settings surface

The managed daemon lifecycle is driven entirely by four auth-wrapped HTTP endpoints under `/api/config/ocode/htr`, plus a live-apply semantic on the existing browser-config write. These are the interface between Settings > Browser and the supervisor-owned daemon described above.

### Endpoints

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/config/ocode/htr` | Snapshot: persisted `htr_enabled` flag plus live daemon probe |
| `POST` | `/api/config/ocode/htr/start` | Persist `htr_enabled=true`, then ensure exactly one managed daemon |
| `POST` | `/api/config/ocode/htr/stop` | Persist `htr_enabled=false`, then stop the managed daemon |
| `GET` | `/api/config/ocode/htr/tabs` | List browser tabs connected to the managed daemon |

All four are wrapped by the existing auth middleware (`server.go:393–396`); unauthenticated callers receive 401/403.

#### GET /api/config/ocode/htr — status snapshot

Returns `htrStatusResponse` (`internal/server/handler_config.go:1934`):

```json
{
  "enabled": true,
  "running": true,
  "managed": true,
  "addr": "127.0.0.1:3846",
  "port": 3846,
  "socket": "/tmp/ocode/htr.sock",
  "binary": "/Users/…/.ocode/…/htrcli",
  "error": ""
}
```

- `enabled` is the **persisted** config flag (`BrowserConfig.HTREnabled`), not a live runtime fact.
- `running`/`managed`/`addr`/`port`/`socket`/`binary` come from the live probe (`cdp.HTRDaemonStatus` in `internal/browse/cdp/htr.go:313`), which reads ocode's owner marker (port match + identity match + PID alive + healthy health-probe). If any check fails, the probe returns `managed:false` without an error.
- `error` is empty when nothing is wrong; it carries the notice string when a caller requested a startup that failed resolution (see status.error contract below).
- This endpoint is read-only: it never starts or stops anything.

#### POST /api/config/ocode/htr/start — enable + ensure one daemon

Two sequential steps, both reported at HTTP 200:

1. Persist `htr_enabled=true` to config (`config.SaveOcodeHTRConfig(true, "", "", 0)`) and apply it in-memory (`setHTREnabledInMemory`).
2. Call `cdp.EnsureHTRServe` (`internal/browse/cdp/htr.go:782`) via `ensureHTRServeFn` through the handler seam.

`EnsureHTRServe` implements the singleton guarantee: it reuses a healthy existing instance (same port/socket/identity, healthy health probe) or starts a new supervised `htrcli serve --no-tray` process (leases, owner marker, supervisor registration). If startup fails (asset resolution, native-host setup, port binding, extension launch), the failure is reported in the status body's `error` field at HTTP 200 — the endpoint itself never non-200s for a daemon reason.

#### POST /api/config/ocode/htr/stop — disable + stop daemon

Two sequential steps, both at HTTP 200:

1. Persist `htr_enabled=false` and apply in-memory.
2. Call `cdp.StopHTRServe` (`internal/browse/cdp/htr.go:340`) via `stopHTRServeFn`.

`StopHTRServe` only ever kills a process positively attributable to ocode — verified either by the owner marker's executable/start-token match or a healthy managed-identity health probe (`processMatchesOwner` / `htrHealthyForInstance`, line 360). A standalone `htrcli` daemon (no ocode owner marker, port `3845`) is never touched. Stopping an already-stopped daemon returns `Running:false` with no error.

#### GET /api/config/ocode/htr/tabs — connected browser tabs

Calls `cdp.ListHTRTabs` (`internal/browse/cdp/htr.go:384`), which:
1. Reads the owner marker; if no managed daemon is recorded (no owner, wrong port, empty identity) returns an error.
2. Issues `GET http://127.0.0.1:<port>/api/tabs` with `Authorization: Bearer <owner.Identity>` — the daemon's own bearer-protected tabs endpoint.
3. Returns `{ "tabs": [{ id, url, title, active, browser? }] }` at HTTP 200. If the daemon is not running or the query fails, returns `{ "tabs": [], "error": "…" }` at HTTP 200 so the Settings UI can render the error inline.

### enable = start, stop = disable semantics

`POST …/start` and `POST …/stop` are the **only** write paths for `htr_enabled` from the web/desktop Settings UI. They make the intent and the runtime action inseparable:

- **Start** = persist `enabled: true` first, then ensure a daemon. If the daemon cannot be started, `enabled` is still `true` in config (the user asked for it) and `error` explains the failure. Refreshing `GET /api/config/ocode/htr` shows `enabled: true, running: false, error: "…"`.
- **Stop** = persist `enabled: false` first, then stop the daemon. The daemon is stopped regardless of the `BrowserConfig.HTREnabled` in-memory value — `stopManagedHTR` reads `bcfg.HTRPort` and acts on it.
- A standalone `htrcli serve` on port `3845` is **never** started by these endpoints (the managed daemon listens on `3846` by default) and is **never** stopped by them (no ocode owner marker).

### HTTP 200 status.error contract

All three mutating/querying endpoints (`start`, `stop`, `tabs`) return HTTP 200 for daemon-level outcomes and encode failure in the response body:

| Field | Type | When populated |
|---|---|---|
| `error` | `string` (omitempty) | A descriptive message on any internal failure; empty on success |

This keeps the UI on a single response shape (the `HtrStatus` / `{tabs, error}` types) and lets the Settings panel show the reason inline without a separate error path. Non-200 responses only occur for auth failures or unexpected panics, not for daemon startup/stop/query failures.

### PUT /api/config/ocode/browser live-apply

The existing browser-config write handler (`internal/server/handler_config.go:1816`) gained live-apply semantics for `htr_enabled`: when `PUT /api/config/ocode/browser` carries `htr_enabled: true`, the handler invokes `startManagedHTR()` (line 1907); when `false`, it invokes `stopManagedHTR()` (line 1909). This means toggling HTR from the Settings checkbox is equivalent to the dedicated start/stop endpoints — the checkbox is a shortcut for "persist + act". Other browser fields (chrome path, idle timeout, screencast quality) continue to persist without side effects.

### Settings > Browser UI

`web/src/components/Settings/BrowserForm.tsx` renders an "HTR NControl daemon" section: a status line (running/stopped + port + binary path), an enabled checkbox (live-applied via `PUT /api/config/ocode/browser`), Start and Stop buttons (calling `POST …/start` and `POST …/stop`), a List tabs button (calling `GET …/tabs`), and an error display. Status is re-probed every 10 seconds while mounted, because another ocode process or the tray icon may have changed the daemon state. The HTR tab list renders under a `data-testid="htr-tabs"` container.

### Route registration

All four endpoints are registered in `internal/server/server.go:393–396` behind `authMiddleware`:

```
GET  /api/config/ocode/htr          → handleGetHTRStatus
POST /api/config/ocode/htr/start    → handleStartHTR
POST /api/config/ocode/htr/stop     → handleStopHTR
GET  /api/config/ocode/htr/tabs     → handleListHTRTabs
```

The server-level handler delegates to `Handler` methods (`HandleGetHTRStatus`, `HandleStartHTR`, `HandleStopHTR`, `HandleListHTRTabs`) in `internal/server/handler_config.go`. The daemon-probe functions are seam-aliased at lines 1926–1930 (`ensureHTRServeFn = cdp.EnsureHTRServe`, `stopHTRServeFn = cdp.StopHTRServe`, `htrDaemonStatusFn = cdp.HTRDaemonStatus`, `listHTRTabsFn = cdp.ListHTRTabs`, `htrOptionsFn = resolveManagedHTROptions`) so the server test suite never resolves assets, probes a real port, or spawns an `htrcli` process.

## Failure behavior

HTR startup is best effort. If extraction, native-host setup, daemon startup, port binding, browser-family registration, or extension launch fails, ocode logs the reason, starts managed Chrome without HTR, and keeps browsing available. Branded Google Chrome's unsupported unpacked-extension preload is handled the same way. The browser panel displays a persistent actionable notice identifying the failure (`Server.SetHTRNotice`, `server.go:185`); there is no silent fallback. The `error` field on the four `/api/config/ocode/htr/*` responses carries the same information to Settings > Browser.

## Cross-platform behavior

The build produces the matching `htrcli` executable for macOS, Linux, and Windows targets. Native-host installation selects the configured browser family (Chrome, Chromium, Canary, Edge, or Brave) and uses each platform's supported mechanism. Process cleanup uses the existing ocode process-supervision abstractions plus the daemon lease timeout for crash recovery. The Windows daemon and packaging path are buildable, but ocode's embedded Chrome CDP pipe remains explicitly gated by `ErrUnsupportedPlatform`; this is not a claim of full Windows browser-panel support.

## Validation

Tests will cover configuration defaults and overrides, asset selection, platform-specific command construction, native-host isolation, shared leases, last-owner shutdown, crashed-owner expiry, startup fallback notices, and unchanged behavior when HTR is disabled.

Specific to the HTTP API surface:

- `internal/server/handler_config_test.go` — start persists `htr_enabled=true` and returns `running:true` or `error`; stop persists `htr_enabled=false`; tabs returns an empty list with `error` when the daemon is absent; all responses are HTTP 200.
- `internal/browse/cdp/htr_test.go` — `HTRDaemonStatus` reports `managed:false` when no owner marker is present (standalone daemon untouched); `StopHTRServe` refuses to stop a process that fails owner verification; `ListHTRTabs` fails when no managed daemon is recorded.
- `web/src/components/Settings/BrowserForm.test.tsx` — status renders the port and running state; start/stop toggle `enabled`; tabs render under `htr-tabs`.
