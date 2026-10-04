---
type: Gotcha
title: Pending ask recovery from live session state (sentinel-less transcript)
description: 'Added async turn_error bus event as a fourth recovery trigger for pending-ask hydration, updated sentinel names, updated timestamp; livePendingAsks now merges the child permission-ask registry (own mutex, read unconditionally) with the TryLock-gated transcript scan.'
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
  - async
  - turn_error
timestamp: 2026-10-03T00:26:24Z
---
---
type: Gotcha
title: "Pending ask recovery from live session state (sentinel-less transcript)"
description: "Gotcha: pending permission/question dialog not appearing when SSE frame missed and transcript has no sentinel — livePendingAsks recovery"
tags: [gotcha, web, desktop, permission, dialog, sse, reconcile, TryLock, pending-ask]
timestamp: 2026-09-17T00:00:00Z
status: active
---

# Pending ask recovery from live session state (sentinel-less transcript)

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

## Recovery path (four layers)

### 1. Server: `GET /api/sessions/:id/state` returns live pending asks

`internal/server/handler_session_state.go` — `HandleSessionState` appends a
`pending_asks` field (type `PendingAsks`, omitempty) to the `SessionState`
response. The field is populated by `livePendingAsks`, which merges **TWO
sources** that are readable under OPPOSITE lock conditions:

- **Sentinel (main-agent) asks** — the live agent's trailing tool round, read via
  `pendingAsksFromMessages` → `trailingToolRunStart` (`run_states.go:142`), under a
  non-blocking `as.mu.TryLock()`.
- **Sub-agent permission asks** — `as.childAsks`, the per-session registry in
  `internal/server/child_perm_asks.go`. It has its OWN mutex and NEVER takes
  `as.mu`, and `livePendingAsks` reads it **unconditionally** — no lock gate at
  all, before the TryLock is even attempted.

**TryLock rule — SENTINEL source only.** For the transcript scan,
`livePendingAsks` uses `as.mu.TryLock()` (non-blocking) on purpose. `runTurn`
(`agent_session.go`) holds `as.mu` for the entire turn (minutes), and this
endpoint is polled by the browser reconcile + the turn watchdog — a blocking lock
would pin an HTTP connection behind a running turn (the "stuck session" class).
For a sentinel ask the TryLock skip is correct: while a turn holds the lock it
cannot yet be paused on one (the pause and the unlock happen together when the
step returns), so reporting nothing is right for THIS source, and any ask the
turn does raise arrives over SSE. This is a statement about `as.messages`, not
about asks in general.

**Why the registry read is unconditional.** A sub-agent permission ask is
registered INSIDE a turn that still holds `as.mu`: a synchronous sub-agent
dispatch parks within `runTurn`, so a TryLock-gated read would hide the ask for
exactly as long as the ask existed — the very window the dialog needs to be
visible. The registry is therefore built to live outside `as.mu` (own mutex, no
`as.mu` anywhere in it), which is also what makes it answerable while the parked
turn holds the lock: `handleChildPermResolve` never takes `as.mu`, and a stale id
404s instead of falling through to `findPendingSession`, which takes a BLOCKING
`as.mu`. The park itself is bounded — `childPermAskTimeout` is 10 minutes, after
which the ask auto-denies and `permission_resolved` closes the dialog (parent
cancel/stop auto-denies too).

**Both sources, both consumers.** `livePendingAsks` merges them (main-agent
first — the order the transcript scan already established, and the newest ask is
what the client shows; no dedup is needed because a main-agent ask lives in the
transcript and a sub-agent ask only in the registry, never both).
`PendingPermissionAsks` (`internal/server/run_states.go`, the desktop
badge/quit-dialog counter) does the same: `as.childAsks.list()` first and
unconditionally, then the TryLock-gated `tailIsPermissionAsk` scan. A failed
TryLock means "not pending" for the sentinel source only, which is why the
registry is consulted outside the lock.

**Recovery contract intact.** `pending_asks` must stay recoverable from live
session state; the merge is what satisfies it — a session parked on a sub-agent
ask is now recoverable from `/state` even though the transcript scan is
necessarily locked out for the whole turn.

**Lock order:** `livePendingAsks` takes `h.mu` only to read the session pointer
(`lookupAgentSession`), releases it, then takes `as.mu` — preserving the
`as.mu → h.mu` order documented in `agent_session.go`. The registry needs no such
care: it never takes `as.mu` or `h.mu`.

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
(`ErrPermissionPending`, `run_states.go:20`, turned into the 409 by
`HandleSendMessage` at `handler.go:1583`), the catch
block calls `hydratePendingAsks()` which fetches session state via
`api.getSessionState` and dispatches `PERMISSION_REQUEST` / `QUESTION_REQUEST`
for each pending ask, opening the dialog.

### 4. Client: async `turn_error` frame triggers live-state hydration

`web/src/api/client.ts` — the web client **always** sends `async:true` on
`sendMessage`. A send on a session paused on a permission ask therefore returns
**202**, not the HTTP 409 the `useChat` submit catch checks. `runTurn`'s
`ErrPermissionPending` refusal is published later by `publishTurnError`
(`internal/server/agent_session.go`) as a `turn_error` bus event (and a legacy
`error` frame headless).

The client reacts to that frame: `scheduleHydratePendingAsks` in
`web/src/lib/sessionEvents.ts` (per-session in-flight dedupe) → exported
`hydratePendingAsks(sessionId, router)` → `api.getSessionState` →
`dispatchPendingAsks` → dispatches `PERMISSION_REQUEST` / `QUESTION_REQUEST`.
It matches the error text against `PENDING_ASK_ERROR = "permission decision is
pending"` (mirrors `ErrPermissionPending` in `internal/server/run_states.go`),
because the legacy `error` frame carries only the string.

`reconcileOpenSessions` now reuses the same `dispatchPendingAsks` helper.

**Do NOT "fix" the async path by returning 409 pre-dispatch:**
`TestAsyncTurnRefusedWhilePermissionPending` in
`internal/server/zz_repro_perm_test.go` asserts the async contract (202 + the
refused message stays queued for retry). The only missing piece was the client
trigger.

## Rule

**When reading `agentSession.messages` from an HTTP handler, use `TryLock` not
`Lock`** because `runTurn` holds `as.mu` for the entire turn. A blocking lock
in a reconcile/poll handler pins an HTTP connection behind the turn — the
"stuck session" bug class documented in CLAUDE.md. This rule is about the
transcript: for a sentinel ask, a failed TryLock legitimately means "nothing to
report right now".

**Never apply that rule to `as.childAsks`.** The sub-agent permission-ask
registry must be read unconditionally (it has its own mutex and never takes
`as.mu`) — gating it on `TryLock` would hide a live ask for exactly as long as
the ask exists, because the parked child sits inside the very turn that holds the
lock. `livePendingAsks` and `PendingPermissionAsks` both read it that way; keep
any new consumer consistent.

**Ordering in reconcile:** live-pending-ask hydration must happen AFTER
`MERGE_SNAPSHOT`, not before, because the merge overwrites pending fields from
the (possibly stale) transcript.

## Regression

- `internal/server/handler_session_state_test.go` — tests for `livePendingAsks`
  with TryLock semantics.
- `internal/server/child_perm_asks_test.go` — the second source and the merge:
  `TestLivePendingAsksReportsChildAskWhileTurnLockHeld`,
  `TestLivePendingAsksMergesBothSources`,
  `TestLivePendingAsksNeverBlocksOnTheTurnLock`,
  `TestPendingPermissionAsksCountsChildAsksWhileTurnLockHeld`,
  `TestPendingPermissionAsksStillCountsMainAgentSentinelAsks`.
- Client: the 409 path is exercised when a send is refused while a permission
  dialog is live but unseen.
- Client: the async `turn_error` path is covered by
  `TestAsyncTurnRefusedWhilePermissionPending` in
  `internal/server/zz_repro_perm_test.go` (server-side contract), and the
  `scheduleHydratePendingAsks` dispatch path in `sessionEvents.ts` (client-side).