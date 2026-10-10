---
type: Concept
title: "Discovery TypeSafe Relevance Judge"
description: TypeSafe relevance judge that vets discovery candidates per-turn (fail-open attach, shared with doc_search) plus the fail-closed auto-injection of the top-scoring skill body with its own 0.8 floor; score plumbing, selection, dedupe, bounding, user-role injection.
tags: [discovery, typesafe, skills, architecture, observability, auto-inject]
timestamp: 2026-10-04T12:31:48Z
resource: internal/agent/discovery_glue.go
---
# Discovery TypeSafe Relevance Judge

## Overview

When discovery is enabled and a decision backend is connected, a per-turn judge asks whether each embedder-selected candidate — a skill, project markdown doc, or MCP tool — is actually in scope for the current request. Only confirmed candidates join the sticky attached set. The judge can only ever veto; it never attaches fewer docs than the pre-judge attach-everything behavior.

The judge shares mechanics with the [doc_search relevance judge](concepts/doc-search-relevance-judge.md) through a common core (`judgeRelevanceQuestions` in `internal/agent/relevance_typesafe.go`), including the lenient confidence floor and the "slight relevancy kept, different scope skipped" rubric.

Since 2026-10-01 the judge's raw per-candidate scores also drive a second, **fail-closed** consumer: auto-injection of the single top-scoring skill's full body into the prompt (see the 2026-10-01 amendment below). That changes nothing about the attach contract above — attach stays veto-only and fail-open.

## Activation condition

There is no separate config flag. `discoveryJudgeClient()` in `internal/agent/discovery_typesafe.go:35` returns a `Decider` only when `resolveDecider(slotDiscovery)` yields a client with a non-empty API key. A nil config, a non-decision factory result, or a keyless client all mean "no judge", and discovery behaves exactly as before (every candidate is seeded).

`discoveryJudgeClient` is the **connected check** for the discovery relevance judge. It resolves through `resolveDecider(slotDiscovery)` (`internal/agent/decider.go:112`), which reads the `discovery.judge_model` config key (default `typesafe/jev-latest`). The result is cached per discovery state (`discoveryState.judge`, guarded by `judgeMu`), keyed on the slot's model id. A nil client is deliberately NOT cached, so connecting a provider mid-session takes effect on the next judge call without requiring a `/discovery toggle` or restart.

## Confidence floor

The floor is `relevanceJudgeMinConfidenceDefault` = **0.5** (`internal/agent/relevance_typesafe.go:20`), not the high-stakes permission floor (`autoJudgeMinConfidenceDefault` = 0.85). This is deliberate: a relevance veto only hides a retrieval result, it never grants a tool call. 0.5 is TypeSafe's documented "genuinely unsure / do not act" boundary, so anything Jev judges more likely relevant than not is kept.

The floor is resolved by `resolveRelevanceJudgeMinConfidence()` (`internal/agent/relevance_typesafe.go:27`), which is intentionally decoupled from `permissions.auto.min_confidence` — overloading that key would let a strict permission tuning silently suppress slightly-relevant retrieval results.

There are now **three** floors in play, each scaled to the cost of a wrong decision (the risk-scaling rule every judge here follows):

| Decision | Floor | Where | Cost of a wrong pass |
|---|---|---|---|
| Relevance vetoes — "should this NAME/result be shown?" (discovery attach, doc_search, tool/code-search) | **0.5** | `relevanceJudgeMinConfidenceDefault` (`internal/agent/relevance_typesafe.go:20`) | a slightly-relevant retrieval result hidden |
| Discovery auto-inject — "inline this skill's full BODY?" | **0.8** | `discoveryAutoInjectFloor` (`internal/agent/discovery_autoinject.go:57`), hardcoded, no config key | up to 8KB of wrong instructions in every request |
| Auto-permission / interpreter — "allow this tool call?" | **0.85** | `autoJudgeMinConfidenceDefault` via `permissions.auto.min_confidence` | a destructive tool call allowed |

Why the auto-inject floor is neither of the other two is argued in the 2026-10-01 amendment; the short version is that inlining a body is a much bigger commitment than printing a name, and coupling a prompt-spend decision to a permission tuning key would let a stricter permission setting silently disable injection.

## Rubric

`discoveryJudgeInstructions` asks whether each candidate is "even slightly relevant" to the request. Answer yes when the candidate touches the same subject, component, decision, workflow, or concept as the request. Answer no only when it is of a different scope — an unrelated subject that merely happens to share a word with the request. This is the same lenient rubric used by the doc_search judge.

## State and question shape

`buildDiscoveryJudgeState` (pure, `internal/agent/discovery_typesafe.go:112`) assembles a structured map with three keys:

- `request` — the discovery query text (the user's message, optionally enriched with project-type signal).
- `transcript_tail` — the last 6 non-empty messages (`discoveryJudgeTailN`), each capped at 4000 chars (`discoveryJudgeTailCap`). Empty messages are skipped.
- `candidates` — one `{id, kind, name, summary}` per candidate. Summary is `discovery.Doc.Text` capped at 1000 chars (`discoveryJudgeSummaryCap`).

`judgeDiscoveryCandidates` (`internal/agent/discovery_typesafe.go:81`) delegates to `judgeRelevanceQuestions` (`internal/agent/relevance_typesafe.go:57`), which sends one `noul` (yes-probability) question per candidate in a single `DecideCtx` call, keyed by doc ID. The question text is built by `discoveryJudgeInstructions` and references the candidate by its backticked state path (`candidates[i]`).

**Return shape (since 2026-10-01).** `judgeRelevanceQuestions` returns a THIRD value beside the keep map: `scores map[string]float64` — the raw per-candidate `noul` for every candidate that got a real noul answer (`internal/agent/relevance_typesafe.go:80`). Its contract:

- Keep/veto semantics of ALL THREE judges sharing the helper are UNCHANGED — the boolean keep map remains the authority, and the signature change is additive. The other two callers discard the scores with `_`: doc_search (`internal/agent/doc_search_typesafe.go:107`) and tool/code-search (`internal/agent/search_typesafe.go:121`).
- A VETOED candidate still reports its real score (the score is written at `:80` before the floor comparison at `:81`) — that is what lets a caller see how close a turn came to firing.
- A MISSING or non-noul answer is ABSENT from the scores map, never defaulted to 0.0 (that path `continue`s at `internal/agent/relevance_typesafe.go:73-76` before the write). Absent means "unknown"; callers must treat it that way, never as a zero score.
- `judgeDiscoveryCandidates` correspondingly returns `([]discovery.Doc, map[string]float64, error)` (`internal/agent/discovery_typesafe.go:81`, returning at `:106`); `runDiscovery` captures the map as `judgeScores` (`internal/agent/discovery_glue.go:435`, assigned at `:443`).

## Split Select/Seed

`Session.Select` (`internal/discovery/engine.go:114`) ranks the query against the corpus via `Engine.Rank`, applies `SelectRankRelative` (which caps at `SelectCap = 30`), and returns the not-yet-attached candidates **without mutating the sticky set**. This lets the caller inspect or veto candidates before they join.

`Seed` (`internal/discovery/engine.go:149`) marks the survivors as attached.

`Discover` (`internal/discovery/engine.go:134`) is exactly `Select` + `Seed` — the plain attach-everything wrapper used when no judge is active.

The judge path in `runDiscovery` (`internal/agent/discovery_glue.go:356`) calls `Select`, feeds the candidates to `judgeDiscoveryCandidates`, and only calls `Seed` with the kept IDs. That same seam is where auto-injection is decided: `maybeAutoInjectSkill(keep, judgeScores, tail)` runs after judging and before `Seed` (`internal/agent/discovery_glue.go:454`), and only on this automatic per-turn path — never from `discover_more`.

## Fail-open matrix

| Condition | Behaviour |
|---|---|
| No decision backend connected (`discoveryJudgeClient()` returns nil) | Seed all candidates — attach-everything |
| Transport or decode error from `DecideCtx` | Seed all candidates (logged via `emitDebug("DISCOVERY", ...)`) |
| Candidate answer missing or not `type:"noul"` | That candidate is kept (fail-open per candidate) |
| Real below-threshold noul (Noul < 0.5) | Vetoes only that candidate |

The judge can only ever **veto** — a failure never attaches fewer docs than the pre-judge behavior. A vetoed doc stays unattached and is re-judged on a later turn when the query or transcript changes. The judge never changes `renderDiscoveryContext` (`internal/agent/discovery_glue.go:815`); the names-index is a function of the doc set, not of which ids are attached.

**The one deliberate exception: auto-injection fails CLOSED.** The matrix above governs *attach*; the auto-inject consumer of the same scores inverts every row — no judge, a transport error, or a missing/non-noul answer all mean no score, and no score means no injection. Rationale in the 2026-10-01 amendment.

## Amendment (2026-09-26): `discover_more` is a second judged attach path — and the warm gate is NOT the judge

The sections above describe the judge as a per-turn `runDiscovery` mechanism. That was accurate when written; the on-demand attach path has since been brought under the same gate. (Line references in this amendment were re-derived against the tree on 2026-10-02: `runDiscovery` now lives at `internal/agent/discovery_glue.go:356`, `renderDiscoveryContext` at `:815`, `DiscoveryStatusInfo.Judge`/`JudgeVetoed` at `:511-529`.)

**Every retrieval attach path is now judged.** `discover_more` (`discoverMoreTool.Execute`, `internal/agent/discovery_glue.go:953`) used to call `Session.Discover` (Select+Seed, no judge), so a model that named a need bypassed Jev entirely — the exact guard-bypass class where a fallback path skips the gate the primary path runs. It now calls `Session.Select` (`:972`), then `judgeDiscoveryCandidates` when `discoveryJudgeClient()` resolves (`:977`), then `Seed`s only the survivors (`:994`).

**Its fail-open matrix is identical to the table above.** Same four rows: no backend connected → seed all candidates; `DecideCtx` transport/decode failure → seed all (logged as `discover_more judge failed (fail-open, all attached): ...`, `:982`); missing or non-`noul` answer → keep that candidate; below-threshold noul → veto that candidate. Real vetoes increment the **same** `discoveryState.judgeVetoed` counter the per-turn path uses (`:986` vs. `:445`), so `/discovery status`'s `vetoed N` totals both paths. The path's own debug line is `discover_more("<need>") → +N tools (judge kept N/M)` (`:995`).

**The on-demand judge sees the conversation too.** `noteDiscoveryTail` / `discoveryTail` (`:926` / `:944`) record a bounded copy of the turn's last `discoveryJudgeTailN` (6) messages on `discoveryState.tail`, guarded by `tailMu`. `runDiscovery` records it right after `ensureDiscovery()` and **before** its early returns (`:364-369`) — the cold-cache deferral is precisely the turn where nothing is attached and the model must fall back to `discover_more`.

**The reply distinguishes veto-all from no-match.** When candidates matched but the judge vetoed all of them, `discover_more` now returns `N tool(s) matched that need but were judged out of scope for this request. Try a different need, or continue without them.` instead of the old `No additional tools matched that need.` — so the model can distinguish "nothing matched" from "matched but out of scope" and retry rather than conclude the capability is missing.

**The reply distinguishes veto-all from no-match.** When candidates matched but the judge vetoed all of them, `discover_more` now returns `N tool(s) matched that need but were judged out of scope for this request. Try a different need, or continue without them.` instead of the old `No additional tools matched that need.` — so the model can distinguish "nothing matched" from "matched but out of scope" and retry rather than conclude the capability is missing.

**The corpus-warm gate is a SEPARATE mechanism — do not conflate it with the judge's fail-open.** The fail-open matrix above governs *which retrieval candidates* get seeded; the warm gate governs *whether any MCP tool schemas are callable at all*:

| Mechanism | Location | Failure it handles | Effect |
|---|---|---|---|
| Judge fail-open (matrix above) | `runDiscovery` `:433-449`, `discover_more` `:977-989` | TypeSafe transport/decode failure, or TypeSafe not connected | **Attaches everything the embedder proposed** — the judge only ever filters retrieval results; it never gates the tool list |
| Corpus-warm / rank gate (`discoveryAllows`) | `internal/agent/discovery_glue.go:630-641` | Cold embedder cache or failed rank — deliberately **no fail-open** for these (the only surviving fail-open is discovery-off) | **Attaches nothing** — hides unattached MCP tool *schemas* from the model's callable tool list; the names-only index still advertises every name |

A warm failure and a judge failure therefore have **opposite** effects on attach volume: warm-fail → zero MCP tool definitions this turn (recover via `discover_more`, which warms with no timeout); judge-fail → every candidate attached. The warm gate's history and rules are documented in [Discovery MCP Tool Gating](concepts/discovery-mcp-tool-gating.md).

## Amendment (2026-10-01): auto-injected skill body — the judge's one fail-closed consumer

Discovery used to advertise skills by NAME ONLY (a cached, system-role name index) and rely on the model choosing to call the `skill` tool for the body — a round trip that depends on the model taking it. Now, when Jev is confident that ONE skill is the right one, the full body is inlined into the prompt automatically. Implementation: `internal/agent/discovery_autoinject.go` (tests in `internal/agent/discovery_autoinject_test.go`). All line references in this section were verified against the tree on 2026-10-02.

### Score plumbing

Described under [State and question shape](#state-and-question-shape) above: `judgeRelevanceQuestions` gained the third `scores` return, `judgeDiscoveryCandidates` forwards it (`internal/agent/discovery_typesafe.go:77`), and `runDiscovery` hands the map to `maybeAutoInjectSkill(keep, judgeScores, tail)` (`internal/agent/discovery_glue.go:454`). The keep/veto contract of the discovery, doc_search and tool-search judges is untouched; the staging state lives on `discoveryState` (`autoInject`, `autoInjected`, `autoInjectMu` — `internal/agent/discovery_glue.go:57-75`).

### Its own confidence floor: `discoveryAutoInjectFloor = 0.8`

Hardcoded (`internal/agent/discovery_autoinject.go:57`) — deliberately **no config key**. It is neither of the two existing floors:

- **Not `relevanceJudgeMinConfidenceDefault` (0.5):** that floor answers "should this NAME be shown?", where the product rule is "even slight relevancy should be presented". Inlining a body is a much bigger commitment than printing a name.
- **Not `permissions.auto.min_confidence` (`autoJudgeMinConfidenceDefault`, 0.85):** reusing it would couple a prompt-spend decision to a permission tuning, so a stricter auto-permission setting would silently disable auto-injection.

This is the same risk-scaling rule the other judges follow — each judge has its own floor scaled to the cost of a wrong decision (floor table in [Confidence floor](#confidence-floor) above).

### Fails CLOSED, unlike every other judge here

No judge connected, a judge transport error, or a missing/non-noul answer all mean **no score**, and no score means **no injection** — `maybeAutoInjectSkill` returns immediately on an empty score map (`internal/agent/discovery_autoinject.go:232`). The existing discovery judge fails OPEN because it can only ever veto, and a veto merely hides a retrieval result. Auto-injection is the opposite: a spurious injection spends real prompt budget on the wrong instructions, on every request until compaction. The asymmetry is deliberate — fail-open can only lose information, fail-closed here can only lose spend.

### Selection

`pickAutoInjectSkill(keep, scores)` (`internal/agent/discovery_autoinject.go:98`) takes the **highest-scoring** candidate whose `Kind == "skill"` and whose noul ≥ 0.8. Ties break by embedder rank — candidates arrive in rank order from `Session.Select` (`internal/discovery/engine.go:114`), so first-wins is deterministic rather than map-iteration order. MCP tools and project markdown docs are never inlined (an MCP tool's gate is a separate mechanism; md docs already emit as summaries).

A skill is a candidate only on the turn it **FIRST** attaches, because `Session.Select` skips already-attached docs — so selection happens at most once per skill per session, with no re-judging loop to get wrong. Auto-inject runs only on the automatic per-turn path (`runDiscovery`), **NOT** from the `discover_more` tool, which discards the scores (`judged, _, jerr` at `internal/agent/discovery_glue.go:978`): there the model asked for more and will load the body itself.

### Dedupe, three layers

1. **Sticky set** — `recordAutoInject` (`internal/agent/discovery_autoinject.go:299`) refuses a replay: a skill name already in `discoveryState.autoInjected` is never staged again. A name that passes is **appended** to `discoveryState.autoInject`, which is now a `[]*autoInjectSkill` list rather than a single slot, so a second confident pick never evicts the first skill's body. (The former single slot DID evict — and because the blocks are never persisted while the sticky set still refused to re-select the evicted name, only a compaction could bring that body back.)
2. **`chatAlreadyHasSkill`** (`internal/agent/discovery_autoinject.go:166`) scans the **whole transcript** for three signals: a prior `skill`/`load_skill` tool call naming it (the JSON `name` field is compared after a cheap `strings.Contains` pre-filter, so `pdf` does not match a `load_skill("pdf-forms")` call); the skill's `Source` path appearing in any message (the model read the SKILL.md directly); or a previous auto-inject block (marker `auto-loaded skill`, `internal/agent/discovery_autoinject.go:71`).
3. **Compaction resets** — `resetAutoInjected()` (`internal/agent/discovery_autoinject.go:326`) clears the sticky set AND **every staged block**, called beside `resetDirMDSeen()` at the compaction splice (`internal/agent/agent.go:2898`, and `resetAutoInjected()` on the next line at `:2807`). Lifetime is the **session**, not the turn, and that is deliberate, not an oversight: these blocks are request-time injections that are **never persisted**, so dropping them between turns would lose the bodies with no way to get them back — clearing them only at the compaction splice is what keeps them available while keeping the re-select honest (after a splice the model genuinely no longer has them and the next judged turn must be free to re-select).

Once staged, the blocks are re-rendered on every subsequent request of the session until compaction (`autoInjectBlock` is read by `injectDiscoveryContext` on each Step) — that is intentional: the bodies stay available across turns without re-judging. Nothing clears them between turns and nothing persists them either; the session lifetime named in the previous list is the deliberate consequence of both.

### Bounded at 8KB

`discoveryAutoInjectMaxBytes = 8 << 10` (`internal/agent/discovery_autoinject.go:65`). `truncateSkillBody` (`:121`) keeps the head and cuts on a line boundary so the block never ends mid-sentence. The block states that it was truncated and that the rest is one `skill` call away (`renderAutoInjectBlock`, `:206`).

### User-role, and this is load-bearing for the prompt cache

`autoInjectBlock()` renders **every** staged block, oldest first, and `injectDiscoveryContext` (`internal/agent/discovery_glue.go:754`) appends them as ONE **`user`-role message LAST in the tail** (`:797-799`) — a single message carrying the whole list, so the volatile append stays one message and the cached block above it is untouched either way. `collectAndRemoveSystemMessages` hoists EVERY system-role message into the cached `system` field, so a per-turn-varying injection there would invalidate the entire cached prefix. `TestInjectDiscoveryContextAutoInjectKeepsSystemBlockByteIdentical` (`internal/agent/discovery_autoinject_test.go:246`) pins that the system block is byte-identical whether or not any injection fires. In the stable/volatile split of `injectDiscoveryContext`, the auto-inject blocks are on the **volatile** side — uncached user tail, alongside attached-skill descriptions.

### The latent root bug fixed on the way

`skill.LoadSkill` resolved the project root from `os.Getwd()` — the SERVER process's cwd, the wrong root for any session not rooted at the process cwd. Added `skill.LoadSkillForRoot(name, root)` (`internal/skill/loader.go:605`); `LoadSkill` (`internal/skill/loader.go:595`) delegates with the cwd exactly as before, so its existing callers — `internal/tool/misc.go:59` and `internal/memory/memory.go:241` — are unaffected. Auto-inject uses the root-aware form with `a.workDir` (`loadAutoInjectBody`, `internal/agent/discovery_autoinject.go:141`; test `TestLoadAutoInjectBodyUsesWorkDirNotProcessCwd` at `internal/agent/discovery_autoinject_test.go:434`).

## How to observe

`/discovery` status shows `judge: typesafe/jev-latest (vetoed N this session)` only when TypeSafe is connected (`DiscoveryStatusInfo.Judge` / `JudgeVetoed` in `internal/agent/discovery_glue.go:511-529`).

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

**Auto-inject lines (same `DISCOVERY` debug kind, via `a.emitDebug`).** The top skill's real score is logged on **every judged turn, including below the floor**, so the 0.8 constant can be tuned against observed numbers rather than guessed at, and so "the judge never scored anything high" is distinguishable from "the feature is dead":

- `auto-inject: no skill qualified (best %q noul=%.3f < floor %.2f)` — a scored skill existed but none reached 0.8 (`logAutoInjectMiss`, `internal/agent/discovery_autoinject.go:269`).
- `auto-inject: skill %q scored %.2f (floor %.2f) — body inlined (%d bytes, truncated=%v)` — success (`:250`).
- `auto-inject: skill %q scored %.2f but skipped (already injected this session, or the chat already has it)` — dedupe refused (`:247`).
- `auto-inject: skill %q scored %.2f but its SKILL.md could not be read from the session root; not injecting` — loader miss (`:242`).

No `auto-inject:` line at all on a turn with candidates means there was no judge that turn (fail-closed — nothing to log or inject).

**Per-candidate and summary lines (unchanged).** The existing lines from `judgeRelevanceQuestions` in `internal/agent/relevance_typesafe.go` remain the detailed breakdown:

- `discovery_typesafe id=<id> noul=<score> verdict=keep|veto` — per-candidate verdict.
- `discovery_typesafe kept=<n> vetoed=<n> min=<threshold> model=<model>` — summary line after the judge call.

## Cost

At most one `Decide` per turn that has new candidates, with at most `SelectCap` (30) noul questions. Recorded as side usage via `RecordSideUsage`. No call at all when nothing new is selected (empty candidates from `Select`).

Auto-injection adds **no extra `Decide`** — it reuses the same turn's score map. Its marginal cost is one local skill-file read plus at most `discoveryAutoInjectMaxBytes` (8KB) **per staged skill** appended to the **uncached** user tail, on the staging turn and, for every block then staged, on later turns of the session until compaction (the staged blocks are re-rendered per request because they are never persisted).

## See also

- [Doc Search Relevance Judge](concepts/doc-search-relevance-judge.md) — the doc_search judge that shares the same lenient core and floor.
- [Discovery Web Surfaces](concepts/discovery-web-surfaces.md) — how discovery status and live notices are surfaced in the web/desktop UI, including the runtime status endpoint and the map-race safety rule for reading agent state.
- [Discovery MCP Tool Gating](concepts/discovery-mcp-tool-gating.md) — the names-only index vs. callable definitions split, the strict no-warm-fail-open gate, and the `discover_more` recovery path this amendment's judge path serves.
- [Prompt Cache Stability](concepts/prompt-cache-stability.md) — the tools → system → messages prefix order and the role-determines-caching rule the user-role auto-inject block obeys.

## Amendment (2026-10-04): Per-slot model configuration, the Decider seam, and the cache that finally lets `/connect` take effect

**The `Decider` interface.** A new file `internal/agent/decider.go` introduces a `Decider` interface with exactly four methods: `DecideCtx`, `Decide`, `GetProvider`, `GetModel`. It is deliberately narrower than `LLMClient` so a decision-only backend cannot reach the chat, compaction, small-model or interpreter-effects paths. `*TypesafeClient` satisfies it unchanged. `isDecisionModel(modelID)` (`internal/agent/decider.go:214`) is the single place that decides which provider/model ids route to a decision backend (currently `typesafe/*` and Cloudflare Workers AI clef models).

**Per-judge model configuration.** Five new config keys, each `omitempty`, each defaulting to `typesafe/jev-latest`:
- `discovery.judge_model`
- `doc_search.judge_model`
- `search.judge_model`
- `network_guard.judge_model`
- `content_guard.judge_model`

`permission` and `auto_continue` keep their existing keys (`permissions.auto.model` and `auto_continue_model`) — they were not moved or renamed. The default `typesafe/jev-latest` lives in one place: `defaultJudgeModel` in `internal/config/ocodeconfig.go:789`.

**The three hardcoded constants are deleted.** `discoveryJudgeModel`, `networkGuardJudgeModel`, and `contentGuardJudgeModel` no longer exist. The default `typesafe/jev-latest` lives in `defaultJudgeModel` (`internal/config/ocodeconfig.go:789`).

**doc_search and code search are now independent of discovery.** Previously all three relevance judges called the single `discoveryJudgeClient()` and shared one client, so setting one model moved all three. Each is now its own slot with its own key. The `code_search` SLOT reads the `search` KEY, because `search` is also a tool name and naming the slot to match would read as though the tool itself were configured.

**The judge client cache changed, and this fixes a real bug.** It used to be a `sync.Once` that pinned whatever resolved first for the whole session. It is now a mutex plus a stored model id, and — the point — **a nil client is deliberately NOT cached.** Consequence: `/connect <provider>` now takes effect on the next judge call instead of requiring a `/discovery toggle` or a restart.

**Debug labels and ledger attribution are now provider-qualified** via `deciderLabel(client)` (provider + "/" + model), replacing reads of the concrete client's `Model` field with a hardcoded `typesafe/` prefix. Judge log lines, `/discovery status`'s `Judge` field, and `RecordSideUsage` spend attribution all now name the backend that actually answered. This matters for the usage ledger: before, every decision backend's tokens would have been booked to TypeSafe.

**Unchanged and worth stating explicitly**, since it is the load-bearing invariant: the fail-open contract is untouched. A judge may only ever *hide* a retrieval result, never invent one; a transport or decode error keeps every candidate; the relevance confidence floor (`relevanceJudgeMinConfidenceDefault` = 0.5) and the permission floor are unchanged; `permissions.auto.min_confidence` still governs the opaque relaxation; and the MCP tool gate (`discoveryAllows`) is untouched and still must never fail open.
