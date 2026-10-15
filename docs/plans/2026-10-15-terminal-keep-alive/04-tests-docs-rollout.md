Part 04 — Integration tests, docs and CLAUDE.md invariants, rollout, follow-ups
Status: designed, not started.

Context needed for this part alone: an opt-in detached `terminal-host` process
(`internal/termhost`, hidden subcommand in both binaries) owns terminal ptys; the
server proxies websockets to it when `terminal_keep_alive_on_quit` is on, mirrors its
inventory into `terminalRegistry`, and skips terminating host-owned shells at quit when
the setting is on. History logs are written by whichever process owns the pty, at the
unchanged path.

Task 1 — End-to-end test in `internal/server` (high)
- `terminal_keepalive_e2e_test.go`: start a real host in-process on a temp data dir
  (call the host's run function in a goroutine with a test listener; do not re-exec),
  build a `Handler` pointed at it with the setting on, create a terminal through the
  real `HandleTerminalWS` with `httptest` + websocket client, run a shell loop that
  prints a counter every 100ms, close the browser socket, call `shutdownTerminals` with
  a short context, assert the host still lists the shell and its pid is alive, build a
  SECOND fresh `Handler` against the same host, reattach by id, assert the attach frame
  is `resumed:true` and the replay contains counter values higher than the last one
  seen before the "quit". Then `DELETE`, assert the host's table empties and the idle
  timer is armed.
- Same test with the setting off at the second shutdown: assert drain is requested
  and the pid is gone within the grace window.
- Run under `go test -race`. Skip (with the reason) when `/dev/ptmx` is unavailable, the
  same way existing pty tests do.

Task 2 — Discovery and spawn tests in `internal/termhost` (standard)
- Real re-exec test behind the usual integration guard: build is `go test`'s own test
  binary, so test the spawn helper with an env override that points `executable` at a
  tiny helper mode of the test binary (pattern used by `os/exec` tests via `TestMain`
  dispatch) rather than the real `ocode`. Assert Setsid (child's sid != parent's), the
  metadata file appears with mode 0600, and the parent's exit does not kill the child.
- Incompatible protocol: write metadata with protocol+1 and a fake health server;
  assert `EnsureRunning` returns the typed error with the old metadata and does not
  spawn.

Task 3 — Docs (standard)
- `docs/architecture/terminal-detach-reattach.md`: add a "Host mode" section: the two
  owners, the placement and keep-or-kill rules, no detach TTL under the host, protocol
  versioning, env inheritance caveat.
- `docs/terminal-history-persistence-and-restore.md`: the log is written by the pty
  owner (server or host); periodic sync cadence; path unchanged.
- `docs/concepts/data-storage-layout.md`: `terminal-host.json`, `terminal-host.lock`,
  `logs/terminal-host.log`.
- `docs/web-desktop-parity-todo.md` terminal line: mention survival across app
  restart when the setting is on.
- `README.md` feature list: one line. `skills/ocode-usage/SKILL.md`: one line under
  terminals (read the exact file before editing; it is a shared artifact).
- Config key reference page: `terminal_keep_alive_on_quit`.

Task 4 — CLAUDE.md invariants (standard)
- Add a short block "Terminal host (keep-alive)" with these rules only:
  the pty core (`serveTerminalPTY`) has exactly two callers, in-process and host, and
  must never be copied; the host is discovered, never restarted, on protocol mismatch;
  `shutdownTerminals` must never signal the host's shells when the setting is on; the
  host spawn is the one sanctioned exception to the process-supervisor rule and why;
  the host trusts the caller for project validation, so every gate stays on the
  server side before forwarding; inventory carries cols/rows because the registry is
  mirrored, not observed.

Task 5 — Rollout and manual verification (standard)
- Default off. No migration. Ship behind the toggle in one release; flip no defaults.
- Manual script (desktop build with `make desktop`): toggle on; open two terminals in
  two projects; start `ping` in one and a `while` loop in the other; Cmd+Q; confirm the
  overlay count reads 2; check `ps -o pid,sid,command` shows the host and both shells
  with the host's sid; relaunch; both tabs reattach with output produced while closed;
  close one tab (host still up); toggle off; Cmd+Q; confirm the host drains and exits;
  relaunch; the remaining tab reports the shell ended and offers a fresh shell.
- Upgrade check: run the manual script, then install a build with `Protocol` bumped,
  relaunch, confirm the Settings notice names the older host and the shells are left
  running; exit them by hand; confirm the old host exits after idle.

Follow-ups (not in this plan; add to TODO.md when the plan starts)
- Windows: ConPTY cannot be handed between processes; would need the host to own the
  console from the start (same design, different spawn), tracked separately.
- `ocode serve --remote`: the host works there too because the handler is shared, but
  it is untested in this plan; add it to the remote persistent-sessions doc when
  verified.
- Agent `bash` tool background jobs are a different lifecycle and are not covered.
