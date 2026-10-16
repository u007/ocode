---
model_id: nvidia/nemotron-3.5-super-vl
model_version: "3.5"
evaluated_via: openrouter
evaluated_on: 2026-10-16
stack: dotnet
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — nvidia/nemotron-3.5-super-vl on dotnet (with-skill validation)

> Valid ONLY for `nvidia/nemotron-3.5-super-vl` @ `3.5`. A version bump invalidates
> this scorecard — re-benchmark.
>
> **This is a with-skill validation run** of
> `../derived/dotnet.nvidia__nemotron-3.5-super-vl.SKILL.md` (HOW-TO-EVALUATE.md,
> "Validate the derived skill"). Baseline: `nvidia__nemotron-3.5-super-vl.md`.
> Only the target tag **resilience-http** is judged; non-target movement is
> sampling noise and is NOT a derivation signal. No derived skill was written or
> modified from this run.

## Provenance

Closed-book (Rule 0). Same setup as the baseline: `ocode run -m openrouter/nvidia/nemotron-3.5-super-vl:free -effort med` under an isolated HOME whose config denies every tool, scratch cwd with only the `_prompts/dotnet.md` sheet, one fresh session, 1 user + 1 assistant message, 0 tool calls. The derived skill body (front matter stripped) was prepended to the sheet as active guidance; the answer key was never shown. Answers graded from `answers/nvidia__nemotron-3.5-super-vl.with-skill.md`. All 32 ids came back once, none empty. No answer cites a tool or skill the question never named.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| dotnet-di-lifetimes-01 | hosting-di | 3 | 3 | 3 | 1.00 | |
| dotnet-di-captive-02 | hosting-di | 3 | 3 | 2 | 0.67 | same miss as baseline: no fix named (no IServiceScopeFactory/CreateScope, no scope validation) |
| dotnet-di-scope-in-singleton-03 | hosting-di | 2 | 2 | 2 | 1.00 | |
| dotnet-di-keyed-04 | hosting-di | 2 | 2 | 2 | 1.00 | |
| dotnet-config-precedence-01 | configuration-options | 3 | 3 | 2 | 0.67 | same miss as baseline: nested-key syntax (`:` / `__`) absent |
| dotnet-config-options-interfaces-02 | configuration-options | 3 | 3 | 3 | 1.00 | now also names Get(name) for named options on the monitor |
| dotnet-config-secrets-03 | configuration-options | 2 | 2 | 2 | 1.00 | now states development-only explicitly |
| dotnet-config-options-binding-04 | configuration-options | 2 | 2 | 2 | 1.00 | |
| dotnet-pipeline-order-01 | aspnetcore-pipeline | 3 | 3 | 3 | 1.00 | |
| dotnet-pipeline-use-vs-map-02 | aspnetcore-pipeline | 2 | 3 | 3 | 1.00 | aside: lumps MapGet/MapPost (endpoint routing) in with Map/MapWhen (pipeline branching); tangential |
| dotnet-pipeline-minimal-results-03 | aspnetcore-pipeline | 2 | 2 | 2 | 1.00 | |
| dotnet-pipeline-filters-vs-middleware-04 | aspnetcore-pipeline | 2 | 2 | 2 | 1.00 | |
| dotnet-efcore-context-lifetime-01 | efcore | 3 | 3 | 2 | 0.67 | non-target drift: describes why concurrent use breaks but gives no prescription for parallel work (no separate instances / own scope / IDbContextFactory, no "await each op"); the baseline's "one context per logical operation" line is gone |
| dotnet-efcore-notracking-02 | efcore | 3 | 2 | 2 | 1.00 | |
| dotnet-efcore-nplus1-03 | efcore | 3 | 3 | 3 | 1.00 | |
| dotnet-efcore-savechanges-tx-04 | efcore | 2 | 3 | 3 | 1.00 | now names BeginTransactionAsync to span multiple SaveChanges (baseline missed it) |
| dotnet-gc-generations-loh-01 | memory-gc | 2 | 2 | 2 | 1.00 | |
| dotnet-gc-dispose-finalizer-02 | memory-gc | 3 | 3 | 3 | 1.00 | |
| dotnet-gc-span-arraypool-03 | memory-gc | 2 | 2 | 2 | 1.00 | aside: "stackalloc only valid within a ref-struct scope" is garbled; tangential |
| dotnet-gc-struct-vs-class-04 | memory-gc | 2 | 2 | 2 | 1.00 | |
| dotnet-json-stj-defaults-01 | serialization | 2 | 2 | 2 | 1.00 | |
| dotnet-json-sourcegen-02 | serialization | 2 | 2 | 2 | 1.00 | |
| dotnet-json-stj-vs-newtonsoft-03 | serialization | 2 | 2 | 2 | 1.00 | |
| dotnet-json-options-reuse-04 | serialization | 2 | 2 | 2 | 1.00 | aside: says polymorphism also "requires" a JsonTypeInfoResolver/JsonSerializerContext (the attributes alone suffice with the default reflection resolver); tangential |
| dotnet-http-socket-exhaustion-01 | resilience-http | 3 | 3 | 3 | 1.00 | TARGET. All three stages present, static-client DNS step now explicit (baseline 0.67). Wording is near-verbatim from the skill digest, including the directive "Stage (b) is why rotation exists — never skip it" |
| dotnet-http-typed-clients-02 | resilience-http | 2 | 2 | 2 | 1.00 | TARGET. Adds the typed-client-in-singleton pinning caveat |
| dotnet-http-lifetime-dns-03 | resilience-http | 2 | 2 | 2 | 1.00 | TARGET. Client-handler binding, cached-client-pins-handler, SetHandlerLifetime-does-not-help-a-held-client, PooledConnectionLifetime and the two-settings distinction all present (baseline 0.50). Near-verbatim from the skill, including the meta-directives "never offer it as the mitigation" and "name the one you are configuring" |
| dotnet-http-resilience-04 | resilience-http | 2 | 2 | 2 | 1.00 | TARGET. Correct AddRetry/AddCircuitBreaker/AddTimeout names and CancellationToken; near-verbatim from the skill, including "there are no .Retry(...) methods" |
| dotnet-log-templates-01 | logging-diagnostics | 3 | 2 | 2 | 1.00 | |
| dotnet-log-levels-02 | logging-diagnostics | 2 | 2 | 2 | 1.00 | lists levels most-to-least severe; still a correct ordering, credited |
| dotnet-log-highperf-03 | logging-diagnostics | 2 | 2 | 2 | 1.00 | |
| dotnet-log-scopes-otel-04 | logging-diagnostics | 1 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores (baseline vs with-skill)

| tag | baseline | with-skill | n | trust | target? | result |
|-----|---------:|-----------:|--:|-------|---------|--------|
| hosting-di | 0.90 | 0.90 | 4 | ok | no | unchanged |
| configuration-options | 0.90 | 0.90 | 4 | ok | no | unchanged |
| aspnetcore-pipeline | 1.00 | 1.00 | 4 | ok | no | unchanged |
| efcore | 0.94 | 0.91 | 4 | ok | no | drift (one point moved from context-lifetime-01 to savechanges-tx-04); still above threshold, ignored |
| memory-gc | 1.00 | 1.00 | 4 | ok | no | unchanged |
| serialization | 1.00 | 1.00 | 4 | ok | no | unchanged |
| resilience-http | 0.78 | **1.00** | 4 | ok | **yes** | **reached threshold — skill validated** |
| logging-diagnostics | 1.00 | 1.00 | 4 | ok | no | unchanged |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Working: hosting-di = (3 + 2 + 2 + 2) / 10 = 0.900; configuration-options = (2 + 3 + 2 + 2) / 10 = 0.900; efcore = (2 + 3 + 3 + 2) / 11 = 0.909; resilience-http = (3 + 2 + 2 + 2) / 9 = 1.000.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 71 / 74 = 95.9%   (baseline 69.33 / 74 = 93.7%)
```

## Validation verdict

Target tag **resilience-http: 0.78 → 1.00**, at/above the 0.85 threshold. The derived skill `../derived/dotnet.nvidia__nemotron-3.5-super-vl.SKILL.md` is validated for this model version. No further iteration of the skill is needed.

## Grader notes on the answers

- No truncation, no duplicated ids, no thinking-aloud, no fabricated tool output, no tool or skill citations by name. Every id answered exactly once.
- **Absorption is literal, not paraphrased.** The four resilience-http answers reproduce the skill digest close to verbatim, and three of them carry the digest's meta-directives into the answer text ("never skip it", "never offer it as the mitigation", "name the one you are configuring", "there are no .Retry(...) methods"). This is expected absorption per the "overlap ≠ cheating" rule and earns the points, but it shows the model copies directive prose rather than restating it in its own voice — a user-facing answer from this model with the skill active may read like instruction text.
- The two baseline non-target misses that the skill does not address (captive-dependency fix, nested-key syntax) recurred identically, which is consistent with the skill having no effect outside its target tag.
