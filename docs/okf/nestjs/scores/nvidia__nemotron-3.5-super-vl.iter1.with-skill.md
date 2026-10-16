---
model_id: nvidia/nemotron-3.5-super-vl
model_version: "3.5"
evaluated_via: openrouter
evaluated_on: 2026-10-16
stack: nestjs
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — nvidia/nemotron-3.5-super-vl on nestjs (with-skill validation run)

> Valid ONLY for `nvidia/nemotron-3.5-super-vl` @ `3.5`. A version bump invalidates
> this scorecard — re-benchmark.

With-skill validation run: the closed-book answerer was given the derived skill
(`../derived/nestjs.nvidia__nemotron-3.5-super-vl.SKILL.md` body) prepended to the
sheet. This scorecard validates that skill. Answers:
`answers/nvidia__nemotron-3.5-super-vl.with-skill.md`. Baseline:
`scores/nvidia__nemotron-3.5-super-vl.md`. Same grading standards as the baseline.

## Provenance

Answerer was `ocode run -m openrouter/nvidia/nemotron-3.5-super-vl:free -effort med`
with the SKILL.md body (front matter stripped) prepended to the full
`_prompts/nestjs.md` sheet, isolated HOME with every tool denied (no tools advertised),
cwd holding only the sheet; one session this time — all 32 ids returned at once, no
re-ask needed. The session sqlite shows exactly 1 user + 1 assistant message and 0 tool
calls. No answer key was shown to the answerer. No answer cites a tool or skill the
question never named; no duplicated answers, thinking-aloud or fabricated tool output.
routing-02 again hedges honestly on the v11 wildcard change. providers-01 invents a
provider option `async: true` (no such key exists); noted, point still awarded since the
async `useFactory` + `inject` recipe is otherwise correct.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| nestjs-modules-01 | modules | 3 | 3 | 3 | 1.00 | |
| nestjs-modules-02 | modules,di | 3 | 3 | 3 | 1.00 | |
| nestjs-modules-03 | modules,providers-async | 2 | 3 | 3 | 1.00 | |
| nestjs-modules-04 | modules | 1 | 2 | 2 | 1.00 | |
| nestjs-di-01 | di | 3 | 3 | 3 | 1.00 | class-as-token implicit; "same shared singleton instance everywhere by default" present (same leniency as baseline) |
| nestjs-di-02 | di,providers-async | 2 | 2 | 2 | 1.00 | |
| nestjs-di-03 | di | 3 | 3 | 3 | 1.00 | |
| nestjs-di-04 | di | 3 | 3 | 3 | 1.00 | |
| nestjs-routing-01 | controllers-routing | 3 | 3 | 3 | 1.00 | |
| nestjs-routing-02 | controllers-routing | 2 | 2 | 1 | 0.50 | param route ok; "I am not certain about the specific change to wildcard routes in NestJS 11, so I will not state it as fact" — same miss as baseline (non-target) |
| nestjs-routing-03 | controllers-routing,pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-routing-04 | controllers-routing | 1 | 2 | 2 | 1.00 | |
| nestjs-lifecycle-01 | lifecycle | 2 | 2 | 2 | 1.00 | |
| nestjs-lifecycle-02 | lifecycle | 2 | 2 | 2 | 1.00 | all three hooks in order incl. `beforeApplicationShutdown(signal)`; `enableShutdownHooks()` present (baseline miss fixed) |
| nestjs-lifecycle-03 | lifecycle | 2 | 2 | 2 | 1.00 | reverse order A → B → C and attributes the guarantee to NestJS 11 (baseline miss fixed) |
| nestjs-lifecycle-04 | lifecycle | 1 | 2 | 2 | 1.00 | |
| nestjs-validation-01 | pipes-validation | 3 | 3 | 3 | 1.00 | |
| nestjs-validation-02 | pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-validation-03 | pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-validation-04 | pipes-validation | 2 | 2 | 2 | 1.00 | |
| nestjs-guards-01 | guards-interceptors | 3 | 3 | 2 | 0.67 | `CanActivate` + boolean + 403 now present, but STILL never says how a guard is attached (`@UseGuards` / `useGlobalGuards` / `APP_GUARD`) or what it is for (authn/authz) — the skill's first directive was not applied |
| nestjs-guards-02 | guards-interceptors,pipes-validation | 3 | 3 | 3 | 1.00 | full `next.handle()` before/after mechanism given here |
| nestjs-guards-03 | guards-interceptors | 2 | 3 | 2 | 0.67 | interface + uses ok (operators `map`/`tap`/`catchError` on "the response stream"), but STILL no `next.handle()` and no before/after split in this answer — the mechanism appears only in guards-02, as in the baseline; graded per answer |
| nestjs-guards-04 | guards-interceptors,di | 2 | 2 | 2 | 1.00 | |
| nestjs-filters-01 | exception-filters | 2 | 2 | 2 | 1.00 | |
| nestjs-filters-02 | exception-filters | 2 | 3 | 3 | 1.00 | `@UseFilters` binding not named; ArgumentsHost → response access present (same leniency as baseline) |
| nestjs-filters-03 | exception-filters | 2 | 2 | 2 | 1.00 | `useGlobalFilters` (plural, correct this time) + `APP_FILTER` |
| nestjs-filters-04 | exception-filters | 1 | 2 | 2 | 1.00 | |
| nestjs-providers-01 | providers-async | 2 | 2 | 2 | 1.00 | invents `async: true` provider option (does not exist); async `useFactory` + `inject` + Nest awaits before listening all present |
| nestjs-providers-02 | providers-async | 2 | 3 | 3 | 1.00 | |
| nestjs-providers-03 | providers-async,modules | 2 | 2 | 2 | 1.00 | `forRoot()` not named; `ConfigService.get` + `isGlobal` present (same leniency as baseline) |
| nestjs-providers-04 | providers-async,di | 2 | 3 | 3 | 1.00 | `overrideProvider(...).useValue(mock)` now correctly placed "before compiling" (baseline miss fixed, non-target) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | Δ | n | target? |
|-----|---------:|-----------:|---:|--:|---------|
| lifecycle | 0.71 | **1.00** | +0.29 | 4 | **TARGET** — reached |
| guards-interceptors | 0.83 | **0.83** | 0 | 4 | **TARGET** — NOT reached |
| modules | 1.00 | 1.00 | 0 | 5 | no |
| di | 0.96 | 1.00 | +0.04 | 7 | no |
| controllers-routing | 0.88 | 0.88 | 0 | 4 | no |
| pipes-validation | 1.00 | 1.00 | 0 | 6 | no |
| exception-filters | 1.00 | 1.00 | 0 | 4 | no |
| providers-async | 0.94 | 1.00 | +0.06 | 6 | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.
lifecycle = 7/7; guards-interceptors = 8.33/10; controllers-routing = 7/8.

## Stack score

```
baseline:   63.67 / 69 = 92.3%
with-skill: 66.33 / 69 = 96.1%
```

## Verdict

**PARTIAL.** `lifecycle` moved 0.71 → 1.00 (both baseline misses, the omitted
`beforeApplicationShutdown` and the denied v11 reverse-order change, are now answered in
full). `guards-interceptors` did NOT move: 0.83 → 0.83, below the 0.85 threshold.

Rubric points still missed on the failing target:

- nestjs-guards-01 — "attached via @UseGuards; used for authz/authn". The answer gives
  the `CanActivate` signature and the true/false/403 semantics (the skill's 403 line was
  absorbed) but never names any binding mechanism or the authn/authz purpose.
- nestjs-guards-03 — "next.handle() gives an Observable — run before and transform after
  (RxJS)". The answer names operators applied to "the response stream" but never
  `next.handle()` or the before/after split. The model does produce that exact
  mechanism in guards-02, so it knows it; it is not carrying it into the "what is an
  interceptor" question.

Iterate the `guards-interceptors` section per HOW-TO-EVALUATE.md step 4 (sharper
directive: e.g. "any answer defining a guard MUST include its `@UseGuards` binding and
authn/authz purpose; any answer defining an interceptor MUST name `next.handle()`").

Non-target drift (noted, not acted on): none downward. `di` and `providers-async`
recovered on providers-04; `controllers-routing` unchanged at 0.875 from the same
routing-02 v11-wildcard unknown, still a watch item.
