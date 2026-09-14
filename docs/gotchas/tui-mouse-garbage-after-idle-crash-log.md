---
type: Gotcha
title: TUI Leaves Mouse Tracking On After Silent Exit — Diagnose via tui-crash.log
description: After long idle with two TUI instances, the shell prompt appeared under the still-visible frame and every mouse move printed "35;14;1M" garbage. No panic, no zsh job message, no resume summary. Root cause unconfirmed; fd 2 is now mirrored to ~/.local/share/ocode/logs/tui-crash.log with tty job-control state so the next occurrence self-reports.
resource: internal/tui/crash_log.go; internal/tui/tui.go; internal/tui/tty_foreground_unix.go; internal/lsp/daemon_spawn_unix.go; internal/crashguard/crashguard.go
tags:
  - gotcha
  - tui
  - terminal
  - mouse
  - crash
  - job-control
  - diagnostics
timestamp: 2026-09-14T04:38:36Z
---
## Symptom

Reported 2026-09-14 with two TUI instances open in plain local tabs, after a long idle. In one tab the TUI frame was still drawn, the input line still visible, and a zsh prompt (`user@host ocode %`) appeared directly beneath it with zsh's no-trailing-newline `%` marker. Every mouse move appended SGR mouse-motion payloads (`35;14;1M35;18;2M…`) to the prompt line, pushing the frame off the top.

That output pattern means the terminal still has mouse tracking (`?1003h`/`?1006h`) enabled while a cooked-mode shell is reading the tty. Three things were **absent**, and each rules out a candidate:

- No Go panic trace → not a panic in `Update`/`View`, a command goroutine, or a `crashguard.Go` goroutine (all print a stack).
- No `zsh: killed` / `terminated` / `hangup` / `suspended` job line → not SIGKILL, SIGTERM, SIGHUP, or a job stop, if the prompt is the parent shell.
- No `Session ID: … Resume with:` lines → `tui.Run` never returned normally, so no `tea.Quit` path ran (every ocode quit path is user-driven or SIGINT/SIGTERM via `cleanupRequestMsg`; there is no idle timer).

## What was ruled out (2026-09-14 investigation)

- **Advisor failures.** Connection refused, HTTP 401/429, garbage body, empty choices, mid-call cancel: all return errors, and the main loop converts them to an `Error: …` tool message (`agent.go` sequential path). Verified with temporary end-to-end tests.
- **Self-kill via pid 0.** `syscall.Kill(-0, SIGKILL)` would kill ocode's own process group silently, but every `killProcessGroup`/`forceKillProcess`/`terminateProcessTree*` caller guards nil or non-positive pids, and `os.Process.Signal` refuses pid 0.
- **In-process `os.Exit`.** None on the TUI path. The LSP daemon's idle `os.Exit(0)` runs in a separate `ocode lsp-daemon` subprocess.
- **Cross-instance reaping.** `reapStrayChatServer`/`reapStrayEmbedServer` only signal pids that own the model port and match a server cmdline; a TUI never matches. Pid locks are never killed, only reclaimed.
- **Login-shell tty hijack from `internal/shell.Build`** (`$SHELL -l -c`, Setpgid). Probed under a real pty: the foreground group never changed. (`-ilc` does stop the child, which is why `discovery/python_env_unix.go` already uses Setsid.)
- **Bubbletea leaving mouse on during `tea.ExecProcess`.** v2.0.6 `releaseTerminal` runs `renderer.close()`, which writes and flushes the mouse-off and alt-screen-exit sequences. No ocode child inherits the real tty outside `ExecProcess`.

## Remaining hypotheses

1. **Foreground-group loss.** If something moves the terminal's foreground process group away from ocode, every tty read returns EIO (SIGTTIN is ignored via `reclaimTTYForeground`), bubbletea exits with an error, and all restore writes plus the error print are dropped. Exit is silent, exit status is 1, so zsh prints no job message. This is the only theory consistent with every absence above **if the prompt is the parent shell**.
2. **ocode still alive, another reader on the tty.** The user's own read of the screenshot. Would require a shell child attached to the terminal while mouse tracking is on. No such spawn path was found.

The identical `35;14;1M35;18;2M` prefix on both prompt lines suggests a single ZLE buffer being redrawn, not two independent motion streams.

## Instrumentation added (so the next occurrence self-reports)

- `internal/tui/crash_log.go`: at TUI start, fd 2 is `dup2`'d onto `~/.local/share/ocode/logs/tui-crash.log` (append). Go runtime panics, `crashguard.Recover` traces, `log` output routed to stderr, and `main`'s print of the `tui.Run` error all land there. A start line records pid, own pgrp, and the tty foreground pgrp.
- `internal/tui/tui.go`: logs `run returned err=… pgrp=… tty_fg=…` when `p.Run` returns; prints the error to **stdout** with the log path (stderr is now a file). `watchProgramSignals` also catches SIGHUP (graceful quit through bubbletea instead of default silent death) and logs SIGCONT as evidence of a stop/resume, each with tty state.
- `internal/tui/tty_foreground_unix.go`: `ttyForegroundPgrp` (TIOCGPGRP on `/dev/tty`).
- `internal/lsp/daemon_spawn_unix.go`: `Setpgid` → `Setsid`, matching `tool/proc_unix.go` and `discovery/python_env_unix.go`. The daemon is this same binary and spawns the language server in ocode's session; a new session has no controlling terminal, so it cannot touch `/dev/tty`.

## How to diagnose next time

From another tab, **before** pressing anything in the broken one:

```
ps -ax -o pid,pgid,stat,tty,tpgid,command | grep -E 'ocode|zsh' | grep -v grep
tail -20 ~/.local/share/ocode/logs/tui-crash.log
```

- `run returned` line present → ocode exited; the line carries the error and whether `pgrp` still equalled `tty_fg`.
- No such line but ocode is in `ps` → hypothesis 2: it is alive; `tpgid` vs `pgid` shows who owns the terminal.
- A `signal …` line names any SIGHUP/SIGCONT/SIGTERM that arrived.

`reset` restores the tab afterward.

## Related

- `internal/crashguard/crashguard.go` doc comment: the same visible symptom from a raw `go func()` panic, which prints a stack (not observed here).
- `internal/tui/tty_foreground_unix.go` doc comment: two earlier confirmed foreground-group hijacks (local-model server, interactive PATH probe), both fixed with Setsid.
