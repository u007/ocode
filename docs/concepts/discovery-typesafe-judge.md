---
type: Concept
title: "Discovery TypeSafe Relevance Judge"
description: Amended to record discover_more as a second judged attach path (identical fail-open matrix, shared veto counter, turn-tail snapshot, veto-all reply) and to separate the corpus-warm gate from the judge's fail-open.
tags: [discovery, typesafe, skills, architecture, observability]
timestamp: 2026-09-26T05:31:14Z
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

The judge path in `runDiscovery` (`internal/agent/discovery_glue.go:337`) calls `Select`, feeds the candidates to `judgeDiscoveryCandidates`, and only calls `Seed` with the kept IDs.

## Fail-open matrix

| Condition | Behaviour |
|---|---|
| TypeSafe not connected (`discoveryJudgeClient()` returns nil) | Seed all candidates — attach-everything |
| Transport or decode error from `Decide` | Seed all candidates (logged via `emitDebug("DISCOVERY", ...)`) |
| Candidate answer missing or not `type:"noul"` | That candidate is kept (fail-open per candidate) |
| Real below-threshold noul (Noul < 0.5) | Vetoes only that candidate |

The judge can only ever **veto** — a failure never attaches fewer docs than the pre-judge behavior. A vetoed doc stays unattached and is re-judged on a later turn when the query or transcript changes. The judge never changes `renderDiscoveryContext` (`internal/agent/discovery_glue.go:782`); the names-index is a function of the doc set, not of which ids are attached.

## Amendment (2026-09-26): `discover_more` is a second judged attach path — and the warm gate is NOT the judge

The sections above describe the judge as a per-turn `runDiscovery` mechanism. That was accurate when written; the on-demand attach path has since been brought under the same gate. (Line references in this amendment are current as of 2026-09-26: `runDiscovery` now lives at `internal/agent/discovery_glue.go:337`, `renderDiscoveryContext` at `:782`, `DiscoveryStatusInfo.Judge`/`JudgeVetoed` at `:485-503`.)

**Every retrieval attach path is now judged.** `discover_more` (`discoverMoreTool.Execute`, `internal/agent/discovery_glue.go:920`) used to call `Session.Discover` (Select+Seed, no judge), so a model that named a need bypassed Jev entirely — the exact guard-bypass class where a fallback path skips the gate the primary path runs. It now calls `Session.Select` (`:939`), then `judgeDiscoveryCandidates` when `discoveryJudgeClient()` resolves (`:944`), then `Seed`s only the survivors (`:961`).

**Its fail-open matrix is identical to the table above.** Same four rows: TypeSafe not connected → seed all candidates; `Decide` transport/decode failure → seed all (logged as `discover_more judge failed (fail-open, all attached): ...`, `:949`); missing or non-`noul` answer → keep that candidate; below-threshold noul → veto that candidate. Real vetoes increment the **same** `discoveryState.judgeVetoed` counter the per-turn path uses (`:953` vs. `:424`), so `/discovery status`'s `vetoed N` totals both paths. The path's own debug line is `discover_more("<need>") → +N tools (judge kept N/M)` (`:962`).

**The on-demand judge sees the conversation too.** `noteDiscoveryTail` / `discoveryTail` (`:893` / `:911`) record a bounded copy of the turn's last `discoveryJudgeTailN` (6) messages on `discoveryState.tail`, guarded by `tailMu`. `runDiscovery` records it right after `ensureDiscovery()` and **before** its early returns (`:345-350`) — the cold-cache deferral is precisely the turn where nothing is attached and the model must fall back to `discover_more`.

**The reply distinguishes veto-all from no-match.** When candidates matched but the judge vetoed all of them, `discover_more` now returns `N tool(s) matched that need but were judged out of scope for this request. Try a different need, or continue without them.` instead of the old `No additional tools matched that need.` — so the model can distinguish "nothing matched" from "matched but out of scope" and retry rather than conclude the capability is missing.

**The corpus-warm gate is a SEPARATE mechanism — do not conflate it with the judge's fail-open.** The fail-open matrix above governs *which retrieval candidates* get seeded; the warm gate governs *whether any MCP tool schemas are callable at all*:

| Mechanism | Location | Failure it handles | Effect |
|---|---|---|---|
| Judge fail-open (matrix above) | `runDiscovery` `:414-428`, `discover_more` `:944-955` | TypeSafe transport/decode failure, or TypeSafe not connected | **Attaches everything the embedder proposed** — the judge only ever filters retrieval results; it never gates the tool list |
| Corpus-warm / rank gate (`discoveryAllows`) | `internal/agent/discovery_glue.go:604-615` | Cold embedder cache or failed rank — deliberately **no fail-open** for these (the only surviving fail-open is discovery-off) | **Attaches nothing** — hides unattached MCP tool *schemas* from the model's callable tool list; the names-only index still advertises every name |

A warm failure and a judge failure therefore have **opposite** effects on attach volume: warm-fail → zero MCP tool definitions this turn (recover via `discover_more`, which warms with no timeout); judge-fail → every candidate attached. The warm gate's history and rules are documented in [Discovery MCP Tool Gating](concepts/discovery-mcp-tool-gating.md).

## How to observe

`/discovery` status shows `judge: typesafe/jev-latest (vetoed N this session)` only when TypeSafe is connected (`DiscoveryStatusInfo.Judge` / `JudgeVetoed` in `internal/agent/discovery_glue.go:485-503`).

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
- [Discovery MCP Tool Gating](concepts/discovery-mcp-tool-gating.md) — the names-only index vs. callable definitions split, the strict no-warm-fail-open gate, and the `discover_more` recovery path this amendment's judge path serves.
