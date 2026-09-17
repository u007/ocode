---
type: Gotcha
title: Git action errors "come out then disappear by themselves" — TUI truncation, sticky status, and background-refresh error clearing
description: 'Root causes of git push errors vanishing instantly: TUI status bar appended the message after a ~250-col hint line so width-truncation deleted it; status messages auto-cleared; gitRunInDir/cmdNetworkOp discarded stderr; and the web GitPanel poll (10s) plus git_status events cleared the error with setError(null). Includes the sticky-error / timeout-success contract.'
resource: internal/tui/git_model.go; web/src/components/Git/GitPanel.tsx
tags:
  - git
  - tui
  - web
  - ux
  - gotcha
  - stderr
  - truncation
timestamp: 2026-09-16T06:56:57Z
---
# Git action errors "come out then disappear by themselves"

Fixed 2026-09-16. Symptom reported as: **"git push comes out error and disappears by itself."**
Two independent root causes (one TUI, one web) plus a missing success signal and lost stderr.

> **Note on scope:** `docs/acp-zed-spec.md` (the file referenced when the bug was reported) is the
> Zed / Agent Client Protocol spec and contains **no git-action content** — it is unrelated to this
> behaviour. This gotcha documents it instead.

## KEY LESSON / GENERAL RULE

1. **When a status/error banner shares a line with a long static hint string, render the message with
   priority and truncate the hint.** Appending the message *after* the hint lets width-truncation
   silently delete the message — the failure "disappears" with no error on screen.
2. **Any background refresh (poll or event) must never clear a user-action error.** If a periodic
   poll calls `setError(null)` at the top of its fetch, error feedback vanishes on a timer.

## 1. TUI — status bar truncation (`internal/tui/git_model.go`, `gitModel.View`)

The status bar was built as:

```go
statusBar = hints + "   " + errorStyle.Render(m.statusMsg)
statusBar = lipgloss.NewStyle().Width(w).MaxHeight(1).Render(statusBar)
```

`renderHints()` for the Changes panel is **~250 columns** (measured), so after width-truncation the
status message was cut off entirely — the error was never visible on screen.

**Fix** (`git_model.go:~2235-2258`): render the status message **first** (hints become the
`default` fallback branch), truncate with `ansi.Truncate(m.statusMsg, w, "…")`, and colour it red when
it is an error. The `filterActive` branch still renders filter state + hints.

**Regression tests:** `TestGitStatusMessageVisibleDespiteLongHints` (fails at the old code; renders
`m.View(80, 20, ...)` and asserts the message appears) and `TestGitStatusMessageTruncatedToWidth`.

## 2. TUI — status lifetime: sticky errors, auto-clearing successes

Errors must be **sticky** (stay until the next git action); successes auto-clear after **5s** (the
on-screen toast). Helpers in `internal/tui/git_model.go`:

- `gitStatusIsError(text)` — classifies from the **text** (`"failed"`, `"error"`, `"cannot "` prefix,
  `"required"` suffix), **not** a stored flag, so it cannot go stale when other code assigns
  `statusMsg` directly (`git_model.go:660`).
- `setGitStatus(text)` — records terminal state; errors are sticky + raise an OS notification,
  successes schedule the timeout (`git_model.go:690`).
- `gitOKDone(text)` / `gitErrDone(text)` — set status then `cmdRefresh()` (`git_model.go:703-711`).
- `gitStatusTimeout()` — 5s → `gitStatusTimeoutMsg{seq}` (`git_model.go:674`).
- `statusSeq` field — a stale timeout from an earlier action cannot wipe a newer message
  (`git_model.go:136-139`, checked at `~:869`).

Every git mutation routes through these: push/pull/fetch, commit, stage/unstage, discard, stash
push/pop/apply/drop, branch create/delete/merge/checkout, hunk apply, gitignore.

**Tests:** `TestGitErrorMessageIsSticky`, `TestGitSuccessAutoClearsAfterTimeout`,
`TestGitStaleStatusTimeoutDoesNotClearNewMessage`, `TestGitOSNotificationOnlyForErrors`,
`TestGitStatusClassifiesErrors`.

## 3. TUI — lost stderr

`gitRunInDir` and `gitRunTimeout` used `cmd.Output()`, which **discards stderr**, so failures surfaced
only as `exit status 128` with no reason. `cmdNetworkOp` read stdout only (`git_model.go:~618`).

**Fix:** fold stderr into the error text; `cmdNetworkOp` uses `cmd.CombinedOutput()`
(`git_model.go:625-628`). **Test:** `TestGitRunInDirSurfacesStderr` (asserts
`"not a git repository"` appears in the error).

## 4. TUI — OS notification policy

Failures additionally raise a desktop notification via `github.com/gen2brain/beeep` (already a dep,
used for the bell fallback). **Successes deliberately do NOT raise an OS notification** — they would
spam the notification centre; the on-screen 5s status message is the toast. `notifyGitAction` is a
**package-level var** so tests can stub it and never spawn `osascript`/`notify-send`
(`git_model.go:648`).

## 5. WEB — same bug (`web/src/components/Git/GitPanel.tsx`)

`load()` began with `setError(null)` and ran on a **10-second `REFRESH_INTERVAL` poll** and on every
`git_status` eventBus event — so a failed push's error was wiped within seconds.

**Fix:** `load({ background: true })` for the poll and bus-driven refreshes (`GitPanel.tsx:204`,
`:225-226`). Background loads never clear a user-visible error (`if (!background) setError(null)`,
`:165`) and never surface a transient poll failure (`:189-191`). Added a green success notice
(`data-testid="git-notice"`, `:527`) that auto-clears after 5 seconds.

**Test:** web regression `"keeps a failed push error visible across background refreshes"` — verified
to fail at the old code via `emitGitStatus()` (invokes the real registered `git_status` handler), and
`GitPanel.test.tsx` is 11/11.

## Validation

- `internal/tui` suite green
- `internal/server -run TestGit` green
- web `tsc` clean; `GitPanel.test.tsx` 11/11

## Related gotchas

- [`sandbox-git-push-ssh-agent-tty.md`](sandbox-git-push-ssh-agent-tty.md) — why sandbox `git push` can fail (SSH agent / TTY)
- [`git-ext-transport-auto-allow-bypass.md`](git-ext-transport-auto-allow-bypass.md) — git auto-permission security bypass
