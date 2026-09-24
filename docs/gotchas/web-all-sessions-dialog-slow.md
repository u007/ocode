---
type: Gotcha
title: Web "All sessions" dialog slow to open (render bottleneck, child filtering + pagination)
description: 'Get-rid of All-sessions dialog 0.73s popup: render-of-6650-rows root cause, child-session filter + 50/page client window, measured numbers, legacy-scan follow-up.'
tags:
  - web
  - sessions
  - performance
  - session-dialog
  - pagination
timestamp: 2026-09-24T06:12:33Z
---
# Web "All sessions" dialog was slow to pop up (2026-09-24, fixed)

## Symptom
Opening the "All sessions" dialog (`web/src/components/Layout/SessionDialog.tsx`, opened by the "All sessions" button in `web/src/components/Layout/UnifiedTabBar.tsx`) felt laggy — ~0.7s to appear on a large project.

## Root cause: render, not fetch
Measured live on a project with **6,646 sessions** (`GET /api/projects/sessions` = 949 KB): the dialog mounted **6,650 row buttons in ~0.73s**. The bottleneck was client-side rendering of every row, not the network fetch. 443 of the 6,646 sessions were subagent/child-context sessions that shouldn't be listed at all.

## Fixes

### 1. Child-session filtering
Subagent/child sessions are minted by `internal/agent/child_session.go:10` as:

```
<parentSessionID>_child_<agentName>_<ts>
```

e.g. `ses_2026-09-24-133707-d5eda0e8_child_context_2026-09-24-133958`. They are execution detail, not resumable chats.

- Helper: `isChildSessionId(id)` in `web/src/lib/sessionId.ts:15` — an `_child_` infix test (infix, so `ses_...-childless` is NOT matched).
- `SessionDialog.tsx:42` filters `projectSessions` through it before search/render.

### 2. Infinite-scroll pagination (client-side window)
- `SESSION_DIALOG_PAGE_SIZE = 50` (`SessionDialog.tsx:17`); only the first page renders.
- An `IntersectionObserver` sentinel (below the list) appends the next page; a visible **"Load more (N remaining)"** button is the fallback where `IntersectionObserver` is undefined (jsdom).
- **Reset contract**: `visibleCount` resets to 50 on dialog open and on every search change; a background list revalidation only *clamps* it (never resets), so the user keeps their place.
- The store cache (`projectStore.projectSessions`) and the unpaginated endpoint are unchanged — the window is purely client-side, keeping warm cached lists at instant first paint and staying compatible with older remote servers.

## Measured results (real browser)
- Popup: **0.73s → 0.11s**.
- `Load more (6153 remaining)`: 50 + 6153 = 6,203 = 6,646 − 443 children exactly.
- Search resets to page one.

## Tests
- `web/src/components/Layout/SessionDialog.test.tsx` — child hidden; first page renders 50 rows; Load more shows; both mutation-verified (removing the filter/slice fails them).
- `web/src/lib/sessionId.test.ts` — `_child_` infix match, `childless` negative, empty-id negative.

## Known follow-up (NOT fixed)
The server still re-reads ~5,527 legacy `.json`/`.ojsonl` session files per list request (**~390ms**) and returns the full payload. Indexing legacy sessions is a separate optimization; the client fix deliberately does not touch the endpoint.
