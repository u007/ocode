---
type: Concept
title: Remote Persistent Sessions and Terminals
description: Architecture of remote persistent sessions and terminals — routing, websocket auth, detach/reattach lifecycle, sidebar UI, and wake reconnect.
tags:
  - remote
  - terminal
  - websocket
  - proxy
  - sessions
  - sidebar
  - architecture
timestamp: 2026-09-18T04:28:11Z
---
## Overview

A sidebar remote (SSH/WSL) project's terminal now runs on the host inside `ocode serve --remote`, reached through the local server's reverse proxy at `/api/remote/{host}/api/...`. Previously terminal, Files, git, `!` commands, and port forwards all used per-request SSH. Now only the terminal moved to the host. Files tab, git, `!` commands, and port forwards still use per-request SSH/Wsl.exe.

## Architecture

### Routing

The SPA routes remote terminal traffic through the local server's reverse proxy. All path-bearing calls carry the header `X-Ocode-Project: <path>` so `HandleRemoteProxy` registers the project on the host on first use. No `host=`/`port=` query params are sent (the remote server would otherwise SSH from the host to itself). `~` is expanded by the host via `projects.ExpandHome`. The host runs a plain local login shell in the project directory — the same code path as a local project.

Endpoints:

- `/api/remote/{host}/api/terminal/ws?project_path=…` — websocket attach
- `/api/remote/{host}/api/terminal/{id}/history` (GET) — history replay
- `/api/remote/{host}/api/terminal/{id}` (DELETE) — kill a terminal
- `/api/remote/{host}/api/terminal/processes` — live process list
- `/api/remote/{host}/api/terminal` (GET) — inventory list

The existing local SSH/Wsl.exe PTY spawn path in `HandleTerminalWS` is unchanged and still used by legacy `ocode remote --web`.

### Websocket auth through the proxy

The remote server runs in `--remote` mode where query-string tokens are forbidden. The websocket token must arrive as the subprotocol `ocode.bearer.<token>`. The proxy (`remote.InjectAuth` in `internal/remote/proxy.go`) strips `?token=` and `Authorization`, removes any `ocode.bearer.*` entry the browser offered, and appends `ocode.bearer.<remoteToken>`. `httputil.ReverseProxy` forwards the 101 Upgrade and hijacks the connection. The remote server's 101 response carries `Sec-WebSocket-Protocol: ocode.bearer.<remoteToken>`; the proxy's `ModifyResponse` restores the header to exactly what the browser offered (and deletes it when the browser offered nothing), so the handshake succeeds and the remote token never reaches the browser. The browser's offered value is stashed on the request context by the Director.

### Detach TTL

`terminalDetachTTL` is 30 minutes locally (`internal/server/terminal_session_table.go`). In `--remote` mode it becomes `terminalDetachTTLRemote` = 24 hours. The shell survives its websocket going away for that window.

### Reattach

Terminal IDs persist in localStorage per project (`web/src/components/Terminal/terminalPersistence.ts`). The persistence key is `projectTerminalsKey(path, host)` = `<host>::<path>` for remote projects (local projects keep the bare path, so existing entries load). Reattach is by ID with history replay via `GET /api/terminal/{id}/history`. When localStorage is cleared, the sidebar inventory offers the same IDs.

### Remote server version policy

`EnsureRemoteServer` (`internal/remote/serve.go`) reuses an alive and healthy but version-mismatched server rather than replacing it, setting `ServeState.Outdated` (bool, `json:"-"`). Connect progress reports `reusing remote vX (local vY)`. A dead/unhealthy server is still replaced with a fresh one. There is no auto-restart; restart is always explicit.

### Lifecycle endpoints

Registered on the **local** server, never proxied. The trust boundary is that `{host}` must be the host of a saved project.

- `GET /api/remote/{host}/status` → `{ "host", "connected": bool, "version", "local_version", "outdated": bool, "pid" }`. Does not connect; reports what the registry knows (a status poll must not SSH to every remote project, including hosts that would prompt for a passphrase).
- `POST /api/remote/{host}/connect` → same body, after running `workspaceForPort` (discover/start server, open tunnel, register saved projects).
- `POST /api/remote/{host}/restart` → same body after the new server is up. Order: kill the discovered PID then wait until gone (bounded, then `kill -9`); drop the host registry entry (closes tunnel/proxy); `workspaceForPort` again (EnsureRemoteServer starts a fresh server at the local version); re-register every saved project on that host. No guard on running agent turns or open terminals. Errors return 502 `{error, stage: "remote-restart"}` (connect errors use `"remote-connect"`) and leave the registry entry dropped.

### Inventory endpoint

Any server, proxied for remote. `GET /api/terminal?project_path=…` → `{ "terminals": [ { "id", "title", "pid", "started_at", "attached": bool } ] }`. Lists only live named (resumable) sessions for that project; anonymous (empty-ID) sessions are excluded because they cannot be reattached. Sorted by `started_at` ascending; unpaginated — bounded by live PTY count. Implemented by `HandleTerminalList` + `terminalSessionTable.listForProject` in `internal/server/handler_terminal.go` / `terminal_session_table.go`.

### Sidebar UI

`web/src/components/Layout/RemoteProjectStatus.tsx`, mounted under the project row in `ProjectSidebar.tsx`.

Below the existing `host:path` line, a status line reads `v1.2.3 · 2 chats (1 running) · 3 terminals`. `outdated=true` → amber dot before the version, an inline Restart action, and a "Restart remote server" context-menu item. `connected=false` → line reads `not connected` with a Connect action and no counts. Restart in progress → `restarting…`, action disabled. Clicking the status line expands the row: Chats (title, running badge, click opens the session as a tab) and Terminals (title, click attaches by ID, small kill icon calls the proxied DELETE).

Hooks: `useRemoteHostStatus(host)` (fetch on mount, every 30 s while visible, and on every event-bus `onReconnect`) and `useRemoteTerminals(host, path)` (fetch when expanded and on event-bus reconnect).

### Wake reconnect

`onWake(handler)` in `web/src/lib/wakeSignal.ts` fires on `window` `online` and on `visibilitychange` to visible, deduplicated within one second. The event bus (`web/src/lib/eventBus.ts`) subscribes once and calls `restart()` on wake, which clears each connection's pending backoff timer, resets its delay to `RECONNECT_BASE_MS`, and reopens the stream. `TerminalPanel.tsx` subscribes in its socket lifecycle effect: clears `reconnectTimerRef`, resets `reconnectAttemptRef`, and calls its connect function when the socket is not open. Without this a wake can wait up to 30 seconds (max backoff) before the first retry.

## What does not survive

- A remote host reboot or a remote `ocode serve` crash would need tmux; this was rejected.
- Local project terminals are out of scope.
- The TUI (`ocode remote` without `--web`) already uses tmux/screen.
- Auto-restart of an outdated remote server is out of scope; restart is always explicit.