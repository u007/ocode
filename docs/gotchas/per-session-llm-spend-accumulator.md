---
type: Gotcha
title: Per-session LLM spend accumulator
description: Per-session LLM spend accumulator — persistence key, seeding, carry-on-replace, and the daily-total fallback
tags:
  - spend
  - telemetry
  - tui
  - web
  - session
timestamp: 2026-09-19T13:00:24Z
---
# Per-session LLM spend accumulator

## Summary

The web/desktop sidebar shows per-session LLM spend via
`TUIStatus.SpendingUSD` (`json:"spending_usd,omitempty"`,
`internal/server/tui_status.go`). This is the **per-session** total, not
the daily/process-wide total. The daily total lives at
`GET /api/spending` (`internal/server/handler_tui_status.go:~152`) and is
only used as a fallback when `spending_usd` is absent from the status
snapshot.

## Persistence

The session metadata key is `"spend"` — deliberately the same key the
TUI's `sidebarTelemetry` writes via
`(*sidebarTelemetry).metadata()` and `telemetryFromSessionMetadata`
(`internal/tui/model.go:~20041/20076`). An earlier implementation used a
private `"spent_usd"` key; do not reintroduce it — it splits the total
between TUI and server.

## Server-side accumulator

Source: `internal/server/agent_session.go`.

- `agentSession.spentMicros atomic.Int64` — the live per-agent
  accumulator.
- `addSpendUSD` / `addSpendFromMessages` — sums only a Step's *new*
  messages' `Spend`, never the whole transcript.
- `spendUSD` — reads the current total.
- `seedSpend` — raises, never lowers (idempotent seeding).
- `sessionSpendFromMetadata` — tolerates `float64`/`int`/`int64`/`float32`
  from persisted JSON.
- `buildAgentSession` seeds from persisted metadata on creation.
- `replaceAgentSession` carries the outgoing agent's live total forward
  to the new agent instance.
- `persistSessionSpend` writes metadata at turn end from
  `publishTurnStatusSnapshot`.

The snapshot is set by `applySessionSpending`
(`internal/server/handler_session_state.go`): live agent total if
available, else persisted metadata.

## TUI mirror

`buildTUIStatusSnapshot` (`internal/tui/model.go:~15636`) sets
`snap.SpendingUSD` from `m.sessionTelemetry.spend`.

## Why seeding and carry-on-replace exist

The accumulator is per-agent, so every rebuild (model switch / profile
reconcile / plugin reload / idle eviction / server restart) resets it to
zero. Without seeding, the gauge would only show the current turn's cost.
`seedSpend` on build + carry on replace is what preserves session history.

## Caveat

A headless (non-TUI) session created *before* this change has no recorded
per-session spend anywhere — `agent.Message.Spend` is `json:"-"` (not
persisted in transcripts) and `usage.Record` has no session id — so
pre-existing headless history cannot be backfilled.

## Tests

- `internal/server/session_spend_test.go`
- `internal/tui/model_test.go` `TestBuildTUIStatusSnapshotIncludesSessionSpend`
