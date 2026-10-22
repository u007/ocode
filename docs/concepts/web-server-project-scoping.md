---
type: Concept
title: Web/Desktop Server Project Scoping
description: 'Project dirs are per-session not per-process: session project roots, project-scoped endpoint params, remote project proxying rules and the server-side terminal tab list.'
resource: CLAUDE.md
tags:
  - server
  - project
  - remote
  - terminal
timestamp: 2026-10-18T00:41:42Z
---
# Web/Desktop Server Project Scoping

`h.workDir` (process cwd at startup, or what desktop boot passes to
`SetWorkDir`) is only the **default** project for a web/desktop server — never
the working directory of a session's work. The rules:

- **Session work follows the session's project root.** `SessionManager` binds
  session id → project root; `buildAgentSession` calls `ag.SetWorkDir` with it
  and takes its LSP manager from `h.lspManagerFor(projectRoot)` — one manager
  per project root, shared across tabs on the same repo. Never route a
  session-scoped operation through `h.workDir`.
- **Project-scoped endpoints take an explicit project param**, validated
  against `allowedProjectRoots()` (workdir + saved **local** projects — the
  shared local trust boundary; saved remote/SSH/WSL entries are excluded
  because their path is interpreted on the other machine, see
  `docs/gotchas/remote-project-path-trust-boundary.md`): git uses
  `?project=`, terminal uses `?project_path=` (plus
  `&host=` for a registered remote project, which spawns ssh/wsl.exe instead
  of a local shell), file tree
  confines `?path=`, command-context and uploads use `?project=` (uploads
  must land in `<project>/.ocode/uploads` — chat and terminal reference them
  by the relative path `.ocode/uploads/<name>`, which resolves against the
  session's project dir). A new endpoint that binds work to a directory must
  follow the same pattern, not read `h.workDir`.
- **The TUI RC bridge passes its own workdir** to `RegisterExternalSession`
  explicitly; `h.workDir` is only the empty-root fallback.
- **Never call `os.Getwd()` directly inside an `internal/server` handler.** A
  Finder/Dock-launched desktop `.app` starts with process cwd `/`, so a raw
  `os.Getwd()` silently resolves to the filesystem root instead of the actual
  project — this has both broken file reads/writes (`HandleInit` writing
  `AGENTS.md` to `/`) and defeated a path-containment security check
  (`resolveWithinWorkdir`'s "must stay inside the working dir" guard passes
  trivially when the working dir is `/`). Always resolve against `h.workDir`
  (falling back to `os.Getwd()` only if `h.workDir` is itself empty, which in
  practice it never is once `NewHandler`/`SetWorkDir` have run).
- The TUI itself is unaffected by any of this: it drives `internal/agent`
  directly with `m.workDir` and only touches `internal/server` for RC bridge
  types.

### Remote projects: chat, terminals and the browser run on the host; files/git/forwards stay per-request

A sidebar remote (SSH/WSL) project's **chat/agent/session traffic, its
terminal websocket + terminal HTTP calls, and its embedded browser** are
served by an
`ocode serve --remote` process **on that host**, reached through
`/api/remote/{host}/api/{rest...}` (`internal/server/handler_remote_proxy.go`,
route registered in `server.registerRoutes`). `Handler.remoteHosts`
(`internal/server/remote_hosts.go`) owns one lazily-connected
`remote.RemoteWorkspace` per host, shared by every remote project on that host
and closed at shutdown; the proxy builder is
`remote.NewAPIProxy`/`remote.InjectAuth` (`internal/remote/proxy.go`), which
`internal/desktop/proxy.go` also uses. Terminal pty's are therefore children of
the **host's** server, not the local one: a remote project's shell survives a
laptop sleep or a desktop restart (24 h detach TTL in `--remote` mode; 2 h
in plain web mode; none in the desktop app). The Files tab, git, `!` commands, and port forwards keep their existing
per-request ssh/wsl.exe paths — they are not proxied.

**Embedded browser.** A remote project's browser tab fetches its origin from the
host, not from this machine. `GET /api/browse/config?host=<remote>`
(`handleRemoteBrowseConfig`, `internal/server/handler_remote_browse.go`) returns
an **absolute URL on the browse proxy's second listener**
(`http://<host>:<proxyPort>/api/remote/<host>/browse`) rather than the tunneled
loopback origin, because the workspace tunnel's local end is loopback-only and a
remote client (a phone, another browser) cannot dial it. Browse content is served
on a second listener bound to the main server's host: local projects at `/b/`
and `/__ocode_capture.js` (reverse proxy to the loopback browse origin), remote
projects at `/api/remote/{host}/browse/{rest...}` (`handleRemoteBrowseProxy`,
prefix stripped, `Set-Cookie Path=/b/` rewritten). The second listener is a
**different port** from the SPA, so browsed pages are cross-origin with the SPA
(origin-scoped storage, no CORS headers). Grants, revoke and TLS bypass go
through `/api/remote/{host}/api/browse/*`, so the host's own browse server mints
and records them, and records the browser's `Origin` for the capture script's
postMessage target. A `localhost` page in a remote project is therefore a
`localhost` on the host. Local projects still use this machine's browse origin
(no `host` param). Rules that keep this honest:

- **The unauthenticated browse proxy never dials.** `handleRemoteBrowseProxy` sits
  outside `authMiddleware`, so it only reuses a workspace the authenticated
  `/api/browse/config` call already connected (`remoteHostRegistry.connectedWorkspace`);
  an unconnected host is a 409, never an SSH connect. `browseProxyGate` also counts a
  downstream 401/403 (forged `__grant`, stale cookie) against the per-IP lockout.
- **The tunnel proves the browse forward.** `remote.tunnelArgs` binds the browse
  `-L` BEFORE the api `-L` under `ExitOnForwardFailure=yes`, and
  `RemoteWorkspace.Connect` waits on the api port (a fresh `FreeLocalPort`),
  not the browse port. The browse number is the remote's own, so a local
  collision is possible, and a dial of a colliding listener would "succeed"
  against the wrong service. With this order the api listener exists only once
  the browse bind succeeded. The handler reads `ws.BrowseURL()` (on the
  `remoteHostWorkspace` interface) and never re-derives the port itself.
- **Unknown project ⇒ no load.** `hostForProject(projects, path)`
  (`web/src/lib/browserStore.ts`) returns `null` when the path is not in the
  project list (list not loaded, project removed); `BrowserPanel` then sets the
  tab error and fetches nothing. `PreviewHost` receives the ACTIVE project's own
  host, never a lookup, so an absent host there is a local project.
- **Browse content is served on a second listener (origin isolation).** The
  proxy routes skip the main server's auth middleware (iframes and WebSockets
  cannot send an `Authorization` header), but the listener is a **separate
  port** from the SPA, so browsed pages are cross-origin: a proxied page
  cannot read the SPA's `localStorage`/`sessionStorage` or read `/api/*`
  responses (no CORS headers). The security boundary is the browse origin's
  own grant/cookie auth — grants minted only by the authenticated
  `POST /api/browse/grant`, and `handleBrowse`/`handleCDP` reject a request
  without a valid grant or cookie (401). `browseProxyGate` adds a cheap
  pre-filter (reject a request carrying neither a `__grant` nor the
  `ocode_browse` cookie before the upstream hop) and shares the API's per-IP
  rate limiter (5 failures to 1 min lockout). Over Tailscale the phone reaches
  the second listener via `tailscale serve --https=<proxy_port>`; on the LAN it
  is reached directly on the same host as the main server.
- **Remote origins are re-read per load.** The host's browse listener binds an
  ephemeral port that changes when `ocode serve --remote` restarts, so
  `getBrowseBase(host)` caches only the local origin; a remote one is read on
  every call (concurrent callers share one request) and `BrowserPanel.loadInto`
  re-reads it before each navigation. The server-side workspace registry is the
  cache. A failed read is surfaced through `actions.setError`, and a read that
  resolves after the panel's host changed is dropped.
- **Revoke is addressed by the tab's project host** (`browserActions.close(key,
  host)` → `revokeBrowseSession(key, host)`), never by an in-memory record of
  where the grant was minted: after an SPA reload no such record exists, and a
  revoke sent to this machine would leave the host's session live.

Terminal routes (`/api/remote/{host}/api/terminal/{ws,…}`), the `ocode.bearer.*`
websocket auth flow, detach TTLs, lifecycle endpoints and the sidebar inventory
are specified in `docs/concepts/remote-persistent-sessions-terminals.md`.

Rules:

- **Only saved project hosts may be proxied.** `{host}` must be the `Host` of
  at least one saved project (the same trust boundary `remoteWorkFor`
  enforces); a non-matching host is rejected before any connect, and the route
  never consults the local `allowedProjectRoots` path allowlist.
- **Remote file tree admits any path inside a saved project root.** Expanding a
  folder requests `?path=<projectRoot>/<dir>`, so `remoteFileTree` admits the
  longest registered root containing the path (`remoteTreeRoot`, exact or
  `root + "/"` prefix, `..` rejected) and returns Paths anchored to that root.
  Every other remote endpoint still goes through `remoteWorkFor`, which
  requires the exact registered path.
- **The remote token never reaches the browser.** The proxy strips the local
  `token` query param and `Authorization` header and injects the remote bearer
  server-side. On a 101 it must restore the browser's offered websocket
  subprotocol (`remote.InjectAuth` + `ModifyResponse`); any new Upgrade-capable
  proxy path must preserve this or it breaks the handshake and leaks the token.
- **`~` is expanded only by the server owning that `$HOME`**
  (`projects.ExpandHome`, called from `Store.Add`, `HandleAddProject`, and
  `HandleChat`). A remote project's saved path stays verbatim locally and is
  expanded by its host; a local `~/…` project is expanded locally.
- **A remote failure blocks the turn** (502 `{error, stage:
  "remote-connect"}`); never fall back to running a remote project's agent
  locally.
- **The SPA resolves the host per session, not per active project**
  (`resolveSessionHost` in `web/src/hooks/useSessionHost.ts`, from the tab's
  project binding). New session-scoped calls must pass that host — plus an
  `X-Ocode-Project` header when they carry a path — or they hit the wrong
  machine. Local calls pass no host and keep byte-identical URLs.
- **A version-mismatched remote server is retired on connect.** `EnsureRemoteServer`
  kills an alive, healthy server whose version differs from the local build, starts
  a fresh one, then reaps every other-version `ocode serve` on the host
  (`killOtherVersionServers`) — leftovers held memory and starved sshd. Running
  turns and terminals on the old server are not drained. Explicit restart remains
  `POST /api/remote/{host}/restart`.
- **Terminal reattach keys are host-qualified** (`projectTerminalsKey(path,
  host)` = `<host>::<path>` for remote, bare path for local), so a local and a
  remote project at the same path cannot share terminal tabs.

### The open-terminal TAB LIST is server state, like session tabs

`GET/PUT /api/terminal-tabs` → `internal/termtabs` (`terminals.json` under the
global data dir, cross-process lock + mtime reload + atomic rename) mirrors
`internal/tabs`, because localStorage is per-origin. The store design
(`terminalPersistence.ts` is only a MIRROR, `state.revision`, `activeId` not
shared, one attachment slot / `superseded` / **Take over**) lives in
`skills/ocode-web/SKILL.md` (terminalStore). Rules recorded only here:

- **Keys are opaque.** A key is the client's `<host::path>` composite and is
  stored verbatim — never `filepath.Clean`, which would mangle `wsl:Ubuntu::/home/x`
  and `C:\Users\dev\app`.
- **The PUT is a MERGE.** A provided key replaces that project, an empty
  `terminals` list deletes it, and keys ABSENT from the body are preserved. A
  client must therefore emit an explicit empty entry for a project whose last tab
  it just closed (same trap as `toServerTabs`).
- **Closing a tab is a PERMANENT discard, not a detach.** `DELETE
  /api/terminal/{id}` kills the shell AND removes its append-only disk history
  (`sess.history.remove()`, `handler_terminal.go`); unmounting the panel alone
  only detaches and keeps both (2 h local / 24 h remote TTL). With the list
  shared, a close ends the session for EVERY client, including one attached to
  that shell. `TestTerminalKillDropsDiskHistory` pins the history removal.
