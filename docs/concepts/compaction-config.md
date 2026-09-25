---
type: Concept
title: 'Compaction Config: First-Token/Idle Timeouts, Operation Cap, and Timeout Classification'
description: 'Implemented 2026-09-25 large-context /compact reliability: first-token vs idle timeouts, explicit-zero semantics, per-batch contexts + 30-min cap, ErrCompactionTimeout→504, delta-callback ownership, web sticky errors. Line anchors re-verified 2026-09-25.'
tags:
  - compact
  - compaction
  - config
  - timeout
  - reliability
  - server
  - web
timestamp: 2026-09-25T11:57:55Z
---
# Compaction config, timeout windows, and error classification

- **Status:** Implemented (2026-09-25). Design: `superpowers/specs/2026-09-25-web-compact-large-context-reliability-design.md`.
- **Scope:** `internal/config` (persisted knobs), `internal/agent` (batch timeouts, delta-callback ownership, `ErrCompactionTimeout`), `internal/server` (`HandleCompactSession` status mapping), `web/` (sticky inline error + app-wide action error).

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
2. **First-token window, then idle window** — `inactivityContextWithParent` (`compact.go:1008`) starts at `initial` (first-token timeout) and, after the first delta-driven `reset()`, switches to `idle` (`summary_timeout_seconds`) for subsequent gaps (`TestInactivityContextGivesFirstTokenItsOwnWindow`). Retries within a single batch share that batch's first-token window; the first successful streamed token switches that batch to the configured idle timeout, and a new batch gets a new first-token window.
3. **Fixed 30-minute overall cap** — `compactOverallCap = 30 * time.Minute` (`compact.go:164`, a var so tests can shorten it) bound via `context.WithTimeoutCause(..., ErrCompactionTimeout)` (`compact.go:183`). It is an *operation* bound, not an idle bound: per-batch contexts are its children, so a batch that is **still receiving tokens** is aborted at the cap (`TestRunCompactOverallCapStopsAnActiveBatch`).
4. **No partial mutation** — `runCompact` returns `CompactResult{OK:false, Err}` before any splice; `HandleCompactSession` only rewrites `as.messages` when `result.OK` (the splice starts at `handler.go:1861`). Failed compaction leaves the transcript byte-identical (asserted by `TestCompactSessionTimeoutReturns504AndLeavesTranscriptUnchanged`).

## 3. Delta-callback ownership

Two deliberate, *different* lifetimes coexist:

- **Compaction (per-call, context-scoped):** `runCompact` attaches the idle-reset hook with `withDeltaCallback(batchCtx, reset)` (`agent.go:2606`, `client.go:69`). `GenericClient.ChatWithContext` reads it from the request context (`client.go:783`) and routes stream deltas to it **exclusively** — the shared `OnDelta` field is neither wrapped nor invoked when a per-call callback exists. A concurrent chat calling `SetOnDelta(nil)` can no longer clear the compaction reset hook, and summary deltas no longer leak into a chat's stream (`TestChatWithContextPerCallDeltaCallbackTakesPrecedence`).
- **Main turn (shared, `chatWithDelta`):** `chatWithDelta` (`agent.go:959`) still does `gc.SetOnDelta(a.OnDelta); defer gc.SetOnDelta(nil)` around one `Chat` call. This **shared-callback lifetime is intentional and unchanged** — it is how the agent's `OnDelta` reaches the stream for the duration of a single call, and the always-clear-on-return contract is what keeps subagents from inheriting a stale callback. Do not "unify" it with the context path.

## 4. Error classification (server)

`ErrCompactionTimeout` (`compact.go:159`) is the **only** timeout sentinel. It is set as the cancellation *cause* by the per-batch inactivity expiry (`compact.go:1040`, `:1060`) and by the 30-min operation cap (`compact.go:183`), and survives wrapping — `runSummary` returns `fmt.Errorf("compact: summary timed out: %w", contextCause(ctx))` (`compact.go:853`, `:879`), so `errors.Is` matches.

`HandleCompactSession` (`handler.go:1829-1857`) classifies **by sentinel identity only**, never by message text or by "is this a `context.Canceled`":

| Condition | Status | Response written? | Notes |
|---|---|---|---|
| Session not found | 404 | yes | `handler.go:1782` |
| Compaction disabled in config | 422 | yes | `!enabled` |
| Nothing to compact (`!OK`, `Err == nil`) | 422 | yes | |
| `errors.Is(Err, ErrCompactionTimeout)` (batch idle/first-token expiry **or** 30-min cap) | **504** | yes, **if the request ctx is alive** | `"compaction timed out; transcript unchanged; retry the command"` (`handler.go:1843`) |
| Request already cancelled / client gone (`r.Context().Err() != nil`) | — | **no** | logged + `finish()` only; the client has disconnected, writing is pointless (`handler.go:1837-1841` in the timeout branch, `:1846-1849` otherwise) |
| Any other error, request alive | 500 | yes | includes bare `context.Canceled` that is *not* the sentinel |
| Success | 200 | yes | original/compacted lengths |

Two classification gotchas, both intentional:

- **A message can say "timed out" without being the sentinel.** `runSummary` wraps *any* context cause with the text `compact: summary timed out: …` (`compact.go:853`), including a plain `context.Canceled`. Text is not evidence — only `errors.Is(…, ErrCompactionTimeout)` yields 504. Do not classify from the string.
- **Bare cancellation ≠ timeout.** A request disconnect or any non-sentinel cancellation must **not** become 504 (`handler.go:1846-1849` logs `"compaction request cancelled"` and writes nothing). Pre-existing 404/422 mappings are unchanged.

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
| 504 + transcript unchanged | `internal/server/compact_timeout_test.go` |
| Sticky inline error + app-wide action error on `/compact` rejection | `web/src/components/Chat/commands.compact.error.test.tsx` |
| Composer lifecycle (sticky across remounts, queued work after failure) | `web/src/components/Chat/ChatInput.compaction.test.tsx` |

Docs: `README.md` (config example + "First-token timeout" feature row), `CHANGES.md` 2026-09-25 entry, `skills/ocode-agent-architecture/SKILL.md` compaction section, and the approved design spec linked above.

## 7. Invariants to preserve

- Persist ≠ resolve: **never** clamp explicit `0` in `applyCompactConfig`; **always** normalize `<= 0` in `resolveCompactRuntime`.
- `ErrCompactionTimeout` is the sole 504 trigger; bare cancellation is not a timeout, and neither is message text.
- Failure ⇒ transcript unchanged (no splice, no save).
- The context-scoped delta callback is for compaction; `chatWithDelta`'s shared `SetOnDelta`/clear pair stays as-is on purpose.
