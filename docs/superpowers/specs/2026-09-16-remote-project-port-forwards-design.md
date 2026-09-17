---
type: spec
title: Remote-project port forwards in the web/desktop UI
description: Design for restoring the Port forwards button so it works for the active remote SSH project (project-scoped /api/portmaps routes, per-project ssh -L forwards)
tags:
  - port-forwards
  - remote-ssh
  - desktop
  - web
  - api
timestamp: 2026-09-16T11:01:36Z
---
# Remote-project port forwards (Port forwards button)

Date: 2026-09-16
Status: Accepted

## Problem

The **Port forwards** button (`web/src/components/Layout/PortMapsWidget.tsx`,
rendered in `TopTabs.tsx`) disappeared from the desktop UI. Investigation:

- The `/api/desktop/portmaps*` routes are registered **only** by
  `startRemoteServer()` (`internal/desktop/boot.go`), i.e. the desktop
  remote-SSH **workspace** mode, which requires a hand-written
  `~/.local/share/ocode/workspace.json` (`mode: 1`). No UI writes that file,
  so the mode is unreachable in practice.
- In local desktop mode the app boots `server.New(...)` (`internal/server`),
  which has no such route.
- Before commit `bbdc0d27` (2026-09-16), unknown `/api/*` paths fell through
  to the SPA fallback (`index.html`, 200 text/html). The old
  `isPortMapsAvailable()` treated that non-`ApiError` as "available", so the
  button rendered — but every action failed. `bbdc0d27` added the JSON 404
  for unknown `/api/*` (`internal/server/web.go`, `internal/desktop/boot.go`),
  so the probe now correctly reports unavailable and the button is hidden.
- The user's actual workflow is remote SSH **projects** (sidebar entries with
  a `host` field, e.g. `~/www/kakiit` → `james@217.216.72.49`), which use
  per-request `ssh` exec (`internal/server/handler_remote_work.go`), not the
  desktop remote-workspace tunnel. Port forwards were never wired for them.

So the disappearance was the 404 fix working as designed; the real gap is
that port forwards only existed for a mode the UI cannot enter.

## Decision

Wire port forwards for the **active remote SSH project**, served by
`internal/server`, available in both the desktop app and the browser web UI.

The data model already fits: `projects.Store.PortMaps(ProjectRef{Host,Path})`
is host-scoped (`findRemoteIdx` requires a remote entry), the
`Port forwards` panel was only ever a missing HTTP surface plus widget
scoping.

## Architecture

### Backend — `internal/server/handler_portmaps.go`

- `Handler.portMaps` — `map[string]*remote.ForwardManager` keyed by the
  canonical target string + remote path, mutex-guarded. Each manager is
  constructed with the server's `tool.ProcessSupervisor` (new
  `Handler.procSup` field set beside `computerSup`, so the feature is not
  coupled to computer-use).
- Routes (all `authMiddleware`, all project-scoped by `?host=&project=`):
  - `GET  /api/portmaps`
  - `POST /api/portmaps` (body `{remote_port, local_port}`; local defaults to remote)
  - `DELETE /api/portmaps/{port}`
  - `POST /api/portmaps/{port}/enable`
  - `POST /api/portmaps/{port}/disable`
- Admission reuses `h.remoteWorkFor(host, path)`: only a registered remote
  project is accepted. WSL targets (`KindWSL`) are rejected with 400 — WSL2
  shares the Windows loopback, so extra forwards are unnecessary (same rule
  `internal/remotecli` applies by only wiring the `PortMapHook` for SSH).
- First touch of a project's manager lazily auto-starts its persisted
  `Enabled` forwards (bounded `remote.waitForTunnelReady`, same as the
  desktop boot), so forwards survive a server restart.
- Responses are `[{remote_port, local_port, enabled, live}]`
  (`live` = `ForwardManager.IsLive`).
- The existing `/api/desktop/portmaps*` family in
  `internal/desktop/portmaps.go` is unchanged and keeps serving the desktop
  remote-workspace mode.

### Web — `PortMapsWidget.tsx`

- Resolve the active project's host from the **trusted** project snapshot
  (the `getTrustedTerminalProject` rule: exactly one match by path, never
  route from a stale or ambiguous path).
- No remote SSH host active → probe `/api/desktop/portmaps`
  (remote-workspace fallback); hide on 404.
- Remote SSH host active → probe `/api/portmaps?host=&project=`; hide on 404
  (older server). Re-probe when the active project changes.
- Every mutation carries the same `host`/`project`.

## Error handling

- Unregistered host / missing project → 400.
- WSL target → 400 with an explanatory message.
- Store unavailable → 503.
- Forward fails to open (local port taken, ssh unreachable) → the map is
  saved and the response is a 502 naming the failure, so the panel can show
  a saved-but-not-live row (mirrors `portMapsHandler`).

## Testing

- `internal/server/handler_portmaps_test.go` — auth, unregistered host,
  WSL rejection, CRUD persistence, unknown-port 404. Mirrors
  `internal/desktop/portmaps_test.go`, which deliberately does not require a
  real `ssh` (the live-open attempt is expected to fail without a host).
- `web/src/components/Layout/PortMapsWidget.test.tsx` — remote-project mode:
  hidden without a remote host, list/add/remove call the project-scoped route
  with `host`/`project`, and the desktop fallback is used when no host is
  active.

## Consequences

- The Port forwards button now appears in the plain browser web UI whenever
  the active project is a remote SSH project. This is intentional: the server
  already opens `ssh` to registered remote projects for git/files/terminal,
  so extra `ssh -N -L` forwards are the same trust model.
- The desktop remote-workspace mode keeps its own route family; the widget
  prefers project-scoped routing when a host is active and falls back
  otherwise.