---
model_id: space-bunny-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-09-24
stack: tanstack
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — space-bunny-free on tanstack

> Valid ONLY for `space-bunny-free` @ `alpha`. A version bump invalidates this
> scorecard — re-benchmark.

Answers graded closed-book from `../answers/space-bunny-free.md` (produced with
only `_prompts/tanstack.md` as input). Half-point awards below mark a rubric
`point` whose primary concept was present but whose secondary clause was
missing; the note names the missing clause.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| tanstack-query-keys-01 | query-keys | 2 | 2 | 1.5 | 0.75 | deterministic hash + structural equality covered; never said object-key order is ignored while array order matters |
| tanstack-query-keys-02 | query-keys, invalidation | 3 | 2 | 2 | 1.00 | |
| tanstack-query-keys-03 | query-keys, query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-query-keys-04 | query-keys, caching | 2 | 2 | 1.5 | 0.75 | dedup correct; shared-Query observer model present but per-key staleTime/gc tracking only implied via "if data is fresh" |
| tanstack-caching-01 | caching | 3 | 3 | 3 | 1.00 | |
| tanstack-caching-02 | caching | 2 | 2 | 1.5 | 0.75 | stale-while-revalidate on remount correct; never said raising staleTime suppresses the background refetch |
| tanstack-invalidation-01 | invalidation | 2 | 2 | 2 | 1.00 | |
| tanstack-invalidation-02 | invalidation, caching | 2 | 2 | 2 | 1.00 | |
| tanstack-mutations-01 | mutations | 2 | 2 | 1.5 | 0.75 | imperative-vs-auto and v5 isPending correct; never mentioned onSuccess/onError/onSettled as a tracking surface |
| tanstack-mutations-02 | mutations, invalidation | 2 | 2 | 2 | 1.00 | |
| tanstack-mutations-03 | mutations | 3 | 3 | 3 | 1.00 | |
| tanstack-mutations-04 | mutations, query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-query-fn-01 | query-fn | 3 | 2 | 2 | 1.00 | |
| tanstack-query-fn-02 | query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-query-fn-03 | query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-suspense-01 | suspense | 2 | 2 | 2 | 1.00 | |
| tanstack-suspense-02 | suspense, query-fn | 2 | 2 | 1 | 0.50 | errors-to-ErrorBoundary correct (plus QueryErrorResetBoundary / route errorComponent); never stated the Suspense+ErrorBoundary pairing or that queryFn must still throw |
| tanstack-suspense-03 | suspense, prefetch | 2 | 2 | 2 | 1.00 | |
| tanstack-suspense-04 | suspense | 2 | 2 | 2 | 1.00 | |
| tanstack-prefetch-01 | prefetch | 2 | 2 | 2 | 1.00 | |
| tanstack-prefetch-02 | prefetch | 2 | 2 | 1.5 | 0.75 | returns/throws vs void/swallows and when-to-pick correct; never said both respect staleTime |
| tanstack-router-loaders-01 | router-loaders, router-typesafety | 3 | 2 | 2 | 1.00 | |
| tanstack-router-loaders-02 | router-loaders, prefetch | 2 | 2 | 2 | 1.00 | |
| tanstack-router-loaders-03 | router-loaders, prefetch | 2 | 2 | 1.5 | 0.75 | beforeLoad + loader run on intent correct; never said preload respects staleTime/loader caching |
| tanstack-router-loaders-04 | router-loaders, router-typesafety | 2 | 2 | 1.5 | 0.75 | ordering and auth/redirect correct; described context only as supplied at createRouter, never that beforeLoad's return merges into context for descendants |
| tanstack-router-search-01 | router-search | 3 | 2 | 2 | 1.00 | |
| tanstack-router-search-02 | router-search | 2 | 2 | 2 | 1.00 | |
| tanstack-router-search-03 | router-search, router-loaders | 2 | 2 | 2 | 1.00 | |
| tanstack-router-search-04 | router-search, router-typesafety | 2 | 2 | 1.5 | 0.75 | Link/navigate/search-hook typing correct; never said a schema rename surfaces as compile errors at every call site |
| tanstack-router-typesafety-01 | router-typesafety | 2 | 2 | 2 | 1.00 | |
| tanstack-router-typesafety-02 | router-typesafety | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| query-keys | 0.89 | 4 | ok | omit (strong) |
| caching | 0.89 | 4 | ok | omit (strong) |
| invalidation | 1.00 | 4 | ok | omit (strong) |
| mutations | 0.94 | 4 | ok | omit (strong) |
| query-fn | 0.92 | 6 | ok | omit (strong) |
| suspense | 0.88 | 4 | ok | omit (strong) |
| prefetch | 0.90 | 5 | ok | omit (strong) |
| router-loaders | 0.91 | 5 | ok | omit (strong) |
| router-search | 0.94 | 4 | ok | omit (strong) |
| router-typesafety | 0.91 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 63 / 68 = 92.6%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none**. No derived skill written for
`space-bunny-free` on tanstack.

The only sub-0.75 single question is `tanstack-suspense-02` (0.50), the same
gap mimo-v2.5 showed: names the ErrorBoundary but not the Suspense+ErrorBoundary
pairing or the queryFn-must-throw contract. Every other deduction is a missing
secondary clause, not a wrong mechanism. Weakest tag is `suspense` at 0.88.

## Contamination check

No answer is a verbatim or near-verbatim copy of the reference. All answers are
in the model's own phrasing and several add correct detail the key omits
(`throwOnError`, `QueryErrorResetBoundary`, `useQueries`, preload delay, route
`errorComponent`). `tanstack-router-search-03` reuses the
`loaderDeps: ({ search }) => ({ page: search.page })` snippet found in the key,
but that is the canonical docs example and the surrounding prose differs.
