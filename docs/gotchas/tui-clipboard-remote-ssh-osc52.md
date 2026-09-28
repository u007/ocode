---
type: Gotcha
title: TUI copy over SSH never reaches the local clipboard — route every copy through OSC 52 (`copyToClipboard`)
description: A TUI that copies via a local clipboard utility (pbcopy/xclip/xsel/wl-copy) silently fails on a remote SSH host; route every TUI copy through copyToClipboard → tea.SetClipboard (OSC 52) with the local utility as fallback.
tags:
  - gotcha
  - tui
  - clipboard
  - osc52
  - ssh
  - remote
  - bubbletea
timestamp: 2026-09-28T23:14:18Z
resource: internal/tui/clipboard.go; internal/tui/clipboard_test.go; internal/tui/model.go; internal/tui/files_model.go; internal/tui/review_overlay.go; internal/tui/sidebar_cwd_test.go; skills/ocode-tui/SKILL.md
---
## Symptom

Reported 2026-09-29: "terminal claude code copy on remote ssh project terminal never gets copied." A text selection copied in the ocode Bubble Tea TUI (mouse-selection release, `ctrl+y`, review copy, session-id copy) never reached the user's **local** clipboard when the TUI ran on a remote host over SSH — silently, with no error.

## Root cause

The TUI copied via `github.com/atotto/clipboard`'s `clipboard.WriteAll` at ~13 call sites (`internal/tui/model.go` mouse-selection release block, `files_model.go` `copySelectedPath`, `review_overlay.go` `copyReviewToClipboard`, chat/log `ctrl+y`, session-id copy). `atotto/clipboard` shells out to a **local** utility (`pbcopy` / `xclip` / `xsel` / `wl-copy`). On a headless remote host none exist, so `clipboard_unix.go` returns `errors.New("No clipboard utilities available…")` and — at the transcript release site — the error was discarded (`_ = clipboard.WriteAll(text)`) → silent no-op. At best it writes the **remote machine's** clipboard, never the user's. A local clipboard utility can only touch the machine the process runs on.

Mouse tracking was investigated and ruled out: a browser reproduction showed xterm only starts a selection when the app does NOT hold mouse tracking, and Claude Code 2.1.283 tested via pty enabled only `?1004h` (focus reporting), no mouse modes. The terminal emulator in the user's setup is Supacode, which bundles Ghostty (supports OSC 52).

## Fix

New `internal/tui/clipboard.go` exposes the single copy entry point:

```go
func copyToClipboard(text string) tea.Cmd {
    if text == "" { return nil }
    return tea.Batch(tea.SetClipboard(text), writeLocalClipboard(text))
}
```

- `tea.SetClipboard` (bubbletea v2, `charm.land/bubbletea/v2@v2.0.6/clipboard.go`) produces `setClipboardMsg` → `p.execute(ansi.SetSystemClipboard(...))` = an **OSC 52** sequence (`\x1b]52;c;<base64>\x07`). The user's terminal emulator executes it, so the copy reaches the **local** clipboard across the SSH connection.
- `writeLocalClipboard` keeps the `atotto/clipboard` write as a fallback for terminals without OSC 52; its error is logged, not surfaced, because OSC 52 has no acknowledgement to combine results with.

Every call site now returns `copyToClipboard(text)` instead of calling `clipboard.WriteAll` directly; the `atotto/clipboard` import survives only in `clipboard.go`. Tests: `internal/tui/clipboard_test.go` (unit + model wiring; asserts the emitted msg type is `tea.setClipboardMsg` and its payload), and `TestSidebarCWDDragStillSelects` in `sidebar_cwd_test.go` was updated because "non-nil cmd" no longer means the cwd row was clicked.

## Rule

**Never call `clipboard.WriteAll` directly from a TUI view — route through `copyToClipboard`.** Any copy path in the TUI must emit OSC 52 so it works when ocode runs on a remote host; the local utility is fallback only.

## Files

`internal/tui/clipboard.go`, `internal/tui/clipboard_test.go`, `internal/tui/model.go`, `internal/tui/files_model.go`, `internal/tui/review_overlay.go`, `internal/tui/sidebar_cwd_test.go`, `skills/ocode-tui/SKILL.md` (§5 mouse-selection recipe + files-to-know).

## Related

- `gotchas/radix-modal-focus-trap-breaks-execCommand-clipboard.md` and `gotchas/monaco-webkit-clipboard-workaround.md` — the browser/web side of clipboard failures; this gotcha is the TUI/SSH side.