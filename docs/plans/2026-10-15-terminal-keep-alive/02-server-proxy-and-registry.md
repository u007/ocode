Part 02 — `internal/server`: placement switch, websocket proxy, registry mirror, shutdown
Status: designed, not started. Platform: macOS/Linux only (`handler_terminal_windows.go`
stubs stay as they are).

Context needed for this part alone: a detached terminal host process (hidden subcommand
`terminal-host`, package `internal/termhost`) owns ptys and serves the same
`/api/terminal/ws`, `GET /api/terminal`, `DELETE /api/terminal/{id}` surface on a
loopback port with its own bearer token, discovered through
`termhost.EnsureRunning(dataDir)`. The user setting `terminal_keep_alive_on_quit`
(ocodeconfig, default false) decides placement at spawn time and keep-or-kill at quit
time. `HandleTerminalWS` has been split into gates + `serveTerminalPTY` core.

Task 1 — Placement switch (standard)
- In `Handler`, add `terminalHostClient` (nil when the setting is off or the host is not
  usable) resolved lazily on first terminal create, re-resolved when nil (so turning the
  setting on mid-session takes effect without restart, same pattern as the nil decision
  client rule in CLAUDE.md). Config is read through the existing ocodeconfig loader; do
  not cache it at boot.
- `HandleTerminalWS` after the gates: if setting on and the host is `live`, proxy;
  if setting on and the host is `incompatible`, fall through to in-process AND record a
  one-line notice on the handler (`terminalHostNotice`) that part 03 surfaces; if the
  host cannot be started, return 503 with a clear error (no silent in-process fallback
  when the user explicitly asked for keep-alive; log the error with what was attempted).
- Reattach must work even when the setting is off now but was on earlier: before
  spawning in-process for a known `terminal_id`, ask the host inventory whether it owns
  that id and proxy if so. This is what keeps a tab that was created under the setting
  from being replaced by a fresh shell after the user toggles it off.

Task 2 — Websocket proxy (high)
- New file `handler_terminal_proxy.go`: upgrade the browser connection with
  `terminalUpgrader` (unchanged), dial the host with `gorilla/websocket` `Dialer`
  (`Authorization: Bearer <host token>`, same query string the browser sent, plus
  `project` already validated by the gates; for remote shells forward `host` and
  `project_path`). Copy frames in both directions preserving frame type (binary vs
  text) and close reasons. Two goroutines through `crashguard.Go`; either side closing
  closes the other.
- The host emits the existing `attach`/`detach` control frames itself, so the proxy
  never synthesises protocol frames. One exception: if the dial fails after the browser
  upgrade succeeded, send the existing detach frame shape with a new reason
  `host-unavailable` and close (the SPA's reconnect/backoff then applies).
- Supersede semantics stay on the host (it already closes the previous attachment and
  sends `superseded`). The proxy holds no per-terminal state of its own.
- Ping/keepalive: the existing server→browser ping loop in `serveTerminalSocket` must
  also run on the proxied browser socket; host→proxy pings are handled by the dialer's
  default pong handler.

Task 3 — Kill, list, history forwarding (standard)
- `HandleTerminalKill`: after the existing gates, if the host owns the id → forward
  `DELETE` to the host, then remove the local registry entry and notify; else existing
  in-process path. Deleting history on explicit close stays where it is today.
- `HandleTerminalList`: merge host inventory (filtered by the requested project) with
  in-process sessions; sort by `started_at` as today.
- `HandleTerminalHistory`: unchanged (disk).
- Remote-project shells (`host=` query, ssh launched in a local pty): these are local
  ptys and follow the same placement rule. Nothing special, but call it out in tests.

Task 4 — Registry mirror for Pulse and Processes tab (high)
- `terminalRegistry` (`terminal_registry.go`) is consumed by `emitters.go` (processes
  SSE), `HandleTerminalProcesses`, `pulse_terminal_screen.go` (needs cols/rows and
  liveness) and `pulse_terminals.go` (generation-keyed rows). Keep it the single source
  on the desktop side and reconcile it from the host inventory: on handler construction,
  after every proxied create/kill, and on each processes-emitter tick while a host client
  exists. Reconcile = add missing ids (project, pid, cols, rows), update sizes, remove
  ids the host no longer lists; bump `generation` only on an actual change.
- Resize frames pass through the proxy unparsed; sizes reach the registry via the
  inventory reconcile, which is why the inventory carries cols/rows (part 01).

Task 5 — Shutdown changes (high)
- `shutdownTerminals`: still seals the table and terminates every **in-process** session.
  For the host: if setting on → do nothing (proxies die with the HTTP server; the host
  keeps shells with no TTL). If setting off and the host owns shells → call
  `POST /api/termhost/drain` and wait on the shutdown context for health to go
  unreachable; log and proceed on timeout. Never SIGKILL the host from here.
- Expose `Handler.KeptAliveTerminalCount()` (host inventory size when setting on) for
  the desktop quit overlay (part 03).
- `Server.Shutdown` ordering is unchanged; the only new behaviour is inside
  `shutdownTerminals`.

Verification for this part
- `go test -race ./internal/server/ -run 'Terminal'` (pty tests need `/dev/ptmx`; a
  sandbox that denies it fails them at pristine HEAD too, see CLAUDE.md worktree notes).
- New tests: proxy copies binary and text frames both ways; dial failure after upgrade
  sends `host-unavailable` and closes; kill forwards and clears registry; list merges;
  registry reconcile adds/updates/removes and bumps generation only on change; shutdown
  with setting on leaves the fake host untouched, with setting off calls drain.

Task 6 — Orphan adoption on relaunch (standard, added 2026-10-15)
- On the first host inventory reconcile after handler construction, compare host
  shells against `internal/termtabs` entries for each project. A host shell with no tab
  (created just before a crash, before the tab PUT landed) is adopted: append a tab
  entry for its project with the shell's `terminal_id` and OSC title (fallback: the
  shell's start time), through the existing merge-PUT store path, never a whole-map
  write. Shells whose project is no longer a registered root are left running and
  listed only under the Settings host notice (part 03); never killed by adoption.
- Test: fake host lists two shells, tab store has one; after reconcile the store has
  both and the new entry carries the host's title.
