# Part 03 — Frontend: Per-Session Host Routing

Self-contained. Spec: `docs/superpowers/specs/2026-09-17-remote-project-agent-on-host-design.md`.

Constraints that apply to every task here: TDD with vitest (`cd web && pnpm test`), no code in this plan, no fallbacks, log caught errors, surgical diffs, match existing style, do not commit unless the user asks.

Backend contract this part relies on (already implemented on the local server): every route under `/api/remote/{host}/api/*` is forwarded to the `ocode serve --remote` process on `host`, where `{host}` is the saved project's `host` string percent-encoded once. Any prefixed request may carry an `X-Ocode-Project: <project path>` header; the server uses it to register the project on the remote the first time. Connect failures come back as HTTP 502 with a JSON body `{error, stage: "remote-connect"}`.

Trust rule reused throughout: `getTrustedTerminalProject(projects, path)` in `web/src/lib/trustedProject.ts` returns `{known: true, host?}` only when exactly one saved project has that path. Ambiguous or unknown paths yield no host and therefore stay local, exactly as `!` commands do today in `web/src/hooks/useChat.ts` (lines 48–60).

---

### Task 1: `remoteApiBase` and host-aware fetch/SSE helpers in the API client

**Files:**
- Modify: `web/src/api/client.ts` — new exported `remoteApiBase(host?: string): string`; `fetchJSON(path, init?, host?)` and `readSSEStream(...)`/the chat stream builder accept an optional `host` and prefix `path` with `remoteApiBase(host)` before `apiPath`; when a host is given and a `projectPath` is known, set `X-Ocode-Project`.
- Modify: `web/src/api/client.ts` — session-scoped members of `api` gain a trailing optional `host` argument and pass it through: `getSession`, `truncateSession`, `setSessionModel`, `clearSessionModel`, `getSessionContext`, `getSessionState`, `getSessionStatus`, `sendMessage`, `chat` (which already takes `projectPath`), `compactSession`, `recapSession`, `shareSession`, `btwSession`, `setSessionTitle`, `generateSessionTitle`, `exportSession`, `cancelSession`, `closeSession`, `resolvePermission`, `answerQuestion`, `listAgentRuns`, `listModels`, `listProjectSessions` (which already takes `host`; it now uses the prefix instead of the local `host=` query param).
- Test: `web/src/api/client.remoteBase.test.ts` (new), alongside `client.basePath.test.ts`.

**Interfaces:**
- Produces: `remoteApiBase(host?)`; every listed `api.*` member accepts `host?: string` last. Tasks 2–4 consume these.

- [ ] Write failing tests: `remoteApiBase()` is `""`; `remoteApiBase("user@host")` is `/api/remote/user%40host`; `remoteApiBase("wsl:Ubuntu")` encodes the colon; `fetchJSON("/api/sessions/x", undefined, "h")` fetches `/api/remote/h/api/sessions/x` and, when a project path is supplied, sends `X-Ocode-Project`; without a host the URL is byte-identical to today; `api.chat(..., projectPath, host)` opens the stream under the prefix.
- [ ] Run; confirm failure.
- [ ] Implement in `client.ts`; keep the `backendBase` handling in `apiPath` untouched (the prefix is applied to the path before `apiPath`, so desktop backend-origin switching still works).
- [ ] Run `pnpm test -- client`; all client test files green, including the existing `client.basePath`, `client.sse`, `client.token` suites.
- [ ] Ready to commit: `feat(web): host-prefixed API base for remote project sessions`.

---

### Task 2: `useChat` resolves the host per session and passes it on every call

**Files:**
- Modify: `web/src/hooks/useChat.ts` — the existing `projectPath`/`projectHost` derivation (lines 48–60) already binds through `findProjectPathForTab(projectState, sessionId)`; extend the memoised `projectHost` to every `api.*` call in the hook (`chat`, `sendMessage`, `cancelSession`, `getSessionState`, `resolvePermission`, `answerQuestion`). The `useCallback` dependency lists gain `projectHost`.
- Modify: `web/src/components/Chat/commands.ts` and `web/src/components/Layout/UnifiedTabBar.tsx` — slash commands and tab actions that call `compactSession`, `recapSession`, `shareSession`, `btwSession`, `setSessionTitle`, `truncateSession`, `closeSession`, `exportSession` receive the session's host from the same rule (a small `useSessionHost(sessionId)` hook in `web/src/hooks/useSessionHost.ts` wrapping `findProjectPathForTab` + `getTrustedTerminalProject`).
- Create: `web/src/hooks/useSessionHost.ts`.
- Test: `web/src/hooks/useChat.remoteHost.test.tsx` (new; copy the harness from `useChat.shellHost.test.tsx`), `web/src/hooks/useSessionHost.test.tsx` (new).

**Interfaces:**
- Consumes: `api.*(…, host?)` from Task 1; `findProjectPathForTab`, `getTrustedTerminalProject`.
- Produces: `useSessionHost(sessionId?: string): string | undefined`. Tasks 3–4 consume it.

- [ ] Write failing tests: a tab bound to `/r` where `/r` is saved as host `devbox` sends `api.chat` and `api.sendMessage` with host `devbox`; after the active project switches to a local project the same tab still sends `devbox`; a second tab bound to a local path sends no host; an ambiguous path (saved both local and remote) sends no host; `useSessionHost` returns `undefined` for an unknown tab.
- [ ] Run; confirm failure.
- [ ] Implement `useSessionHost`, thread the host through `useChat` and the two command call sites.
- [ ] Run `pnpm test -- useChat useSessionHost commands`; green, including `useChat.rerender.test.tsx` (the host must not add renders per streamed token: memoise it).
- [ ] Ready to commit: `feat(web): route each chat session to its project's host`.

---

### Task 3: Event bus keeps one stream per host with open tabs

**Files:**
- Modify: `web/src/lib/eventBus.ts` — `EventBus` gains `setHosts(hosts: string[])` alongside `setProjects`; internally one connection per entry in `[""] ∪ hosts` (empty string = local), each built with `apiPath(remoteApiBase(host) + "/api/events?…")`, each with its own reconnect timer, liveness timer, and last-seq; frames from all connections dispatch through the existing subscriber path unchanged; `stop()` and `restart()` cover every connection.
- Modify: `web/src/App.tsx` (where `eventBus.setProjects` is called) — compute the set of hosts for all open tabs via `findProjectPathForTab` + `getTrustedTerminalProject` over `tabsByProject`, and call `setHosts` whenever that set changes. A host leaves the set when its last tab closes, not when the active project changes.
- Test: `web/src/lib/eventBus.test.ts` (extend), `web/src/App.hosts.test.tsx` (new, minimal render asserting `setHosts` calls).

**Interfaces:**
- Consumes: `remoteApiBase` from Task 1.
- Produces: `eventBus.setHosts(hosts: string[])`.

- [ ] Write failing tests: `setHosts(["a"])` opens a second fetch to `/api/remote/a/api/events`; `setHosts(["a","b"])` holds three fetches; `setHosts([])` aborts the remote ones and keeps local; a frame with `session_id: s1` from host `a` reaches a subscriber for `s1`; reconnect back-off is independent per host (aborting `a` does not reset local); `stop()` aborts everything.
- [ ] Run; confirm failure.
- [ ] Implement per-connection state as a small internal class instantiated per host; keep the public subscribe API unchanged.
- [ ] Wire `App.tsx`; run `pnpm test -- eventBus App`; green.
- [ ] Ready to commit: `feat(web): per-host event streams for remote project sessions`.

---

### Task 4: Session lists, header, and agent-run views use the session's host

**Files:**
- Modify: `web/src/stores/projectStore.tsx` — the session-list fetch (around line 663, `api.listProjectSessions(project.path, project.host)`) already passes `host`; it now goes through the prefixed base (Task 1 changed the helper), and the cache key `projectSessionKey(path, host)` is unchanged. Resuming a session from the list must open the tab under that project so `findProjectPathForTab` binds it (verify the existing open-tab action already does this; if not, pass the project into the open action).
- Modify: `web/src/components/Layout/ModelDialog.tsx` and the session header that calls `getSessionContext`/`setSessionModel`/`listModels` — pass `useSessionHost(sessionId)` so a remote session's model list and context come from the remote server.
- Modify: `web/src/hooks/useAgentRuns.ts` — `listAgentRuns(session, host)` and its stream use the session's host.
- Test: `web/src/stores/projectStore.test.tsx` (extend), `web/src/hooks/useAgentRuns.test.ts` (extend or new).

**Interfaces:**
- Consumes: `useSessionHost`, `api.*(…, host?)`.

- [ ] Write failing tests: session list for a remote project is fetched from the prefixed URL and cached under `host::path`; a resumed remote session's tab resolves back to the same host via `useSessionHost`; `useAgentRuns` for a remote session hits the prefixed runs endpoint and stream.
- [ ] Run; confirm failure.
- [ ] Implement the call-site changes.
- [ ] Run the full `pnpm test`; green. Then `pnpm build` to confirm types.
- [ ] Ready to commit: `feat(web): remote session lists, model dialog, and agent runs follow the session host`.
