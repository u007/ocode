---
name: tanstack-tuning-mimo-v2.5
description: >
  Corrective TanStack Router knowledge for mimo-v2.5, targeting the
  router-search gaps this model showed on the closed-book tanstack benchmark
  (reading search params via useSearch(), loaderDeps for search-driven
  loaders, and the type-safety surface useSearch() gets from validateSearch).
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves to
  exactly `mimo-v2.5` AND the repository is a TanStack project (`@tanstack/react-query`
  or `@tanstack/react-router` dep present — per meta.yaml detection).
  For any other model or non-TanStack repo, do not load.
tuned_for: mimo-v2.5
tuned_version: "2.5"
stack: tanstack
source_scorecard: ../scores/mimo-v2.5.md
threshold: 0.75
revalidate_when: model_version changes
---

# TanStack corrections for mimo-v2.5

mimo-v2.5 is strong across almost the entire stack (90% overall; query-keys,
caching, invalidation, mutations, query-fn, suspense, prefetch,
router-loaders, and router-typesafety are all ≥ 0.78). The one area below
threshold is **router-search** (subscore 0.61). The sections below target
only the specific mistakes it made there — everything else it already knows,
so nothing else is restated here.

## router-search: reading is not the same as writing

mimo-v2.5 consistently over-indexes on the *write* side of search params
(`navigate({ search: ... })`) and drops the *read* side and the type-linkage
between them. Always cover both halves.

- URL search state has two APIs, not one: **read** with the route's
  `useSearch()` (or `Route.useSearch()`), **write** with typed `navigate()` /
  `<Link search={...}>`. An answer that only describes updating search params
  and never names `useSearch()` is incomplete — state both.
- The anti-pattern to name explicitly: mirroring URL-owned filter state into
  `useState`/`useReducer` and hand-syncing it to the URL. This desyncs and
  throws away shareability/back-button/deep-linking — say why, not just "put
  it in the URL."

## router-search: loaders don't see search params for free — name `loaderDeps`

mimo-v2.5 conflated **typing** search params (`validateSearch`) with **loader
re-run tracking**, and never mentioned `loaderDeps` at all. These are
different mechanisms:

- By default, a loader is only keyed/reloaded on **path params**. Search
  params are NOT tracked as loader dependencies even after `validateSearch`
  is defined — `validateSearch` only makes the *type* available, it does not
  make the loader re-run when the search value changes.
- To make a loader see and react to a search param, declare
  `loaderDeps: ({ search }) => ({ page: search.page })`. Those returned
  values become part of the loader's `deps` argument and its cache identity,
  so the loader receives the value and reloads when it changes.
- Do not describe "the loader receives `search` on its argument, typed via
  `validateSearch`" as the fix — that explains typing, not reload tracking.
  `loaderDeps` is the actual mechanism the question is testing.

## router-search: what validateSearch buys `useSearch()` specifically

When asked what type-safety `validateSearch` unlocks app-wide, always name
`useSearch()` as one of the typed surfaces, not just `<Link>`/`navigate`:

- `Route.useSearch()` returns the fully validated/typed search object — not
  just `<Link to>`/`navigate()` accepting a schema-shaped `search` prop.
- Say explicitly that renaming or changing a search field in the schema
  surfaces as **compile errors at every navigation and every read site**
  (`useSearch()` call sites included) — the type-safety guarantee is
  end-to-end, covering both writing (Link/navigate) and reading (useSearch),
  not just one side.
