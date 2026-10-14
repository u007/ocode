---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: dotnet
stack_corpus_rev: 1
threshold: 0.85
validation: with-skill
---

# Scorecard — cat-jam-free on dotnet (with-skill validation)

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers: `dotnet/answers/cat-jam-free.with-skill.md`,
> produced closed-book with the current derived skill
> `derived/dotnet.cat-jam-free.SKILL.md` (`dotnet-tuning-cat-jam-free`)
> prepended to the `_prompts/dotnet.md` sheet as active guidance. Baseline:
> `scores/cat-jam-free.md`. No new derived skill is produced from this run.

> Target tags for this skill (baseline < 0.85): **efcore** (0.848),
> **resilience-http** (0.67).

Graded against `dotnet/questions.yaml` corpus_rev 1 with the same strictness as
the baseline: a `point` whose concept is genuinely present earns full credit
(minor missing sub-items are noted); a materially wrong claim about the same
construct withholds that point; a `partial` applies only when no `point` is
awarded. Wrong asides that do not touch a rubric point are recorded in the notes.

## Provenance

Answerer was `ocode run -m opencode-go/cat-jam-free -effort med` with the SKILL.md
body (front matter stripped) prepended to the `_prompts/dotnet.md` sheet, in an
isolated HOME with every tool denied and a cwd holding only the sheet; the session
sqlite shows exactly 1 user + 1 assistant message and 0 tool calls. All 32 ids
present once, none truncated.

## History

- Earlier with-skill scorecard: none (first with-skill validation for
  cat-jam-free on dotnet).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| dotnet-di-lifetimes-01 | hosting-di | 3 | 3 | 3 | 1.00 | |
| dotnet-di-captive-02 | hosting-di | 3 | 3 | 2 | 0.67 | definition and shared-across-requests/threads consequence present; still no fix (neither IServiceScopeFactory nor scope validation), same loss as baseline |
| dotnet-di-scope-in-singleton-03 | hosting-di | 2 | 2 | 2 | 1.00 | |
| dotnet-di-keyed-04 | hosting-di | 2 | 2 | 2 | 1.00 | |
| dotnet-config-precedence-01 | configuration-options | 3 | 3 | 2 | 0.67 | order and last-wins correct; nested `:` / `__` key syntax still absent (same as baseline) |
| dotnet-config-options-interfaces-02 | configuration-options | 3 | 3 | 3 | 1.00 | named-options support of IOptionsSnapshot not mentioned (same as baseline) |
| dotnet-config-secrets-03 | configuration-options | 2 | 2 | 2 | 1.00 | "Development-only" not stated explicitly (baseline credited the same) |
| dotnet-config-options-binding-04 | configuration-options | 2 | 2 | 2 | 1.00 | |
| dotnet-pipeline-order-01 | aspnetcore-pipeline | 3 | 3 | 3 | 1.00 | |
| dotnet-pipeline-use-vs-map-02 | aspnetcore-pipeline | 2 | 3 | 3 | 1.00 | |
| dotnet-pipeline-minimal-results-03 | aspnetcore-pipeline | 2 | 2 | 2 | 1.00 | aside: "null or void often becomes 204" is loose (a void/Task handler returns 200 with an empty body); object→JSON and IResult control present; tangential |
| dotnet-pipeline-filters-vs-middleware-04 | aspnetcore-pipeline | 2 | 2 | 2 | 1.00 | app-wide vs per-endpoint and parameter/result access present; "before routing / after binding" timing only implied (as baseline), credited |
| dotnet-efcore-context-lifetime-01 | efcore | 3 | 3 | 3 | 1.00 | parallel-work remedy now given: await each op on one context, separate context per concurrent task (the point lost at baseline). IDbContextFactory not named; covered by "separate context per task" |
| dotnet-efcore-notracking-02 | efcore | 3 | 2 | 2 | 1.00 | |
| dotnet-efcore-nplus1-03 | efcore | 3 | 3 | 3 | 1.00 | |
| dotnet-efcore-savechanges-tx-04 | efcore | 2 | 3 | 3 | 1.00 | explicit `BeginTransactionAsync`/`CommitAsync` to span multiple SaveChanges now stated (the point lost at baseline); `migrations add` / `database update` named |
| dotnet-gc-generations-loh-01 | memory-gc | 2 | 2 | 2 | 1.00 | |
| dotnet-gc-dispose-finalizer-02 | memory-gc | 3 | 3 | 2 | 0.67 | deterministic vs safety-net and IAsyncDisposable present; finalizer cost (extra GC survival) and GC.SuppressFinalize still absent (same as baseline) |
| dotnet-gc-span-arraypool-03 | memory-gc | 2 | 2 | 2 | 1.00 | |
| dotnet-gc-struct-vs-class-04 | memory-gc | 2 | 2 | 2 | 1.00 | |
| dotnet-json-stj-defaults-01 | serialization | 2 | 2 | 2 | 1.00 | |
| dotnet-json-sourcegen-02 | serialization | 2 | 2 | 2 | 1.00 | `[JsonSerializable]` not named; JsonSerializerContext + compile-time generation present |
| dotnet-json-stj-vs-newtonsoft-03 | serialization | 2 | 2 | 2 | 1.00 | |
| dotnet-json-options-reuse-04 | serialization | 2 | 2 | 2 | 1.00 | declared-type default not stated (same as baseline); opt-in attributes named, credited |
| dotnet-http-socket-exhaustion-01 | resilience-http | 3 | 3 | 2 | 0.67 | TIME_WAIT/port exhaustion present; factory now pools AND rotates handlers "so DNS stays current" (the factory point lost at baseline, recovered). Static-client stage and its DNS flaw still absent, despite the skill's explicit "never skip stage (b)" |
| dotnet-http-typed-clients-02 | resilience-http | 2 | 2 | 2 | 1.00 | |
| dotnet-http-lifetime-dns-03 | resilience-http | 2 | 2 | 2 | 1.00 | client-to-handler binding and the cached-client-pins-handler consequence now stated (the point lost at baseline); PooledConnectionLifetime + `SetHandlerLifetime(Timeout.InfiniteTimeSpan)`; the two settings distinguished. Wording tracks the skill closely — intended absorption |
| dotnet-http-resilience-04 | resilience-http | 2 | 2 | 2 | 1.00 | `AddStandardResilienceHandler` / `AddResilienceHandler` now named (baseline named only the package) |
| dotnet-log-templates-01 | logging-diagnostics | 3 | 2 | 2 | 1.00 | |
| dotnet-log-levels-02 | logging-diagnostics | 2 | 2 | 2 | 1.00 | "most specific wins" not stated (baseline credited the same) |
| dotnet-log-highperf-03 | logging-diagnostics | 2 | 2 | 2 | 1.00 | |
| dotnet-log-scopes-otel-04 | logging-diagnostics | 1 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | Δ | n | trust | target? |
|-----|---------:|-----------:|---:|--:|-------|---------|
| hosting-di | 0.90 | 0.90 | 0.00 | 4 | ok | no |
| configuration-options | 0.90 | 0.90 | 0.00 | 4 | ok | no |
| aspnetcore-pipeline | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| efcore | 0.848 | 1.00 | +0.15 | 4 | ok | **yes** |
| memory-gc | 0.89 | 0.89 | 0.00 | 4 | ok | no |
| serialization | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| resilience-http | 0.67 | 0.89 | +0.22 | 4 | ok | **yes** |
| logging-diagnostics | 1.00 | 1.00 | 0.00 | 4 | ok | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: hosting-di (3 + 2 + 2 + 2) / 10 = 0.900; configuration-options
(2 + 3 + 2 + 2) / 10 = 0.900; aspnetcore-pipeline 9/9; efcore 11/11 (baseline
9.333/11); memory-gc (2 + 2 + 2 + 2) / 9 = 0.889; serialization 8/8;
resilience-http (2 + 2 + 2 + 2) / 9 = 0.889 (baseline 6/9); logging-diagnostics 8/8.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 70 / 74 = 94.6%
```

Baseline 66.33 / 74 = 89.6%. Recovered: efcore-context-lifetime-01 (+1),
efcore-savechanges-tx-04 (+0.67), http-socket-exhaustion-01 (+1),
http-lifetime-dns-03 (+1). No new losses.

## Target-tag verdict

| tag | baseline | with-skill | pass (≥ 0.85)? |
|-----|---------:|-----------:|----------------|
| efcore | 0.848 | 1.00 | **PASS** |
| resilience-http | 0.67 | 0.89 | **PASS** |

**PASS.** Every target tag reaches the 0.85 threshold. The skill is validated for
cat-jam-free @ alpha.

Residual gap inside a passing target tag: http-socket-exhaustion-01 still omits
stage (b), the shared static HttpClient that fixes exhaustion but never
re-resolves DNS, although the skill lists it as a numbered stage and says "never
skip stage (b)". The model went straight from new-per-request to the factory.
resilience-http passes at 0.89 regardless; if a later run drops it below
threshold, this is the directive to sharpen (e.g. require the answer to name
"static HttpClient" explicitly).

Non-target drift (not acted on): none. Every non-target tag scored exactly its
baseline value, and the non-target losses are the same omissions as at baseline
(captive-dependency fix, nested-key syntax, finalizer cost / SuppressFinalize).

## Grader notes on the answers

- No truncation; every id answered exactly once.
- Absorption is visible in the target answers: the EF Core and HttpClient answers
  carry the skill's remedies (await-or-separate-context, explicit transaction,
  client-to-handler binding, `AddStandardResilienceHandler`) and in places its
  phrasing. This is the intended overlap; the reference answers were never in the
  prompt and the wording differs from them.
- The baseline pattern of stating a problem without its remedy persists outside
  the target tags (captive-02, dispose-finalizer-02), which is expected since the
  skill does not cover them.
