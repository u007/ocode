---
name: tanstack-tuning-nvidia-nemotron-3.5-super-vl
description: >
  Corrective TanStack Query + Router knowledge for nvidia/nemotron-3.5-super-vl,
  targeting its query-keys, suspense, prefetch, router-loaders and router-search
  gaps: how query keys are hashed and that cache state is per key, the
  Suspense + ErrorBoundary pairing behind useSuspenseQuery, fetchQuery and
  preloading both respecting the cache, what actually runs on hover preload,
  router context as the typed tree-threaded carrier of the QueryClient,
  loaderDeps as the only way a loader sees search params, and the TanStack
  Router search API names (not React Router's).
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves to
  exactly `nvidia/nemotron-3.5-super-vl` (e.g.
  `openrouter/nvidia/nemotron-3.5-super-vl` → `nvidia/nemotron-3.5-super-vl`;
  a `:free` route suffix is not part of the id) AND the repository is a
  TanStack project (`@tanstack/react-query` or `@tanstack/react-router` dep
  present — per meta.yaml detection). For any other model or non-TanStack repo,
  do not load.
tuned_for: nvidia/nemotron-3.5-super-vl
tuned_version: "3.5"
stack: tanstack
source_scorecard: ../scores/nvidia__nemotron-3.5-super-vl.md
threshold: 0.85
revalidate_when: model_version changes
---
# TanStack corrections for nvidia/nemotron-3.5-super-vl

<!-- kaizen:digest -->
## query-keys: deterministic hashing, per-key state

- A query key is a serializable array hashed **deterministically**: array order
  matters, but **plain-object key order does not** (object keys are sorted before
  hashing), so `['todos', { a: 1, b: 2 }]` and `['todos', { b: 2, a: 1 }]` are one
  cache entry. Never describe key equality as "deeply equal in the same order"
  without that object-key exception.
- Everything is tracked **per key, not per component**: concurrent observers of
  one key share one in-flight request (dedup), one cached result, one `staleTime`
  freshness clock and one `gcTime` timer. When asked "how many requests", give
  the count AND say that cache, freshness and GC are shared across all observers
  of that key.

## suspense: say what `data` is, and pair both boundaries

- `useSuspenseQuery` returns `data` **guaranteed defined** (never `undefined`);
  there is no `isPending`/`isLoading` or `isError` branch in the component. State
  the type-level guarantee, not only "it suspends".
- A thrown `queryFn` error propagates during render to the nearest React
  `<ErrorBoundary>`. Always name the pairing: `<Suspense fallback>` owns loading,
  `<ErrorBoundary>` (or the route's `errorComponent` — TanStack Router has no
  `errorElement`) owns failure, both wrapping the suspending component.
- Say the precondition out loud: the `queryFn` must still **throw** on a bad
  response (`fetch` resolves on 4xx/5xx). If it resolves with the error body, the
  boundary never fires and the error renders as data.

## prefetch: every imperative fetch respects `staleTime`

- `fetchQuery`, `prefetchQuery` and `ensureQueryData` all read the cache first and
  skip the network while the entry is **fresh** per `staleTime`. Never say
  `fetchQuery` "always performs a network request".
- The axis that differs is the return: `fetchQuery` returns the data and
  **rejects** on failure (await it, handle the error); `prefetchQuery` returns
  `void` and **swallows** errors (best-effort warming); `ensureQueryData` returns
  cached-or-fetched data and is the one to `await` in a loader.
- Route preloading on hover is a cache-respecting fetch too: it runs the route's
  `loader` (see below), gated by `staleTime` / loader caching, so warm data is not
  refetched.

## router-loaders: preload runs the loader; context carries the QueryClient

- With intent preloading (`defaultPreload: 'intent'`) hovering or touching a
  `<Link>` runs the target route's **`beforeLoad` and `loader`** ahead of the
  click, warming its data (and any `ensureQueryData` cache). It does not merely
  download the route's code chunk. Because `beforeLoad` runs, auth guards and
  redirects run during preload too.
- Router `context` is a typed object (e.g. `{ queryClient, auth }`) supplied at
  `createRouter` and **threaded down the route tree**: whatever a route's
  `beforeLoad` returns is merged into `context` for every descendant's
  `beforeLoad`, `loader` and component. That merge is how services are injected
  and routes are guarded.
- Query + Router integration: put the shared `QueryClient` on router `context`,
  call `context.queryClient.ensureQueryData(queryOptions)` in the loader, and use
  the **same `queryOptions`** in `useSuspenseQuery` in the component. Name the
  context mechanism, not just "the same key".
- The loader argument has `params`, `deps`, `context`, `location`,
  `abortController`, `cause`, `preload`, `route`, `navigate`. It has **no
  `search` field**: search values arrive only through `deps` (via `loaderDeps`).

## router-search: TanStack Router's own API, and `loaderDeps` for loaders

- Read search params with the route's typed **`Route.useSearch()`** (or
  `useSearch({ from })`); write them with typed **`navigate({ search: prev =>
  ({ ...prev, page: 2 }) })`** or **`<Link search={...}>`**. `useSearchParams`
  and `setSearchParams` are React Router APIs and do not exist in TanStack
  Router — never cite them. The URL is the single source of truth; do not mirror
  it into `useState` and hand-sync.
- A loader is keyed and re-run on **path params only** by default. Search params
  are not loader dependencies, and `validateSearch` does **not** change that: it
  parses the query string into the typed object for `useSearch()`, `Link search`
  and `navigate({ search })` — it never passes search into the loader.
- To fetch in a loader from a search param declare
  `loaderDeps: ({ search }) => ({ page: search.page })`. The returned object
  becomes the loader's `deps` argument **and** part of its cache identity, so the
  loader receives `deps.page` and reloads when it changes. Name `loaderDeps`
  whenever the task is "loader needs a search param".
- Type-safety payoff of `validateSearch`: `<Link to>` and `navigate()` to that
  route require a schema-matching `search`, `useSearch()` returns the fully typed
  object, and renaming or retyping a search field is a **compile error at every
  read and write site**, not a runtime bug. There is no typed "loader search
  arg" to mention.
<!-- /kaizen:digest -->
