---
model_id: nvidia/nemotron-3.5-super-vl
model_version: "3.5"
evaluated_via: openrouter
evaluated_on: 2026-10-16
stack: dotnet
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — nvidia/nemotron-3.5-super-vl on dotnet

> Valid ONLY for `nvidia/nemotron-3.5-super-vl` @ `3.5`. A version bump invalidates
> this scorecard — re-benchmark. Closed-book answers from
> `answers/nvidia__nemotron-3.5-super-vl.md`.

## Provenance

Closed-book (Rule 0). The answerer was `ocode run -m openrouter/nvidia/nemotron-3.5-super-vl:free -effort med` under an isolated HOME whose config denies every tool (no tools were advertised), in a scratch cwd containing only the `_prompts/dotnet.md` sheet, one fresh session; the session sqlite shows exactly 1 user + 1 assistant message and 0 tool calls. All 32 ids came back once, none empty. Headless runs inject the bundled skill-catalogue digest into the system prompt; no answer cites a tool or skill the question never named. The OpenRouter `:free` route variant is not part of the model id.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| dotnet-di-lifetimes-01 | hosting-di | 3 | 3 | 3 | 1.00 | |
| dotnet-di-captive-02 | hosting-di | 3 | 3 | 2 | 0.67 | definition and sharing consequence are right; no fix at all — neither IServiceScopeFactory/CreateScope nor scope validation is mentioned |
| dotnet-di-scope-in-singleton-03 | hosting-di | 2 | 2 | 2 | 1.00 | also offers IDbContextFactory as an alternative |
| dotnet-di-keyed-04 | hosting-di | 2 | 2 | 2 | 1.00 | uses GetKeyedService rather than GetRequiredKeyedService; same feature, credited |
| dotnet-config-precedence-01 | configuration-options | 3 | 3 | 2 | 0.67 | order and last-wins correct; nested-key syntax (`:` / `__` in env var names) never mentioned |
| dotnet-config-options-interfaces-02 | configuration-options | 3 | 3 | 3 | 1.00 | named-options support of IOptionsSnapshot not mentioned; lifetime/reload/injection split complete, credited |
| dotnet-config-secrets-03 | configuration-options | 2 | 2 | 2 | 1.00 | does not say user secrets load only in Development; out-of-repo + not-for-prod present, credited |
| dotnet-config-options-binding-04 | configuration-options | 2 | 2 | 2 | 1.00 | |
| dotnet-pipeline-order-01 | aspnetcore-pipeline | 3 | 3 | 3 | 1.00 | |
| dotnet-pipeline-use-vs-map-02 | aspnetcore-pipeline | 2 | 3 | 3 | 1.00 | |
| dotnet-pipeline-minimal-results-03 | aspnetcore-pipeline | 2 | 2 | 2 | 1.00 | asides: `TypedResults.Oak<T>` typo; claims a returned `bool` maps to 200/404 (it is serialized as JSON like any object); tangential |
| dotnet-pipeline-filters-vs-middleware-04 | aspnetcore-pipeline | 2 | 2 | 2 | 1.00 | |
| dotnet-efcore-context-lifetime-01 | efcore | 3 | 3 | 3 | 1.00 | IDbContextFactory and "await each op" not named; "one context per logical operation, never share across threads" covers the separate-instances point, credited |
| dotnet-efcore-notracking-02 | efcore | 3 | 2 | 2 | 1.00 | |
| dotnet-efcore-nplus1-03 | efcore | 3 | 3 | 3 | 1.00 | |
| dotnet-efcore-savechanges-tx-04 | efcore | 2 | 3 | 2 | 0.67 | atomic SaveChanges and migrations correct; never says an explicit transaction (BeginTransaction) is needed to span multiple SaveChanges/commands |
| dotnet-gc-generations-loh-01 | memory-gc | 2 | 2 | 2 | 1.00 | aside: calls every gen 2 collection "full, blocking (stop-the-world)" (background GC exists); tangential |
| dotnet-gc-dispose-finalizer-02 | memory-gc | 3 | 3 | 3 | 1.00 | |
| dotnet-gc-span-arraypool-03 | memory-gc | 2 | 2 | 2 | 1.00 | |
| dotnet-gc-struct-vs-class-04 | memory-gc | 2 | 2 | 2 | 1.00 | |
| dotnet-json-stj-defaults-01 | serialization | 2 | 2 | 2 | 1.00 | aside: `AddJsonOptions<JsonOptions>(...)` generic form does not exist (it is `AddControllers().AddJsonOptions(...)` / `ConfigureHttpJsonOptions`); tangential |
| dotnet-json-sourcegen-02 | serialization | 2 | 2 | 2 | 1.00 | |
| dotnet-json-stj-vs-newtonsoft-03 | serialization | 2 | 2 | 2 | 1.00 | |
| dotnet-json-options-reuse-04 | serialization | 2 | 2 | 2 | 1.00 | |
| dotnet-http-socket-exhaustion-01 | resilience-http | 3 | 3 | 2 | 0.67 | never states the middle step: a single static HttpClient avoids exhaustion but ignores DNS changes. The only DNS remark is "each client also pins one DNS resolution", said of the per-request client, and DNS otherwise appears only as a side benefit of factory rotation |
| dotnet-http-typed-clients-02 | resilience-http | 2 | 2 | 2 | 1.00 | |
| dotnet-http-lifetime-dns-03 | resilience-http | 2 | 2 | 1 | 0.50 | handler-binding point withheld (judgment call): says "HttpClient holds the handler" and that a client that "outlives a DNS change" goes stale, but never that a cached factory client keeps its original handler and so never sees rotation; its lead mitigation, shortening `SetHandlerLifetime`, does nothing for a client that is already cached, which is a materially wrong claim about the construct the question asks about. PooledConnectionLifetime alternative present, credited |
| dotnet-http-resilience-04 | resilience-http | 2 | 2 | 2 | 1.00 | aside: pipeline methods given as `.Retry(...)/.CircuitBreaker(...)/.Timeout(...)`; Polly v8 names are `AddRetry/AddCircuitBreaker/AddTimeout`; tangential |
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
| efcore | 0.94 | 4 | ok | omit (strong) |
| memory-gc | 1.00 | 4 | ok | omit (strong) |
| serialization | 1.00 | 4 | ok | omit (strong) |
| resilience-http | 0.78 | 4 | ok | **derive** |
| logging-diagnostics | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Working: hosting-di = (3 + 2 + 2 + 2) / 10 = 0.900; configuration-options = (2 + 3 + 2 + 2) / 10 = 0.900; efcore = (3 + 3 + 3 + 1.333) / 11 = 0.939; resilience-http = (2 + 2 + 1 + 2) / 9 = 0.778.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 69.33 / 74 = 93.7%
```

## Derivation targets

Tags below threshold (`< 0.85`): **resilience-http** → feed into
`derived/dotnet.nvidia__nemotron-3.5-super-vl.SKILL.md`.

## Grader notes on the answers

- No truncation, no duplicated ids, no thinking-aloud, no fabricated tool output, no tool or skill citations. Every id answered exactly once.
- No verbatim overlap with the reference answers; wording and structure are the model's own, and the lost points are concepts the reference contains and the answer lacks — consistent with a genuine closed-book run.
- Miss pattern: the model states mechanisms accurately but tends to stop before the "and here is the fix / the extra rule" half (captive-dependency fix, explicit transaction across SaveChanges, nested-key syntax, the static-client DNS step).
- Recurring minor pattern: small API hallucinations in otherwise-correct answers (`TypedResults.Oak<T>`, `AddJsonOptions<JsonOptions>`, `.Retry/.CircuitBreaker/.Timeout`, `bool` → 200/404). None cost a rubric point; recorded in the notes column.
