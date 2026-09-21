---
type: Concept
title: Session-bound dialogs scope to their chat session
description: 'Rule: session-owned ask dialogs only mount when that session''s Chat sub-tab is on screen; pending asks persist invisibly via sidebar Bell + attention chime when off-surface.'
tags:
  - web-ui
  - dialogs
  - session-scoping
  - ask
  - permissions
timestamp: 2026-09-21T03:59:54Z
---
# Session-bound dialogs scope to their chat session

## The rule
A dialog owned by a chat session — the permission ask, the `question` tool ask, and any future session-scoped prompt — may only be MOUNTED while that session's chat surface is actually on screen. Implemented as the single pure predicate `sessionAskSurfaceVisible({ activeView, focusedKind, activeSubTab })` in `web/src/lib/dialogScope.ts`, which is true only when `activeView === "sessions" && focusedKind === "chat" && activeSubTab === "chat"`. `web/src/App.tsx` gates both dialog mounts on it: `{pendingPermission && sessionAskVisible && (<PermissionDialog .../>)}` and the same for `pendingQuestion`/`QuestionDialog` (search for `sessionAskVisible`). `sessionAskVisible` is computed in HomeApp from `activeSessionTab?.activeSubTab` (App.tsx, just after `const activeSessionTab = tabs.find(...)`).

## Why (the bug this prevents)
`web/src/components/ui/dialog.tsx` renders Radix `DialogPortal` + a `fixed inset-0 z-50` overlay with a focus trap. Mounting an ask dialog off-surface therefore blocked the ENTIRE app (sidebar, top tabs, files, other panels) for a session the user was not even looking at. The root cause: `activeTabId` (`web/src/stores/projectStore.tsx:106-109`, `activeTabId(state)`) tracks the active project's active tab independently of `activeView`, `focusedKind`, and the session sub-tab. So a pending ask for the active tab opened a full-screen modal even while the user was on Files/Git/Cron/Assets/Settings, on the terminal half of Sessions, or on a non-Chat sub-tab of the same session.

## Why hiding is safe (the ask is not lost)
- The pending ask lives in the per-session chat-store slice (`pendingPermission`/`pendingQuestion` in `web/src/stores/chatStore.tsx`, selected by `useChat(sessionId)` via `getSessionSlice`). Unmounting the dialog does not clear it, so returning to that session's Chat sub-tab re-opens the prompt.
- Background-session signals already exist and must not be removed: the project sidebar Bell `pendingCount` badge ("N waiting for input") in `web/src/components/Layout/ProjectSidebar.tsx` (aggregates `slice.pendingPermission || slice.pendingQuestion`), and the `AttentionSoundBridge` chime (`web/src/components/common/AttentionSoundBridge.tsx`).
- Note: `useChat(activeTabId)` in HomeApp already meant only the ACTIVE tab's ask had a dialog; this rule adds the missing surface/sub-tab dimension.

## Deliberate consequence
While an ask is pending and the user is not on that session's Chat sub-tab, no dialog is shown — the sidebar Bell badge + attention chime are the out-of-sight signal.

## Tests / how to verify
- `web/src/lib/dialogScope.test.ts` — 14 cases pinning the predicate matrix (all non-sessions views, terminal/browser halves, every non-chat sub-tab, and no-active-tab).
- `web/src/App.askDialogScope.test.tsx` — 5 App-level cases: dialogs present on the Chat surface; absent on another top-level view; absent on the terminal half; absent on a non-chat sub-tab; re-opened after a `tabFocusActions.request({kind:"chat"})` returns the user to chat. Mutation-verified: removing `&& sessionAskVisible` failed exactly the 4 gating cases while the positive control still passed.
- Full web suite: 194 files / 1678 tests green at the time of writing.
