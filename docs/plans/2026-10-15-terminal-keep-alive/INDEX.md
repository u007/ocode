Plan: Keep terminal shells alive across ocode.app restart (opt-in)
Level: design (decisions and file/function names, no code).
Risk: high (process lifecycle, pty ownership, shutdown path, cross-package).
Date: 2026-10-15. Status: designed, not started.

## Problem

Desktop runs the API server in-process (`internal/desktop/boot.go` `StartServer`, same
PID as the Wails window). Every terminal tab is a `pty.Start` inside that PID
(`internal/server/handler_terminal.go` ~line 449). On quit, `app.OnShutdown`
(`cmd/ocode-desktop/main.go` ~line 591) calls `Server.Shutdown` which calls
`Handler.shutdownTerminals` (`internal/server/handler_shutdown.go`) and SIGTERM/SIGKILLs
every shell. Even if that kill were skipped, the app exiting closes the pty master fd,
the kernel sends SIGHUP to the shell, and a relaunched app cannot reopen another
process's pty master. So "do not kill on quit" is not implementable in-process.

Already in place and reused as-is: in-lifetime detach/reattach with replay buffer
(`terminal_session.go`, `terminalReplayCap`), durable on-disk history log
(`terminal_history.go`, paged by `GET /api/terminal/{id}/history`), server-side tab list
(`internal/termtabs`, restored by the SPA on relaunch), OSC title scanner, SPA
`resumed:true` handling in `web/src/components/Terminal/TerminalPanel.tsx`.

## Decision

Add an opt-in detached **terminal host** process that owns the ptys. The desktop (or
`ocode serve`) becomes a byte-transparent proxy for terminal websockets when the setting
is on. Same shape as the existing LSP daemon (`internal/lsp/manager.go`
`spawnDaemonProcess`, `daemon_spawn_unix.go` Setsid re-exec of own binary, hidden
subcommand routed in both `main.go` and `cmd/ocode-desktop/main.go`, metadata file with
port + token + protocol + pid in `internal/lsp/broker/metadata.go`).

Rejected: tmux/screen wrapper (external dependency, loses OSC title and resize
fidelity, no Windows path); full split of desktop into a thin client of a standalone
server (keeps agents alive too, but touches every subsystem and is far beyond the ask).

## Locked design points (repeated in each part where relevant)

- Setting: `terminal_keep_alive_on_quit` in `ocodeconfig.json`, default `false`.
  Mirrors `auto_share_on_start` plumbing (`internal/config/ocodeconfig.go` load, raw-key
  consume, save payload) and its API pair `GET/PUT /api/config/ocode/auto-share`.
- Placement rule: a shell is placed at spawn time (setting on -> host, off -> in-process).
  Keep-or-kill rule at quit: host-owned shells are kept iff the setting is on at quit
  time; in-process shells are always terminated as today. Toggling on does not rescue
  shells already running in-process, and the Settings UI says so.
- One host per user data dir (`paths.GlobalDataDir()`), all projects. Not per project.
- Transport: loopback TCP, random port, random bearer token, metadata file
  `<GlobalDataDir>/terminal-host.json` mode 0600, single-instance via
  `internal/filelock` on `<GlobalDataDir>/terminal-host.lock`. Protocol version is an
  integer constant; mismatch means "do not attach", never "restart the host".
- The host speaks the **existing** browser terminal protocol on `/api/terminal/ws`
  (binary frames = pty bytes, text frames = control JSON: attach/detach/resize), so the
  desktop proxy copies frames verbatim and the SPA is unchanged.
- The host trusts its caller for project validation. The desktop-side handler keeps
  every existing gate (`terminalAccessAllowed`, `projectForTerminal`, remote
  registration) and only then forwards. The host is never reachable without the token.
- Host-owned shells have no detach TTL while detached by an app quit. The host exits on
  its own only when it owns zero shells for an idle period (5 minutes, flag), or on
  SIGTERM (graceful terminate-all, same `shutdownTerminals` logic), or on an explicit
  drain request from the desktop (setting turned off at quit).
- History log path is unchanged (`<GlobalDataDir>/project/<slug>/terminal/<sha>.log`),
  written by whichever process owns the pty. Pulse `terminal_read`/`terminal_screen`
  and the Processes tab keep working because they read disk history plus the
  `terminalRegistry`, which the proxy side mirrors from the host inventory.
- Platform: macOS and Linux. Windows terminal handlers are already `501` stubs
  (`handler_terminal_windows.go`); the toggle is hidden there.
- Spawn of the host is a direct Setsid re-exec (precedent `daemon_spawn_unix.go`),
  deliberately NOT through the process supervisor: the whole point is that the host
  outlives the supervisor's owner. Document that exception where the spawn lives.

## Parts

- `01-terminal-host-process.md` — new hidden subcommand, host server, metadata,
  discovery, lifecycle, logging.
- `02-server-proxy-and-registry.md` — `internal/server` placement switch, websocket
  proxy, kill/list forwarding, registry reconcile, shutdown changes.
- `03-desktop-settings-web.md` — config key, API pair, Settings toggle, quit overlay
  text, outdated-host notice.
- `04-tests-docs-rollout.md` — unit/integration/race tests, docs and CLAUDE.md
  invariants, manual verification script, follow-ups.

## Out of scope (say so in the UI/docs, do not build)

- Agent `bash` tool shells and `bash_output` background jobs (different lifecycle).
- Keeping agent sessions or SSE streams alive across restart.
- Windows (ConPTY handles cannot be handed between processes cheaply).
- Rescuing shells that are already running in-process when the toggle is turned on.

## Crash and recovery (added 2026-10-15 after review)

- App crash = app quit from the host's point of view: shells keep running, relaunch
  rediscovers the host via metadata + health check and reattaches. Tabs come back from
  `internal/termtabs` (persisted on every PUT merge, not at quit).
- Orphan adoption: on relaunch the server reconciles host inventory against the tab
  list and adopts shells that have no tab (created just before the crash). Part 02/03.
- Host crash: shells die with it (same as today's app crash); disk history survives
  because the host syncs it on a cadence; discovery marks the stale metadata `dead`,
  deletes it, spawns a fresh host. No zombie process, no stale file.
- Self-healing metadata: the host re-publishes `terminal-host.json` every 30 s when the
  file is missing or carries another pid, so a deleted file cannot strand a live host
  behind the single-instance lock. Discovery re-reads after a failed spawn. Part 01.
- Pid reuse: liveness is never pid-alone; health with token is required.
- Ways to stop the host, all graceful: close every tab (idle exit), Settings "Stop
  terminal host now" button (drain), toggle off then quit (drain), SIGTERM (`kill`,
  macOS logout). Process is visible as `ocode terminal-host` in argv; Settings shows
  pid, state and shell count.
