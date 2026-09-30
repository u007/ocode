---
type: Concept
title: Web/Desktop Context Gauge Resolution
description: Provider-reported context occupancy first, transcript estimate only as fallback; the shared resolution chain for status snapshots and the /context endpoint.
resource: CLAUDE.md
tags:
  - context
  - gauge
  - web
  - tokens
timestamp: 2026-09-30T07:37:09Z
---
# Web/Desktop Context Gauge Resolution

The web/desktop Context gauge (`TUIStatus.context_current_tokens`) and the
`/context` summary (`current_tokens`) carry the backend's provider-reported
context occupancy whenever one exists. A character-count estimate over the
session transcript is used ONLY as a last-resort fallback, never in preference
to a provider reading.

- **Source of truth:** `Agent.LastInputTokens()`, an atomic set from
  `resp.Usage` inside `Step` (so it covers every provider, not just the
  streaming-usage ones). The bridged TUI session keeps using its own live
  `ContextCurrentTokens`.
- **Resolution chain (identical in both entry points):** live TUI value →
  `Agent.LastInputTokens()` → `Agent.CompactedContextTokens()` (a `/compact`
  clears `LastInputTokens`, so the agent records a post-splice estimate) →
  `estimateContextFromTranscript` / `estimateContextFromMessages` (chars/4 over
  the persisted transcript, for a restored / idle-evicted session with no live
  agent in this process).
- **One resolution, two entry points:** `Handler.applySessionContext` (status
  snapshots) and `Handler.HandleSessionContext` (the `/context` endpoint) MUST
  walk the SAME chain, and the report's `contextbudget.Input.ContextTokens`
  override is fed the same value so the report's Context row and the summary
  cannot disagree. Label the override's `ContextSource` by origin: a provider
  reading (or its post-compaction tail estimate) is `"actual"`; the chars/4
  transcript fallback is `"estimated"` — never present an estimate as "actual".
- **Why the fallback exists:** the earlier rule forbade any estimate, so a
  session whose agent had been idle-evicted or restored-after-restart rendered
  the gauge as "unknown" until its next turn — a confusing regression on every
  tab switch. Provider numbers still always win, and the fallback is the same
  `CurrentContextEstimate` heuristic the TUI already uses for compaction, so the
  web and TUI agree rather than diverging.
- **API shape:** `GET /api/sessions/:id/context` returns `current_tokens`
  (renamed from `estimated_tokens`), alongside `max_tokens` / `model` / the
  optional `report`.
