# Transcript Mouse Selection — Design Spec

**Date:** 2026-05-18  
**Status:** Approved

## Overview

Add character-level mouse selection to the chat transcript viewport in the ocode TUI. When the user clicks and drags in the transcript area, the selected text is highlighted and copied to the clipboard on mouse release.

## Background

The ocode TUI uses bubbletea with `MouseModeCellMotion`. When mouse mode is active the terminal's native text selection is disabled, so selection must be implemented in application code. The transcript viewport (`m.viewport`) displays ANSI-styled content built from chat messages.

## Architecture

### New state on `model`

```
sel struct {
    active   bool   // a selection exists (shown highlighted)
    dragging bool   // mouse button currently held
    startLine int   // content-space line (0-based, includes scroll offset)
    startCol  int   // visual column (0-based)
    endLine   int
    endCol    int
}
rawTranscriptLines []string  // ANSI-stripped lines of the current transcript
```

`rawTranscriptLines` is populated in `rebuildTranscript()` after `SetContent` so we always have the plain-text source for both coordinate mapping and clipboard extraction.

### New file: `internal/tui/selection.go`

Four pure functions, no model state:

| Function | Purpose |
|---|---|
| `visualColToRuneIdx(line string, visualCol int) int` | Walk a plain-text line counting visual widths (handles wide runes via `go-runewidth`), return byte index |
| `applySelectionHighlight(lines []string, rawLines []string, startLine, startCol, endLine, endCol int) []string` | Given rendered ANSI lines and the selection range, inject a reverse-video highlight (`\x1b[7m…\x1b[27m`) over the selected character range on each affected line |
| `extractSelectionText(rawLines []string, startLine, startCol, endLine, endCol int) string` | Return the selected plain-text substring joined with `\n` |
| `stripANSI(s string) string` | Remove all `\x1b[…m` escape sequences; used to produce `rawTranscriptLines` |

### Coordinate mapping

The transcript viewport's top-left content corner is at:
- **Y:** `headerHeight + 1` (same formula used by `transcriptScrollbarHit`)
- **X:** `0`

Screen → content space:
```
contentLine = (mouse.Y - viewportContentTopY()) + m.viewport.YOffset
visualCol   = mouse.X
```

A helper `viewportContentTopY() int` is added to `model` (one-liner, same as scrollbar top).

A selection click is only accepted when `m.activeTab == tabChat` and `mouse.X < m.mainScrollbarX()` and the computed `contentLine` is within `[0, len(rawTranscriptLines))`.

### Mouse event changes (`handleMouseAction` / `handleMouseMotion`)

**`MouseClickMsg` (pressed=true):**
1. If in transcript area: clear existing selection, set `sel.dragging=true`, compute and store `sel.startLine/Col`, set `sel.endLine/Col = sel.startLine/Col`, set `sel.active=false`, call `applyOrClearSelectionHighlight()`, return handled.
2. Otherwise: if selection is active, clear it and reapply content — fall through to existing handlers.

**`MouseMotionMsg` (button held):**
1. If `sel.dragging`: update `sel.endLine/Col`, set `sel.active=true`, call `applyOrClearSelectionHighlight()`, return handled.

**`MouseReleaseMsg`:**
1. If `sel.dragging`: set `sel.dragging=false`. If `sel.active`, call `extractSelectionText` and write to clipboard via `clipboard.WriteAll`. Keep `sel.active=true` so the highlight remains visible.

### `applyOrClearSelectionHighlight()` (method on `*model`)

Called after any selection state change:
- If `!sel.active`: call `m.viewport.SetContent(wrapView(currentRawANSIContent, m.viewport.Width()))` — restores un-highlighted content.
- If `sel.active`: normalise `(startLine,startCol)` / `(endLine,endCol)` so start ≤ end, call `applySelectionHighlight`, call `m.viewport.SetContent` with the highlighted version.

The current ANSI content (pre-highlight) is stored in `m.transcriptContent string`, set in `rebuildTranscript()` before `SetContent`. This avoids re-rendering all messages on every mouse motion event.

### `rebuildTranscript()` changes

After building the full ANSI string `b.String()`:
1. Store `m.transcriptContent = wrapView(b.String(), m.viewport.Width())`
2. Populate `m.rawTranscriptLines = strings.Split(stripANSI(m.transcriptContent), "\n")`
3. Call `m.viewport.SetContent(m.transcriptContent)`
4. Clear any active selection (`m.sel = selectionState{}`)

### Clipboard

Use `github.com/atotto/clipboard` (already in `go.mod`):
```go
clipboard.WriteAll(text)
```
Errors are silently ignored (clipboard may be unavailable in headless environments).

## Edge Cases

| Case | Handling |
|---|---|
| Selection start == end (single click, no drag) | `sel.active` stays false; no highlight, no copy |
| Drag upward (endLine < startLine) | `applySelectionHighlight` normalises to min/max before processing |
| Drag to scrollbar column | `mouse.X < m.mainScrollbarX()` guard prevents it |
| Viewport scrolls while dragging | `contentLine` formula uses live `m.viewport.YOffset`, so coordinates stay correct |
| Wide characters (CJK, emoji) | `visualColToRuneIdx` uses `go-runewidth` (already a transitive dep via lipgloss) |
| New message arrives during selection | `rebuildTranscript()` clears selection — clean slate |
| Mouse disabled in config | No change; `mouseEnabled()` already gates all mouse handling |
| Clipboard unavailable (SSH, headless) | `clipboard.WriteAll` error is discarded silently |

## Files Changed

| File | Change |
|---|---|
| `internal/tui/selection.go` | New — pure helper functions |
| `internal/tui/model.go` | Add `sel`, `transcriptContent`, `rawTranscriptLines` fields; update `rebuildTranscript`; update mouse handlers; add `viewportContentTopY`, `applyOrClearSelectionHighlight` |

## Out of Scope

- Log viewport selection (can be added later with same helpers)
- Keyboard-driven selection
- Selection persistence across sessions
