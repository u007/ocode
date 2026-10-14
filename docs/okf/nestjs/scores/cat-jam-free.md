---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: nestjs
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — cat-jam-free on nestjs

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book (Rule 0): answerer was `ocode run -m opencode-go/cat-jam-free -effort med`
in an empty scratch dir with only the `_prompts/nestjs.md` sheet attached; the session
sqlite shows exactly 1 user + 1 assistant message and 0 tool calls. Headless runs inject
ocode's bundled skill-catalogue digest into the system prompt. opencode-go free/alpha
model with no published version; it self-identifies as "CatJam, developed by LitterBox AI".

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| nestjs-modules-01 | modules | 3 | 3 | 3 | 1.00 | |
| nestjs-modules-02 | modules,di | 3 | 3 | 3 | 1.00 | |
| nestjs-modules-03 | modules,providers-async | 2 | 3 | 3 | 1.00 | |
| nestjs-modules-04 | modules | 1 | 2 | 2 | 1.00 | |
| nestjs-di-01 | di | 3 | 3 | 3 | 1.00 | "class is its own token" not stated explicitly; constructor-metadata resolution + singleton default present |
| nestjs-di-02 | di,providers-async | 2 | 2 | 2 | 1.00 | |
| nestjs-di-03 | di | 3 | 3 | 3 | 1.00 | |
| nestjs-di-04 | di | 3 | 3 | 3 | 1.00 | |
| nestjs-routing-01 | controllers-routing | 3 | 3 | 3 | 1.00 | |
| nestjs-routing-02 | controllers-routing | 2 | 2 | 2 | 1.00 | overstates: says bare `*` is "no longer accepted" (v11 auto-converts legacy syntax); named-wildcard change concept present |
| nestjs-routing-03 | controllers-routing,pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-routing-04 | controllers-routing | 1 | 2 | 2 | 1.00 | |
| nestjs-lifecycle-01 | lifecycle | 2 | 2 | 2 | 1.00 | |
| nestjs-lifecycle-02 | lifecycle | 2 | 2 | 2 | 1.00 | |
| nestjs-lifecycle-03 | lifecycle | 2 | 2 | 1 | 0.50 | reverse order A → B → C correct, but asserts v11 "keeps" the order rather than changing it (wrong: the reverse-order guarantee is a v11 change) |
| nestjs-lifecycle-04 | lifecycle | 1 | 2 | 2 | 1.00 | |
| nestjs-validation-01 | pipes-validation | 3 | 3 | 3 | 1.00 | |
| nestjs-validation-02 | pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-validation-03 | pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-validation-04 | pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-guards-01 | guards-interceptors | 3 | 3 | 2 | 0.67 | never says how a guard is attached (@UseGuards) or what it is for (authn/authz) |
| nestjs-guards-02 | guards-interceptors,pipes-validation | 3 | 3 | 3 | 1.00 | |
| nestjs-guards-03 | guards-interceptors | 2 | 3 | 2 | 0.67 | says intercept() "returns an Observable" but no `next.handle()` stream and no before/after RxJS transform mechanism; interface + uses ok |
| nestjs-guards-04 | guards-interceptors,di | 2 | 2 | 2 | 1.00 | |
| nestjs-filters-01 | exception-filters | 2 | 2 | 2 | 1.00 | |
| nestjs-filters-02 | exception-filters | 2 | 3 | 3 | 1.00 | @UseFilters binding not named; ArgumentsHost → response access present |
| nestjs-filters-03 | exception-filters | 2 | 2 | 2 | 1.00 | |
| nestjs-filters-04 | exception-filters | 1 | 2 | 2 | 1.00 | |
| nestjs-providers-01 | providers-async | 2 | 2 | 2 | 1.00 | |
| nestjs-providers-02 | providers-async | 2 | 3 | 3 | 1.00 | |
| nestjs-providers-03 | providers-async,modules | 2 | 2 | 2 | 1.00 | `forRoot()` not named; ConfigService.get + isGlobal present |
| nestjs-providers-04 | providers-async,di | 2 | 3 | 3 | 1.00 | `resolve()` for scoped providers not mentioned |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| modules | 1.00 | 5 | ok | omit (strong) |
| di | 1.00 | 7 | ok | omit (strong) |
| controllers-routing | 1.00 | 4 | ok | omit (strong) |
| pipes-validation | 1.00 | 6 | ok | omit (strong) |
| providers-async | 1.00 | 6 | ok | omit (strong) |
| exception-filters | 1.00 | 4 | ok | omit (strong) |
| lifecycle | 0.86 | 4 | ok | omit (at threshold: 6/7 = 0.857) |
| guards-interceptors | 0.83 | 4 | ok | **derive** (8.33/10) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 66.33 / 69 = 96.1%
```

## Derivation targets

Tags below threshold (`< 0.85`): **guards-interceptors** → feed into
`derived/nestjs.cat-jam-free.SKILL.md`.

`lifecycle` sits at 0.857, one half-point above the line, from the wrong
"v11 did not change destroy order" claim (lifecycle-03). Treat it as a watch item.
