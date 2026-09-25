---
type: Design
title: Reopen a locally hidden question dialog
description: 'Design spec: locally hide the QuestionDialog via X/Escape with request-ID-scoped hidden state and reopen it from the transcript''s Open question button.'
tags:
  - web
  - desktop
  - dialog
  - question
  - design
  - state
timestamp: 2026-09-25T07:19:27Z
---
# Reopen a locally hidden question dialog — design

Date: 2026-09-25
Status: approved (design approved 2026-09-25; implementation pending)

## Problem

The web/desktop `QuestionDialog` already has an `Open question` button for an
unanswered question sentinel, but X/Escape/Cancel all call the server
dismissal endpoint (`/api/questions/cancel`). That rewrites the sentinel to
`QuestionDismissedResult`, clears `pendingQuestion`, releases the pending ask,
and makes reopening impossible. The user wants a button to redisplay an
accidentally dismissed question.

## Approved behavior

1. X and Escape locally hide the dialog without calling
   `/api/questions/cancel`; the server prompt remains pending.
2. The footer destructive action is renamed from `Cancel` to `Don't answer`
   and retains the existing final server-side dismissal semantics.
3. Overlay click remains prevented.
4. The transcript's existing `Open question` button clears the local hidden
   state and reopens the same request.
5. A new request opens normally. Recovery/reconciliation of the SAME request
   must not defeat a local hide. This requires request-ID-scoped hidden state
   rather than a global transient boolean.
6. A final answer, final don't-answer, resolved/superseded request, reset, or
   rekey clears the hidden state with pending state.
7. Navigating away does not discard the hidden state; returning to Chat keeps
   the question pending and the user can use `Open question`.

## State / data flow

- Add request-ID-scoped `hiddenQuestionRequestId: string | null` (or
  equivalently precise naming) to `SessionSlice` (`web/src/stores/chatStore.tsx`).
- Add explicit store actions such as `QUESTION_HIDE` and `QUESTION_SHOW`; use
  request IDs and stale-ID guards.
- `QUESTION_REQUEST` hydrates `pendingQuestion`. It opens a new ID, but
  preserves hidden state when re-requesting the same already-hidden ID.
  `QUESTION_SHOW` is the explicit user reopen path.
- `QUESTION_HIDE` marks only the matching pending request hidden and is a
  no-op for stale IDs.
- `QUESTION_RESOLVED`, `QUESTION_ANSWERED`, and `QUESTION_DISMISSED` clear
  hidden state only for the matching current request; broad
  resolution/reset/rekey clears it safely.
- App derives dialog visibility from
  `pendingQuestion && hiddenQuestionRequestId !== pendingQuestion.request_id && sessionAskVisible`.
- `QuestionDialog` (`web/src/components/Chat/QuestionDialog.tsx`) receives
  separate `onHide` (X/Escape) and `onCancel` (Don't answer) callbacks.
  `onHide` is synchronous/local; `onCancel` retains async failure behavior.
- ChatPanel's existing `Open question` button dispatches explicit show rather
  than a synthetic fresh request.

## Error / consistency handling

- Local hide cannot fail and performs no network call.
- Final `Don't answer` keeps current loading suppression, error reporting,
  stale 404/409 handling, optimistic transcript rewrite, and server broadcast
  behavior.
- A recovered pending ask can still be answered after reopening; if it became
  stale, the existing answer path's 404/409 logic clears it.
- Hidden state is session-local UI state and must never be persisted as prompt
  state.

## Components / files expected

- `web/src/stores/chatStore.tsx` — SessionSlice: field, actions, reducer,
  initialization, reset, rekey.
- `web/src/hooks/useChat.ts` — selector and hide callback if needed.
- `web/src/App.tsx` — dialog open derivation.
- `web/src/components/Chat/QuestionDialog.tsx` — callbacks/labels.
- `web/src/components/Chat/ChatPanel.tsx` — open button dispatches show.
- Focused tests for each of the above.

No backend endpoint/schema/API change.

## Testing / TDD

Write failing tests first.

- Store tests: hide/show matching IDs, stale hide/show no-op, same-ID
  recovery remains hidden, new-ID recovery opens, answer/dismiss/resolved/
  reset/rekey cleanup.
- QuestionDialog tests: X/Escape call `onHide` and not `onCancel`; `Don't
  answer` calls `onCancel`; loading behavior unchanged.
- ChatPanel test: existing `Open question` action shows same hidden request.
- App/scope regression: hidden ask does not mount while another surface is
  active and returns as hidden, not auto-open.
- Run targeted Vitest, typecheck, and relevant full Chat/App suites. Account
  for known unrelated concurrent-WIP failures by reporting exact fail sets.

## Documentation

- Update inline comments and `skills/ocode-web/SKILL.md` during
  implementation.
- The OKF docs already align; note that the recovery-path caveat
  (`gotchas/pending-ask-recovery-live-session-state.md` — recovery currently
  reopens unconditionally) is addressed by request-scoped hidden state: a
  recovery re-dispatch of the same `request_id` preserves the local hide.
- No public API/schema/migration.

## Non-goals

- No reversible server dismissal (the server-side `Don't answer` stays final).
- No read-only history viewer for dismissed questions.
- No backend changes (endpoint, schema, or broadcast).
- No TUI behavior change.
