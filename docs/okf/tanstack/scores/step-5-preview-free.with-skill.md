---
model_id: step-5-preview-free
model_version: "5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-12
stack: tanstack
stack_corpus_rev: 1
threshold: 0.85
validation: with-skill
---

# Scorecard — step-5-preview-free on tanstack (with-skill validation)

> Valid ONLY for `step-5-preview-free` @ `5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers: `tanstack/answers/step-5-preview-free.with-skill.md`,
> produced closed-book with the current derived skill
> `derived/tanstack.step-5-preview-free.SKILL.md` (`tanstack-tuning-step-5-preview-free`)
> prepended to the answerer prompt as active guidance (session audited: 1 user
> message, 1 assistant message, 0 tool calls). Baseline: `scores/step-5-preview-free.md`.
> No new derived skill is produced from this run.

> Target tags for this skill (baseline < 0.85): **query-fn (0.846), router-search (0.83)**.

Graded against `tanstack/questions.yaml` corpus_rev 1 (Query v5 + Router v1 scope).
Grading rule applied (same strictness as the baseline): a `point` whose concept is
genuinely present earns full credit (minor missing sub-items are noted); a `point`
accompanied by a materially wrong claim about the same construct earns 0, marked
"(judgment)". Points are whole; fractions come only from a listed partial.

## History

- Earlier with-skill scorecard: none (this is the first with-skill validation for
  step-5-preview-free on tanstack).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| tanstack-query-keys-01 | query-keys | 2 | 2 | 2 | 1.00 | names `hashKey`; array order matters, object key order does not — both halves explicit this time |
| tanstack-query-keys-02 | query-keys, invalidation | 3 | 2 | 2 | 1.00 | also notes `exact: true` disables prefix matching |
| tanstack-query-keys-03 | query-keys, query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-query-keys-04 | query-keys, caching | 2 | 2 | 1 | 0.50 | one request + in-flight dedup correct; "shared cache entry" stated but never that staleTime/gc are tracked per key across observers — same gap as baseline |
| tanstack-caching-01 | caching | 3 | 3 | 3 | 1.00 | |
| tanstack-caching-02 | caching | 2 | 2 | 2 | 1.00 | stale-while-revalidate and gcTime-elapsed case covered; "raising staleTime suppresses the refetch" not spelled out (minor) |
| tanstack-invalidation-01 | invalidation | 2 | 2 | 2 | 1.00 | also `refetchType: 'active'`, `cancelRefetch`, returned promise |
| tanstack-invalidation-02 | invalidation, caching | 2 | 2 | 2 | 1.00 | |
| tanstack-mutations-01 | mutations | 2 | 2 | 2 | 1.00 | aside: claims v4 `mutate` took `{ variables }` — v4 also took the variables directly (tangential, out of v5 scope) |
| tanstack-mutations-02 | mutations, invalidation | 2 | 2 | 2 | 1.00 | |
| tanstack-mutations-03 | mutations | 3 | 3 | 3 | 1.00 | writes `onError(variables, error, context)`; real order is `(error, variables, context)` (minor) |
| tanstack-mutations-04 | mutations, query-fn | 2 | 2 | 1 | 0.50 | point 1 now earned: `mutate` swallows (`.catch(noop)`), never an unhandled rejection; `mutateAsync` is the caller's promise — the baseline's wrong claim is gone. Point 2 withheld: describes how failure is *observed* (onError / status / error field) but never states the contract that failure = `mutationFn` throws or rejects (the baseline had this). Net unchanged at 1/2 |
| tanstack-query-fn-01 | query-fn | 3 | 2 | 2 | 1.00 | |
| tanstack-query-fn-02 | query-fn | 2 | 2 | 2 | 1.00 | aside: still recommends per-query onSuccess/onError options, removed from useQuery in v5 (tangential, same as baseline) |
| tanstack-query-fn-03 | query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-suspense-01 | suspense | 2 | 2 | 2 | 1.00 | |
| tanstack-suspense-02 | suspense, query-fn | 2 | 2 | 2 | 1.00 | pairs Suspense (loading) + ErrorBoundary (failure) explicitly and states the queryFn-must-still-throw precondition — the baseline gap is closed. Aside: offers `throwOnError: false` as an opt-out; `useSuspenseQuery` omits that option (tangential) |
| tanstack-suspense-03 | suspense, prefetch | 2 | 2 | 2 | 1.00 | |
| tanstack-suspense-04 | suspense | 2 | 2 | 2 | 1.00 | also names `skipToken` for the non-suspense path |
| tanstack-prefetch-01 | prefetch | 2 | 2 | 2 | 1.00 | |
| tanstack-prefetch-02 | prefetch | 2 | 2 | 2 | 1.00 | "both respect staleTime" not explicit (minor) |
| tanstack-router-loaders-01 | router-loaders, router-typesafety | 3 | 2 | 2 | 1.00 | |
| tanstack-router-loaders-02 | router-loaders, prefetch | 2 | 2 | 2 | 1.00 | adds the staleTime caveat for first-mount refetch |
| tanstack-router-loaders-03 | router-loaders, prefetch | 2 | 2 | 2 | 1.00 | aside: "Link's default preload='intent'"; `defaultPreload` is `false` unless set (question premise invited this; tangential, same as baseline). Caching/staleTime gating not mentioned (minor) |
| tanstack-router-loaders-04 | router-loaders, router-typesafety | 2 | 2 | 2 | 1.00 | the baseline's "loader receives `search`" aside is gone |
| tanstack-router-search-01 | router-search | 3 | 2 | 2 | 1.00 | |
| tanstack-router-search-02 | router-search | 2 | 2 | 2 | 1.00 | |
| tanstack-router-search-03 | router-search, router-loaders | 2 | 2 | 2 | 1.00 | now correct: only path params tracked by default; `validateSearch` is typing, not tracking; `loaderDeps` → `deps` arg + cache identity + reload on change; loader arg has no `search` field. Baseline 0.25 |
| tanstack-router-search-04 | router-search, router-typesafety | 2 | 2 | 2 | 1.00 | |
| tanstack-router-typesafety-01 | router-typesafety | 2 | 2 | 2 | 1.00 | |
| tanstack-router-typesafety-02 | router-typesafety | 2 | 2 | 2 | 1.00 | search-prop and unknown-route checks named; refactor propagation not spelled out (same as baseline) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | delta | n | trust | target? |
|-----|---------:|-----------:|------:|--:|-------|---------|
| query-keys | 0.89 | 0.89 | 0.00 | 4 | ok | no |
| caching | 0.89 | 0.89 | 0.00 | 4 | ok | no |
| invalidation | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| mutations | 0.89 | 0.89 | 0.00 | 4 | ok | no |
| query-fn | 0.85 (0.846) | 0.92 (0.923) | +0.08 | 6 | ok | **yes** |
| suspense | 0.88 | 1.00 | +0.12 | 4 | ok | no |
| prefetch | 1.00 | 1.00 | 0.00 | 5 | ok | no |
| router-loaders | 0.86 | 1.00 | +0.14 | 5 | ok | no |
| router-search | 0.83 | 1.00 | +0.17 | 4 | ok | **yes** |
| router-typesafety | 1.00 | 1.00 | 0.00 | 5 | ok | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: query-keys 8/9; caching 8/9; invalidation 9/9; mutations 8/9;
query-fn 12/13; suspense 8/8; prefetch 10/10; router-loaders 11/11;
router-search 9/9; router-typesafety 11/11.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 66 / 68 = 97.1%
```

(Losses: query-keys-04 −1.0, mutations-04 −1.0.) Baseline 63.5 / 68 = 93.4%.

## Target-tag verdict

| tag | baseline | with-skill | pass (≥ 0.85)? |
|-----|---------:|-----------:|----------------|
| query-fn | 0.846 | 0.923 | **PASS** |
| router-search | 0.83 | 1.00 | **PASS** |

Every target tag reaches 0.85. The skill is validated for step-5-preview-free @ 5-preview.

Residual inside a target tag (not a failure, recorded for the next iteration): on
mutations-04 the model absorbed the `mutate`-swallows correction but dropped the
"failure = `mutationFn` throws / rejects" contract it had stated at baseline, so the
question stays at 1/2. The skill does state that contract; the model simply did not
restate it. query-fn still clears the threshold on the suspense-02 gain alone.

Non-target drift: no non-target tag moved down; suspense (0.88 → 1.00) and
router-loaders (0.86 → 1.00) moved up because their shared questions (suspense-02,
router-search-03) are the skill's own targets. Per HOW-TO-EVALUATE.md, non-target
movement is not acted on either way.

## Provenance

Closed-book (Rule 0). Answers `../answers/step-5-preview-free.with-skill.md` were
produced by `opencode-go/step-5-preview-free` with the derived skill body prepended to
the `_prompts/tanstack.md` sheet as active guidance; the session audit shows 1 user
message, 1 assistant message and 0 tool calls, so no repo file, answer key or web
source was consulted. Grader: a separate Claude session that produced no answer and
did not modify the derived skill.
