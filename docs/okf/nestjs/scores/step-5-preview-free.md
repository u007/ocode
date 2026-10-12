---
model_id: step-5-preview-free
model_version: "5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-12
stack: nestjs
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — step-5-preview-free on nestjs

> Valid ONLY for `step-5-preview-free` @ `5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book (Rule 0): answerer was `ocode run -m opencode-go/step-5-preview-free -effort med`
in an empty scratch dir with only the `_prompts/nestjs.md` sheet attached; the session
sqlite shows exactly 1 user + 1 assistant message and 0 tool calls. The model appended a
short self-summary after the YAML list; it was ignored for grading.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| nestjs-modules-01 | modules | 3 | 3 | 3 | 1.00 | |
| nestjs-modules-02 | modules,di | 3 | 3 | 3 | 1.00 | |
| nestjs-modules-03 | modules,providers-async | 2 | 3 | 3 | 1.00 | |
| nestjs-modules-04 | modules | 1 | 2 | 2 | 1.00 | |
| nestjs-di-01 | di | 3 | 3 | 3 | 1.00 | |
| nestjs-di-02 | di,providers-async | 2 | 2 | 2 | 1.00 | |
| nestjs-di-03 | di | 3 | 3 | 3 | 1.00 | |
| nestjs-di-04 | di | 3 | 3 | 3 | 1.00 | |
| nestjs-routing-01 | controllers-routing | 3 | 3 | 3 | 1.00 | |
| nestjs-routing-02 | controllers-routing | 2 | 2 | 2 | 1.00 | overstates: says bare `*` errors at boot (v11 auto-converts); concept of v11/Express-5 named-wildcard change present |
| nestjs-routing-03 | controllers-routing,pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-routing-04 | controllers-routing | 1 | 2 | 2 | 1.00 | |
| nestjs-lifecycle-01 | lifecycle | 2 | 2 | 2 | 1.00 | |
| nestjs-lifecycle-02 | lifecycle | 2 | 2 | 1 | 0.50 | wrong order: put beforeApplicationShutdown before onModuleDestroy (correct: onModuleDestroy → beforeApplicationShutdown → onApplicationShutdown); enableShutdownHooks ok |
| nestjs-lifecycle-03 | lifecycle | 2 | 2 | 2 | 1.00 | |
| nestjs-lifecycle-04 | lifecycle | 1 | 2 | 2 | 1.00 | |
| nestjs-validation-01 | pipes-validation | 3 | 3 | 3 | 1.00 | |
| nestjs-validation-02 | pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-validation-03 | pipes-validation | 2 | 2 | 2 | 1.00 | rubric concepts present, but falsely claims `transform: true` is the pipe default and misattributes param coercion to `enableImplicitConversion` |
| nestjs-validation-04 | pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-guards-01 | guards-interceptors | 3 | 3 | 2 | 0.67 | never says how a guard is attached (@UseGuards / useGlobalGuards); authz role ok |
| nestjs-guards-02 | guards-interceptors,pipes-validation | 3 | 3 | 3 | 1.00 | |
| nestjs-guards-03 | guards-interceptors | 2 | 3 | 3 | 1.00 | |
| nestjs-guards-04 | guards-interceptors,di | 2 | 2 | 2 | 1.00 | |
| nestjs-filters-01 | exception-filters | 2 | 2 | 2 | 1.00 | |
| nestjs-filters-02 | exception-filters | 2 | 3 | 3 | 1.00 | |
| nestjs-filters-03 | exception-filters | 2 | 2 | 1 | 0.50 | says filters use the "same" global → controller → route hierarchy and that global is checked first (correct: route → controller → global, reverse of guards/pipes); self-contradictory "most specific wins" tail; useGlobalFilters / APP_FILTER ok |
| nestjs-filters-04 | exception-filters | 1 | 2 | 2 | 1.00 | |
| nestjs-providers-01 | providers-async | 2 | 2 | 2 | 1.00 | |
| nestjs-providers-02 | providers-async | 2 | 3 | 3 | 1.00 | |
| nestjs-providers-03 | providers-async,modules | 2 | 2 | 2 | 1.00 | |
| nestjs-providers-04 | providers-async,di | 2 | 3 | 3 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| modules | 1.00 | 5 | ok | omit (strong) |
| di | 1.00 | 7 | ok | omit (strong) |
| controllers-routing | 1.00 | 4 | ok | omit (strong) |
| pipes-validation | 1.00 | 6 | ok | omit (strong) |
| providers-async | 1.00 | 6 | ok | omit (strong) |
| guards-interceptors | 0.90 | 4 | ok | omit (strong) |
| lifecycle | 0.86 | 4 | ok | omit (at threshold: 6/7 = 0.857) |
| exception-filters | 0.86 | 4 | ok | omit (at threshold: 6/7 = 0.857) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 66 / 69 = 95.7%
```

## Derivation targets

Tags below threshold (`< 0.85`): **none** → no
`derived/nestjs.step-5-preview-free.SKILL.md` is created.

`lifecycle` and `exception-filters` both sit at 0.857, a single half-point above the
line: one wrong shutdown-hook order (lifecycle-02) and one wrong filter-resolution
direction (filters-03). A re-run that drops either answer further would put that tag
under threshold; treat both as watch items rather than validated strengths.
