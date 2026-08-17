---
model_id: mimo-v2.5
model_version: "2.5"
evaluated_via: opencode-go
evaluated_on: 2026-08-17
stack: tanstack
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — mimo-v2.5 on tanstack

> Valid ONLY for `mimo-v2.5` @ `2.5`. A version bump invalidates this scorecard —
> re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| tanstack-query-keys-01 | query-keys | 2 | 2 | 1 | 0.50 | described deep-equality, not deterministic hash / object-key-order-insensitivity |
| tanstack-query-keys-02 | query-keys, invalidation | 3 | 2 | 2 | 1.00 | |
| tanstack-query-keys-03 | query-keys, query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-query-keys-04 | query-keys, caching | 2 | 2 | 1 | 0.50 | got dedup, missed per-key staleTime/gc tracking |
| tanstack-caching-01 | caching | 3 | 3 | 3 | 1.00 | |
| tanstack-caching-02 | caching | 2 | 2 | 2 | 1.00 | |
| tanstack-invalidation-01 | invalidation | 2 | 2 | 2 | 1.00 | |
| tanstack-invalidation-02 | invalidation, caching | 2 | 2 | 2 | 1.00 | |
| tanstack-mutations-01 | mutations | 2 | 2 | 2 | 1.00 | |
| tanstack-mutations-02 | mutations, invalidation | 2 | 2 | 2 | 1.00 | |
| tanstack-mutations-03 | mutations | 3 | 3 | 3 | 1.00 | |
| tanstack-mutations-04 | mutations, query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-query-fn-01 | query-fn | 3 | 2 | 2 | 1.00 | |
| tanstack-query-fn-02 | query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-query-fn-03 | query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-suspense-01 | suspense | 2 | 2 | 2 | 1.00 | |
| tanstack-suspense-02 | suspense, query-fn | 2 | 2 | 1 | 0.50 | named ErrorBoundary but not the Suspense+ErrorBoundary pairing / queryFn-must-throw |
| tanstack-suspense-03 | suspense, prefetch | 2 | 2 | 2 | 1.00 | |
| tanstack-suspense-04 | suspense | 2 | 2 | 2 | 1.00 | |
| tanstack-prefetch-01 | prefetch | 2 | 2 | 2 | 1.00 | |
| tanstack-prefetch-02 | prefetch | 2 | 2 | 2 | 1.00 | swallow/propagate distinction correct despite mischaracterizing fetchQuery as "internal/not recommended" |
| tanstack-router-loaders-01 | router-loaders, router-typesafety | 3 | 2 | 2 | 1.00 | |
| tanstack-router-loaders-02 | router-loaders, prefetch | 2 | 2 | 2 | 1.00 | |
| tanstack-router-loaders-03 | router-loaders, prefetch | 2 | 2 | 2 | 1.00 | |
| tanstack-router-loaders-04 | router-loaders, router-typesafety | 2 | 2 | 2 | 1.00 | |
| tanstack-router-search-01 | router-search | 3 | 2 | 2 | 1.00 | |
| tanstack-router-search-02 | router-search | 2 | 2 | 1 | 0.50 | update-via-navigate covered; never mentioned reading via `useSearch()` |
| tanstack-router-search-03 | router-search, router-loaders | 2 | 2 | 1 | 0.25 | conflated `validateSearch` typing with `loaderDeps` reload-tracking; never named loaderDeps |
| tanstack-router-search-04 | router-search, router-typesafety | 2 | 2 | 1 | 0.50 | Link/navigate enforcement covered; missed typed `useSearch()` + refactor-breaks-call-sites |
| tanstack-router-typesafety-01 | router-typesafety | 2 | 2 | 2 | 1.00 | |
| tanstack-router-typesafety-02 | router-typesafety | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| query-keys | 0.78 | 4 | ok | omit (strong) |
| caching | 0.89 | 4 | ok | omit (strong) |
| invalidation | 1.00 | 4 | ok | omit (strong) |
| mutations | 1.00 | 4 | ok | omit (strong) |
| query-fn | 0.92 | 6 | ok | omit (strong) |
| suspense | 0.88 | 4 | ok | omit (strong) |
| prefetch | 1.00 | 5 | ok | omit (strong) |
| router-loaders | 0.86 | 5 | ok | omit (strong) |
| router-search | 0.61 | 4 | ok | **derive** |
| router-typesafety | 0.91 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 61.5 / 68 = 90.4%
```

## Derivation targets

Tags below threshold (`< 0.75`): **router-search** → feed into
`derived/tanstack.mimo-v2.5.SKILL.md`.
