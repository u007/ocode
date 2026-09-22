---
type: Concept
title: Web Chat Composer Input History (↑/↓ Navigation)
description: 'Per-tab ↑/↓ history navigation in the web/desktop chat composer, parity with the TUI''s Up/Down : Navigate input history. Local per-tab list, append-on-submit, caret-gated walk, queue precedence, rekey/clear lifecycle.'
resource: web/src/lib/tabInputHistory.ts; web/src/components/Chat/ChatInput.tsx (handleKeyDown ~:738, handleSend ~:580); web/src/lib/sessionEvents.ts; web/src/App.tsx
tags:
  - web
  - chat
  - composer
  - history
  - keyboard
  - TUI-parity
timestamp: 2026-09-22T02:38:46Z
---
# Web Chat Composer Input History (↑/↓ Navigation)

Per-tab history recall in the web/desktop chat composer via ↑/↓ — parity with the TUI's `Up/Down : Navigate input history` (`internal/tui/model.go`).

## What it is

A module-level `Map<tabId, string[]>` (`web/src/lib/tabInputHistory.ts`) storing the user's own submitted chat input per tab. Pressing ↑ in the composer walks **back** through what was sent; ↓ walks **forward** and, past the newest entry, restores the draft that was in the box when navigation began.

The composer reads it imperatively on a key press and never renders from it, so no subscriber plumbing is needed (refs, not React state — a key press does not re-render the memoized composer).

## Design contract

| Rule | Detail |
|---|---|
| **Source** | Local per-tab list appended at the single submit choke point `handleSend` (~:594). **Not** derived from the transcript. |
| **Why local, not transcript** | A transcript walk would surface the `!shell` output wrapper the composer sends and huge server-assembled prompts (`/standup`-style), none of which is user-typed input. |
| **Append rules** (TUI parity) | Empty text skipped; `!`-prefixed shell commands skipped; immediate repeat of the previous entry collapsed; per-tab cap `MAX_INPUT_HISTORY = 200` (oldest dropped). |
| **Single recording** | Recorded once at `handleSend`, even for messages queued while busy (queueing happens inside `handleSend`); the later auto-drain sends without touching history. |
| **Walk entry** | Bare ↑/↓ only (Shift/Alt/Meta/Ctrl excluded). ↑ enters the walk only when caret is on the first line (`selectionStart === selectionEnd`, no `\n` above) and no slash menu open. |
| **Walk continuation** | Once `historyIndexRef !== -1`, arrows keep walking regardless of caret position (a recalled entry's caret is not meaningful). |
| **Draft stash/restore** | Entering the walk stashes the in-progress draft (`historyDraftRef`); ↓ past the newest restores it and exits. |
| **Clamping** | Clamped at oldest (can't walk past the first entry). |
| **Queue precedence** | With an empty box, ↑ first recalls a still-queued item (`restoreLastQueued`), only then enters sent history. |
| **Walk reset** | Any non-history input mutation (`updateDraft` — typing, slash selection, queued recall) resets the walk (`historyIndexRef = -1`). `applyHistoryValue` (setInput+setDraft) deliberately does **not** reset it. |
| **State location** | Refs (`historyIndexRef`, `historyDraftRef`), not React state, so a key press does not re-render the memoized composer. |

## Lifecycle

| Event | Action | Where |
|---|---|---|
| `new-*` → real session (first send) | `rekeyInputHistory(tempId, realId)` — old entries (chronologically older) are **prepended** | `sessionEvents.ts:226` (off `session_started`); `App.tsx:790` (`rekeySession`, first-send path) |
| `/reset-id` | `rekeyInputHistory(oldId, newId)` — same prepend rule; re-key happens **before** return so the tab already points at the new id | `sessionEvents.ts:266` (off `session_rekeyed`); `App.tsx:1019` (reset-id caller of `rekeySession`) |
| Tab close | `clearInputHistory(activeTabId)` | `App.tsx:770` (`closeActiveChat` / Cmd+W teardown) |

## Tests

- `web/src/lib/tabInputHistory.test.ts` (9 tests) — records/reads, skips empty/`!`/whitespace, collapses immediate repeats, trims, caps at 200 (drops oldest), rekeys with oldest-first merge, no-op for same-id/missing rekey, clear drops the list.
- `web/src/components/Chat/ChatInput.history.test.tsx` (8 tests) — ↑/↓ walk + clamp, draft stash/restore, queue precedence over sent history on empty box, caret-gate (no hijack when on a later line), ↓ does nothing when not walking, modifier keys ignored, `!shell` excluded from recall, duplicate submission collapsed.

## Related

- [Web Composer Quick-Actions Strip](web-composer-quick-actions.md) — quick-action pills that queue through the same `handleSend` pipeline
- [Web UI Global Keyboard Shortcuts](web-keyboard-shortcuts.md) — global + composer-local key bindings
- [Interrupted Turn Notice](interrupted-turn-notice.md) — how turns interrupted after an answered ask surface Resume, which the composer also sends through `handleSend`
