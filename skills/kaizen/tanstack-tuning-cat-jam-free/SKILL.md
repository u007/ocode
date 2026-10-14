---
name: tanstack-tuning-cat-jam-free
description: >
  Corrective TanStack knowledge for cat-jam-free, targeting its query-keys,
  caching, query-fn and router-typesafety gaps: how query keys are hashed
  (object key order ignored), per-key cache state shared by observers,
  staleTime vs gcTime as independent axes with their defaults, the
  throw-to-fail contract behind mutations and suspense errors, and what the
  generated route tree actually type-checks.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves to
  exactly `cat-jam-free` AND the repository is a TanStack project
  (`@tanstack/react-query` or `@tanstack/react-router` dep present — per
  meta.yaml detection). For any other model or non-TanStack repo, do not load.
tuned_for: cat-jam-free
tuned_version: "alpha"
stack: tanstack
source_scorecard: ../scores/cat-jam-free.md
threshold: 0.85
revalidate_when: model_version changes
---
# TanStack corrections for cat-jam-free

Answer the whole question. Each rule below is a second half that tends to get
dropped; state it explicitly.

## query-keys: deterministic hashing, per-key state

- A query key is a serializable array. The client hashes it deterministically:
  **array order matters, object key order does not** (object keys are sorted
  before hashing), so `['todos', { a: 1, b: 2 }]` and `['todos', { b: 2, a: 1 }]`
  are the same cache entry. Never describe key equality as "deeply equal in the
  same order" without that object-key exception.
- Everything is tracked **per key, not per component**: concurrent observers of
  one key share one in-flight request (dedup), one cached result, one
  `staleTime` freshness clock and one `gcTime` timer. When asked "how many
  requests", give the count AND say that cache, freshness and GC are shared
  across all observers of that key.

## caching: staleTime and gcTime are independent axes

- `staleTime` = how long data is fresh (controls whether mounts/focus/reconnect
  refetch). `gcTime` (v4 `cacheTime`) = how long an **inactive** query's data is
  retained after its last observer unmounts. They are independent: one governs
  refetching, the other memory retention. Data can be stale but still cached,
  and fresh data is never garbage-collected while observed.
- Always state the defaults: `staleTime: 0` (always stale, so every remount /
  window refocus refetches in the background) and `gcTime: 5 minutes`. Raising
  `staleTime` suppresses those background refetches while data is fresh.

## query-fn: failure means throw or reject

- A query or mutation fails **only** when its `queryFn` / `mutationFn` throws or
  returns a rejected Promise. Returning an error object or a resolved
  `Response` with `ok === false` is success. When asked "how does it know it
  failed", give this cause first; `isError`, `error`, `onError` and a rejected
  `mutateAsync` are the consequences, not the mechanism.
- With `useSuspenseQuery` the thrown error propagates during render to the
  nearest `<ErrorBoundary>`. Say both halves: pair `<Suspense fallback>`
  (loading) with `<ErrorBoundary>` (failure) around the component, and the
  `queryFn` must still throw on a bad response or the boundary never fires.

## router-typesafety: what the generated tree checks

- Each route file exports `createFileRoute('/path')`; the Router plugin/CLI
  generates `routeTree.gen.ts`, registering every path with its params and
  validated search types. The router is typed from that tree.
- Spell out the payoff: `<Link to>`, `navigate()`, `useParams()` and
  `useSearch()` are compile-checked against real routes. A non-existent path,
  a missing or unknown param (`$postId`), or a search value that doesn't match
  `validateSearch` is a **type error, not a runtime 404**.
- Refactors propagate: renaming a route path, param or search field breaks
  every `Link`/`navigate`/read site at compile time. Mention `navigate` and
  `search` alongside `Link` whenever describing typed navigation.
