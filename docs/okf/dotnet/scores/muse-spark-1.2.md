---
model_id: muse-spark-1.2
model_version: "1.2"
evaluated_via: opencode-go
evaluated_on: 2026-08-20
stack: dotnet
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — muse-spark-1.2 on dotnet

> Valid ONLY for `muse-spark-1.2` @ `1.2`. A version bump invalidates
> this scorecard — re-benchmark.

Graded closed-book (answers produced with no corpus access, via
`ocode2 run -model opencode-go/muse-spark-1.2 -dir /tmp/kaizen-dotnet-answer`)
by an independent grader. Strict grading: a rubric point is awarded only when
its concept is genuinely and correctly present; omissions and wrong
statements score 0.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| dotnet-di-lifetimes-01 | hosting-di | 3 | 3 | 3 | 1.00 | transient/scoped/singleton all correct |
| dotnet-di-captive-02 | hosting-di | 3 | 3 | 3 | 1.00 | capture + shared-across-requests corruption + scope-validation-catches-it fix |
| dotnet-di-scope-in-singleton-03 | hosting-di | 2 | 2 | 2 | 1.00 | IServiceScopeFactory + per-iteration CreateScope/dispose, with code |
| dotnet-di-keyed-04 | hosting-di | 2 | 2 | 2 | 1.00 | keyed services .NET 8, AddKeyedTransient/[FromKeyedServices]/GetRequiredKeyedService |
| dotnet-config-precedence-01 | configuration-options | 3 | 3 | 3 | 1.00 | full 5-source order + user secrets + `__`-for-`:` env naming |
| dotnet-config-options-interfaces-02 | configuration-options | 3 | 3 | 3 | 1.00 | all three lifetimes/reload/injectability correct |
| dotnet-config-secrets-03 | configuration-options | 2 | 2 | 2 | 1.00 | user secrets out-of-repo + Development-only + prod Key Vault/env vars |
| dotnet-config-options-binding-04 | configuration-options | 2 | 2 | 2 | 1.00 | Bind/Configure + ValidateDataAnnotations + ValidateOnStart |
| dotnet-pipeline-order-01 | aspnetcore-pipeline | 3 | 3 | 3 | 1.00 | onion model, order-matters, correct Routing→Auth→Endpoints ordering |
| dotnet-pipeline-use-vs-map-02 | aspnetcore-pipeline | 2 | 3 | 3 | 1.00 | Use/Run/Map + short-circuit all correct, with code |
| dotnet-pipeline-minimal-results-03 | aspnetcore-pipeline | 2 | 2 | 2 | 1.00 | auto-serialization + TypedResults typing/OpenAPI |
| dotnet-pipeline-filters-vs-middleware-04 | aspnetcore-pipeline | 2 | 2 | 2 | 1.00 | HttpContext-vs-endpoint scope + filters see bound args/result |
| dotnet-efcore-context-lifetime-01 | efcore | 3 | 3 | 3 | 1.00 | scoped default + not-thread-safe + IDbContextFactory for parallel work |
| dotnet-efcore-notracking-02 | efcore | 3 | 2 | 2 | 1.00 | change tracker + AsNoTracking read-only rationale |
| dotnet-efcore-nplus1-03 | efcore | 3 | 3 | 3 | 1.00 | deferred IQueryable + N+1 + Include/projection/AsSplitQuery fix |
| dotnet-efcore-savechanges-tx-04 | efcore | 2 | 3 | 3 | 1.00 | atomic SaveChanges + explicit-transaction-for-multiple + migrations |
| dotnet-gc-generations-loh-01 | memory-gc | 2 | 2 | 2 | 1.00 | gen0/1/2 promotion + 85KB LOH threshold/fragmentation |
| dotnet-gc-dispose-finalizer-02 | memory-gc | 3 | 3 | 2 | 0.67 | Dispose-vs-finalizer + IAsyncDisposable correct; never mentions GC.SuppressFinalize |
| dotnet-gc-span-arraypool-03 | memory-gc | 2 | 2 | 2 | 1.00 | Span/stackalloc/ArrayPool allocation-avoidance all correct |
| dotnet-gc-struct-vs-class-04 | memory-gc | 2 | 2 | 2 | 1.00 | struct-vs-class allocation + boxing correct |
| dotnet-json-stj-defaults-01 | serialization | 2 | 2 | 2 | 1.00 | PropertyNamingPolicy + [JsonPropertyName] |
| dotnet-json-sourcegen-02 | serialization | 2 | 2 | 2 | 1.00 | compile-time source gen + AOT/trim safety |
| dotnet-json-stj-vs-newtonsoft-03 | serialization | 2 | 2 | 2 | 1.00 | STJ vs Newtonsoft trade-offs both sides |
| dotnet-json-options-reuse-04 | serialization | 2 | 2 | 1 | 0.50 | reuse-for-caching correct; frames polymorphism as "no discriminator by default" rather than "serializes by declared type by default" |
| dotnet-http-socket-exhaustion-01 | resilience-http | 3 | 3 | 2 | 0.67 | TIME_WAIT/exhaustion + factory pooling correct; omits the static-HttpClient-avoids-exhaustion-but-ignores-DNS middle case |
| dotnet-http-typed-clients-02 | resilience-http | 2 | 2 | 2 | 1.00 | named vs typed + preference reasons |
| dotnet-http-lifetime-dns-03 | resilience-http | 2 | 2 | 1 | 0.50 | gives PooledConnectionLifetime alternative but never states that caching a factory-created client in a singleton is what defeats handler rotation |
| dotnet-http-resilience-04 | resilience-http | 2 | 2 | 2 | 1.00 | Microsoft.Extensions.Http.Resilience + cancellation token flow |
| dotnet-log-templates-01 | logging-diagnostics | 3 | 2 | 2 | 1.00 | structured templates + eager-format-when-disabled waste |
| dotnet-log-levels-02 | logging-diagnostics | 2 | 2 | 2 | 1.00 | correct level sequence + category filtering |
| dotnet-log-highperf-03 | logging-diagnostics | 2 | 2 | 2 | 1.00 | source-gen + boxing/allocation avoidance on hot paths |
| dotnet-log-scopes-otel-04 | logging-diagnostics | 1 | 2 | 2 | 1.00 | BeginScope + Activity/ActivitySource/Meter + OTel |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| hosting-di | 1.00 | 4 | ok | omit (strong) |
| configuration-options | 1.00 | 4 | ok | omit (strong) |
| aspnetcore-pipeline | 1.00 | 4 | ok | omit (strong) |
| efcore | 1.00 | 4 | ok | omit (strong) |
| memory-gc | 0.89 | 4 | ok | omit (strong) |
| serialization | 0.875 | 4 | ok | omit (strong) |
| resilience-http | 0.78 | 4 | ok | omit (strong) |
| logging-diagnostics | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 70.02 / 74 = 95%
```

## Derivation targets

No tag fell below threshold (`< 0.75`); the lowest is `resilience-http` at
0.78. **No derived skill is written for this eval** — every tag already
clears the bar, so there is nothing to correct without wasting prompt/cache
budget on knowledge the model already has.

Recurring gap pattern worth noting for a future re-eval (all still ≥
threshold, so not actioned): muse-spark-1.2 states the primary mechanism
correctly but consistently drops the same secondary/paired detail — the
`GC.SuppressFinalize(this)` half of the dispose pattern
(`dotnet-gc-dispose-finalizer-02`), the middle-ground "static HttpClient
avoids exhaustion but not DNS" case in a three-way contrast
(`dotnet-http-socket-exhaustion-01`), the "caching a client defeats handler
rotation" causal link (`dotnet-http-lifetime-dns-03`), and the
"serializes-by-declared-type-by-default" framing before explaining
opt-in polymorphism (`dotnet-json-options-reuse-04`). None of these push
their tag below 0.75.

## Contamination check

Stack score is high (95%) with four tags at a perfect 1.00, which on its
face resembles the "suspiciously high" pattern the runbook warns about. On
inspection this reads as strong-but-genuine performance on mainstream,
extremely well-documented .NET platform material, not leakage:

- Answers are original prose with the model's own code samples (e.g. its own
  `BackgroundService` example for `dotnet-di-scope-in-singleton-03`, its own
  `[LoggerMessage]` partial-class shape) — not near-verbatim rubric language.
  The rubric's `answer:` fields are terse; the model's answers are longer,
  differently structured, and include extra correct detail the rubric
  doesn't ask for (e.g. `AsSplitQuery` for N+1, `ExecuteInTransactionAsync`
  for cross-SaveChanges transactions).
- The model has the *same specific gaps* as this stack's other two graded
  models on the *same two hardest questions* (`dotnet-gc-dispose-finalizer-02`
  missing `SuppressFinalize`; `dotnet-http-socket-exhaustion-01` missing the
  static-client/DNS middle case) — a copied-answer-key run would not
  reproduce another model's specific omission pattern; independently
  knowledgeable models converging on the same edge-case blind spot is the
  expected signature of genuine (if strong) knowledge, not contamination.
- The topics scoring 1.00 (DI lifetimes, config precedence, middleware
  ordering, EF Core lifetime/N+1) are textbook ASP.NET Core / EF Core
  material covered exhaustively in Microsoft Learn docs and virtually every
  .NET tutorial — plausible for any well-trained model, closed-book.
- Sibling scorecards in this same stack (`mimo-v2.5.md`) score 89% with an
  overlapping set of correct/incorrect questions, so 95% is not an outlier
  jump to 100% — it's consistent with this being a well-documented, easy
  corpus for capable models rather than a contaminated run.

Verdict: no contamination flag warranted; treat 95% as a real (high)
score on well-covered platform knowledge.
