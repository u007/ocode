# Part 01 — Remote-Side Foundations

Self-contained. Spec: `docs/superpowers/specs/2026-09-17-remote-project-agent-on-host-design.md`.

Constraints that apply to every task here: TDD (failing test first), no code in this plan, no fallbacks, log every caught error with what was attempted, surgical diffs, run `go test ./internal/...` before declaring a task done, do not commit unless the user asks.

Everything in this part is machine-neutral infrastructure that the local server (part 02) and the remote `ocode serve --remote` process both use. None of it changes runtime behaviour for local projects except the `~` expansion, which is deliberately server-wide.

---

### Task 1: `~` expansion for project paths on the server that owns `$HOME`

**Why:** Saved remote paths like `~/www/app` reach the remote server verbatim. `projects.Store.Add` only runs `filepath.Clean`, and the chat handler binds `project_path` verbatim, so the remote server would reject or mis-bind such a path. Expansion must happen on the machine whose home it refers to, so it lives in the server code and is not gated on remote mode.

**Files:**
- Modify: `internal/projects/projects.go` — `Store.Add` (around line 206) and a new exported helper `ExpandHome(path string) (string, error)` that rewrites a leading `~` or `~/` using `os.UserHomeDir` and returns any other path unchanged; a `~user` form is not expanded.
- Modify: `internal/server/handler_projects.go` — `HandleAddProject` (line 34): expand before validation so an existence check sees the real directory.
- Modify: `internal/server/handler.go` — `HandleChat` around lines 715–740: expand `req.ProjectPath` before it becomes `projectRoot` and before `SessionManager.BindNewOrVerify`.
- Test: `internal/projects/projects_test.go`, `internal/server/handler_projects_test.go`, `internal/server/handler_test.go` (or the existing chat handler test file).

**Interfaces:**
- Produces: `projects.ExpandHome(path string) (string, error)`. Part 02 does not call it; the remote server calls it implicitly through these handlers.

- [ ] Write failing tests: `Store.Add("~/x")` stores `<HOME>/x` (set `HOME` with `t.Setenv`); `Store.Add("/abs")` unchanged; `ExpandHome("~")` returns `HOME`; `ExpandHome("~bob/x")` unchanged.
- [ ] Write failing handler tests: add-project with `~/x` where `<HOME>/x` exists succeeds and lists the expanded path; chat with `project_path: "~/x"` binds the session to `<HOME>/x` (assert via the session manager snapshot).
- [ ] Run the tests; confirm they fail for the expected reason.
- [ ] Implement `ExpandHome`, call it in `Store.Add`, `HandleAddProject`, and `HandleChat`. An `os.UserHomeDir` error is returned to the caller as a 400/500 with a logged message, never swallowed.
- [ ] Run `go test ./internal/projects/... ./internal/server/...`; green.
- [ ] Ready to commit: `feat(projects): expand ~ in project paths against the server's own home`.

---

### Task 2: Shared reverse-proxy builder in `internal/remote`

**Why:** `internal/desktop/proxy.go` already knows how to forward `/api/*` to a `RemoteWorkspace` (rewrite scheme/host/path, drop the local `token` query and bearer, inject the remote bearer from `ServeState.Token`, return 502 on backend error). The local server needs the same logic for a per-host route, and `internal/server` already imports `internal/remote` (see `handler_remote_work.go`), so the builder moves down to `internal/remote` and desktop delegates to it. The stale comment in `proxy.go` about an import cycle is removed with the move.

**Files:**
- Create: `internal/remote/proxy.go` — `NewAPIProxy(apiURL string, remoteToken string, onError func(error)) (*httputil.ReverseProxy, error)`; exported `InjectAuth(req *http.Request, remoteToken string)`; `singleJoiningSlash` moves here.
- Modify: `internal/desktop/proxy.go` — `NewRemoteProxy` calls `remote.NewAPIProxy`; keep `RemoteProxy.ServeHTTP` and the local-token check unchanged.
- Test: `internal/remote/proxy_test.go` (new); `internal/desktop/proxy_test.go` must keep passing unchanged.

**Interfaces:**
- Produces: `remote.NewAPIProxy(apiURL, remoteToken string, onError func(error)) (*httputil.ReverseProxy, error)` and `remote.InjectAuth`. Part 02 consumes both. `onError` is invoked from the proxy's `ErrorHandler` before it writes 502, so the caller can drop its cached workspace.

- [ ] Write failing tests in `internal/remote/proxy_test.go` against an `httptest.Server` backend: local `?token=` is stripped; local `Authorization` is replaced by `Bearer <remote>`; path `/api/x` is forwarded as `/api/x`; a closed backend yields 502 and calls `onError` once; a `text/event-stream` response body arrives before the backend closes (flush behaviour).
- [ ] Run; confirm failure (`NewAPIProxy` undefined).
- [ ] Implement `internal/remote/proxy.go` by moving the director, error handler, and helpers out of `internal/desktop/proxy.go`; `ErrorHandler` logs the error with method and path via `log.Printf` then calls `onError`.
- [ ] Update `internal/desktop/proxy.go` to delegate; delete the moved helpers and the import-cycle comment.
- [ ] Run `go test ./internal/remote/... ./internal/desktop/...`; green.
- [ ] Ready to commit: `refactor(remote): share the remote API reverse-proxy builder`.

---

### Task 3: `RemoteWorkspace.Connect` runs credential sync and supports WSL without a tunnel

**Why:** `Connect` in `internal/remote/workspace.go` (line 93) currently does ensure-binary → discover-or-start → `StartTunnel`. It skips the credential sync the CLI path runs (`runSyncStage` in `connect.go` line 438), so a freshly provisioned remote server has no provider keys and cannot run a turn. It also always tunnels, which is wrong for `wsl:` targets (Windows shares loopback with WSL2; see `StartTunnel` doc in `serve.go`).

**Files:**
- Modify: `internal/remote/workspace.go` — `Connect`, `Disconnect` (line 136), `APIURL`.
- Modify: `internal/remote/connect.go` — `runSyncStage` stays; expose a way to run it without a TUI progress writer (a `Progress` that writes to `io.Discard`, see `progress.go`).
- Test: `internal/remote/workspace_test.go` (existing; uses `fake_transport_test.go`), `internal/remote/connect_wsl_test.go` patterns.

**Interfaces:**
- Consumes: `runSyncStage(progress *Progress, transport Transport, hostKey, ver string) error`, `Target.Kind == KindWSL`, `ServeState.Port`.
- Produces: unchanged public surface (`NewRemoteWorkspace`, `Connect`, `Disconnect`, `APIURL`, `State`), now with sync and WSL semantics. Part 02 relies on `Connect` returning only after the API URL is reachable.

- [ ] Write failing tests with the fake transport: after `Connect`, the transport saw a `remote-receive-config` sync exec (or the cached-hash path when unchanged) between ensure-binary and server discovery; for a `wsl:` target no `ssh` tunnel is started and `APIURL()` is `http://127.0.0.1:<State.Port>`; `Disconnect` on a WSL workspace does not touch the supervisor.
- [ ] Run; confirm failure.
- [ ] Implement: call the sync stage after `ensureBinary` using the target's canonical string as `hostKey` (matches the CLI cache key); branch on `KindWSL` to skip `FreeLocalPort`/`StartTunnel`, set `APIPort` from `State.Port`, and guard `Disconnect` on a nil tunnel. Sync failure is fatal for `Connect` and logged with the host.
- [ ] Run `go test ./internal/remote/...`; green. Also run `go test ./internal/desktop/...` because the whole-app remote mode shares this code.
- [ ] Ready to commit: `feat(remote): RemoteWorkspace.Connect syncs credentials and skips the tunnel for WSL`.
