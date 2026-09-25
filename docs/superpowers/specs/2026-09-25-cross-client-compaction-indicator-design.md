---
type: Design
title: Cross-Client Compaction Indicator — Design Spec
description: 'Implemented design spec for server-authoritative, session-scoped compaction indicator visible across clients (SSE events + /state reconcile), rev 3: monotonic compaction_generation on both lifecycle events and /state, client-side stale-done generation guard for out-of-lock publish reordering, compaction_error only for real failure/timeout with a live client (cancellation clears silently), plus overlap-safe counter, stale /state request-version guard, and older-server compatibility.'
tags:
  - compaction
  - sse
  - session-state
  - web
  - design-spec
  - generation
  - reordering
timestamp: 2026-09-25T15:37:18Z
---
# Cross-Client Compaction Indicator — Design Spec

**Date:** 2026-09-25
**Status:** **Implemented (2026-09-25).** Rev 3 below describes the shipped code, not a proposal: the session-manager counter + monotonic generation, both SSE lifecycle events, the manual and automatic wiring, the web generation/request-version guards, and the `/state` reconcile are all in place, with the regression anchors listed in §12.
**Type:** Design (bug fix + lifecycle hardening)
**Revision:** **rev 3 (2026-09-25)** — updated to match the **implemented behavior**: a monotonic per-session `compaction_generation` carried on both lifecycle events **and** on `/state`; clients **ignore a `compaction_done` whose generation is older than the latest observed start** (lifecycle events are published *outside* the session lock and can be reordered); `/state` additionally exposes `compaction_error`; and **only a real failure/timeout with a live request publishes an error — request cancellation clears the indicator without one**.
*(rev 2, same day — **verified overlap finding**: manual and automatic compaction can overlap; the lifecycle state is a **per-session counter**; `compaction_done` is published **only when the count reaches zero**; the **first real error** is preserved as the aggregate terminal error. All rev 2 semantics remain in force.)*

## 1. Problem

The compaction indicator (the "compacting…" status shown while a session transcript is compacted) only appears in the browser tab that submits `/compact`. Another client already viewing the same session — a second browser, or a phone — never sees it, because the initiating client flips a local-only state while the compaction lifecycle itself has no cross-client signal. The indicator disappears as soon as the initiating tab's local promise resolves, and a client that opens the session *during* a compaction has no way to learn that one is in flight.

Both entry points are affected:

- **Manual compaction** — user runs `/compact` in the chat input.
- **Automatic compaction** — the agent triggers compaction on its own when the context threshold is reached (`Agent.OnCompactStart` / `Agent.OnCompact` callbacks).

**Verified overlap finding (rev 2):** the two entry points are **not** serialized against each other. The manual path runs the compaction LLM call under the per-session agent lock (`as.mu`), while the automatic pass is launched asynchronously *outside* that lock; its result callback later takes the lock to splice the summary into the transcript. A manual `/compact` and an automatic pass can therefore be in flight **at the same time** for one session (and further automatic passes can join the group). Any lifecycle design that assumes one attempt at a time — a single boolean slot cleared by the first finisher — would let the first completion hide the others: the indicator would clear while a pass was still running, and an early success would erase an earlier failure. The design therefore uses a per-session **counter**.

**Reordering finding (rev 3):** `beginCompaction`/`finishCompaction` update the manager under its lock but **broadcast the lifecycle event after releasing it**. Two passes that start and end back-to-back can therefore publish in the opposite order: a client may receive `compaction_started` for a **new** group before the `compaction_done` of the **previous** group that was already in flight. A terminal event is not self-describing — "clear the indicator" is only correct if it belongs to the newest group. The fix is a **monotonic generation**: the manager increments `compactionGeneration` each time a group starts, both events carry it, and a client that already observed a newer generation **drops the older `compaction_done`** instead of clearing a live indicator.

**Cancellation finding (rev 3):** the manual handler distinguishes a *summarization failure* from a *gone client*. On `agent.ErrCompactionTimeout` or any other non-OK result, it checks `r.Context().Err()`: if the HTTP request is still live, the pass retires with the real error (surfaced as `/state.compaction_error` while the group drains and as `compaction_done {ok:false,error}`); if the request was cancelled, it retires with an **empty** error — the shared indicator clears with `ok:true` and no error banner, because a user navigating away is not a compaction failure. A "bare cancellation" (the provider returned `context.Canceled` while the HTTP request itself is still alive) is still a real failure.

Verified in code: `internal/server/session_manager.go` (`compactingCount`, `compactionGeneration`, `BeginCompaction`/`EndCompaction`, `SessionState` at :819), `internal/server/compaction_events.go` (`compactionStartedEvent`/`compactionDoneEvent` both carry `generation`, `beginCompaction`/`finishCompaction`), `internal/server/handler.go` `HandleCompactSession` (:1779 — cancellation/timeout branches), `internal/server/agent_session.go` (`wireCompactCallbacks` at :1975, `applyCompactResult`), and the web `web/src/lib/compactionState.ts` + `web/src/lib/sessionEvents.ts` guards. Regression tests: `TestCompactionLifecycleTracksOverlappingOperations` (`internal/server/session_manager_state_test.go:66`), `TestManualCompactPublishesLifecycle` (`internal/server/compact_status_test.go:119`), `TestManualCompactFailurePublishesTerminalError` (`internal/server/compact_status_test.go:261`), `TestAutoCompactPublishesLifecycle` (`internal/server/auto_compact_test.go:84`), and the web suite `web/src/lib/sessionEvents.test.ts` ("ignores a late done from an older generation after a newer start").

## 2. Goals

1. Make the compaction lifecycle **server-authoritative** and **session-scoped**: the server owns "is this session compacting right now, and since when"; clients only render it.
2. Cover **both** manual `/compact` and automatic compaction, **including the case where they overlap**.
3. Emit **session-scoped SSE lifecycle events** so every open client on the session (same ocode server, or proxied through the same remote-host bus) sees the indicator live.
4. Expose **authoritative compaction fields** on `GET /api/sessions/:id/state` — `compacting`, `compaction_started_at`, `compaction_generation`, `compaction_error` — so a client that opens mid-compaction (or reconnects) catches up immediately, and so stale state clears on reconcile.
5. Guarantee **all success and error paths clear the state** — no permanently stuck "compacting" indicator — and, when passes overlap, clear **only after the last pass finishes**, reporting the **first real error** observed in the group as the terminal error.
6. Make lifecycle events **reordering-safe**: every event carries a monotonic `generation`, and a terminal event for an older generation can never clear a newer group's indicator.
7. Report **failures, not cancellations**: an error is published only for a real summarization failure/timeout seen by a live client; a cancelled request clears the indicator silently.
8. Preserve **session and host isolation**: no cross-session leakage of compaction state.

## 3. Non-Goals

- **Separate server processes with independent in-memory session managers** are *not* promised live in-progress sync. A phone connected to a different ocode server process does not stream the other process's `compaction_started`/`compaction_done`. Those clients still get the existing completed-transcript **revision convergence** (the 15s `useSessionRevisionSync` poll + `MERGE_SNAPSHOT`), so the compacted transcript and its cleared state converge — only the *in-progress* indicator is not live across processes. *(Approved scope unchanged in rev 3.)*
- **No attempt to serialize manual against automatic compaction.** Overlap is *supported*, not prevented; the counter exists precisely because the async auto pass runs outside `as.mu`. Forcing serialization would mean holding the agent lock across a background LLM pass.
- **No server-side event reordering/retry.** The generation makes the *consumer* tolerant of publish-order jitter; the server does not sequence or deduplicate publishes beyond the existing bus `seq`.
- No change to compaction itself (triggering, sizing, transcript rewrite semantics).
- No persistence of compaction lifecycle state to disk; it lives in the in-memory session manager like other session state. The generation is in-memory only and does not survive a server restart (a restarted server starts at 0 and simply issues a fresh first generation).
- No new UI surface; the existing `CompactionStatus` component is reused.
- No change to the revision/`MERGE_SNAPSHOT` protocol beyond consuming `/state` during existing reconcile paths.

## 4. Design Overview

```
manual /compact ──┐                                                ┌── SSE compaction_started {started_at, generation} ──▶ every client (per pass begin)
                  ├─▶ Handler.beginCompaction/finishCompaction ────┤
auto compaction ──┘        │                                       └── SSE compaction_done {ok, error?, generation} ────▶ only when count → 0 (critical)
                           ▼
   SessionManager.compaction{count++, startedAt, firstErr, generation++}   ← per-session counter + monotonic generation; overlaps allowed
                           │  (increment / decrement under manager lock; events published AFTER unlock ⇒ reorderable)
                           ▼
   GET /api/sessions/:id/state → { compacting: count>0, compaction_started_at, compaction_generation, compaction_error? }
                           │
                           ▼
              web: routeBusEnvelope → generation guard → compactionState store → <CompactionStatus/>
              web: initial load / reconnect / revision revalidate → /state (request-version guard) → reconcile
```

Each entry point participates in the same per-session group: `beginCompaction` increments the counter, `finishCompaction` decrements it, and the terminal event fires only on the transition to zero. The **generation increments when a group starts (counter 0→1)** and is never reset. The initiating client keeps its **optimistic active state** (instant feedback before the first SSE event lands); server events and `/state` reconcile are authoritative and overwrite it in both directions.

## 5. SSE Events

Two session-scoped events, both now carrying the group generation:

| Event | Payload | Critical | Meaning |
|---|---|---|---|
| `compaction_started` | `{ started_at: string, generation?: number }` (`started_at` RFC3339; `generation` omitted when 0) | no | A compaction pass began for this session. Emitted on **every** pass begin, including one joining an already-active group; `started_at` always reports the **first active pass's** timestamp (overlapping passes share it — the client's elapsed timer never resets mid-group). `generation` is the group generation in force at that moment — a joining pass reports the **unchanged** group generation. |
| `compaction_done` | `{ ok: boolean, error?: string, generation?: number }` | **yes** | **All** overlapping passes finished (the counter reached zero). `ok` is false only for a real summarization failure (aggregate error present); disabled/no-op compaction and **cancelled requests** complete with `ok:true`. `generation` is the finished group's generation. |

Rules:

- Add both to the server's `sessionScopedEvents` set **and** to the web client's `SESSION_SCOPED_EVENTS` table (`sessionEvents.ts:160`) so `ROUTABLE_EVENTS` subscribes and `routeBusEnvelope` routes them to the correct session tab (same pattern as `session_rekeyed`). `compaction_done` is marked **critical** via the server's `criticalEvents` map (`internal/server/event_bus.go:36`) so the terminal signal cannot be silently dropped while a client holds an active compaction state.
- Both events carry the session id in the envelope (standard session-scoped envelope); `routeBusEnvelope` fans them out by session — never to other sessions or other hosts.
- **Generation semantics (rev 3):** the manager keeps a **monotonic per-session `compactionGeneration`** (`internal/server/session_manager.go`) that increments **each time a compaction group starts** (the counter's 0→1 transition in `BeginCompaction`, :455) and is never reset — successive groups always differ. Every `compaction_started` carries the generation in force; `compaction_done` carries the finished group's generation (returned by `EndCompaction`). On the wire the field is `omitempty`/`omitzero`, so a payload with no `generation` (0, or a pre-generation server) is read as "unknown".
- **Stale-`done` guard (rev 3):** lifecycle events are broadcast **after** the session-manager lock is released, so a `compaction_done` for group *N* can arrive **after** `compaction_started` for group *N+1*. The client records the **latest generation observed from a `compaction_started` (or from `/state` while `compacting:true`)** and **ignores any `compaction_done` whose generation is strictly older** — dropping it, not clearing. `generation: 0`/absent never triggers the drop (unknown generation ⇒ apply normally, which degrades to the idempotent clear). Implemented in `applyCompactionDone` (`web/src/lib/sessionEvents.ts:236`, `if (generation > 0 && generation < getCompactionGeneration(sessionId)) return;`) with `noteCompactionGeneration`/`getCompactionGeneration` in `web/src/lib/compactionState.ts`.
- **Ordering (counter semantics):** `compaction_started` is emitted as each pass begins (manager updated first, so an immediate `/state` read already shows `compacting:true`). `compaction_done` is emitted **exactly once per overlap group, only after the final decrement** has cleared the slot (§6, §7, §8) — never when a pass finishes while another is still running. The server clears before emitting, so a client that receives an **applicable** `compaction_done` may assume a subsequent `/state` read shows `compacting:false`.
- **Stale/extra finish callbacks** (counter already at zero — e.g. a duplicated result callback) publish **no** terminal event (`wasActive:false`); they must not emit a spurious `compaction_done`.
- **Aggregate error:** the terminal payload carries the **first real error** observed in the group (retained until the last pass finishes). A later successful pass can never erase an earlier failure; an earlier failure never hides behind a later success — `ok:false` with that first error wins. A cancellation never enters this slot (§7).
- A terminal event without a preceding start (missed `compaction_started`) degrades harmlessly: clients treat `compaction_done` as "clear active state for this session", unconditional and idempotent (subject only to the generation guard above).
- A repeated `compaction_started` for an already-active client is a no-op refresh (same `started_at` and same generation while the group lasts) — no flicker, no timer reset; `noteCompactionGeneration` only ever raises the observed maximum.
- The initiating client sets its optimistic active state **before** sending the request; when the echoing `compaction_started` arrives it writes the same active state (server `started_at` overwrites the optimistic one) — no flicker, no double-clear.

## 6. `GET /api/sessions/:id/state`

Authoritative fields, embedded in `sessionStateResponse` from `SessionManager.State` (`internal/server/session_manager.go:819`, handler `internal/server/handler_session_state.go:76`):

```json
{
  "compacting": true,
  "compaction_started_at": "2026-09-25T10:00:00Z",
  "compaction_generation": 3,
  "compaction_error": "summarizer failed"
}
```

- `compacting: boolean` — always present; **`true` iff the per-session counter > 0**.
- `compaction_started_at` — `omitzero`: present iff `compacting` is true; recorded when the counter goes 0→1 and kept for the whole overlap group (a joining pass does not move it); zeroed again when the group drains.
- `compaction_generation: number` — **`omitzero`** (new in rev 3). The same monotonic generation as the events; **not** reset when the group drains, so after the first compaction it is always present (even at `compacting:false`) and lets a reconciling client learn the latest generation without waiting for an event. Never decrements within a server process.
- `compaction_error: string` — **`omitempty`** (new in rev 3). The group's **retained first error**, so it is non-empty only while `compacting:true` **and** at least one pass has already failed (it is set at the failing pass's `EndCompaction` and cleared when the group drains). It is populated **only by a real failure/timeout with a live request context** (§7); a cancelled request passes an empty error and therefore never populates it.
- Read/write of the fields happens **under the session manager lock**; readers get a consistent snapshot (never `compacting:false` with a timestamp, or a generation that disagrees with the counter).
- **All success and error paths decrement the counter**: any outcome of manual or automatic compaction (success, error, cancellation, handler panic path) retires exactly its own pass.
- The client typing (`web/src/api/client.ts` `getSessionState`) includes `compacting?`, `compaction_started_at?`, `compaction_generation?`, `compaction_error?`. The current store applies `compacting`/`compaction_started_at`/`compaction_generation` (`applyServerCompactionState`); `compaction_error` is exposed and typed for reconcile consumers but the visible error banner is rendered from the terminal `compaction_done` payload.

### Stale-`/state` request-version guard *(preserved from rev 2)*

`web/src/lib/compactionState.ts` keeps a per-session monotonic `eventVersions` counter, bumped by every local mutation (`setCompactionState`, `clearCompaction`, `dismissCompaction`, `markLocalCompactionEnd`). Callers capture `getCompactionEventVersion(sessionId)` **before** fetching `/state` and pass it as `versionAtStart`; `applyServerCompactionState` **discards the response if the version moved during the fetch** (`getCompactionEventVersion(sessionId) !== versionAtStart`). This stops a slow poll that observed `compacting:false` from clearing an indicator a `compaction_started` already activated mid-flight — the ordering twin of the generation guard (which protects *event vs. event*, this protects *event vs. in-flight request*). Two additional guards stay: an **older server's response with no `compacting` field is "unknown", not "idle"** (preserve local state), and a **pre-begin `/state` response must not erase the initiating tab's optimistic state** (`isLocalCompactionPending`).

## 7. Server: Manual Path

The manual `/compact` endpoint handler (`HandleCompactSession`, `internal/server/handler.go:1779`):

1. Acquire the per-session agent lock (the lock that serializes turns and splices for that session).
2. Only after acquiring it, call `beginCompaction`: **increment the counter** under the session manager lock (records `started_at` and bumps `compactionGeneration` on the 0→1 transition), then emit `compaction_started {started_at, generation}` (shared first-pass timestamp and unchanged generation when joining a group).
3. Run compaction.
4. In **every** outcome (success, error, cancellation, early return, panic-guard): release the agent lock, then call `finishCompaction` → **decrement the counter**; the lifecycle slot is cleared and `compaction_done {ok, error?, generation}` (aggregate first real error) is emitted **only when the counter reaches zero**. `compaction_done` is critical and may wait on a slow bus subscriber, so it must never publish while holding the agent lock. The `finish` closure is idempotent (`finished` flag) so no path can double-retire a pass.
5. Release the lock (if not already released in step 4).

**Failure vs. cancellation (rev 3):**

| Outcome | `finish(...)` argument | Client sees |
|---|---|---|
| Success / `!enabled` / "nothing to compact" | `""` | `compaction_done {ok:true}` (disabled/no-op is not an error) |
| `ErrCompactionTimeout`, **request still live** | timeout error | `/state.compaction_error` while the group drains, then `compaction_done {ok:false,error}`; HTTP **504** with retry guidance |
| `ErrCompactionTimeout`, **request cancelled** (`r.Context().Err() != nil`) | `""` | indicator clears with `ok:true`, **no error banner**, no response written |
| Other failure, **request still live** | real error | `/state.compaction_error` + `compaction_done {ok:false,error}`; HTTP 500 |
| Other failure, **request cancelled** | `""` | indicator clears silently, no response written |
| Bare cancellation (provider `context.Canceled`, HTTP request still live) | real error | treated as a failure — HTTP **500** (`TestCompactSessionBareCancellationRemains500`, `internal/server/compact_timeout_test.go:91`) |

So **only a real failure/timeout observed by a live client publishes `compaction_error`**; a request cancellation clears the shared indicator without an error — one user's Escape/navigate-away must not paint a failure banner on every other client watching the session.

Registering the pass only after lock acquisition means a queued-but-not-yet-started manual compaction does not lie about its start time, and a manual pass overlapping an in-flight automatic pass simply joins the same counter group.

## 8. Server: Automatic Path

Wire the existing agent callbacks (`wireCompactCallbacks`, `internal/server/agent_session.go:1975`):

- `Agent.OnCompactStart` → `beginCompaction`: increment the counter + emit `compaction_started {started_at, generation}` (first-pass timestamp/generation if this starts the group, otherwise the group's existing values). A legacy/internal path that has no registered lifecycle row is registered on the spot so the state is still authoritative (`compaction_events.go:26`).
- `Agent.OnCompact` (terminal callback, via `applyCompactResult`) → **always retire the pass — decrement the counter — independently of transcript application** (a `defer` runs even for malformed/stale results), then emit `compaction_done {ok, error?, generation}` only if the counter hit zero (with the aggregate error).

The terminal retire must not depend on the transcript rewrite succeeding: if transcript application fails or races (including a racing manual `/compact` shrinking the transcript — the stale splice is dropped, `TestApplyCompactResultSkipsStaleSplice`), the pass still decrements (the failure itself surfaces through the normal error path and the revision convergence). The pass's error (if any) is folded into the group's retained first error before the zero-transition decides the payload. Because the async pass runs outside the agent lock, it can overlap a manual `/compact` — and, being outside the lock, its publish can interleave with the next group's start; that is exactly the reordering the generation guard absorbs.

## 9. Web Client

- **Subscription:** `compaction_started` / `compaction_done` are in `SESSION_SCOPED_EVENTS` (`web/src/lib/sessionEvents.ts:160`) so they flow through the existing `ROUTABLE_EVENTS` → `routeBusEnvelope` path. No new transport code.
- **Store:** events update the existing `compactionState` module store (keyed by session, alongside host), which feeds the existing `CompactionStatus` component. `compaction_started` first calls `noteCompactionGeneration` (raise-only) then sets active + `started_at` (a repeat during an active group carries the same first-pass `started_at` — treat as a no-op refresh); `compaction_done` **checks the generation guard first** (§5) and only then clears (unconditionally, idempotently) or records the error state (`{status:"error", error}`).
- **Generation tracking:** the latest observed generation comes from `compaction_started` events and from `/state` while `compacting:true`. It is never lowered (`noteCompactionGeneration` ignores non-finite, ≤0, and smaller values).
- **Reconnect reset:** the web client clears its generation watermarks on SSE reconnect because a server restart resets the in-memory generation counter.
- **Optimistic state:** the initiating client still sets active locally the moment the user runs `/compact` (instant feedback); server events/`/state` reconcile overwrite it authoritatively in both directions.
- **Reconcile:** apply the new `/state` fields during (a) initial session load, (b) reconnect/reconcile (`reconcileOpenSessions`), and (c) the existing revision revalidation poll (`useSessionRevisionSync` → `revalidateSession` → `/state`) — each capture the request version *before* the fetch (§6 guard). So a client opening a session mid-compaction immediately shows the indicator, and a client that missed `compaction_done` (closed laptop, dropped SSE) clears stale state within one reconcile/poll interval.
- **Isolation:** state is keyed by session id and host; events are routed only to the tab(s) owning that session. Remote-host projects route `/state` and events through the same `/api/remote/{host}` bus — no cross-session or cross-host leakage.
- **Scope boundary:** same server / same remote-host bus = live. Separate server processes = no live in-progress push (§3), but the `/state` fields ride along with the existing revision revalidation that already runs cross-process, so stale active state still self-clears and the completed transcript converges as today.

## 10. Concurrency & Error Semantics

- **One counter per session** (plus shared `started_at`, a retained first error, and the monotonic `compactionGeneration`), all guarded by the session manager lock. Passes within a group are *not* serialized against each other: the manual pass holds the agent lock for its LLM call while the async automatic pass runs outside it — overlap is expected and supported.
- **Increment happens only after the per-session lock is held (manual) or at the start signal (auto); decrement happens on every terminal path, before any terminal publish.** `compaction_done` is published **only on the 0→1→…→0 group's final decrement**, so a subscriber that receives the terminal event can safely assume the `/state` read afterwards shows `compacting:false` — *provided the event applies*, i.e. its generation is not older than the latest observed start.
- **Publishes happen outside the session lock ⇒ reorder is possible.** The manager mutation is atomic with its lock, but the `broadcastEvent` call is deliberately after unlock (a critical `compaction_done` may wait on a slow subscriber). Consequence: `started(N+1)` can precede `done(N)` on the wire. The **generation guard** (§5) is the correctness rule for that case: drop the older `done`, keep the indicator. The `/state` **request-version guard** (§6) is the twin rule for the request path. Neither mechanism reorders the server; they make consumers order-independent.
- **Aggregate terminal error = first real error.** The first non-empty pass error in the group is retained until the group drains; the final `compaction_done` carries it (`ok:false`), and while the group is still active it is also readable as `/state.compaction_error`. A later success cannot clear an earlier failure; a per-pass success emits no terminal event at all while siblings remain. Cancellation contributes `""` and therefore never wins this slot.
- Emits are best-effort at the SSE layer; the `/state` endpoint is the correctness backstop. A client that misses events converges within the revision-poll interval; a client that receives a stale terminal event only performs an idempotent clear (or, with an older generation, no clear at all).
- Error paths: a failing pass clears its own counter slot (decrement), and only the group's last finisher emits `compaction_done {ok:false, aggregateError}` (critical); the pass error also returns through the normal response/stream error channel. The indicator never outlives the group.

## 11. Backward Compatibility

- New `/state` fields are additive: `compaction_started_at` omitted when idle, `compaction_generation` omitted until the first compaction of the process, `compaction_error` omitted unless a failure is retained — old clients ignore them.
- `generation` on the events is additive and `omitempty`; a payload without it reads as 0 → the client's guard (`generation > 0 && generation < latest`) never fires → the event is applied normally (today's unconditional behavior). A **new client + old server** therefore works unchanged.
- New SSE event names are additive; old clients ignore unknown event types (existing `routeBusEnvelope` behavior).
- No persisted-format change → no migration; the generation is in-memory only.
- Old client + new server: no cross-client compaction indicator (today's behavior), nothing breaks.
- New client + old server: no events, `/state` lacks the fields → reconcile treats absence of `compacting` as "unknown" and **preserves current client state** (`typeof state.compacting !== "boolean"` ⇒ return); the initiating client's optimistic state still works.

## 12. Test-First Regression Targets

**Go (server/session):**

1. Manual `/compact` emits `compaction_started` with `started_at` + `generation` and, on success, `compaction_done {ok:true, generation}`; `/state` reports `compacting:false` afterwards. *(`TestManualCompactPublishesLifecycle`, `internal/server/compact_status_test.go:119` — asserts `compaction_generation != 0` while active and `generation != 0` on the terminal payload.)*
2. Manual `/compact` failure clears state and emits `compaction_done {ok:false, error}`; `/state` shows `compacting:false`. *(`TestManualCompactFailurePublishesTerminalError`, `internal/server/compact_status_test.go:261`.)*
3. Automatic compaction via `OnCompactStart`/`OnCompact`: started event sets state; terminal callback clears **even when transcript application errors** (mutation: make the clear conditional on transcript success → test must fail). *(`TestAutoCompactPublishesLifecycle`, `internal/server/auto_compact_test.go:84`.)*
4. `/state` shape: `compacting` + `compaction_started_at` + `compaction_generation` present/absent correctly (`omitzero`/`omitempty` semantics); consistent snapshot read under lock. *(`TestManualCompactPublishesLifecycle` decodes the live `/state` payload; add an assertion for `compaction_error` in the overlapping-failure case — see 9a.)*
5. Lifecycle events are in `sessionScopedEvents`; `compaction_done` is marked critical. *(`TestCompactionEventsAreSessionScopedAndDoneIsCritical`, `internal/server/event_bus_test.go:119`.)*
6. A second client polling `/state` mid-compaction sees `compacting:true` (cross-client visibility).
7. **Overlap: counter not boolean** — two begins → `/state` still `compacting:true` after the first end; only the second end returns idle, with `generation` non-zero at both ends (mutation: clear on first end → test must fail). *(`TestCompactionLifecycleTracksOverlappingOperations`, `internal/server/session_manager_state_test.go:66`.)*
8. **Overlap: single terminal event** — a group that begins twice emits `compaction_started` per begin but exactly **one** `compaction_done`, on the final decrement; the mid-group end emits none.
9. **First-error-wins aggregation** — pass 1 fails, pass 2 succeeds → `compaction_done {ok:false, error:pass1}`; pass 1 succeeds, pass 2 fails → same first-error rule applies for whichever non-empty error came first; all-success → `ok:true` (mutation: take the *last* error or clear the error at 1→2 → test must fail).
   9a. **`compaction_error` on `/state`** — while the group is still active after a failed pass, `/state` shows `compacting:true` + `compaction_error:<first error>`; once drained, `compaction_error` is absent. *(Currently unasserted in Go — add; the field is exercised indirectly by the failure tests.)*
10. **Shared `started_at`** — a joining pass does not move `compaction_started_at`; a fresh group after zero records a new timestamp **and a new generation** (generation never repeats within a process).
11. **Stale finish** — `finishCompaction` with the counter already zero publishes no `compaction_done` (`wasActive:false`).
12. Manual + automatic overlap end-to-end: begin auto, begin manual, finish one → no `compaction_done`, `/state.compacting` stays true; finish the other → one `compaction_done`.
13. **Cancellation ≠ failure** — request context cancelled during a failing/timed-out compaction → the pass retires with an empty error (no `compaction_error`, `compaction_done {ok:true}`) and no response body is written; provider error with a live request → 500 with the error published; timeout with a live request → 504 with the error published. *(`TestCompactSessionTimeoutReturns504AndLeavesTranscriptUnchanged` `internal/server/compact_timeout_test.go:38`, `TestCompactSessionBareCancellationRemains500` `:91`, `TestCompactSessionCancelledRequestSkipsResponseWrite` `:114`.)*

**Web (routing/reconcile/guards):**

14. `compaction_started`/`compaction_done` are in `SESSION_SCOPED_EVENTS` and `routeBusEnvelope` delivers them to the owning session's store (mutation: remove from the table → routing test fails). *("routes compaction lifecycle events for a client that did not start them", `sessionEvents.test.ts:76`.)*
15. `CompactionStatus` renders from the store for the correct session only (no cross-session leakage).
16. Initial load applies `/state` compaction fields → indicator shows for a client opening mid-compaction. *("hydrates an active compaction for a client that opened mid-operation", `sessionEvents.test.ts:715`.)*
17. Revision revalidation applies `/state` → stale `compacting:true` clears after a missed `compaction_done` (mutation: skip `/state` apply → test fails). *("clears an active indicator when the authoritative state reports idle", `sessionEvents.test.ts:1197`.)*
18. Optimistic initiating-client state still works and is not flicker-cleared/duplicated by the echoing `compaction_started`; a repeated started with the same `started_at` does not reset visible elapsed state. *("does not clear the initiating tab's optimistic state from a pre-begin poll", `sessionEvents.test.ts:1222`.)*
19. **Stale-`/state` request-version guard** — a `/state` response whose fetch overlapped a `compaction_started` must not clear the indicator (mutation: drop the `versionAtStart` check → test fails). *("does not let a stale false state clear a start event received mid-fetch", `sessionEvents.test.ts:729`.)*
20. **Older-server compatibility** — a `/state` response with no `compacting`/generation fields preserves an active indicator (mutation: treat absence as idle → test fails). *("preserves an active indicator when an older server omits compaction fields", `sessionEvents.test.ts:1210`.)*
21. **Stale-`done` generation guard** — after `compaction_started {generation:2}`, a `compaction_done {generation:1}` is ignored (indicator stays active) and `compaction_done {generation:2}` clears it; a `done` with no generation still clears (mutation: remove the guard → first assertion fails). *("ignores a late done from an older generation after a newer start", `sessionEvents.test.ts:98`.)*
22. Failure rendering cross-client: `compaction_done {ok:false, error}` leaves `{status:"error"}` for the non-initiating client. *("keeps a failed compaction visible to the other client", `sessionEvents.test.ts:88`.)*

Suites verified green for rev 3: `go test ./internal/server/ -run 'Compaction|Compact'` and `web/src/lib/sessionEvents.test.ts` (80 tests).

## 13. Documentation / API Impact

- API: `GET /api/sessions/:id/state` gains `compacting`, `compaction_started_at`, `compaction_generation`, `compaction_error`; two new SSE event types, each carrying `generation`. Document in the API/session reference — including the counter semantics (`compaction_done` fires once per overlap group; `compaction_started` may repeat with a shared `started_at` and a shared group generation; terminal error is the group's first real error and excludes cancellations) and the consumer rule (drop a `compaction_done` older than the latest observed start generation).
- New design doc: this file (`docs/superpowers/specs/2026-09-25-cross-client-compaction-indicator-design.md`).
- `CHANGES.md` entry for the fix; `skills/ocode-web/SKILL.md` note for the new session event names if the event table is enumerated there.

## 14. Implementation Sequence

*(All six steps completed 2026-09-25; the list is retained as the as-built map.)*

1. Session manager per-session **counter** lifecycle (`BeginCompaction`/`EndCompaction`: increment / decrement under lock, shared `started_at`, retained first error, **monotonic `compactionGeneration`**) + `/state` fields (`compacting`, `compaction_started_at`, `compaction_generation`, `compaction_error`) + Go tests.
2. Server SSE events: add to `sessionScopedEvents`, carry `generation` on both, wire manual endpoint (begin-after-lock, decrement-all-outcomes, terminal publish only at zero, **failure-vs-cancellation split**) + tests.
3. Wire automatic path (`OnCompactStart`/`OnCompact`, decrement-independent-of-transcript) + overlap/aggregation tests.
4. Web: add events to `SESSION_SCOPED_EVENTS`, route into `compactionState` with the **generation guard**, keep `CompactionStatus` + routing tests.
5. Web: apply `/state` on initial load / reconnect / revision revalidation behind the **request-version guard**, preserving older-server absence semantics + reconcile tests.
6. Docs (`CHANGES.md`, API notes, skill note) and full-suite verification.

**⚠ Working-tree note:** the repository has **current uncommitted concurrent WIP** in `App.tsx`, `sessionEvents.ts`, and related server files from other sessions. This work must **preserve** that WIP — re-read files immediately before editing, keep changes additive around existing hunks, and never revert or reformat unrelated in-flight changes.

## 15. Acceptance Criteria

1. Running `/compact` in one browser shows the compaction indicator in **every** already-open client viewing that session (second browser/phone, same server or same remote-host bus), for both manual and automatic compaction.
2. A client that opens the session **during** a compaction shows the indicator via `/state` on initial load.
3. The indicator clears on **every** outcome (success, error, cancellation) in every client — no stuck state; `/state` reports `compacting:false` after completion, and a client that missed the terminal event clears within one reconcile/revision-poll interval.
4. **Manual and automatic compaction may overlap:** the indicator stays active until the **last** overlapping pass finishes (one `compaction_done` per group, on the final decrement), and the terminal error is the group's **first real error** — a later success never erases an earlier failure.
5. **Reordering safety:** every lifecycle event and `/state` snapshot carries the monotonic `compaction_generation`; a client that has observed generation *N+1* never clears on a generation-*N* `compaction_done`, and an in-flight `/state` response whose request version moved is discarded. An absent/0 generation (older server) applies normally.
6. **Error fidelity:** `compaction_error` / `compaction_done {ok:false}` are produced only by a real summarization failure or timeout seen by a live request; a cancelled `/compact` clears the indicator with no error anywhere.
7. No cross-session or cross-host leakage of compaction state.
8. Separate-process clients retain existing completed-transcript revision convergence (no regression).
9. All listed Go and web regression tests exist and pass; targeted mutation checks (conditional clear, removed event table entry, skipped `/state` apply, clear-on-first-finisher, last-error-instead-of-first, **removed generation guard**, **removed request-version check**, **absence-of-fields-treated-as-idle**) each fail their test.
10. Uncommitted concurrent WIP in `App.tsx`/`sessionEvents.ts`/server files is intact after the change.