---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: tanstack
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — longcat-2.5-preview-free on tanstack

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump
> invalidates this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** These answers were produced closed-book with
> the derived tuning skill `derived/tanstack.longcat-2.5-preview-free.SKILL.md`
> (`tanstack-tuning-longcat-2.5-preview-free`) prepended to the answerer
> prompt as active guidance. The answerer never saw `questions.yaml` or the
> rubric (`ocode run` from an isolated dir; session audited: 1 user + 1
> assistant turn, zero tool calls). Grading is held to the same strict
> standard as the baseline `scores/longcat-2.5-preview-free.md`, including
> its half-point convention (primary concept present, secondary clause
> missing). No derived skill is written or modified from this run.
>
> Target tags for this skill: **query-keys, router-loaders, router-search,
> router-typesafety**.

Answers graded from `../answers/longcat-2.5-preview-free.with-skill.md`.

Contamination check: no answer copies the reference. The target-tag answers
track the skill's wording closely (e.g. query-keys-01 reuses the skill's
"sorts plain-object keys" and `{ a: 1, b: 2 }` example; router-search-03
reuses the skill's `loaderDeps: ({ search }) => ({ page: search.page })`
line). That is expected absorption, not answer-key access. Non-target
answers still diverge from the key in the same places as the baseline:
query-fn-02 again recommends a `useEffect` reacting to query state,
mutations-01 again lists a v4-style `isLoading` flag, prefetch-01 claims
`prefetchQuery` "always fires the queryFn" (it respects `staleTime`), and
suspense-02 names the v4 `useErrorBoundary` option. Consistent with a blind
run. Verdict: clean.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| tanstack-query-keys-01 | query-keys | 2 | 2 | 2 | 1.00 | serializable array identifying the entry + stable hash, object keys sorted, array order matters, both present (baseline 0.50) |
| tanstack-query-keys-02 | query-keys, invalidation | 3 | 2 | 2 | 1.00 | prefix invalidation + broad-and-precise payoff of the hierarchy, both present (baseline 0.75) |
| tanstack-query-keys-03 | query-keys, query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-query-keys-04 | query-keys, caching | 2 | 2 | 1.5 | 0.75 | dedup to one request correct; "cache state is per key, not per component" present, but staleTime/gc tracking shared across observers never named (baseline 0.50) |
| tanstack-caching-01 | caching | 3 | 3 | 3 | 1.00 | staleTime, gcTime, independence and defaults (0 / 5 min) all present |
| tanstack-caching-02 | caching | 2 | 2 | 1.5 | 0.75 | stale-while-revalidate on remount correct; never says raising staleTime suppresses the refetch (same as baseline) |
| tanstack-invalidation-01 | invalidation | 2 | 2 | 2 | 1.00 | |
| tanstack-invalidation-02 | invalidation, caching | 2 | 2 | 2 | 1.00 | |
| tanstack-mutations-01 | mutations | 2 | 2 | 1.5 | 0.75 | imperative-vs-auto and v5 `'pending'` status correct; claims `isLoading` is available on the mutation and never names onSuccess/onError/onSettled |
| tanstack-mutations-02 | mutations, invalidation | 2 | 2 | 2 | 1.00 | |
| tanstack-mutations-03 | mutations | 3 | 3 | 2.5 | 0.83 | cancelQueries, snapshot, optimistic setQueryData, rollback, onSettled invalidate all present; snapshot never described as returned from onMutate as context |
| tanstack-mutations-04 | mutations, query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-query-fn-01 | query-fn | 3 | 2 | 2 | 1.00 | |
| tanstack-query-fn-02 | query-fn | 2 | 2 | 1.5 | 0.75 | pure fetch / invoked multiple times correct; again recommends a `useEffect` reacting to query state instead of reading `data` / deriving (same as baseline) |
| tanstack-query-fn-03 | query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-suspense-01 | suspense | 2 | 2 | 1.5 | 0.75 | Suspense boundary + no loading state in the body present; never states `data` is guaranteed defined (same as baseline) |
| tanstack-suspense-02 | suspense, query-fn | 2 | 2 | 1 | 0.50 | errors thrown to ErrorBoundary correct; no Suspense+ErrorBoundary pairing, no "queryFn must still throw"; cites v4 `useErrorBoundary` option (same score as baseline) |
| tanstack-suspense-03 | suspense, prefetch | 2 | 2 | 2 | 1.00 | "may not start fetching the second until the first resolves" + prefetch in a parent loader, both present; surrounding reasoning is muddled (claims both suspend simultaneously yet the wait is the sum) |
| tanstack-suspense-04 | suspense | 2 | 2 | 2 | 1.00 | |
| tanstack-prefetch-01 | prefetch | 2 | 2 | 2 | 1.00 | both points present; incorrect aside that prefetchQuery "always fires the queryFn" (it respects staleTime) not penalized, rubric does not test it |
| tanstack-prefetch-02 | prefetch | 2 | 2 | 1.5 | 0.75 | returns/throws vs void/swallows and when-to-pick correct; never says both respect staleTime (same as baseline) |
| tanstack-router-loaders-01 | router-loaders, router-typesafety | 3 | 2 | 2 | 1.00 | loader before render + `useLoaderData()` typed from the loader's return, and useEffect contrast, both present (baseline 0.75) |
| tanstack-router-loaders-02 | router-loaders, prefetch | 2 | 2 | 2 | 1.00 | ensureQueryData with same queryOptions + shared QueryClient on router context, both present (baseline 0.75) |
| tanstack-router-loaders-03 | router-loaders, prefetch | 2 | 2 | 2 | 1.00 | beforeLoad + loader on hover, instant nav, respects staleTime/loader cache, both present (baseline 0.75) |
| tanstack-router-loaders-04 | router-loaders, router-typesafety | 2 | 2 | 2 | 1.00 | ordering, guards/redirects + typed context threaded down the tree with beforeLoad merge, both present (baseline 0.75) |
| tanstack-router-search-01 | router-search | 3 | 2 | 2 | 1.00 | validateSearch/Zod typed object + URL is untrusted user-editable input, both present (baseline 0.75) |
| tanstack-router-search-02 | router-search | 2 | 2 | 2 | 1.00 | read via `Route.useSearch()`, write via typed navigate/Link + no useState mirroring, both present (baseline 0.75) |
| tanstack-router-search-03 | router-search, router-loaders | 2 | 2 | 2 | 1.00 | path-params-only tracking by default + `loaderDeps` exposes and reloads, explicitly separated from validateSearch, both present (baseline 0.25) |
| tanstack-router-search-04 | router-search, router-typesafety | 2 | 2 | 2 | 1.00 | Link/navigate schema-checked + typed useSearch and schema change breaks every site, both present (baseline 0.50) |
| tanstack-router-typesafety-01 | router-typesafety | 2 | 2 | 2 | 1.00 | |
| tanstack-router-typesafety-02 | router-typesafety | 2 | 2 | 2 | 1.00 | route/param enforced + navigate/search and refactor errors at every call site, both present (baseline 0.50) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | n | trust | target? |
|-----|---------:|-----------:|--:|-------|---------|
| query-keys | 0.69 | 0.94 | 4 | ok | **yes** |
| caching | 0.78 | 0.89 | 4 | ok | no |
| invalidation | 0.92 | 1.00 | 4 | ok | no |
| mutations | 0.89 | 0.89 | 4 | ok | no |
| query-fn | 0.88 | 0.88 | 6 | ok | no |
| suspense | 0.81 | 0.81 | 4 | ok | no |
| prefetch | 0.85 | 0.95 | 5 | ok | no |
| router-loaders | 0.66 | 1.00 | 5 | ok | **yes** |
| router-search | 0.58 | 1.00 | 4 | ok | **yes** |
| router-typesafety | 0.70 | 1.00 | 5 | ok | **yes** |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Per-tag arithmetic (with-skill): query-keys 8.5/9; caching 8/9; invalidation
9/9; mutations 8/9; query-fn 11.5/13; suspense 6.5/8; prefetch 9.5/10;
router-loaders 11/11; router-search 9/9; router-typesafety 11/11.

## Target tags verdict

| tag | baseline | with-skill | verdict |
|-----|---------:|-----------:|---------|
| query-keys | 0.69 | 0.94 | **PASS** |
| router-loaders | 0.66 | 1.00 | **PASS** (saturated) |
| router-search | 0.58 | 1.00 | **PASS** (saturated) |
| router-typesafety | 0.70 | 1.00 | **PASS** (saturated) |

Validation verdict: **SUCCESS** — every target tag crosses 0.75. No target
tag FAILs, so no skill tweak is indicated.

- query-keys: the key-hashing directive landed fully (query-keys-01
  0.50 → 1.00) and the broad-and-precise hierarchy point landed
  (query-keys-02 0.75 → 1.00). One residual miss: query-keys-04 says "cache
  state is per key, not per component" but still does not name
  staleTime/gcTime as the per-key state shared across observers, even
  though the skill states it. Tag is well above threshold, so this is not
  a failure; if the skill is ever sharpened, that bullet is the one that
  landed only partially.
- router-loaders / router-search / router-typesafety: every rubric point
  missed at baseline is now present. The biggest mover is router-search-03
  (0.25 → 1.00): the answer now says loaders track path params only and that
  `loaderDeps` is separate from `validateSearch`, which was the baseline's
  root failure.

Non-target drift (not a failure): caching +0.11 (caching-01 now states the
0 / 5 min defaults; query-keys-04 half-point shared), invalidation +0.08
(via query-keys-02), prefetch +0.10 (via router-loaders-02/03). mutations,
query-fn and suspense are unchanged, and their misses are the same as the
baseline (useEffect state mirroring in query-fn-02, `data` never called
guaranteed-defined in suspense-01, no Suspense+ErrorBoundary pairing in
suspense-02, no "raising staleTime suppresses the refetch" in caching-02,
no "both respect staleTime" in prefetch-02). All non-target tags remain
≥ 0.75.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 63.5 / 68 = 93.4%
```

(Baseline: 53.75 / 68 = 79.0%.)

## Derivation targets

None — this is a validation run; no derived skill is written from it. All
tags are ≥ 0.75.
