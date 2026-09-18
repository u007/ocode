---
type: Decision
title: Remote Persistent Sessions and Terminals Design
description: 'Approved design: remote (SSH/WSL) project terminals run inside the host ocode serve --remote and survive laptop sleep/restart; version-mismatched remote servers are reused, surfaced, and restartable; sidebar lists remote chats and terminals for reattach.'
tags:
  - remote
  - terminal
  - sessions
  - design
  - approved
  - web
  - server
timestamp: 2026-09-18T00:30:00Z
---
# Remote Persistent Sessions and Terminals Design

**Date:** 2026-09-18
**Status:** Approved — not yet implemented
**Scope:** Remote (SSH/WSL) projects only. Local projects are untouched.

## Problem

A remote project's chat already runs on the host inside a detached
`ocode serve --remote` (`internal/remote/serve.go`), so an agent turn survives
the laptop sleeping. Three things do not:

1. **Terminals.** The pty for a remote project is started on the *local*
   machine and spawns `ssh`/`wsl.exe` into the host
   (`internal/server/handler_terminal.go`). Suspend kills ssh, the remote
   shell gets SIGHUP, and everything in it is lost.
2. **Version mismatch.** `EnsureRemoteServer` auto-launches a fresh server
   when the discovered one has a different version, leaving the old one
   orphaned with its terminals and in-flight chats. The user never sees this.
3. **Inventory.** After wake, app restart, or reconnect there is no view of
   which chats and terminals are alive on the host, and no way to reattach.

## Decisions (from brainstorming)

- Terminal persistence level: **the remote server owns the pty.** No tmux.
- Restart policy: **restart always, no guard.** Terminals die, chats resume
  from disk.
- UI placement: **per remote project row in the project sidebar**, not a new
  top-level panel.

## Section 1: Terminal runs on the host

### Routing

For a project with a `host`, the SPA opens the terminal websocket and calls
the terminal HTTP endpoints through the existing remote proxy:

```
/api/remote/{host}/api/terminal/ws?project_path=…
/api/remote/{host}/api/terminal/{id}/history
/api/remote/{host}/api/terminal/{id}          (DELETE)
/api/remote/{host}/api/terminal/processes
/api/remote/{host}/api/terminal              (GET, new — see Section 3)
```

Every call carries `X-Ocode-Project: <path>` so `HandleRemoteProxy` registers
the project on the host on first use. The remote server then runs a plain
local login shell in the project directory — the same code path a local
project uses today. `~` is expanded by the host (`projects.ExpandHome`).

The `host=`/`port=` query params are **not** sent on the proxied URL; the
remote server would otherwise try to ssh from the host to itself.

The existing local ssh/wsl.exe pty spawn path in `HandleTerminalWS` stays as
is. It is still used by `ocode remote --web` (the legacy flow) and is not
touched by this change.

### Websocket auth through the proxy

The remote server is in `--remote` mode: query-string tokens are forbidden
and the websocket token must arrive as the subprotocol
`ocode.bearer.<token>` (`remoteWSToken` in `server.go`). The browser
authenticates to the *local* server the way it does today: `?token=` when
the local server is a normal desktop/serve process, or its own
`ocode.bearer.<localToken>` subprotocol when the local server is itself in
`--remote` mode (legacy `ocode remote --web`).

`remote.InjectAuth` currently only sets `Authorization: Bearer`. Extend it
for Upgrade requests: strip `?token=` (already done), remove any
`ocode.bearer.*` entry from the offered `Sec-WebSocket-Protocol` list, and
append `ocode.bearer.<remoteToken>`. `httputil.ReverseProxy` forwards 101
Upgrade responses and hijacks the connection, so no separate websocket proxy
is needed.

The remote server's 101 response carries
`Sec-WebSocket-Protocol: ocode.bearer.<remoteToken>`
(`terminalUpgradeRespHeader`). Forwarding it unchanged both breaks the
handshake (the browser did not offer that protocol) and **leaks the remote
token to the browser**, which the remote token model forbids. The proxy's
`ModifyResponse` therefore restores the header to exactly what the browser
offered, and deletes it when the browser offered nothing. The browser's
offered value is stashed on the request context by the Director.

### Detach TTL

`terminalDetachTTL` is a constant of 30 min (`terminal_session_table.go`).
In `--remote` mode it becomes **24 h**. A laptop asleep overnight is the
point of this feature. Local mode keeps 30 min.

### Reattach

The SPA already persists terminal ids per project in localStorage
(`terminalPersistence.ts`) and reattaches by id with history replay. That
works because the id is now looked up on the remote server. One fix is
needed: the persistence key is the project path alone, so a local project
and a remote project at the same path would share tabs and reattach ids.
The key becomes `<host>|<path>` for remote projects; local projects keep the
bare path so existing entries still load. When localStorage is gone (new
machine, cleared storage), the sidebar list in Section 4 offers the same ids
for reattach.

## Section 2: Remote server version policy and restart

### Reuse on mismatch

`EnsureRemoteServer` changes: a discovered server that is **alive and
healthy but version-mismatched is reused**, not replaced. `ServeState` gains
`Outdated bool` (remote version differs from local). The connect progress
line reports `reusing remote vX (local vY)`. The `staleVersionPID` return
value and its operator notice are removed; nothing is orphaned any more.

A dead or unhealthy server is still replaced with a fresh one, as today.

### Status endpoint (local server, not proxied)

```
GET /api/remote/{host}/status
→ { "host", "connected": bool, "version", "local_version",
    "outdated": bool, "pid" }
```

`connected=false` with no version when the host entry has never connected or
was dropped. The handler does not connect; it reports what the registry
knows. Connecting from a status poll would ssh to every remote project on
sidebar mount, including hosts that are down or prompt for a passphrase.
Instead the row offers an explicit **Connect** action:

```
POST /api/remote/{host}/connect
→ same body as status
```

which runs `workspaceForPort` (discover or start the server, open the tunnel,
register saved projects) and returns the resulting status. Opening a chat or
terminal on the host connects implicitly as today.

Same trust boundary as the proxy: `{host}` must belong to a saved project.

### Restart endpoint (local server, not proxied)

```
POST /api/remote/{host}/restart
→ same body as status, after the new server is up
```

Steps, in order, on the host's transport:

1. `kill <pid>` of the discovered server, then wait until the pid is gone
   (bounded, a few seconds, then `kill -9`).
2. Drop the host entry in `remoteHostRegistry` (closes tunnel and proxy).
3. `workspaceForPort` again, which runs `EnsureRemoteServer` and starts a
   fresh server at the local version.
4. Re-register every saved project on that host.

No guard on running agent turns or open terminals (user decision). The
event bus and terminal websockets on the SPA reconnect with their existing
backoff; terminal tabs whose shell died show the existing "shell exited"
state and can be reopened from the tab.

Errors at any step return 502 `{error, stage: "remote-restart"}` and leave
the registry entry dropped so the next request reconnects.

## Section 3: Inventory endpoints

### Terminal list (any server, proxied for remote)

```
GET /api/terminal?project_path=…
→ { "terminals": [ { "id", "title", "pid", "started_at", "attached": bool } ] }
```

Sorted by `started_at` ascending. Lists only live sessions from the terminal
session table for that project. Anonymous (empty id) sessions are excluded
because they cannot be reattached. Small enough that pagination is not
needed; a `// unpaginated: bounded by live pty count` comment says so.

### Chat sessions

No new server code. The SPA uses the already-proxied `GET /api/sessions`
(list) and the runs stream (`useAgentRuns`) for the running badge.

## Section 4: Sidebar UI and wake handling

### Remote project row

Below the existing `host:path` line, a status line:

```
v1.2.3 · 2 chats (1 running) · 3 terminals
```

- `outdated=true`: amber dot before the version and an inline **Restart**
  action, plus a "Restart remote server" item in the row's context menu.
- `connected=false`: the line reads `not connected` with a **Connect**
  action and no counts.
- Restart in progress: line reads `restarting…`, action disabled.

Clicking the status line expands the row:

- **Chats**: session title, running badge, click opens the session as a tab
  (existing open-session action). Already-open sessions are marked.
- **Terminals**: title (OSC title or shell name), click attaches it as a
  terminal tab by id using the existing reattach. Already-open ids are
  marked. A small kill icon calls the proxied DELETE.

### Hooks

- `useRemoteHostStatus(host)`: fetches status on mount, every 30 s while the
  row is visible, and on every event bus `onReconnect`.
- `useRemoteTerminals(host, path)`: fetches the terminal list when the row
  is expanded and again on event bus reconnect.

### Wake handling

The event bus and the terminal panel both reconnect with exponential
backoff. Add one shared trigger: on `window` `online` and on
`visibilitychange` to visible, reset the backoff and reconnect immediately.
Without this a wake can wait up to 30 s before the first attempt.

## Out of scope

- Surviving a remote host reboot or a remote `ocode serve` crash (would need
  tmux; rejected).
- Local project terminals.
- The TUI (`ocode remote` without `--web`), which already uses tmux/screen.
- Auto-restart of an outdated remote server. Restart is always explicit.

## Testing

Go:

- `internal/remote`: `InjectAuth` on an Upgrade request strips `?token=`
  and any `ocode.bearer.*` subprotocol and appends the remote one;
  `ModifyResponse` restores the browser's offered subprotocol and deletes
  the header when none was offered, so the remote token never appears in a
  response to the browser. `EnsureRemoteServer` reuses an alive mismatched
  server and sets `Outdated`; still replaces a dead one.
- `internal/server`: status endpoint for unknown host is 403, for a
  never-connected host reports `connected=false`; connect endpoint brings a
  never-connected host to `connected=true`; restart with the fake transport
  kills the old pid, starts a new server, re-registers projects, and returns
  the new state; terminal list endpoint returns only live named sessions for
  the project, sorted; `--remote` mode uses the 24 h TTL.

Vitest:

- Terminal panel builds the proxied websocket URL for a host project with
  no `host=` param and the `X-Ocode-Project` header on HTTP calls.
- Terminal persistence keys a remote project by `<host>|<path>` and a local
  project by bare path, and a pre-existing bare-path entry still loads.
- Sidebar row renders version, counts, outdated marker, and calls restart.
- Status hook refetches on event bus reconnect.
- Wake trigger resets backoff on `online`.

Manual:

1. Open a remote project terminal, run `top`.
2. Sleep the laptop for a few minutes, wake. The terminal reconnects and
   `top` is still running.
3. Quit and relaunch the desktop app. The terminal tab restores and attaches.
4. Install a newer local build. Sidebar shows the amber outdated marker.
   Click Restart. Row shows the new version; chats reopen and resume.
