---
type: Concept
title: "Discovery TypeSafe Relevance Judge"
description: Added turn-rank judge token to the "How to observe" section of the Discovery TypeSafe Relevance Judge concept doc.
tags: [discovery, typesafe, skills, architecture, observability]
timestamp: 2026-09-18T17:44:13Z
resource: internal/agent/discovery_glue.go
---
---
title: Discovery TypeSafe Relevance Judge
type: Concept
description: TypeSafe relevance judge that vets discovery candidates per-turn, only when connected. Shared lenient core with doc_search judge.
tags:
  - discovery
  - typesafe
  - skills
  - architecture
---

# Discovery TypeSafe Relevance Judge

## Overview

When discovery is enabled and the TypeSafe provider is connected, a per-turn judge asks Jev (the `typesafe/jev-latest` model) whether each embedder-selected candidate — a skill, project markdown doc, or MCP tool — is actually in scope for the current request. Only confirmed candidates join the sticky attached set. The judge can only ever veto; it never attaches fewer docs than the pre-judge attach-everything behavior.

The judge now shares mechanics with the [doc_search relevance judge](concepts/doc-search-relevance-judge.md) through a common core (`judgeRelevanceQuestions` in `internal/agent/relevance_typesafe.go`), including the lenient confidence floor and the "slight relevancy kept, different scope skipped" rubric.

## Activation condition

There is no separate config flag. `discoveryJudgeClient()` in `internal/agent/discovery_typesafe.go:40` returns a `*TypesafeClient` only when `newClientFn(a.config, "typesafe/jev-latest")` yields a client with a non-empty API key. A nil config, a non-TypeSafe factory result, or a keyless client all mean "no judge", and discovery behaves exactly as before (every candidate is seeded).

`discoveryJudgeClient` is the **shared connected check** for both the discovery relevance judge and the doc_search relevance judge — both use the same factory, model, and caching behaviour.

The resolution is cached once per discovery state (`discoveryState.judge`, guarded by `sync.Once`), so the factory and its "no API key ... refusing to build client" debug line run once per session rather than on every turn and every `/discovery` status read. Connecting TypeSafe mid-session takes effect after the next `ResetDiscovery` (toggle `/discovery` off and on) or a restart.

## Confidence floor

The floor is `relevanceJudgeMinConfidenceDefault` = **0.5** (`internal/agent/relevance_typesafe.go:19`), not the high-stakes permission floor (`autoJudgeMinConfidenceDefault` = 0.85). This is deliberate: a relevance veto only hides a retrieval result, it never grants a tool call. 0.5 is TypeSafe's documented "genuinely unsure / do not act" boundary, so anything Jev judges more likely relevant than not is kept.

The floor is resolved by `resolveRelevanceJudgeMinConfidence()` (`internal/agent/relevance_typesafe.go:26`), which is intentionally decoupled from `permissions.auto.min_confidence` — overloading that key would let a strict permission tuning silently suppress slightly-relevant retrieval results.

## Rubric

`discoveryJudgeInstructions` asks whether each candidate is "even slightly relevant" to the request. Answer yes when the candidate touches the same subject, component, decision, workflow, or concept as the request. Answer no only when it is of a different scope — an unrelated subject that merely happens to share a word with the request. This is the same lenient rubric used by the doc_search judge.

## State and question shape

`buildDiscoveryJudgeState` (pure, `internal/agent/discovery_typesafe.go:102`) assembles a structured map with three keys:

- `request` — the discovery query text (the user's message, optionally enriched with project-type signal).
- `transcript_tail` — the last 6 non-empty messages (`discoveryJudgeTailN`), each capped at 4000 chars (`discoveryJudgeTailCap`). Empty messages are skipped.
- `candidates` — one `{id, kind, name, summary}` per candidate. Summary is `discovery.Doc.Text` capped at 1000 chars (`discoveryJudgeSummaryCap`).

`judgeDiscoveryCandidates` (`internal/agent/discovery_typesafe.go:71`) delegates to `judgeRelevanceQuestions` (`internal/agent/relevance_typesafe.go:41`), which sends one `noul` (yes-probability) question per candidate in a single `Decide` call, keyed by doc ID. The question text is built by `discoveryJudgeInstructions` and references the candidate by its backticked state path (`candidates[i]`).

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
| Real below-threshold noul (Noul < 0.5) | Vetoes only that candidate |

The judge can only ever **veto** — a failure never attaches fewer docs than the pre-judge behavior. A vetoed doc stays unattached and is re-judged on a later turn when the query or transcript changes. The judge never changes `renderDiscoveryContext` (`internal/agent/discovery_glue.go:734`); the names-index is a function of the doc set, not of which ids are attached.

## How to observe

`/discovery` status shows `judge: typesafe/jev-latest (vetoed N this session)` only when TypeSafe is connected (`DiscoveryStatusInfo.Judge` / `JudgeVetoed` in `internal/agent/discovery_glue.go:432-451`).

**Turn-rank line (primary entry point).** Every turn that selects new candidates emits one line to the debug log (kind `DISCOVERY`):

```
turn rank: <n> newly attached, <m> total [<judgeNote>] (q="<query>")
```

The `[<judgeNote>]` token at the end is the single place to look when answering "did Jev filter this turn, or was it simply not connected?" The three variants are:

- `[judge=none (typesafe not connected)]` — TypeSafe not connected; every candidate was seeded without vetting.
- `[judge=<model> error (fail-open)]` — the `Decide` call failed (transport, decode, etc.); every candidate was seeded.
- `[judge=<model> kept N/M]` — the judge succeeded; N of M candidates survived the vet.

Example (success): `turn rank: 2 newly attached, 5 total [judge=jev-latest kept 4/5] (q="...")`

This line is always present when `Select` returns candidates (an empty candidate set produces no line at all). It is emitted to the debug log, not to stdout.

**Per-candidate and summary lines (unchanged).** The existing lines from `judgeRelevanceQuestions` in `internal/agent/relevance_typesafe.go` remain the detailed breakdown:

- `discovery_typesafe id=<id> noul=<score> verdict=keep|veto` — per-candidate verdict.
- `discovery_typesafe kept=<n> vetoed=<n> min=<threshold> model=<model>` — summary line after the judge call.

## Cost

At most one `Decide` per turn that has new candidates, with at most `SelectCap` (30) noul questions. Recorded as side usage via `RecordSideUsage`. No call at all when nothing new is selected (empty candidates from `Select`).

## See also

- [Doc Search Relevance Judge](concepts/doc-search-relevance-judge.md) — the doc_search judge that shares the same lenient core and floor.
- [Discovery Web Surfaces](concepts/discovery-web-surfaces.md) — how discovery status and live notices are surfaced in the web/desktop UI, including the runtime status endpoint and the map-race safety rule for reading agent state.