---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-05
stack: dotnet
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — longcat-2.5-preview-free on dotnet (full re-run 2026-10-05)

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump invalidates
> this scorecard — re-benchmark. Baseline re-run of 2026-10-05, closed-book answers
> from `answers/longcat-2.5-preview-free.rerun-2026-10-05.md`. An earlier 2026-09-28
> scorecard exists beside this one and was not consulted.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| dotnet-di-lifetimes-01 | hosting-di | 3 | 3 | 3 | 1.00 | |
| dotnet-di-captive-02 | hosting-di | 3 | 3 | 2 | 0.67 | no IServiceScopeFactory fix |
| dotnet-di-scope-in-singleton-03 | hosting-di | 2 | 2 | 2 | 1.00 | |
| dotnet-di-keyed-04 | hosting-di | 2 | 2 | 2 | 1.00 | |
| dotnet-config-precedence-01 | configuration-options | 3 | 3 | 2 | 0.67 | missed ':' / '__' nested keys |
| dotnet-config-options-interfaces-02 | configuration-options | 3 | 3 | 3 | 1.00 | |
| dotnet-config-secrets-03 | configuration-options | 2 | 2 | 2 | 1.00 | |
| dotnet-config-options-binding-04 | configuration-options | 2 | 2 | 2 | 1.00 | |
| dotnet-pipeline-order-01 | aspnetcore-pipeline | 3 | 3 | 3 | 1.00 | |
| dotnet-pipeline-use-vs-map-02 | aspnetcore-pipeline | 2 | 3 | 3 | 1.00 | |
| dotnet-pipeline-minimal-results-03 | aspnetcore-pipeline | 2 | 2 | 2 | 1.00 | |
| dotnet-pipeline-filters-vs-middleware-04 | aspnetcore-pipeline | 2 | 2 | 2 | 1.00 | |
| dotnet-efcore-context-lifetime-01 | efcore | 3 | 3 | 2 | 0.67 | no separate instances / IDbContextFactory for parallel work |
| dotnet-efcore-notracking-02 | efcore | 3 | 2 | 2 | 1.00 | |
| dotnet-efcore-nplus1-03 | efcore | 3 | 3 | 3 | 1.00 | |
| dotnet-efcore-savechanges-tx-04 | efcore | 2 | 3 | 2 | 0.67 | no explicit transaction across multiple SaveChanges |
| dotnet-gc-generations-loh-01 | memory-gc | 2 | 2 | 2 | 1.00 | LOH non-compaction not stated; point credited |
| dotnet-gc-dispose-finalizer-02 | memory-gc | 3 | 3 | 3 | 1.00 | |
| dotnet-gc-span-arraypool-03 | memory-gc | 2 | 2 | 2 | 1.00 | |
| dotnet-gc-struct-vs-class-04 | memory-gc | 2 | 2 | 2 | 1.00 | |
| dotnet-json-stj-defaults-01 | serialization | 2 | 2 | 2 | 1.00 | |
| dotnet-json-sourcegen-02 | serialization | 2 | 2 | 2 | 1.00 | |
| dotnet-json-stj-vs-newtonsoft-03 | serialization | 2 | 2 | 2 | 1.00 | |
| dotnet-json-options-reuse-04 | serialization | 2 | 2 | 2 | 1.00 | declared-type default not stated; point credited |
| dotnet-http-socket-exhaustion-01 | resilience-http | 3 | 3 | 2 | 0.67 | missed static-HttpClient-ignores-DNS |
| dotnet-http-typed-clients-02 | resilience-http | 2 | 2 | 2 | 1.00 | |
| dotnet-http-lifetime-dns-03 | resilience-http | 2 | 2 | 1 | 0.50 | no client-tied-to-handler / singleton-caching point |
| dotnet-http-resilience-04 | resilience-http | 2 | 2 | 2 | 1.00 | |
| dotnet-log-templates-01 | logging-diagnostics | 3 | 2 | 2 | 1.00 | |
| dotnet-log-levels-02 | logging-diagnostics | 2 | 2 | 2 | 1.00 | |
| dotnet-log-highperf-03 | logging-diagnostics | 2 | 2 | 2 | 1.00 | |
| dotnet-log-scopes-otel-04 | logging-diagnostics | 1 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| hosting-di | 0.90 | 4 | ok | omit (strong) |
| configuration-options | 0.90 | 4 | ok | omit (strong) |
| aspnetcore-pipeline | 1.00 | 4 | ok | omit (strong) |
| efcore | 0.85 | 4 | ok | omit (strong) |
| memory-gc | 1.00 | 4 | ok | omit (strong) |
| serialization | 1.00 | 4 | ok | omit (strong) |
| resilience-http | 0.78 | 4 | ok | omit (above threshold, weakest) |
| logging-diagnostics | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 68.33 / 74 = 92.3%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none**. No derived skill needed.
