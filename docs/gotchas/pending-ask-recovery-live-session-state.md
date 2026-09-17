---
type: Gotcha
title: Pending ask recovery from live session state (sentinel-less transcript)
description: 'Gotcha: pending permission/question dialog not appearing when SSE frame missed and transcript has no sentinel — livePendingAsks recovery'
tags:
  - gotcha
  - web
  - desktop
  - permission
  - dialog
  - sse
  - reconcile
  - TryLock
  - pending-ask
timestamp: 2026-09-17T01:56:14Z
---
## Problem

When a session is paused on a permission or question ask, the browser can fail to
show the approve/reject dialog while every send returns:

> a permission decision is pending for this session; resolve it before sending a
> new message

The user is stuck with no visible dialog and no way to proceed.

## Trigger

The `PERMISSION_ASK:` permission sentinel — or the `QUESTION_PROMPT:` +
`WAITING_FOR_USER_RESPONSE` question pair — was NOT written to the persisted
transcript (sqlite messages table). This happens when:

1. The agent pauses mid-turn and the in-memory `as.messages` snapshot outlives a
   failed or conflicting session save — the pause post-dates the last disk write.
2. A concurrent save overwrites the tail, dropping the sentinel.
3. The tab was backgrounded and the live SSE frame carrying the ask was missed,
   and the follow-up reconcile refetch from disk finds only plain user messages.

Pre-recovery, the only source of truth was `as.messages` (the live agent's
in-memory transcript), but the web client never read it for asks.

## Recovery path (three layers)

### 1. Server: `GET /api/sessions/:id/state` returns live pending asks

`internal/server/handler_session_state.go` — `HandleSessionState` now appends a
`pending_asks` field (type `PendingAsks`, omitempty) to the `SessionState` response.
The field is populated by `livePendingAsks` which reads the live agent's trailing
tool round via `pendingAsksFromMessages` → `trailingToolRunStart` (`run_states.go:122`).

**TryLock rule:** `livePendingAsks` uses `as.mu.TryLock()` (non-blocking) on
purpose. `runTurn` (`agent_session.go`) holds `as.mu` for the entire turn
(minutes), and this endpoint is polled by the browser reconcile + the turn
watchdog — a blocking lock would pin an HTTP connection behind a running turn (the
"stuck session" class). While a turn holds the lock it cannot yet be paused on an
ask (the pause and unlock happen together when the step returns), so returning nil
is correct; any ask the turn does raise arrives over SSE.

**Lock order:** `livePendingAsks` takes `h.mu` only to read the session pointer,
releases it, then takes `as.mu` — preserving the `as.mu → h.mu` order documented
in `agent_session.go`.

### 2. Client: reconcile counts live pending asks

`web/src/lib/sessionEvents.ts` — `reconcileOpenSessions` now reads
`state.pending_asks` from the reconcile response. It counts
`hasLivePending` toward `hasPendingAsk` so the paused turn's running indicator
stays visible. The `PERMISSION_REQUEST` / `QUESTION_REQUEST` dispatches happen
**AFTER** the `MERGE_SNAPSHOT` dispatch: the merge's non-mid-turn branch
overwrites `pendingPermission` / `pendingQuestion` from the (possibly
sentinel-less) transcript, so dispatching before would be clobbered. The reducer
dedupes by `request_id`, so an ask already set by the merge is unaffected.

### 3. Client: 409 error triggers live-state hydration

`web/src/hooks/useChat.ts` — when `api.sendMessage` fails with HTTP 409
(`ErrPermissionPending` from `run_states.go:20` → `handler.go:863`), the catch
block calls `hydratePendingAsks()` which fetches session state via
`api.getSessionState` and dispatches `PERMISSION_REQUEST` / `QUESTION_REQUEST`
for each pending ask, opening the dialog.

## Rule

**When reading `agentSession.messages` from an HTTP handler, use `TryLock` not
`Lock`** because `runTurn` holds `as.mu` for the entire turn. A blocking lock
in a reconcile/poll handler pins an HTTP connection behind the turn — the
"stuck session" bug class documented in AGENTS.md.

**Ordering in reconcile:** live-pending-ask hydration must happen AFTER
`MERGE_SNAPSHOT`, not before, because the merge overwrites pending fields from
the (possibly stale) transcript.

## Regression

- `internal/server/handler_session_state_test.go` — tests for `livePendingAsks`
  with TryLock semantics.
- Client: the 409 path is exercised when a send is refused while a permission
  dialog is live but unseen.