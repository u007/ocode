---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-05
stack: nestjs
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — longcat-2.5-preview-free on nestjs (with-skill validation run)

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

With-skill validation run: the closed-book answerer was given the derived skill
(`derived/nestjs.longcat-2.5-preview-free.SKILL.md` body) prepended to the sheet.
Answers: `answers/longcat-2.5-preview-free.with-skill.2026-10-05.md`.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| nestjs-modules-01 | modules | 3 | 3 | 3 | 1.00 | |
| nestjs-modules-02 | modules, di | 3 | 3 | 3 | 1.00 | |
| nestjs-modules-03 | modules, providers-async | 2 | 3 | 3 | 1.00 | |
| nestjs-modules-04 | modules | 1 | 2 | 2 | 1.00 | |
| nestjs-di-01 | di | 3 | 3 | 3 | 1.00 | class-as-token implicit ("by type") |
| nestjs-di-02 | di, providers-async | 2 | 2 | 2 | 1.00 | |
| nestjs-di-03 | di | 3 | 3 | 3 | 1.00 | |
| nestjs-di-04 | di | 3 | 3 | 2 | 0.67 | bubbling direction reversed (says dependencies become request-scoped, not dependents) |
| nestjs-routing-01 | controllers-routing | 3 | 3 | 3 | 1.00 | |
| nestjs-routing-02 | controllers-routing | 2 | 2 | 2 | 1.00 | wildcard change attributed to path-to-regexp; bare `*` not called discouraged (lenient) |
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
| nestjs-guards-01 | guards-interceptors | 3 | 3 | 2 | 0.67 | no @UseGuards / authz-authn purpose |
| nestjs-guards-02 | guards-interceptors, pipes-validation | 3 | 3 | 3 | 1.00 | |
| nestjs-guards-03 | guards-interceptors | 2 | 3 | 3 | 1.00 | |
| nestjs-guards-04 | guards-interceptors, di | 2 | 2 | 2 | 1.00 | |
| nestjs-filters-01 | exception-filters | 2 | 2 | 2 | 1.00 | |
| nestjs-filters-02 | exception-filters | 2 | 3 | 3 | 1.00 | @UseFilters binding not stated (lenient) |
| nestjs-filters-03 | exception-filters | 2 | 2 | 1 | 0.50 | resolution order wrong (said global→controller→route); useGlobalFilters correct |
| nestjs-filters-04 | exception-filters | 1 | 2 | 2 | 1.00 | |
| nestjs-providers-01 | providers-async | 2 | 2 | 2 | 1.00 | |
| nestjs-providers-02 | providers-async | 2 | 3 | 3 | 1.00 | |
| nestjs-providers-03 | providers-async | 2 | 2 | 2 | 1.00 | forRoot not named explicitly |
| nestjs-providers-04 | providers-async, di | 2 | 3 | 2 | 0.67 | no moduleRef.get()/resolve() retrieval |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| modules | 1.000 | 5 | ok | omit (strong) |
| controllers-routing | 1.000 | 4 | ok | omit (strong) |
| lifecycle | 1.000 | 4 | ok | omit (strong) |
| pipes-validation | 1.000 | 6 | ok | omit (strong) |
| providers-async | 0.944 | 6 | ok | omit (strong) |
| di | 0.907 | 7 | ok | omit (strong) |
| guards-interceptors | 0.900 | 4 | ok | omit (strong) |
| exception-filters | 0.857 | 4 | ok | omit (strong, marginal: 0.007 above threshold) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 65.33 / 69 = 94.7%
```

## Derivation targets

Tags below threshold (`< 0.85`): **none**. All 8 tags reach the threshold in this
with-skill run. `exception-filters` (0.857) is the thinnest margin: a single miss
(filters-03 resolution order, route → controller → global) accounts for the gap.
Residual misses: di-04 (REQUEST bubbles UP to dependents), guards-01 (@UseGuards),
providers-04 (moduleRef.get).
