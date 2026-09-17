# Part 02 — Local Server: Host Registry and Proxy Route

Self-contained. Spec: `docs/superpowers/specs/2026-09-17-remote-project-agent-on-host-design.md`.

Constraints that apply to every task here: TDD (failing test first), no code in this plan, no fallbacks, log every caught error with what was attempted, surgical diffs, run `go test ./internal/server/...` before declaring a task done, do not commit unless the user asks.

Prerequisites already present in `internal/remote` (from the foundations part): `NewAPIProxy(apiURL, remoteToken string, onError func(error)) (*httputil.ReverseProxy, error)`, `InjectAuth`, and a `RemoteWorkspace` whose `Connect` performs provision, credential sync, discover-or-start, and tunnel (or direct loopback for `wsl:`), exposing `APIURL()` and `State.Token`. `NewRemoteWorkspace(target Target, remotePath string, workspaceID string, sup *tool.ProcessSupervisor)` constructs one; `Disconnect()` tears it down.

---

### Task 1: Per-host `RemoteWorkspace` registry on `Handler`

**Why:** One `ocode serve --remote` per host, connected lazily on first use, shared by every remote project on that host, torn down at shutdown. Mirrors `portMapRegistry` in `internal/server/handler_portmaps.go` (line 34), which is the existing per-project registry pattern.

**Files:**
- Create: `internal/server/remote_hosts.go` — `remoteHostRegistry` struct with a mutex, `byHost map[string]*remoteHostEntry`, and an injectable `connect func(target remote.Target, path string) (remoteHostWorkspace, error)`; `remoteHostWorkspace` is a small interface (`APIURL() string`, `Token() string`, `Disconnect() error`) so tests never touch ssh; `newRemoteHostRegistry(sup *tool.ProcessSupervisor)` wires the real constructor; methods `workspaceFor(host, path string) (remoteHostWorkspace, error)`, `drop(host string)`, `closeAll(ctx)`.
- Modify: `internal/server/handler.go` — add `remoteHosts *remoteHostRegistry` next to `portMaps` (line 46); `Handler.Shutdown` calls `closeAll`.
- Modify: `internal/server/server.go` — construct the registry where `h.portMaps` is built (line 155).
- Modify: `internal/remote/workspace.go` — add `Token() string` returning `State.Token` so the interface is satisfied without exposing the struct field in server code.
- Test: `internal/server/remote_hosts_test.go` (new).

**Interfaces:**
- Produces: `h.remoteHosts.workspaceFor(host, path)`; `h.remoteHosts.drop(host)`; `h.remoteHosts.closeAll(ctx)`. Task 2 consumes all three. `remoteHostEntry` also carries `registeredPaths map[string]struct{}` guarded by the same mutex, exposed via `markRegistered(host, path) (first bool)`; Task 2 uses it to register a project on the remote exactly once per process.

- [ ] Write failing tests using a fake `connect` that counts calls and blocks on a channel: 20 goroutines calling `workspaceFor("h", "/p")` produce exactly one connect and all receive the same workspace; a connect error is returned to every waiter and the next call retries (second connect call observed); `drop` makes the next call reconnect; `closeAll` calls `Disconnect` on every entry once; `markRegistered` returns true the first time for a `(host, path)` and false after.
- [ ] Run; confirm failure (`newRemoteHostRegistry` undefined).
- [ ] Implement the registry. The connect must run outside the registry-wide mutex (hold a per-entry `sync.Mutex` or a "connecting" state with a `sync.Cond`/channel) so a slow ssh handshake to host A never blocks requests for host B. Log connect start, success (host, API URL without token), and failure (host, error).
- [ ] Wire construction in `server.go` and shutdown in `Handler.Shutdown`; run the existing server tests to make sure construction with a nil project store still works.
- [ ] Run `go test ./internal/server/... ./internal/remote/...`; green.
- [ ] Ready to commit: `feat(server): lazy per-host RemoteWorkspace registry`.

---

### Task 2: `/api/remote/{host}/api/*` proxy route with admission and remote project registration

**Why:** This is the seam the SPA uses for every chat, session, event, and permission call on a remote project. Admission is the same trust boundary `remoteWorkFor` in `internal/server/handler_remote_work.go` enforces: the host must be the `Host` of a saved project, and the pair `(host, path)` must be registered. The remote server's own allowlist is its workdir plus its saved projects, so the first proxied request for a second project on the same host must first register that path on the remote.

**Files:**
- Create: `internal/server/handler_remote_proxy.go` — `HandleRemoteProxy(w, r)`; helpers `remoteProxyTarget(r) (host, rest string, err error)` that unescapes `{host}` and rebuilds `/api/` + `{rest...}` with the original query string; `ensureRemoteProject(ctx, ws remoteHostWorkspace, host, path string) error` that POSTs `{path}` to the remote `/api/projects` through the same proxy client when `markRegistered` reports first use, treating an already-registered response as success.
- Modify: `internal/server/server.go` — `registerRoutes` (line 197): register `"/api/remote/{host}/api/{rest...}"` for all methods, wrapped in `s.authMiddleware`, before the generic `/api/` handlers so `ServeMux` precedence is explicit.
- Modify: `internal/server/remote_hosts.go` — `remoteHostEntry` caches the `*httputil.ReverseProxy` built once per connect via `remote.NewAPIProxy` with `onError` bound to `drop(host)`.
- Test: `internal/server/handler_remote_proxy_test.go` (new), using an `httptest.Server` as the "remote" and the registry's injectable `connect`.

**Interfaces:**
- Consumes: `h.remoteHosts.workspaceFor`, `drop`, `markRegistered`; `remote.NewAPIProxy`; `h.projects.List()` for admission; `remoteProjectEntry(host, path)` in `handler_remote_work.go` for the `(host, path)` check.
- Produces: the route. Which project path a request belongs to is read from, in order: `project_path` in a JSON body for `POST /api/chat`, `?project=` / `?path=` query params, else the `X-Ocode-Project` request header the SPA sets on every prefixed call. If none is present the request is still proxied (session-scoped endpoints carry no path) but no registration is attempted. The SPA side (part 03) sets the header on every prefixed call so registration is deterministic.

- [ ] Write failing tests: unknown host → 403 and no connect; host known but path not a saved project on that host → 403; valid pair → request reaches the fake remote at `/api/chat` with the local `token` query removed and `Authorization: Bearer <remote token>`; first request for `(host, path)` is preceded by exactly one `POST /api/projects` on the fake remote, second request none; fake remote closed → 502 and the registry entry is dropped so the following request triggers a new connect; SSE body from the fake remote streams through before close; a percent-encoded host such as `user%40host` maps to the saved `user@host`.
- [ ] Run; confirm failure.
- [ ] Implement `HandleRemoteProxy`: admission → `workspaceFor` → `ensureRemoteProject` (only when a path is present) → cached proxy `ServeHTTP` with `r.URL.Path` rewritten. Connect failure returns 502 with a JSON body `{error, stage: "remote-connect"}` matching the shape `writeError` produces so the SPA's bootstrap error rendering applies; log host and error.
- [ ] Register the route; run the full server test package to catch mux pattern conflicts with existing `/api/` routes.
- [ ] Run `go test ./internal/server/...`; green.
- [ ] Ready to commit: `feat(server): host-prefixed proxy route for remote project chat`.
