---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: dotnet
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — longcat-2.5-preview-free on dotnet

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump
> invalidates this scorecard — re-benchmark.

Graded closed-book (answers produced with no corpus access, via
`opencode-go/longcat-2.5-preview-free` through `ocode run` from an isolated
dir, from `_prompts/dotnet.md` only; session audited: 1 user + 1 assistant
turn, zero tool calls) by an independent grader. Strict grading: a rubric
point is awarded only when its concept is genuinely and correctly present;
omissions and wrong statements score 0. Orchestrator housekeeping before
grading: stripped markdown fences; fixed id typo `dotnet-efcore-nplus-03` ->
`dotnet-efcore-nplus1-03`.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| dotnet-di-lifetimes-01 | hosting-di | 3 | 3 | 3 | 1.00 | transient/scoped-per-request/singleton all correct |
| dotnet-di-captive-02 | hosting-di | 3 | 3 | 3 | 1.00 | capture past lifetime + stale data/DbContext concurrency consequence + scope validation throws (IServiceScopeFactory fix not named, rubric accepts scope validation) |
| dotnet-di-scope-in-singleton-03 | hosting-di | 2 | 2 | 2 | 1.00 | IServiceScopeFactory + CreateAsyncScope per iteration, resolve from scope.ServiceProvider, dispose |
| dotnet-di-keyed-04 | hosting-di | 2 | 2 | 2 | 1.00 | keyed services .NET 8, AddKeyed*, [FromKeyedServices]/IKeyedServiceProvider |
| dotnet-config-precedence-01 | configuration-options | 3 | 3 | 2 | 0.67 | last-wins layering + correct default order incl. user secrets; never mentions nested-key syntax (`:` / `__`) |
| dotnet-config-options-interfaces-02 | configuration-options | 3 | 3 | 3 | 1.00 | three lifetimes + reload behaviour correct; Snapshot-not-in-singleton credited from its contrast "Snapshot safe for scoped/transient" vs "Monitor … including singletons"; named options not mentioned |
| dotnet-config-secrets-03 | configuration-options | 2 | 2 | 2 | 1.00 | user secrets dev-only, local file, out of source control; prod Key Vault / Secrets Manager / platform env vars |
| dotnet-config-options-binding-04 | configuration-options | 2 | 2 | 1 | 0.50 | Bind / Configure<T>(section) correct; validation via DataAnnotations/IValidateOptions named but no ValidateOnStart — and it explicitly says validation runs on first resolve, i.e. NOT fail-fast at startup |
| dotnet-pipeline-order-01 | aspnetcore-pipeline | 3 | 3 | 3 | 1.00 | "each middleware can act before and after the next" (onion) + registration order + UseRouting → UseAuthentication → UseAuthorization → endpoint |
| dotnet-pipeline-use-vs-map-02 | aspnetcore-pipeline | 2 | 3 | 3 | 1.00 | Use/Run/Map + short-circuit all correct |
| dotnet-pipeline-minimal-results-03 | aspnetcore-pipeline | 2 | 2 | 1 | 0.50 | object → JSON + 200 auto-conversion correct; claims Results AND TypedResults are both compile-time type-safe — never distinguishes TypedResults' concrete `Ok<T>` types from untyped `IResult` (wrong statement for Results) |
| dotnet-pipeline-filters-vs-middleware-04 | aspnetcore-pipeline | 2 | 2 | 2 | 1.00 | app-wide vs per-endpoint + filters see action arguments/model state that middleware can't |
| dotnet-efcore-context-lifetime-01 | efcore | 3 | 3 | 2 | 0.67 | scoped per request + not thread-safe (InvalidOperationException/corruption) correct; never says to use separate instances / IDbContextFactory for parallel work |
| dotnet-efcore-notracking-02 | efcore | 3 | 2 | 2 | 1.00 | change tracker states for SaveChanges + AsNoTracking for read-only (less memory, faster) |
| dotnet-efcore-nplus1-03 | efcore | 3 | 3 | 3 | 1.00 | deferred IQueryable vs ToListAsync + navigation-in-loop N+1 + Include/explicit load/projection |
| dotnet-efcore-savechanges-tx-04 | efcore | 2 | 3 | 2 | 0.67 | atomic single SaveChanges + migrations as versioned schema evolution (CLI not named); never mentions an explicit transaction (BeginTransaction) to span multiple SaveChanges; typo `SaveChangesChangesAsync` |
| dotnet-gc-generations-loh-01 | memory-gc | 2 | 2 | 2 | 1.00 | gen0/1/2 promotion, gen2 rare/expensive + LOH ≥85,000 bytes collected only with gen2 (not-compacted/fragmentation omitted) |
| dotnet-gc-dispose-finalizer-02 | memory-gc | 3 | 3 | 2 | 0.67 | deterministic Dispose vs GC-run finalizer + IAsyncDisposable correct; no GC.SuppressFinalize, no extra-GC-survival cost, no "finalizer only for directly owned unmanaged handles" |
| dotnet-gc-span-arraypool-03 | memory-gc | 2 | 2 | 2 | 1.00 | Span allocation-free view, stackalloc no GC, ArrayPool rent/return |
| dotnet-gc-struct-vs-class-04 | memory-gc | 2 | 2 | 2 | 1.00 | value vs reference allocation + boxing allocates on heap |
| dotnet-json-stj-defaults-01 | serialization | 2 | 2 | 2 | 1.00 | PropertyNamingPolicy CamelCase + [JsonPropertyName]; stray non-existent "[JsonSerializerOptions] defaults" attribute noted |
| dotnet-json-sourcegen-02 | serialization | 2 | 2 | 2 | 1.00 | compile-time generation instead of reflection + perf + trim/AOT safety (JsonSerializerContext/[JsonSerializable] not named) |
| dotnet-json-stj-vs-newtonsoft-03 | serialization | 2 | 2 | 2 | 1.00 | STJ built-in/fast/AOT but less flexible vs Newtonsoft feature-rich/lenient |
| dotnet-json-options-reuse-04 | serialization | 2 | 2 | 2 | 1.00 | options cache → reuse one instance + [JsonPolymorphic]/[JsonDerivedType]; declared-type-by-default not stated |
| dotnet-http-socket-exhaustion-01 | resilience-http | 3 | 3 | 2 | 0.67 | TIME_WAIT socket exhaustion + factory handler pooling/rotation with DNS refresh; never states the single-static-HttpClient middle ground and its stale-DNS problem |
| dotnet-http-typed-clients-02 | resilience-http | 2 | 2 | 2 | 1.00 | named CreateClient vs typed AddHttpClient<T> + preference reasons |
| dotnet-http-lifetime-dns-03 | resilience-http | 2 | 2 | 1 | 0.50 | PooledConnectionLifetime alternative correct; never says caching a factory client in a singleton defeats handler rotation; conflates the factory's HandlerLifetime rotation with PooledConnectionLifetime |
| dotnet-http-resilience-04 | resilience-http | 2 | 2 | 1 | 0.50 | CancellationToken flow correct; resilience half cites legacy Microsoft.Extensions.Http.Polly with fabricated `AddRetryPolicy`/`AddTimeoutPolicy`/`AddCircuitBreakerPolicy` methods — no Microsoft.Extensions.Http.Resilience / AddStandardResilienceHandler |
| dotnet-log-templates-01 | logging-diagnostics | 3 | 2 | 2 | 1.00 | template + values captured separately (indexable) + no formatting when level disabled |
| dotnet-log-levels-02 | logging-diagnostics | 2 | 2 | 2 | 1.00 | correct level order + LogLevel Default + per-category override |
| dotnet-log-highperf-03 | logging-diagnostics | 2 | 2 | 2 | 1.00 | [LoggerMessage] partial method + no boxing / params array / disabled-level allocation |
| dotnet-log-scopes-otel-04 | logging-diagnostics | 1 | 2 | 2 | 1.00 | BeginScope correlation context + ActivitySource/Meter exported via OpenTelemetry |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| hosting-di | 1.00 | 4 | ok | omit (strong) |
| configuration-options | 0.80 | 4 | ok | omit (strong) |
| aspnetcore-pipeline | 0.89 | 4 | ok | omit (strong) |
| efcore | 0.85 | 4 | ok | omit (strong) |
| memory-gc | 0.89 | 4 | ok | omit (strong) |
| serialization | 1.00 | 4 | ok | omit (strong) |
| resilience-http | 0.67 | 4 | ok | **derive** |
| logging-diagnostics | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 65.33 / 74 = 88%
```

## Derivation targets

Tags below threshold (`< 0.75`): **resilience-http** (0.67) → feed into
`derived/dotnet.longcat-2.5-preview-free.SKILL.md`.

Failure pattern in `resilience-http`: every miss sits on the modern
HttpClient story — the static-client stale-DNS middle ground
(`dotnet-http-socket-exhaustion-01`), cached-factory-client-defeats-rotation
plus a HandlerLifetime/PooledConnectionLifetime conflation
(`dotnet-http-lifetime-dns-03`), and a hallucinated legacy Polly API in place
of `Microsoft.Extensions.Http.Resilience` / `AddStandardResilienceHandler`
(`dotnet-http-resilience-04`, the only fabricated-API answer in the run).

Above-threshold weak spots (not actioned): nested-key `:`/`__` syntax and
`ValidateOnStart` (configuration-options 0.80); explicit transaction across
multiple SaveChanges and IDbContextFactory for parallel work (efcore 0.85);
Results vs TypedResults typing (aspnetcore-pipeline); GC.SuppressFinalize /
finalizer cost (memory-gc).

Contamination check: no leak. Wording and structure differ from the key
throughout; the one close phrase ("Keyed services, introduced in .NET 8.
Register with AddKeyedSingleton/AddKeyedScoped/AddKeyedTransient ... passing
a key") is canonical API naming, and the answer diverges after it
(IKeyedServiceProvider, no .NET 9 note). Misses land precisely on items the
key states verbatim (`:`/`__`, ValidateOnStart, BeginTransaction,
SuppressFinalize, static-client DNS, AddStandardResilienceHandler), and the
answers contain errors absent from the key (fabricated Polly methods,
`[JsonSerializerOptions]` attribute, `SaveChangesChangesAsync`) — consistent
with a genuinely closed-book run.
