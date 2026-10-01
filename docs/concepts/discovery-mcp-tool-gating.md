---
type: Concept
title: Discovery MCP Tool Gating
description: 'Why the MCP tool gate (discoveryAllows) never fails open: the names-only index vs. callable schema split, the single surviving escape, the cold-turn zero-tools contract, the failed-Select rules, the judged attach paths, turn-tail capture, and the debug lines to check.'
resource: internal/agent/discovery_glue.go
tags:
  - discovery
  - mcp
  - tools
  - gating
  - judge
  - architecture
timestamp: 2026-10-01T17:11:59Z
---
# Discovery MCP Tool Gating

## Overview

When discovery is on, `discoveryAllows` (`internal/agent/discovery_glue.go:630`) decides which MCP tool definitions reach the model. Built-ins are never gated; MCP tools are gated to the sticky attached set (`session.IsAttached("mcp:" + name)`). The filter runs in `GetToolDefinitions` (`internal/agent/agent.go:5039`), so every turn's tool list is shaped before the provider sees it.

The gate exists to **shrink the tool list, and fail-open defeats it.** One real session logged `exposing 322 tools` (`internal/agent/agent.go:5069`), 274 of them `zoho-books_*`. The judge that decides *what* attaches is documented in [Discovery TypeSafe Relevance Judge](concepts/discovery-typesafe-judge.md); this page is about the gate itself — when it holds, the one escape that survives, and the attach paths that must stay behind the judge.

## Names-only index vs. callable definitions

Every connected MCP tool exists in the prompt in one of two representations:

1. **Names-only index** — one short line per tool in the system-role block headed `Available MCP tools (names only — not all loaded)` (`discovery_glue.go:820`), introduced by the `discoveryPromptContract` (`discovery_glue.go:721`) which tells the model to call `discover_more` with a natural-language need BEFORE claiming it cannot do something. Rendered by `renderDiscoveryContext` (`discovery_glue.go:815`) as a function of the doc **set** only — never of which ids are attached — so attaching mid-session leaves the hoisted, cached system prompt byte-identical. Injection is split by volatility in `injectDiscoveryContext` (`discovery_glue.go:754`, called from `internal/agent/agent.go:1370`): index = system-role (cached), attached-skill descriptions = user-role tail (uncached).
2. **Full callable tool definitions** — complete JSON schemas, only for tools that pass the gate, assembled in `GetToolDefinitions` (`internal/agent/agent.go:5036`) and recomputed **every Step loop iteration** (`internal/agent/agent.go:1428-1430`), so a mid-turn `discover_more` attach becomes visible to the LLM on the next iteration.

**Why the split: schema cost vs. index cost.** Full MCP schemas run hundreds of tokens each (the 322-tool session above); an index line costs a few. The token-accounting helper `DiscoveryGatedTokens` (`discovery_glue.go:570`) reports `attached, total, gatedToks, indexToks`: gated tokens ≈ `len(json.Marshal(Definition()))/4` per unattached tool (`:583-585`), index tokens ≈ `len(name)+1` chars / 4 over all tools (`:573-578`, `:587`) — bytes/4 is a deliberate approximation for `/context`, not exact tokenization.

## The gating chain

```
runDiscovery → Session.Select → judge → Seed → discoveryAllows → GetToolDefinitions
```

1. **`runDiscovery(query, tail)`** (`discovery_glue.go:356`) — no-op when discovery is off, the query is empty, or a background warm is in flight.
2. **`Session.Select`** (`internal/discovery/engine.go:114`, invoked at `discovery_glue.go:408` under a 500ms budget) — ranks and returns not-yet-attached candidates **without mutating the sticky set**.
3. **Judge** (`discovery_glue.go:433-448`, call at `:437`) — `judgeDiscoveryCandidates` when TypeSafe resolves; otherwise (or on judge error) every candidate is kept (fail-open). Vetoes increment `discoveryState.judgeVetoed` (`:445`).
4. **`Session.Seed`** (`discovery_glue.go:459`; `internal/discovery/engine.go:149`) — marks survivors attached; sticky for the session.
5. **`discoveryAllows(name)`** (`discovery_glue.go:630`) — per-tool gate, consulted at render time (next section).
6. **`GetToolDefinitions`** (`internal/agent/agent.go:5036`) — filters `discoveryAllows` AND `isToolAllowed`, sorts names for a stable provider tool-cache prefix (`:5046`), emits the `TOOLS` debug line (`:5047`).

## The gate consults no warm/corpus state

`discoveryAllows` deliberately consults NO warm/corpus state. A cold embedder cache is the *normal* first turn: `runDiscovery` (`internal/agent/discovery_glue.go:356`) gives `engine.Warm` a 500ms synchronous budget (`discovery_glue.go:394`) and defers to `startBackgroundWarm` (`discovery_glue.go:476` — single-flight via the `warming atomic.Bool` field, generous `discoveryWarmTimeout`) when that budget is not enough. The old "warm failed → don't gate" escape therefore fired on exactly the turns discovery exists to shrink, exposing the entire MCP corpus right when the tool list was supposed to be smallest — the 322-tool session above.

See [Discovery Corpus Cache](concepts/discovery-corpus-cache.md) for what sits *behind* `engine.Warm` — the machine-global corpus file, its cross-instance flock, and the `ErrCorpusLocked` skip on contention. None of it feeds back into the gate: a contended or cold warm is just another warm error that defers to the background path described above, and `discoveryAllows` still consults only `disco.enabled` and the sticky set. The gate's contract is unchanged.

Failing open was never necessary: the names-only index (`discoveryPromptContract`, `discovery_glue.go:721`) still advertises every MCP tool by name even when its definition is gated.

## The only surviving fail-open

`disco == nil || !disco.enabled` — the first two lines of `discoveryAllows`. Discovery never initialized (embedder unresolved → the feature is OFF), so gating would strand every MCP tool behind a `discover_more` that cannot work; in that state `discover_more` itself answers "Discovery is not active; all tools are already available." (`discovery_glue.go:962-964`).

A nil `session` does the opposite: it gates (returns false) rather than failing open, so an invariant violation can never re-expose the corpus or panic `IsAttached`. Covered by `TestGateOnDisabledDiscoveryStillFailsOpen`.

## A cold turn legitimately starts with ZERO MCP tool definitions

This is a contract, not a bug. While the background warm is in flight — or the synchronous 500ms warm deferred — `runDiscovery` attaches nothing, the gate holds, and the model sees only built-ins plus the names-only index. The recovery path is that index plus `discover_more`, which warms with **no timeout** (`context.Background()`, `discovery_glue.go:964`) and ranks without a per-turn budget (`discovery_glue.go:972`), then seeds for the rest of the turn. Pinned by `TestGateOnColdCorpusGatesMCPTools` (cold corpus → built-ins only) and `TestGateOnColdCorpusStillExposesAttached` (the sticky set stays visible).

## Failed Session.Select: cost the turn, not the gate (newest fix)

`runDiscovery` bounds ranking with a 500ms budget (`discovery_glue.go:407-409`). On error it attaches **nothing for that turn** and returns — it must **never** set `disco.enabled = false`. Setting the gate off re-opens the whole corpus for the rest of the session: the same leak as the cold-corpus path, reachable whenever the embedder blows the 500ms rank budget. Staying active is safe because the names-only index still advertises every tool, `discover_more` retries the rank with no timeout, and the next turn ranks again.

`TestRankFailureKeepsTheGateClosed` pins both halves: after a query-embed failure, `disco.enabled` stays `true` and `GetToolDefinitions` still returns only built-ins.

## The `discover_more` recovery path

Registered by `ensureDiscovery` into `a.tools` (`discovery_glue.go:158-160`) but deliberately **not** added to `a.mcpTools`, so `discoveryAllows` always returns true for it (comment at `:154-157`). Its `Execute` (`discovery_glue.go:953`):

1. Discovery not active → "Discovery is not active; all tools are already available." (`:962`).
2. **Warms with `context.Background()` — no timeout** (`:964`) — which is what makes a cold turn self-healing: the on-demand path may take as long as the embedder needs, unlike the per-turn 500ms budgets. A warm failure surfaces as a tool error (`discover_more warm: ...`).
3. `Session.Select` on the need (`:972`); `discover_more rank: ...` on error.
4. **Judge** (`:977-989`, call at `:978`): `judgeDiscoveryCandidates(client, a.discoveryTail(), p.Need, candidates)` when TypeSafe resolves; judge errors fail open (logged `discover_more judge failed (fail-open, all attached): ...`, `:982`); real vetoes increment the **same** `discoveryState.judgeVetoed` counter the per-turn path uses (`:986`), so `/discovery status` totals both paths.
5. `Seed` the survivors (`:994`), log `discover_more("<need>") → +N tools (judge kept N/M)` (`:995`).
6. Reply (`:996-1010`): kept > 0 → `Attached: <names>. They are available on your next step.`; kept == 0 with candidates > 0 → `N tool(s) matched that need but were judged out of scope for this request. Try a different need, or continue without them.` (the model can distinguish "nothing matched" from "matched but out of scope" and retry rather than conclude the capability is missing); kept == 0 with no candidates → `No additional tools matched that need. Available tools are listed in the discovery index.`

## Every attach path is judged

- **Per-turn `runDiscovery`** runs `judgeDiscoveryCandidates` (`discovery_glue.go:437`) before `Seed` (`discovery_glue.go:459`).
- **On-demand `discover_more`** runs `judgeDiscoveryCandidates` (`discovery_glue.go:978`) before `Seed` (`discovery_glue.go:994`). The inline comment is explicit: `Discover` (plain Select+Seed) stays a deliberately no-judge wrapper, because the on-demand path is the one the model reaches for when per-turn ranking attached nothing — a convenience wrapper there would let a named need bypass the judge entirely.

Both funnel through `judgeDiscoveryCandidates` (`internal/agent/discovery_typesafe.go:77`) with the once-cached `discoveryJudgeClient` (`discovery_typesafe.go:41`). **A second, unjudged attach path is a bypass.**

Note the direction: this gate's fail-open is forbidden, while the judge's is required — a judge transport/decode failure seeds every candidate (the judge may only veto, never attach fewer than pre-judge behavior).

## Turn-tail capture

`runDiscovery` records the bounded transcript tail via `noteDiscoveryTail` (`discovery_glue.go:926`) after `ensureDiscovery` but **before its warm/rank early returns**, so the on-demand `discover_more` judge has conversation context precisely on the cold turn where nothing was attached. The snapshot is bounded (`discoveryJudgeTailN` = 6, `internal/agent/discovery_typesafe.go:28`) and copied under `tailMu` because callers reuse the message backing array while the judge reads the snapshot later. `TestDiscoverMoreJudgeSeesTurnTailOnColdTurn` asserts the judge state carries the tail.

## Debug lines readers can check

`TOOLS` (kind `TOOLS`, emitted from `GetToolDefinitions`):

- `exposing <N> tools: <sorted names>` — `internal/agent/agent.go:5069`. The definitive answer to "how many full tool definitions did this turn send"; the leak symptom was `exposing 322 tools: advisor, ...`.

`DISCOVERY` (kind `DISCOVERY`):

- `turn rank: <N> newly attached, <M> total [judge=...] (q="...")` — `discovery_glue.go:467`.
- `corpus warm: <D> docs (<K> embedded) model=<m> dim=<d>` — `internal/discovery/engine.go:65`, on a successful warm.
- `corpus warm deferred to background: <err>` — `discovery_glue.go:399`; this turn attaches nothing (gate holds).
- `background corpus warm complete (cache hot; ranking resumes next turn)` — `discovery_glue.go:489`.
- ``rank failed (attaching nothing this turn): <err>` — `discovery_glue.go:417`; gate stays closed (no `enabled = false`).
- `discover_more("<need>") → +N tools (judge kept N/M)` — `discovery_glue.go:995`.
- ``discover_more judge failed (fail-open, all attached): <err>` — `discovery_glue.go:982`.
- `disabled (fail-open): <err>` — `discovery_glue.go:137`; discovery never initialized → the ONE surviving fail-open is in effect.

## Regression tests

All in `internal/agent/discovery_glue_test.go`:

- `TestGateOnColdCorpusGatesMCPTools` (line 873) — cold corpus must not fail open to the whole MCP corpus.
- `TestGateOnColdCorpusStillExposesAttached` (line 891) — the sticky set survives a cold corpus.
- `TestGateOnDisabledDiscoveryStillFailsOpen` (line 907) — the one surviving escape.
- `TestDiscoverMoreJudgeSeesTurnTailOnColdTurn` (line 973) — tail capture on a cold turn.
- `TestRankFailureKeepsTheGateClosed` (line 1025) — a query-embed failure leaves `disco.enabled` true and the gate closed.

## See also

- [Discovery TypeSafe Relevance Judge](concepts/discovery-typesafe-judge.md) — the judge that both attach paths must clear before `Seed`.
- `internal/agent/discovery_glue.go` — the gate (`discoveryAllows`), per-turn attach (`runDiscovery`), background warm, and the `discover_more` handler.
- `internal/agent/discovery_typesafe.go` — `judgeDiscoveryCandidates` and the cached judge client.