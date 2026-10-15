Part 03 — Config key, API pair, Settings toggle, desktop quit UX, outdated-host notice
Status: designed, not started.

Context needed for this part alone: terminal shells can be hosted by a detached
`terminal-host` process so they survive an ocode.app quit/relaunch. The server side
decides placement from `terminal_keep_alive_on_quit` (ocodeconfig, default false) at
spawn time, and keeps host-owned shells at quit iff the setting is on at that moment.
Shells already running in-process when the setting is turned on are not rescued. The
host is never restarted on a version/protocol mismatch; the server records a notice.

Task 1 — Config key (standard)
- `internal/config/ocodeconfig.go`: add `TerminalKeepAliveOnQuit bool` on the struct, the
  `*bool` file field with json tag `terminal_keep_alive_on_quit,omitempty`, the raw-key
  consume block, and the save payload line. Copy the `auto_share_on_start` plumbing
  exactly (struct ~line 903, file field ~line 1293, consume ~line 1990, payload ~line
  2690). Saving must go through the targeted load-modify-write saver, never a whole
  in-memory `SaveOcodeConfig` snapshot (concurrent sessions erase each other's
  permissions otherwise, see memory note on config concurrent writes).
- Add the key to the config docs page that lists ocodeconfig keys (find the page that
  documents `auto_share_on_start` and add the new key beside it).

Task 2 — API pair (standard)
- `internal/server/server.go`: `GET/PUT /api/config/ocode/terminal-keep-alive` next to
  the auto-share pair (~line 527), same middleware wrapper. Handlers in a new
  `handler_terminal_keepalive.go`: GET returns `{enabled, supported, host}` where
  `supported` is false on Windows, and `host` is `{state, version, shells}` from
  `termhost.Discover` (`none|dead|live|incompatible`, executable version, shell count)
  without starting a host. PUT body `{enabled}` writes the key and returns the same
  shape. A PUT that turns the setting off while host shells exist does NOT drain them;
  drain happens at quit (part 02 shutdown rule). Say that in the response doc comment.
- `web/src/api/client.ts`: `getTerminalKeepAlive` / `setTerminalKeepAlive` beside
  `getAutoShareConfig` / `setAutoShareConfig` (~line 2644).

Task 3 — Settings toggle (standard)
- New `web/src/components/Settings/TerminalKeepAliveForm.tsx`, mounted in the Settings
  Features section (same place and toggle primitive as `AutoShareForm.tsx`; use the
  existing switch/checkbox component from the UI library, never a hand-rolled one).
- Copy: label "Keep terminals running after quit". Helper text: "Terminals opened while
  this is on keep running when ocode quits and reconnect when it reopens. Terminals
  already open now will still close on quit." When `host.state === "incompatible"`:
  "N terminals are still running under an older ocode. They cannot be reattached; they
  end when their program exits." When `supported` is false, do not render the form.
- Show the live count: "N terminals currently kept alive" when `host.state === "live"`.
- Test: `TerminalKeepAliveForm.test.tsx` covering render/hide by `supported`, toggle
  calls PUT, incompatible notice renders. Typecheck with `bun check .` in `web/`.

Task 4 — Desktop quit UX (standard)
- `cmd/ocode-desktop/main.go` `OnShutdown`: before `Srv.Shutdown`, read
  `Handler.KeptAliveTerminalCount()` through a small accessor on `*server.Server`. When
  > 0, set the window title to "ocode — Quitting (N terminals kept running)" instead of
  the generic "Waiting for processes…" text, and pass the count into
  `showQuittingIndicator` so the overlay says the same. No new behaviour when 0.
- The quit guard (`internal/desktop/quit_guard.go`) is unchanged: keep-alive never
  blocks quit.

Task 5 — Boot (standard)
- No host start at desktop boot. The host starts lazily on the first terminal create
  with the setting on (part 02). On relaunch, the SPA restores tabs from
  `GET /api/terminal-tabs` and reconnects each by `terminal_id`; the server's reattach
  check finds the id in the host inventory and proxies, which yields the existing
  `resumed:true` replay. Verify in the SPA that `TerminalPanel.tsx` treats a
  `resumed:true` after a full app relaunch exactly like after a page reload (clears the
  history-restored scrollback once, then paints the replay); no SPA change expected.

Verification for this part
- `go test ./internal/config/ ./internal/server/ -run 'KeepAlive|TerminalKeepAlive'`.
- `cd web && bun check . && bun test TerminalKeepAliveForm`.
- Manual: toggle on in Settings, open a terminal, run a long loop printing timestamps,
  Cmd+Q, confirm the overlay count, relaunch, confirm the same tab shows lines printed
  while the app was closed and the loop is still running.

Task 6 — Stop host now (standard, added 2026-10-15)
- `POST /api/config/ocode/terminal-keep-alive/stop-host`: calls the host drain
  endpoint and returns 202; the form polls GET until `host.state` is `none`/`dead`.
- Settings form: a destructive "Stop terminal host now" button with a confirm dialog
  from the UI library ("Ends N running terminals. Open tabs will show the shell ended."),
  shown only when `host.state` is `live` or `incompatible` (the latter is the only way
  to end an older host's shells from the UI). Also show pid and version beside the
  state so a user can find it in Activity Monitor.
