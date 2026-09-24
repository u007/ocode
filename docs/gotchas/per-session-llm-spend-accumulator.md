---
type: Gotcha
title: Per-session LLM spend accumulator
description: Per-session LLM spend and token totals — persistence keys, seeding, carry-on-replace, and the daily-total fallback
tags:
  - spend
  - tokens
  - telemetry
  - tui
  - web
  - session
timestamp: 2026-09-24T06:00:41Z
resource: web/src/components/Layout/CoworkSidebar.tsx
---
# Per-session LLM spend accumulator

**Type:** Gotcha  
**Description:** Per-session LLM spend and token totals — persistence keys, seeding, carry-on-replace, and the daily-total fallback  
**Tags:** spend, tokens, telemetry, tui, web, session  

---

# Per-session LLM spend accumulator

## Summary

The web/desktop sidebar shows per-session LLM spend via
`TUIStatus.SpendingUSD` (`json:"spending_usd,omitempty"`,
`internal/server/tui_status.go`). This is the **per-session** total, not
the daily/process-wide total. The daily total lives at
`GET /api/spending` (`internal/server/handler_tui_status.go:~152`) and is
only used as a fallback when `spending_usd` is absent from the status
snapshot. The same status payload now also carries per-session token totals
(`input_tokens`, `output_tokens`, `cached_tokens`, `total_tokens`), rendered by
the web CoworkSidebar token block, StatusBar row 2, and StatusPanel.

## Persistence

The session metadata key for spend is `"spend"` — deliberately the same key the
TUI's `sidebarTelemetry` writes via
`(*sidebarTelemetry).metadata()` and `telemetryFromSessionMetadata`
(`internal/tui/model.go:~20041/20076`). An earlier implementation used a
private `"spent_usd"` key; do not reintroduce it — it splits the total
between TUI and server.

The token totals use the TUI's own keys `input_tokens`, `output_tokens`,
`billed_tokens` (the billed total), and `cached_tokens`, so token history is
shared across surfaces in the same way.

## Server-side accumulator

Source: `internal/server/agent_session.go`.

- `agentSession.spentMicros atomic.Int64` — the live per-agent
  accumulator; plus four atomic token accumulators
  (`inTokens`/`outTokens`/`cachedTokens`/`totalTokens`).
- `addSpendUSD` / `addSpendFromMessages` — sums only a Step's *new*
  messages' `Spend`, never the whole transcript.
- `spendUSD` — reads the current total.
- `seedSpend` / `seedUsage` — raise, never lower (idempotent seeding).
- `sessionSpendFromMetadata` / `sessionUsageFromMetadata` — tolerate
  `float64`/`int`/`int64`/`float32` from persisted JSON.
- `tokenCountsFromMessage` normalizes prompt tokens (excludes cache reads) so a
  provider that folds cache reads into the prompt count is not double-counted.
- `addUsageFromMessages` / `addRawUsage` — fold a Step's tokens and side-path
  (advisor/compact) usage into the session totals; `runTurn` calls
  `addUsageFromMessages`, and `OnSideUsage` now captures tokens (it previously
  discarded the token arguments, keeping only spend).
- `buildAgentSession` seeds from persisted metadata on creation.
- `replaceAgentSession` carries the outgoing agent's live totals forward
  to the new agent instance.
- `persistSessionTelemetry` writes spend + all four token keys in ONE
  `session.UpdateMetadataForDir` call at turn end from
  `publishTurnStatusSnapshot` (it replaced `persistSessionSpend`).

The snapshot is set by `applySessionUsage`
(`internal/server/handler_session_state.go`, renamed from
`applySessionSpending`): live agent totals if available, else persisted
metadata. Spend has one extra last-resort fallback — the session's attributed
usage-ledger rows via `usage.SessionSpend`; tokens do not.

## Token totals (web/desktop mirror)

Tokens are populated for headless (non-TUI) web/desktop sessions by
`applySessionUsage`; previously only an attached TUI (RC bridge) supplied them,
so a headless chat showed no token breakdown. Web surfaces: CoworkSidebar (an
Input/Cached(+cache %)/Output block with no billed Total row — the Cached row
appends the cache hit % (`cached ÷ (input + cached)`, rounded), mirroring the
TUI sidebar's `Cache <n> (<pct>%)` line — rendered independently of the context
gauge so a restored session with no provider reading still shows its totals),
StatusBar row 2 (`in … · cache … · out …`), and StatusPanel (token rows). All
are hidden when every count is 0 — never a fabricated 0.

## TUI mirror

`buildTUIStatusSnapshot` (`internal/tui/model.go:~15636`) sets
`snap.SpendingUSD` from `m.sessionTelemetry.spend`, and
`snap.InputTokens`/`OutputTokens`/`CachedTokens`/`TotalTokens` from the same
telemetry.

## Why seeding and carry-on-replace exist

The accumulator is per-agent, so every rebuild (model switch / profile
reconcile / plugin reload / idle eviction / server restart) resets it to
zero. Without seeding, the gauge would only show the current turn's cost.
`seedSpend`/`seedUsage` on build + carry on replace is what preserves session
history.

## Caveat

A headless (non-TUI) session created *before* this change has no recorded
per-session spend or token totals anywhere — `agent.Message.Spend` AND
`agent.Message.Usage` are `json:"-"` (not persisted in transcripts) and the
usage ledger is not a token source — so pre-existing headless history cannot
be backfilled. A restored session with no token metadata therefore shows no
token breakdown (the same "n/a" the TUI shows); spend alone can still recover
from the ledger.

## Tests

- `internal/server/session_spend_test.go`
- `internal/server/session_usage_test.go` (incl. `TestRunTurnAccumulatesSessionTokens`)
- `internal/tui/model_test.go` `TestBuildTUIStatusSnapshotIncludesSessionSpend`
- `web/src/components/Layout/CoworkSidebar.tokens.test.tsx` (now also asserts the cache hit % on the Cached row and the absence of the Total row)
- `web/src/components/common/StatusBar.tokens.test.tsx`
