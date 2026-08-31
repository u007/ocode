# Terminal Shells Survive Page Reload (Detach / Reattach)

**Status:** Implemented 2026-08-31. Supersedes the earlier "one websocket ==
one pty, never resumed" contract.

## Problem

In the desktop app (and web UI) a right-click → Reload of the webview
unmounted every `TerminalPanel`, which closed its WebSocket; the server
treated socket close as "kill the shell". Every terminal tab's process died
on every reload, even though the Go server itself kept running.

## Design

Server (`internal/server/terminal_session.go`, `terminal_session_table.go`,
`handler_terminal.go`):

- Each pty shell is a `terminalSession` keyed by the frontend's `terminal_id`
  (the `TerminalPanel` `id`, already persisted in localStorage per project).
  `Handler.terminalSessions` owns them; `terminalProcs` (Processes tab
  registry) is unchanged.
- The pty → socket pump is a per-session goroutine (`readLoop`) that
  outlives any socket. It appends output to a **256 KB replay buffer**
  (trimmed to a line boundary) and forwards it to the attached socket.
- **Socket close = detach**, not kill. A 30 min TTL timer
  (`terminalDetachTTL`) arms; if nobody reattaches, the shell is reaped via
  `terminateProcessTree` (SIGTERM → 2 s → SIGKILL).
- A new `GET /api/terminal/ws?terminal_id=X` for a live session
  **reattaches**: the old socket (if any) is closed, the timer cancelled, and
  the replay buffer sent. Reattach is refused with **409** if the session's
  project root differs from the requested `project_path` (same trust
  boundary as spawn).
- Anonymous sockets (no `terminal_id`) keep the old behaviour: shell dies
  with the socket.
- **Explicit close** is now a separate call: `DELETE /api/terminal/{id}`
  (`HandleTerminalKill`, 204 / 404). The frontend store's `closeTerminal`
  calls it; `shutdownTerminals` on app exit still sweeps every pid.
- Framing change: the first server → client frame is a **text** control
  frame `{"type":"attach","resumed":bool}`; all later frames are binary pty
  output.

Frontend (`web/src/components/Terminal/TerminalPanel.tsx`,
`web/src/stores/terminalStore.tsx`):

- On `attach` with `resumed=true` the panel `term.reset()`s the
  localStorage-restored scrollback so the server replay is not shown twice.
  With `resumed=false` (shell gone: server restarted or TTL expired) the
  saved text stays as history, exactly as before.
- `closeTerminal` fires the DELETE; a mere unmount/remount only detaches.

## Gotchas

- Interactive shells ignore SIGTERM, so an explicit close takes ~2 s to
  actually reap the process (grace then SIGKILL). The DELETE returns
  immediately; the Processes tab catches up on the next tick.
- Two browser windows sharing localStorage share terminal ids: opening the
  same project in both makes the second window steal the shell from the
  first (attach supersedes). Acceptable for v1; a per-window id suffix would
  fix it.
- The replay is raw bytes, so a full-screen TUI that was mid-frame repaints
  from the replay and is then corrected by the resize (SIGWINCH) that the
  panel sends on open.

## Tests

`internal/server/terminal_session_test.go` covers reattach (same pid, replay
includes output produced while detached), project-mismatch 409, TTL reap,
anonymous kill-on-close, and the DELETE endpoint.
`web/src/components/Terminal/TerminalTabs.test.tsx` asserts close issues the
DELETE.
