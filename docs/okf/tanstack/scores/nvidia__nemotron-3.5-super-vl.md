---
model_id: nvidia/nemotron-3.5-super-vl
model_version: "3.5"
evaluated_via: openrouter
evaluated_on: 2026-10-16
stack: tanstack
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — nvidia/nemotron-3.5-super-vl on tanstack

> Valid ONLY for `nvidia/nemotron-3.5-super-vl` @ `3.5`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book (Rule 0). The answerer was `ocode run -m openrouter/nvidia/nemotron-3.5-super-vl:free -effort med`
under an isolated HOME whose config denies every tool (no tools were advertised), in a
scratch cwd containing only the `_prompts/tanstack.md` sheet, one fresh session; the
session sqlite shows exactly 1 user + 1 assistant message and 0 tool calls, so no repo
file, answer key or web source was consulted. All 31 ids came back once, none empty.
Headless runs inject the bundled skill-catalogue digest into the system prompt; no answer
cites a tool or skill the question never named. The OpenRouter `:free` route variant is
provenance only and not part of the model id. Answers: `../answers/nvidia__nemotron-3.5-super-vl.md`.
Grader: a separate Claude session that never produced any answer.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| tanstack-query-keys-01 | query-keys | 2 | 2 | 1 | 0.50 | serializable array identifying the entry present; equality described as "deeply equal — same entries in the same order", which implies object key order matters — it does not (object keys are sorted when hashed); deterministic-hash exception absent |
| tanstack-query-keys-02 | query-keys, invalidation | 3 | 2 | 2 | 1.00 | prefix match + one call for list and per-item/detail queries; "precise single-item" side only implied |
| tanstack-query-keys-03 | query-keys, query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-query-keys-04 | query-keys, caching | 2 | 2 | 1 | 0.50 | one request + dedup correct; never says staleTime/gc are tracked per key and shared across observers |
| tanstack-caching-01 | caching | 3 | 3 | 3 | 1.00 | |
| tanstack-caching-02 | caching | 2 | 2 | 2 | 1.00 | stale-while-revalidate + always-stale on remount; also covers the gcTime-elapsed cold-load case; "raising staleTime suppresses the refetch" not spelled out (minor) |
| tanstack-invalidation-01 | invalidation | 2 | 2 | 2 | 1.00 | |
| tanstack-invalidation-02 | invalidation, caching | 2 | 2 | 2 | 1.00 | aside: says invalidate "briefly shows stale/loading data" — invalidation keeps showing stale data, no loading state (tangential) |
| tanstack-mutations-01 | mutations | 2 | 2 | 2 | 1.00 | |
| tanstack-mutations-02 | mutations, invalidation | 2 | 2 | 2 | 1.00 | |
| tanstack-mutations-03 | mutations | 3 | 3 | 3 | 1.00 | |
| tanstack-mutations-04 | mutations, query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-query-fn-01 | query-fn | 3 | 2 | 2 | 1.00 | |
| tanstack-query-fn-02 | query-fn | 2 | 2 | 2 | 1.00 | "breaks deduping and refetch assumptions" taken as the QC-controls-invocation reason |
| tanstack-query-fn-03 | query-fn | 2 | 2 | 2 | 1.00 | `enabled: !!userId` + idle/no-fetch while gated; `status: 'pending'` not named (minor) |
| tanstack-suspense-01 | suspense | 2 | 2 | 1.5 | 0.75 | Suspense boundary, error boundary and no-isLoading-branch present; never states `data` is guaranteed defined (judgment, half point) |
| tanstack-suspense-02 | suspense, query-fn | 2 | 2 | 1 | 0.50 | errors to nearest ErrorBoundary / route `errorComponent` credited; never pairs Suspense with ErrorBoundary and never says the queryFn must still throw for the boundary to fire |
| tanstack-suspense-03 | suspense, prefetch | 2 | 2 | 2 | 1.00 | |
| tanstack-suspense-04 | suspense | 2 | 2 | 2 | 1.00 | |
| tanstack-prefetch-01 | prefetch | 2 | 2 | 2 | 1.00 | ensureQueryData returns/resolves data vs prefetch opportunistic background fetch; `void` return not named (minor) |
| tanstack-prefetch-02 | prefetch | 2 | 2 | 1 | 0.50 | throws-vs-swallows correct; claims "fetchQuery always performs a network request" — it respects staleTime like prefetchQuery, which is the second point's construct (judgment: point withheld) |
| tanstack-router-loaders-01 | router-loaders, router-typesafety | 3 | 2 | 2 | 1.00 | |
| tanstack-router-loaders-02 | router-loaders, prefetch | 2 | 2 | 1.5 | 0.75 | ensureQueryData in loader + same key in component = one cache; shared QueryClient via router context never mentioned (half point) |
| tanstack-router-loaders-03 | router-loaders, prefetch | 2 | 2 | 0.5 | 0.25 | states preload only downloads the lazy module and that "the route loader does not run during preload" — wrong; beforeLoad + loader both run on intent. Only "transition is instant" survives → partial |
| tanstack-router-loaders-04 | router-loaders, router-typesafety | 2 | 2 | 1.5 | 0.75 | ordering and auth/redirect guards correct; context described only as a shared typed object passed to beforeLoad/loader, never threaded down the tree or extended by beforeLoad's return (half point) |
| tanstack-router-search-01 | router-search | 3 | 2 | 2 | 1.00 | validateSearch/zod coercion + typed result present; aside: reads with "useSearchParams" (a React Router hook; TanStack is `Route.useSearch()`) |
| tanstack-router-search-02 | router-search | 2 | 2 | 1.5 | 0.75 | URL-as-state, `router.navigate`, useState anti-pattern present; read/update also attributed to `useSearchParams`/`setSearchParams`, which do not exist in TanStack Router (judgment, half point) |
| tanstack-router-search-03 | router-search, router-loaders | 2 | 2 | 0.5 | 0.25 | attributes the gap to the query string being "opaque unless you opt in" and prescribes `validateSearch` "which passes the typed search object into the loader" — wrong mechanism; `loaderDeps` and reload-tracking never mentioned → partial |
| tanstack-router-search-04 | router-search, router-typesafety | 2 | 2 | 1.5 | 0.75 | Link/navigate require schema-matching search credited; typed read attributed to "useSearchParams and loader search args" (neither exists) and schema-change propagation not stated (half point) |
| tanstack-router-typesafety-01 | router-typesafety | 2 | 2 | 2 | 1.00 | generated tree + compile-checked Link/params/loader data; names "useSearchParams" again (aside) |
| tanstack-router-typesafety-02 | router-typesafety | 2 | 2 | 2 | 1.00 | search-prop check named; navigate/refactor propagation not spelled out |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| query-keys | 0.78 | 4 | ok | **derive** |
| caching | 0.89 | 4 | ok | omit (strong) |
| invalidation | 1.00 | 4 | ok | omit (strong) |
| mutations | 1.00 | 4 | ok | omit (strong) |
| query-fn | 0.92 | 6 | ok | omit (strong) |
| suspense | 0.81 | 4 | ok | **derive** |
| prefetch | 0.70 | 5 | ok | **derive** |
| router-loaders | 0.64 | 5 | ok | **derive** |
| router-search | 0.72 | 4 | ok | **derive** |
| router-typesafety | 0.91 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.
suspense is 6.5/8 = 0.8125 and rests on the half point withheld on suspense-01
(`data` guaranteed defined never stated); with that point the tag would be 0.875.
It is derived per the rule, but treat it as marginal when validating. The other
four derived tags are clear misses, not judgment calls.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 58.5 / 68 = 86.0%
```

## Derivation targets

Tags below threshold (`< 0.85`): **query-keys, suspense, prefetch, router-loaders,
router-search** → feed into `derived/tanstack.nvidia__nemotron-3.5-super-vl.SKILL.md`.

## Answer anomalies

- No truncation, no duplicated or empty answers: all 31 ids answered once, terse
  single-paragraph prose, no thinking-aloud, no fabricated tool output, no tool or
  skill cited.
- Query side is strong (caching, invalidation, mutations, query-fn all ≥ 0.89); every
  loss is concentrated on the Router side plus two per-key-state omissions.
- Recurring React Router contamination: `useSearchParams` / `setSearchParams` /
  `errorElement` are named as TanStack Router APIs in router-search-01/02/04 and
  router-typesafety-01 (TanStack has `Route.useSearch()`, typed `navigate`/`Link
  search`, `errorComponent`). Two invented constructs follow from it: a loader
  "search arg" (router-search-03/04) and `validateSearch` feeding the loader.
- Two confident, materially wrong claims rather than omissions: "the route loader
  does not run during preload" (router-loaders-03) and "fetchQuery always performs a
  network request" (prefetch-02).
- No contamination signal: wording diverges from the reference throughout, and the
  biggest losses contradict the reference rather than echo it.
