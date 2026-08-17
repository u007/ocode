---
model_id: mimo-v2.5
model_version: "2.5"
evaluated_via: opencode-go
evaluated_on: 2026-08-17
stack: dotnet
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — mimo-v2.5 on dotnet

> Valid ONLY for `mimo-v2.5` @ `2.5`. A version bump invalidates
> this scorecard — re-benchmark.

Graded closed-book (answers produced with no corpus access, via
`ocode run -model opencode-go/mimo-v2.5`) by an independent grader. Strict
grading: a rubric point is awarded only when its concept is genuinely and
correctly present; omissions and wrong statements score 0.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| dotnet-di-lifetimes-01 | hosting-di | 3 | 3 | 3 | 1.00 | transient/scoped/singleton all correct |
| dotnet-di-captive-02 | hosting-di | 3 | 3 | 2 | 0.67 | capture + corruption correct; never names the fix (IServiceScopeFactory/CreateScope) |
| dotnet-di-scope-in-singleton-03 | hosting-di | 2 | 2 | 2 | 1.00 | IServiceScopeFactory + per-iteration CreateScope/dispose |
| dotnet-di-keyed-04 | hosting-di | 2 | 2 | 2 | 1.00 | keyed services .NET 8, [FromKeyedServices]/GetRequiredKeyedService |
| dotnet-config-precedence-01 | configuration-options | 3 | 3 | 1 | 0.33 | order given omits User Secrets entirely; nested-key syntax (`:`/`__`) not mentioned |
| dotnet-config-options-interfaces-02 | configuration-options | 3 | 3 | 3 | 1.00 | all three lifetimes/reload/injectability correct |
| dotnet-config-secrets-03 | configuration-options | 2 | 2 | 2 | 1.00 | user secrets out-of-repo + prod env vars/Key Vault, not-for-prod stated |
| dotnet-config-options-binding-04 | configuration-options | 2 | 2 | 2 | 1.00 | Bind/Configure + ValidateDataAnnotations + ValidateOnStart |
| dotnet-pipeline-order-01 | aspnetcore-pipeline | 3 | 3 | 2 | 0.67 | order/next() correct; omits the two-pass "runs on the way back" onion detail |
| dotnet-pipeline-use-vs-map-02 | aspnetcore-pipeline | 2 | 3 | 3 | 1.00 | Use/Run/Map + short-circuit all correct |
| dotnet-pipeline-minimal-results-03 | aspnetcore-pipeline | 2 | 2 | 2 | 1.00 | auto-serialization + TypedResults typing/OpenAPI |
| dotnet-pipeline-filters-vs-middleware-04 | aspnetcore-pipeline | 2 | 2 | 2 | 1.00 | HttpContext-vs-endpoint scope + filters see bound args/result |
| dotnet-efcore-context-lifetime-01 | efcore | 3 | 3 | 3 | 1.00 | scoped default + not-thread-safe + separate context per op |
| dotnet-efcore-notracking-02 | efcore | 2 | 2 | 2 | 1.00 | change tracker + AsNoTracking read-only rationale |
| dotnet-efcore-nplus1-03 | efcore | 3 | 3 | 3 | 1.00 | deferred IQueryable + N+1 + Include/projection fix |
| dotnet-efcore-savechanges-tx-04 | efcore | 2 | 3 | 3 | 1.00 | atomic SaveChanges + explicit-transaction-for-multiple + migrations |
| dotnet-gc-generations-loh-01 | memory-gc | 2 | 2 | 2 | 1.00 | gen0/1/2 promotion + LOH threshold/fragmentation |
| dotnet-gc-dispose-finalizer-02 | memory-gc | 3 | 3 | 2 | 0.67 | Dispose-vs-finalizer + IAsyncDisposable correct; never mentions GC.SuppressFinalize |
| dotnet-gc-span-arraypool-03 | memory-gc | 2 | 2 | 2 | 1.00 | Span/stackalloc/ArrayPool allocation-avoidance all correct |
| dotnet-gc-struct-vs-class-04 | memory-gc | 2 | 2 | 2 | 1.00 | struct-vs-class allocation + boxing correct |
| dotnet-json-stj-defaults-01 | serialization | 2 | 2 | 2 | 1.00 | PropertyNamingPolicy + [JsonPropertyName] |
| dotnet-json-sourcegen-02 | serialization | 2 | 2 | 2 | 1.00 | compile-time source gen + AOT/trim safety |
| dotnet-json-stj-vs-newtonsoft-03 | serialization | 2 | 2 | 2 | 1.00 | STJ vs Newtonsoft trade-offs both sides |
| dotnet-json-options-reuse-04 | serialization | 2 | 2 | 1 | 0.50 | reuse-for-caching correct; skips "serializes by declared type by default" before explaining opt-in polymorphism |
| dotnet-http-socket-exhaustion-01 | resilience-http | 3 | 3 | 2 | 0.67 | TIME_WAIT/exhaustion + factory pooling correct; omits the static-HttpClient-avoids-exhaustion-but-ignores-DNS middle case |
| dotnet-http-typed-clients-02 | resilience-http | 2 | 2 | 2 | 1.00 | named vs typed + preference reasons |
| dotnet-http-lifetime-dns-03 | resilience-http | 2 | 2 | 1 | 0.50 | gives PooledConnectionLifetime alternative but never states that caching a factory-created client in a singleton is what defeats handler rotation |
| dotnet-http-resilience-04 | resilience-http | 2 | 2 | 2 | 1.00 | Microsoft.Extensions.Http.Resilience + cancellation token flow |
| dotnet-log-templates-01 | logging-diagnostics | 3 | 2 | 2 | 1.00 | structured templates + eager-format-when-disabled waste |
| dotnet-log-levels-02 | logging-diagnostics | 2 | 2 | 2 | 1.00 | correct level sequence (mislabels direction as "most to least" but list itself is correct) + category filtering |
| dotnet-log-highperf-03 | logging-diagnostics | 2 | 2 | 2 | 1.00 | source-gen + boxing/allocation avoidance on hot paths |
| dotnet-log-scopes-otel-04 | logging-diagnostics | 1 | 2 | 2 | 1.00 | BeginScope + Activity/ActivitySource/Meter + OTel |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| hosting-di | 0.90 | 4 | ok | omit (strong) |
| configuration-options | 0.80 | 4 | ok | omit (strong) |
| aspnetcore-pipeline | 0.89 | 4 | ok | omit (strong) |
| efcore | 1.00 | 4 | ok | omit (strong) |
| memory-gc | 0.89 | 4 | ok | omit (strong) |
| serialization | 0.875 | 4 | ok | omit (strong) |
| resilience-http | 0.78 | 4 | ok | omit (strong) |
| logging-diagnostics | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 66.0 / 74 = 89%
```

## Derivation targets

No tag fell below threshold (`< 0.75`); the lowest is `resilience-http` at
0.78. **No derived skill is written for this eval** — every tag already
clears the bar, so there is nothing to correct without wasting prompt/cache
budget on knowledge the model already has.

Recurring weak spots worth noting for a future re-eval (all still ≥ threshold,
so not actioned): mimo-v2.5 consistently states the mechanism of a fix but
omits the second-order detail — the concrete remediation API name
(`dotnet-di-captive-02`), the middle-ground alternative in a three-way
contrast (`dotnet-http-socket-exhaustion-01`), or a cleanup/suppression step
paired with the main pattern (`dotnet-gc-dispose-finalizer-02`,
`[JsonPolymorphic]`'s default-behavior framing in
`dotnet-json-options-reuse-04`). None of these push their tag below 0.75.
