# Remote Project Chat Agent Runs on the Host — Design Spec

Date: 2026-09-17

## Problem

A sidebar remote project (`Project.Host != ""`, SSH `[user@]host` or
`wsl:<distro>`) already forwards its terminal, Files tab, git, `!` shell
commands, and port forwards over ssh/wsl.exe, and each of those resolves the
**remote's** own login shell (`remote.LoginShellScript`).

The chat agent does not. `buildAgentSession` builds the agent on the local
desktop/web server with the remote path as a local workdir and only adds a
`Project host:` line to the prompt. Consequences on every agent turn against
a remote project:

- The bash tool runs the **local** login shell (`tool.SetLoginShell`), so
  `$HOME`, `~`, `$PATH`, and cwd are the dev machine's.
- Read/write/edit/grep/ls/rgrep tools touch the **local** filesystem at the
  remote path, which usually does not exist.
- A saved remote path written as `~/www/app` can only be meaningful on the
  host; the local server has no correct way to expand it.

Decision (2026-09-17, user-approved): the chat agent for a remote project
runs **on the remote host**, on an `ocode serve --remote` server, reusing
the whole-app remote-workspace machinery (`internal/remote.RemoteWorkspace`,
`internal/desktop/proxy.go`). The local server becomes a proxy for that
traffic.

Rejected alternatives:

- Bash tool only over ssh: file tools stay wrong.
- Every agent tool remote-aware over ssh: wide surface, one ssh round-trip
  per tool call, duplicates what the remote server already does natively.
- Session-scoped proxy (local map of session id → host): every session
  endpoint must consult the map; easy to leak one. Host-prefixed proxy was
  chosen instead.

## Principle

For a remote project, execution authority for chat/agent work is the remote
`ocode serve --remote` process on that host. Shell, `$HOME`, files, git, LSP,
and tool permissions all resolve there natively. The local server never
expands a remote path and never runs an agent turn for a remote project.

Terminal, Files tab, git tab, `!` commands, and port forwards keep their
existing per-request ssh/wsl.exe paths. They are not moved in this change.

## Architecture

```
Browser / desktop SPA
   │
   │ local project:      /api/chat, /api/sessions/{id}/…  (unchanged)
   │ remote project:     /api/remote/{host}/api/chat, …/api/sessions/{id}/…
   ▼
Local ocode server (desktop or `ocode serve`)
   ├─ remoteHostRegistry: host → *remote.RemoteWorkspace (lazy, once)
   └─ remote host proxy:  strips local token, injects remote token,
                          forwards to http://127.0.0.1:<tunnel port>
                                   │  ssh -N -L  (SSH)   /  direct loopback (WSL)
                                   ▼
                        Remote `ocode serve --remote` on the host
                        (agent, tools, LSP, git, permissions, sessions)
```

One remote server per **host**, shared by every remote project on that host.
This matches the existing per-host state file `~/.ocode/remote/serve.json`
that `RemoteWorkspace` discovers and launches against today.

## Local server

### Host workspace registry

New `internal/server/remote_hosts.go`:

- `remoteHostRegistry` on `Handler`, keyed by the canonical host string
  (`remote.Target.String()`, the same key `projects.Project.Host` stores).
  Pattern: the existing `portMapRegistry`.
- `workspaceFor(host) (*remote.RemoteWorkspace, error)`: returns the
  connected workspace, connecting on first call. A per-host mutex serializes
  concurrent first requests so exactly one `Connect` runs; losers wait and
  reuse the result. A failed connect is not cached: the next request retries.
- `RemoteWorkspace` is constructed with the target parsed from the saved
  project and `RemotePath` = the path of the project that triggered the first
  connect (the launch command already `cd`s there before `serve --remote`).
- Connect stages, in order, reusing existing code: ensure binary
  (`ensureBinary`), credential sync (`runSyncStage`; already drops
  machine-local keys such as `terminal_shell`), discover-or-start server,
  tunnel (SSH) or direct loopback (WSL, see below). Today
  `RemoteWorkspace.Connect` skips the sync stage; this change adds it, since
  a remote agent without provider keys cannot run a turn.
- `Close()` on every workspace during server shutdown, alongside the port
  forward managers.
- `h.projects` is the only source of admissible hosts. A host that is not the
  `Host` of at least one saved project is rejected with 403 before any
  connect is attempted, the same trust boundary `remoteWorkFor` enforces.

### Host-prefixed proxy route

New route in `server.registerRoutes`:

```
/api/remote/{host}/api/{rest...}
```

- `{host}` is URL-path-escaped; the handler unescapes it and compares it
  verbatim against saved project hosts. No `ParseTarget` normalisation is
  applied on the way in, so `user@host` and `host` are distinct keys exactly
  as they are in the project store.
- Wrapped in `authMiddleware` like every other `/api/` route, so the local
  token is validated first.
- Body is reverse-proxied to `workspace.APIURL()` with the path rewritten to
  `/api/{rest}`. Auth rewrite reuses the whole-app logic
  (`internal/desktop/proxy.go` `injectAuth`): drop the local `token` query
  param and `Authorization` header, set `Authorization: Bearer <remote
  token>` from `ServeState.Token`. `internal/server` already imports
  `internal/remote`, so the proxy builder moves to `internal/remote` (or a
  small shared file) and `internal/desktop/proxy.go` calls the same builder.
- Streaming: `httputil.ReverseProxy` already flushes `text/event-stream`
  immediately, which covers `/api/events` and `/api/chat/stream`. No
  WebSocket route is proxied in this change.
- Proxy error (tunnel dead, remote server gone): respond 502 with a JSON
  error body in the shape the SPA already renders for bootstrap failures,
  log at warn with host and path, and drop the registry entry so the next
  request reconnects.

### Project admission on the remote server

A remote server's `allowedProjectRoots` is its workdir plus its own saved
projects. The first project on a host is the workdir; any further project on
the same host is not admissible until registered there. Therefore, before
the first proxied request for a `(host, path)`, the local server calls the
remote's `POST /api/projects` with `{path}` through the same proxy client.
This is idempotent (the store's `Add` is a no-op for an existing path) and is
done once per `(host, path)` per local process lifetime, tracked in the
registry entry.

## Remote server: home path resolution

Saved remote paths may start with `~` (the project editor accepts
`~/www/app`). The **remote** server owns expansion, against its own
`$HOME`:

- `projects.Store.Add` / the add-project handler and the chat `project_path`
  binding (`HandleChat` → `SessionManager.BindNewOrVerify`) expand a leading
  `~` or `~/` with `os.UserHomeDir()` before `filepath.Clean`. This is a
  server-wide behaviour, not gated on `remoteMode`: a local user typing
  `~/www/app` into the local project dialog gets the same expansion, which is
  correct on that machine too.
- The local server never expands a remote path. `projectHostFor` keeps
  matching the verbatim saved path.
- The launch command already passes `RemotePath` through `shellQuotePath`,
  which rewrites `~/…` to `"$HOME/…"`, so the remote server's workdir is the
  expanded path from the start.

## Frontend

`web/src/api/client.ts`:

- New `remoteApiBase(host?: string): string` returning
  `/api/remote/${encodeURIComponent(host)}` for a host, `""` otherwise.
- `fetchJSON`, the SSE `EventSource` for `/api/events`, and the chat stream
  accept an optional `host` and prefix the path with `remoteApiBase(host)`.
  Local projects keep byte-identical URLs.
- **Host resolution is per session, not per active project.** Every open
  chat tab is already bound to a project path
  (`findProjectPathForTab(projectState, sessionId)` in `useChat`); the host
  for a session's calls is derived from *that* path via the existing trust
  rule (`getTrustedTerminalProject`: exactly one saved project matches the
  path). The active project is consulted only for a brand-new tab that has
  no session id yet. Switching the sidebar's active project therefore never
  re-routes an existing session: a session opened on host A keeps talking to
  host A while the user works in a local project, and two tabs on different
  hosts run side by side. An ambiguous or unknown path yields no host, which
  means the local server, exactly as `useChat` treats `!` commands today.
- Session identity lives on the remote server: the remote `ocode serve`
  creates the session id, persists the transcript under its own session
  store, and binds it to the expanded project root through its own
  `SessionManager`. The local server keeps no record of remote session ids;
  a remote id reaching an unprefixed local route resolves to "unknown
  session" and fails loudly rather than being silently rebound locally.
- Every session-scoped helper in `client.ts` (`getSession`, `sendMessage`,
  `cancel`, `compact`, `truncate`, `setModel`, `title`, `context`, `state`,
  `status`, `btw`, `export`, `share`, permission/question resolve) takes the
  session's host, so a session-scoped call can never be issued without the
  host its session was created on.

Endpoints that take the prefix for a remote project: `/api/chat`,
`/api/chat/stream`, `/api/chat/messages`, `/api/events`, `/api/sessions` and
every `/api/sessions/{id}/*`, `/api/projects/sessions`, `/api/agents/runs`
and its stream, `/api/models`, `/api/config/*` model settings shown in the
session header, `/api/changes*`, `/api/lsp/statuses`, and the remote-control
permission/question routes. Terminal, files, fs, git, secret, shell, uploads,
portmaps, browse, and project-store routes stay local.

`web/src/stores/projectStore.tsx` already keys session caches by
`projectSessionKey(path, host)`; session lists for a remote project are
fetched through the prefixed base and cached under that key. Views that
aggregate sessions across all projects remain local-only in this change.

Event bus: `lib/eventBus` keeps one stream per host that currently has at
least one open session tab (plus the local stream). Frames from every stream
feed the same bus, and consumers keep routing by `session_id` exactly as
today. A host's stream is closed when its last session tab closes, not when
the active project changes. Session ids are random on both sides, so a local
and a remote frame cannot collide on `session_id`.

Session-list views: `/api/projects/sessions?path=` for a remote project is
fetched through that host's prefix and cached under the existing
`projectSessionKey(path, host)`, so resuming a session from the sidebar
carries its host into the tab binding from the start.

## WSL

`RemoteWorkspace.Connect` currently always starts an ssh tunnel. Add a
branch on `Target.Kind == KindWSL`: no tunnel process, `APIPort` =
`ServeState.Port`, `localAPIURL` = `http://127.0.0.1:<port>`. Windows shares
loopback with WSL2, as `StartTunnel`'s doc and the whole-app spec already
state. `Disconnect` skips tunnel teardown when none was started.

## Failure handling

| Failure | Behaviour |
|---|---|
| Host not in project store | 403, no connect attempted |
| ssh auth / unreachable host | Connect error surfaced as the chat bootstrap stage error (`stage: "remote-connect"`), logged with host; entry not cached |
| Provision fails (unsupported platform, upload) | Same as above with the provision error text |
| Tunnel dies mid-session | Proxy returns 502, entry dropped, next request reconnects; SSE consumers reconnect via their existing retry |
| Remote server version mismatch | Existing `ServerAlive` check restarts a fresh server |

There is no fallback to a local agent for a remote project. A connect
failure blocks the turn with an error; it never silently runs locally.

## Security invariants

- The remote token never reaches the browser: it is injected server-side and
  the response is not rewritten to include it.
- Only hosts present in the saved project store are proxied.
- Local-path handlers are untouched, so the existing rule that remote paths
  never enter the local filesystem trust boundary is preserved
  (`docs/gotchas/remote-project-path-trust-boundary.md`).
- `~` expansion runs only on the machine whose `$HOME` it refers to.

## Files

New:

- `internal/server/remote_hosts.go` — registry, `workspaceFor`, shutdown
- `internal/server/handler_remote_proxy.go` — `/api/remote/{host}/api/`
  handler, admission, project registration on the remote
- `internal/remote/proxy.go` — shared reverse-proxy builder and `injectAuth`
  (moved from `internal/desktop/proxy.go`)

Modified:

- `internal/remote/workspace.go` — sync stage in `Connect`, WSL no-tunnel
  branch
- `internal/desktop/proxy.go` — delegate to `internal/remote` builder
- `internal/server/server.go` — route registration, shutdown hook
- `internal/server/handler.go` / `handler_projects.go` — `~` expansion on
  add-project and chat `project_path`
- `internal/projects/projects.go` — `~` expansion helper used by `Add`
- `web/src/api/client.ts` — `remoteApiBase`, host-aware fetch/SSE
- `web/src/hooks/useChat.ts`, `web/src/lib/eventBus.ts`,
  `web/src/stores/projectStore.tsx` — pass the trusted host through
- `AGENTS.md` — replace the "per-project remote projects execute their chat
  agent on the local server" statements; `internal/agent/prompt.go`
  `Project host:` wording updated to match
- `docs/architecture/terminal-detach-reattach.md` — same statement

## Tests

Go:

- `remote_hosts_test.go`: N concurrent `workspaceFor` calls connect once
  (fake transport); failed connect is retried; unknown host rejected.
- `handler_remote_proxy_test.go`: 403 for unregistered host; local token
  stripped and remote bearer injected; path rewrite; 502 on backend error
  drops the entry; project registration issued once per `(host, path)`.
- `workspace_test.go`: WSL target starts no tunnel and uses the state port;
  sync stage runs on connect.
- `projects_test.go` / `handler_projects_test.go` / `handler_test.go`: `~`
  and `~/x` expand against `HOME`; bare paths unchanged.

Frontend (vitest):

- `client.test.ts`: `remoteApiBase` for local, remote, and encoded hosts;
  fetch URL prefixing.
- `useChat` / `eventBus` tests: remote project opens the prefixed stream;
  ambiguous path stays local; a tab bound to host A keeps its host after the
  active project switches to a local project; two tabs on different hosts
  hold two streams; closing the last tab on a host closes its stream.

Manual verification before closing: on a real SSH project, an agent turn
running `echo $HOME && pwd && ls` reports the remote home and project path,
and the read tool returns a remote file. Repeat on WSL.

## Out of scope

- Moving terminal/files/git/`!`/port forwards to the remote server's native
  endpoints.
- Cross-host aggregation of sessions or agent runs in global views.
- LSP or browse-origin proxying for remote projects.
- TUI.
- Re-homing existing sessions when a saved remote project's host is edited
  (`PATCH /api/projects/remote`): open tabs keep the old host until closed.
- Per-workspace state files on the remote (spec Fix 2 from 2026-09-11 stays
  deferred; one server per host is the accepted model).
