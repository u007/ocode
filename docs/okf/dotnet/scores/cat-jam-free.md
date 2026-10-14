---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: dotnet
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — cat-jam-free on dotnet

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark. Closed-book answers from
> `answers/cat-jam-free.md`.

## Provenance

`cat-jam-free` is an opencode-go free/alpha model with no published version. It
self-identifies as "CatJam, developed by LitterBox AI". Answerer was
`ocode run -m opencode-go/cat-jam-free -effort med` in an empty scratch cwd with
only the `_prompts/dotnet.md` sheet attached; the session sqlite shows exactly
1 user + 1 assistant message and 0 tool calls. Headless runs inject ocode's
bundled skill-catalogue digest into the system prompt.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| dotnet-di-lifetimes-01 | hosting-di | 3 | 3 | 3 | 1.00 | |
| dotnet-di-captive-02 | hosting-di | 3 | 3 | 2 | 0.67 | definition and singleton-holds-scoped consequence correct; no fix given (neither IServiceScopeFactory nor scope validation) |
| dotnet-di-scope-in-singleton-03 | hosting-di | 2 | 2 | 2 | 1.00 | |
| dotnet-di-keyed-04 | hosting-di | 2 | 2 | 2 | 1.00 | |
| dotnet-config-precedence-01 | configuration-options | 3 | 3 | 2 | 0.67 | order and last-wins correct; nested `:` / `__` key syntax not mentioned |
| dotnet-config-options-interfaces-02 | configuration-options | 3 | 3 | 3 | 1.00 | named-options support of IOptionsSnapshot not mentioned; lifetime/reload/injection split complete, credited |
| dotnet-config-secrets-03 | configuration-options | 2 | 2 | 2 | 1.00 | |
| dotnet-config-options-binding-04 | configuration-options | 2 | 2 | 2 | 1.00 | |
| dotnet-pipeline-order-01 | aspnetcore-pipeline | 3 | 3 | 3 | 1.00 | |
| dotnet-pipeline-use-vs-map-02 | aspnetcore-pipeline | 2 | 3 | 3 | 1.00 | |
| dotnet-pipeline-minimal-results-03 | aspnetcore-pipeline | 2 | 2 | 2 | 1.00 | |
| dotnet-pipeline-filters-vs-middleware-04 | aspnetcore-pipeline | 2 | 2 | 2 | 1.00 | global vs handler-specific scope and bound-argument/result access present; "before routing / after model binding" timing only implied, credited |
| dotnet-efcore-context-lifetime-01 | efcore | 3 | 3 | 2 | 0.67 | scoped + not thread-safe correct; never says what to do for parallel work (separate instances / own scope / IDbContextFactory, await each op) |
| dotnet-efcore-notracking-02 | efcore | 3 | 2 | 2 | 1.00 | |
| dotnet-efcore-nplus1-03 | efcore | 3 | 3 | 3 | 1.00 | |
| dotnet-efcore-savechanges-tx-04 | efcore | 2 | 3 | 2 | 0.67 | single-SaveChanges atomicity and migrations correct; omits that an explicit transaction is needed to span multiple SaveChanges calls; migrations described without `migrations add` / `database update`, credited |
| dotnet-gc-generations-loh-01 | memory-gc | 2 | 2 | 2 | 1.00 | |
| dotnet-gc-dispose-finalizer-02 | memory-gc | 3 | 3 | 2 | 0.67 | deterministic vs safety-net and IAsyncDisposable present; finalizer cost (extra GC survival) and GC.SuppressFinalize absent, so the "costly, SuppressFinalize" point withheld |
| dotnet-gc-span-arraypool-03 | memory-gc | 2 | 2 | 2 | 1.00 | |
| dotnet-gc-struct-vs-class-04 | memory-gc | 2 | 2 | 2 | 1.00 | |
| dotnet-json-stj-defaults-01 | serialization | 2 | 2 | 2 | 1.00 | |
| dotnet-json-sourcegen-02 | serialization | 2 | 2 | 2 | 1.00 | |
| dotnet-json-stj-vs-newtonsoft-03 | serialization | 2 | 2 | 2 | 1.00 | |
| dotnet-json-options-reuse-04 | serialization | 2 | 2 | 2 | 1.00 | does not say STJ serializes by declared type by default; opt-in attributes named, credited |
| dotnet-http-socket-exhaustion-01 | resilience-http | 3 | 3 | 1 | 0.33 | TIME_WAIT/port exhaustion correct; static-client middle step and its DNS flaw absent; factory described as pooling + "manages handler lifetime" with no mention that rotation respects DNS, so the factory point withheld |
| dotnet-http-typed-clients-02 | resilience-http | 2 | 2 | 2 | 1.00 | |
| dotnet-http-lifetime-dns-03 | resilience-http | 2 | 2 | 1 | 0.50 | gives PooledConnectionLifetime; never says a client is bound to its handler, so caching a factory client in a singleton defeats rotation — the "held too long" issue the question asks about. Instead frames staleness as waiting for the factory's own handler lifetime to expire |
| dotnet-http-resilience-04 | resilience-http | 2 | 2 | 2 | 1.00 | names Microsoft.Extensions.Http.Resilience and retry/timeout/circuit-breaker but not `AddStandardResilienceHandler()`; credited |
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
| efcore | 0.848 | 4 | ok | **derive** |
| memory-gc | 0.89 | 4 | ok | omit (strong) |
| serialization | 1.00 | 4 | ok | omit (strong) |
| resilience-http | 0.67 | 4 | ok | **derive** |
| logging-diagnostics | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Working: hosting-di = (3 + 2 + 2 + 2) / 10 = 0.900; configuration-options =
(2 + 3 + 2 + 2) / 10 = 0.900; efcore = (2 + 3 + 3 + 1.333) / 11 = 0.848;
memory-gc = (2 + 2 + 2 + 2) / 9 = 0.889; resilience-http = (1 + 2 + 1 + 2) / 9 = 0.667.

efcore sits just under the threshold (0.848 < 0.85); it is derived because the
rule is a strict `<`.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 66.33 / 74 = 89.6%
```

## Derivation targets

Tags below threshold (`< 0.85`): **efcore, resilience-http** → feed into
`derived/dotnet.cat-jam-free.SKILL.md`.

## Grader notes on the answers

- No truncation; every id answered exactly once.
- Answers are terse (one short paragraph each) and have no verbatim overlap with
  the reference answers, consistent with a genuine closed-book run. No
  materially wrong claims were found; every lost point is an omission.
- Recurring pattern: the model states the problem correctly but drops the
  second half of the answer: the fix for a captive dependency, what to do for
  parallel DbContext work, when an explicit transaction is needed, the
  static-HttpClient DNS flaw, the client-to-handler binding, the finalizer cost
  and SuppressFinalize.
