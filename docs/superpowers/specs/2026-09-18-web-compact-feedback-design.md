---
type: Decision
title: Web Compact Feedback Design
description: 'Approved design for web/desktop /compact feedback beside composer: queued/running/complete/error states, queue semantics, test plan. Implementation complete.'
tags:
  - web
  - compact
  - design
  - approved
  - implemented
  - frontend
  - tui
timestamp: 2026-09-17T16:39:22Z
---
# Web Compact Feedback Design

**Type:** Decision
**Description:** Approved design for web/desktop /compact feedback beside composer: queued/running/complete/error states, queue semantics, test plan.
**Tags:** web, compact, design, approved, frontend, tui

---

# Web Compact Feedback Design — Approved, Implementation Complete

**Date:** 2026-09-18
**Status:** Approved — Implementation complete
**Scope:** Frontend web/desktop `/compact` feedback beside composer, outside transcript scroll

## Summary

Add visible feedback for the `/compact` slash command in the web/desktop UI. Previously `/compact` ran silently — the user saw no indication that compaction was in progress, when it completed, or whether it failed. This adds a persistent, non-intrusive status indicator beside the composer area.

## Implementation

### Files

| File | Role |
|------|------|
| `web/src/lib/compactionState.ts` | Session-keyed `useSyncExternalStore` state (active/complete/error) — in-memory, survives SET_MESSAGES/remount, does not survive page reload |
| `web/src/components/Chat/CompactionStatus.tsx` | Composer-area spinner, elapsed time, status text, and dismiss control for terminal state |
| `web/src/components/Chat/ChatInput.tsx` | Renders `CompactionStatus` above input; derives queued state from `tabQueue`; blocks/serializes follow-on work during active compaction |
| `web/src/lib/commands.ts` | `handleCompact` updates state and preserves host API routing |
| `web/src/components/Chat/ChatPanel.tsx` | Old compact banner removed |

### Design

#### Location
Status bar renders **above the composer input**, outside the transcript scroll area. It never injects into the conversation transcript.

#### States

| State | Visual |
|-------|--------|
| **Queued** | Status text (e.g. "Queued for compaction…") — shown when `/compact` is entered while a turn is active |
| **Running** | Spinner + elapsed time counter (e.g. "Compacting… 12s"). Never times out — spinner persists until completion or error |
| **Complete** | Before/after message counts displayed (e.g. "Compacted: 42 messages → 8 messages"). Auto-clears after a few seconds |
| **Error** | Persistent error message with dismiss control. Does NOT auto-clear — user must dismiss explicitly |

#### Queue Semantics

Preserves all existing safe queue behavior:
- `/compact` entered while the agent is streaming is queued and executed after the current turn ends
- Queued status is visible immediately in the status bar
- **Queue removal or recall clears the queued status** — if the user cancels or the queued item is removed, the status indicator disappears
- Session/project-host scoping is preserved — compaction runs against the correct session
- The safe queue does not interrupt the current turn

#### API Routing

No backend protocol or algorithm changes. The compaction endpoint and behavior remain unchanged. This is a frontend-only feedback layer.

#### Error Handling

Errors are **persistent and dismissible** — they stay on screen until the user clicks dismiss. This prevents the common UX problem where error feedback disappears before the user notices it.

## Scope and Limitations

### What This Covers
- Manual `/compact` via the web/desktop UI
- Queued/running/complete/error feedback states
- Session-scoped state (survives remount and SET_MESSAGES within a session)

### Explicit Limitations
- **Manual web `/compact` only** — this does not cover backend auto-compaction (ratio-triggered). Auto-compaction is a backend process with its own lifecycle; this feedback layer is not wired to it.
- **Does not survive page reload** — state is in-memory (`useSyncExternalStore`); a full page refresh loses the indicator. This is intentional: the compaction itself completes server-side regardless.
- **No cross-browser/tab persistence** — each tab maintains its own state independently.
- **Does not change backend protocol or algorithm** — the compaction endpoint, summary model routing, and anchor-update logic are untouched.

### What This Does NOT Change
- Backend compaction algorithm or protocol
- Transcript content (no injection of compaction status into the conversation)
- Queue execution order or semantics
- Session isolation guarantees
- Project-host scoping

## Test Plan

Covered by `web/src/components/Chat/ChatInput.compaction.test.tsx`:

| Scenario | Assertion |
|----------|-----------|
| `/compact` during idle | Status transitions queued → running → complete |
| `/compact` during active turn | Shows queued, then running, then complete |
| Compaction takes >8 seconds | Spinner/elapsed continues without timeout |
| Session isolation | Compaction feedback scoped to correct session |
| API routing | No change to existing endpoint behavior |
| Error state | Error persists until explicit dismiss |
| Queue removal | Queued status clears when item removed |
| Recall (new `/compact`) | Previous queued status clears |
| Mount/unmount | State cleaned up on unmount |
| Message replacement (SET_MESSAGES) | State survives transcript replacement |

**Verification:** Full suite (141 files / 1211 tests) + typecheck + build passed at time of implementation.
