---
model_id: space-bunny-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-09-24
stack: nestjs
stack_corpus_rev: 1
threshold: 0.75
---

<!-- Filename: model_id with "/" flattened to "__" so it is one valid path
     segment. `space-bunny-free` has no "/" so it is unchanged. -->

# Scorecard — space-bunny-free on nestjs

> Valid ONLY for `space-bunny-free` @ `alpha`. A version bump invalidates this
> scorecard — re-benchmark.

Answers graded from `../answers/space-bunny-free.md` (closed-book, produced
from `_prompts/nestjs.md` only). Contamination check: no answer is a verbatim
or near-verbatim copy of the reference; wording, structure and the two factual
errors below are the model's own.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| nestjs-modules-01 | modules | 3 | 3 | 3 | 1.00 | |
| nestjs-modules-02 | modules, di | 3 | 3 | 3 | 1.00 | |
| nestjs-modules-03 | modules, providers-async | 2 | 3 | 3 | 1.00 | |
| nestjs-modules-04 | modules | 1 | 2 | 2 | 1.00 | |
| nestjs-di-01 | di | 3 | 3 | 2.5 | 0.83 | singleton caching present; never states the class is its own injection token |
| nestjs-di-02 | di, providers-async | 2 | 2 | 2 | 1.00 | |
| nestjs-di-03 | di | 3 | 3 | 3 | 1.00 | |
| nestjs-di-04 | di | 3 | 3 | 2 | 0.67 | wrong: claims TRANSIENT "also propagates upward" — it does not bubble to dependents |
| nestjs-routing-01 | controllers-routing | 3 | 3 | 3 | 1.00 | |
| nestjs-routing-02 | controllers-routing | 2 | 2 | 2 | 1.00 | |
| nestjs-routing-03 | controllers-routing, pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-routing-04 | controllers-routing | 1 | 2 | 2 | 1.00 | |
| nestjs-lifecycle-01 | lifecycle | 2 | 2 | 2 | 1.00 | |
| nestjs-lifecycle-02 | lifecycle | 2 | 2 | 2 | 1.00 | |
| nestjs-lifecycle-03 | lifecycle | 2 | 2 | 2 | 1.00 | |
| nestjs-lifecycle-04 | lifecycle | 1 | 2 | 2 | 1.00 | |
| nestjs-validation-01 | pipes-validation | 3 | 3 | 3 | 1.00 | |
| nestjs-validation-02 | pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-validation-03 | pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-validation-04 | pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-guards-01 | guards-interceptors | 3 | 3 | 2 | 0.67 | never mentions @UseGuards attachment or the authn/authz role |
| nestjs-guards-02 | guards-interceptors, pipes-validation | 3 | 3 | 3 | 1.00 | |
| nestjs-guards-03 | guards-interceptors | 2 | 3 | 2.5 | 0.83 | names next.handle() but not that it returns an RxJS Observable to transform after the handler |
| nestjs-guards-04 | guards-interceptors, di | 2 | 2 | 2 | 1.00 | |
| nestjs-filters-01 | exception-filters | 2 | 2 | 2 | 1.00 | |
| nestjs-filters-02 | exception-filters | 2 | 3 | 2.5 | 0.83 | ArgumentsHost → response shown; omits @UseFilters binding |
| nestjs-filters-03 | exception-filters | 2 | 2 | 1 | 0.50 | wrong: says filters resolve global → controller → route like guards; it is the reverse. Global registration correct |
| nestjs-filters-04 | exception-filters | 1 | 2 | 2 | 1.00 | |
| nestjs-providers-01 | providers-async | 2 | 2 | 2 | 1.00 | |
| nestjs-providers-02 | providers-async | 2 | 3 | 3 | 1.00 | |
| nestjs-providers-03 | providers-async, modules | 2 | 2 | 2 | 1.00 | |
| nestjs-providers-04 | providers-async, di | 2 | 3 | 3 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| modules | 1.00 | 5 | ok | omit (strong) |
| di | 0.92 | 7 | ok | omit (strong) |
| controllers-routing | 1.00 | 4 | ok | omit (strong) |
| lifecycle | 1.00 | 4 | ok | omit (strong) |
| pipes-validation | 1.00 | 6 | ok | omit (strong) |
| guards-interceptors | 0.87 | 4 | ok | omit (strong) |
| exception-filters | 0.81 | 4 | ok | omit (strong) |
| providers-async | 1.00 | 6 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 64.83 / 69 = 94.0%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none** — no derived skill written for
`space-bunny-free` on nestjs.

Weakest spots for the record (all above threshold): the two outright factual
errors are TRANSIENT-scope bubbling (nestjs-di-04) and exception-filter
resolution order (nestjs-filters-03).
