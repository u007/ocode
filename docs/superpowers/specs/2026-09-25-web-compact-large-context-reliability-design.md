---
type: Decision
title: 'Design Spec: Reliable Large-Context Compaction (web/desktop /compact)'
description: Design spec for reliable large-context /compact on web/desktop (2026-09-25); IMPLEMENTED — per-batch first-token/idle windows, fixed 30-min cap, context-scoped delta callback, ErrCompactionTimeout→504, sticky inline + app-wide web errors.
tags:
  - compact
  - compaction
  - timeout
  - reliability
  - design-spec
  - web
  - server
  - config
timestamp: 2026-09-25T12:00:02Z
---
# Design Spec: Reliable Large-Context Compaction (web/desktop `/compact`)

- **Status:** **IMPLEMENTED (2026-09-25).** The approved design below shipped as described; see *As-built notes* for the exact code anchors and the few wording deltas. §11's documentation tasks are done (`docs/concepts/compaction-config.md`, README, CHANGES, TODO, `skills/ocode-agent-architecture/SKILL.md`).
- **Date:** 2026-09-25
- **Scope:** ocode — `internal/agent` (compaction core), `internal/server` (compact HTTP handler), `internal/config`, `web/` (compact command + settings).

## As-built notes (2026-09-25)

What actually landed, in the order the design specifies it:

1. **Per-batch windows.** `runCompact` builds a **fresh** batch context *inside* the batch loop (`internal/agent/agent.go:2592-2607`) with `inactivityContextWithParent(operationCtx, idle, firstToken)` (`internal/agent/compact.go:1008`). The batch starts under `summary_first_token_timeout_seconds` (resolved default 300s); the first streamed delta performs the first `reset()`, after which later gaps are bounded by `summary_timeout_seconds`.
2. **Fixed 30-minute cap.** `compactOverallCap = 30 * time.Minute` (`internal/agent/compact.go:161-164`, a package var so tests shorten it) applied via `newCompactOperationContext()` → `context.WithTimeoutCause(..., ErrCompactionTimeout)` (`compact.go:178-184`). Batch contexts are children of it, so the cap aborts a batch that is *still receiving tokens*. No config knob (§4 non-goal held).
3. **Context-scoped delta callback.** `withDeltaCallback` / `deltaCallbackFromContext` (`internal/agent/client.go:65-79`) carry the reset hook through the request context; `ChatWithContext` reads it (`client.go:783`) and routes summary deltas to it instead of `GenericClient.OnDelta`. `runCompact` installs it at `agent.go:2606` and never calls `gc.SetOnDelta`; `chatWithDelta`'s shared `SetOnDelta`/`SetOnDelta(nil)` pair is unchanged as §6.3 required.
4. **Error taxonomy → 504.** `agent.ErrCompactionTimeout` (`internal/agent/compact.go:159`) is the only timeout cause. `HandleCompactSession` (`internal/server/handler.go:1779`, classification `:1829-1857`) maps `errors.Is(result.Err, agent.ErrCompactionTimeout)` → **504** with `"compaction timed out; transcript unchanged; retry the command"`; a dead request context is logged and **no response is written**; any other error stays **500**; 404/422 mappings unchanged. Classification is by sentinel identity only, never by message text.
5. **Web sticky + app-wide error.** `handleCompact` (`web/src/components/Chat/commands.ts:1562-1597`) keeps the session-scoped `setCompactionState(sessionId, {status:"error", error})` **and** calls `reportActionError(err, "Compact conversation")` (`web/src/lib/actionErrors.ts:55`) in the same catch path, so the failure survives composer/tab remount.
6. **Config.** `summary_first_token_timeout_seconds` is a real field: `internal/config/ocodeconfig.go:311` (field), `:1106` (default `300`), `:2003-2005` (merge); persisted raw — including explicit `0` — by `applyCompactConfig` (`ocodeconfig.go:1978`); normalized only in `resolveCompactRuntime` (`internal/agent/agent.go:1975`, `<= 0 → 300` at `:2024-2026`, idle `<= 0 → 600` at `:2020-2021`). Web row: `web/src/components/Settings/CompactForm.tsx:23` ("First-token timeout (s)").
7. **Tests.** `internal/agent/compact_reliability_test.go` (fresh per-batch deadline, first-token grace, absolute cap), `internal/agent/client_reliability_test.go` (per-call callback survives a foreign `SetOnDelta(nil)`), `internal/config/compact_config_reliability_test.go` (round-trip incl. explicit `0`), `internal/server/compact_timeout_test.go` (504 + transcript unchanged), `web/src/components/Chat/commands.compact.error.test.tsx` (dual surfacing).
8. **Invariant held.** On every failure path the splice is skipped, `saveSession` does not run, and no `messages` broadcast is sent; `finishCompaction` still publishes `compaction_done` so other clients clear the indicator.

Sections below keep their approved-design wording. §2 and §5's "Current (failing) flow" describe the pre-fix state and are historical evidence, not current behavior; §11's "none have been touched by this spec" line is superseded by the status bullet above.

## 1. Problem

Manual `/compact` on web/desktop often appears to fail silently, especially when the transcript is large (observed above ~400k estimated context tokens). The user sees no durable error, the transcript is unchanged, and the context gauge stays high. In practice the compaction request fails after minutes of waiting, and the failure is either lost in the UI (state not surviving the view) or misreported by the server as a generic HTTP 500.

## 2. Evidence (verified 2026-09-25)

### 2.1 Production log evidence — `~/.local/share/opencode/logs/compact.log`

Real local log lines from 2026-09-25 (manual compaction runs):

```
2026-09-25T12:29:34+08:00 [COMPACT] manual compaction requested: messages=1561 window=1048576
2026-09-25T12:35:55+08:00 [COMPACT] summary failed on batch 6/12: compact: summary timed out: context canceled
2026-09-25T12:32:47+08:00 [COMPACT] manual compaction requested: messages=951 window=1000000
2026-09-25T12:36:17+08:00 [COMPACT] summary failed on batch 2/8: compact: summary timed out: context canceled
```

Later the same day, after the context had grown past the auto threshold, the pattern degenerated into near-continuous failure:

```
2026-09-25T13:14:54+08:00 [COMPACT] summary failed on batch 2/11: compact: summary timed out: context canceled
2026-09-25T13:16:26+08:00 [COMPACT] summary failed on batch 1/12: compact: summary timed out: context canceled
2026-09-25T13:18:31+08:00 [COMPACT] summary failed on batch 1/12: compact: summary timed out: context canceled
2026-09-25T13:20:57+08:00 [COMPACT] summary failed on batch 2/12: compact: summary timed out: context canceled
2026-09-25T13:22:43+08:00 [COMPACT] summary failed on batch 1/12: compact: summary timed out: context canceled
```

The user's `ocodeconfig.json` sets `summary_timeout_seconds: 90` (the code default is 600). Each failure is a mid-pass batch timeout carrying `context canceled` as the wrapped cause.

### 2.2 UI state — already session-scoped, but insufficient

`web/src/components/Chat/commands.ts:1555-1585` (`handleCompact`) already implements session-scoped active/error compaction state:

- `setCompactionState(sessionId, {status:"active"})` synchronously before the `await` (closes the submission race).
- On rejection: `setCompactionState(sessionId, {status:"error", error})` + an inline `**Compaction failed:**` assistant message.

The gap is that the sticky state lives only in the composer's in-memory session-scoped store — a composer/tab remount (or a slow multi-minute failure that outlives the view) loses it, so long-running failures "disappear".

### 2.3 Server error mapping — everything is 500

`HandleCompactSession` (`internal/server/handler.go:1756`, formerly cited as `:1696+` — line numbers have drifted under concurrent WIP) maps every failure uniformly:

```go
if !result.OK {
    as.mu.Unlock()
    if result.Err != nil {
        writeError(w, http.StatusInternalServerError, result.Err.Error()) // ← every error
        return
    }
    writeError(w, http.StatusUnprocessableEntity, "nothing to compact")
    return
}
```

There is currently **no test pinning an error status** for the compact endpoint, and no distinction between a timeout, a disconnect, and a genuine internal error. The handler also does not check whether the request context is already cancelled before writing.

### 2.4 Compaction core — one shared inactivity context + shared-client callback mutation

`internal/agent/agent.go:runCompact` (starts `:2485`) currently:

1. Creates **one** inactivity context for the whole multi-batch pass (`inactivityContext(rt.SummaryTimeoutSeconds)`, `compact.go:966`).
2. Installs the deadline reset by mutating the **shared** summary client: `gc.SetOnDelta(func(kind, text string) { reset() })` at `agent.go:2579`, with `defer gc.SetOnDelta(nil)` at `agent.go:2580`.

`internal/agent/agent.go:chatWithDelta` has a separate, **intentional** `SetOnDelta`/`SetOnDelta(nil)` pair at `agent.go:963-964` (used to forward deltas to the live turn stream).

`compactSummaryClient()` (`agent.go:2695`) normally builds a separate `GenericClient` copy with `ThinkingBudget = 0` (small-model or copy), but the copy is still a *shared, mutable* object: any concurrent `SetOnDelta(nil)` on that same client instance erases the compaction reset hook, after which the single inactivity context can never be reset and fires mid-pass — exactly the observed `summary timed out: context canceled` at batch N/M.

### 2.5 Advisor assessment (accepted)

- A shared/stale context across batches is a **secondary** contributor.
- The **stronger** candidates are: (a) **slow first token** on huge first batches — the inactivity clock starts before any token arrives, so a long queue/prompt-processing delay is indistinguishable from a hang; (b) **shared-client hook clearing** (`SetOnDelta(nil)` racing the compaction reset).
- Direction: **fix the callback ownership** rather than relying only on per-batch contexts.

## 3. Goals

1. Large manual `/compact` runs (multi-batch, >400k tokens) either succeed or fail **loudly, fast, and with a correct HTTP status**.
2. A slow-starting summary gets a distinct, generous first-token grace period, separate from the steady-state idle timeout.
3. Compaction's deadline reset cannot be erased by any other user of the summary client.
4. The web UI surfaces a compaction failure both **sticky inline** (existing) and **app-wide** (new), so it survives composer/tab remount.
5. Failures never mutate the transcript; the API stays synchronous.

## 4. Non-goals

Explicitly **out of scope** (approved as non-goals):

- No async/background compaction jobs, job queues, or worker model.
- No `sessionStorage`-based error recovery or reload persistence beyond the app-wide toast.
- No SSE progress events, batch progress UI, or per-batch status streaming.
- No cancel/abort control for an in-flight compaction.
- No change to `chatWithDelta`'s `SetOnDelta`/`SetOnDelta(nil)` pair at `agent.go:963-964` (knowingly left as-is for this minimal fix — see §6.3).
- No config knob for the overall operation cap (fixed 30 minutes, §6.2).
- No changes to compaction prompt, section validation, retry policy, `pruneToolResults`, splice/anchoring logic, or auto-compaction thresholds.

## 5. Architecture / data flow

Current (failing) flow:

```
web handleCompact (commands.ts:1555)
  → POST compactSession → HandleCompactSession (handler.go:1756)
    → agent.CompactWithFocus → runCompact (agent.go:2485)
      → ONE inactivityContext(summary_timeout_seconds) for all batches
      → shared GenericClient.SetOnDelta(reset)  ← racy, cleared by defer/parallel SetOnDelta(nil)
      → for batch 1..N: summary LLM call under ctx
    ← CompactResult{OK:false, Err: "... context canceled"}
  ← HTTP 500 for EVERY Err  (no 504, no disconnect distinction)
← catch → setCompactionState(error) (in-memory only; lost on remount)
```

Target flow:

```
web handleCompact
  → session-scoped active state (unchanged)
  → POST compactSession
    → HandleCompactSession
      → agent.CompactWithFocus → runCompact
        → per-batch: FRESH inactivity context
             initial deadline  = summary_first_token_timeout_seconds (default 300)
             first token       → reset to summary_timeout_seconds for subsequent resets
        → whole operation guarded by a FIXED 30-minute cap context
             (package var; overridable in tests)
        → per-call delta callback carried through the REQUEST CONTEXT
             (no mutation of GenericClient.OnDelta; immune to foreign SetOnDelta(nil))
        → timeout/abort → wraps as ErrCompactionTimeout (sentinel)
      ← CompactResult.Err == ErrCompactionTimeout (or wrapped)
    → errors.Is(ErrCompactionTimeout)            → 504
    → r.Context().Err() != nil (client gone)     → log only, NO response write
    → other Err                                  → 500
    → log session id + error class BEFORE writing response
  ← rejection
    → setCompactionState({status:"error"})   (existing sticky inline CompactionStatus)
    → reportActionError(err, "Compaction")   (NEW — app-wide sticky toast, survives remount)
```

Data-flow invariants on failure:

- `as.messages` is **not** replaced (the splice at `handler.go:1793-1799` only runs on `result.OK`).
- No `messages` SSE broadcast, no `publishTurnStatusSnapshot` (both only run on success today — preserved).
- The transcript on disk is untouched (`h.saveSession` only runs on success).

## 6. Detailed design (approved decisions)

### 6.1 Config: `summary_first_token_timeout_seconds` (default 300)

Add a real, documented config field — **not** a hidden floor:

| Layer | Change |
|---|---|
| `internal/config/ocodeconfig.go` | `Compact` struct field `SummaryFirstTokenTimeoutSeconds int \`json:"summary_first_token_timeout_seconds"\`` (~line 304 area, next to `SummaryTimeoutSeconds`); pointer override struct (~line 955); default `300` in the defaults block (~line 1088); merge/apply (~line 1973). Inline comment explaining first-token vs idle semantics. |
| `internal/agent` Go side | `CompactConfig` field, `compactConfigFile` field, `defaultCompactConfig` (→ 300), `applyCompactConfig` (persists the **raw** value, including explicit `0`), `compactRuntime` field. |
| `resolveCompactRuntime` | The **only** place that normalizes: `<= 0 → 300`. |
| `web/src/api/client.ts` | `CompactConfig` interface field (~line 64-72). |
| `web/src/components/Settings/CompactForm.tsx` | `EMPTY` default (~line 8-11) + a form row in the field list (~line 22), labeled `First-token timeout (s)`. |
| Tests | Round-trip cases (below). |

Semantics (exact, must be pinned by tests):

- **Omitted** in config → value comes from defaults → `300` → persists `300` on next save → resolves `300`.
- **Explicit `0`** → `applyCompactConfig` persists raw `0` (no silent rewrite) → `resolveCompactRuntime` normalizes to `300` at runtime.
- **Explicit `300`** → round-trips as `300` through load → resolve → save.

It is *not* a floor on `summary_timeout_seconds`: the two knobs are independent (first-token grace vs steady-state idle), and the new field does not clamp or modify the existing one.

### 6.2 Per-batch fresh inactivity contexts + fixed 30-minute cap

In `runCompact`:

- **Replace** the single pass-wide `inactivityContext(...)` with a **fresh context per summary batch**.
- **Initial deadline** of each batch's context = `rt.SummaryFirstTokenTimeoutSeconds` (resolved, default 300s) — this is the *first-token* grace: a batch may start slowly without tripping the idle timer.
- **On the first streamed token** of that batch, subsequent resets switch to `rt.SummaryTimeoutSeconds` (the existing idle/`summary_timeout_seconds` value, user config 90 / default 600). The first token therefore also performs the first idle-reset under the new value.
- **Overall operation cap:** a single `context.WithTimeout` wrapping the entire `runCompact` pass, fixed at **30 minutes**. No config knob (approved). Implemented as a package-level var (e.g. `compactOverallCap = 30 * time.Minute`) so tests can override it — the same override pattern used for test seams elsewhere in the package.
- Batch context derives from the cap context (`WithTimeout(capCtx, …)`), so the cap aborts an in-flight batch promptly.
- Expiry of a batch inactivity context **or** the overall cap → the batch/operation fails with the §6.4 sentinel.

### 6.3 Per-call delta callback via request context (callback ownership fix)

The root fix per advisor feedback:

- Introduce a **per-call delta callback** carried through the `context.Context` used for the compaction request (a small unexported context key + helper, e.g. `withDeltaCallback(ctx, fn)` / `deltaCallbackFrom(ctx)`).
- The compaction path (batch loop → `GenericClient` streaming call) reads the callback from the request context instead of `gc.SetOnDelta(...)`. `runCompact` **no longer mutates shared `GenericClient.OnDelta`**, and `defer gc.SetOnDelta(nil)` at `agent.go:2580` is removed.
- Consequence (pinned by test): a concurrent `SetOnDelta(nil)` from any other code path can no longer erase the compaction reset hook — the two no longer contend for the same field.
- `chatWithDelta` at `agent.go:963-964` is **knowingly left unchanged**: it is the live-turn delta forwarder for a different call site with different lifetime semantics, and touching it would widen this minimal fix beyond compaction reliability. It remains a shared-client mutation and a theoretical cross-interference source, but with compaction no longer writing `OnDelta` there is nothing left for it to erase. Document this in an inline comment at both sites and in CHANGES.md.

### 6.4 Error taxonomy: `ErrCompactionTimeout` sentinel → 504

- Define a distinct sentinel `agent.ErrCompactionTimeout` (`errors.New("compaction timed out")`), wrapped with context (`fmt.Errorf("compact: summary timed out after %s (batch %d/%d): %w", …)`) so `errors.Is` matches through wrapping.
- Produced by: per-batch inactivity expiry **and** overall-cap expiry. Preserve diagnostic text (idle duration, configured timeouts, batch X/Y) in the wrapped message.
- **Only** this sentinel maps to **HTTP 504 Gateway Timeout** in `HandleCompactSession`.
- **Bare cancellation / request disconnect** (`r.Context().Err() != nil` or a `context.Canceled` that is not the compaction sentinel): **log it and do not convert to 504**; if the request context is already cancelled, **do not attempt to write any response** (the client is gone). The session-side work still fails cleanly with no transcript mutation.
- All other errors remain **HTTP 500**. Do **not** turn every `context.Canceled` into 504.
- Pre-existing mappings unchanged: 404 (session lookup), 422 (compaction disabled / nothing to compact).
- Server logs the **session id + error class + full error** **before** writing any response.

### 6.5 Synchronous API, no partial mutation

- The compact endpoint stays **synchronous** (request holds until done or capped at 30 min).
- On any failure: `as.messages` unchanged, no `saveSession`, no `messages` broadcast, no status-snapshot publish. (This is already true today for `!result.OK`; the spec freezes it as an invariant with a test.)

### 6.6 UI: sticky inline error + app-wide error

- **Keep** the existing session-scoped `CompactionStatus` error (`setCompactionState(..., {status:"error"})` → inline `CompactionStatus` error UI).
- **Also** call `reportActionError(err, "Compaction")` (`web/src/lib/actionErrors.ts:55`) from `handleCompact`'s catch path, so the failure renders as the app-wide sticky error toast and survives composer/tab remount.
- No other UI changes: no progress, no cancel button, no recovery-on-reload (see §4).

### 6.7 Structured diagnostics

Emitted via the existing `emitDebug("COMPACT", ...)` channel on failure (and on timeout classification):

- `batch X/Y` (already present — keep),
- observed **idle duration** (time since last reset/last token),
- **configured** first-token timeout and idle (`summary_timeout_seconds`) values,
- **client pointer** on timeout (the `GenericClient` receiver address / client identity) to diagnose which instance served the batch and whether a foreign `SetOnDelta(nil)` could have hit it,
- server-side: session id + error class logged in `HandleCompactSession` before responding.

## 7. Error handling matrix (server)

| Condition | Status | Response written? | Log |
|---|---|---|---|
| Session not found | 404 | yes | (existing) |
| Compaction disabled | 422 | yes | (existing) |
| Nothing to compact (`Err == nil`, `!OK`) | 422 | yes | (existing) |
| `errors.Is(Err, ErrCompactionTimeout)` (batch idle or 30-min cap) | **504** | yes (if request ctx alive) | session id + full error |
| Request already cancelled / client disconnected (bare `context.Canceled`, not sentinel) | — | **no** | session id + disconnect classification |
| Any other `Err != nil` | 500 | yes (if request ctx alive) | session id + full error |
| Success | 200 + lengths | yes | (existing) |

Ordering rule: classify → log → (write response only if `r.Context().Err() == nil`).

## 8. Compatibility / migration

- **Config:** new key is additive. Absent key → default 300. No migration required; old configs load unchanged. Explicit `0` is legal and means "use default at runtime" (§6.1) — this is a deliberate, documented semantic, not an error.
- **API:** endpoint path, method, and success shape unchanged. Only the error status for timeouts changes (500 → 504) for compaction-timeout errors specifically. Clients that only check `res.ok` are unaffected; the web client surfaces the message text either way.
- **Behavioral:** worst case a single batch can now run up to 300s before first token (was: idle timeout from t=0, e.g. 90s), and the whole operation is bounded at 30 min (previously unbounded-but-shared-idle). Slow-but-alive compactions now have room to finish; wedged ones fail predictably at the cap.
- **No transcript/DB format changes.**
- **Working-tree caution:** the tree has concurrent WIP (line numbers above already drifted, e.g. handler 1696→1756). Implementation must make **small, targeted edits** and **inspect diffs** (`git diff`) before/after each edit to avoid clobbering others' work.

## 9. Test plan (tests first)

Write these before/with the implementation; keep all existing happy-path tests green.

**`internal/agent`:**

1. **Callback ownership:** concurrent shared-client per-call callback survives another `SetOnDelta(nil)` — install the per-call callback via context, run a foreign `SetOnDelta(nil)` mid-flight, assert the reset hook still fires (compaction does not time out).
2. **Fresh per-batch deadline:** multi-batch run — a later batch gets a *fresh* deadline (not the residual of batch 1); assert via injected clock/context inspection that batch 2's budget is restored.
3. **First-token grace:** a batch whose first token arrives late but still within the first-token timeout succeeds (no idle trip); separately, once the first token has arrived, an idle gap exceeding `summary_timeout_seconds` fails with `ErrCompactionTimeout`.
4. **Overall cap:** with the package cap override shortened, a batch that is *still receiving tokens* is aborted at the cap with `ErrCompactionTimeout` (proves the cap is absolute, not idle-based).
5. **Config round-trip:** omitted → 300; explicit `0` persists `0` and resolves 300; explicit `300` round-trips as 300 (default/apply/resolve/save paths).
6. **Sentinel identity:** `errors.Is` holds through the wrapped batch-timeout error.

**`internal/server`:**

7. Timeout sentinel → **504**, and transcript **unchanged** (messages identical, no save, no `messages` broadcast).
8. Non-sentinel error → 500 (preserved).
9. Request context already cancelled → no response write attempted (assert no panic / no body written), logged.

**`web`:**

10. 504 rejection → sticky inline `CompactionStatus` error **and** `reportActionError` app-wide error both fire.
11. Generic promise rejection → same dual surfacing.
12. Existing success path (clears state, no banner) preserved.

All new tests mutation-verified per project convention (revert the fix → test fails → restore).

## 10. Rollout / validation

1. Land tests + implementation in one change set (tests first).
2. `go build ./...`, `go vet`, `gofmt`, full `internal/agent` + `internal/server` suites; web `tsgo --noEmit`, `vite build`, focused suites (commands/CompactForm/actionErrors), then full web suite.
3. **Live validation:** rebuild, then re-run a large manual `/compact` on a >400k-token session while tailing `~/.local/share/opencode/logs/compact.log` — expect either success (batch lines complete) or a single clear `ErrCompactionTimeout` diagnosis line with batch/idle/timeout/client fields; confirm the UI shows the sticky inline error **and** the app-wide toast; confirm HTTP status is 504 (not 500) via the browser network tab or a direct request.
4. Verify config: absent key behaves as 300; set explicit `0` and `300` and confirm round-trip.
5. Watch for regressions in auto-compaction (`preflight`/`triggered` log lines) since the context plumbing is shared.

## 11. Documentation updates (implementation-time, exact files)

When implementing, these must be updated (none have been touched by this spec):

1. `README.md` — config example block (line ~547, **correct the current `"summary_timeout_seconds": 30` example**, which contradicts the 600 default) and the compaction/batching feature row (line ~80, `max_summary_input_tokens` batching row); add `summary_first_token_timeout_seconds`.
2. `internal/config/ocodeconfig.go` — inline field comments for the new key.
3. `skills/ocode-agent-architecture/SKILL.md` — compaction section: per-batch contexts, first-token vs idle timeouts, per-call delta callback ownership, timeout sentinel.
4. `CHANGES.md` — entry under the current release.
5. `docs/concepts/compaction-config.md` — **new**, created via the context system (`doc_write`, path without `docs/` prefix).
6. `TODO.md` — a concrete bullet for the deferred work: config reload/recovery of an in-flight compaction + async-compaction exploration (non-goals in §4).

All six are now done (README `summary_first_token_timeout_seconds`, CHANGES release notes, `TODO.md` "Large-context compaction recovery after client disconnect/reload", the agent skill's Compact section, and `concepts/compaction-config.md`).

## 12. Open issues / deferred

- **Reload/recovery:** if the server restarts mid-compaction, there is no resume — deferred (TODO bullet, §11.6).
- **Async compaction:** deferred (§4, §11.6).
- **`chatWithDelta` shared mutation:** knowingly unfixed (§6.3) — acceptable now that compaction no longer writes `OnDelta`; revisit if a future change makes `chatWithDelta` and compaction share a client instance in a harmful way.
- **Concurrent WIP:** implementation must re-verify line references at edit time (§8).
