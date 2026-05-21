# Ambient File Context Injection

**Date:** 2026-05-21  
**Status:** Draft

## Problem

Users can explicitly attach files to the conversation via the `a` key (creates a permanent system message in history). But there's no lightweight way to say "I'm looking at this file/these lines right now — use it as context for my next message."

## Goal

When a user sends a message, automatically inject a system message containing their current file/line selection from the Files tab or Git tab. No extra keypress required. Context persists until the user deselects.

## Scope

- Files tab: add space-key multi-select (new), ambient system message injection
- Git tab: existing multi-select already present, add injection
- Line highlights: inject selected lines with line numbers and file path header
- No highlight: inject file path only (lightweight)

## What Gets Injected

As a system message appended right after the existing context/rules message on every `askAgent()` call (not in snapshot/compaction path):

```
[Selected context]

## Files
- src/internal/tui/model.go
- src/internal/tui/git_model.go

Highlighted lines — src/internal/tui/model.go:
  42: func buildFileContext() string {
  43:     return ""
  44: }

## Git diff
- internal/tui/model.go (modified)
- internal/tui/git_model.go (staged)

Highlighted diff lines — internal/tui/model.go:
  12: +func newThing() {
  13: +    return nil
  14: +}
```

Rules:
- File path only when no lines highlighted
- Line highlight: path header + `  N: content` per line
- Git diff file: include status (modified/staged/untracked)
- Only inject if something is actually selected (method returns `""` → no system message added)

## Architecture

### New method: `(m model) buildSelectionContext() string`

In `model.go`. Reads:
- `m.files.selectedFiles` (map[int]bool → node indices → `m.files.nodes[i].path`)
- `m.filesSel` (active highlight → extract from `m.files.previewRawLines`)
- `m.files.previewPath` (only used to provide the highlight's filename label)
- `m.git.selectedFiles` (map[int]bool → `m.git.currentFileList()[i]`)
- `m.git.filesCursor` + `m.git.currentFileList()` (current diff file)
- `m.gitSel` (active highlight → extract from `m.git.diffRawLines`)

Returns empty string if nothing selected.

### Injection sites

Inject in all live-request builders — not `buildAgentMessagesSnapshot` (compaction snapshot must stay pure history):

| Function | Line | Action |
|---|---|---|
| `askAgent()` | ~3287 | append system msg after ctx/rules |
| `sendCustomCommandPrompt()` | ~2897 | same |
| Agent step at ~3408 | same |
| Agent step at ~3834 | same |

Extract a shared helper `m.appendSelectionMsg(msgs []agent.Message) []agent.Message` to avoid repeating the logic.

### Files tab multi-select

Add to `filesModel`:
- `selectedFiles map[int]bool` field
- Space key in tree panel: toggle `selectedFiles[m.cursor]`
- Shift+↑/↓: extend selection (match git tab behavior)
- Esc: clear `selectedFiles` (before existing esc logic)
- Status bar: show `N selected` when active

### Esc clear order

On both tabs, esc should clear in priority order (first match wins, then normal esc):
1. If line highlight active → clear it
2. Else if file multi-select active → clear it  
3. Else → normal `handleEscKey()` logic

This lets users peel back layers without losing everything at once.

### Clearing highlights on file change

Existing behavior already clears `m.filesSel` on file change. No change needed there.

## What This Is NOT

- Not a replacement for the explicit `a` key attach (which persists permanently in history)
- Not injecting full file content when only a file is selected (path only)
- Not injecting into `buildAgentMessagesSnapshot` (compaction stays clean)

## Open Questions

None — all resolved in design session.
