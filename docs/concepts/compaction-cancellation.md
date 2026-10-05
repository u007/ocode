---
type: Concept
title: 'Compaction Cancellation: User-Initiated Cancel of an In-Flight Pass'
description: 'User-initiated cancellation of in-flight conversation compaction: the compact/cancel endpoint, the ErrCompactionCanceled sentinel vs bare context.Canceled, Stop-also-cancels without poisoning pendingCancel, register-before-OnCompactStart ordering, and the web/desktop Cancel button.'
tags:
  - compaction
  - cancellation
  - api
  - server
  - web
  - session-lifecycle
  - sse
timestamp: 2026-10-01T08:28:01Z
---
# Compaction Cancellation: User-Initiated Cancel of an In-Flight Pass

- **Status:** Implemented (2026-10-01). All anchors below re-derived from source.
- **Scope:** `internal/agent` (pass registry, `CancelCompaction`, `ErrCompactionCanceled`), `internal/server` (cancel endpoint, Stop path, result classification), `web/` (`CompactionCancelButton`, both compaction bars).
- **Related:** `concepts/compaction-config.md` (timeout/error classification §4 — the cancel row lives there too), `superpowers/specs/2026-09-25-cross-client-compaction-indicator-design.md` (the `compaction_started`/`compaction_done` lifecycle every client converges on), `superpowers/specs/2026-09-25-web-compact-large-context-reliability-design.md` (per-batch windows + `ErrCompactionTimeout`).

A compaction pass can run for minutes (30-minute `compactOverallCap`, multi-batch summaries). Before this feature the only way to stop one was to close the session or wait: the web/desktop bars had no Cancel affordance, and the session Stop endpoint did not touch compaction at all (`Agent.Cancel()` only closes the Step loop's stop channel, which `runCompact` never observes). This page documents the shipped cancellation path end to end.

## 1. Endpoint

```
POST /api/sessions/{id}/compact/cancel        →  200 {"cancelled": true | false}
                                                 404 {"error": "session not found"}   (unknown session)
```

- Route: `internal/server/server.go:352` (registered next to `POST /api/sessions/{id}/compact` at `:351`, behind `s.authMiddleware`); wrapper `handleCancelCompaction` at `internal/server/server.go:2093-2095`.
- Handler: `Handler.HandleCancelCompaction` (`internal/server/handler_cancel.go:94-104`). It resolves the session (404 if unknown), then calls `as.agent.CancelCompaction()` and returns `{"cancelled": <bool>}`.
- **Idempotent by design:** cancelling with nothing in flight returns `200 {"cancelled": false}`, not an error, so a stale or double-clicked Cancel button can never surface as a failure (`TestCancelCompactionEndpointNoPassIsNoop`, `internal/server/compact_cancel_test.go:198`).
- **No lifecycle bookkeeping in this handler.** The running pass retires its own slot and publishes the terminal `compaction_done` frame itself — a second publisher here would double-decrement the session's compaction counter (see the indicator spec §5 ordering rules).
- Client: `api.cancelCompaction(id, host)` (`web/src/api/client.ts:2401-2406`) POSTs the route through the optional `/api/remote/{host}` prefix.

## 2. Agent primitive: a registry of in-flight passes

`Agent.CancelCompaction() bool` (`internal/agent/compact.go:198-212`) cancels **every** in-flight compaction pass for the agent — manual `/compact` and automatic passes alike — with cause `ErrCompactionCanceled`, and reports whether at least one pass was active. It is safe to call when nothing is compacting (returns `false`).

- **Registry:** `compactPassMu` / `compactPassCancels map[uint64]context.CancelCauseFunc` / `compactPassSeq` on the `Agent` (`internal/agent/agent.go:725-736`), lazily initialised in `registerCompactPass` (`internal/agent/compact.go:180-190`) because many tests construct `&Agent{}` directly.
- **The mutex is deliberately separate from `compactMu`** (`internal/agent/agent.go:725-728`): the compaction goroutine holds `compactMu` for the whole pass, so a cancel arriving from an HTTP handler must be able to read the registry *without waiting on the pass it is cancelling*.
- **Registration:** `beginCompactPass` (`internal/agent/compact.go:167-178`) creates the operation context, registers its cause-cancel under a fresh id, and returns a release func that unregisters and releases the context (`internal/agent/compact.go:174-177`). Callers `defer release()` around the whole pass.
- `CancelCompaction` snapshots the cancel funcs under the lock, releases it, then fires them outside the lock (`internal/agent/compact.go:202-211`) — never holding `compactPassMu` across the cancellation of a pass.

Both entry points register: manual synchronous compaction goes through `runCompact` (`internal/agent/agent.go:2810-2814`), the async automatic/manual path through `beginCompactPass` inside `startCompactAsync` (`internal/agent/agent.go:2387`).

## 3. Why a distinct sentinel (`ErrCompactionCanceled` ≠ bare `context.Canceled`)

`ErrCompactionCanceled` (`internal/agent/compact.go:161-165`) exists because **a bare provider `context.Canceled` observed while the HTTP request is still alive must stay a real failure** — pinned by `TestCompactSessionBareCancellationRemains500` (`internal/server/compact_timeout_test.go:91`).

- A cancellation nobody asked for (provider abort, transport teardown, a mis-routed context) is a *fault*: silently reporting it as a clean user cancel would discard a compaction the user still wants and hide the fault from every client. A user cancel is *intent*: it must clear the shared indicator with no error banner.
- `summaryContextErr` wraps the cause so the class survives: a non-timeout cause becomes `compact: summary cancelled: %w` (`internal/agent/compact.go:1012`), and because it wraps with `%w`, `errors.Is(err, agent.ErrCompactionCanceled)` matches through the label. Classification is **by sentinel identity only, never by message text**.
- In `HandleCompactSession` the ordering matters: the `ErrCompactionTimeout` branch (`internal/server/handler.go:1923-1937`) and the dead-request branch (`internal/server/handler.go:1938-1942`) are checked **before** the cancel-sentinel branch (`internal/server/handler.go:1943`); a bare `context.Canceled` that reaches neither falls through to the 500 (`internal/server/handler.go:1959-1962`).

## 4. Context plumbing: parent cause, child deadline

`newCompactOperationContext` (`internal/agent/compact.go:234-248`) now returns a `context.CancelCauseFunc`:

- the **parent** is `context.WithCancelCause` and carries the caller's cancel cause (`ErrCompactionCanceled` for a user cancel);
- the **child** is `context.WithTimeoutCause(parent, compactOverallCap, ErrCompactionTimeout)` (`internal/agent/compact.go:243`) and owns the fixed 30-minute cap (`compactOverallCap`, `internal/agent/compact.go:217`), so a cap expiry still reports `ErrCompactionTimeout` and maps to 504 (see `concepts/compaction-config.md` §4).

The returned func cancels the **parent before the child's timeout cancel** (`internal/agent/compact.go:244-246`): cancelling the parent propagates its cause to the child, so `context.Cause(child)` reports *which* one ended the pass. Doing it the other way round would set the child's cause to `context.Canceled` and lose the reason.

## 5. Ordering: the pass is registered before `OnCompactStart`

`startCompactAsync` registers the pass **before** calling `OnCompactStart` (`internal/agent/agent.go:2384-2390`):

1. `ctx, release := a.beginCompactPass()` — registry entry exists;
2. `a.OnCompactStart()` — this is what makes the operation visible to clients (the server's `beginCompaction` → `compaction_started` → every connected client's Cancel button).

A client that reacts to `compaction_started` by pressing Cancel must therefore **always find a registered pass** — otherwise `CancelCompaction` would return `false`, the request would report `cancelled:false`, and the pass would run to completion underneath a Cancel button that appeared to work.

To support this, `runCompact` was split: `runCompact` (`internal/agent/agent.go:2810-2814`) creates + registers the pass itself and is used by the synchronous manual `Compact`/`CompactWithFocus` (`internal/agent/agent.go:2475`, `internal/agent/agent.go:2471`) and by direct test callers; `runCompactWithCtx` (`internal/agent/agent.go:2806-2813`) is the body on a caller-supplied context, which `startCompactAsync` invokes after its own registration (`internal/agent/agent.go:2408`).

## 6. Server result classification

### Manual path — `HandleCompactSession` (`internal/server/handler.go:1889`)

On `errors.Is(result.Err, agent.ErrCompactionCanceled)` with a **live** request (`internal/server/handler.go:1943`): `finish("")` — a clean `compaction_done {ok:true}`, no error banner — then `200 {"cancelled": true, "original_len": result.OriginalLen, "compacted_len": len(as.messages)}` (`internal/server/handler.go:1951-1956`). The branch sits **after** the `ErrCompactionTimeout` and `r.Context().Err() != nil` branches, never before them.

### Automatic path — `applyCompactResult` (`internal/server/agent_session.go:2100`)

Treats `ErrCompactionCanceled` as a clean cancel (`internal/server/agent_session.go:2088-2092`): `compactionErr` stays `""`, the failure log at `internal/server/agent_session.go:2099-2103` is skipped, and the deferred `finishCompaction` (`internal/server/agent_session.go:2096-2098`) still always retires the lifecycle slot. The terminal `compaction_done {ok:true}` therefore clears the shared indicator on **every** client, not just the one that pressed Cancel.

### Latch: a cancel never stops auto-compaction

`recordCompactOutcome` (`internal/agent/agent.go:2341-2351`) classifies `ErrCompactionCanceled` exactly like `context.Canceled` (`internal/agent/agent.go:2343-2345`): neither latches `compactFailed`. A user cancel says nothing about whether the next pass would succeed — see `concepts/compaction-config.md` §5 for the latch itself.

## 7. Stop also cancels — and the `pendingCancel` poisoning hazard

`interruptSessionWork` (`internal/server/handler_cancel.go:42-86`), the shared body behind `POST /api/sessions/{id}/cancel`, now reads `h.sessions.IsCompacting(id)` (`internal/server/handler_cancel.go:51`; `SessionManager.IsCompacting` at `internal/server/session_manager.go:474-484`):

- **No active turn** (`internal/server/handler_cancel.go:54-68`): it clears any stale `pendingCancel` flag (`internal/server/handler_cancel.go:60`) and, if a compaction pass is running, calls `as.agent.CancelCompaction()` (`internal/server/handler_cancel.go:62-66`). It deliberately does **not** record `pendingCancel` — **that is the poisoning hazard**: `pendingCancel` is consumed by `executeTurnJob` at the start of the *next* turn, and a compaction pass has no `executeTurnJob` waiting to consume it. Recording the flag for compaction-only work would swallow the next user message with a cancelled `turn_error` and never run it.
- **Active turn** (`internal/server/handler_cancel.go:69-85`): after `as.agent.Cancel()` it also calls `as.agent.CancelCompaction()` (`internal/server/handler_cancel.go:79-82`), because `Agent.Cancel` only closes the Step loop's stop channel, which `runCompact` does not observe. (Here `pendingCancel` *is* recorded — a real turn job will consume it.)
- `HandleCancelSession` (`internal/server/handler_cancel.go:20-27`) keeps its existing `{"cancelled": true}` response; only the internal behavior widened.

## 8. Web / desktop UI (one SPA for both)

`CompactionCancelButton` (`web/src/components/Chat/CompactionCancelButton.tsx:13-49`):

- Shows **Cancel**; while the request is in flight it disables itself and reads **"Cancelling…"** (`web/src/components/Chat/CompactionCancelButton.tsx:36-48`), so a second click is a no-op.
- On error it re-enables and surfaces `reportActionError(err, "Cancel compaction")` (`web/src/components/Chat/CompactionCancelButton.tsx:30-33`).
- **On success it stays disabled and clears nothing.** Only the server's `compaction_done` retires the bar — that is what makes every connected client (the initiator, a second browser, a phone) converge on the same terminal frame instead of each client guessing.

Rendered by **both** bars, because one suppresses the other:

- `CompactionStatus` (`web/src/components/Chat/CompactionStatus.tsx:43`) carries Cancel for automatic passes and other clients' passes; it suppresses its own row while *this* client's `/compact` is already drawn by the command bar (`web/src/components/Chat/CompactionStatus.tsx:22-24`) to avoid two identical spinner rows.
- `CommandActivityBar` (`web/src/components/Chat/CommandActivityBar.tsx:76-77`) carries Cancel for this client's own `/compact`, gated to compact commands only (other commands and skill loads have no cancel affordance).
- `host` is threaded from `ChatInput`'s `projectHost` into both bars (`web/src/components/Chat/ChatInput.tsx:1160-1161`) so remote (SSH/WSL) projects POST through `/api/remote/{host}`.

## 9. Tests

| Area | Suite |
|---|---|
| Cancelling an in-flight pass yields `ErrCompactionCanceled` and does not latch auto-compaction off | `internal/agent/compact_cancel_test.go:271` `TestCancelCompactionCancelsInFlightPass` |
| Cancel with no pass returns `false` | `internal/agent/compact_cancel_test.go:304` `TestCancelCompactionWithoutPassReturnsFalse` |
| `recordCompactOutcome` ignores the cancel sentinel | `internal/agent/compact_outcome_cancel_test.go:9` `TestRecordCompactOutcomeIgnoresCancel` |
| Endpoint cancels a manual pass; Stop cancels compaction; auto cancel publishes a clean terminal event; no-pass cancel is a no-op; route registered through the real mux | `internal/server/compact_cancel_test.go:94`, `:140`, `:180`, `:198`, `:217` |
| A bare provider `context.Canceled` with a live request still fails 500 | `internal/server/compact_timeout_test.go:91` `TestCompactSessionBareCancellationRemains500` |
| Button: cancels server-side, disables while in flight, ignores a second click, re-enables on error; Cancel offered only for compact commands | `web/src/components/Chat/CompactionCancelButton.test.tsx` |

## 10. Invariants to preserve

- **One cancel sentinel.** User/Stop cancellation is `ErrCompactionCanceled`; classification everywhere is `errors.Is`, never message text. A bare `context.Canceled` with a live request must remain a real failure (500).
- **Register before `OnCompactStart`.** Reordering these two reopens a window where a client's Cancel finds no pass and the compaction runs anyway.
- **Stop must not record `pendingCancel` for compaction-only work** — the next turn would be swallowed by a cancelled `turn_error`.
- **The cancel endpoint does no lifecycle bookkeeping**; the running pass's `compaction_done` is the single terminal frame, and it must be `ok:true` with an empty error on every path (manual and auto) so no client paints an error banner for a deliberate cancel.
- **The Cancel button never clears local state on success** — only `compaction_done` does.
- **`compactPassMu` never wraps the cancel calls** (it would deadlock against a pass holding `compactMu`), and it stays a *separate* mutex from `compactMu`.