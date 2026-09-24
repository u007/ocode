---
type: Gotcha
title: 'Side Pane StateKey Convention: Per-Session Scoping'
description: '"The right-hand Browser/Preview side pane''s open state and previewed shell state are keyed per session surface (`side:chat:<id>` / `side:term:<id>`), not per project. Opening the pane in one chat never opens it in another; returning to a session restores its own pane state. When a session id changes (new chat → real ses_ id, or /reset-id), the pane state is rekeyed to follow it."'
resource: web/src/lib/sidePaneState.ts; web/src/lib/browserStore.ts; web/src/components/Preview/sidebarPreviewState.ts; web/src/components/Preview/PreviewHost.tsx; web/src/components/Layout/UnifiedTabBar.tsx
tags:
  - gotcha
  - web
  - session
  - side-pane
  - browser
  - preview
  - state-key
timestamp: 2026-09-24T04:41:08Z
---
## StateKey naming

Every side pane surface has a unique `sideStateKey` that serves as the
identity for both the live browser surface and the persisted preview
shell:

| Surface | Key pattern | Helper |
|---------|-------------|--------|
| Chat session | `side:chat:<sessionId>` | `sideChatKey(sessionId)` |
| Terminal | `side:term:<terminalId>` | `sideTermKey(terminalId)` |

Both helpers live in `web/src/lib/sidePaneState.ts`. The id is the real
`ses_...` / terminal id, not the temporary id assigned when a chat is
created (a new chat gets its real id on the first message).

## Per-session open/collapsed state

The pane's `panelOpen` (browser vs preview surface) and collapsed state
live in `browserStore` (`web/src/lib/browserStore.ts`) keyed by the
`sideStateKey`. Each session has its own entry — opening the pane in chat
A never opens it in chat B. Switching away and back to the same session
restores exactly what that session had. The old cross-session propagation
effect and its `panelClosedByUser` workaround in `App.tsx` were removed
because they were unnecessary: each session's state is already isolated.

`browserActions.rekey(oldKey, newKey)` (in `browserStore.ts`) moves a
surface's live state to a new key; it is a no-op if the source is absent
or the target already exists.

## Per-session preview shell persistence

The preview shell (which surface is active + which file/page is shown)
persists in localStorage `ocode.ui.sidebarPreview.v2`, keyed by
`sideStateKey` via `loadSidebarPreviewState(stateKey)` /
`saveSidebarPreviewState(stateKey, state)` in
`web/src/components/Preview/sidebarPreviewState.ts`. Old `v1` entries
(keyed `host::root`) are intentionally orphaned and are no longer read.

Per-file viewer state (zoom, scroll position, page index) is separate and
still keyed per file per project in `web/src/lib/previewViewState.ts`
(`ocode.ui.previewViewState.v1`, key `host::root::path`).

## Rekey on session-id change

A chat starts with a temporary id that becomes its real `ses_...` id on
the first message. `/reset-id` also rekeys. Both paths call
`rekeySidePaneState(oldId, newId)` (`web/src/lib/sidePaneState.ts`),
which moves the live browser surface (via `browserActions.rekey`) and the
persisted preview shell (via `rekeySidebarPreviewState`) to the new key.
This is called from:

- `App.rekeySession` — when a new chat's temp id becomes its real id
- `sessionEvents.ts` — in both the `session_started` and `session_rekeyed`
  event handlers

Without rekeying, the pane state would be orphaned under the temp id and
lost on the session-id change.

## Two layers, two scopes

The side pane has two layers of state, each with its own scope:

- **Open/collapsed + surface type** → per session, in `browserStore`
  (`side:chat:<id>` / `side:term:<id>`)
- **Previewed file/page (shell)** → per session, localStorage v2
  (`side:chat:<id>` / `side:term:<id>`)
- **Viewer detail (zoom, scroll, page)** → per file per project, v1
  (`host::root::path` in `previewViewState.ts`)

When debugging pane state issues, always ask: *which session and which
surface key is this state keyed under?*

## Tests

- `web/src/lib/sidePaneState.test.tsx` (stateKey helpers + rekey)
- `web/src/lib/browserStore.test.tsx` (rekey move + no-op cases)
- `web/src/components/Preview/sidebarPreviewState.test.tsx` (v2 key, load/save/rekey)
- `web/src/components/Preview/PreviewHost.test.tsx` (per-session isolation within same project)
- `web/src/App.sidePaneScope.test.tsx` (end-to-end: open s1 → switch to s2 → absent; back to s1 → restored)
