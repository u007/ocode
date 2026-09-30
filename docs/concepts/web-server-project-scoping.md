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
timestamp: 2026-09-30T07:37:09Z
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

### Remote projects: chat and terminals run on the host; files/git/forwards stay per-request

A sidebar remote (SSH/WSL) project's **chat/agent/session traffic and its
terminal websocket + terminal HTTP calls** are reverse-proxied to an
`ocode serve --remote` process **on that host**, reached through
`/api/remote/{host}/api/{rest...}` (`internal/server/handler_remote_proxy.go`,
route registered in `server.registerRoutes`). `Handler.remoteHosts`
(`internal/server/remote_hosts.go`) owns one lazily-connected
`remote.RemoteWorkspace` per host, shared by every remote project on that host
and closed at shutdown; the proxy builder is
`remote.NewAPIProxy`/`remote.InjectAuth` (`internal/remote/proxy.go`), which
`internal/desktop/proxy.go` also uses. Terminal pty's are therefore children of
the **host's** server, not the local one: a remote project's shell survives a
laptop sleep or a desktop restart (24 h detach TTL in `--remote` mode; 30 min
local). The Files tab, git, `!` commands, and port forwards keep their existing
per-request ssh/wsl.exe paths — they are not proxied.

Terminal routes (`/api/remote/{host}/api/terminal/{ws,…}`), the `ocode.bearer.*`
websocket auth flow, detach TTLs, lifecycle endpoints and the sidebar inventory
are specified in `docs/concepts/remote-persistent-sessions-terminals.md`.

Rules:

- **Only saved project hosts may be proxied.** `{host}` must be the `Host` of
  at least one saved project (the same trust boundary `remoteWorkFor`
  enforces); a non-matching host is rejected before any connect, and the route
  never consults the local `allowedProjectRoots` path allowlist.
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
- **Version mismatch is reused, never auto-restarted.** `EnsureRemoteServer`
  reuses an alive, healthy but version-mismatched server (`ServeState.Outdated`);
  restart is an explicit, unguarded `POST /api/remote/{host}/restart` — running
  turns and terminals are not drained.
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
  only detaches and keeps both (30 min local / 24 h remote TTL). With the list
  shared, a close ends the session for EVERY client, including one attached to
  that shell. `TestTerminalKillDropsDiskHistory` pins the history removal.
