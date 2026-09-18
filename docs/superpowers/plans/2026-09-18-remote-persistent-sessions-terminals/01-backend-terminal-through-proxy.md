# Part 1: Backend — Terminal Through the Proxy

**Spec:** `docs/superpowers/specs/2026-09-18-remote-persistent-sessions-terminals-design.md`, Section 1 and Section 3.

**Context for this part.** A remote project's chat is already reverse-proxied to `ocode serve --remote` on the host through `/api/remote/{host}/api/{rest...}` (`internal/server/handler_remote_proxy.go`, proxy built by `remote.NewAPIProxy` in `internal/remote/proxy.go`). Terminals are not: the local server starts the pty and spawns ssh into the host. This part makes the proxy able to carry the terminal websocket, lengthens the detach TTL on the host, and adds a list endpoint so the SPA can enumerate live terminals. Nothing in this part changes how a local project's terminal works.

Constraints that apply to every task here: the remote token never reaches the browser; every caught error is logged; TDD; surgical diffs; `go test ./internal/remote/... ./internal/server/...` green before each commit.

---

### Task 1: Websocket subprotocol rewrite in the proxy

**Files:**
- Modify: `internal/remote/proxy.go` (`NewAPIProxy`, `InjectAuth`)
- Test: `internal/remote/proxy_test.go`

**Why.** The remote server is in `--remote` mode: it rejects `?token=` and reads the websocket token from the `Sec-WebSocket-Protocol` list as `ocode.bearer.<token>` (`remoteWSToken` and `remoteWSProtocolPrefix` in `internal/server/server.go`). Its 101 response echoes `ocode.bearer.<remoteToken>` (`terminalUpgradeRespHeader` in `internal/server/handler_terminal.go`). Forwarded unchanged that breaks the browser handshake and leaks the remote token.

**Interfaces produced.**
- `InjectAuth(req, remoteToken)`: unchanged signature. New behaviour: when the request is an Upgrade, remove every `ocode.bearer.*` entry from the offered `Sec-WebSocket-Protocol` list and append `ocode.bearer.<remoteToken>`. Non-upgrade behaviour unchanged.
- `NewAPIProxy` gains a `ModifyResponse` that restores the browser's offered `Sec-WebSocket-Protocol` on a 101 response, or deletes the header when the browser offered none. The browser's offered value is stashed on the request context by the Director before `InjectAuth` runs.
- Export the `ocode.bearer.` prefix constant from `internal/remote` so both packages use one value; `internal/server` keeps its own constant name but reads the value from `internal/remote` (one-line change in `server.go`).

- [ ] **Step 1: Write failing tests** in `proxy_test.go`, following the style of `TestInjectAuth_StripsTokenAndSetsBearer`:
  - `TestInjectAuth_UpgradeReplacesBearerSubprotocol`: request with `Connection: Upgrade`, `Upgrade: websocket`, offered list `ocode.bearer.local, other`; after `InjectAuth` the list contains `other` and `ocode.bearer.remote`, not `ocode.bearer.local`.
  - `TestInjectAuth_UpgradeNoOfferedProtocols`: offered list empty; after `InjectAuth` the list is exactly `ocode.bearer.remote`.
  - `TestInjectAuth_NonUpgradeLeavesSubprotocolAlone`.
  - `TestNewAPIProxy_Upgrade101RestoresBrowserSubprotocol`: an `httptest` backend that responds 101 with `Sec-WebSocket-Protocol: ocode.bearer.remote`; the browser request offered `other`; the proxied response carries `other`, never the remote token.
  - `TestNewAPIProxy_Upgrade101DeletesSubprotocolWhenNoneOffered`: same backend, browser offered nothing; header absent in the proxied response.
- [ ] **Step 2: Run** `go test ./internal/remote/ -run 'InjectAuth|Upgrade' -v`. Expected: FAIL.
- [ ] **Step 3: Implement** in `proxy.go`: a helper that detects an Upgrade request; the subprotocol list rewrite inside `InjectAuth`; a context key set in the Director holding the browser's offered header; a `ModifyResponse` on the proxy that acts only on status 101.
- [ ] **Step 4: Run** the same tests plus the whole package. Expected: PASS.
- [ ] **Step 5: Commit** `feat(remote): proxy rewrites websocket subprotocol token both ways`.

---

### Task 2: 24 h terminal detach TTL in `--remote` mode

**Files:**
- Modify: `internal/server/terminal_session_table.go` (`terminalDetachTTL`, `newTerminalSessionTable`)
- Modify: `internal/server/server.go` (`SetRemoteMode`)
- Modify: `internal/server/handler.go` (a setter on `Handler` that updates `terminalSessions.detachTTL`)
- Test: `internal/server/terminal_session_table_test.go` (create if absent) or the existing terminal handler test file

**Why.** `terminalDetachTTL` is a 30 min constant. The laptop can be asleep overnight; the host must keep the detached shell for 24 h.

**Interfaces produced.**
- Two constants: `terminalDetachTTL` (30 min, local) and `terminalDetachTTLRemote` (24 h).
- `Handler.SetTerminalDetachTTL(d time.Duration)` called from `Server.SetRemoteMode(true)`.

- [ ] **Step 1: Write failing test**: a `Server` constructed as in existing server tests, `SetRemoteMode(true)`, assert `s.handler.terminalSessions.detachTTL == terminalDetachTTLRemote`; and a second assertion that a fresh handler defaults to 30 min.
- [ ] **Step 2: Run** `go test ./internal/server/ -run DetachTTL -v`. Expected: FAIL.
- [ ] **Step 3: Implement** the constant, the handler setter, and the call from `SetRemoteMode`. The setter takes the table mutex.
- [ ] **Step 4: Run** the package tests. Expected: PASS.
- [ ] **Step 5: Commit** `feat(server): 24h terminal detach TTL in --remote mode`.

---

### Task 3: `GET /api/terminal` live terminal list

**Files:**
- Modify: `internal/server/terminal_session.go` (add `startedAt time.Time` and `title string` fields; set `startedAt` in `newTerminalSession`; a `snapshot()` accessor under the session mutex)
- Modify: `internal/server/terminal_session_table.go` (`listForProject(project string) []terminalListEntry`, sorted by `startedAt` ascending, excluding empty ids)
- Modify: `internal/server/handler_terminal.go` (`HandleTerminalList`, project resolved with `resolveTerminalHistoryProject`, same access gate as `HandleTerminalProcesses`)
- Modify: `internal/server/handler_terminal_windows.go` (stub returning 501, matching its neighbours)
- Modify: `internal/server/server.go` (route `GET /api/terminal` next to the other terminal routes, wrapped like `handleTerminalProcesses`)
- Test: `internal/server/handler_terminal_test.go` (or the existing terminal handler test file)

**Why.** The SPA needs to enumerate live terminals on the host to offer reattach when localStorage no longer knows them. `HandleTerminalProcesses` lists processes, not sessions.

**Interfaces produced.**
- Response body `{ "terminals": [ { "id", "title", "pid", "started_at" (RFC3339), "attached": bool } ] }`, sorted by `started_at` ascending. `attached` is true when a websocket is currently bound. Anonymous sessions (empty id) are excluded. A comment on the handler reads `// unpaginated: bounded by live pty count`.
- `title` is the session's last OSC title if the server ever learns one, else empty. This task does not add OSC parsing; the field exists so the SPA can fill it from its own persisted title.

- [ ] **Step 1: Write failing tests**: seed the table with three sessions for two projects (one with empty id) using the existing test helpers for `terminalSession`; call the handler for one project; assert only that project's named sessions are returned, in start order, with `attached` false; assert 403 when the project is not registered, mirroring the history handler tests.
- [ ] **Step 2: Run** `go test ./internal/server/ -run TerminalList -v`. Expected: FAIL.
- [ ] **Step 3: Implement** the fields, the table method, the handler, the Windows stub, and the route.
- [ ] **Step 4: Run** `go test ./internal/server/...`. Expected: PASS, including existing terminal tests.
- [ ] **Step 5: Commit** `feat(server): GET /api/terminal lists live terminal sessions per project`.
