---
type: Gotcha
title: 'Web ask dialogs: 202 + background continuation (broadcast *_resolved before Step)'
description: Ask resolve endpoints now return 202 and run the agent continuation on a background goroutine via dispatchAskContinuation (lock-ownership handoff + dispatchTurn shutdown parity); web client echoes optimistically and re-hydrates pending_asks on retryable failure; historical broadcast-before-Step note retained.
tags:
  - gotcha
  - web
  - server
  - questions
  - permissions
  - ask
  - "202"
  - async
  - concurrency
  - locks
timestamp: 2026-09-25T06:00:07Z
---
# Web ask dialogs: 202 + background continuation (broadcast `*_resolved` before Step)

**Symptom (historical):** the web/desktop `QuestionDialog` (or
`PermissionDialog`) stays on screen after the user answers, for as long as the
model's follow-up round takes. Looks like "dialog won't close" — or, worse,
the submit itself looks hung with no result.

## Current design (shipped 2026-09-25): the endpoints return 202 and run the continuation in the background

Both resolve handlers used to call `as.agent.Step` synchronously inside the
HTTP request, so the POST stayed pending for the whole model round (Step can
run for minutes). The browser's `await` never settled, the dialog's local
answer echo + dismissal were gated on it, and the request held one of the
browser's ~6 connections per origin for its whole duration — the same
starvation the async send path was fixed for (`web/src/api/client.ts:1448`
`async: true`; see the comment at `web/src/hooks/useChat.ts:161`). The TUI
never had this problem: resolving a dialog just resumes its single in-process
loop.

Now both endpoints apply the answer, persist the answered sentinel, and
acknowledge with `202 Accepted`; the continuation runs on its own goroutine
via:

- `(*Handler).dispatchAskContinuation(sessionID, as, fn)` —
  `internal/server/handler_ask_continuation.go:31`.
- `HandleAnswerQuestion` dispatches at
  `internal/server/handler_questions.go:282` and writes 202 at
  `handler_questions.go:336`. The callback wires callbacks, sets
  `turnActive(true)`, starts the heartbeat + activity broadcast, broadcasts
  `question_resolved`, runs `Step`, persists, broadcasts `messages` +
  `turn_done`, and calls `MaybeCompactAsync`.
- `HandleResolvePermission` dispatches at
  `internal/server/handler_permissions_resolve.go:282` and writes 202 at
  `handler_permissions_resolve.go:371`. The approved-tool execution (the
  `executeApprovedWithTempPath` call) AND the re-Step both run inside the
  dispatched callback — they previously ran inline before Step. The
  `permission_resolved` broadcast still happens synchronously BEFORE dispatch
  (`handler_permissions_resolve.go:270-274`); for questions the
  `question_resolved` broadcast happens inside the callback but still before
  `Step` (`handler_questions.go:309`, Step at `:311`).
- Multi-ask branch: if another unresolved permission ask remains in the same
  trailing round, the callback saves the single resolution and broadcasts
  `messages` without running Step (`handler_permissions_resolve.go:308-322`),
  and the handler still returns 202.

### Lock-ownership handoff (the subtle part)

`findPendingSession` returns the `agentSession` with `as.mu` **held**
(`handler_questions.go:250`,
`handler_permissions_resolve.go:214`). The caller must **NOT** unlock it on
the success path — `dispatchAskContinuation` transfers ownership to the
callback goroutine, which unlocks via `defer as.mu.Unlock()` when `fn`
returns (`handler_ask_continuation.go:70`). Consequently there is no
handler-level unlock defer anymore: every post-`findPendingSession` error
path must unlock explicitly (e.g. `handler_questions.go:260,266`;
`handler_permissions_resolve.go:228,234,243,250,256`). A new validation
branch added after `findPendingSession` that forgets its explicit unlock
deadlocks the session.

### Shutdown admission mirrors `dispatchTurn`

`dispatchTurn` (`internal/server/agent_session.go:1615`) is the pattern;
`dispatchAskContinuation` mirrors it exactly:

- Under `shutdownMu`, if `shutdownStarted`: unlock and run `fn` INLINE on the
  caller's goroutine (with `defer as.mu.Unlock()`), so no unregistered
  goroutine can write after the shutdown join
  (`handler_ask_continuation.go:32-38`). In that case the 202 is written only
  after the continuation finishes — shutdown degrades to the old sync
  behavior deliberately.
- Otherwise `turnJobsWG.Add(1)` before releasing `shutdownMu`
  (`:39`), then register `turnInFlight[sessionID]++` BEFORE starting the
  goroutine (`:45-50`) so a racing `Stop` (and `shutdownAgentSessions`
  cancellation) still sees the session as in-flight.
- The goroutine's defer chain (LIFO): unlock `as.mu` → recover panics (logged
  as `serve error: ask continuation panic`) → decrement `turnInFlight` →
  `turnJobsWG.Done()` (`handler_ask_continuation.go:52-72`).

The continuation keeps the full `turnActive`/heartbeat contract from the
original fix below: `setTurnActive(true)` + `startTurnHeartbeat` +
`startAgentActivityBroadcast` with defers declared last so they run first,
preserving the `drainPendingClose` → `setTurnActive(false)` order
(`handler_questions.go:288-303`, `handler_permissions_resolve.go:336-350`).

## Web client: echo/dismiss BEFORE the await, re-hydrate on retryable failure

`resolvePermission` and `submitQuestionAnswers` in
`web/src/hooks/useChat.ts` now dispatch their local effects BEFORE awaiting
the POST, so the visible result never depends on response latency:

- `resolvePermission` dispatches `PERMISSION_RESOLVED` first
  (`useChat.ts:255`), then awaits `api.resolvePermission`.
- `submitQuestionAnswers` dispatches `QUESTION_ANSWERED` (local transcript
  echo of the answers) + `QUESTION_RESOLVED` first (`useChat.ts:291-292`),
  then awaits `api.answerQuestion`.
- On a retryable failure (anything that is not HTTP 404/409) they call
  `hydratePendingAsks()` (`useChat.ts:101`, called at `:266` and `:300`),
  which refetches `GET /api/sessions/:id/state` and re-dispatches
  `PERMISSION_REQUEST`/`QUESTION_REQUEST` from the server's live
  `pending_asks` (`internal/server/handler_session_state.go:78`) — so an
  optimistically dismissed dialog reappears and the user can retry (the
  continuation never ran). A 404/409 means the server no longer holds the
  ask, so staying dismissed is correct.

The SSE `*_resolved` frame is therefore a belt-and-braces dismissal signal
now, not the only one.

## Historical note: the original fix (broadcast before Step)

Before the async conversion, the only lever was ordering: broadcast
`question_resolved` / `permission_resolved` immediately after the answer was
applied to the working transcript and BEFORE `agent.Step`, so the dialog
closed on the frame rather than on the (still-minutes-long) POST. That rule
still holds inside the new callbacks — the frame fires before Step in both
handlers — but it is no longer what unblocks the request. Fixed for
permissions earlier; questions fixed 2026-09-08; endpoints made async
202 + background continuation 2026-09-25.

## Tests

- `internal/server/handler_questions_test.go:288`
  `TestHandleAnswerQuestionReturns202BeforeContinuation` and
  `handler_questions_test.go:335`
  `TestHandleResolvePermissionReturns202BeforeContinuation` — the endpoint
  must return 202 while the continuation is still blocked; reverting to
  inline Step fails both (mutation-verified).
- Broadcast-before-Step guards still stand:
  `TestHandleAnswerQuestionBroadcastsResolvedBeforeContinuation`
  (`handler_questions_test.go:577`) and
  `TestHandleResolvePermissionBroadcastsResolvedBeforeContinuation`
  (`handler_permissions_resolve_test.go:621`) — they gate the fake model
  client so the test only passes if the frame arrives while the model round
  is still blocked.
- Continuation still emits heartbeats:
  `TestHandleAnswerQuestionContinuationEmitsHeartbeats`,
  `TestHandleResolvePermissionContinuationEmitsHeartbeats`.
- Web: `web/src/hooks/useChat.remoteHost.test.tsx:273`
  `describe("useChat.submitQuestionAnswers optimistic echo")` and
  `web/src/hooks/useChat.test.tsx:51` (404/409 stays dismissed;
  retryable failure re-hydrates the ask; success dismisses).

## Related docs

- `gotchas/question-answer-transcript-echo.md` — client-side
  `QUESTION_ANSWERED` transcript echo (now dispatched pre-await).
- `gotchas/pending-ask-recovery-live-session-state.md` — recovering pending
  asks from live session state (what `hydratePendingAsks` uses).
- `gotchas/web-sse-stream-silent-death-liveness.md` — the turnActive +
  heartbeat contract every continuation must honor.
- `superpowers/plans/2026-08-12-multiproject-event-architecture/03-async-bootstrap-turn-state.md`
  — the persist-then-202 turn contract the ask endpoints now follow.
