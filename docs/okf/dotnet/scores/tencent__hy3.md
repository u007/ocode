---
model_id: tencent/hy3
model_version: "3.0"
evaluated_via: novita
evaluated_on: 2026-07-11
stack: dotnet
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — tencent/hy3 on dotnet

> Valid ONLY for `tencent/hy3` @ `3.0`. A version bump invalidates
> this scorecard — re-benchmark.

Graded closed-book (answers produced with no corpus access) by an independent
grader (Claude Opus 4.8, 1M). Strict grading: a rubric point is awarded only when
its concept is genuinely and correctly present; omissions and wrong statements
score 0.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| dotnet-di-lifetimes-01 | hosting-di | 3 | 3 | 3 | 1.00 | transient/scoped/singleton all correct |
| dotnet-di-captive-02 | hosting-di | 3 | 3 | 3 | 1.00 | capture + cross-request sharing + scope-validation catch |
| dotnet-di-scope-in-singleton-03 | hosting-di | 2 | 2 | 2 | 1.00 | IServiceScopeFactory + per-iteration CreateScope/dispose |
| dotnet-di-keyed-04 | hosting-di | 2 | 2 | 2 | 1.00 | keyed services .NET 8, [FromKeyedServices]/GetKeyedService |
| dotnet-config-precedence-01 | configuration-options | 3 | 3 | 2 | 0.67 | missed nested-key syntax (`:` / `__` in env var names) |
| dotnet-config-options-interfaces-02 | configuration-options | 3 | 3 | 3 | 1.00 | lifetimes/reloads correct; snapshot "injectable in scoped/transient" implies not-in-singleton; named-options omitted (minor) |
| dotnet-config-secrets-03 | configuration-options | 2 | 2 | 2 | 1.00 | user secrets out-of-repo + prod env/Key Vault |
| dotnet-config-options-binding-04 | configuration-options | 2 | 2 | 2 | 1.00 | Bind/Configure + ValidateDataAnnotations + ValidateOnStart |
| dotnet-pipeline-order-01 | aspnetcore-pipeline | 3 | 3 | 3 | 1.00 | two-pass chain + routing→auth→endpoint ordering |
| dotnet-pipeline-use-vs-map-02 | aspnetcore-pipeline | 2 | 3 | 3 | 1.00 | Use/Run/Map + short-circuit all correct |
| dotnet-pipeline-minimal-results-03 | aspnetcore-pipeline | 2 | 2 | 2 | 1.00 | auto-serialization + TypedResults typing/OpenAPI |
| dotnet-pipeline-filters-vs-middleware-04 | aspnetcore-pipeline | 2 | 2 | 2 | 1.00 | HttpContext-vs-endpoint scope + filters see bound args/result |
| dotnet-efcore-context-lifetime-01 | efcore | 3 | 3 | 3 | 1.00 | scoped default + not-thread-safe + separate context per op |
| dotnet-efcore-notracking-02 | efcore | 3 | 3 | 3 | 1.00 | change tracker + AsNoTracking read-only rationale |
| dotnet-efcore-nplus1-03 | efcore | 3 | 3 | 3 | 1.00 | deferred IQueryable + N+1 + Include/projection fix |
| dotnet-efcore-savechanges-tx-04 | efcore | 2 | 3 | 2 | 0.67 | atomic SaveChanges + migrations correct; did not state you can wrap multiple SaveChanges in an explicit BeginTransaction |
| dotnet-gc-generations-loh-01 | memory-gc | 2 | 2 | 2 | 1.00 | gen0/1/2 + LOH ≥85KB, gen2-only, non-compacted |
| dotnet-gc-dispose-finalizer-02 | memory-gc | 3 | 3 | 3 | 1.00 | Dispose vs finalizer + only-for-unmanaged + IAsyncDisposable; did not name GC.SuppressFinalize/extra-GC cost (minor) |
| dotnet-gc-span-arraypool-03 | memory-gc | 2 | 2 | 2 | 1.00 | Span/stackalloc/ArrayPool each with what it avoids |
| dotnet-gc-struct-vs-class-04 | memory-gc | 2 | 2 | 2 | 1.00 | value/reference allocation + boxing |
| dotnet-json-stj-defaults-01 | serialization | 2 | 2 | 2 | 1.00 | CamelCase policy + [JsonPropertyName] |
| dotnet-json-sourcegen-02 | serialization | 2 | 2 | 2 | 1.00 | compile-time metadata + AOT/trim safety |
| dotnet-json-stj-vs-newtonsoft-03 | serialization | 2 | 2 | 2 | 1.00 | STJ perf/AOT/strict vs Newtonsoft feature-rich |
| dotnet-json-options-reuse-04 | serialization | 2 | 2 | 2 | 1.00 | metadata cache reuse + [JsonDerivedType] discriminator |
| dotnet-http-socket-exhaustion-01 | resilience-http | 3 | 3 | 2 | 0.67 | TIME_WAIT exhaustion + factory pool/rotate correct; omitted the "single static HttpClient avoids exhaustion but ignores DNS changes" middle step |
| dotnet-http-typed-clients-02 | resilience-http | 2 | 2 | 2 | 1.00 | named vs typed + prefer-typed reasons |
| dotnet-http-lifetime-dns-03 | resilience-http | 2 | 2 | 2 | 1.00 | handler-lifetime staleness + SocketsHttpHandler.PooledConnectionLifetime; did not name the cached-in-singleton failure mode (minor) |
| dotnet-http-resilience-04 | resilience-http | 2 | 2 | 2 | 1.00 | Http.Resilience/Polly retry-timeout-CB + CancellationToken |
| dotnet-log-templates-01 | logging-diagnostics | 3 | 2 | 2 | 1.00 | structured template + eager-format/suppressed-level cost |
| dotnet-log-levels-02 | logging-diagnostics | 2 | 2 | 2 | 1.00 | level order + per-category Logging:LogLevel filtering |
| dotnet-log-highperf-03 | logging-diagnostics | 2 | 2 | 2 | 1.00 | [LoggerMessage] cached method + no parse/box/alloc + IsEnabled |
| dotnet-log-scopes-otel-04 | logging-diagnostics | 1 | 2 | 2 | 1.00 | BeginScope context + Activity/Meter/OpenTelemetry |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| hosting-di | 1.00 | 4 | ok | omit (strong) |
| configuration-options | 0.90 | 4 | ok | omit (strong) |
| aspnetcore-pipeline | 1.00 | 4 | ok | omit (strong) |
| efcore | 0.94 | 4 | ok | omit (strong) |
| memory-gc | 1.00 | 4 | ok | omit (strong) |
| serialization | 1.00 | 4 | ok | omit (strong) |
| resilience-http | 0.89 | 4 | ok | omit (strong) |
| logging-diagnostics | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 71.33 / 74 = 96%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none** — every tag scores ≥ 0.89, so no
`derived/dotnet.tencent__hy3.SKILL.md` is produced (no derivation). The three
docked points (config precedence nested-key syntax, EF multi-SaveChanges explicit
transaction, HttpClient static-client DNS tradeoff) are isolated omissions, not a
weak area of the stack.
