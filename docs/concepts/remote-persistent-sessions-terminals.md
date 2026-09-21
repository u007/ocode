---
type: Concept
title: Remote Persistent Sessions and Terminals
description: Architecture of remote persistent sessions and terminals — routing, websocket auth, detach/reattach lifecycle, sidebar UI and tab reveal, and wake reconnect.
tags:
  - remote
  - terminal
  - websocket
  - proxy
  - sessions
  - sidebar
  - architecture
timestamp: 2026-09-21T04:31:54Z
resource: web/src/components/Layout/RemoteProjectStatus.tsx, web/src/lib/tabFocus.ts, web/src/App.tsx, CHANGES.md (2026-09-21)
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

### Revealing an inventory tab (focus queue)

Opening a chat or terminal from the inventory must reveal the tab the user just picked. The action rows live in `RemoteProjectStatus.tsx`, which is mounted in the sidebar — **outside `HomeApp`'s component tree** — so they cannot set `HomeApp`'s `activeView`/`focusedKind` directly. The bridge is `web/src/lib/tabFocus.ts`, a module-level TanStack Store (no provider) exposing:

- `TabFocusRequest = { kind: "chat" | "terminal", projectPath, host?, terminalId? }`
- `tabFocusActions.request(req)` — queues a request; a later request **replaces an unconsumed one** (the user's most recent click wins)
- `tabFocusActions.clear()` — drops the pending request once applied
- `useTabFocusRequest()` — `useSelector` over the store

`RemoteProjectStatus.revealTab(...)` selects the clicked project first when it is not already active (`selectProject(project)`), then queues. Chat clicks also call `openSessionTab(s.id, title, project.path)` — the optional third `projectPath` binds the tab to the clicked project (per the `projectStore` contract, a caller resuming from a non-active project's list must thread the project through). Terminal clicks call `attachTerminal(project.path, host, t.id, title)` and pass `terminalId`. It ends by calling `onRevealTab?.()`.

`HomeApp` consumes the queue in a **passive `useEffect`** (`web/src/App.tsx`, immediately after the view-persist effect) — deliberately NOT a layout effect: the per-project view restore (`loadViewStateForProject`) is a layout effect, so a request that lands in the same commit as a project switch is applied after the restore and wins ("Sessions + chat/terminal" beats the restored view). The effect gates on **(path, host)**, not path alone: it early-returns unless BOTH `request.projectPath === activeProject.path` AND `(request.host ?? "") === (activeProject.host ?? "")`. Path is not project identity — a local and a remote project can legitimately share an absolute path — so a request for the remote `/srv` must never be applied to the local one; the producer's `isActive` check in `revealTab` compares host the same way. While the gate is unmet the request stays queued until the switch lands. On match it calls `setActiveId(projectPath, terminalId, host)` for terminal requests, then `setActiveView("sessions")` + `setFocusedKind(request.kind)`, then `tabFocusActions.clear()`.

**Mobile reachability / drawer dismissal.** Every inventory control stops propagation, so the sidebar row's `onSelect` — the only other drawer-dismiss path — never ran, and the revealed tab stayed hidden behind the off-canvas drawer. The dismiss action is threaded as an optional `onRevealTab?: () => void` on `RemoteProjectStatus`, called at the end of `revealTab(...)`; `ProjectSidebar`'s `SortableProjectRow` passes `onRevealTab={isMobile ? onToggle : undefined}`. Separately, the status *line*'s expand toggle previously did NOT stop propagation, so on mobile tapping it bubbled to the row's `onSelect`, dismissed the drawer, and made the Chats/Terminals list unusable; it now calls `e.stopPropagation()` before `setExpanded(...)`, matching the Connect/Restart buttons (and on desktop this also means clicking the status line no longer doubles as project selection). **Rule:** any control nested in a sidebar row that stops propagation must restore the drawer-dismiss / reachability behaviour itself, because `onSelect` is the only other path to it.

Failure mode this prevents: without selecting the project AND binding the tab to it, the session tab is filed under whichever project is active and `resolveSessionHost` routes the remote session through the local server (`/api/...` instead of `/api/remote/{host}/...`).

Testability hook: `<main>` in `App.tsx` carries `data-active-view` and `data-focused-kind` so tests can assert the revealed view/kind.

Tests: `web/src/App.tabFocus.test.tsx` (a chat request overrides a restored Files view; a terminal request reveals the terminal half and activates the requested id; a request naming a non-active project is not applied and stays queued; and "does not apply a request whose host differs even when the path matches" — a `host: "dev@box"` request for `/proj` stays queued against the local `/proj`), `web/src/lib/tabFocus.test.ts` (queue/replace/clear), additions in `RemoteProjectStatus.test.tsx` (binds to the clicked project, selects a non-active project, queues the right focus) and `projectStore.test.tsx` (tab bound to the non-active project, host resolves). `web/src/App.tabFocusRemote.test.tsx` is the end-to-end one: it renders the real `App` + real `ProjectSidebar` + real `RemoteProjectStatus` (only `useRemoteHostStatus`, `useRemoteTerminals`, and heavy visual children stubbed) and asserts that ONE click on a chat in a NON-active remote project's inventory selects that project, binds the tab to it (the rendered chat panel carries `data-session-id`), and overrides that project's **persisted "Files" view** with Sessions — pinning the passive-effect-beats-layout-restore ordering. Two test-relevant details it encodes: the chat inventory row's handler is `onPointerUp` while the terminal row's is `onClick`; and the sidebar rows rebuild while boot auto-select lands, so the test settles boot before clicking. The load-bearing behaviors were mutation-verified by temporary revert. Cross-reference `CHANGES.md` 2026-09-21.

### Wake reconnect

`onWake(handler)` in `web/src/lib/wakeSignal.ts` fires on `window` `online` and on `visibilitychange` to visible, deduplicated within one second. The event bus (`web/src/lib/eventBus.ts`) subscribes once and calls `restart()` on wake, which clears each connection's pending backoff timer, resets its delay to `RECONNECT_BASE_MS`, and reopens the stream. `TerminalPanel.tsx` subscribes in its socket lifecycle effect: clears `reconnectTimerRef`, resets `reconnectAttemptRef`, and calls its connect function when the socket is not open. Without this a wake can wait up to 30 seconds (max backoff) before the first retry.

## What does not survive

- A remote host reboot or a remote `ocode serve` crash would need tmux; this was rejected.
- Local project terminals are out of scope.
- The TUI (`ocode remote` without `--web`) already uses tmux/screen.
- Auto-restart of an outdated remote server is out of scope; restart is always explicit.
