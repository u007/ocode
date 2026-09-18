# Part 4: Frontend — Sidebar Inventory, Connect, Restart, Wake

**Spec:** `docs/superpowers/specs/2026-09-18-remote-persistent-sessions-terminals-design.md`, Sections 3 and 4.

**Context for this part.** The project sidebar is `web/src/components/Layout/ProjectSidebar.tsx`; each project is a `SortableProjectRow` that already shows `host:path` for remote projects and builds a context menu (`contextItems`) with an "Edit connection" item for hosts. `useProjectIndicators(projectPath)` computes chat badges from the project store and chat store. Sessions per project are cached in `projectStore.tsx` (`sessionsByProject`, keyed by `projectSessionKey(path, host)`, filled by `prefetchProjectSessions(project)`); `openSessionTab(sessionId, title)` opens one. Running state per session comes from the `runs` event on `eventBus` (`web/src/lib/eventBus.ts`), which holds one connection per host declared via `setHosts` and fires `onReconnect` handlers after a reconnect with exponential backoff (`RECONNECT_BASE_MS` 1 s to `RECONNECT_MAX_MS` 30 s). The terminal panel (`TerminalPanel.tsx`) has its own backoff using `reconnectAttemptRef` and `reconnectTimerRef`. Terminal tabs are opened with `openTerminal` from `web/src/stores/terminalStore.tsx`, which mints a new id; the store keys tabs by `projectSessionKey(path, host)`. `authedFetch` and `remoteApiBase(host)` live in `web/src/api/client.ts`.

Backend endpoints this part consumes (all on the local server, `{host}` URI-encoded):

- `GET /api/remote/{host}/status` → `{ host, connected, version, local_version, outdated, pid }`. Never connects.
- `POST /api/remote/{host}/connect` → same body after connecting; 502 `{error, stage}` on failure.
- `POST /api/remote/{host}/restart` → same body after a fresh server is up; 502 `{error, stage}` on failure. Unguarded: kills running turns and terminals.
- `GET /api/remote/{host}/api/terminal?project_path=…` with `X-Ocode-Project: <path>` → `{ terminals: [ { id, title, pid, started_at, attached } ] }`, sorted by `started_at`.
- `GET /api/remote/{host}/api/sessions` (already used by the session cache).

Constraints: use existing UI primitives (`Button`, context menu, dialog components under `web/src/components/ui`); no new top-level panel; every catch logs via the existing logger pattern in the file; vitest for each hook and component; surgical diffs.

---

### Task 9: API client functions and hooks

**Files:**
- Modify: `web/src/api/client.ts` (`getRemoteHostStatus(host)`, `connectRemoteHost(host)`, `restartRemoteHost(host)`, `listRemoteTerminals(host, projectPath)`)
- Modify: `web/src/api/types.ts` (`RemoteHostStatus`, `RemoteTerminalEntry`)
- Create: `web/src/hooks/useRemoteHostStatus.ts`
- Create: `web/src/hooks/useRemoteTerminals.ts`
- Test: `web/src/api/client.remote.test.ts` (create), `web/src/hooks/useRemoteHostStatus.test.tsx`, `web/src/hooks/useRemoteTerminals.test.tsx`

**Interfaces produced.**
- `RemoteHostStatus { host: string; connected: boolean; version: string; local_version: string; outdated: boolean; pid: number }`.
- `RemoteTerminalEntry { id: string; title: string; pid: number; started_at: string; attached: boolean }`.
- `getRemoteHostStatus(host): Promise<RemoteHostStatus>`; `connectRemoteHost(host)` and `restartRemoteHost(host)` POST and return the status; a non-2xx response throws an `Error` whose message includes the body's `error` and `stage`.
- `listRemoteTerminals(host, projectPath): Promise<RemoteTerminalEntry[]>` calls the proxied path with the `X-Ocode-Project` header.
- `useRemoteHostStatus(host: string | undefined, enabled: boolean)` returns `{ status, loading, error, refresh, connect, restart, busy: "idle" | "connecting" | "restarting" }`. Fetches on mount when enabled, every 30 s while enabled, and on every `eventBus.onReconnect`. `connect` and `restart` set `busy`, call the API, and store the returned status; on error they store the error message and refresh.
- `useRemoteTerminals(host, projectPath, enabled)` returns `{ terminals, loading, error, refresh }`. Fetches when enabled becomes true and on `eventBus.onReconnect`.

- [ ] **Step 1: Write failing tests**: client functions hit the exact URLs with the right method and header and throw with `stage` in the message on 502; the status hook fetches on mount, refetches on a simulated `onReconnect`, and `restart` transitions `busy` through `restarting` back to `idle` with the new status; the terminals hook does not fetch while disabled and fetches once enabled. Use the `authedFetch` mocking pattern already present in `web/src/hooks/useSessionStatus.test.tsx`.
- [ ] **Step 2: Run** the three test files. Expected: FAIL.
- [ ] **Step 3: Implement** types, client functions, and hooks.
- [ ] **Step 4: Run** `cd web && pnpm test -- remote`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(web): remote host status/terminal hooks and client`.

---

### Task 10: Sidebar row status line, expand list, Connect and Restart

**Files:**
- Create: `web/src/components/Layout/RemoteProjectStatus.tsx` (the status line and the expanded inventory for one remote project)
- Modify: `web/src/components/Layout/ProjectSidebar.tsx` (`SortableProjectRow` renders `RemoteProjectStatus` under the `host:path` line when `project.host` is set; `contextItems` gains "Restart remote server" for hosts)
- Modify: `web/src/stores/terminalStore.tsx` (`attachTerminal(projectPath, host, id, title)` opens a tab with an existing id instead of minting one; no-op if the id is already open)
- Test: `web/src/components/Layout/RemoteProjectStatus.test.tsx`, `web/src/components/Layout/ProjectSidebar.test.tsx`, `web/src/stores/terminalStore.test.tsx` (create if absent)

**Interfaces produced.**
- `RemoteProjectStatus({ project })` uses `useRemoteHostStatus(project.host, true)` and, when expanded, `useRemoteTerminals(project.host, project.path, expanded)` plus the project's cached session list (`prefetchProjectSessions(project)` on expand, sessions read from `sessionsByProject[projectSessionKey(path, host)]`) and running state from the `runs` bus events for that host.
- Collapsed line text, in order: version (amber dot before it when `outdated`), `N chats (R running)`, `M terminals`. When `connected` is false the line reads `not connected` and shows a **Connect** button. When `outdated` it shows an inline **Restart** button. While `busy` is `connecting` or `restarting` the line reads `connecting…` / `restarting…` and both buttons are disabled.
- Clicking the line toggles expansion. Expanded: a **Chats** list (title, running badge, click → `openSessionTab`, already-open sessions marked) and a **Terminals** list (title or shell name fallback, click → `attachTerminal`, already-open ids marked, a small kill icon that DELETEs via the proxied URL then refreshes).
- Context menu item "Restart remote server" calls the same `restart` as the inline button.
- `attachTerminal` reuses the tab shape `openTerminal` builds and persists it through the same save path so a reload restores it.

- [ ] **Step 1: Write failing tests**: renders `not connected` + Connect when status is disconnected; renders `v1.2.3 · 2 chats (1 running) · 3 terminals` for a connected status with mocked hooks; amber marker and Restart appear only when `outdated`; clicking Restart calls the hook's `restart`; expanding lists chats and terminals and clicking a terminal calls `attachTerminal` with the id; `attachTerminal` in the store adds a tab with the given id once and ignores a second call; `ProjectSidebar` shows the status component only for host projects and its context menu contains the restart item only for hosts.
- [ ] **Step 2: Run** the test files. Expected: FAIL.
- [ ] **Step 3: Implement** the component, the store action, and the sidebar wiring using existing `ui` primitives.
- [ ] **Step 4: Run** `cd web && pnpm test -- Layout terminalStore`. Expected: PASS, including existing `ProjectSidebar.test.tsx` cases.
- [ ] **Step 5: Commit** `feat(web): sidebar shows remote server version, chats, terminals; connect and restart`.

---

### Task 11: Immediate reconnect on wake

**Files:**
- Create: `web/src/lib/wakeSignal.ts` (`onWake(handler): () => void` fires on `window` `online` and on `visibilitychange` to visible, deduplicated within one second)
- Modify: `web/src/lib/eventBus.ts` (each host connection subscribes to `onWake`: clear the pending reconnect timer, reset `reconnectDelay` to `RECONNECT_BASE_MS`, reconnect now if not open)
- Modify: `web/src/components/Terminal/TerminalPanel.tsx` (subscribe to `onWake` in the socket lifecycle effect: clear `reconnectTimerRef`, reset `reconnectAttemptRef`, and call the connect function when the socket is not open)
- Test: `web/src/lib/wakeSignal.test.ts`, `web/src/lib/eventBus.test.ts`, a new `web/src/components/Terminal/TerminalPanel.wake.test.tsx`

**Why.** Backoff can reach 30 s. After sleep the user expects the chat stream and terminals to be back within a second of the network returning.

- [ ] **Step 1: Write failing tests**: `onWake` fires once for an `online` event and once for a visibility change, and not twice within a second; the bus, after a simulated drop with backoff at its max, reconnects immediately on wake; the terminal panel, with a closed socket and a pending timer, opens a new socket on wake and resets its attempt counter.
- [ ] **Step 2: Run** the test files. Expected: FAIL.
- [ ] **Step 3: Implement** the helper and the two subscriptions, unsubscribing on teardown.
- [ ] **Step 4: Run** `cd web && pnpm test -- eventBus wake Terminal`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(web): reconnect event bus and terminals immediately on wake`.
