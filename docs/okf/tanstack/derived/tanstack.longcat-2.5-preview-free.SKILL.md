---
name: tanstack-tuning-longcat-2.5-preview-free
description: >
  Corrective TanStack Query + Router knowledge for longcat-2.5-preview-free,
  targeting query-keys (deterministic key hashing, per-key cache state,
  hierarchy for broad AND precise invalidation), router-loaders (router
  context as a typed tree-threaded object carrying the QueryClient, loaderDeps,
  preload respecting caching) and router-search (loaderDeps vs validateSearch,
  reading via useSearch(), untrusted-URL rationale, compile-time propagation)
  and router-typesafety (typed useLoaderData, the full typed navigation surface,
  refactors surfacing at every call site).
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `longcat-2.5-preview-free` (e.g.
  `opencode-go/longcat-2.5-preview-free` → `longcat-2.5-preview-free`) AND the
  repository is a TanStack project (`@tanstack/react-query` or
  `@tanstack/react-router` dep present — per meta.yaml detection). For any
  other model or non-TanStack repo, do not load.
tuned_for: longcat-2.5-preview-free
tuned_version: "2.5-preview"
stack: tanstack
source_scorecard: ../scores/longcat-2.5-preview-free.md
threshold: 0.75
revalidate_when: model_version changes
---
# TanStack corrections for longcat-2.5-preview-free

<!-- kaizen:digest -->
**Query keys are hashed stably, not by a plain `JSON.stringify`:** plain-object keys are sorted before serialization, so `{ a: 1, b: 2 }` and `{ b: 2, a: 1 }` are the SAME entry; array order DOES matter. Freshness (`staleTime`) and garbage collection (`gcTime`) are tracked per key and shared by every observer of that key. Hierarchical keys (generic → specific) buy both broad (`['todos']`) and precise (`['todos', id]`) invalidation.

**Loaders don't track search params:** a loader re-runs only on path params by default. `validateSearch` types the search object; it does NOT make the loader see or reload on it. Declare `loaderDeps: ({ search }) => ({ page: search.page })` — the loader reads them from `deps`, and they become part of its cache identity.

**Router context:** a typed object (e.g. `{ queryClient, auth }`) created at `createRouter`, threaded down the route tree; whatever `beforeLoad` returns is merged into it for descendant routes. Put the shared `QueryClient` there and call `context.queryClient.ensureQueryData(opts)` in the loader, with the same `queryOptions` in the component. Preloading on hover still respects `staleTime`/loader caching.

**Search params are state with two sides:** read with `Route.useSearch()` (fully typed from `validateSearch`), write with typed `navigate({ search })` / `<Link search>`. Validate because the URL is untrusted, user-editable input. Changing the schema causes compile errors at every read and write site.
**Router type safety is end-to-end:** `useLoaderData()` is typed from the loader's return; `Link`, `navigate`, params and search are all checked against the generated route tree. Renaming a path, param or search field surfaces as compile errors at every call site.
<!-- /kaizen:digest -->

## query-keys: how keys map to cache entries

- Keys are serializable arrays hashed **stably**: serialization sorts
  plain-object keys first, so property order inside an object never creates
  a new entry; element order inside the array does. Saying only "keys match
  when their `JSON.stringify` output matches" is incomplete — always state the
  object-key normalization and that array order matters.
- A key identifies one `Query` in the cache. Concurrent observers of the same
  key share one in-flight request (dedup), AND share that key's freshness
  (`staleTime`) and garbage-collection (`gcTime`) timers. The cache state is
  per key, not per component.
- Structure keys generic → specific (`['todos']`, `['todos', 'list', filters]`,
  `['todos', 'detail', id]`). State both payoffs: a prefix invalidates the
  whole subtree, and a full key targets one item precisely.

## router-loaders: context, the QueryClient, and preload

- Router `context` is a typed dependency object, supplied at `createRouter`
  (declared via `createRootRouteWithContext`) and threaded down the whole
  route tree to every child's `beforeLoad`, `loader` and component. A
  `beforeLoad` return value is **merged** into context for descendant routes
  — that is how guards inject `auth`/services.
- Query + Router integration: put the single shared `QueryClient` on router
  context; in the loader `await context.queryClient.ensureQueryData(opts)`;
  in the component use `useSuspenseQuery(opts)` / `useQuery(opts)` with the
  **same `queryOptions`**. One client, one cache entry, no double fetch.
- Intent preload runs `beforeLoad` + `loader` early, and it respects
  `staleTime`/loader caching, so fresh data is not refetched on every hover.
- A loader only sees what it is keyed on. Search-driven loaders need
  `loaderDeps` (see below).

## router-search: loaderDeps, reading, and why validate

- `validateSearch` ≠ loader dependency tracking. By default a loader re-runs
  only when **path params** change. To use a search param in a loader, declare
  `loaderDeps: ({ search }) => ({ page: search.page })`; the loader receives
  those values as `deps` and reloads when they change. Never say the loader
  gets search "automatically" or "via validateSearch".
- Read search state with `Route.useSearch()`; write it with
  `navigate({ search: prev => ({ ...prev, page: 2 }) })` or
  `<Link search={...}>`. Name both sides. The URL is the single source of
  truth; never mirror it into `useState` and hand-sync.
- Why validate: the URL is **untrusted, user-editable input**. Validation
  (often a Zod schema with defaults/`.catch()`) coerces strings, recovers
  from garbage or missing values, and gives the route a typed search object.
- Type-safety payoff: `useSearch()` returns the validated type, `Link`/
  `navigate` must pass a schema-matching `search`, and renaming or changing a
  field produces compile errors at **every** read and navigation site.

## router-typesafety: name the whole typed surface

- `useLoaderData()` is typed: the loader's return type flows to the component
  with no manual annotation. Say "typed" explicitly when describing it.
- Typed navigation is not just `<Link to>`: `navigate()`, `params`, required
  `search`, `useParams()` and `useSearch()` are all checked against the
  generated `routeTree.gen.ts` (built from `createFileRoute` files).
- State the refactor payoff: renaming a route path, a `$param` or a search
  field turns every stale `Link`/`navigate`/read site into a compile error, not
  a runtime 404.
