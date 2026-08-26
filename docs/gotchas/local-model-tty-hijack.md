---
type: Gotcha
title: Local Model Auto-Start Hijacks the Controlling Terminal
description: 'A locally-spawned model server (Setpgid only, same session as ocode) can grab the terminal foreground process group via TIOCSPGRP, crashing the TUI and corrupting the whole shell session'
tags:
  - local-model
  - tui
  - tty
  - process-group
  - sigttou
  - sigttin
  - startup
timestamp: 2026-08-26T09:30:00Z
---
## Local-model child hijacking the terminal foreground process group

**Files:** `internal/tool/proc_unix.go` (`setProcGroup`), `internal/tui/tty_foreground_unix.go` (new)

### Symptom

Starting the TUI (`ocode` with no args) with an enabled local model
(`local/qwen3.8-4b-distill` etc.) would, on some machines, immediately after
`[LOCALMODEL] auto-start: ... ready`:

- Print `bubbletea: error entering raw mode: interrupted system call`, or
  just hang/exit with no UI, and
- `zsh: suspended (tty output)` or `zsh: suspended (tty input)` — the whole
  shell **job** (not just ocode) gets stopped, and
- Afterward, even the parent shell breaks: `zsh: error on TTY read:
  Input/output error`, sometimes reading as "the terminal window just
  closed" to the user, since the corrupted tty state can make the terminal
  emulator give up on the session.

### Root cause

`autoStartEnabledLocalModels` synchronously spawns the local model server
(e.g. `python3 -m mlx_lm.server`) via `ProcessRegistry.StartBackground`,
which used `setProcGroup` = `Setpgid: true` only. That puts the child in its
**own process group** but the **same session** as ocode — so the child (or
something it does during its own startup) can still `open("/dev/tty")` and
get a live handle on the real controlling terminal, since session membership
— not process group — is what gates access to a controlling terminal.

Something in that startup path was confirmed (via targeted `TIOCGPGRP`
instrumentation, temporarily added to `internal/tui/tui.go` at three
checkpoints around `newModel()`/`p.Run()`) issuing `TIOCSPGRP` against that
tty, reassigning the terminal's foreground process group to itself and never
restoring it. The next thing ocode does — `tcsetattr` to enter raw mode for
bubbletea — hits POSIX's unconditional rule: a background process group
changing terminal attributes is sent `SIGTTOU`. That stops the whole job the
shell is tracking. Once the hijacking child later exits, the terminal's
foreground pgrp is left pointing at a dead group, breaking tty I/O
(`SIGTTIN`/`EIO`) for the rest of the shell session too.

### Confirming which fix mattered

Two candidate fixes were tried in sequence; only instrumentation settled it:

1. `signal.Ignore(SIGTTOU)` + one-shot reclaim (`TIOCSPGRP` back to ocode's
   own pgrp) after `newModel()` — got raw-mode entry working, but a *second*
   hijack shortly after (right as bubbletea started reading keystrokes) still
   broke things with `SIGTTIN`.
2. Swapping `signal.Ignore` for `signal.Notify` (catch instead of ignore) —
   made it *worse*: a caught `SIGTTOU` still interrupts `tcsetattr` with
   `EINTR`, which bubbletea does not retry, so raw-mode entry itself started
   failing every time.
3. `Setsid` instead of `Setpgid` in `setProcGroup` — put the child in a
   **new session** with no controlling terminal at all, so `open("/dev/tty")`
   fails outright for it and any descendant.

Re-running with `TIOCGPGRP` logged at `before-newModel` / `after-newModel` /
`before-p.Run` confirmed: with `Setsid`, `tty_fg_pgrp` never drifts from
ocode's own pgrp at any checkpoint, even with two local models auto-starting.
`Setsid` is the actual fix; `reclaimTTYForeground`'s `signal.Ignore(SIGTTOU)`
+ one-shot reclaim never fires anymore and is kept only as a backstop for
some other spawn path that isn't (yet) using `setProcGroup`.

### Fix

- `internal/tool/proc_unix.go`: `setProcGroup` now sets `Setsid: true`
  instead of `Setpgid: true`. Safe because every caller already redirects the
  child's stdio (pipes or `/dev/null`) — none of them rely on inheriting the
  controlling terminal — and `Setsid` still leaves `pgid == pid`, so existing
  `syscall.Kill(-pid, ...)` group-kill logic (`killProcessGroup`,
  `terminateProcess`, `forceKillProcess`) is unaffected.
- `internal/tui/tty_foreground_unix.go` (new, POSIX-only, with a Windows
  no-op counterpart): `reclaimTTYForeground()` — `signal.Ignore(SIGTTOU)`
  plus a one-shot `TIOCSPGRP` reassert back to ocode's own pgrp after
  `newModel()`, before bubbletea starts. Defense-in-depth only.
- Unrelated but adjacent: `tui.Run` now also rejects starting the TUI at all
  when stdin/stdout aren't a real terminal (`isatty`), with a clear error
  message instead of leaking bubbletea's raw `could not open TTY: open
  /dev/tty: device not configured` — a different failure mode (no
  controlling terminal whatsoever, e.g. run from a non-interactive tool
  shell) that was initially confused with this one before real-terminal
  repro evidence (`zsh: suspended ...`) ruled it out.

### Lesson

When a background child's stdio is fully redirected, `Setpgid` alone is not
enough isolation from the controlling terminal — only `Setsid` guarantees
`open("/dev/tty")` fails for that child and everything it spawns. Reactive
fixes (catch-and-reclaim on `SIGTTOU`/`SIGTTIN`) are strictly worse than
they look: `signal.Ignore` can rescue a `write`/`tcsetattr` from a background
pgrp, but `signal.Notify` cannot — a caught signal still returns `EINTR` to
the interrupted syscall, and not every caller retries on `EINTR`.
