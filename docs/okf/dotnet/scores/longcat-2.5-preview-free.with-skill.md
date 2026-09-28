---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: dotnet
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — longcat-2.5-preview-free on dotnet (with-skill validation, iteration 3)

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump
> invalidates this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN, ITERATION 3 (two samples).** The model
> re-answered the full sheet twice, closed-book
> (`opencode-go/longcat-2.5-preview-free` via `ocode run` from an isolated
> dir), with the **iteration-3** derived tuning skill
> `../derived/dotnet.longcat-2.5-preview-free.SKILL.md`
> (`dotnet-tuning-longcat-2.5-preview-free`, target tag: **resilience-http**)
> prepended to the question sheet as active guidance:
>
> - run A: `../answers/longcat-2.5-preview-free.with-skill.md`
> - run B: `../answers/longcat-2.5-preview-free.with-skill.run2.md`
>
> Both sessions were audited: 1 user + 1 assistant turn, zero tool calls.
> Before grading, the orchestrator fixed id typos and changed nothing else.
> Both files contain all 32 key ids, with none missing or duplicated
> (verified against `questions.yaml`). Grading is independent and uses the
> same strict standard as the baseline and iterations 1–2: a point is awarded
> only where the concept is genuinely and correctly present, a `partial`
> applies only when no `point` is awarded, and a point whose *distinguishing*
> claim is stated wrongly is not awarded. No derived skill is written or
> modified from this run.
>
> Target tag for this skill: **resilience-http**, the baseline's only
> below-0.75 tag. PASS requires **both** runs ≥ 0.75.

## Per-question results

`A` / `B` = awarded points in run A / run B; `nA` / `nB` = normalized.

| id | tags | weight | full | A | nA | B | nB | notes |
|----|------|-------:|-----:|--:|---:|--:|---:|-------|
| dotnet-di-lifetimes-01 | hosting-di | 3 | 3 | 3 | 1.00 | 3 | 1.00 | both: transient/scoped-per-request/singleton correct |
| dotnet-di-captive-02 | hosting-di | 3 | 3 | 2 | 0.67 | 2 | 0.67 | both: capture past lifetime + consequence (A "stale data, and concurrency bugs"; B "never replaced … effectively becomes a singleton … stale state"). Neither gives a fix (no IServiceScopeFactory, no scope validation), same as iterations 1–2 |
| dotnet-di-scope-in-singleton-03 | hosting-di | 2 | 2 | 2 | 1.00 | 2 | 1.00 | both: IServiceScopeFactory + CreateScope/CreateAsyncScope per iteration, resolve from scope.ServiceProvider, dispose |
| dotnet-di-keyed-04 | hosting-di | 2 | 2 | 2 | 1.00 | 2 | 1.00 | both: .NET 8 keyed services, AddKeyed*, [FromKeyedServices]/GetKeyedService. A also names a non-existent `IKeyedService<IService>` (noted, not scored, following the `[JsonSerializerOptions]` precedent) |
| dotnet-config-precedence-01 | configuration-options | 3 | 3 | 2 | 0.67 | 2 | 0.67 | both: last-wins + correct default order incl. user secrets. No nested-key `:` / `__` syntax (same miss as every prior run) |
| dotnet-config-options-interfaces-02 | configuration-options | 3 | 3 | 3 | 1.00 | 3 | 1.00 | both: three lifetimes + reload behaviour, Snapshot not in singletons, Monitor OnChange injectable anywhere (named options absent, as before) |
| dotnet-config-secrets-03 | configuration-options | 2 | 2 | 2 | 1.00 | 2 | 1.00 | both: user secrets outside project / never committed; prod Key Vault / Secrets Manager / env vars |
| dotnet-config-options-binding-04 | configuration-options | 2 | 2 | 2 | 1.00 | 2 | 1.00 | A: AddOptions<T>().Bind().ValidateDataAnnotations() + ValidateOnStart(). B: Configure<T>(section) + ValidateDataAnnotations "or" ValidateOnStart. B presents ValidateOnStart as an alternative rather than a companion, but both APIs and startup fail-fast are stated correctly, so it is awarded |
| dotnet-pipeline-order-01 | aspnetcore-pipeline | 3 | 3 | 3 | 1.00 | 2 | 0.67 | A: "each middleware can act before and after the next" (onion) + order + Routing → AuthN → AuthZ → endpoint. B: order + concrete ordering only; **onion / before-and-after-next() missing** (same drop as iteration 1) |
| dotnet-pipeline-use-vs-map-02 | aspnetcore-pipeline | 2 | 3 | 3 | 1.00 | 3 | 1.00 | both: Use/Run/Map + short-circuit correct |
| dotnet-pipeline-minimal-results-03 | aspnetcore-pipeline | 2 | 2 | 1 | 0.50 | 1 | 0.50 | both: object → JSON + 200 correct. Both again credit Results AND TypedResults alike with type safety, with no TypedResults-vs-IResult distinction (same miss as every prior run). A also says they "avoid reflection-based serialization" (wrong, noted) |
| dotnet-pipeline-filters-vs-middleware-04 | aspnetcore-pipeline | 2 | 2 | 2 | 1.00 | 2 | 1.00 | A: every-request/cross-cutting vs matched-endpoint-only + filters see action parameters and modify results. B: "middleware runs early … before routing, no knowledge of the endpoint" vs filters inside endpoint execution with arguments/model state/result transformation |
| dotnet-efcore-context-lifetime-01 | efcore | 3 | 3 | 3 | 1.00 | 2 | 0.67 | both: scoped per unit of work + not thread-safe (races / corrupted state). A adds "Each concurrent operation needs its own DbContext instance", which earns the separate-instances point **for the first time in any run** (IDbContextFactory still unnamed). B omits it, as before |
| dotnet-efcore-notracking-02 | efcore | 3 | 2 | 2 | 1.00 | 2 | 1.00 | both: change-tracker states drive SaveChanges SQL + AsNoTracking for read-only (no snapshot, faster, less memory) |
| dotnet-efcore-nplus1-03 | efcore | 3 | 3 | 3 | 1.00 | 3 | 1.00 | both: deferred IQueryable vs ToListAsync + navigation-per-child N+1 + Include (A also ThenInclude/projection) |
| dotnet-efcore-savechanges-tx-04 | efcore | 2 | 3 | 2 | 0.67 | 2 | 0.67 | both: single SaveChanges atomic + migrations as versioned/incremental schema evolution. No explicit transaction across multiple SaveChanges (same miss as every prior run) |
| dotnet-gc-generations-loh-01 | memory-gc | 2 | 2 | 2 | 1.00 | 2 | 1.00 | both: generations + LOH ≥85,000 bytes, gen2-only, **not compacted** (B: "fragmentation"). Iteration 2's inverted "requires compacting" claim is gone |
| dotnet-gc-dispose-finalizer-02 | memory-gc | 3 | 3 | 2 | 0.67 | 2 | 0.67 | both: deterministic Dispose vs non-deterministic finalizer + IAsyncDisposable. Cost/SuppressFinalize point not awarded. A near-miss: "add a finalizer only when you directly hold unmanaged resources" covers one of the point's three sub-claims but not its core (finalizer cost / extra GC survival, SuppressFinalize) |
| dotnet-gc-span-arraypool-03 | memory-gc | 2 | 2 | 2 | 1.00 | 2 | 1.00 | both: Span allocation-free view, stackalloc no heap/GC, ArrayPool rent/return |
| dotnet-gc-struct-vs-class-04 | memory-gc | 2 | 2 | 2 | 1.00 | 2 | 1.00 | both: value vs reference allocation + boxing allocates on the heap |
| dotnet-json-stj-defaults-01 | serialization | 2 | 2 | 2 | 1.00 | 2 | 1.00 | both: JsonNamingPolicy.CamelCase + [JsonPropertyName]. The fake `[JsonSerializerOptions]` attribute seen earlier is absent from both |
| dotnet-json-sourcegen-02 | serialization | 2 | 2 | 2 | 1.00 | 2 | 1.00 | both: compile-time generation instead of reflection + perf + trim/AOT (JsonSerializerContext not named, as before) |
| dotnet-json-stj-vs-newtonsoft-03 | serialization | 2 | 2 | 2 | 1.00 | 2 | 1.00 | both: STJ built-in/faster/AOT but fewer features/more manual config vs Newtonsoft feature-rich (B: reference loops, converters) |
| dotnet-json-options-reuse-04 | serialization | 2 | 2 | 2 | 1.00 | 2 | 1.00 | both: create once / reuse (A "construction expensive", B "builds internal caches") + [JsonPolymorphic]/[JsonDerivedType]. Declared-type default not stated (as before) |
| dotnet-http-socket-exhaustion-01 | resilience-http | 3 | 3 | 3 | 1.00 | 3 | 1.00 | **both runs reproduce the skill's three must-include sentences verbatim**: TIME_WAIT port exhaustion / "one shared static HttpClient avoids exhaustion but never re-resolves DNS … stale IPs after a failover" / "IHttpClientFactory solves BOTH: pools HttpMessageHandlers … rotates them on HandlerLifetime (default 2 min), so DNS changes are picked up". All three points, up from 1/3 in iterations 1–2 |
| dotnet-http-typed-clients-02 | resilience-http | 2 | 2 | 2 | 1.00 | 2 | 1.00 | A: `AddHttpClient("name")` + `CreateClient("name")` vs `AddHttpClient<MyClient>()`; typed preferred (testable, encapsulated). B: named = registered by string key via `AddHttpClient("name", …)` vs typed `AddHttpClient<TClient>()` + testability/API surface. B never says `CreateClient("name")`. It is still awarded because the named-registration vs typed-class distinction the point asks for is present. If denied, B's tag would be 8/9 = 0.89 and the verdict would not change |
| dotnet-http-lifetime-dns-03 | resilience-http | 2 | 2 | 2 | 1.00 | 2 | 1.00 | both: "An HttpClient is bound to the handler it was created with … caching a factory client in a singleton/static field pins the old handler and defeats rotation" + SocketsHttpHandler.PooledConnectionLifetime + SetHandlerLifetime(InfiniteTimeSpan); separate settings (skill digest item 2, verbatim) |
| dotnet-http-resilience-04 | resilience-http | 2 | 2 | 2 | 1.00 | 2 | 1.00 | both: Microsoft.Extensions.Http.Resilience + AddStandardResilienceHandler (retry/backoff, total + per-attempt timeout, circuit breaker) + AddResilienceHandler + CancellationToken flow; fabricated Polly helpers explicitly disowned |
| dotnet-log-templates-01 | logging-diagnostics | 3 | 2 | 2 | 1.00 | 2 | 1.00 | both: template + values captured separately, queryable by field + no formatting cost when level disabled |
| dotnet-log-levels-02 | logging-diagnostics | 2 | 2 | 2 | 1.00 | 2 | 1.00 | both: correct order incl. None + Logging:LogLevel Default + per-namespace override (A also AddFilter) |
| dotnet-log-highperf-03 | logging-diagnostics | 2 | 2 | 2 | 1.00 | 2 | 1.00 | both: [LoggerMessage] compile-time generated method (B: pre-parsed template) + avoids boxing/params/parsing. A also says the generated code "skips the check for whether the log level is enabled". That is wrong: it guards IsEnabled. It is awarded anyway because the point's distinguishing claim (avoids per-call allocation/boxing) is stated correctly and the IsEnabled guard is parenthetical in the key. That differs from iteration 2's LOH case, where the wrong claim *was* the distinguishing one (non-compaction) |
| dotnet-log-scopes-otel-04 | logging-diagnostics | 1 | 2 | 2 | 1.00 | 2 | 1.00 | both: BeginScope correlation + ActivitySource/Activity and Meter exported via OpenTelemetry (iteration 2's miss recovered). A adds "OTel SDK … create spans from log scopes", which is wrong but outside the rubric point (noted) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

| tag | baseline | iter 1 | iter 2 | iter 3-A | iter 3-B | iter 3-mean | n | trust | action |
|-----|---------:|-------:|-------:|---------:|---------:|------------:|--:|-------|--------|
| hosting-di | 1.00 | 0.90 | 0.90 | 0.90 | 0.90 | 0.90 | 4 | ok | omit (strong) |
| configuration-options | 0.80 | 0.90 | 0.90 | 0.90 | 0.90 | 0.90 | 4 | ok | omit (strong) |
| aspnetcore-pipeline | 0.89 | 0.78 | 0.89 | 0.89 | 0.78 | 0.83 | 4 | ok | omit (strong); non-target drift in B |
| efcore | 0.85 | 0.85 | 0.85 | 0.94 | 0.85 | 0.89 | 4 | ok | omit (strong); non-target drift in A (up) |
| memory-gc | 0.89 | 0.89 | 0.78 | 0.89 | 0.89 | 0.89 | 4 | ok | omit (strong) |
| serialization | 1.00 | 1.00 | 1.00 | 1.00 | 1.00 | 1.00 | 4 | ok | omit (strong) |
| resilience-http | 0.67 | 0.67 | 0.78 | **1.00** | **1.00** | **1.00** | 4 | ok | **validation target, PASSED (both runs)** |
| logging-diagnostics | 1.00 | 1.00 | 0.94 | 1.00 | 1.00 | 1.00 | 4 | ok | omit (strong) |

Weighted sums (Σ normalized×weight / Σ weight):
hosting-di A 9/10, B 9/10; configuration-options A 9/10, B 9/10;
aspnetcore-pipeline A 8/9, B 7/9; efcore A 10.33/11, B 9.33/11;
memory-gc A 8/9, B 8/9; serialization A 8/8, B 8/8;
resilience-http A 9/9, B 9/9; logging-diagnostics A 8/8, B 8/8.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight)
  iteration 3, run A = 69.33 / 74 ≈ 94%
  iteration 3, run B = 67.33 / 74 ≈ 91%
  iteration 3, mean  ≈ 92%
  iteration 2        = 64.83 / 74 ≈ 88%
  iteration 1        = 64.33 / 74 ≈ 87%
  baseline           = 65.33 / 74 ≈ 88%
```

## Target tags

| tag | baseline | iter 1 | iter 2 | iter 3-A | iter 3-B | verdict |
|-----|---------:|-------:|-------:|---------:|---------:|---------|
| resilience-http | 0.67 | 0.67 | 0.78 | 1.00 | 1.00 | **PASS** (both runs ≥ 0.75) |

Per question (baseline → iter 1 → iter 2 → iter 3-A / iter 3-B):

| id | baseline | iter 1 | iter 2 | iter 3-A | iter 3-B |
|----|---------:|-------:|-------:|---------:|---------:|
| dotnet-http-socket-exhaustion-01 (w3) | 0.67 | 0.33 | 0.33 | 1.00 | 1.00 |
| dotnet-http-typed-clients-02 (w2) | 1.00 | 1.00 | 1.00 | 1.00 | 1.00 |
| dotnet-http-lifetime-dns-03 (w2) | 0.50 | 0.50 | 1.00 | 1.00 | 1.00 |
| dotnet-http-resilience-04 (w2) | 0.50 | 1.00 | 1.00 | 1.00 | 1.00 |

**What changed.** Digest item 1 is now three must-include sentences, and both
runs absorbed it. Iterations 1–2 had phrased it as an answer-shape directive
("walk through all three options … never leave it out"), and the model
paraphrased that away twice. With concrete sentences to include, both samples
reproduced them verbatim, so socket-exhaustion-01 went from 0.33 to 1.00. The
static-client middle ground appeared for the first time in any run. Item 2
(lifetime-dns-03) and item 3 (resilience-04) held at 1.00 in both samples.

**How robust the PASS is.** The pass is no longer marginal. Both independent
samples score 1.00 on the tag, and the one soft award (B typed-clients-02,
missing `CreateClient("name")`) cannot move the verdict: denying it gives
0.89. The cost is that the target answers are now near-verbatim copies of the
skill digest, not the model's own reasoning. For a force-injected production
skill that is acceptable, since the skill is always present for these tasks.
It does mean the tag measures absorption of the digest wording. The skill is
**validated** for `longcat-2.5-preview-free` @ `2.5-preview` on dotnet.

## Non-target drift

Noted, not acted on, per HOW-TO-EVALUATE's "only trust TARGET-tag movement":

- **aspnetcore-pipeline B 0.78** (A 0.89). pipeline-order-01 in run B dropped
  the onion / before-and-after-next() model, the same single-sample drop as
  iteration 1. Still ≥ 0.75.
- **efcore A 0.94** (B 0.85, the long-standing value). Run A's
  context-lifetime-01 said "each concurrent operation needs its own DbContext
  instance", earning the separate-instances point for the first time. Upward
  resampling, not skill-driven.
- **memory-gc back to 0.89** in both runs. Iteration 2's inverted "LOH
  requires compacting" claim is gone, and both runs state that the LOH is not
  compacted.
- **logging-diagnostics back to 1.00** in both runs. scopes-otel-04 again
  names ActivitySource/Activity + Meter.
- Persistent known misses, unchanged: captive-02 fix, `:`/`__` keys,
  TypedResults typing, BeginTransaction, SuppressFinalize/finalizer cost,
  IDbContextFactory (named in no run).

Answer length: run A 2,411 words, run B 2,080 (file `wc -w`), vs about 1,990
in the baseline and iteration 2. Run A's extra length comes mainly from
longer non-target answers, not from the skill copy.

## Derivation targets

None written; this is a validation run. **resilience-http** is ≥ 0.75 in both
iteration-3 samples (1.00 / 1.00). The skill is **validated** for
`longcat-2.5-preview-free` @ `2.5-preview` on dotnet.

## Iteration history

- **Iteration 1** (original skill): resilience-http 0.67 → 0.67, **FAIL**.
  A gain and a loss cancelled. resilience-04 went 0.50 → 1.00 (the
  AddStandardResilienceHandler fix landed), while socket-01 went 0.67 → 0.33
  (no static-client middle ground, and the factory's DNS rotation dropped
  out). lifetime-dns-03 stayed at 0.50: the model copied the skill's
  PooledConnectionLifetime *recipe* bullets but not the *mechanism* bullets.
  Stack 87%.
- **Skill change for iteration 2:** digest items 1–2 were rewritten from
  facts and coding rules into answer-shape directives. Item 1 says to walk
  through all three options (per-request / static / factory) and each one's
  failure whenever explaining HttpClient lifetime or the factory. Item 2
  says to state in words "an HttpClient is bound to the handler it was
  created with … caching a factory client in a singleton/static field …
  defeats rotation" before giving the PooledConnectionLifetime alternative.
- **Iteration 2:** resilience-http 0.67 → 0.78, **PASS (marginal, one
  sample)**. Item 2 landed and lifted lifetime-dns-03 to 1.00. Item 1 still
  did not land, and socket-01 stayed at 0.33. Stack 88%.
- **Skill change for iteration 3:** digest item 1 was rewritten again, from
  an answer-shape directive into three literal must-include sentences:
  per-request TIME_WAIT exhaustion; the static client avoids exhaustion but
  goes stale on DNS; the factory pools and rotates handlers on
  `HandlerLifetime`, which solves both. A note says to state the static step
  even when only per-request vs factory is asked. Items 2–3 unchanged.
- **Iteration 3** (this run, two samples): resilience-http 1.00 / 1.00,
  **PASS in both runs**. The must-include sentences were reproduced verbatim
  in both samples, and socket-01 went 0.33 → 1.00. Stack 94% / 91%. Lesson:
  for this model, literal sentences to include got absorbed where
  answer-shape directives were not.

## Contamination check

No leak of the answer key. The target answers track the *skill's* wording,
not the key's: "stale IPs after a failover", "solves BOTH", "default 2 min",
"pins the old handler and defeats rotation", "AddRetryPolicy, AddTimeoutPolicy
and AddCircuitBreakerPolicy do not exist", "HttpContext.RequestAborted or a
linked CancellationTokenSource". All of these are digest phrasings that the
key does not use. This overlap is expected (absorption, per HOW-TO-EVALUATE).
Items that exist only in the key are still missing across both samples:
`:`/`__` nested keys, BeginTransaction, SuppressFinalize, TypedResults typing,
IDbContextFactory, declared-type JSON default, and JsonSerializerContext. The
answers also contain errors absent from the key: A's `IKeyedService<IService>`,
"skips the IsEnabled check", "spans from log scopes", and "TypedResults avoid
reflection-based serialization". Both samples are consistent with genuine
closed-book runs with the skill active.
