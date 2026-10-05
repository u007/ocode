---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-05
stack: nestjs
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — longcat-2.5-preview-free on nestjs (2026-10-05 full re-run)

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump invalidates
> this scorecard — re-benchmark. Baseline rerun 2026-10-05; an earlier 2026-09-28
> scorecard exists beside it.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| nestjs-modules-01 | modules | 3 | 3 | 3 | 1.00 |
| nestjs-modules-02 | modules,di | 3 | 3 | 3 | 1.00 |
| nestjs-modules-03 | modules,providers-async | 2 | 3 | 3 | 1.00 |
| nestjs-modules-04 | modules | 1 | 2 | 2 | 1.00 |
| nestjs-di-01 | di | 3 | 3 | 2 | 0.67 | missed class-is-own-token / default singleton caching |
| nestjs-di-02 | di,providers-async | 2 | 2 | 2 | 1.00 |
| nestjs-di-03 | di | 3 | 3 | 3 | 1.00 |
| nestjs-di-04 | di | 3 | 3 | 3 | 1.00 |
| nestjs-routing-01 | controllers-routing | 3 | 3 | 3 | 1.00 |
| nestjs-routing-02 | controllers-routing | 2 | 2 | 2 | 1.00 |
| nestjs-routing-03 | controllers-routing,pipes-validation | 2 | 2 | 2 | 1.00 |
| nestjs-routing-04 | controllers-routing | 1 | 2 | 2 | 1.00 |
| nestjs-lifecycle-01 | lifecycle | 2 | 2 | 2 | 1.00 |
| nestjs-lifecycle-02 | lifecycle | 2 | 2 | 1 | 0.50 | omitted onModuleDestroy; hook order incomplete; enableShutdownHooks ok |
| nestjs-lifecycle-03 | lifecycle | 2 | 2 | 1 | 0.50 | reverse order ok; wrongly said unchanged in v11 |
| nestjs-lifecycle-04 | lifecycle | 1 | 2 | 1 | 0.50 | awaits async ok; trigger not app.init/listen |
| nestjs-validation-01 | pipes-validation | 3 | 3 | 3 | 1.00 |
| nestjs-validation-02 | pipes-validation | 2 | 2 | 2 | 1.00 |
| nestjs-validation-03 | pipes-validation | 2 | 2 | 2 | 1.00 |
| nestjs-validation-04 | pipes-validation | 2 | 2 | 2 | 1.00 |
| nestjs-guards-01 | guards-interceptors | 3 | 3 | 2 | 0.67 | no @UseGuards / authn-authz use |
| nestjs-guards-02 | guards-interceptors,pipes-validation | 3 | 3 | 3 | 1.00 |
| nestjs-guards-03 | guards-interceptors | 2 | 3 | 2 | 0.67 | no next.handle()/Observable detail |
| nestjs-guards-04 | guards-interceptors,di | 2 | 2 | 2 | 1.00 |
| nestjs-filters-01 | exception-filters | 2 | 2 | 2 | 1.00 |
| nestjs-filters-02 | exception-filters | 2 | 3 | 3 | 1.00 |
| nestjs-filters-03 | exception-filters | 2 | 2 | 2 | 1.00 |
| nestjs-filters-04 | exception-filters | 1 | 2 | 1 | 0.50 | 500 ok; no built-in global filter / throw HttpException note |
| nestjs-providers-01 | providers-async | 2 | 2 | 2 | 1.00 |
| nestjs-providers-02 | providers-async | 2 | 3 | 3 | 1.00 |
| nestjs-providers-03 | providers-async,modules | 2 | 2 | 2 | 1.00 |
| nestjs-providers-04 | providers-async,di | 2 | 3 | 2 | 0.67 | no moduleRef.get()/resolve() |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| modules | 1.00 | 5 | ok | omit (strong) |
| controllers-routing | 1.00 | 4 | ok | omit (strong) |
| pipes-validation | 1.00 | 6 | ok | omit (strong) |
| providers-async | 0.94 | 6 | ok | omit (strong) |
| exception-filters | 0.93 | 4 | ok | omit (strong) |
| di | 0.91 | 7 | ok | omit (strong) |
| guards-interceptors | 0.83 | 4 | ok | omit (strong) |
| lifecycle | 0.64 | 4 | ok | **derive** |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 90.8%
```

## Derivation targets

Tags below threshold (`< 0.75`): **lifecycle** (derived skill not written in this task).
