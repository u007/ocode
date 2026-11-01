---
type: Concept
title: Discovery MCP Tool Gating
description: 'Why the MCP tool gate (discoveryAllows) never fails open: the names-only index vs. callable schema split, the single surviving escape, the cold-turn zero-tools contract, the failed-Select rules, the judged attach paths, server-group sibling attach, per-session pinned servers, turn-tail capture, and the debug lines to check.'
resource: internal/agent/discovery_glue.go
tags:
  - discovery
  - mcp
  - tools
  - gating
  - judge
  - architecture
timestamp: 2026-11-01T05:24:30Z
---
# Discovery MCP Tool Gating

## Overview

When discovery is on, `discoveryAllows` (`internal/agent/discovery_glue.go`) decides which MCP tool definitions reach the model. Built-ins are never gated; MCP tools are gated to the sticky attached set (`session.IsAttached("mcp:" + name)`). The filter runs in `GetToolDefinitions` (`internal/agent/agent.go`), so every turn's tool list is shaped before the provider sees it.

The gate exists to **shrink the tool list, and fail-open defeats it.** One real session logged `exposing 322 tools`, 274 of them `zoho-books_*`. The judge that decides *what* attaches is documented in [Discovery TypeSafe Relevance Judge](concepts/discovery-typesafe-judge.md); this page is about the gate itself: when it holds, the one escape that survives, the attach paths that must stay behind the judge, and the two ways a server's tools can attach as a group.

## Names-only index vs. callable definitions

Every connected MCP tool exists in the prompt in one of two representations:

1. **Names-only index**: one short line per tool in the system-role block headed `Available MCP tools (names only — not all loaded)`, rendered by `renderDiscoveryContext`. It is introduced by `discoveryPromptContract`, which tells the model to call `discover_more` with a natural-language need BEFORE claiming it cannot do something. The render is a function of the doc **set** only, never of which ids are attached, so attaching mid-session leaves the hoisted, cached system prompt byte-identical. Injection is split by volatility in `injectDiscoveryContext`, called from `Agent.Step`: index = system-role (cached), attached-skill descriptions = user-role tail (uncached).
2. **Full callable tool definitions**: complete JSON schemas, only for tools that pass the gate. They are assembled in `GetToolDefinitions` and recomputed **every Step loop iteration**, so a mid-turn `discover_more` attach becomes visible to the LLM on the next iteration.

**Why the split: schema cost vs. index cost.** Full MCP schemas run hundreds of tokens each (the 322-tool session above); an index line costs a few. The token-accounting helper `DiscoveryGatedTokens` reports `attached, total, gatedToks, indexToks`. Gated tokens are approximately `len(json.Marshal(Definition()))/4` per unattached tool, and index tokens approximately `len(name)+1` chars / 4 over all tools. Bytes/4 is a deliberate approximation for `/context`, not exact tokenization.

## The gating chain

```
SyncPinnedSkills (pinned servers, every turn)
runDiscovery → Session.Select → judge → expandMCPServerGroups → Seed → discoveryAllows → GetToolDefinitions
```

1. **`runDiscovery(query, tail)`**: no-op when discovery is off, the query is empty, or a background warm is in flight. `SyncPinnedSkills` runs just before its early returns (see [Pinned MCP servers](#pinned-mcp-servers-per-session)).
2. **`Session.Select`** (`internal/discovery/engine.go`, invoked inside `runDiscovery` under a 500ms budget): ranks and returns not-yet-attached candidates **without mutating the sticky set**.
3. **Judge** (`judgeDiscoveryCandidates`, called inside `runDiscovery`): when TypeSafe resolves. Otherwise, or on judge error, every candidate is kept (fail-open). Vetoes increment `discoveryState.judgeVetoed`.
4. **`expandMCPServerGroups`**: widens the kept set to whole MCP servers, excluding siblings the judge vetoed in the same pass (see [Server groups](#server-groups-siblings-attach-with-the-approved-tool)).
5. **`Session.Seed`** (`internal/discovery/engine.go`): marks survivors attached; sticky for the session.
6. **`discoveryAllows(name)`**: per-tool gate, consulted at render time (next section).
7. **`GetToolDefinitions`** (`internal/agent/agent.go`): filters `discoveryAllows` AND `isToolAllowed`, sorts names for a stable provider tool-cache prefix, and emits the `TOOLS` debug line.

## The gate consults no warm/corpus state

`discoveryAllows` deliberately consults NO warm/corpus state. A cold embedder cache is the *normal* first turn: `runDiscovery` gives `engine.Warm` a 500ms synchronous budget and defers to `startBackgroundWarm` (single-flight via the `warming` flag, generous `discoveryWarmTimeout`) when that budget is not enough. The old "warm failed → don't gate" escape fired on exactly the turns discovery exists to shrink, exposing the entire MCP corpus right when the tool list was supposed to be smallest.

See [Discovery Corpus Cache](concepts/discovery-corpus-cache.md) for what sits *behind* `engine.Warm`: the machine-global corpus file, its cross-instance flock, and the `ErrCorpusLocked` skip on contention. None of it feeds back into the gate. A contended or cold warm is just another warm error that defers to the background path, and `discoveryAllows` still consults only `disco.enabled` and the sticky set.

Failing open was never necessary: the names-only index (`discoveryPromptContract`) still advertises every MCP tool by name even when its definition is gated.

## The only surviving fail-open

`disco == nil || !disco.enabled`, the first two lines of `discoveryAllows`. Discovery never initialized (embedder unresolved, so the feature is OFF) would strand every MCP tool behind a `discover_more` that cannot work. In that state `discover_more` itself answers "Discovery is not active; all tools are already available."

A nil `session` does the opposite: it gates (returns false) rather than failing open, so an invariant violation can never re-expose the corpus or panic `IsAttached`. Covered by `TestGateOnDisabledDiscoveryStillFailsOpen`.

## A cold turn legitimately starts with ZERO ranked MCP tool definitions

This is a contract, not a bug. While the background warm is in flight, or the synchronous 500ms warm is deferred, `runDiscovery` attaches nothing **by ranking**, the gate holds, and the model sees only built-ins plus the names-only index. The exception is pinned servers, which `SyncPinnedSkills` attaches before those early returns and which therefore do appear on a cold turn. The recovery path for ranked tools is that index plus `discover_more`, which warms with **no timeout** (`context.Background()`) and ranks without a per-turn budget, then seeds for the rest of the turn. Pinned by `TestGateOnColdCorpusGatesMCPTools` (cold corpus → built-ins only) and `TestGateOnColdCorpusStillExposesAttached` (the sticky set stays visible).

## Failed Session.Select: cost the turn, not the gate

`runDiscovery` bounds ranking with a 500ms budget. On error it attaches **nothing ranked for that turn** and returns. It must **never** set `disco.enabled = false`. Setting the gate off re-opens the whole corpus for the rest of the session: the same leak as the cold-corpus path, reachable whenever the embedder blows the 500ms rank budget. Staying active is safe because the names-only index still advertises every tool, `discover_more` retries the rank with no timeout, and the next turn ranks again.

`TestRankFailureKeepsTheGateClosed` pins both halves: after a query-embed failure, `disco.enabled` stays `true` and `GetToolDefinitions` still returns only built-ins.

## The `discover_more` recovery path

Registered by `ensureDiscovery` into `a.tools` but deliberately **not** added to `a.mcpTools`, so `discoveryAllows` always returns true for it. Its `Execute` method (`discoverMoreTool.Execute`):

1. Discovery not active → "Discovery is not active; all tools are already available."
2. **Warms with `context.Background()`, no timeout**, which is what makes a cold turn self-healing: the on-demand path may take as long as the embedder needs, unlike the per-turn 500ms budgets. A warm failure surfaces as a tool error (`discover_more warm: ...`).
3. `Session.Select` on the need; `discover_more rank: ...` on error.
4. **Judge** (`judgeDiscoveryCandidates`): when TypeSafe resolves. Judge errors fail open (logged `discover_more judge failed (fail-open, all attached): ...`). Real vetoes increment the **same** `discoveryState.judgeVetoed` counter the per-turn path uses, so `/discovery status` totals both paths.
5. `expandMCPServerGroups` on the survivors, then `Seed`, logging `discover_more("<need>") → +N tools (judge kept N/M)`.
6. Reply: kept > 0 → `Attached: <names>. They are available on your next step.`, where the names include server-group siblings. Kept == 0 with candidates > 0 → `N tool(s) matched that need but were judged out of scope for this request. Try a different need, or continue without them.` Kept == 0 with no candidates → `No additional tools matched that need. Available tools are listed in the discovery index.`

## Every ranked attach path is judged

- **Per-turn `runDiscovery`** runs `judgeDiscoveryCandidates` before `Seed`.
- **On-demand `discover_more`** runs `judgeDiscoveryCandidates` before `Seed`. The inline comment is explicit: `Discover` (plain Select+Seed) stays a deliberately no-judge wrapper, because the on-demand path is the one the model reaches for when per-turn ranking attached nothing. A convenience wrapper there would let a named need bypass the judge entirely.

Both funnel through `judgeDiscoveryCandidates` (`internal/agent/discovery_typesafe.go`) with the once-cached `discoveryJudgeClient`. **A second, unjudged attach path driven by the model's own request is a bypass.**

Two attach paths are deliberately NOT judged, because the candidate is not named by the model:

- **Server-group siblings** ride on an approved tool. They are not candidates, so the judge never sees them. See the next section for the consequence.
- **Pinned MCP servers** are configured by whoever creates the session. See the section after that one.

Note the direction: this gate's fail-open is forbidden, while the judge's is required. A judge transport/decode failure seeds every candidate, because the judge may only veto, never attach fewer than pre-judge behavior.

## Server groups: siblings attach with the approved tool

MCP tools from one server are used together. The Excel bridge's 13 `excel_*` tools are the example. Ranking each tool alone stranded the siblings behind another `discover_more` round trip. So `expandMCPServerGroups` widens the approved set.

- **Mechanism.** The known server of each tool comes from `a.mcpServers`, filled in `addMCPTools` from the optional `tool.MCPServerTool` interface, which `mcp.MCPTool` implements. For every kept `mcp:<tool>` ID, every sibling tool of the same server is also seeded.
- **Call sites.** Applied in both `runDiscovery` and `discoverMoreTool.Execute`, immediately before `Seed`.
- **Order.** The input IDs come first in their original order. Siblings are appended, sorted.
- **Unknown server.** A name with no known server expands to itself. The TUI `RestoreMCPToolNames` path carries names only, so this is expected, not a fallback.
- **Veto wins.** Siblings the judge vetoed in that same pass stay out (`judgeVetoedIDs`). They are re-judged on a later turn like any veto.
- **Trade-off.** A sibling the embedder did not select is never shown to the judge. It attaches on the strength of the approved tool. That is the price of keeping one server's tools callable together, and it is the one place an MCP tool attaches without its own judge call.
- **Cache.** Still grow-only, so the wider attach re-primes the prompt cache once.
- **Visibility.** The `turn rank` debug line gains ` (+N via server group)`, and the `discover_more` reply lists siblings in `Attached:`.

## Pinned MCP servers (per session)

A session can keep a server's tools attached for its whole life, without ranking.

- **Storage.** `Agent.SetPinnedMCPServers` stores the names, sorted and deduplicated, in `pinnedMCPServers`. `PinnedMCPServers` returns a copy.
- **Expansion.** `pinnedIDs` returns the pinned skill IDs plus `mcp:<tool>` for every tool of a pinned server, looked up through `a.mcpServers`.
- **Seeding.** `SyncPinnedSkills` seeds `pinnedIDs()`. It runs on every `runDiscovery`, right after `ensureDiscovery` and before the warm early returns. That placement is why pinned tools attach even on a cold corpus or an empty query. The check is a cheap set compare, and it picks up MCP tools that arrive after session creation.
- **Not ranked, not judged.** Pinned tools are seeded directly, never through `Session.Select` or the judge. Pinned skills already worked this way before MCP servers were added. The pin is the session creator's configuration, not a need the model named.
- **Server side.** `ChatRequest.PinnedMCPServers` (`pinned_mcp_servers`) is honoured only on the session-creating branch of `HandleChat`, and persisted in session metadata under `pinned_mcp_servers`. `validatePinnedMCPServers` runs in that branch before any session is registered. An empty name, a duplicate, or a name not under the `mcp` config map is rejected with HTTP 400, and a missing config with HTTP 500. `buildAgentSession` restores the stored value through `pinnedMCPServersFromMetadata`. A malformed stored value fails the build instead: the agent is shut down and the session does not come back unpinned (stage `pinned-mcp`).
- **Why not TUI-owned.** The key is deliberately absent from the TUI's `sessionOwnedMetadataKeys`. The TUI's base metadata map drops owned keys and session save overwrites metadata wholesale, so owning the key would erase the pins on the first TUI save.
- **Discovery off.** A no-op. `SyncPinnedSkills` returns early, and all tools are sent anyway.
- **Consumer.** The Excel add-in task pane sends `["excel"]`.

Covered by `TestSyncPinnedMCPServersSeedsAndUnpins`, `TestRunDiscoveryAttachesPinnedServerAddedAfterSessionCreated`, and `TestPinnedMCPServersFromMetadata`.

## Turn-tail capture

`runDiscovery` records the bounded transcript tail via `noteDiscoveryTail` after `ensureDiscovery` but **before its warm/rank early returns**, so the on-demand `discover_more` judge has conversation context precisely on the cold turn where nothing was attached. The snapshot is bounded (`discoveryJudgeTailN` = 6) and copied under `tailMu` because callers reuse the message backing array while the judge reads the snapshot later. `TestDiscoverMoreJudgeSeesTurnTailOnColdTurn` asserts the judge state carries the tail.

## Debug lines readers can check

`TOOLS` (kind `TOOLS`, emitted from `GetToolDefinitions`):

- `exposing <N> tools: <sorted names>`. The definitive answer to "how many full tool definitions did this turn send". The leak symptom was `exposing 322 tools: advisor, ...`.

`DISCOVERY` (kind `DISCOVERY`):

- `turn rank: <N> newly attached, <M> total [judge=...] (q="...")`, with ` (+N via server group)` appended when siblings were added.
- `corpus warm: <D> docs (<K> embedded) model=<m> dim=<d>`, emitted by the discovery engine's `Warm` on a successful warm.
- `corpus warm deferred to background: <err>`. This turn attaches nothing by ranking, and the gate holds.
- `background corpus warm complete (cache hot; ranking resumes next turn)`.
- `rank failed (attaching nothing this turn): <err>`. The gate stays closed (no `enabled = false`).
- `discover_more("<need>") → +N tools (judge kept N/M)`.
- `discover_more judge failed (fail-open, all attached): <err>`.
- `disabled (fail-open): <err>`. Discovery never initialized, so the ONE surviving fail-open is in effect.

## Regression tests

In `internal/agent/discovery_glue_test.go`:

- `TestGateOnColdCorpusGatesMCPTools`: a cold corpus must not fail open to the whole MCP corpus.
- `TestGateOnColdCorpusStillExposesAttached`: the sticky set survives a cold corpus.
- `TestGateOnDisabledDiscoveryStillFailsOpen`: the one surviving escape.
- `TestDiscoverMoreJudgeSeesTurnTailOnColdTurn`: tail capture on a cold turn.
- `TestRankFailureKeepsTheGateClosed`: a query-embed failure leaves `disco.enabled` true and the gate closed.
- `TestExpandMCPServerGroupsAddsSiblingsOfSameServer` and `TestExpandMCPServerGroupsUnknownServerExpandsToItself`: the expansion rules.
- `TestRunDiscoveryAttachesServerSiblingsOfRankedTool` and `TestDiscoverMoreListsServerSiblings`: siblings attach on both paths.
- `TestRunDiscoveryVetoedSiblingNotAttachedByServerGroup` and `TestDiscoverMoreVetoedSiblingNotAttached`: veto wins over the sibling expansion.
- `TestSetPinnedMCPServersSortsAndDedupes`, `TestSyncPinnedMCPServersSeedsAndUnpins`, and `TestRunDiscoveryAttachesPinnedServerAddedAfterSessionCreated`: pinned servers.

In `internal/server/pinned_mcp_servers_test.go`: `TestPinnedMCPServersFromMetadata`, `TestHandleChatPersistsPinnedMCPServersAtCreation`, `TestHandleChatRejectsInvalidPinnedMCPServers`, `TestBuildAgentSessionRestoresPinnedMCPServers`, and `TestBuildAgentSessionRejectsMalformedPinnedMCPServers`.

## See also

- [Discovery TypeSafe Relevance Judge](concepts/discovery-typesafe-judge.md): the judge that both attach paths must clear before `Seed`, and the rule that a veto beats a sibling expansion.
- `internal/agent/discovery_glue.go`: the gate (`discoveryAllows`), per-turn attach (`runDiscovery`), server-group expansion (`expandMCPServerGroups`), pinned servers (`SyncPinnedSkills`), background warm (`startBackgroundWarm`), and the `discover_more` handler.
- `internal/agent/discovery_typesafe.go`: `judgeDiscoveryCandidates` and the cached judge client.
