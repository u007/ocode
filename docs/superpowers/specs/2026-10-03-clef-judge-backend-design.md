# Clef decision-judge backend + per-judge model selection — design

Date: 2026-10-03
Status: approved in chat (all five sections accepted; scope, config-migration and
oversized-state decisions confirmed). Awaiting spec review.

## Provenance and verification notes

- **Every code anchor below was mechanically verified** against the working tree
  at the time of writing: each cited line was printed and checked for existence
  and for needle uniqueness. Three anchors were corrected during that pass (the
  `TypesafeQuestion` struct, `relevanceJudgeMinConfidenceDefault`, and
  `buildTypesafePermissionState` were each one line lower than first written).
  Several anchored files are concurrently modified in the working tree, so
  **re-verify anchors immediately before committing** — a clean run is
  point-in-time only.
- This file was placed in the bundle by the main agent with the user's explicit
  approval to lift CLAUDE.md's sole-automated-writer invariant **for this
  artifact only**. `docs/index.md` and `docs/log.md` are auto-managed and were
  deliberately not edited; the `context` agent should pick the spec up and index
  it when its provider quota allows.

## Summary

Add `@cf/cloudflare/clef-flash` (and its sibling `@cf/cloudflare/clef`) as a second
decision-judge backend alongside the existing TypeSafe/Jev System One client,
behind a provider seam so either can serve any judge. Expose a user-visible model
picker for **every** judge call site, so each of the seven judges can
independently select its backend.

Clef is a drop-in at the wire level. Cloudflare describes it as *"a fast 9B
multimodal decision model that turns a state and a schema of typed questions into
decisions"* and states *"The Clef-Flash API is fully compatible with Jev and
SystemOne"* — the `images` field is the only documented *"Clef extension to the
System One API"*. Its output schema is structurally identical to ocode's existing
`TypesafeResponse`.

## Background: why Clef, and what it costs

Two sibling models, both Cloudflare-hosted, both released 2026-09-29:

| | `@cf/cloudflare/clef-flash` | `@cf/cloudflare/clef` |
| --- | --- | --- |
| Params | 9B | 27B |
| Context | 65,536 tokens | 65,536 |
| Vision | yes | yes |
| Streaming | yes | yes |

Pricing (Workers AI model page): **$0.09 per M input tokens**, no separate output
charge — it emits probabilities, not text. Rate limit: the default Text
Generation limit of **300 requests/minute** (the model page does NOT carry the
"Paid access required" notice, so the 20 req/min paid-model ceiling does not
apply).

This is **more expensive than Jev**, which is $0.042 per M input tokens with free
output (TypeSafe's Models page: "$42 / $0.042" per Btok/Mtok, "Charged per input
token. Output tokens are free"). clef-flash is roughly 2.1x Jev's input price.
What it buys is latency and measured calibration.

### Provenance of the claims in this section

These come from two very different classes of source and should not be weighted
equally. Each load-bearing claim is listed with its own source.

| Claim | Source | Class |
| --- | --- | --- |
| model ids `@cf/cloudflare/clef` / `clef-flash`; 9B / 27B params; 65,536 context; $0.09/M input; vision; streaming | `https://developers.cloudflare.com/workers-ai/models/clef-flash/` and `.../clef/` | vendor page |
| body `model` selector is `clef`/`clef-flash`, pattern `^\s*(clef|clef-flash)\s*$` | `https://developers.cloudflare.com/workers-ai/models/clef-flash/schema-input.json` | **schema (authoritative)** |
| `questions` `minProperties:1` `maxProperties:64` | same `schema-input.json` | **schema (authoritative)** |
| question id charset: *"ids may use letters, digits, `'_'`, `'.'`, `'-'` (max 100 chars)"* | same `schema-input.json`, `questions.description` | **schema (authoritative)** — quoted verbatim |
| choice `criteria` 2–255 options; score `criteria` 2–10 ordered levels; `images` max 4, 13 MiB body cap | same `schema-input.json` | **schema (authoritative)** |
| *"Long text state is truncated to fit the model's token limit"* — silent, no error | same `schema-input.json`, `state.description` | **schema (authoritative)** — quoted verbatim |
| `answers` shape: `choice` → `choice`+`probabilities`+`confidence`; `noul` → `noul`; `score` → `score`+`legend`+`probabilities`+`confidence`; `confidence` *"derived from the probabilities"* | `https://developers.cloudflare.com/workers-ai/models/clef-flash/schema-output.json` | **schema (authoritative)** |
| *"The Clef-Flash API is fully compatible with Jev and SystemOne"* | `https://huggingface.co/Cloudflare/clef-flash` | vendor claim |
| one logit per option, softmax per question | same HF card | vendor claim |
| Decision Index 0.2.1 benchmark rows incl. ForecastBench 10.6, median 38.8 ms, p95 122.4 ms | same HF card | **vendor-claimed self-evaluation — not independently reproduced by ocode** |
| Jev median 524.1 ms / p95 536.0 ms, ForecastBench 17.4 | same HF card (Cloudflare's run) | **vendor-claimed, and Jev is a competitor** — treat with extra suspicion |
| 300 req/min default Text Generation limit | `https://developers.cloudflare.com/workers-ai/platform/limits/` | **inferred** — clef-flash's page carries no "Paid access required" notice, so it should fall under the default rather than the 20/50 req/min paid-model ceiling. This is an argument from a doc's silence, not a stated fact; confirm against a live account before relying on it |
| Jev price ($42/Btok, $0.042/Mtok), free output, 64k request / 32k state context, RLCD calibration | `https://docs.typesafe.ai/models` | **primary** |
| Jev failure modes (large state, adversarial content, choice option order) | `https://docs.typesafe.ai/model-jaggedness/jev-1.13` | **primary** |

The two rows to keep in mind when reading this spec: the **calibration** row that
justifies sharing the confidence floors is vendor-claimed, and the Jev latency row
is a competitor's measurement of us. §5 is what replaces both with first-party
numbers.

Cloudflare's internal Decision Index 0.2.1 results (percentages; ForecastBench is
a Brier score where lower is better), selected rows:

| Benchmark | Clef | Clef-flash | Jev |
| --- | --- | --- | --- |
| ForecastBench (Brier, lower better) | 13.9 | **10.6** | 17.4 |
| BFCL (case exact accuracy) | 98.5 | **98.8** | 95.8 |
| API-Bank (accuracy) | 91.9 | **93.1** | 88.2 |
| ToolRet (nDCG@10) | **69.2** | 66.4 | 65.3 |
| RouterBench (selected quality) | 79.7 | 79.9 | 79.9 |
| CLINC150+OOS (macro-F1) | **97.4** | 66.8 | 89.3 |
| Habermas Machine (accuracy) | 68.7 | **71.8** | 45.9 |
| MMLU (accuracy) | 90.3 | **91.8** | 91.7 |
| MMLU-Pro (accuracy) | 65.9 | 65.3 | **82.7** |
| BBH (accuracy) | 73.7 | 68.9 | **92.9** |
| GPQA Diamond (accuracy) | 48.0 | 51.0 | **78.3** |
| Median latency (ms) | 209.3 | **38.8** | 524.1 |
| p95 latency (ms) | 238.6 | **122.4** | 536.0 |

The split matters: clef-flash is better calibrated (the ForecastBench row is the
one that matters, because every ocode judge floors on a probability) and better on
tool-routing/classification, while Jev is decisively better on hard reasoning.
This is the risk profile the permission judge sits on.

Calibration compatibility is a design premise, not an assumption we get to skip.
Clef is **not** a text LLM emitting a self-reported number. Per the HuggingFace
card, its output is *"one logit per allowed option for each question. Apply a
softmax per question to get probabilities"*, and the output schema documents
`confidence` as *"How certain the model is, derived from the probabilities."*
TypeSafe independently describes Jev as *"trained with RLCD to return calibrated
decisions"* — also a calibrated-decision model by construction, not a text model
reporting a feeling. That is why the two APIs are wire-identical. The agreement
eval in §5 exists to convert the premise into a first-party measurement.

## Measured fit against Clef's limits

Clef's input schema constrains three things: `questions` has `maxProperties: 64`;
question ids may use only `letters, digits, '_', '.', '-'` up to 100 chars; and
`state` is *"truncated to fit the model's token limit"* — silently, with no error.

Each judge request was built at its documented worst case through the real
builders and marshalled. Question counts come from the production caps
`SelectCap           = 30` at `internal/discovery/index.go:75` and
`const searchJudgeMaxCandidates = 40` at
`internal/tool/search_judge_apply.go:14`.

> **Reproducibility.** These numbers came from a throwaway harness built in
> `internal/agent` that constructed each worst-case request through the real
> builders (`buildDiscoveryJudgeState`, `buildSearchJudgeState`,
> `buildDocSearchJudgeState`, `buildTypesafePermissionState`,
> `buildContentGuardState`) and marshalled `{state, questions}`. The harness was
> deleted after reading the figures, so the numbers are **not currently
> reproducible from the tree**. §5 turns it into a permanent assertion-based
> regression test — see the last bullet there.

| Judge | Questions | Bytes | ~tokens | vs 65,536 ctx |
| --- | --- | --- | --- | --- |
| discovery relevance | 30 | 83,387 | 20,846 | 32% |
| code search relevance | 40 | 51,200 | 12,800 | 20% |
| doc_search relevance | 40 | 74,883 | 18,720 | 29% |
| permission (200 KB `write` payload) | 2 | 219,820 | **54,955** | **84%** |
| content guard (6 KB chunk) | 2 | 10,575 | 2,643 | 4% |
| network guard | 2 | 8,515 | 2,128 | 3% |

Three conclusions:

1. **The 64-question cap is not currently binding.** Worst observed is 40,
   discovery caps at 30. Headroom is 24 questions, so the ceiling is insurance
   rather than a blocker.
2. **The id charset is a hard blocker, and it is worse than paths alone.**
   Discovery keys its questions by `discovery.Doc.ID`, whose canonical form is
   `skill:<name>` or `mcp:<Server>/<tool>` — containing `:` and `/`. doc_search
   (`internal/agent/doc_search_typesafe.go:98`) and code search
   (`internal/agent/search_typesafe.go:112`) key by file path, containing `/`. All
   three relevance judges therefore send ids Clef rejects outright.
3. **The context limit is a real risk on the permission judge — but it is
   PRE-EXISTING, not something Clef introduces.** This corrects an earlier draft
   of this spec, which asserted that Jev "tolerates this today because TypeSafe's
   context is far larger". That was wrong. TypeSafe's Models page states Jev's
   limits as *"64k tokens per request; 32k tokens for `state` plus the longest
   question"*. So Jev's total budget (64k) is effectively the same as Clef's
   (65,536), and Jev's *state* budget (32k) is **half** of Clef's. A 200 KB
   `write` — ~55K tokens of arguments — already exceeds Jev's documented 32k
   state allowance in production today. ocode puts the raw arguments into the
   judge state uncapped (`"arguments":         arguments,` at
   `internal/agent/permission_typesafe.go:411`); `maxCtxBytes` (default 2048)
   bounds only `project_context`. TypeSafe's own guidance for this case is
   *"retrieve and filter in code first, and send only the fields the question
   needs"*, and their jaggedness page lists "Large state full of irrelevant
   detail" as a named accuracy failure mode.

   The consequence for this design: the state-size guard **must not live in
   `clef.go`**. It is a backend-independent constraint that the existing Jev path
   needs just as much, so it belongs in the shared seam (§1), sized to the
   stricter of the two budgets. Switching to Clef neither introduces this problem
   nor fixes it — it is a latent bug this work makes visible.

## §1 The seam

A `Decider` interface in a new `internal/agent/decider.go`. `TypesafeClient`
already has exactly this method set — `func (c *TypesafeClient) DecideCtx` at
`internal/agent/typesafe.go:88` — so it satisfies the interface with **no changes
to its existing methods**:

```go
type Decider interface {
    DecideCtx(ctx context.Context, state any, q map[string]TypesafeQuestion) (*TypesafeResponse, error)
    Decide(state any, q map[string]TypesafeQuestion) (*TypesafeResponse, error)
    GetProvider() string
    GetModel() string
}
```

One resolver replaces the five hardcoded `newClientFn(...).(*TypesafeClient)`
sites — `if client := newClientFn(a.config, model); client != nil {` at
`internal/agent/agent.go:2607` (auto-continue),
`client, ok := newClientFn(a.config, modelName).(*TypesafeClient)` at
`internal/agent/agent.go:3833` (permission),
`newClientFn(a.config, discoveryJudgeModel).(*TypesafeClient)` at
`internal/agent/discovery_typesafe.go:53`,
`newClientFn(a.config, networkGuardJudgeModel).(*TypesafeClient)` at
`internal/agent/network_guard_typesafe.go:315`, and
`newClientFn(a.config, contentGuardJudgeModel).(*TypesafeClient)` at
`internal/agent/content_guard_typesafe.go:335`:

```go
func (a *Agent) resolveDecider(slot judgeSlot) Decider
```

with slot constants `slotPermission`, `slotAutoContinue`, `slotDiscovery`,
`slotDocSearch`, `slotCodeSearch`, `slotNetworkGuard`, `slotContentGuard`.

Judge function signatures widen from `*TypesafeClient` to `Decider`. This is
mechanical and their existing tests continue to pass unchanged.

`type TypesafeQuestion struct {` at `internal/agent/typesafe.go:43` and
`type TypesafeAnswer struct {` at `internal/agent/typesafe.go:51` remain the
shared vocabulary unchanged — they are already the System One schema, which is
precisely why Clef needs no translation layer.

Four non-judge decision callers share the same `Decide` path
(`internal/agent/doc_maintenance.go:226`,
`internal/agent/memory_maintenance.go:203`,
`internal/agent/task_contract.go:96`,
`internal/agent/permission_interpreter.go:233`). They move to the seam and gain
no UI slot.

## §2 The Clef backend

`internal/agent/clef.go` — `ClefClient{APIKey, AccountID, Model}`, mirroring
`DecideCtx` line for line: the same context-deadline fallback that is never
extended when the caller supplies its own deadline, no `http.Client.Timeout` (the
context governs), the same 1 MiB `io.LimitReader` read cap, and the same
`newProviderStatusError` on a non-2xx.

Clef is served only by the **native** Workers AI endpoint, not the
OpenAI-compatible chat one, so `GenericClient` cannot be reused:

- URL: `https://api.cloudflare.com/client/v4/accounts/{account_id}/ai/run/@cf/cloudflare/clef-flash`
- Body: `{"model":"clef-flash", "state": ..., "questions": {...}}`

The body `model` field is the selector (`clef` or `clef-flash`) and is distinct
from the URL model id. `Chat()` returns a new `ErrClefDecisionOnly` sentinel
mirroring `var ErrTypesafeDecisionOnly = errors.New(` at
`internal/agent/typesafe.go:19`, so the backend can never be routed into the
chat, compaction, or small-model paths.

### Guards, and which layer owns them

The state-size guard is **backend-independent** and lives in the shared seam, not
here — see finding 3 above. The id-sanitising and question-ceiling guards *are*
Clef-specific (Jev has no such limits) and live here.

| Guard | Layer | Trigger | Behaviour |
| --- | --- | --- | --- |
| state size | shared seam (§1) | pre-flight estimate over the effective budget, which is the **stricter** of the two backends' documented limits | **for choice judges (permission, network guard, content guard): return an error, which fails open to a human ask.** For noul relevance judges the measured worst case is 21K, well inside budget, so the guard is belt-and-braces |
| id sanitise + demux | `clef.go` | ids containing `:` or `/` | map into `[A-Za-z0-9_.-]`, truncate at 100 chars, disambiguate collisions, and demux answers back to the original ids |
| question ceiling | `clef.go` | more than 64 questions | trim to 64; the remainder keeps its pre-judge state, so a judge can still only ever *hide* a result, never invent one |

Because the budget is currently the *stricter* one, the seam guard will fire on
large permission writes **whether the slot is set to Jev or to clef-flash** —
which is the correct outcome, since Jev's 32k state allowance is already being
exceeded today. Surfacing that is a behaviour change on the Jev path and is
called out deliberately rather than discovered later.

The state-size guard is the load-bearing one. Cloudflare truncates oversized
`state` silently, and on the permission path that means grading a command the
judge cannot fully see. There is deliberately **no silent fallback to the other
backend**: a provider switch on the highest-stakes path is exactly the kind of
behaviour change that must be visible.

## §3 Config: keep existing keys, add five

`permissions.auto.model` and `auto_continue_model` stay exactly where they are,
untouched and still authoritative for their two judges. Five new small blocks
mirror the existing top-level file layout (`Permissions permissionConfigFile` at
`internal/config/ocodeconfig.go:1164` and `Discovery discoveryConfigFile` at
`internal/config/ocodeconfig.go:1172` are the precedents):

```json
"discovery":     { "judge_model": "typesafe/jev-latest" },
"doc_search":    { "judge_model": "typesafe/jev-latest" },
"search":        { "judge_model": "typesafe/jev-latest" },
"network_guard": { "judge_model": "typesafe/jev-latest" },
"content_guard": { "judge_model": "typesafe/jev-latest" }
```

Every slot defaults to `typesafe/jev-latest` — the value of the constants being
deleted — so **an unconfigured install behaves identically**. The three hardcoded
constants `const discoveryJudgeModel = "typesafe/jev-latest"` at
`internal/agent/discovery_typesafe.go:18`,
`const networkGuardJudgeModel = "typesafe/jev-latest"` at
`internal/agent/network_guard_typesafe.go:40`, and
`const contentGuardJudgeModel = "typesafe/jev-latest"` at
`internal/agent/content_guard_typesafe.go:51` are deleted and replaced by slot
lookups.

The one deliberate behaviour change: discovery, doc_search and code search today
share one client via `func (a *Agent) discoveryJudgeClient() *TypesafeClient` at
`internal/agent/discovery_typesafe.go:41`, consumed at
`internal/agent/doc_search_typesafe.go:128` and
`internal/agent/search_typesafe.go:150`. Splitting them into three
independently-defaulted slots means setting one no longer silently moves the
other two.

`permissions.auto.min_confidence` keeps its meaning. Under the approved decision
all backends share the existing floors — 0.85 for the permission judge
(`autoJudgeMinConfidenceDefault`), and `const relevanceJudgeMinConfidenceDefault
= 0.5` at `internal/agent/relevance_typesafe.go:20` for the lenient relevance
judges — on the calibration argument above. §5 is what validates it.

## §4 UI

One model picker per slot, seven total, in both the TUI and the web/desktop SPA.
The web side feeds off the existing `GET /api/models` provider/model list, the
same data the main model picker already consumes, so each slot is a dropdown row
rather than new plumbing. Each row shows provider, model, and a note when the slot
has no credential configured. Writes go to the config keys in §3 via the existing
settings persistence path.

## §5 Validation

**Agreement eval — a deliverable, not a follow-up.** Skips cleanly when the
credential is absent (so a normal `go test` run never touches the network).
Replays the existing judge fixtures against both backends and reports verdict
agreement, p50/p95 latency, tokens and cost per decision.
`internal/agent/content_guard_eval_test.go` is the template. This is what turns
"share the floors" from a premise into a measurement, and it is the gate on
trusting clef-flash with the permission judge.

**Unit tests:**

- id sanitise + demux round-trips a path-keyed and a `skill:`-keyed question set
  back to the original ids
- the question ceiling trims to 64 without inventing a verdict for the trimmed
  remainder
- an oversized permission state returns an error that produces a human ask, never
  a silent allow or deny
- each of the seven slots defaults to `typesafe/jev-latest`
- provider routing builds a `ClefClient` for `cloudflare-workers` with a `clef*`
  model, and a `GenericClient` for every other `cloudflare-workers` model
- **a permanent budget regression test** that rebuilds every judge request at its
  documented worst case through the real builders and *asserts* the marshalled
  size stays inside the effective budget. This is what makes the measurement
  table above reproducible from the tree, and it fails loudly if a future change
  grows a judge's payload past what any backend will accept. This subsumes the
  throwaway harness the original measurement used.

### Two documented Jev failure modes this work should also address

Surfaced by TypeSafe's own "Jev 1.13 jaggedness" page (last reviewed 2026-10-02).
Both apply to ocode today, independent of which backend is selected, and both are
worth folding into this work because the seam is where they get fixed once:

- **Adversarial content in `state`.** *"State is data, and `jev-1.13` does not
  treat it as hostile by default. Content written to adversarially steer the
  model … can move the answer."* ocode puts tool arguments and candidate
  summaries into judge state, so a file containing prompt-injection text can move
  a verdict. The mitigation is the one already used for secrets — `judgeMaskRegistry`
  redaction in `buildTypesafePermissionState` — plus keeping the state to what
  the question needs.
- **Choice option order bias.** *"we observed that the order of a `Choice`'s
  options can affect the answer, and `jev-1.13` leans toward the option that
  comes first."* The permission judge's `criteria` map is the live instance of
  this. The eval in §5 should score `allow`/`deny` in both orders and report
  disagreement, so an order-sensitive verdict is visible rather than assumed
  away.

Neither is a reason to block this work. Both are reasons the seam is the right
place to fix them.

## Non-goals

- Routing Clef through the chat, compaction or small-model paths.
- Any silent fallback between backends.
- Refactoring `permissions.auto.model` or `auto_continue_model` into a unified
  block.
- Batch API usage (unconfirmed for this model).
- The `images` input extension.

## Sources

- `https://developers.cloudflare.com/workers-ai/models/clef-flash/` —
  capability, context window, $0.09/M input pricing
- `https://developers.cloudflare.com/workers-ai/models/clef-flash/schema-input.json`
  — question/id/criteria limits, `model` selector pattern, `images` extension
- `https://developers.cloudflare.com/workers-ai/models/clef-flash/schema-output.json`
  — `answers` / `confidence` / `probabilities` / `usage` shape
- `https://huggingface.co/Cloudflare/clef-flash` — "fully compatible with Jev and
  SystemOne", one-logit-per-option softmax construction, Decision Index benchmark
  table, median/p95 latency
- `https://developers.cloudflare.com/workers-ai/platform/limits/` — 300 req/min
  default Text Generation limit
- `https://docs.typesafe.ai/models` — **primary.** Jev pricing ($42/Btok,
  $0.042/Mtok, free output), context limits (*"64k tokens per request; 32k
  tokens for `state` plus the longest question"*), rate limits (100K tokens/s,
  80 req/s), text-only input, and the RLCD calibration claim
- `https://docs.typesafe.ai/model-jaggedness/jev-1.13` — **primary.** Named
  failure modes: large-state accuracy decay, adversarial content in state, choice
  option order bias, literal reading

An earlier draft of this spec cited Jev's price from third-party sites
(jevtypesafeai.com, LLMReference, OpenRouter) and asserted Jev's context was
"far larger" than Clef's. Both have been replaced with the primary TypeSafe
documentation, which contradicts the context claim — see finding 3.