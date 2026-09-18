---
type: Concept
title: Discovery TypeSafe Relevance Judge
description: TypeSafe relevance judge that vets discovery candidates per-turn, only when connected
tags:
  - discovery
  - typesafe
  - skills
  - architecture
timestamp: 2026-09-18T02:32:11Z
---
## Overview

When discovery is enabled and the TypeSafe provider is connected, a per-turn judge asks Jev (the `typesafe/jev-latest` model) whether each embedder-selected candidate — a skill, project markdown doc, or MCP tool — is actually needed for the current request. Only confirmed candidates join the sticky attached set. The judge can only ever veto; it never attaches fewer docs than the pre-judge attach-everything behavior.

## Activation condition

There is no separate config flag. `discoveryJudgeClient()` in `internal/agent/discovery_typesafe.go` returns a `*TypesafeClient` only when `newClientFn(a.config, "typesafe/jev-latest")` yields a client with a non-empty API key. A nil config, a non-TypeSafe factory result, or a keyless client all mean "no judge", and discovery behaves exactly as before (every candidate is seeded).

The resolution is cached once per discovery state (`discoveryState.judge`, guarded by `sync.Once`), so the factory and its "no API key ... refusing to build client" debug line run once per session rather than on every turn and every `/discovery` status read. Connecting TypeSafe mid-session takes effect after the next `ResetDiscovery` (toggle `/discovery` off and on) or a restart.

## State and question shape

`buildDiscoveryJudgeState` (pure, `internal/agent/discovery_typesafe.go:97`) assembles a structured map with three keys:

- `request` — the discovery query text (the user's message, optionally enriched with project-type signal).
- `transcript_tail` — the last 6 non-empty messages (`discoveryJudgeTailN`), each capped at 4000 chars (`discoveryJudgeTailCap`). Empty messages are skipped.
- `candidates` — one `{id, kind, name, summary}` per candidate. Summary is `discovery.Doc.Text` capped at 1000 chars (`discoveryJudgeSummaryCap`).

`judgeDiscoveryCandidates` (`internal/agent/discovery_typesafe.go:54`) asks one `noul` (yes-probability) question per candidate in a single `Decide` call, keyed by doc ID. The question text is built by `discoveryJudgeInstructions` and references the candidate by its backticked state path (`candidates[i]`).

A candidate is kept when `answer.Noul >= permissions.auto.min_confidence` (default 0.85, `autoJudgeMinConfidenceDefault` in `internal/agent/permissions.go:5594`).

## Split Select/Seed

`Session.Select` (`internal/discovery/engine.go:114`) ranks the query against the corpus via `Engine.Rank`, applies `SelectRankRelative` (which caps at `SelectCap = 30`), and returns the not-yet-attached candidates **without mutating the sticky set**. This lets the caller inspect or veto candidates before they join.

`Seed` (`internal/discovery/engine.go:149`) marks the survivors as attached.

`Discover` (`internal/discovery/engine.go:134`) is exactly `Select` + `Seed` — the plain attach-everything wrapper used when no judge is active.

The judge path in `runDiscovery` (`internal/agent/discovery_glue.go:322`) calls `Select`, feeds the candidates to `judgeDiscoveryCandidates`, and only calls `Seed` with the kept IDs.

## Fail-open matrix

| Condition | Behaviour |
|---|---|
| TypeSafe not connected (`discoveryJudgeClient()` returns nil) | Seed all candidates — attach-everything |
| Transport or decode error from `Decide` | Seed all candidates (logged via `emitDebug("DISCOVERY", ...)`) |
| Candidate answer missing or not `type:"noul"` | That candidate is kept (fail-open per candidate) |
| Real below-threshold noul | Vetoes only that candidate |

The judge can only ever **veto** — a failure never attaches fewer docs than the pre-judge behavior. A vetoed doc stays unattached and is re-judged on a later turn when the query or transcript changes. The judge never changes `renderDiscoveryContext` (`internal/agent/discovery_glue.go:734`); the names-index is a function of the doc set, not of which ids are attached.

## How to observe

`/discovery` status shows `judge: typesafe/jev-latest (vetoed N this session)` only when TypeSafe is connected (`DiscoveryStatusInfo.Judge` / `JudgeVetoed` in `internal/agent/discovery_glue.go:432-451`).

Debug log lines (via `emitDebug("DISCOVERY", ...)`):

- `discovery_typesafe kept=<n> vetoed=<n> min=<threshold> model=<model>` — summary line after the judge call.
- `discovery_typesafe id=<id> noul=<score> verdict=keep|veto` — per-candidate verdict.

## Cost

At most one `Decide` per turn that has new candidates, with at most `SelectCap` (30) noul questions. Recorded as side usage via `a.RecordSideUsage(resp.Usage.InputTokens, resp.Usage.OutputTokens, 0, 0, "typesafe/"+client.Model)`. No call at all when nothing new is selected (empty candidates from `Select`).
