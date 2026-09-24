---
model_id: space-bunny-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-09-24
stack: dotnet
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — space-bunny-free on dotnet

> Valid ONLY for `space-bunny-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

Graded closed-book (answers produced with no corpus access, via
`opencode-go/space-bunny-free`, from `_prompts/dotnet.md` only) by an
independent grader. Strict grading: a rubric point is awarded only when its
concept is genuinely and correctly present; omissions and wrong statements
score 0.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| dotnet-di-lifetimes-01 | hosting-di | 3 | 3 | 3 | 1.00 | transient/scoped-per-request/singleton all correct |
| dotnet-di-captive-02 | hosting-di | 3 | 3 | 3 | 1.00 | capture past lifetime + concurrent-access/stale-data consequence + "create a new scope where needed" fix (IServiceScopeFactory not named, but the scope-creation remedy is stated) |
| dotnet-di-scope-in-singleton-03 | hosting-di | 2 | 2 | 2 | 1.00 | IServiceScopeFactory + CreateAsyncScope per iteration, resolve from scope.ServiceProvider, dispose |
| dotnet-di-keyed-04 | hosting-di | 2 | 2 | 2 | 1.00 | keyed services .NET 8, AddKeyed*, GetRequiredKeyedService/[FromKeyedServices] |
| dotnet-config-precedence-01 | configuration-options | 3 | 3 | 2 | 0.67 | correct layering + correct default order incl. user secrets; never mentions nested-key syntax (`:` / `__`) |
| dotnet-config-options-interfaces-02 | configuration-options | 3 | 3 | 3 | 1.00 | all three lifetimes, reload behaviour, and Snapshot-not-in-singleton correct (named options not mentioned) |
| dotnet-config-secrets-03 | configuration-options | 2 | 2 | 2 | 1.00 | user secrets outside project, dev-time only; prod env vars / Key Vault |
| dotnet-config-options-binding-04 | configuration-options | 2 | 2 | 2 | 1.00 | AddOptions.Bind + data-annotation validation + ValidateOnStart |
| dotnet-pipeline-order-01 | aspnetcore-pipeline | 3 | 3 | 2 | 0.67 | registration-order execution + UseRouting/UseAuthentication/UseAuthorization/endpoint ordering correct; omits the onion / "runs again on the way back after next()" two-pass model |
| dotnet-pipeline-use-vs-map-02 | aspnetcore-pipeline | 2 | 3 | 3 | 1.00 | Use/Run/Map + short-circuit all correct |
| dotnet-pipeline-minimal-results-03 | aspnetcore-pipeline | 2 | 2 | 2 | 1.00 | object→JSON auto-conversion, IResult for status control, TypedResults concrete types for inference/metadata |
| dotnet-pipeline-filters-vs-middleware-04 | aspnetcore-pipeline | 2 | 2 | 2 | 1.00 | app-wide vs per-endpoint scope + filters see bound arguments and result |
| dotnet-efcore-context-lifetime-01 | efcore | 3 | 3 | 3 | 1.00 | scoped per request / per unit of work + not thread-safe (tracking state races) + one context per logical operation or async flow (IDbContextFactory not named) |
| dotnet-efcore-notracking-02 | efcore | 3 | 2 | 2 | 1.00 | change tracker states/snapshots + AsNoTracking for read-only, keep tracking for updates |
| dotnet-efcore-nplus1-03 | efcore | 3 | 3 | 3 | 1.00 | deferred IQueryable vs ToListAsync + lazy-loading N+1 + Include/ThenInclude/projection/split query |
| dotnet-efcore-savechanges-tx-04 | efcore | 2 | 3 | 3 | 1.00 | atomic SaveChanges + outer transaction spanning multiple SaveChanges + migrations as versioned schema changes (CLI commands not named) |
| dotnet-gc-generations-loh-01 | memory-gc | 2 | 2 | 2 | 1.00 | gen0/1/2 promotion + LOH ~85 KB, not compacted, fragmentation |
| dotnet-gc-dispose-finalizer-02 | memory-gc | 3 | 3 | 2 | 0.67 | deterministic Dispose vs GC-run finalizer + IAsyncDisposable correct; never mentions GC.SuppressFinalize or the extra-GC-survival cost of finalizable objects ("coordinates it with the finalizer" is too vague) |
| dotnet-gc-span-arraypool-03 | memory-gc | 2 | 2 | 2 | 1.00 | Span view, stackalloc stack buffer, ArrayPool rent/return |
| dotnet-gc-struct-vs-class-04 | memory-gc | 2 | 2 | 2 | 1.00 | value vs reference allocation + boxing allocates a heap copy |
| dotnet-json-stj-defaults-01 | serialization | 2 | 2 | 2 | 1.00 | PropertyNamingPolicy CamelCase (+ JsonSerializerDefaults.Web) + [JsonPropertyName] |
| dotnet-json-sourcegen-02 | serialization | 2 | 2 | 2 | 1.00 | JsonSerializerContext/[JsonSerializable] compile-time metadata + perf + trim/AOT safety |
| dotnet-json-stj-vs-newtonsoft-03 | serialization | 2 | 2 | 2 | 1.00 | STJ built-in/default/perf/AOT vs Newtonsoft converter ecosystem + JObject dynamic APIs; hedges rather than saying STJ is stricter, but the trade-off is present |
| dotnet-json-options-reuse-04 | serialization | 2 | 2 | 2 | 1.00 | metadata cache → reuse one instance + [JsonPolymorphic]/[JsonDerivedType] discriminator |
| dotnet-http-socket-exhaustion-01 | resilience-http | 3 | 3 | 2 | 0.67 | TIME_WAIT / ephemeral-port exhaustion + factory handler pooling/rotation correct; never states the single-static-HttpClient middle ground and its stale-DNS problem |
| dotnet-http-typed-clients-02 | resilience-http | 2 | 2 | 2 | 1.00 | named CreateClient vs typed AddHttpClient<T> + preference reasons |
| dotnet-http-lifetime-dns-03 | resilience-http | 2 | 2 | 1 | 0.50 | SocketsHttpHandler.PooledConnectionLifetime alternative correct; describes 2-minute handler rotation but never says that caching a factory-created client in a singleton is what defeats the rotation |
| dotnet-http-resilience-04 | resilience-http | 2 | 2 | 2 | 1.00 | Microsoft.Extensions.Http.Resilience / AddStandardResilienceHandler + CancellationToken flow |
| dotnet-log-templates-01 | logging-diagnostics | 3 | 2 | 2 | 1.00 | structured named properties + no formatting when level disabled |
| dotnet-log-levels-02 | logging-diagnostics | 2 | 2 | 2 | 1.00 | correct level order + Logging:LogLevel per-category overrides / AddFilter |
| dotnet-log-highperf-03 | logging-diagnostics | 2 | 2 | 2 | 1.00 | [LoggerMessage] generated delegate + avoids per-call parsing/allocation on hot paths |
| dotnet-log-scopes-otel-04 | logging-diagnostics | 1 | 2 | 2 | 1.00 | BeginScope contextual values + Activity/ActivitySource/Meter collected by OpenTelemetry |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| hosting-di | 1.00 | 4 | ok | omit (strong) |
| configuration-options | 0.90 | 4 | ok | omit (strong) |
| aspnetcore-pipeline | 0.89 | 4 | ok | omit (strong) |
| efcore | 1.00 | 4 | ok | omit (strong) |
| memory-gc | 0.89 | 4 | ok | omit (strong) |
| serialization | 1.00 | 4 | ok | omit (strong) |
| resilience-http | 0.78 | 4 | ok | omit (strong) |
| logging-diagnostics | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 69.0 / 74 = 93%
```

## Derivation targets

No tag fell below threshold (`< 0.75`); the lowest is `resilience-http` at
0.78. **No derived skill is written for this eval** — every tag clears the
bar, so there is nothing to correct without wasting prompt/cache budget on
knowledge the model already has.

Recurring weak spot worth noting for a future re-eval (still ≥ threshold, so
not actioned): every point lost is a *secondary* detail omitted from an
otherwise-correct answer, never a wrong statement — nested-key `:`/`__`
syntax (`dotnet-config-precedence-01`), the two-pass onion model
(`dotnet-pipeline-order-01`), `GC.SuppressFinalize` and finalizer cost
(`dotnet-gc-dispose-finalizer-02`), the static-HttpClient/stale-DNS middle
ground (`dotnet-http-socket-exhaustion-01`), and the cached-client-defeats-
rotation failure mode (`dotnet-http-lifetime-dns-03`). The two
`resilience-http` misses are both about the *DNS-staleness* thread of the
HttpClient story; if a re-eval drops that tag under 0.75 that is the section
to write.

Contamination check: no answer reads as a copy of the reference. Wording and
structure differ throughout, several answers carry correct detail absent from
the key (CreateAsyncScope, IKeyedServiceProvider, JsonSerializerDefaults.Web,
DictionaryKeyPolicy, split queries, 408/429 retry statuses, W3C trace
context), and the misses are precisely items the reference states verbatim —
consistent with a genuinely closed-book run.
