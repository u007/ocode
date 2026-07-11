---
model_id: tencent/hy3
model_version: "3.0"
evaluated_via: novita
evaluated_on: 2026-07-11
stack: tanstack
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — tencent/hy3 on tanstack

> Valid ONLY for `tencent/hy3` @ `3.0`. A version bump invalidates this
> scorecard — re-benchmark.

Grader independent (Opus 4.8); answers produced closed-book (`ocode run -dir <empty>`, no corpus access).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| tanstack-query-keys-01 | query-keys | 2 | 2 | 2 | 1.00 | serializable array + deterministic hash (sorted object keys, array order matters) both present |
| tanstack-query-keys-02 | query-keys, invalidation | 3 | 2 | 2 | 1.00 | prefix match + generic→specific hierarchy both present |
| tanstack-query-keys-03 | query-keys, query-fn | 2 | 2 | 2 | 1.00 | "input signature" = dependency-array concept; cache collision covered |
| tanstack-query-keys-04 | query-keys, caching | 2 | 2 | 1 | 0.50 | dedup correct; MISS: never states cache/staleTime/gc are tracked per key (only observer sharing) |
| tanstack-caching-01 | caching | 3 | 3 | 3 | 1.00 | staleTime=freshness, gcTime=inactive retention, orthogonal; defaults not cited but independence clear |
| tanstack-caching-02 | caching | 2 | 2 | 2 | 1.00 | instant cached render + background refetch (SWR) both present |
| tanstack-invalidation-01 | invalidation | 2 | 2 | 2 | 1.00 | marks stale by prefix, refetches active, does not delete data |
| tanstack-invalidation-02 | invalidation, caching | 2 | 2 | 2 | 1.00 | setQueryData vs invalidate tradeoff fully covered |
| tanstack-mutations-01 | mutations | 2 | 2 | 2 | 1.00 | imperative vs auto; v5 isPending (replaced isLoading) correct |
| tanstack-mutations-02 | mutations, invalidation | 2 | 2 | 2 | 1.00 | write→stale reads→invalidate-to-resync all present |
| tanstack-mutations-03 | mutations | 3 | 3 | 3 | 1.00 | cancelQueries+snapshot / setQueryData+context / onError rollback / onSettled reconcile all correct |
| tanstack-mutations-04 | mutations, query-fn | 2 | 2 | 2 | 1.00 | mutate void vs mutateAsync Promise; must-throw contract present |
| tanstack-query-fn-01 | query-fn | 3 | 3 | 3 | 1.00 | fetch doesn't reject on 5xx → check res.ok and throw; correct |
| tanstack-query-fn-02 | query-fn | 2 | 2 | 2 | 1.00 | pure queryFn, QC controls invocation, side effects elsewhere |
| tanstack-query-fn-03 | query-fn | 2 | 2 | 2 | 1.00 | enabled:!!userId, pending + fetchStatus idle correct |
| tanstack-suspense-01 | suspense | 2 | 2 | 2 | 1.00 | suspends, data returned directly, needs Suspense boundary, errors to boundary |
| tanstack-suspense-02 | suspense, query-fn | 2 | 2 | 1 | 0.50 | errors→ErrorBoundary correct; MISS: no explicit Suspense+ErrorBoundary pairing nor "queryFn must still throw" |
| tanstack-suspense-03 | suspense, prefetch | 2 | 2 | 2 | 1.00 | nesting→waterfall; loader/ensureQueryData/useSuspenseQueries fixes |
| tanstack-suspense-04 | suspense | 2 | 2 | 2 | 1.00 | correctly: no enabled on useSuspenseQuery; conditionally render child instead |
| tanstack-prefetch-01 | prefetch | 2 | 2 | 2 | 1.00 | ensureQueryData awaitable/returns data vs prefetch fire-and-forget |
| tanstack-prefetch-02 | prefetch | 2 | 2 | 2 | 1.00 | fetchQuery throws vs prefetchQuery swallows; pick-when guidance present (staleTime parity not cited) |
| tanstack-router-loaders-01 | router-loaders, router-typesafety | 3 | 2 | 2 | 1.00 | before render, typed useLoaderData, vs useEffect-after-mount |
| tanstack-router-loaders-02 | router-loaders, prefetch | 2 | 2 | 2 | 1.00 | ensureQueryData in loader + same key in component = one cache |
| tanstack-router-loaders-03 | router-loaders, prefetch | 2 | 2 | 2 | 1.00 | intent preload runs beforeLoad+loader, instant nav (staleTime respect not explicit) |
| tanstack-router-loaders-04 | router-loaders, router-typesafety | 2 | 2 | 2 | 1.00 | beforeLoad before loader, guards/redirects, context merge, loader=data |
| tanstack-router-search-01 | router-search | 3 | 3 | 3 | 1.00 | validateSearch/Zod parses raw query; untrusted→coerce/default/type |
| tanstack-router-search-02 | router-search | 2 | 2 | 2 | 1.00 | search params via navigate; useState anti-pattern (minor: cites setSearchParams, a RR API) |
| tanstack-router-search-03 | router-search, router-loaders | 2 | 2 | 0.5 | 0.25 | WRONG: claims loader receives search automatically and fix is validateSearch; MISS: never mentions loaderDeps or that search params aren't tracked loader deps. Only partial (reads search in loader) |
| tanstack-router-search-04 | router-search, router-typesafety | 2 | 2 | 2 | 1.00 | Link/navigate search type-checked; useSearch typed, build-time errors |
| tanstack-router-typesafety-01 | router-typesafety | 2 | 2 | 2 | 1.00 | file routes → routeTree.gen.ts with paths/params/search; compile-checked |
| tanstack-router-typesafety-02 | router-typesafety | 2 | 2 | 1 | 0.50 | Link example fully checked; MISS: no generalization to navigate parity / refactor-safety at call sites |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| query-keys | 0.89 | 4 | ok | omit (strong) |
| caching | 0.89 | 4 | ok | omit (strong) |
| invalidation | 1.00 | 4 | ok | omit (strong) |
| mutations | 1.00 | 4 | ok | omit (strong) |
| query-fn | 0.92 | 6 | ok | omit (strong) |
| suspense | 0.88 | 4 | ok | omit (strong) |
| prefetch | 1.00 | 5 | ok | omit (strong) |
| router-loaders | 0.86 | 5 | ok | omit (strong) |
| router-search | 0.83 | 4 | ok | omit (strong) |
| router-typesafety | 0.91 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 63.5 / 68 = 93%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none**. Every tag scores ≥ 0.75, so **no
derivation** — no `derived/tanstack.tencent__hy3.SKILL.md` is written. The
model's only real miss (router-search-03 / loaderDeps) is isolated and does not
drag the `router-search` tag (0.83) below threshold.
