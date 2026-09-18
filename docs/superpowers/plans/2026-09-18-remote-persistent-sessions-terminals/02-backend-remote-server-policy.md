# Part 2: Backend — Remote Server Version Policy, Status, Connect, Restart

**Spec:** `docs/superpowers/specs/2026-09-18-remote-persistent-sessions-terminals-design.md`, Section 2.

**Context for this part.** `internal/remote/serve.go` owns server discovery on a host: `DiscoverServer` reads the state file, `ServerAlive` checks pid, version, and health, `StartFreshServer` launches `nohup ocode serve --remote … & disown`, and `EnsureRemoteServer` decides reuse vs fresh. `RemoteWorkspace.discoverOrStartServer` in `internal/remote/workspace.go` is a second copy of that decision used by the local server's host registry. The registry is `remoteHostRegistry` in `internal/server/remote_hosts.go`: one `remoteHostEntry` per host with a `remoteHostWorkspace` (interface: `APIURL`, `Token`, `Disconnect`) and a cached proxy; `workspaceForPort` connects lazily, `drop` tears an entry down, `markRegistered` / `isRegistered` track which project paths were POSTed to the host. `ServeState` (`serve.go`) has `PID`, `Port`, `BrowsePort`, `Token`, `Version`, `StartedAt`.

Today a discovered server with a different version is abandoned and a fresh one is started, orphaning its terminals and turns. This part reverses that policy and gives the SPA endpoints to see and act on it.

Constraints: only saved-project hosts; remote commands go through the host `Transport`; no fallbacks; every error logged; TDD; `go test ./internal/remote/... ./internal/server/...` green before each commit.

---

### Task 4: Reuse a version-mismatched server and record `Outdated`

**Files:**
- Modify: `internal/remote/serve.go` (`ServeState`, `ServerAlive`, `EnsureRemoteServer`)
- Modify: `internal/remote/workspace.go` (`discoverOrStartServer`)
- Modify: `internal/remote/connect.go` (call site of `EnsureRemoteServer` around line 217; progress wording)
- Test: `internal/remote/serve_test.go`, `internal/remote/workspace_test.go`

**Interfaces produced.**
- `ServeState.Outdated bool` with a `json:"-"` tag (derived at discovery time, never persisted to the state file).
- `ServerAlive(t, state, localVersion)` no longer compares versions. A new `serverHealthy(t, state) bool` holds the pid and health probe; `ServerAlive` stays as a thin wrapper for existing callers so nothing else changes.
- `EnsureRemoteServer(t, ver) (state ServeState, reused bool, err error)`: three return values. Alive and healthy → reused, `Outdated = state.Version != ver`. Missing, dead, or unhealthy → fresh, `Outdated = false`. The `staleVersionPID` return and its operator warning in `connect.go` are removed.
- `RemoteWorkspace.discoverOrStartServer` delegates to `EnsureRemoteServer` so the two decision copies collapse into one.
- Progress line in `connect.go`: `reusing existing server (v<remote>, local v<local>, outdated)` when outdated, else the current wording.

- [x] **Step 1: Write failing tests** with `newFakeTransport` from `fake_transport_test.go` (it maps command strings to canned results; look at `TestEnsureRemoteServer*` for how discovery, pid, and health probes are scripted):
  - `TestEnsureRemoteServer_ReusesMismatchedAliveServer`: state file with version `1.0.0`, pid alive, health OK, local version `2.0.0` → reused true, `Outdated` true, no launch command executed.
  - `TestEnsureRemoteServer_ReplacesDeadMismatchedServer`: pid dead → fresh, `Outdated` false.
  - `TestEnsureRemoteServer_MatchingVersionNotOutdated`.
  - In `workspace_test.go`: `discoverOrStartServer` returns `Outdated` true for the mismatched-alive case.
- [x] **Step 2: Run** `go test ./internal/remote/ -run 'EnsureRemoteServer|discoverOrStart' -v`. Expected: FAIL (compile error on the three-value signature is acceptable as the failure).
- [x] **Step 3: Implement** the field, the health split, the new decision, the delegation, and the `connect.go` call site. Update any other test that pattern-matched the four-value signature.
- [x] **Step 4: Run** `go test ./internal/remote/...`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(remote): reuse version-mismatched remote server and flag it outdated`.

---

### Task 5: Registry exposes server state and can kill the remote server

**Files:**
- Modify: `internal/server/remote_hosts.go` (`remoteHostWorkspace` interface, `remoteHostRegistry`)
- Modify: `internal/remote/workspace.go` (`RemoteWorkspace` methods to satisfy the widened interface)
- Modify: `internal/remote/serve.go` (`KillServer(t Transport, pid int) error`)
- Test: `internal/server/remote_hosts_test.go`, `internal/remote/serve_test.go`

**Interfaces produced.**
- `remoteHostWorkspace` gains `State() remote.ServeState` and `Transport() remote.Transport`. `RemoteWorkspace` implements both by returning its `State` and `Transport` fields. The fake workspace in `remote_hosts_test.go` implements them too.
- `remoteHostStatus` struct: `Host, Connected bool, Version, LocalVersion, Outdated bool, PID int`, JSON tags `host, connected, version, local_version, outdated, pid`.
- `remoteHostRegistry.status(host string) remoteHostStatus`: reads the entry without connecting; `Connected` false and empty version when no connected entry exists. `LocalVersion` is always `version.Version`.
- `remoteHostRegistry.restart(host, path string, port int) (remoteHostStatus, error)`: takes the connected entry's transport and pid, calls `remote.KillServer`, then `drop(host)`, then `workspaceForPort(host, path, port)`, then re-registers every saved path recorded for that host (it must snapshot `registeredPaths[host]` before `drop` clears it, and needs a callback or the project store to POST them again — reuse `ensureRemoteProject` from `handler_remote_proxy.go` for each). Returns the new status. Any failure returns an error carrying a `stage` string (`remote-kill`, `remote-connect`, `remote-register`) and leaves the entry dropped.
- `remoteHostRegistry.connect(host, path string, port int) (remoteHostStatus, error)`: `workspaceForPort` then `status`.
- `remote.KillServer(t, pid)`: `kill <pid>`, poll `kill -0` up to a bounded number of attempts with a short interval, then `kill -9` once, then one final `kill -0`; error if still alive. Also removes the state file so a later discovery cannot reuse the dead pid.

- [x] **Step 1: Write failing tests**:
  - `serve_test.go`: `TestKillServer_TermThenKill` scripts `kill -0` alive twice then dead; asserts the command sequence and state-file removal. `TestKillServer_StillAlive` returns an error.
  - `remote_hosts_test.go`, using the existing fake workspace and connect stubs from `TestWorkspaceFor_SingleConnect`: `TestStatus_NeverConnected` → `Connected` false; `TestStatus_ConnectedReportsVersionAndOutdated`; `TestRestart_KillsDropsReconnectsAndReregisters` asserts kill was invoked with the old pid, a second connect happened, and the previously registered paths were re-registered; `TestRestart_KillFailureLeavesEntryDropped`.
- [x] **Step 2: Run** `go test ./internal/remote/ -run KillServer -v && go test ./internal/server/ -run 'Status_|Restart_' -v`. Expected: FAIL.
- [x] **Step 3: Implement** the interface widening, the status struct and methods, `KillServer`, and `restart`/`connect` on the registry.
- [x] **Step 4: Run** both packages' tests. Expected: PASS.
- [ ] **Step 5: Commit** `feat(server): remote host registry status, connect, restart`.

---

### Task 6: `status`, `connect`, `restart` endpoints

**Files:**
- Create: `internal/server/handler_remote_lifecycle.go` (`HandleRemoteStatus`, `HandleRemoteConnect`, `HandleRemoteRestart` on `Handler`)
- Modify: `internal/server/server.go` (routes `GET /api/remote/{host}/status`, `POST /api/remote/{host}/connect`, `POST /api/remote/{host}/restart`, each behind `authMiddleware`, registered before the catch-all `/api/remote/{host}/api/{rest...}` route so the mux prefers the exact patterns)
- Test: `internal/server/handler_remote_lifecycle_test.go`

**Interfaces produced.**
- All three: `{host}` must be the `Host` of at least one saved project, resolved with `firstSavedProjectForHost` from `handler_remote_proxy.go`; otherwise 403 `unknown host`. That saved project's `Path` and `RemotePort` are what `connect` and `restart` pass to the registry.
- `GET status` → 200 with the `remoteHostStatus` JSON. Never connects.
- `POST connect` → 200 with status after connecting; 502 `{error, stage: "remote-connect"}` on failure.
- `POST restart` → 200 with status after restart; 502 `{error, stage: <stage from the registry error>}` on failure. Logged with host and stage.

- [x] **Step 1: Write failing tests** with the `Handler` fixture used in `handler_remote_proxy_test.go` (it injects a fake registry): unknown host → 403 on all three; status on a never-connected saved host → `connected:false`; connect → registry connect called with the saved path and port, 200 body; restart → 200 body from the registry; restart error → 502 with the registry's stage; verify the exact routes resolve (a `httptest` request against the real mux for `GET /api/remote/h/status` must not fall into the proxy).
- [x] **Step 2: Run** `go test ./internal/server/ -run RemoteLifecycle -v`. Expected: FAIL.
- [x] **Step 3: Implement** the handler file and routes.
- [x] **Step 4: Run** `go test ./internal/server/...`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(server): remote host status/connect/restart endpoints`.
