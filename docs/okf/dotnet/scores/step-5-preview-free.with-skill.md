---
model_id: step-5-preview-free
model_version: "5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-12
stack: dotnet
stack_corpus_rev: 1
threshold: 0.85
validation: with-skill
---

# Scorecard — step-5-preview-free on dotnet (with-skill validation)

> Valid ONLY for `step-5-preview-free` @ `5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers: `dotnet/answers/step-5-preview-free.with-skill.md`,
> produced closed-book with the current derived skill
> `derived/dotnet.step-5-preview-free.SKILL.md` (`dotnet-tuning-step-5-preview-free`)
> prepended to the `_prompts/dotnet.md` sheet as active guidance (session audited:
> 1 user + 1 assistant message, 0 tool calls). Baseline:
> `scores/step-5-preview-free.md`. No new derived skill is produced from this run.

> Target tags for this skill (baseline < 0.85): **resilience-http** (0.78).

Graded against `dotnet/questions.yaml` corpus_rev 1. Grading rule applied (same
strictness as the baseline): a `point` whose concept is genuinely present earns
full credit (minor missing sub-items are noted); a materially wrong claim about
the same construct withholds that point and is marked "(judgment)"; a `partial`
applies only when no `point` is awarded. Wrong asides that do not touch a rubric
point are recorded in the notes column, as the baseline did.

## Provenance

Answerer was `ocode run -m opencode-go/step-5-preview-free` in an empty scratch dir with the derived skill body prepended to the `_prompts/dotnet.md` sheet; the session shows exactly 1 user + 1 assistant message and 0 tool calls. All 32 ids present once, none truncated. Grader: independent re-grade against `questions.yaml` rubrics only; the baseline answers file was consulted solely to calibrate strictness on three questions (log-templates-01, config-precedence-01, gc-struct-vs-class-04).
## History

- Earlier with-skill scorecard: none (first with-skill validation for
  step-5-preview-free on dotnet).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| dotnet-di-lifetimes-01 | hosting-di | 3 | 3 | 3 | 1.00 | |
| dotnet-di-captive-02 | hosting-di | 3 | 3 | 3 | 1.00 | fix given via scope validation (rubric allows either). Asides: says `ValidateOnBuild` was "added in .NET 8" (it dates from .NET Core 3.0) and that `ValidateScopes` must be enabled in Development (it is on by default there); tangential. Cites "a cached factory-created client" as a captive example — skill bleed-through, harmless |
| dotnet-di-scope-in-singleton-03 | hosting-di | 2 | 2 | 2 | 1.00 | |
| dotnet-di-keyed-04 | hosting-di | 2 | 2 | 2 | 1.00 | |
| dotnet-config-precedence-01 | configuration-options | 3 | 3 | 3 | 1.00 | nested-key point: gives `__` for env vars and `:` "on some hosts"; never states `:` as the general config-path separator (minor) |
| dotnet-config-options-interfaces-02 | configuration-options | 3 | 3 | 3 | 1.00 | named-options support of IOptionsSnapshot not mentioned (same as baseline); lifetime/reload/injection split complete |
| dotnet-config-secrets-03 | configuration-options | 2 | 2 | 2 | 1.00 | now states Development-only explicitly (baseline did not) |
| dotnet-config-options-binding-04 | configuration-options | 2 | 2 | 2 | 1.00 | aside: dates `ValidateOnStart` to .NET 8 (it is .NET 6); tangential |
| dotnet-pipeline-order-01 | aspnetcore-pipeline | 3 | 3 | 3 | 1.00 | |
| dotnet-pipeline-use-vs-map-02 | aspnetcore-pipeline | 2 | 3 | 3 | 1.00 | same aside as baseline: attributes the 401 short-circuit to authentication (authorization challenges); tangential |
| dotnet-pipeline-minimal-results-03 | aspnetcore-pipeline | 2 | 2 | 2 | 1.00 | TypedResults typing justified via testing; OpenAPI not mentioned (minor) |
| dotnet-pipeline-filters-vs-middleware-04 | aspnetcore-pipeline | 2 | 2 | 2 | 1.00 | |
| dotnet-efcore-context-lifetime-01 | efcore | 3 | 3 | 3 | 1.00 | IDbContextFactory not named (same as baseline); "separate context per parallel task" + await sequentially covers the point |
| dotnet-efcore-notracking-02 | efcore | 3 | 2 | 2 | 1.00 | |
| dotnet-efcore-nplus1-03 | efcore | 3 | 3 | 3 | 1.00 | |
| dotnet-efcore-savechanges-tx-04 | efcore | 2 | 3 | 3 | 1.00 | aside: invents a "custom HasSql" seeding API (it is `migrationBuilder.Sql`); tangential |
| dotnet-gc-generations-loh-01 | memory-gc | 2 | 2 | 2 | 1.00 | baseline's stackalloc-for-LOH aside is gone; mitigation is ArrayPool/chunking |
| dotnet-gc-dispose-finalizer-02 | memory-gc | 3 | 3 | 3 | 1.00 | aside: "resurrects the object for the finalizer thread" is loose wording for promotion to the f-reachable queue; tangential |
| dotnet-gc-span-arraypool-03 | memory-gc | 2 | 2 | 2 | 1.00 | |
| dotnet-gc-struct-vs-class-04 | memory-gc | 2 | 2 | 2 | 1.00 | boxing point now earned: value type → object/interface, heap box, allocation cost; the baseline's wrong `int[]`-as-`IEnumerable` boxing claim is absent (non-target resample, see drift note) |
| dotnet-json-stj-defaults-01 | serialization | 2 | 2 | 2 | 1.00 | |
| dotnet-json-sourcegen-02 | serialization | 2 | 2 | 2 | 1.00 | wires the context via `TypeInfoResolver` (correct; baseline mutated `JsonSerializerOptions.Default`) |
| dotnet-json-stj-vs-newtonsoft-03 | serialization | 2 | 2 | 2 | 1.00 | baseline's "STJ throws on duplicate keys" aside is gone |
| dotnet-json-options-reuse-04 | serialization | 2 | 2 | 2 | 1.00 | aside: dates `[JsonDerivedType]` to .NET 8 (it shipped in .NET 7); tangential |
| dotnet-http-socket-exhaustion-01 | resilience-http | 3 | 3 | 3 | 1.00 | all three stages present: TIME_WAIT/port exhaustion; static client never re-resolves DNS (stated as its own flaw, the point lost at baseline); factory pools handlers AND rotates on HandlerLifetime for DNS |
| dotnet-http-typed-clients-02 | resilience-http | 2 | 2 | 2 | 1.00 | named = `CreateClient("name")`, typed = `AddHttpClient<T>` transient wrapper; prefers typed for encapsulation/no magic strings/testability. Baseline's "configured-but-unnamed" slip is gone |
| dotnet-http-lifetime-dns-03 | resilience-http | 2 | 2 | 2 | 1.00 | states the client-to-handler binding and that caching a factory client in a singleton/static pins the old handler (the point lost at baseline); SocketsHttpHandler.PooledConnectionLifetime + `SetHandlerLifetime(Timeout.InfiniteTimeSpan)`; distinguishes the two settings. Skill-directive phrasing ("Say it plainly", "Name the one you are configuring") leaked into the answer verbatim — absorption artifact, not a rubric matter |
| dotnet-http-resilience-04 | resilience-http | 2 | 2 | 2 | 1.00 | `AddStandardResilienceHandler` / `AddResilienceHandler` from Microsoft.Extensions.Http.Resilience; CancellationToken flowed, timeouts surface as OperationCanceledException. Baseline's `AddStandardHandler()` and `Handle<HttpRequestException>()` inventions are gone (the answer repeats the skill's negative corrections unprompted) |
| dotnet-log-templates-01 | logging-diagnostics | 3 | 2 | 2 | 1.00 | loses-structure half explicit; eager-vs-deferred formatting present ("formatting is deferred until a sink actually writes the event"), but the "even when the level is disabled" waste is not stated — the baseline had it (minor) |
| dotnet-log-levels-02 | logging-diagnostics | 2 | 2 | 2 | 1.00 | |
| dotnet-log-highperf-03 | logging-diagnostics | 2 | 2 | 2 | 1.00 | sample declares `partial void OrderShipped(ILogger logger, …)` without `static` (the generator requires static when ILogger is a parameter); IsEnabled guard implied, not named. Baseline's `LogLevel.LogLevelInformation` slip is gone; tangential |
| dotnet-log-scopes-otel-04 | logging-diagnostics | 1 | 2 | 2 | 1.00 | aside: "console and EF providers honour [scopes] by default" — the console formatter's IncludeScopes is off by default and EF is not a logging provider; tangential |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | Δ | n | trust | target? |
|-----|---------:|-----------:|---:|--:|-------|---------|
| hosting-di | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| configuration-options | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| aspnetcore-pipeline | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| efcore | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| memory-gc | 0.89 | 1.00 | +0.11 | 4 | ok | no |
| serialization | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| resilience-http | 0.78 | 1.00 | +0.22 | 4 | ok | **yes** |
| logging-diagnostics | 1.00 | 1.00 | 0.00 | 4 | ok | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: hosting-di 10/10; configuration-options 10/10; aspnetcore-pipeline
9/9; efcore 11/11; memory-gc 9/9 (baseline 8/9); serialization 8/8;
resilience-http 9/9 (baseline 7/9); logging-diagnostics 8/8.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 74 / 74 = 100.0%
```

Baseline 71 / 74 = 95.9%. The three baseline losses (socket-exhaustion-01 −1,
lifetime-dns-03 −1, gc-struct-vs-class-04 −1) are all recovered; no new losses.

## Target-tag verdict

| tag | baseline | with-skill | pass (≥ 0.85)? |
|-----|---------:|-----------:|----------------|
| resilience-http | 0.78 | 1.00 | **PASS** |

Every target tag reaches the 0.85 threshold. The skill is validated for
step-5-preview-free @ 5-preview. Both baseline gaps the skill names were closed
in the model's own words: the static-client DNS flaw is stated as a distinct
stage (socket-exhaustion-01), and the client-to-handler binding that makes a
cached factory client go stale is stated explicitly (lifetime-dns-03). The
hallucinated API names recorded at baseline (`AddStandardHandler()`,
`Handle<HttpRequestException>()`) do not recur.

Non-target drift (not acted on): memory-gc 0.89 → 1.00 — the baseline's wrong
"`int[]` passed as `IEnumerable` boxes" claim did not recur on this sample; per
HOW-TO-EVALUATE.md this is a single-sample resample of a non-target tag and is
not evidence about the skill. No non-target tag moved down.

## Grader notes on the answers

- No truncation; every id answered exactly once; no duplicate records this run.
- Absorption is visible but not copy-paste: resilience-http answers follow the
  skill's three-stage structure and twice echo its imperative phrasing
  ("Say it plainly", "Name the one you are configuring", "There is no
  AddStandardHandler()") — expected for a skill that is always in context for
  those tasks; it is the intended overlap, not contamination from the answer key
  (reference answers were never in the prompt and the wording differs from them).
- Recurring minor pattern persists outside the target tag: version-dating slips
  (`ValidateOnBuild` / `ValidateOnStart` / `[JsonDerivedType]` all placed in the
  wrong release) and one invented API (`HasSql`). None cost a rubric point.
