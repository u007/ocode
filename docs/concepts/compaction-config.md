---
type: Concept
title: 'Compaction Config: First-Token/Idle Timeouts, Operation Cap, and Timeout Classification'
description: 'Amended 2026-09-27: runSummary cancel-vs-timeout label split (summaryContextErr), completed-summary drain race (usableSummary), rewritten §4 classification + strengthened sentinel invariant (then §7, now §8). Amended 2026-09-30: stop-after-failure latch + queued-message restore (§5), summary_max_retries documented (§1), stale code anchors re-verified against source. Amended 2026-10-01: user-initiated cancel classified as its own §4 row (ErrCompactionCanceled → clean 200), cross-link to concepts/compaction-cancellation.md, all code anchors re-derived.'
tags:
  - compact
  - compaction
  - config
  - timeout
  - reliability
  - server
  - web
  - cancellation
  - failure-handling
timestamp: 2026-10-01T08:49:10Z
---
# Compaction Config: First-Token/Idle Timeouts, Operation Cap, and Timeout Classification

**Type:** Concept  
**Description:** Implemented 2026-09-25 large-context /compact reliability: first-token vs idle timeouts, explicit-zero semantics, per-batch contexts + 30-min cap, ErrCompactionTimeout→504, delta-callback ownership, web sticky errors. Amended 2026-09-27: runSummary cancellation-cause labelling (timed out vs cancelled) + drain of a racing completed summary; §4 anchors re-verified. Amended 2026-09-30: stop-after-failure latch + queued-message restore (§5), `summary_max_retries` documented (§1). Amended 2026-10-01: user-initiated cancel classified as its own §4 row (`ErrCompactionCanceled` → clean 200), cross-link to `concepts/compaction-cancellation.md`, code anchors re-derived.  
**Tags:** compact, compaction, config, timeout, reliability, server, web  

---

# Compaction config, timeout windows, and error classification

- **Status:** Implemented (2026-09-25; amended 2026-09-27 — cancellation-cause labelling and the completed-summary drain in `runSummary`; amended 2026-09-30 — stop-after-failure latch and queued-message restore, §5; `summary_max_retries` documented, §1; amended 2026-10-01 — user-initiated cancellation gets its own §4 classification row, all code anchors re-derived). Design: `superpowers/specs/2026-09-25-web-compact-large-context-reliability-design.md`.
- **Scope:** `internal/config` (persisted knobs), `internal/agent` (batch timeouts, delta-callback ownership, `ErrCompactionTimeout`, cancel-vs-timeout labelling, stop-after-failure latch), `internal/server` (`HandleCompactSession` status mapping incl. the user-cancel row), `internal/tui` (no auto re-dispatch after a failed pass, composer restore), `web/` (sticky inline error + app-wide action error, queued-message restore on failure).
- **See also:** `concepts/compaction-cancellation.md` — user-initiated cancel of an in-flight pass (the `compact/cancel` endpoint, the `ErrCompactionCanceled` sentinel vs a bare `context.Canceled`, Stop-also-cancels, and the Cancel button).

Large multi-batch `/compact` passes used to fail silently: one shared inactivity context spanned the whole pass, a slow first token was charged against the idle budget from t=0, a concurrent chat could clear the summary stream's `OnDelta` hook mid-flight, and *every* failure surfaced as HTTP 500. This page documents the shipped behavior that replaced that.

## 1. Config surface

Two independent timeouts and a retry budget, all in the `compact` section of `ocodeconfig.json`:

| Field | Default | Meaning |
|---|---|---|
| `summary_first_token_timeout_seconds` | **300** | Deadline for the **first** streamed token of each summary batch (slow first token / long prefill). |
| `summary_timeout_seconds` | **600** | **Idle** timeout *after* the first token has streamed — resets on every delta. |
| `summary_max_retries` | **1** | Retries per summary batch inside `runSummary`, plus one extra attempt reserved for malformed output — see below. |

Storage and runtime are deliberately two-stage:

- **Persist (`applyCompactConfig`, `internal/config/ocodeconfig.go:2105`)** — copies an explicitly-present value verbatim, **including `0`**. An explicit `0` round-trips as `0`; it is never silently rewritten on disk.
- **Resolve (`resolveCompactRuntime`, `internal/agent/agent.go:2122`)** — the only place that normalizes: `summary_first_token_timeout_seconds <= 0` → **300** (`agent.go:2171-2173`), `summary_timeout_seconds <= 0` → **600** (`agent.go:2167-2169`).

So the semantics of an explicit `0` **for the two timeouts** are *"use the runtime default"* (300 / 600), **not** "disable this timeout". Config cannot turn either timeout off; the `idle <= 0` plain-context branch inside `inactivityContextWithParent` is defense-in-depth unreachable through normal config resolution. Absent key → default 300; no migration.

`summary_max_retries` is the third knob and behaves differently:

- **Default `1`** comes from `defaultCompactConfig` (`internal/config/ocodeconfig.go:1207`); `LoadOcodeConfig` starts from `defaultOcodeConfig()` (`ocodeconfig.go:1361-1362`, `Compact: defaultCompactConfig()` at `:1246`) and overlays the file, so an absent key → `1`.
- `resolveCompactRuntime` only clamps **negatives** to `0` (`agent.go:2175-2177`) — there is no `<= 0 → default` normalization — so an explicit `0` really means **no retries**.
- Semantics live entirely in `runSummary` (`internal/agent/compact.go:856`): `maxAttempts := maxRetries + 1` (`compact.go:879-881`), and **one additional attempt is reserved for a malformed (template-violating) summary** even when `maxRetries` is `0` (`compact.go:884-886` — the extra attempt is only taken when a malformed summary was already recorded). Backoff between attempts is `attempt × 500 ms` (`compact.go:890`).
- A malformed summary that survives all attempts is returned as a **degraded success** (`compact.go:939-944`) — the batch and the pass succeed and the transcript shrinks, so (relevant to §5) a degraded success does **not** arm the stop-after-failure latch.
- `runCompact` passes the resolved value through per batch (`agent.go:2833`).

**API / UI:** `GET|PUT /api/config/ocode/compact` (`internal/server/server.go:412-413`) serve the raw persisted values; the web `CompactForm` (`web/src/components/Settings/CompactForm.tsx:23`, rows "First-token timeout (s)" / "Summary max retries" at `:18-19`) edits them through `api.getCompactConfig`/`setCompactConfig` (`web/src/api/client.ts:1055`, `:1061`).

## 2. Runtime data flow (one compaction pass)

```
web /compact  →  POST /api/sessions/{id}/compact  (server.go:351)
  → HandleCompactSession (handler.go:1871)
  → Agent.CompactWithFocus → runCompact (agent.go:2705)
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

1. **Fresh inactivity context per batch** — `agent.go:2817` constructs the window *inside* the batch loop, so batch N gets a full deadline instead of the residual of batch N-1 (`TestRunCompactGivesEachBatchFreshTimeout`).
2. **First-token window, then idle window** — `inactivityContextWithParent` (`compact.go:1131`) starts at `initial` (first-token timeout) and, after the first delta-driven `reset()`, switches to `idle` (`summary_timeout_seconds`) for subsequent gaps (`TestInactivityContextGivesFirstTokenItsOwnWindow`). Retries within a single batch share that batch's first-token window; the first successful streamed token switches that batch to the configured idle timeout, and a new batch gets a new first-token window.
3. **Fixed 30-minute overall cap** — `compactOverallCap = 30 * time.Minute` (`compact.go:217`, a var so tests can shorten it) bound via `context.WithTimeoutCause(..., ErrCompactionTimeout)` (`compact.go:243`). It is an *operation* bound, not an idle bound: per-batch contexts are its children, so a batch that is **still receiving tokens** is aborted at the cap (`TestRunCompactOverallCapStopsAnActiveBatch`).
4. **No partial mutation** — `runCompact` returns `CompactResult{OK:false, Err}` before any splice; `HandleCompactSession` only rewrites `as.messages` when `result.OK` (the `!result.OK` early return is `handler.go:1921`, the splice `handler.go:1969-1975`). Failed compaction leaves the transcript byte-identical (asserted by `TestCompactSessionTimeoutReturns504AndLeavesTranscriptUnchanged`).

## 3. Delta-callback ownership

Two deliberate, *different* lifetimes coexist:

- **Compaction (per-call, context-scoped):** `runCompact` attaches the idle-reset hook with `withDeltaCallback(batchCtx, reset)` (`agent.go:2823`, `client.go:69`). `GenericClient.ChatWithContext` reads it from the request context (`client.go:783`) and routes stream deltas to it **exclusively** — the shared `OnDelta` field is neither wrapped nor invoked when a per-call callback exists. A concurrent chat calling `SetOnDelta(nil)` can no longer clear the compaction reset hook, and summary deltas no longer leak into a chat's stream (`TestChatWithContextPerCallDeltaCallbackTakesPrecedence`).
- **Main turn (shared, `chatWithDelta`):** `chatWithDelta` (`agent.go:1071`) still does `gc.SetOnDelta(a.OnDelta); defer gc.SetOnDelta(nil)` around one `Chat` call (`agent.go:1075-1076`). This **shared-callback lifetime is intentional and unchanged** — it is how the agent's `OnDelta` reaches the stream for the duration of a single call, and the always-clear-on-return contract is what keeps subagents from inheriting a stale callback. Do not "unify" it with the context path.

## 4. Error classification (server)

`ErrCompactionTimeout` (`compact.go:159`) is the **only** timeout sentinel. It is set as the cancellation *cause* by the per-batch inactivity expiry (`compact.go:1163`, `:1183`) and by the 30-min operation cap (`compact.go:243`), and survives wrapping.

`runSummary` labels a failed context through the single helper `summaryContextErr` (`compact.go:1004`): it returns nil while the context is still live (`compact.go:1006-1008`), wraps a cause satisfying `errors.Is(cause, ErrCompactionTimeout)` — compaction's own deadline, i.e. either the 30-minute `compactOverallCap` or the per-batch inactivity timer — as `compact: summary timed out: %w` (`compact.go:1010`), and wraps **any other cause** as `compact: summary cancelled: %w` (`compact.go:1012`). Both branches use `%w`, so `errors.Is` matches through the label.

There are exactly **four** places in `runSummary` that can report a dead context — the retry-backoff wait (`compact.go:888-889`), the `ctx.Done()` branch of the wait `select` (`compact.go:912-913`), the failure path of the `done` branch (`compact.go:926-928`), and the post-loop check (`compact.go:936-938`) — and all four call `summaryContextErr`. Earlier revisions had three of them carrying their own inline copy of the `errors.Is(cause, ErrCompactionTimeout)` branch, and the fourth (the retry-backoff wait) labelling every cause as `compact: context cancelled during retry` regardless of class. One rule, four call sites, no site able to drift.

The cause/class split exists because the old code wrapped *every* context cause with the literal prefix `compact: summary timed out:`, so a plain `context.Canceled` surfaced as the self-contradicting `compact: summary timed out: context canceled`. That is not hypothetical: the exact string was recorded 5 times in session `ses_2026-09-24-145135-98fed7b1` (2026-09-25, ~14:45 +0800), where a compaction pass died because the then-current code shared one inactivity context across all summary batches — §2's shared-context bug, already fixed — and its watchdog called a bare `cancel()`. The misleading label is what sent the diagnosis down the wrong path: the remedies differ (raise the deadline vs find whatever cancelled the pass), and "timed out" pointed at the wrong one.

The same change also closes the **other** side of the race the 2026-09-25 fix covered (that fix handled a result chosen while a cancel was already visible; this handles a cancel observed while a result was already in hand). Each attempt now delivers a named `summaryResult{content, err}` (`compact.go:950`) on a buffered channel (`compact.go:893`), and `runSummary` parks on that channel and on `ctx.Done()` (`compact.go:912`) in a single `select`. Go picks uniformly at random when both cases are ready, so a summary that landed in the same instant the inactivity timer fired was discarded on roughly half of those attempts — failing an otherwise successful compaction. The `ctx.Done()` branch is now one line, `return drainBufferedSummary(done, ctx)` (`compact.go:913`), delegating to the helper at `compact.go:985`: it performs a non-blocking receive and keeps the summary when one is already buffered **and** `usableSummary` (`compact.go:959`) accepts it, otherwise it returns `summaryContextErr(ctx)` (`compact.go:993`).

**`drainBufferedSummary` must only be called once `ctx.Done()` has already fired**, which is its sole call site. With a still-live context and an empty channel it returns `("", nil)` — `summaryContextErr` reports nothing to cancel — so a caller reaching it on a live context would read "no summary yet" as "no cancellation" and treat an empty string as a successful summary. `usableSummary` is deliberately conservative: it rejects a transport error, an error delivered alongside content, and content that is empty, whitespace-only, or missing required template sections — returning a malformed summary from the drain would bypass the retry the normal path would still have performed (`compact.go:918`). A summary that has not landed when the cancel fires is genuinely unavailable, and a buffered transport error loses to the cancellation cause, which explains more.

`HandleCompactSession` (`handler.go:1871`) classifies **by sentinel identity only**, never by message text or by "is this a `context.Canceled`":

| Condition | Status | Response written? | Notes |
|---|---|---|---|
| Session not found | 404 | yes | `handler.go:1874` |
| Compaction disabled in config | 422 | yes | `!enabled` at `handler.go:1916-1920` |
| Nothing to compact (`!OK`, `Err == nil`) | 422 | yes | `handler.go:1964-1966` |
| `errors.Is(Err, ErrCompactionTimeout)` (batch idle/first-token expiry **or** 30-min cap) | **504** | yes, **if the request ctx is alive** | `"compaction timed out; transcript unchanged; retry the command"` (`handler.go:1935`); disconnect check at `:1926-1928` |
| Request already cancelled / client gone (`r.Context().Err() != nil`) | — | **no** | logged + `finish()` only; the client has disconnected, writing is pointless (timeout branch clears the banner at `handler.go:1925-1934`, otherwise `handler.go:1938-1942`) |
| `errors.Is(Err, ErrCompactionCanceled)` (user's Cancel button or session Stop), request alive | **200** | yes | `handler.go:1943-1957`: `finish("")` clears the shared indicator with no error banner; body `{"cancelled": true, "original_len", "compacted_len"}`. Full semantics: `concepts/compaction-cancellation.md` |
| Any other error, request alive | 500 | yes | `handler.go:1959-1962`; includes bare `context.Canceled` that is *not* the sentinel |
| Success | 200 | yes | original/compacted lengths |

Three classification gotchas, all intentional:

- **After the split, a "timed out" message implies the sentinel class — but keep classifying by `errors.Is`.** `summaryContextErr` labels the sentinel class `timed out` and every other cause `cancelled`, so a `compact: summary timed out:` string now reliably corresponds to `ErrCompactionTimeout`; the old mislabel (a plain `context.Canceled` reported as a timeout) is gone. Because all four dead-context sites now share one rule, the message agrees with the class in **both** directions — the retry-backoff wait used to be the counter-example, wrapping any cause as `compact: context cancelled during retry` even when it was the sentinel, but it too goes through `summaryContextErr` (`compact.go:888-889`) now. Treat that agreement as informational, not load-bearing: text is not the contract, it is trivially editable, and it says nothing about causes that arrive as `lastErr` (transport failures, `compact: empty summary response`) which are not classified at all. Only `errors.Is` yields 504.
- **Bare cancellation ≠ timeout.** A request disconnect or any non-sentinel cancellation must **not** become 504 (`handler.go:1938-1942` logs `"compaction request cancelled"` and writes nothing). Pre-existing 404/422 mappings are unchanged.
- **A user cancel is a third class — neither timeout nor failure.** `ErrCompactionCanceled` is checked only after the timeout and dead-request branches and yields the clean 200 row above; it must never reach the 500 fallthrough nor populate `compaction_error`, while a *bare* provider `context.Canceled` with a live request must stay a 500 (`TestCompactSessionBareCancellationRemains500`). That asymmetry is deliberate — see `concepts/compaction-cancellation.md` §3.

The transcript is never mutated on any of the failure rows, and `compaction_done` still fires with `ok:false` + the error via `finishCompaction` so clients reconcile. The user-cancel row mutates nothing either and fires `compaction_done` with `ok:true`.

**Classification is per-pass — but a failure does not only produce a status code.** It also stops auto-compaction from re-arming the identical failing pass at the next step boundary; that stop-after-failure policy is §5 below.

## 5. Failure handling / stop after failure (shipped 2026-09-30)

This section is **new behaviour added after the approved 2026-09-25 design**, not a change to it: that spec's §4 "Non-goals" explicitly listed *"No changes to compaction prompt, section validation, **retry policy**, `pruneToolResults`, splice/anchoring logic, or auto-compaction thresholds"* (`superpowers/specs/2026-09-25-web-compact-large-context-reliability-design.md`), so retry/stop policy was deliberately out of scope there. §4 above classifies one failed pass; this section documents what happens **after** one.

### The loop it breaks

Because a failed pass leaves the transcript untouched (§2 property 4), the context stays over threshold, so the next step boundary re-armed the *identical* failing pass: an unbounded loop — one "Compaction failed" warning per turn — that the user could only escape by switching sessions. The fix has three cooperating layers: a backend latch shared by TUI and web/desktop, a TUI-side refusal to re-dispatch, and returning queued messages to the composer on both surfaces.

### Backend latch (`internal/agent`)

- `Agent.compactFailed` (atomic.Bool, `internal/agent/agent.go:737-747`) latches **ON** when a pass returns a non-nil `CompactResult.Err` and **OFF** when a pass returns `OK: true` — both recorded by `recordCompactOutcome` (`agent.go:2246-2256`). A pass with `OK=false, Err=nil` (the "nothing to compact" short-circuit, §4's 422 row) does **not** touch the latch — otherwise a session that briefly had no compactible middle would silently lose auto-compaction forever. A user-initiated cancel (`ErrCompactionCanceled`) also leaves the latch alone — see `concepts/compaction-cancellation.md` §6.
- While latched, `MaybeCompactAsync` (`agent.go:2195`) declines (returns false) after the enabled gate, logging `skipped: auto-compaction stopped after a failed pass; run /compact to retry` (`agent.go:2207-2209`). That single gate covers **every** auto trigger: the TUI `askAgent` pre-flight (`internal/tui/model.go:15398`), the TUI post-turn stream-done check (`model.go:5222`), the server's post-turn checks (`internal/server/agent_session.go:1294`, `handler_sse.go:294`), and the permission/question continuations (`handler_permissions_resolve.go:368`, `handler_questions.go:333`).
- `Agent.CompactFailed() bool` (`agent.go:2261-2263`) exposes the latch so a UI can explain why the session stopped compacting instead of leaving the user to guess.
- **Re-arm:** any successful pass clears the latch (`recordCompactOutcome`, called from the async result path `agent.go:2315`, the panic/abort fallback `agent.go:2309`, and the synchronous manual paths `Compact`/`CompactWithFocus` at `agent.go:2363`/`:2377`). A manual `/compact` additionally re-arms unconditionally: `CompactAsync` → `startCompactAsync(force=true)` stores `false` (`agent.go:2286`) **after** the "no LLM client" and `compactMu.TryLock` guards (`agent.go:2271-2278`) — deliberately after them, so a `/compact` that could not start does not resume the loop it was meant to escape. The panic/abort fallback inside `startCompactAsync` (`agent.go:2303-2312`) latches instead, since that pass did not shrink the context either. The web/desktop HTTP path (`CompactWithFocus`, `agent.go:2370-2379`) records its outcome the same way, so a successful manual pass clears the latch there too.

### TUI: no auto re-dispatch after a failure

Both TUI trigger points defer a turn via `pendingCompactResume` (`model.go:5224`, `:15401`), and `compactFinishedMsg` (`model.go:5343`) used to honour it unconditionally. On a failed result it now returns early (`model.go:5349-5375`) instead of falling through to the shared drain/resume tail. That early return is the **only** thing that stops the loop — verified by mutation testing (removing it fails `TestFailedCompactionDoesNotResumeTheDeferredTurn`; an earlier `resume = false` assignment was dead code and was removed). Skipping the tail also stops the queue drain, because a drained command resets the composer — which would wipe the text just restored — and a queued `/compact` would immediately re-run the pass that just failed. Deferred background jobs and the queue stay parked and drain on the next turn.

### Queued messages are returned to the composer (both surfaces)

A failed pass never reaches the LLM, so anything submitted while it ran still exists only in the queue. After a failure the queued plain-text submissions move back into the composer:

- **Ordering:** queued messages first in submission order (chronological), then the user's current draft as the suffix, joined with a single `"\n"`. Blank/whitespace-only entries are dropped so they cannot leave a doubled separator.
- **Commands (`/compact`, `!ls`) are deliberately NOT inlined** — they are not composer prose and stay queued/dispatchable.
- **TUI:** `mergeQueuedIntoDraft` (`internal/tui/model.go:9018`) + `restoreQueuedMessagesToComposer` (`model.go:9044`). The caret goes to the end of the merged value (`m.input.MoveToEnd()`, `model.go:9072`) because the bubbles textarea API cannot address an arbitrary caret — only `CursorStart`/`CursorEnd`/`SetCursorColumn` are settable and `Cursor()` returns a screen position.
- **Web/desktop:** `mergeQueuedIntoDraft` (`web/src/lib/tabQueue.ts:179`) + `drainQueuedMessagesIntoDraft` (`tabQueue.ts:194`), applied by an effect in `web/src/components/Chat/ChatInput.tsx:205-231` on the transition into `{status:"error"}`. Here the caret IS preserved exactly: the inserted `draftStart` offset is added to the existing `selectionStart`/`selectionEnd` in a layout effect (`ChatInput.tsx:236-247`) — a rAF hop was tried first and let the browser paint the caret at the stale offset. The effect fires **once per DISTINCT error string** (`handledCompactErrorRef`, `ChatInput.tsx:203`), because `/state` reconciliation re-publishes the same error on every poll and re-merging would duplicate the text.
- **Injection reconciliation:** a message typed while a turn was streaming is also handed to the live agent loop via `EnqueueInjection` (`internal/agent/agent.go:92`); restoring it without dropping the matching pending injection would send it twice. The TUI extracts that reconciliation into `discardPickedUpInjections` (`model.go:9078`), now shared with `drainQueuedItems` (`model.go:9105`).

### User-visible copy (TUI)

The failure line now reads `⚠ Compaction failed: <err> (conversation continues uncompacted; auto-compaction is off until /compact succeeds)` (`model.go:5361`), plus a transient hint `↩ your queued message was returned to the input box` when something was restored (`model.go:5363`). The session stays usable: the user can keep typing and sending. Web/desktop keeps the §6 sticky error surfaces.

### Tests

- `internal/agent/compact_stop_test.go` — `TestAutoCompactionStopsAfterAFailedPass`, `TestSuccessfulAndNoOpPassesDoNotLatch`, `TestManualCompactClearsTheFailedPassLatch`.
- `internal/tui/compact_failure_stop_test.go` — `TestFailedCompactionDoesNotResumeTheDeferredTurn` (mutation-verified), `TestSuccessfulCompactionStillDrainsAndResumes`, `TestFailedCompactionRestoresQueuedMessagesIntoTheComposer`, `TestFailedCompactionDropsTheMatchingPendingInjection`, `TestMergeQueuedIntoDraft`.
- `web/src/components/Chat/ChatInput.compactionRestore.test.tsx` + `web/src/lib/tabQueue.restore.test.ts` — restore order and suffix, empty-composer case, commands kept queued, caret anchored, once-per-distinct-error, re-restore after a later success.

## 6. Web / desktop surfacing

`handleCompact` (`web/src/components/Chat/commands.ts:1564`; dual surfacing in the catch at `:1589-1590`) keeps **both** surfaces on rejection:

1. **Sticky inline error** — `setCompactionState(sessionId, {status:"error", error})` → the composer `CompactionStatus` bar (`role="alert"`, "Compaction failed: …", dismissible) plus an inline `**Compaction failed:** …` transcript message. Session-scoped, survives view changes; cleared only by a new attempt, completion, or dismissal.
2. **App-wide action error** — `reportActionError(err, "Compact conversation")` (`web/src/lib/actionErrors.ts:55`), the sticky toast that outlives composer/tab remounts.

The synchronous `active` state is set *before* the `await` (closing the submission race), and `compaction_started` / `compaction_done` SSE events (`sessionEvents.ts:553-562`) reconcile the same store across clients. Success clears the bar — the persisted compaction-summary notice in the transcript is the completion record. On failure, the composer additionally drains queued messages back into the draft (§5); on the TUI, the equivalent restore is `restoreQueuedMessagesToComposer`. Both running bars also carry the user's Cancel button (`concepts/compaction-cancellation.md` §8).

The composer now has **two** activity rows above it, and they must never both paint the same operation. `web/src/components/Chat/ChatInput.tsx` renders `<CommandActivityBar>` (new, untracked) immediately above `<CompactionStatus>`; when the new bar was added next to the old one, a local `/compact` produced TWO stacked spinner + elapsed rows — "Running /compact · 17s elapsed" and "Compacting conversation… 17s elapsed" — for a single pass.

The two bars have deliberately different owners. `CommandActivityBar` (`web/src/lib/commandActivity.ts` + `web/src/components/Chat/CommandActivityBar.tsx`) owns *what THIS client is running right now*: any client-side slash command awaiting the server, plus the skill the model loaded. It has a ~400 ms render-side reveal delay so instant commands never flash a bar. `CompactionStatus` owns the compaction feedback the command bar cannot see.

The suppression between them is deliberately **narrow** — it is not a removal of the compaction bar. `CompactionStatus` suppresses only its RUNNING row, and only under exactly this condition:

```ts
state?.status === "active" && activity?.kind === "command" && isCompactCommand(activity.label)
```

with `activity` from `useSessionActivity(sessionId)` and `isCompactCommand` from `web/src/lib/compactionState.ts`. All four consequences are intended:

- **SUPPRESSED — a compaction this client started itself via `/compact`.** Both entry paths are covered — the typed-submit path and the queue-drain path — because both route through the same wrapper that records command activity.
- **STILL SHOWN — a compaction started by ANOTHER client** (the cross-client `compaction_started` indicator described above): it sets compaction state with no local command activity behind it, so the compaction bar is its only signal.
- **STILL SHOWN — a server-side automatic / auto-compaction pass**, for the same reason: no local command activity behind it.
- **STILL SHOWN, and coexisting — an unrelated `skill` command-activity bar.** The check is scoped to `/compact`, so a skill bar never displaces the compaction bar.

Explicitly **NOT** suppressed — so a future reader does not "simplify" the condition away — are the queued row ("Compaction queued — …") and the error row (`role="alert"`, "Compaction failed: …"). The dismiss `X` is now gated on the ERROR state explicitly instead of on `!active`, because a suppressed-but-active state would otherwise have rendered a dismiss button that `dismissCompaction` refuses to act on (it refuses for ANY active operation). That gating is the one incidental fix in this change.

## 7. Tests & validation

| Area | Suite |
|---|---|
| Config default (300) + explicit-`0`/`300` persistence | `internal/config/compact_config_reliability_test.go` |
| Persist API round-trip | `internal/server/handler_config_test.go` `TestHandleSetCompactConfigPersists` |
| Fresh per-batch deadline, first-token grace, 30-min cap on an active stream, runtime `<=0 → 300` normalization | `internal/agent/compact_reliability_test.go` |
| Per-call delta callback precedence over shared `OnDelta` | `internal/agent/client_reliability_test.go` |
| Cancel-vs-timeout label split + drain accept/reject contract | `internal/agent/compact_cancel_test.go` |
| User-initiated cancel: agent registry, endpoint, Stop-also-cancels, clean terminal event, no latch | `internal/agent/compact_cancel_test.go`, `internal/agent/compact_outcome_cancel_test.go`, `internal/server/compact_cancel_test.go`, `web/src/components/Chat/CompactionCancelButton.test.tsx` |
| 504 + transcript unchanged | `internal/server/compact_timeout_test.go` |
| Sticky inline error + app-wide action error on `/compact` rejection | `web/src/components/Chat/commands.compact.error.test.tsx` |
| Composer lifecycle (sticky across remounts, queued work after failure) | `web/src/components/Chat/ChatInput.compaction.test.tsx` |
| Stop-after-failure latch, no re-dispatch, composer restore | see §5 Tests (agent + TUI + web suites) |

The §4 drain race is not deterministically reproducible from outside `runSummary` (Go's `select` picks at random only when both cases are ready, which needs scheduler control to arrange), so its invariant is enforced *structurally* — the drain delegates to `usableSummary` rather than re-implementing the check — and pinned by `TestUsableSummary` there instead of by a timing test; `TestRunSummaryLabelsNonTimeoutCancelAsCancelled` and `TestRunSummaryLabelsCompactionDeadlineAsTimeout` keep the two label classes distinguishable.

Docs: `README.md` (config example + "First-token timeout" feature row), `CHANGES.md` 2026-09-25 entry and the 2026-09-30 stop-after-failure entry, `skills/ocode-agent-architecture/SKILL.md` compaction section, the approved design spec linked above, and `concepts/compaction-cancellation.md` for the cancel path.

## 8. Invariants to preserve

- Persist ≠ resolve: **never** clamp explicit `0` in `applyCompactConfig`; **always** normalize `<= 0` in `resolveCompactRuntime` (timeouts only — `summary_max_retries` clamps negatives only, and an explicit `0` there means "no retries").
- `ErrCompactionTimeout` is the sole 504 trigger — and this is now **enforced in code, not merely observed in the message**: `summaryContextErr` makes the message agree with the class (sentinel → `timed out`, any other cause → `cancelled`). Bare cancellation is still not a timeout, and `HandleCompactSession` must keep deciding via `errors.Is(err, ErrCompactionTimeout)` — **never the message text**. The post-split string/class agreement is informational; the sentinel is the contract.
- **A user cancel is decided by `errors.Is(err, ErrCompactionCanceled)` only**: clean 200 + `finish("")` + `compaction_done {ok:true}`, never 504/500, never a populated `compaction_error` — while a bare `context.Canceled` with a live request must stay a 500. Stop must not record `pendingCancel` for compaction-only work. Details: `concepts/compaction-cancellation.md`.
- Failure ⇒ transcript unchanged (no splice, no save).
- Stop-after-failure (§5): `compactFailed` latches on `Err != nil` only — never on `OK=false, Err=nil`, and never on a user cancel; all auto triggers must keep going through `MaybeCompactAsync` (never calling `startCompactAsync` directly), and the manual re-arm must stay **after** the client/`TryLock` guards. A failed pass must never re-dispatch the deferred TUI turn or drain the queue behind it.
- The context-scoped delta callback is for compaction; `chatWithDelta`'s shared `SetOnDelta`/clear pair stays as-is on purpose.