---
model_id: step-5-preview-free
model_version: "5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-12
stack: dotnet
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — step-5-preview-free on dotnet

> Valid ONLY for `step-5-preview-free` @ `5-preview`. A version bump invalidates
> this scorecard — re-benchmark. Closed-book answers from
> `answers/step-5-preview-free.md`.

## Provenance

Answerer was `ocode run -m opencode-go/step-5-preview-free -effort med` in an empty scratch dir with only the `_prompts/dotnet.md` sheet attached; the session sqlite shows exactly 1 user + 1 assistant message and 0 tool calls. A first attempt failed at the provider (429) with no output and was re-run. The model emitted a second `dotnet-di-lifetimes-01` record at the end ("Duplicate check — answered as above"); it was removed before grading (noted in the answers file header) and the first occurrence was graded.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| dotnet-di-lifetimes-01 | hosting-di | 3 | 3 | 3 | 1.00 | |
| dotnet-di-captive-02 | hosting-di | 3 | 3 | 3 | 1.00 | opening definition is inverted ("a service whose lifetime is longer than ... the service that captures it"); the mechanism that follows is correct, so credited. Fix given via scope validation, not IServiceScopeFactory (rubric allows either) |
| dotnet-di-scope-in-singleton-03 | hosting-di | 2 | 2 | 2 | 1.00 | |
| dotnet-di-keyed-04 | hosting-di | 2 | 2 | 2 | 1.00 | |
| dotnet-config-precedence-01 | configuration-options | 3 | 3 | 3 | 1.00 | |
| dotnet-config-options-interfaces-02 | configuration-options | 3 | 3 | 3 | 1.00 | named-options support of IOptionsSnapshot not mentioned; lifetime/reload/injection split complete, credited |
| dotnet-config-secrets-03 | configuration-options | 2 | 2 | 2 | 1.00 | does not say user secrets load only in Development; out-of-repo + not-for-prod present, credited |
| dotnet-config-options-binding-04 | configuration-options | 2 | 2 | 2 | 1.00 | |
| dotnet-pipeline-order-01 | aspnetcore-pipeline | 3 | 3 | 3 | 1.00 | |
| dotnet-pipeline-use-vs-map-02 | aspnetcore-pipeline | 2 | 3 | 3 | 1.00 | aside: attributes the 401 short-circuit to UseAuthentication (it is the authorization middleware that challenges); tangential |
| dotnet-pipeline-minimal-results-03 | aspnetcore-pipeline | 2 | 2 | 2 | 1.00 | |
| dotnet-pipeline-filters-vs-middleware-04 | aspnetcore-pipeline | 2 | 2 | 2 | 1.00 | |
| dotnet-efcore-context-lifetime-01 | efcore | 3 | 3 | 3 | 1.00 | IDbContextFactory not named; "one context per thread/operation" + await sequentially covers the point |
| dotnet-efcore-notracking-02 | efcore | 3 | 2 | 2 | 1.00 | |
| dotnet-efcore-nplus1-03 | efcore | 3 | 3 | 3 | 1.00 | |
| dotnet-efcore-savechanges-tx-04 | efcore | 2 | 3 | 3 | 1.00 | |
| dotnet-gc-generations-loh-01 | memory-gc | 2 | 2 | 2 | 1.00 | aside: suggests stackalloc for large (LOH-sized) buffers, which risks stack overflow; tangential |
| dotnet-gc-dispose-finalizer-02 | memory-gc | 3 | 3 | 3 | 1.00 | |
| dotnet-gc-span-arraypool-03 | memory-gc | 2 | 2 | 2 | 1.00 | |
| dotnet-gc-struct-vs-class-04 | memory-gc | 2 | 2 | 1 | 0.50 | boxing point withheld: defines boxing correctly but then claims passing an `int[]` as `IEnumerable` boxes (arrays are reference types; no box) — materially wrong claim about the same construct |
| dotnet-json-stj-defaults-01 | serialization | 2 | 2 | 2 | 1.00 | |
| dotnet-json-sourcegen-02 | serialization | 2 | 2 | 2 | 1.00 | aside: wires the context by mutating `JsonSerializerOptions.Default` (read-only, throws); tangential |
| dotnet-json-stj-vs-newtonsoft-03 | serialization | 2 | 2 | 2 | 1.00 | aside: says STJ throws on duplicate keys (it does not by default); tangential |
| dotnet-json-options-reuse-04 | serialization | 2 | 2 | 2 | 1.00 | |
| dotnet-http-socket-exhaustion-01 | resilience-http | 3 | 3 | 2 | 0.67 | never states the middle step: a single static HttpClient avoids exhaustion but ignores DNS changes (DNS appears only as a side benefit of factory rotation) |
| dotnet-http-typed-clients-02 | resilience-http | 2 | 2 | 2 | 1.00 | slip: calls a named client "configured-but-unnamed"; the CreateClient("name") mechanics are right |
| dotnet-http-lifetime-dns-03 | resilience-http | 2 | 2 | 1 | 0.50 | explains factory handler rotation and PooledConnectionLifetime, but never says a client is bound to the handler it was created with, so caching a factory client in a singleton/static pins the old handler — the exact "held too long" issue the question asks about |
| dotnet-http-resilience-04 | resilience-http | 2 | 2 | 2 | 1.00 | asides: invents `AddStandardHandler()` beside the real `AddStandardResilienceHandler()`; `HttpPolicyExtensions.Handle<HttpRequestException>()` should be `HandleTransientHttpError()`; tangential |
| dotnet-log-templates-01 | logging-diagnostics | 3 | 2 | 2 | 1.00 | |
| dotnet-log-levels-02 | logging-diagnostics | 2 | 2 | 2 | 1.00 | |
| dotnet-log-highperf-03 | logging-diagnostics | 2 | 2 | 2 | 1.00 | aside: sample uses `LogLevel.LogLevelInformation` (should be `LogLevel.Information`); tangential |
| dotnet-log-scopes-otel-04 | logging-diagnostics | 1 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| hosting-di | 1.00 | 4 | ok | omit (strong) |
| configuration-options | 1.00 | 4 | ok | omit (strong) |
| aspnetcore-pipeline | 1.00 | 4 | ok | omit (strong) |
| efcore | 1.00 | 4 | ok | omit (strong) |
| memory-gc | 0.89 | 4 | ok | omit (strong) |
| serialization | 1.00 | 4 | ok | omit (strong) |
| resilience-http | 0.78 | 4 | ok | **derive** |
| logging-diagnostics | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Working: memory-gc = (2 + 3 + 2 + 1) / 9 = 0.889; resilience-http = (2 + 2 + 1 + 2) / 9 = 0.778.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 71 / 74 = 95.9%
```

## Derivation targets

Tags below threshold (`< 0.85`): **resilience-http** → feed into
`derived/dotnet.step-5-preview-free.SKILL.md`.

## Grader notes on the answers

- No truncation; every id answered once (after the duplicate removal noted above).
- No verbatim overlap with the reference answers. Wording and structure are the model's own, usually longer than the reference, and the two lost points are concepts the reference contains and the answer lacks — consistent with a genuine closed-book run.
- Recurring minor pattern: small API hallucinations in otherwise-correct answers (`AddStandardHandler()`, `LogLevel.LogLevelInformation`, mutating `JsonSerializerOptions.Default`, STJ throwing on duplicate keys). None cost a rubric point; recorded in the notes column.
