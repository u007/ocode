---
model_id: glm-5.3-flash
model_version: "5.3"
evaluated_via: aihubmix
evaluated_on: 2026-09-01
stack: tanstack
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — glm-5.3-flash on tanstack

> Valid ONLY for `glm-5.3-flash` @ `5.3`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| tanstack-query-keys-01 | query-keys | 2 | 2 | 2 | 1.00 | deterministic hash + sorted object keys stated; array-order sensitivity only implied |
| tanstack-query-keys-02 | query-keys, invalidation | 3 | 2 | 2 | 1.00 | |
| tanstack-query-keys-03 | query-keys, query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-query-keys-04 | query-keys, caching | 2 | 2 | 1 | 0.50 | got request dedup; never said staleTime/gc are tracked per key and shared across observers |
| tanstack-caching-01 | caching | 3 | 3 | 3 | 1.00 | |
| tanstack-caching-02 | caching | 2 | 2 | 2 | 1.00 | SWR + always-stale covered; did not spell out that raising staleTime suppresses the refetch |
| tanstack-invalidation-01 | invalidation | 2 | 2 | 2 | 1.00 | |
| tanstack-invalidation-02 | invalidation, caching | 2 | 2 | 2 | 1.00 | |
| tanstack-mutations-01 | mutations | 2 | 2 | 2 | 1.00 | |
| tanstack-mutations-02 | mutations, invalidation | 2 | 2 | 2 | 1.00 | |
| tanstack-mutations-03 | mutations | 3 | 3 | 3 | 1.00 | |
| tanstack-mutations-04 | mutations, query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-query-fn-01 | query-fn | 3 | 2 | 2 | 1.00 | |
| tanstack-query-fn-02 | query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-query-fn-03 | query-fn | 2 | 2 | 2 | 1.00 | also named skipToken and the pending+idle vs pending+fetching distinction |
| tanstack-suspense-01 | suspense | 2 | 2 | 2 | 1.00 | |
| tanstack-suspense-02 | suspense, query-fn | 2 | 2 | 2 | 1.00 | ErrorBoundary-above-Suspense pairing present; "queryFn must still throw" not restated explicitly |
| tanstack-suspense-03 | suspense, prefetch | 2 | 2 | 2 | 1.00 | |
| tanstack-suspense-04 | suspense | 2 | 2 | 2 | 1.00 | |
| tanstack-prefetch-01 | prefetch | 2 | 2 | 2 | 1.00 | |
| tanstack-prefetch-02 | prefetch | 2 | 2 | 2 | 1.00 | throws-vs-swallows and when-to-pick correct; did not state both respect staleTime |
| tanstack-router-loaders-01 | router-loaders, router-typesafety | 3 | 2 | 2 | 1.00 | |
| tanstack-router-loaders-02 | router-loaders, prefetch | 2 | 2 | 2 | 1.00 | |
| tanstack-router-loaders-03 | router-loaders, prefetch | 2 | 2 | 2 | 1.00 | hedged on the intent-delay value; not graded |
| tanstack-router-loaders-04 | router-loaders, router-typesafety | 2 | 2 | 2 | 1.00 | |
| tanstack-router-search-01 | router-search | 3 | 2 | 2 | 1.00 | |
| tanstack-router-search-02 | router-search | 2 | 2 | 2 | 1.00 | |
| tanstack-router-search-03 | router-search, router-loaders | 2 | 2 | 2 | 1.00 | named loaderDeps and the not-tracked-by-default reason |
| tanstack-router-search-04 | router-search, router-typesafety | 2 | 2 | 2 | 1.00 | |
| tanstack-router-typesafety-01 | router-typesafety | 2 | 2 | 2 | 1.00 | |
| tanstack-router-typesafety-02 | router-typesafety | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| query-keys | 0.89 | 4 | ok | omit (strong) |
| caching | 0.89 | 4 | ok | omit (strong) |
| invalidation | 1.00 | 4 | ok | omit (strong) |
| mutations | 1.00 | 4 | ok | omit (strong) |
| query-fn | 1.00 | 6 | ok | omit (strong) |
| suspense | 1.00 | 4 | ok | omit (strong) |
| prefetch | 1.00 | 5 | ok | omit (strong) |
| router-loaders | 1.00 | 5 | ok | omit (strong) |
| router-search | 1.00 | 4 | ok | omit (strong) |
| router-typesafety | 1.00 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 67 / 68 = 98.5%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none** — no derived skill written for
`glm-5.3-flash` on tanstack. The single miss (per-key staleTime/gc tracking in
`tanstack-query-keys-04`) leaves query-keys and caching at 0.89, well above
threshold.
