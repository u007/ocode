---
model_id: glm-5.3-flash
model_version: "5.3"
evaluated_via: aihubmix
evaluated_on: 2026-09-01
stack: nestjs
stack_corpus_rev: 1
threshold: 0.75
---

<!-- Filename: model_id with "/" flattened to "__" so it is one valid path
     segment. `glm-5.3-flash` has no "/" so it is unchanged. -->

# Scorecard — glm-5.3-flash on nestjs

> Valid ONLY for `glm-5.3-flash` @ `5.3`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| nestjs-modules-01 | modules | 3 | 3 | 3 | 1.00 | |
| nestjs-modules-02 | modules, di | 3 | 3 | 3 | 1.00 | |
| nestjs-modules-03 | modules, providers-async | 2 | 3 | 3 | 1.00 | |
| nestjs-modules-04 | modules | 1 | 2 | 2 | 1.00 | |
| nestjs-di-01 | di | 3 | 3 | 3 | 1.00 | |
| nestjs-di-02 | di, providers-async | 2 | 2 | 2 | 1.00 | |
| nestjs-di-03 | di | 3 | 3 | 3 | 1.00 | |
| nestjs-di-04 | di | 3 | 3 | 3 | 1.00 | |
| nestjs-routing-01 | controllers-routing | 3 | 3 | 3 | 1.00 | |
| nestjs-routing-02 | controllers-routing | 2 | 2 | 2 | 1.00 | names Express 5 / `*splat` change |
| nestjs-routing-03 | controllers-routing, pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-routing-04 | controllers-routing | 1 | 2 | 2 | 1.00 | |
| nestjs-lifecycle-01 | lifecycle | 2 | 2 | 2 | 1.00 | |
| nestjs-lifecycle-02 | lifecycle | 2 | 2 | 2 | 1.00 | |
| nestjs-lifecycle-03 | lifecycle | 2 | 2 | 2 | 1.00 | correctly attributes reverse-order teardown to v11 |
| nestjs-lifecycle-04 | lifecycle | 1 | 2 | 1 | 0.50 | says "triggered automatically during startup" — never names app.init()/app.listen() as the trigger; await half correct |
| nestjs-validation-01 | pipes-validation | 3 | 3 | 3 | 1.00 | |
| nestjs-validation-02 | pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-validation-03 | pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-validation-04 | pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-guards-01 | guards-interceptors | 3 | 3 | 2 | 0.67 | CanActivate + boolean/403 + authz use present; never mentions @UseGuards attachment |
| nestjs-guards-02 | guards-interceptors, pipes-validation | 3 | 3 | 3 | 1.00 | |
| nestjs-guards-03 | guards-interceptors | 2 | 3 | 3 | 1.00 | |
| nestjs-guards-04 | guards-interceptors, di | 2 | 2 | 2 | 1.00 | |
| nestjs-filters-01 | exception-filters | 2 | 2 | 2 | 1.00 | |
| nestjs-filters-02 | exception-filters | 2 | 3 | 3 | 1.00 | |
| nestjs-filters-03 | exception-filters | 2 | 2 | 2 | 1.00 | |
| nestjs-filters-04 | exception-filters | 1 | 2 | 2 | 1.00 | |
| nestjs-providers-01 | providers-async | 2 | 2 | 2 | 1.00 | |
| nestjs-providers-02 | providers-async | 2 | 3 | 3 | 1.00 | |
| nestjs-providers-03 | providers-async, modules | 2 | 2 | 2 | 1.00 | |
| nestjs-providers-04 | providers-async, di | 2 | 3 | 3 | 1.00 | names overrideProvider().useValue() and moduleRef.get() |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| modules | 1.00 | 5 | ok | omit (strong) |
| di | 1.00 | 7 | ok | omit (strong) |
| controllers-routing | 1.00 | 4 | ok | omit (strong) |
| lifecycle | 0.93 | 4 | ok | omit (strong) |
| pipes-validation | 1.00 | 6 | ok | omit (strong) |
| guards-interceptors | 0.90 | 4 | ok | omit (strong) |
| exception-filters | 1.00 | 4 | ok | omit (strong) |
| providers-async | 1.00 | 6 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 67.5 / 69 = 97.8%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none**. No derived skill written for
`glm-5.3-flash` on nestjs — every tag is at or above threshold.
