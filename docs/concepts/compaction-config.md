---
type: Concept
title: 'Compaction Config: First-Token/Idle Timeouts, Operation Cap, and Timeout Classification'
description: 'Amended 2026-09-27: runSummary cancel-vs-timeout label split (summaryContextErr), completed-summary drain race (usableSummary), rewritten §4 classification + strengthened §7 sentinel invariant.'
tags:
  - compact
  - compaction
  - config
  - timeout
  - reliability
  - server
  - web
  - cancellation
timestamp: 2026-09-27T10:25:19Z
---
# Compaction Config: First-Token/Idle Timeouts, Operation Cap, and Timeout Classification

**Type:** Concept  
**Description:** Implemented 2026-09-25 large-context /compact reliability: first-token vs idle timeouts, explicit-zero semantics, per-batch contexts + 30-min cap, ErrCompactionTimeout→504, delta-callback ownership, web sticky errors. Amended 2026-09-27: runSummary cancellation-cause labelling (timed out vs cancelled) + drain of a racing completed summary; §4 anchors re-verified.  
**Tags:** compact, compaction, config, timeout, reliability, server, web  

---

# Compaction config, timeout windows, and error classification

- **Status:** Implemented (2026-09-25; amended 2026-09-27 — cancellation-cause labelling and the completed-summary drain in `runSummary`). Design: `superpowers/specs/2026-09-25-web-compact-large-context-reliability-design.md`.
- **Scope:** `internal/config` (persisted knobs), `internal/agent` (batch timeouts, delta-callback ownership, `ErrCompactionTimeout`, cancel-vs-timeout labelling), `internal/server` (`HandleCompactSession` status mapping), `web/` (sticky inline error + app-wide action error).

Large multi-batch `/compact` passes used to fail silently: one shared inactivity context spanned the whole pass, a slow first token was charged against the idle budget from t=0, a concurrent chat could clear the summary stream's `OnDelta` hook mid-flight, and *every* failure surfaced as HTTP 500. This page documents the shipped behavior that replaced that.

## 1. Config surface

Two independent timeouts, both in the `compact` section of `ocodeconfig.json`:

| Field | Default | Meaning |
|---|---|---|
| `summary_first_token_timeout_seconds` | **300** | Deadline for the **first** streamed token of each summary batch (slow first token / long prefill). |
| `summary_timeout_seconds` | **600** | **Idle** timeout *after* the first token has streamed — resets on every delta. |

Storage and runtime are deliberately two-stage:

- **Persist (`applyCompactConfig`, `internal/config/ocodeconfig.go:1978`)** — copies an explicitly-present value verbatim, **including `0`**. An explicit `0` round-trips as `0`; it is never silently rewritten on disk.
- **Resolve (`resolveCompactRuntime`, `internal/agent/agent.go:1975`)** — the only place that normalizes: `summary_first_token_timeout_seconds <= 0` → **300** (`agent.go:2024-2026`), `summary_timeout_seconds <= 0` → **600** (`agent.go:2020-2021`).

So the semantics of an explicit `0` are *"use the runtime default"* (300 / 600), **not** "disable this timeout". Config cannot turn either timeout off; the `idle <= 0` plain-context branch inside `inactivityContextWithParent` is defense-in-depth unreachable through normal config resolution. Absent key → default 300; no migration.

**API / UI:** `GET|PUT /api/config/ocode/compact` (`internal/server/server.go:377-378`) serve the raw persisted values; the web `CompactForm` (`web/src/components/Settings/CompactForm.tsx:23`, "First-token timeout (s)") edits them through `api.getCompactConfig`/`setCompactConfig` (`web/src/api/client.ts:818-819`).

## 2. Runtime data flow (one compaction pass)

```
web /compact  →  POST /api/sessions/{id}/compact  (server.go:320)
  → HandleCompactSession (handler.go:1779)
  → Agent.CompactWithFocus → runCompact (agent.go:2499)
       resolveCompactRuntime → chunkMiddleByBudget (batches)
       operationCtx = newCompactOperationContext()   ← fixed 30-min cap
       for each batch:
         batchCtx = inactivityContextWithParent(operationCtx, idle, firstToken)  ← FRESH per batch
         batchCtx = withDeltaCallback(batchCtx, reset)                          ← per-call, context-scoped
         runSummary(batchCtx, …)   // deltas call reset()
       ← any failure returns here, transcript untouched
  → on success only: splice summary into as.messages + saveSession
  → SSE compaction_started / compaction_done (compaction_events.go)
```

Key properties (all implemented, tested):

1. **Fresh inactivity context per batch** — `agent.go:2590` constructs the window *inside* the batch loop, so batch N gets a full deadline instead of the residual of batch N-1 (`TestRunCompactGivesEachBatchFreshTimeout`).
2. **First-token window, then idle window** — `inactivityContextWithParent` (`compact.go:1058`) starts at `initial` (first-token timeout) and, after the first delta-driven `reset()`, switches to `idle` (`summary_timeout_seconds`) for subsequent gaps (`TestInactivityContextGivesFirstTokenItsOwnWindow`). Retries within a single batch share that batch's first-token window; the first successful streamed token switches that batch to the configured idle timeout, and a new batch gets a new first-token window.
3. **Fixed 30-minute overall cap** — `compactOverallCap = 30 * time.Minute` (`compact.go:164`, a var so tests can shorten it) bound via `context.WithTimeoutCause(..., ErrCompactionTimeout)` (`compact.go:183`). It is an *operation* bound, not an idle bound: per-batch contexts are its children, so a batch that is **still receiving tokens** is aborted at the cap (`TestRunCompactOverallCapStopsAnActiveBatch`).
4. **No partial mutation** — `runCompact` returns `CompactResult{OK:false, Err}` before any splice; `HandleCompactSession` only rewrites `as.messages` when `result.OK` (the splice starts at `handler.go:1861`). Failed compaction leaves the transcript byte-identical (asserted by `TestCompactSessionTimeoutReturns504AndLeavesTranscriptUnchanged`).

## 3. Delta-callback ownership

Two deliberate, *different* lifetimes coexist:

- **Compaction (per-call, context-scoped):** `runCompact` attaches the idle-reset hook with `withDeltaCallback(batchCtx, reset)` (`agent.go:2606`, `client.go:69`). `GenericClient.ChatWithContext` reads it from the request context (`client.go:783`) and routes stream deltas to it **exclusively** — the shared `OnDelta` field is neither wrapped nor invoked when a per-call callback exists. A concurrent chat calling `SetOnDelta(nil)` can no longer clear the compaction reset hook, and summary deltas no longer leak into a chat's stream (`TestChatWithContextPerCallDeltaCallbackTakesPrecedence`).
- **Main turn (shared, `chatWithDelta`):** `chatWithDelta` (`agent.go:959`) still does `gc.SetOnDelta(a.OnDelta); defer gc.SetOnDelta(nil)` around one `Chat` call. This **shared-callback lifetime is intentional and unchanged** — it is how the agent's `OnDelta` reaches the stream for the duration of a single call, and the always-clear-on-return contract is what keeps subagents from inheriting a stale callback. Do not "unify" it with the context path.

## 4. Error classification (server)

`ErrCompactionTimeout` (`compact.go:159`) is the **only** timeout sentinel. It is set as the cancellation *cause* by the per-batch inactivity expiry (`compact.go:1094`, `:1114`) and by the 30-min operation cap (`compact.go:183`), and survives wrapping.

`runSummary` labels a failed context through the single helper `summaryContextErr` (`compact.go:935`): it returns nil while the context is still live (`compact.go:937-939`), wraps a cause satisfying `errors.Is(cause, ErrCompactionTimeout)` — compaction's own deadline, i.e. either the 30-minute `compactOverallCap` or the per-batch inactivity timer — as `compact: summary timed out: %w` (`compact.go:941`), and wraps **any other cause** as `compact: summary cancelled: %w` (`compact.go:943`). Both branches use `%w`, so `errors.Is` matches through the label.

There are exactly **four** places in `runSummary` that can report a dead context — the retry-backoff wait (`compact.go:819-820`), the `ctx.Done()` branch of the wait `select` (`compact.go:843-844`), the failure path of the `done` branch (`compact.go:857-859`), and the post-loop check (`compact.go:867-869`) — and all four call `summaryContextErr`. Earlier revisions had three of them carrying their own inline copy of the `errors.Is(cause, ErrCompactionTimeout)` branch, and the fourth (the retry-backoff wait) labelling every cause as `compact: context cancelled during retry` regardless of class. One rule, four call sites, no site able to drift.

The cause/class split exists because the old code wrapped *every* context cause with the literal prefix `compact: summary timed out:`, so a plain `context.Canceled` surfaced as the self-contradicting `compact: summary timed out: context canceled`. That is not hypothetical: the exact string was recorded 5 times in session `ses_2026-09-24-145135-98fed7b1` (2026-09-25, ~14:45 +0800), where a compaction pass died because the then-current code shared one inactivity context across all summary batches — §2's shared-context bug, already fixed — and its watchdog called a bare `cancel()`. The misleading label is what sent the diagnosis down the wrong path: the remedies differ (raise the deadline vs find whatever cancelled the pass), and "timed out" pointed at the wrong one.

The same change also closes the **other** side of the race the 2026-09-25 fix covered (that fix handled a result chosen while a cancel was already visible; this handles a cancel observed while a result was already in hand). Each attempt now delivers a named `summaryResult{content, err}` (`compact.go:881`) on a buffered channel (`compact.go:824`), and `runSummary` parks on that channel and on `ctx.Done()` (`compact.go:843`) in a single `select`. Go picks uniformly at random when both cases are ready, so a summary that landed in the same instant the inactivity timer fired was discarded on roughly half of those attempts — failing an otherwise successful compaction. The `ctx.Done()` branch is now one line, `return drainBufferedSummary(done, ctx)` (`compact.go:844`), delegating to the helper at `compact.go:916`: it performs a non-blocking receive and keeps the summary when one is already buffered **and** `usableSummary` (`compact.go:890`) accepts it, otherwise it returns `summaryContextErr(ctx)` (`compact.go:924`).

**`drainBufferedSummary` must only be called once `ctx.Done()` has already fired**, which is its sole call site. With a still-live context and an empty channel it returns `("", nil)` — `summaryContextErr` reports nothing to cancel — so a caller reaching it on a live context would read "no summary yet" as "no cancellation" and treat an empty string as a successful summary. `usableSummary` is deliberately conservative: it rejects a transport error, an error delivered alongside content, and content that is empty, whitespace-only, or missing required template sections — returning a malformed summary from the drain would bypass the retry the normal path would still have performed (`compact.go:849`). A summary that has not landed when the cancel fires is genuinely unavailable, and a buffered transport error loses to the cancellation cause, which explains more.

`HandleCompactSession` (`handler.go:1831-1860`) classifies **by sentinel identity only**, never by message text or by "is this a `context.Canceled`":

| Condition | Status | Response written? | Notes |
|---|---|---|---|
| Session not found | 404 | yes | `handler.go:1784` |
| Compaction disabled in config | 422 | yes | `!enabled` |
| Nothing to compact (`!OK`, `Err == nil`) | 422 | yes | |
| `errors.Is(Err, ErrCompactionTimeout)` (batch idle/first-token expiry **or** 30-min cap) | **504** | yes, **if the request ctx is alive** | `"compaction timed out; transcript unchanged; retry the command"` (`handler.go:1845`) |
| Request already cancelled / client gone (`r.Context().Err() != nil`) | — | **no** | logged + `finish()` only; the client has disconnected, writing is pointless (`handler.go:1836-1844` in the timeout branch, `:1848-1852` otherwise) |
| Any other error, request alive | 500 | yes | includes bare `context.Canceled` that is *not* the sentinel |
| Success | 200 | yes | original/compacted lengths |

Two classification gotchas, both intentional:

- **After the split, a "timed out" message implies the sentinel class — but keep classifying by `errors.Is`.** `summaryContextErr` labels the sentinel class `timed out` and every other cause `cancelled`, so a `compact: summary timed out:` string now reliably corresponds to `ErrCompactionTimeout`; the old mislabel (a plain `context.Canceled` reported as a timeout) is gone. Because all four dead-context sites now share one rule, the message agrees with the class in **both** directions — the retry-backoff wait used to be the counter-example, wrapping any cause as `compact: context cancelled during retry` even when it was the sentinel, but it too goes through `summaryContextErr` (`compact.go:819-820`) now. Treat that agreement as informational, not load-bearing: text is not the contract, it is trivially editable, and it says nothing about causes that arrive as `lastErr` (transport failures, `compact: empty summary response`) which are not classified at all. Only `errors.Is` yields 504.
- **Bare cancellation ≠ timeout.** A request disconnect or any non-sentinel cancellation must **not** become 504 (`handler.go:1848-1852` logs `"compaction request cancelled"` and writes nothing). Pre-existing 404/422 mappings are unchanged.

The transcript is never mutated on any of the failure rows, and `compaction_done` still fires with `ok:false` + the error via `finishCompaction` so clients reconcile.

## 5. Web / desktop surfacing

`handleCompact` (`web/src/components/Chat/commands.ts:1562-1597`) keeps **both** surfaces on rejection:

1. **Sticky inline error** — `setCompactionState(sessionId, {status:"error", error})` → the composer `CompactionStatus` bar (`role="alert"`, "Compaction failed: …", dismissible) plus an inline `**Compaction failed:** …` transcript message. Session-scoped, survives view changes; cleared only by a new attempt, completion, or dismissal.
2. **App-wide action error** — `reportActionError(err, "Compact conversation")` (`web/src/lib/actionErrors.ts:55`), the sticky toast that outlives composer/tab remounts.

The synchronous `active` state is set *before* the `await` (closing the submission race), and `compaction_started` / `compaction_done` SSE events (`sessionEvents.ts:510-519`) reconcile the same store across clients. Success clears the bar — the persisted compaction-summary notice in the transcript is the completion record.

## 6. Tests & validation

| Area | Suite |
|---|---|
| Config default (300) + explicit-`0`/`300` persistence | `internal/config/compact_config_reliability_test.go` |
| Persist API round-trip | `internal/server/handler_config_test.go` `TestHandleSetCompactConfigPersists` |
| Fresh per-batch deadline, first-token grace, 30-min cap on an active stream, runtime `<=0 → 300` normalization | `internal/agent/compact_reliability_test.go` |
| Per-call delta callback precedence over shared `OnDelta` | `internal/agent/client_reliability_test.go` |
| Cancel-vs-timeout label split + drain accept/reject contract | `internal/agent/compact_cancel_test.go` |
| 504 + transcript unchanged | `internal/server/compact_timeout_test.go` |
| Sticky inline error + app-wide action error on `/compact` rejection | `web/src/components/Chat/commands.compact.error.test.tsx` |
| Composer lifecycle (sticky across remounts, queued work after failure) | `web/src/components/Chat/ChatInput.compaction.test.tsx` |

The §4 drain race is not deterministically reproducible from outside `runSummary` (Go's `select` picks at random only when both cases are ready, which needs scheduler control to arrange), so its invariant is enforced *structurally* — the drain delegates to `usableSummary` rather than re-implementing the check — and pinned by `TestUsableSummary` there instead of by a timing test; `TestRunSummaryLabelsNonTimeoutCancelAsCancelled` and `TestRunSummaryLabelsCompactionDeadlineAsTimeout` keep the two label classes distinguishable.

Docs: `README.md` (config example + "First-token timeout" feature row), `CHANGES.md` 2026-09-25 entry, `skills/ocode-agent-architecture/SKILL.md` compaction section, and the approved design spec linked above.

## 7. Invariants to preserve

- Persist ≠ resolve: **never** clamp explicit `0` in `applyCompactConfig`; **always** normalize `<= 0` in `resolveCompactRuntime`.
- `ErrCompactionTimeout` is the sole 504 trigger — and this is now **enforced in code, not merely observed in the message**: `summaryContextErr` makes the message agree with the class (sentinel → `timed out`, any other cause → `cancelled`). Bare cancellation is still not a timeout, and `HandleCompactSession` must keep deciding via `errors.Is(err, ErrCompactionTimeout)` — **never the message text**. The post-split string/class agreement is informational; the sentinel is the contract.
- Failure ⇒ transcript unchanged (no splice, no save).
- The context-scoped delta callback is for compaction; `chatWithDelta`'s shared `SetOnDelta`/clear pair stays as-is on purpose.
