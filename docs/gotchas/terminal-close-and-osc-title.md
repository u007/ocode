---
type: Gotcha
title: Terminal close semantics and OSC title source
description: Close-vs-detach-vs-app-exit matrix, ghost-socket disposed flag, OSC title parsing and fallback chain for remote terminal rows.
tags:
  - gotcha
  - terminal
  - remote
  - OSC title
  - close
  - dispose
  - reattach
timestamp: 2026-09-18T17:06:06Z
---

# Terminal close semantics and OSC title source

## OSC title parsing

Server-side in `internal/server/terminal_osc_title.go`: `oscTitleScanner.feed()` is a state machine that consumes raw pty bytes. It recognises OSC sequences terminated by BEL (`0x07`) or ST (`ESC \`), ignores non-title OSC codes, sanitises control characters to spaces, collapses whitespace, and caps at 80 runes (`terminalTitleMaxRunes`). Sequences that span multiple pty reads are handled correctly.

`terminalSession.recordLocked(p)` (called under `s.mu` for every pty chunk, including replayed queued bytes) feeds the scanner and sets `s.title`. `snapshot()` / `GET /api/terminal` returns the real title.

### Title fallback chain

```
RemoteProjectStatus row title = t.title          // live OSC title from server
                               ?? localPersistedTitle  // from terminalPersistence localStorage
                               ?? "Terminal <id>"
```

`attachTerminal` also receives the resolved title so a shell idle before the server started shows the persisted title immediately.

**Deployment caveat:** the parser runs in the HOST's `ocode serve --remote`; an already-running host keeps the old binary until sidebar Restart or a version bump.

## Close vs detach vs app-exit matrix

| Action | Local tab | Persisted entry | Host pty | Shell alive? |
|--------|-----------|----------------|----------|-------------|
| Tab **X** (sidebar kill) | removed | removed | DELETE proxied via `killTerminal` | No — server receives kill, shell is reaped |
| Desktop **app exit** | removed | preserved | **not killed** — `shutdownTerminals` only reaps local ptys | Yes — remote shell is child of detached host server, survives up to 24 h detach TTL |
| Laptop **sleep/wake** | disconnected | preserved | WebSocket reconnects after backoff reset | Yes — pty on host is unattended |

## Ghost-socket disposed flag

**Bug it fixes:** `TerminalPanel` cleanup closes the socket, but the async `onclose` handler fires *after* cleanup and arms a reconnect — the ghost socket reattaches and respawns a shell under the closed tab's id.

**Fix:** `TerminalPanel.tsx` sets a `disposed` flag in the unmount cleanup. The `onclose` / `onerror` / wake handlers bail immediately when `disposed` is true. The socket close is idempotent, so this is safe even if cleanup races.

## Remote kill via store

`killTerminal(projectPath, id, host)` in `terminalStore.tsx` removes the local tab and persisted entry, then awaits the proxied DELETE with the `X-Ocode-Project` header. Previously a raw DELETE left the local tab in place, causing reattach + respawn under the closed tab's id. `RemoteProjectStatus.killTerminal` now routes through this store action.

## Source files

- `internal/server/terminal_osc_title.go` — OSC title scanner
- `internal/server/terminal_session.go` — `recordLocked`, `snapshot`, `title` field
- `web/src/components/Layout/RemoteProjectStatus.tsx` — title fallback, kill routing
- `web/src/components/Terminal/TerminalPanel.tsx` — `disposed` flag
- `web/src/stores/terminalStore.tsx` — `killTerminal`, `removeTerminalLocally`
- `internal/server/terminal_osc_title_test.go`, `web/src/stores/terminalStore.closeKill.test.tsx`, `web/src/components/Layout/RemoteProjectStatus.test.tsx`, `web/src/components/Terminal/TerminalPanel.wake.test.tsx` — tests
