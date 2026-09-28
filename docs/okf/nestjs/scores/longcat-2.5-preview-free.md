---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: nestjs
stack_corpus_rev: 1
threshold: 0.75
---

<!-- Filename: model_id with "/" flattened to "__" so it is one valid path
     segment. `longcat-2.5-preview-free` has no "/" so it is unchanged. -->

# Scorecard — longcat-2.5-preview-free on nestjs

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump
> invalidates this scorecard — re-benchmark.

Answers graded from `../answers/longcat-2.5-preview-free.md` (closed-book,
produced from `_prompts/nestjs.md` only via `ocode run` in an isolated dir;
session audited: 1 user + 1 assistant turn, zero tool calls). Contamination
check: no answer is a verbatim or near-verbatim copy of the reference; wording
and structure are the model's own (numbered/bulleted lists, different
examples), and it contains an outright error the key contradicts (v11
destroy-order claim) — no leak detected.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| nestjs-modules-01 | modules | 3 | 3 | 3 | 1.00 | |
| nestjs-modules-02 | modules, di | 3 | 3 | 3 | 1.00 | |
| nestjs-modules-03 | modules, providers-async | 2 | 3 | 3 | 1.00 | |
| nestjs-modules-04 | modules | 1 | 2 | 2 | 1.00 | |
| nestjs-di-01 | di | 3 | 3 | 3 | 1.00 | lookup by constructor param type + singleton caching present |
| nestjs-di-02 | di, providers-async | 2 | 2 | 2 | 1.00 | |
| nestjs-di-03 | di | 3 | 3 | 3 | 1.00 | |
| nestjs-di-04 | di | 3 | 3 | 2.5 | 0.83 | says TRANSIENT "does NOT behave the same" (correct) but frames it as not affecting its *dependencies* rather than not bubbling up to its *dependents* |
| nestjs-routing-01 | controllers-routing | 3 | 3 | 3 | 1.00 | |
| nestjs-routing-02 | controllers-routing | 2 | 2 | 2 | 1.00 | attributes wildcard change to path-to-regexp (named `*splat`) rather than Express 5; concept present |
| nestjs-routing-03 | controllers-routing, pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-routing-04 | controllers-routing | 1 | 2 | 2 | 1.00 | |
| nestjs-lifecycle-01 | lifecycle | 2 | 2 | 2 | 1.00 | |
| nestjs-lifecycle-02 | lifecycle | 2 | 2 | 2 | 1.00 | |
| nestjs-lifecycle-03 | lifecycle | 2 | 2 | 1 | 0.50 | wrong: claims reverse destroy order "did NOT change in NestJS 11" — the guarantee is a v11 change |
| nestjs-lifecycle-04 | lifecycle | 1 | 2 | 2 | 1.00 | |
| nestjs-validation-01 | pipes-validation | 3 | 3 | 3 | 1.00 | |
| nestjs-validation-02 | pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-validation-03 | pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-validation-04 | pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-guards-01 | guards-interceptors | 3 | 3 | 2 | 0.67 | never mentions @UseGuards attachment or the authn/authz role |
| nestjs-guards-02 | guards-interceptors, pipes-validation | 3 | 3 | 3 | 1.00 | |
| nestjs-guards-03 | guards-interceptors | 2 | 3 | 2 | 0.67 | only "a next handler" — no next.handle() / RxJS Observable to transform after the handler |
| nestjs-guards-04 | guards-interceptors, di | 2 | 2 | 2 | 1.00 | |
| nestjs-filters-01 | exception-filters | 2 | 2 | 2 | 1.00 | |
| nestjs-filters-02 | exception-filters | 2 | 3 | 2.5 | 0.83 | ArgumentsHost → request/response present; omits @UseFilters binding |
| nestjs-filters-03 | exception-filters | 2 | 2 | 2 | 1.00 | |
| nestjs-filters-04 | exception-filters | 1 | 2 | 1 | 0.50 | 500 default correct; no built-in global filter / no-leak / throw-HttpException point |
| nestjs-providers-01 | providers-async | 2 | 2 | 2 | 1.00 | |
| nestjs-providers-02 | providers-async | 2 | 3 | 3 | 1.00 | |
| nestjs-providers-03 | providers-async, modules | 2 | 2 | 2 | 1.00 | |
| nestjs-providers-04 | providers-async, di | 2 | 3 | 3 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| modules | 1.00 | 5 | ok | omit (strong) |
| di | 0.97 | 7 | ok | omit (strong) |
| controllers-routing | 1.00 | 4 | ok | omit (strong) |
| lifecycle | 0.86 | 4 | ok | omit (strong) |
| pipes-validation | 1.00 | 6 | ok | omit (strong) |
| guards-interceptors | 0.83 | 4 | ok | omit (strong) |
| exception-filters | 0.88 | 4 | ok | omit (strong) |
| providers-async | 1.00 | 6 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 65.00 / 69 = 94.2%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none** — no derived skill written for
`longcat-2.5-preview-free` on nestjs.

Weakest spots for the record (all above threshold): the one outright factual
error is the NestJS 11 destroy-order attribution (nestjs-lifecycle-03); the
remaining losses are omissions in guards/interceptors (@UseGuards,
next.handle() Observable) and exception filters (@UseFilters, built-in filter
behaviour).
