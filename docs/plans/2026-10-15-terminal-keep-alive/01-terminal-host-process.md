Part 01 — Terminal host process (`terminal-host` hidden subcommand)
Status: designed, not started. Platform: macOS/Linux only.

Context needed for this part alone: the desktop app runs the API server in-process, so
terminal ptys die with the window. This part adds a detached process that owns ptys and
speaks the existing terminal websocket protocol, so an app relaunch reattaches instead of
respawning. Precedent for every mechanism here is the LSP daemon: hidden subcommand
`lsp-daemon` in `main.go` and `cmd/ocode-desktop/main.go`, `internal/lsp/cmd_daemon.go`
`RunDaemon`, `internal/lsp/daemon_spawn_unix.go` (Setsid re-exec of `os.Executable()`),
`internal/lsp/broker/metadata.go` (protocol, port, pid, token, created_at).

Task 1 — Package and entry point (standard)
- New package `internal/termhost`. Exports `Run(args []string) error` (the subcommand
  body), `Metadata` (protocol int, pid, port, token, executable version, created_at,
  data_dir), `MetadataPath()`, `LogPath()`, `Protocol` constant (start at 1),
  `IdleTimeoutDefault` (5 minutes).
- Route hidden subcommand `terminal-host` in `main.go` next to `lsp-daemon`, and in
  `cmd/ocode-desktop/main.go` in the same early `os.Args[1]` switch (the desktop binary
  must handle it before Wails creates a window, exactly like the `lsp-daemon` case and
  for the same reason: otherwise the re-exec opens a second desktop window).
- Flags: `-idle <duration>` (exit after this long with zero shells), `-data-dir`
  (defaults to `paths.GlobalDataDir()`; tests pass a temp dir).
- Logging: `log.SetOutput` to `<paths.LogsDir()>/terminal-host.log` (size-rotated the
  same way other logs under `internal/logfile` are, if a helper exists there; else plain
  append). Never stdout/stderr: the parent redirects both to `io.Discard` and may be the
  TUI-less desktop.

Task 2 — Host HTTP server: terminal routes only (high)
- The host must NOT be a full `server.New(...)`: that boots agents, cron, emitters,
  LSP, browse. Add to `internal/server` a constructor `NewTerminalHostServer(token string)
  *http.Server`-shaped entry (name it `newTerminalHostMux` + `RunTerminalHost(ln, token,
  opts)`) that registers only:
  `GET /api/terminal/ws`, `GET /api/terminal`, `DELETE /api/terminal/{id}`,
  `GET /api/termhost/health` (protocol, version, pid, shell count),
  `POST /api/termhost/drain` (graceful terminate-all, then exit),
  each behind the existing bearer-token auth middleware with the host's own token.
- Create a minimal handler constructor `newTerminalHostHandler()` that initialises only
  `terminalSessions`, `terminalProcs`, `terminalAuthConfigured = true`, and a no-op
  event bus notifier for `notifyTerminalProcsChanged`. Do not reuse `NewHandler()`.
- Seam refactor in `handler_terminal.go`: split `HandleTerminalWS` into (a) the gates
  (`terminalAccessAllowed`, `projectForTerminal`, remote-registration admission, shell
  command construction) and (b) `serveTerminalPTY(w, r, project string, cmd *exec.Cmd,
  terminalID string, detachTTL)` which is the reserve → `pty.Start` → attach → read loop
  core that exists today. The host's websocket handler calls (b) directly after a
  trust-the-caller project parse (query `project`, or `host`+`project_path` for remote
  shells; the host does not own a projects registry). Part 02's in-process path calls
  (a) then (b); its proxy path calls (a) then forwards. One core, no copies.
- Detach TTL in host mode: a shell detached because its proxy socket went away must NOT
  be killed on the 2h/24h timer. Thread a `detachTTL` of zero meaning "no timer" through
  `newTerminalSession`; the host passes zero. Explicit kill (`DELETE`) and shell exit are
  the only ways a host-owned shell ends, plus drain/SIGTERM.
- History: the host calls the same `newTerminalHistory(project, id)`, so the log lands at
  the same path and `GET /api/terminal/{id}/history` on the desktop side keeps reading it
  unchanged. The host must `Sync()` the log on a cadence (every N seconds while output
  flows, not only at exit) so a desktop reading the file mid-stream sees recent bytes;
  today sync happens only on close.
- Inventory (`GET /api/terminal`) must include `cols`/`rows` per shell in addition to the
  existing id/title/pid/started_at/attached, because the desktop mirrors them into its
  `terminalRegistry` for Pulse `terminal_screen` and the Processes tab.

Task 3 — Metadata, single instance, discovery (high)
- Metadata file `<data-dir>/terminal-host.json` written atomically (temp + rename),
  mode 0600, after the listener is bound. Fields: protocol, pid, port, token,
  executable_version (`internal/version.Version`), created_at.
- Single instance: hold `internal/filelock` on `<data-dir>/terminal-host.lock` for the
  host's whole lifetime (`TryLock` on an open file, keep the file open). A second host
  that cannot take the lock exits 0 immediately without touching the metadata file.
- Discovery (client side, in `internal/termhost`): `Discover(dataDir) (Metadata, state)`
  where state is one of: `none` (no file), `dead` (pid gone or health unreachable; delete
  the stale file), `live` (health OK and protocol equal), `incompatible` (health OK but
  protocol differs). Health check is `GET /api/termhost/health` with the token and a
  short deadline; never trust pid liveness alone (pid reuse).
- `EnsureRunning(dataDir) (Metadata, error)`: `live` → return it; `none`/`dead` → spawn
  then poll discovery until `live` or a bounded timeout; `incompatible` → return a typed
  error carrying the old metadata (part 03 surfaces it). Never kill or restart an
  incompatible host: it owns the user's shells.
- Spawn: `os.Executable()` + `terminal-host` + flags, `SysProcAttr{Setsid: true}`,
  stdin/stdout/stderr `io.Discard`, `cmd.Env = os.Environ()` from the parent so the
  shells inherit the desktop's hydrated PATH (see `cmd/ocode-desktop/main.go` PATH
  hydration). `Process.Release()` after start. This is a deliberate exception to the
  "spawn through the process supervisor" rule: the host must outlive the supervisor's
  owner. Put that sentence in a comment at the spawn site and in CLAUDE.md (part 04).
- Env caveat to document: shells spawned later inherit the env the host was started
  with, not the env of the desktop instance that happens to be attached.

Task 4 — Lifecycle (high)
- Idle exit: when the shell table becomes empty, arm the idle timer; a new shell cancels
  it; on fire, remove the metadata file, release the lock, exit 0.
- SIGTERM/SIGINT: run the existing `shutdownTerminals` logic with a bounded context
  (reuse `terminalGraceDuration`), remove metadata, exit. SIGHUP is ignored (Setsid
  already detaches from the controlling terminal; macOS logout sends it to leftovers).
- `POST /api/termhost/drain`: same as SIGTERM but requested by the desktop (used when
  the user quits with the setting turned off while host-owned shells exist). Responds
  202 immediately; the caller polls health until unreachable or gives up at its own
  deadline.
- Metadata hygiene on crash: discovery already treats a dead pid or failed health as
  `dead` and deletes the file, so no startup cleanup pass is needed.

Verification for this part
- `go build ./...`; `go vet ./internal/termhost/... ./internal/server/...`.
- Unit: metadata round-trip, discovery state machine against a fake health server,
  single-instance lock refusal, idle timer arm/cancel.
- Manual: `ocode terminal-host -idle 30s` in one shell; `curl` health with the token
  from the metadata file; create a shell with a websocket client; verify it survives
  the client disconnect and the host exits 30s after `DELETE`.

Task 5 — Crash resilience (high, added 2026-10-15)
- Self-healing metadata: a 30 s ticker checks that `terminal-host.json` exists and
  carries this host's pid and port; otherwise rewrite it atomically. This is what makes
  a deleted or clobbered file recoverable while the single-instance lock is held.
- Discovery after a failed spawn (lock held, no metadata): retry discovery for up to
  one re-publish interval before returning an error; never try to kill the lock holder.
- Health payload includes `started_at` and the host's data dir so a desktop pointed at
  a different data dir refuses to attach (defensive; data dirs map 1:1 to hosts).
- Process identity: argv is `<binary> terminal-host ...`, so Activity Monitor / `ps`
  show it plainly. Do not daemonise further (no double fork, no detach from the user
  session): macOS logout must still deliver SIGTERM so shells end cleanly.
- Test: kill -9 the host in the e2e test and assert discovery reports `dead`, deletes
  the file, and `EnsureRunning` spawns a new one; delete the metadata file under a live
  host and assert it is re-published within the cadence (use a short interval flag in
  tests).
