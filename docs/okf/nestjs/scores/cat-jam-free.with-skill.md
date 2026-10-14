---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: nestjs
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — cat-jam-free on nestjs (with-skill validation run)

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

With-skill validation run: the closed-book answerer was given the derived skill
(`derived/nestjs.cat-jam-free.SKILL.md` body) prepended to the sheet.
Answers: `answers/cat-jam-free.with-skill.md`. Baseline: `scores/cat-jam-free.md`.

## Provenance

Answerer was `ocode run -m opencode-go/cat-jam-free -effort med` with the SKILL.md body
(front matter stripped) prepended to the `_prompts/nestjs.md` sheet, isolated HOME with
every tool denied, cwd holding only the sheet; the session sqlite shows exactly 1 user +
1 assistant message and 0 tool calls. No answer key was shown to the answerer.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| nestjs-modules-01 | modules | 3 | 3 | 3 | 1.00 | |
| nestjs-modules-02 | modules,di | 3 | 3 | 3 | 1.00 | |
| nestjs-modules-03 | modules,providers-async | 2 | 3 | 3 | 1.00 | |
| nestjs-modules-04 | modules | 1 | 2 | 2 | 1.00 | |
| nestjs-di-01 | di | 3 | 3 | 3 | 1.00 | class-as-token implicit ("by type"); singleton default present (same leniency as baseline) |
| nestjs-di-02 | di,providers-async | 2 | 2 | 2 | 1.00 | |
| nestjs-di-03 | di | 3 | 3 | 3 | 1.00 | |
| nestjs-di-04 | di | 3 | 3 | 3 | 1.00 | |
| nestjs-routing-01 | controllers-routing | 3 | 3 | 3 | 1.00 | |
| nestjs-routing-02 | controllers-routing | 2 | 2 | 2 | 1.00 | named `*path` wildcard in v11; no longer overstates |
| nestjs-routing-03 | controllers-routing,pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-routing-04 | controllers-routing | 1 | 2 | 2 | 1.00 | |
| nestjs-lifecycle-01 | lifecycle | 2 | 2 | 2 | 1.00 | |
| nestjs-lifecycle-02 | lifecycle | 2 | 2 | 2 | 1.00 | |
| nestjs-lifecycle-03 | lifecycle | 2 | 2 | 1 | 0.50 | reverse order A → B → C correct; explicitly unsure whether v11 changed it — no v11 attribution |
| nestjs-lifecycle-04 | lifecycle | 1 | 2 | 2 | 1.00 | |
| nestjs-validation-01 | pipes-validation | 3 | 3 | 3 | 1.00 | |
| nestjs-validation-02 | pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-validation-03 | pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-validation-04 | pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-guards-01 | guards-interceptors | 3 | 3 | 3 | 1.00 | now names authz/authn purpose and `@UseGuards` / `useGlobalGuards` / `APP_GUARD` binding (baseline miss fixed) |
| nestjs-guards-02 | guards-interceptors,pipes-validation | 3 | 3 | 3 | 1.00 | |
| nestjs-guards-03 | guards-interceptors | 2 | 3 | 3 | 1.00 | now names `next.handle()` with before/after split and RxJS operators (baseline miss fixed) |
| nestjs-guards-04 | guards-interceptors,di | 2 | 2 | 2 | 1.00 | |
| nestjs-filters-01 | exception-filters | 2 | 2 | 2 | 1.00 | |
| nestjs-filters-02 | exception-filters | 2 | 3 | 3 | 1.00 | `@UseFilters` binding not named; ArgumentsHost → `switchToHttp()` response present (same leniency as baseline) |
| nestjs-filters-03 | exception-filters | 2 | 2 | 2 | 1.00 | |
| nestjs-filters-04 | exception-filters | 1 | 2 | 2 | 1.00 | |
| nestjs-providers-01 | providers-async | 2 | 2 | 2 | 1.00 | |
| nestjs-providers-02 | providers-async | 2 | 3 | 3 | 1.00 | |
| nestjs-providers-03 | providers-async,modules | 2 | 2 | 2 | 1.00 | `forRoot()` now named |
| nestjs-providers-04 | providers-async,di | 2 | 3 | 2 | 0.67 | createTestingModule/compile + overrideProvider ok; retrieval only "from the compiled module/fixture" — no `module.get()`/`resolve()` (baseline named `module.get`) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | Δ | n | target? |
|-----|---------:|-----------:|---:|--:|---------|
| guards-interceptors | 0.83 | **1.00** | +0.17 | 4 | **TARGET** |
| modules | 1.00 | 1.00 | 0 | 5 | no |
| di | 1.00 | 0.96 | −0.04 | 7 | no |
| controllers-routing | 1.00 | 1.00 | 0 | 4 | no |
| pipes-validation | 1.00 | 1.00 | 0 | 6 | no |
| providers-async | 1.00 | 0.94 | −0.06 | 6 | no |
| exception-filters | 1.00 | 1.00 | 0 | 4 | no |
| lifecycle | 0.86 | 0.86 | 0 | 4 | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.
guards-interceptors = 10/10; di = 17.33/18; providers-async = 11.33/12; lifecycle = 6/7.

## Stack score

```
baseline:   66.33 / 69 = 96.1%
with-skill: 67.33 / 69 = 97.6%
```

## Verdict

**PASS.** The only target tag, `guards-interceptors`, moved 0.83 → 1.00 (threshold 0.85).
Both baseline misses the skill cited (guards-01 binding/purpose, guards-03 `next.handle()`
mechanism) are now answered in full.

Non-target drift (noted, not acted on): `providers-async` and `di` dipped on providers-04
(retrieval via `module.get()` dropped this sample); `lifecycle` unchanged at 0.857 from the
same lifecycle-03 v11-attribution miss, still a watch item from the baseline. All non-target
tags remain ≥ 0.85.
