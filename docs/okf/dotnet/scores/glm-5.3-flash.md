---
model_id: glm-5.3-flash
model_version: "5.3"
evaluated_via: aihubmix
evaluated_on: 2026-09-01
stack: dotnet
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — glm-5.3-flash on dotnet

> Valid ONLY for `glm-5.3-flash` @ `5.3`. A version bump invalidates
> this scorecard — re-benchmark.

Graded closed-book (answers produced with no corpus access, via
`ocode run -model aihubmix/glm-5.3-flash`) by an independent grader. Strict
grading: a rubric point is awarded only when its concept is genuinely and
correctly present; omissions and wrong statements score 0.

Contamination check: the result is a near-sweep (73/74), which is the
red-flag pattern for an open-book run. The answers were inspected for
key-mirroring and do not match the key's phrasing — they carry detail the
key never mentions (`RequestDelegateFactory`, `IEndpointMetadataProvider`,
`JsonSerializerOptions.MakeReadOnly()`, `PooledConnectionIdleTimeout`,
`GCSettings.LargeObjectHeapCompactionMode`, `AsSplitQuery`) and miss one
concrete key item (`:`/`__` nested-key syntax). Treated as a genuine
closed-book result.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| dotnet-di-lifetimes-01 | hosting-di | 3 | 3 | 3 | 1.00 | transient/scoped/singleton + first-resolve semantics all correct |
| dotnet-di-captive-02 | hosting-di | 3 | 3 | 3 | 1.00 | capture + cross-request sharing/not-thread-safe; fix point awarded via the rubric's "scope validation catches it" alternative — never names IServiceScopeFactory/CreateScope as the remedy |
| dotnet-di-scope-in-singleton-03 | hosting-di | 2 | 2 | 2 | 1.00 | IServiceScopeFactory + per-iteration CreateScope/resolve/dispose |
| dotnet-di-keyed-04 | hosting-di | 2 | 2 | 2 | 1.00 | keyed services .NET 8, AddKeyed*, [FromKeyedServices]/GetRequiredKeyedService |
| dotnet-config-precedence-01 | configuration-options | 3 | 3 | 2 | 0.67 | layering/last-wins + exact default order correct; never mentions nested-key syntax (`:` / `__` in env var names) |
| dotnet-config-options-interfaces-02 | configuration-options | 3 | 3 | 3 | 1.00 | all three lifetimes/reload/injectability correct (named-options support attributed to Monitor only, not Snapshot — minor) |
| dotnet-config-secrets-03 | configuration-options | 2 | 2 | 2 | 1.00 | user secrets out-of-repo + Development-only; prod env vars/Key Vault |
| dotnet-config-options-binding-04 | configuration-options | 2 | 2 | 2 | 1.00 | Configure/AddOptions.Bind + ValidateDataAnnotations + ValidateOnStart |
| dotnet-pipeline-order-01 | aspnetcore-pipeline | 3 | 3 | 3 | 1.00 | two-pass in/out chain, registration = execution order, Routing→Authn→Authz→endpoint |
| dotnet-pipeline-use-vs-map-02 | aspnetcore-pipeline | 2 | 3 | 3 | 1.00 | Use/Run/Map(+MapWhen) + short-circuit all correct |
| dotnet-pipeline-minimal-results-03 | aspnetcore-pipeline | 2 | 2 | 2 | 1.00 | auto-conversion (IResult/string/JSON) + TypedResults typing/OpenAPI metadata |
| dotnet-pipeline-filters-vs-middleware-04 | aspnetcore-pipeline | 2 | 2 | 2 | 1.00 | app-wide/pre-routing vs per-endpoint/post-binding; filters see arguments + result |
| dotnet-efcore-context-lifetime-01 | efcore | 3 | 3 | 3 | 1.00 | scoped/short-lived + not-thread-safe (throws/corrupts) + IDbContextFactory for parallel work |
| dotnet-efcore-notracking-02 | efcore | 3 | 2 | 2 | 1.00 | snapshots/identity/diff at SaveChanges + AsNoTracking read-only rationale and detached trade-off |
| dotnet-efcore-nplus1-03 | efcore | 3 | 3 | 3 | 1.00 | deferred expression tree + N+1 via lazy nav in loop + Include/projection/split query |
| dotnet-efcore-savechanges-tx-04 | efcore | 2 | 3 | 3 | 1.00 | atomic SaveChanges + explicit BeginTransaction for multiple + migrations add/update |
| dotnet-gc-generations-loh-01 | memory-gc | 2 | 2 | 2 | 1.00 | gen0/1/2 promotion + LOH ~85 KB, gen-2-only collection, not compacted by default |
| dotnet-gc-dispose-finalizer-02 | memory-gc | 3 | 3 | 3 | 1.00 | deterministic Dispose vs GC finalizer, extra-GC cost, SafeHandle, GC.SuppressFinalize, IAsyncDisposable/await using |
| dotnet-gc-span-arraypool-03 | memory-gc | 2 | 2 | 2 | 1.00 | Span slice no-copy, stackalloc no-heap, ArrayPool rent/return vs LOH churn |
| dotnet-gc-struct-vs-class-04 | memory-gc | 2 | 2 | 2 | 1.00 | stack/inline vs heap + boxing allocates a heap box |
| dotnet-json-stj-defaults-01 | serialization | 2 | 2 | 2 | 1.00 | PropertyNamingPolicy.CamelCase + [JsonPropertyName] |
| dotnet-json-sourcegen-02 | serialization | 2 | 2 | 2 | 1.00 | compile-time metadata via JsonSerializerContext/[JsonSerializable]; startup + trim/AOT safety |
| dotnet-json-stj-vs-newtonsoft-03 | serialization | 2 | 2 | 2 | 1.00 | STJ perf/AOT/stricter vs Newtonsoft feature-rich/permissive |
| dotnet-json-options-reuse-04 | serialization | 2 | 2 | 2 | 1.00 | per-instance metadata cache → reuse; declared-type default + [JsonPolymorphic]/[JsonDerivedType] discriminator (default-behavior framing slightly hedged: "in some paths") |
| dotnet-http-socket-exhaustion-01 | resilience-http | 3 | 3 | 3 | 1.00 | TIME_WAIT/ephemeral-port exhaustion + static client ignores DNS + factory pools/rotates handlers |
| dotnet-http-typed-clients-02 | resilience-http | 2 | 2 | 2 | 1.00 | named CreateClient("x") vs typed AddHttpClient<T>; preference reasons |
| dotnet-http-lifetime-dns-03 | resilience-http | 2 | 2 | 2 | 1.00 | caching a factory client past HandlerLifetime defeats rotation; SocketsHttpHandler.PooledConnectionLifetime |
| dotnet-http-resilience-04 | resilience-http | 2 | 2 | 2 | 1.00 | Microsoft.Extensions.Http.Resilience/AddStandardResilienceHandler + CancellationToken flow |
| dotnet-log-templates-01 | logging-diagnostics | 3 | 2 | 2 | 1.00 | structured key/value capture + eager-format-when-disabled waste |
| dotnet-log-levels-02 | logging-diagnostics | 2 | 2 | 2 | 1.00 | correct Trace→Critical order + Logging:LogLevel/AddFilter, longest-prefix wins |
| dotnet-log-highperf-03 | logging-diagnostics | 2 | 2 | 2 | 1.00 | source-gen partial method; pre-parsed template, no boxing, IsEnabled guard |
| dotnet-log-scopes-otel-04 | logging-diagnostics | 1 | 2 | 2 | 1.00 | BeginScope ambient context + ActivitySource/Meter collected by OpenTelemetry |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| hosting-di | 1.00 | 4 | ok | omit (strong) |
| configuration-options | 0.90 | 4 | ok | omit (strong) |
| aspnetcore-pipeline | 1.00 | 4 | ok | omit (strong) |
| efcore | 1.00 | 4 | ok | omit (strong) |
| memory-gc | 1.00 | 4 | ok | omit (strong) |
| serialization | 1.00 | 4 | ok | omit (strong) |
| resilience-http | 1.00 | 4 | ok | omit (strong) |
| logging-diagnostics | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 73.0 / 74 = 98.6%
```

## Derivation targets

No tag fell below threshold (`< 0.75`); the lowest is
`configuration-options` at 0.90. **No derived skill is written for this
eval** — every tag clears the bar, so there is nothing to correct without
wasting prompt/cache budget on knowledge the model already has.

Only observed gap (not actionable at this threshold): on
`dotnet-config-precedence-01` the model gave the provider order and
last-wins rule but omitted the nested-key addressing syntax (`Section:Key`
in code/JSON, `Section__Key` in environment-variable names). Where the
model soft-hedged (`[JsonPolymorphic]` declared-type default "in some
paths"; scope validation named as detection rather than
`IServiceScopeFactory` as the fix on `dotnet-di-captive-02`), the rubric's
own alternatives covered the point.
