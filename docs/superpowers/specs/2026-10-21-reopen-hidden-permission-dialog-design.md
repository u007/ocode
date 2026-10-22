---
type: Design
title: Reopen a locally hidden permission dialog
description: 'Implemented design spec: locally hide the PermissionDialog via X/Escape/Hide with request-ID-scoped hidden state and reopen it from the transcript tool card.'
tags:
  - web
  - desktop
  - dialog
  - permission
  - design
  - state
timestamp: 2026-10-21T00:00:00Z
---

# Reopen a locally hidden permission dialog — design

Date: 2026-10-21
Status: **implemented (2026-10-21).** The request-ID-scoped hidden state, the local X/Escape/Hide hide, the Hide footer button, and the transcript/Open-permission reopen all shipped; see *As-built* below.

Mirrors the existing question-dialog hide/reopen pattern
(`docs/superpowers/specs/2026-09-25-reopen-hidden-question-dialog-design.md`),
applied to `PermissionDialog` instead of `QuestionDialog`.

## As-built

- Store: `SessionSlice.hiddenPermissionRequestId` (`web/src/stores/chatStore.tsx`) with
  `PERMISSION_HIDE` / `PERMISSION_SHOW` actions and stale-ID guards in the reducer.
  `PERMISSION_REQUEST` preserves a hide for the same `request_id`; `PERMISSION_RESOLVED`
  clears only the matching id; reset clears it and `REKEY_SESSION` drops it.
- App: visibility is derived as designed (`web/src/App.tsx` —
  `pendingPermission && hiddenPermissionRequestId !== pendingPermission.request_id &&
  sessionAskVisible`).
- Dialog: `PermissionDialog` takes `onHide` (X/Escape/Hide, local, no network). Overlay
  clicks fall back to deny for callers that don't pass `onHide` (Pulse), keeping their
  existing contract.
- Reopen: the transcript tool card and the live-buffer tool part render an **Open
  permission** button that dispatches `PERMISSION_SHOW` (`web/src/components/Chat/ChatPanel.tsx`
  and `web/src/components/Chat/TurnParts.tsx`).
- Tests: new `PERMISSION_HIDE`/`PERMISSION_SHOW` cases in
  `web/src/stores/chatStore.test.tsx`; no backend change, as the design required.

## Problem

The web/desktop `PermissionDialog` offers only Allow / Deny / Always-allow. X/Escape
previously submitted a server-side deny, which releases the pending ask and leaves no
way to answer it later. The user wants to dismiss the dialog, keep the turn paused,
explore files, then reopen the same permission ask to answer later.

## Approved behavior

1. X, Escape, and the new `Hide` footer button locally hide the dialog without calling
   the server; the server prompt remains pending and the turn stays paused.
2. The footer destructive action (`Deny` / `Withhold`) retains the existing final
   server-side dismissal semantics.
3. Overlay click stays suppressed while a decision is in flight; when not loading it
   hides locally (callers passing `onHide`) or denies (callers that don't, e.g. Pulse).
4. The tool card's `Open permission` button clears the local hidden state and reopens
   the same request, in both the committed transcript and the mid-turn live buffer.
   A hidden ask with neither card (a sub-agent ask: its request id is the child's, and
   it writes no `PERMISSION_ASK` sentinel) gets a standalone `Open permission` row at
   the end of the transcript, so it is never unreachable until reload.
5. A new request opens normally. Recovery/reconciliation of the SAME request must not
   defeat a local hide. This requires request-ID-scoped hidden state rather than a
   global transient boolean.
6. A final allow/deny, resolved/superseded request, reset, or rekey clears the hidden
   state with pending state.
7. Navigating away does not discard the hidden state; returning to Chat keeps the
   permission pending and the user can use `Open permission`.

## State / data flow

- Add request-ID-scoped `hiddenPermissionRequestId: string | null` to
  `SessionSlice` (`web/src/stores/chatStore.tsx`).
- Add explicit store actions `PERMISSION_HIDE` and `PERMISSION_SHOW`; use request IDs
  and stale-ID guards.
- `PERMISSION_REQUEST` hydrates `pendingPermission`. It opens a new ID, but preserves
  hidden state when re-requesting the same already-hidden ID. `PERMISSION_SHOW` is the
  explicit user reopen path.
- `PERMISSION_HIDE` marks only the matching pending request hidden and is a no-op for
  stale IDs.
- `PERMISSION_RESOLVED` clears hidden state only for the matching current request;
  broad resolution/reset/rekey clears it safely.
- App derives dialog visibility from
  `pendingPermission && hiddenPermissionRequestId !== pendingPermission.request_id &&
  sessionAskVisible`.
- `PermissionDialog` (`web/src/components/Chat/PermissionDialog.tsx`) receives `onHide`
  (X/Escape/Hide). `onHide` is synchronous/local; no network call.
- ChatPanel's existing `Open question`-style flow now also dispatches `PERMISSION_SHOW`
  for permission tool cards, in both the committed transcript (render-entries memo
  derives `pendingPermission` from `hiddenPermissionRequestId`/`pendingPermission`) and
  the live buffer (`onOpenPermission`).

## Error / consistency handling

- Local hide cannot fail and performs no network call.
- Final `Deny` keeps current loading suppression, error reporting, stale 404/409
  handling, optimistic transcript rewrite, and server broadcast behavior.
- A recovered pending ask can still be answered after reopening; if it became stale,
  the existing resolve path's 404/409 logic clears it.
- Hidden state is session-local UI state and must never be persisted as prompt state.

## Components / files expected

- `web/src/stores/chatStore.tsx` — SessionSlice: field, actions, reducer,
  initialization, reset, rekey.
- `web/src/hooks/useChat.ts` — selector and hide/show callbacks.
- `web/src/App.tsx` — dialog open derivation.
- `web/src/components/Chat/PermissionDialog.tsx` — callbacks/labels.
- `web/src/components/Chat/ChatPanel.tsx` — open button dispatches show.
- `web/src/components/Chat/TurnParts.tsx` — ToolBlock `onOpenPermission` prop.
- Focused tests for each of the above.

No backend endpoint/schema/API change. The server's `livePendingAsks` already reports
the pending permission while the client hides it, and `agent_session.go` skips new
turn dispatch while `livePendingAsks(sessionID) != nil`, so the turn stays paused.

## Testing / TDD

Write failing tests first.

- Store tests: hide/show matching IDs, stale hide/show no-op, same-ID recovery
  remains hidden, new-ID recovery opens, resolved/reset/rekey cleanup.
- PermissionDialog tests: X/Escape/Hide call `onHide` and not the deny path; Deny
  calls the resolve path; loading behavior unchanged.
- ChatPanel test: `Open permission` action shows same hidden request (committed
  transcript and live buffer).
- App/scope regression: hidden ask does not mount while another surface is active
  and returns as hidden, not auto-open.
- Run targeted Vitest, typecheck, and relevant full Chat/App suites. Account for
  known unrelated concurrent-WIP failures by reporting exact fail sets.

## Documentation

- Update inline comments and `skills/ocode-web/SKILL.md` during implementation.
