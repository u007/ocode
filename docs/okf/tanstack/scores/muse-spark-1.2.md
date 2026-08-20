---
model_id: muse-spark-1.2
model_version: "1.2"
evaluated_via: opencode-go
evaluated_on: 2026-08-19
stack: tanstack
stack_corpus_rev: 1
threshold: 0.75
---

<!-- Filename: model_id with "/" flattened to "__" so it is one valid path
     segment. muse-spark-1.2 has no "/" so it is unchanged. -->

# Scorecard — muse-spark-1.2 on tanstack

> Valid ONLY for `muse-spark-1.2` @ `1.2`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| tanstack-query-keys-01 | query-keys | 2 | 2 | 1 | 0.50 | described deep/structural equality but never named that object *key order* is ignored while array order matters |
| tanstack-query-keys-02 | query-keys, invalidation | 3 | 2 | 2 | 1.00 | |
| tanstack-query-keys-03 | query-keys, query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-query-keys-04 | query-keys, caching | 2 | 2 | 1 | 0.50 | got dedup right but didn't state that staleTime/gc are tracked per-key and shared across observers |
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
| tanstack-suspense-02 | suspense, query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-suspense-03 | suspense, prefetch | 2 | 2 | 2 | 1.00 | |
| tanstack-suspense-04 | suspense | 2 | 2 | 2 | 1.00 | |
| tanstack-prefetch-01 | prefetch | 2 | 2 | 2 | 1.00 | |
| tanstack-prefetch-02 | prefetch | 2 | 2 | 1 | 0.50 | named the return/throw-vs-void split but never said both respect `staleTime` |
| tanstack-router-loaders-01 | router-loaders, router-typesafety | 3 | 2 | 2 | 1.00 | |
| tanstack-router-loaders-02 | router-loaders, prefetch | 2 | 2 | 2 | 1.00 | |
| tanstack-router-loaders-03 | router-loaders, prefetch | 2 | 2 | 1 | 0.50 | named which functions run on preload/intent but never said preload still respects staleTime/caching |
| tanstack-router-loaders-04 | router-loaders, router-typesafety | 2 | 2 | 2 | 1.00 | |
| tanstack-router-search-01 | router-search | 3 | 2 | 2 | 1.00 | |
| tanstack-router-search-02 | router-search | 2 | 2 | 2 | 1.00 | |
| tanstack-router-search-03 | router-search, router-loaders | 2 | 2 | 2 | 1.00 | |
| tanstack-router-search-04 | router-search, router-typesafety | 2 | 2 | 2 | 1.00 | |
| tanstack-router-typesafety-01 | router-typesafety | 2 | 2 | 2 | 1.00 | |
| tanstack-router-typesafety-02 | router-typesafety | 2 | 2 | 1 | 0.50 | got the params-enforcement half, never stated that a route refactor breaks every call site's types |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| query-keys | 0.78 | 4 | ok | omit (above threshold) |
| caching | 0.89 | 4 | ok | omit (strong) |
| invalidation | 1.00 | 4 | ok | omit (strong) |
| mutations | 1.00 | 4 | ok | omit (strong) |
| query-fn | 1.00 | 6 | ok | omit (strong) |
| suspense | 1.00 | 4 | ok | omit (strong) |
| prefetch | 0.80 | 5 | ok | omit (above threshold) |
| router-loaders | 0.91 | 5 | ok | omit (strong) |
| router-search | 1.00 | 4 | ok | omit (strong) |
| router-typesafety | 0.91 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 63 / 68 = 92.6%
```

## Derivation targets

No tag scored below threshold (`< 0.75`). Lowest tag was `query-keys` at
0.78, still above threshold. **No derived skill was created for this
model/stack pair.**

## Grading notes

- Compound rubric points (two sub-concepts joined by "and"/";") were graded
  strictly: both sub-concepts had to be genuinely present for the point,
  matching `rubric-guide.md`'s "genuinely contains (not just name-drops)"
  bar. This is why several answers that got the main mechanism right but
  omitted a secondary clause (e.g. "prefetchQuery/fetchQuery both respect
  staleTime", "preload still respects caching", "refactors break every call
  site") landed at 0.50 rather than 1.00.
- Contamination check: the closed-book run log shows a single
  `[LLM] →`/`[LLM] ←` round trip with **zero tool invocations**, despite 43
  tools (including `websearch`, `webfetch`, `read`) being exposed under
  `-yolo` in an empty, corpus-free CWD — the model never looked anything up.
  Answers also diverge stylistically from the reference and introduce detail
  not present in it (the `@tanstack/query` ESLint plugin, `defaultPreload`,
  `@tanstack/router-plugin`, a full `z.object({ page: z.number()...})`
  example) — signature of parametric knowledge of a well-documented public
  API, not a copied answer key. The near-97%→92.6% range is high but
  plausible for TanStack Query/Router given how heavily documented and
  commonly-trained-on that API surface is.
