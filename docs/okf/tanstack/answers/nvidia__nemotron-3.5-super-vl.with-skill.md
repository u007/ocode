# nvidia/nemotron-3.5-super-vl — with-skill answers for tanstack (openrouter, 2026-10-16); answerer saw only _prompts/tanstack.md; derived/tanstack.nvidia__nemotron-3.5-super-vl.SKILL.md body (front matter stripped) prepended to the sheet as active guidance
- id: tanstack-query-keys-01
  answer: |
    A query key is a serializable array (e.g. `['todos', 5]`) that identifies a query's cache entry. TanStack Query decides identity by hashing the key deterministically: two keys refer to the same cache entry when their stable hashes match. Array order matters, and nested plain objects are normalized (object keys sorted) before hashing, so `['todos', { a: 1, b: 2 }]` and `['todos', { b: 2, a: 1 }]` resolve to one cache entry.
- id: tanstack-query-keys-02
  answer: |
    Structure keys from broad to specific (`['todos']`, `['todos', todoId]`, `['todos', todoId, 'detail']`) so related data shares a key prefix. That hierarchy interacts with invalidation because `invalidateQueries({ queryKey: ['todos'] })` matches every query whose key starts with `['todos']` (unless `exact: true` is set), so invalidating `['todos']` marks `['todos']`, `['todos', 5]`, and deeper nested keys all stale at once.
- id: tanstack-query-keys-03
  answer: |
    The query is keyed only by `['projects']` but its `queryFn` depends on `userId`, so changing `userId` does not create a new cache entry or trigger a refetch: one user's projects can be served to another, or stale data for a previous user is returned. The rule is that every value the `queryFn` reads must be part of the query key — the key must capture all of the query's dependencies.
- id: tanstack-query-keys-04
  answer: |
    One network request fires. TanStack Query deduplicates by key: the second component subscribes to the same in-flight query and both observers share one cached result. Cache, freshness (`staleTime`) and GC (`gcTime`) are tracked per key, not per component, so all observers of `['user', 5]` see the same data and refresh together.
- id: tanstack-caching-01
  answer: |
    `staleTime` is how long cached data is considered fresh — within that window, mounting components read from cache without refetching (default 0, i.e. immediately stale). `gcTime` (formerly `cacheTime`) is how long an unused query stays in memory before garbage collection — the clock runs only while the query has no observers (default 5 minutes). They are independent axes: freshness controls refetching, retention controls cache lifetime.
- id: tanstack-caching-02
  answer: |
    With `staleTime: 0` the data is already stale, so on remount TanStack Query shows the cached value immediately (if any) and fires a background refetch (`refetchOnMount` defaults to true). User-visible behavior: the screen paints old data right away, then updates when the refetch resolves; if there is no cache, the user sees a loading state briefly.
- id: tanstack-invalidation-01
  answer: |
    It marks every query whose key matches `['todos']` (including nested keys like `['todos', 5]` unless `exact: true`) as stale by bumping their staleness timestamp, then actively refetches the ones with active observers. Inactive queries are not refetched immediately; they refetch on next mount. It does not delete or overwrite cached data — that is `removeQueries`.
- id: tanstack-invalidation-02
  answer: |
    Use `setQueryData` when you know the exact new result (e.g. the created/edited item) and want the UI to update instantly without waiting for the network; the tradeoff is the cache may diverge from the server unless you keep it in sync manually. Use `invalidateQueries` when you want guaranteed server truth: it triggers a background refetch, so updates are slower but always consistent with the server.
- id: tanstack-mutations-01
  answer: |
    `useQuery` declares read data and fetches automatically on mount; `useMutation` is for server writes (create/update/delete) and runs only when you call it imperatively via `mutate` or `mutateAsync`. You track it with the returned `isPending` (v5; `isLoading` in v4), `isError`, `isSuccess`, `error`, `data`, `variables`, and `reset`. v5 statuses are `idle | pending | success | error`.
- id: tanstack-mutations-02
  answer: |
    Calling `queryClient.invalidateQueries` in `onSuccess` marks related queries stale so they refetch and pick up the mutation's result (e.g. after creating a todo, invalidate `['todos']` so the list refreshes). It accomplishes cache consistency without manually reconstructing each query's data with `setQueryData`.
- id: tanstack-mutations-03
  answer: |
    `onMutate` cancels outgoing refetches for the affected queries (`cancelQueries`), snapshots their previous data (`getQueryData`), applies the optimistic update (`setQueryData`), and returns a context (typically the snapshot). `onError` rolls back by restoring the snapshot from that context (`setQueryData(key, previous)`). `onSettled` runs regardless of outcome and is where you `invalidateQueries` to reconcile with server truth or clear the rollback snapshot.
- id: tanstack-mutations-04
  answer: |
    `mutate` is fire-and-forget (returns void; errors go through `onError`), while `mutateAsync` returns a promise that resolves with the data or rejects with the error, so you can `await`/catch it. A mutation knows it failed when its `mutationFn` throws or rejects: status becomes `error`, `error` is set, and `onError` fires.
- id: tanstack-query-fn-01
  answer: |
    `fetch` only rejects on network errors — a 500 still resolves, so if the `queryFn` returns `response.json()` without checking `response.ok`, the error body is treated as data and no error ever surfaces. The fix is to throw on bad responses: `if (!response.ok) throw new Error(...)` (or throw the response), so QueryClient receives a real failure.
- id: tanstack-query-fn-02
  answer: |
    A `queryFn` must be a pure data fetch: side effects like `setState` can run multiple times (retries, StrictMode double-invoke, background refetches), break stale closures, cause loops, and never trigger the right re-render. Instead return the data and let the component render from the query result; side effects belong in event handlers, `useEffect`, or mutations.
- id: tanstack-query-fn-03
  answer: |
    Express it with `enabled: !!userId` (or `userId != null`) and include `userId` in the query key. Until the condition is true the query stays disabled — it does not fetch, `isPending`/`isFetching` are false, and `data` is `undefined`; once `userId` is known it becomes active, fetches, and refetches whenever `userId` changes.
- id: tanstack-suspense-01
  answer: |
    `useSuspenseQuery` suspends while loading (throwing a promise) instead of returning `isPending`/`isLoading`, and it returns `data` with a type-level guarantee that it is defined — there is no loading or error branch in the component. The component must be wrapped by `<Suspense fallback>` for loading and an `<ErrorBoundary>` for failures.
- id: tanstack-suspense-02
  answer: |
    Fetch errors propagate during render to the nearest React `<ErrorBoundary>` (or the route's `errorComponent` in TanStack Router, which has no `errorElement`). Handle them by wrapping the suspending component in both boundaries: `<Suspense fallback>` owns loading, `<ErrorBoundary>` owns failure. The precondition is that `queryFn` must throw on bad responses — `fetch` resolves on 4xx/5xx, so check `response.ok` and throw, otherwise the error renders as data and the boundary never fires.
- id: tanstack-suspense-03
  answer: |
    Each component suspends independently, so the second sibling does not start fetching until the first resolves and renders — a sequential request waterfall. Avoid it by fetching both in advance (e.g. `ensureQueryData`/`fetchQuery` in a shared parent or route loader) so the data is already cached when the components render, or by kicking off both fetches before either suspends.
- id: tanstack-suspense-04
  answer: |
    No — `useSuspenseQuery` cannot be made conditional with `enabled: false`; a suspense query must always suspend until data exists, so there is no meaningful disabled state. For dependent data, gate rendering higher up (do not render the component until the dependency is ready) or use `useQuery` with `enabled` for the non-suspense case.
- id: tanstack-prefetch-01
  answer: |
    Use `ensureQueryData`: it reads the cache first, fetches only if stale, and resolves with the cached-or-fetched data, so you `await` it in the loader and the component can read it synchronously afterward. `prefetchQuery` returns `void` and is best-effort warming — it does not guarantee data is available when the component renders.
- id: tanstack-prefetch-02
  answer: |
    `prefetchQuery` returns `void` and swallows errors (best-effort cache warming), while `fetchQuery` returns the data and rejects on failure, so you must await and handle the error. The difference matters when failure must be surfaced (e.g. inside a loader) versus when warming is optional and a failed fetch is acceptable.
- id: tanstack-router-loaders-01
  answer: |
    A route `loader` is an async function on the route definition that runs before the component during navigation and returns `loaderData`, which the component reads typed via `route.data` / `useLoaderData()`. Unlike a `useEffect` fetch, the data is ready on first render with no loading state or mount-time waterfall, and it is tied to the navigation lifecycle rather than component mount.
- id: tanstack-router-loaders-02
  answer: |
    Put the shared `QueryClient` on router `context` (a typed object supplied at `createRouter` and threaded down the route tree). In the loader, `await context.queryClient.ensureQueryData(queryOptions)`, and in the component call `useSuspenseQuery` with the **same `queryOptions`** (same key). The loader warms/reads the same cache the component reads from, so there is no double fetch.
- id: tanstack-router-loaders-03
  answer: |
    With default intent preloading, hovering or touching a `<Link>` runs the target route's `beforeLoad` and `loader` ahead of the click, warming its data (including any `ensureQueryData` cache) — not just downloading the route's code chunk. Because `beforeLoad` runs during preload, auth guards and redirects also run then.
- id: tanstack-router-loaders-04
  answer: |
    `beforeLoad` runs first and is for guards/redirects — it can abort navigation, authenticate, and return data merged into `context` for descendants. `loader` runs after `beforeLoad` and produces the route's `loaderData`. Router `context` is a typed object (e.g. `{ queryClient, auth }`) supplied at `createRouter`; whatever a route's `beforeLoad` returns is merged into `context` for every descendant's `beforeLoad`, `loader`, and component — that is how services are injected and routes guarded.
- id: tanstack-router-search-01
  answer: |
    Define `validateSearch` on the route (e.g. a Zod schema parsing the raw query string), then read the params with the route's typed `Route.useSearch()` or `useSearch({ from })`. Validation converts `string | undefined` URL values into a typed object, so reads are type-safe and invalid values are coerced/rejected instead of leaking into components.
- id: tanstack-router-search-02
  answer: |
    Treat the URL as the single source of truth: read with `useSearch()` and update with typed `navigate({ search: prev => ({ ...prev, page: 2 }) })` or `<Link search={{ page: 2 }} />`. The anti-pattern is mirroring search params into `useState` and hand-syncing with effects, which desyncs the URL, causes extra renders, and loses shareable/bookmarkable links.
- id: tanstack-router-search-03
  answer: |
    Loaders are keyed and re-run on path params only by default; search params are not loader dependencies, and `validateSearch` only types reads/writes (`useSearch`, `Link search`, `navigate({ search })`) — it never passes search into the loader. Add `loaderDeps: ({ search }) => ({ page: search.page })`; its result becomes the loader's `deps` argument and part of its cache identity, so the loader receives `deps.page` and re-runs when it changes.
- id: tanstack-router-search-04
  answer: |
    `<Link to>` and `navigate()` to that route require a schema-matching `search` at compile time, `useSearch()` returns the fully typed object, and renaming or retyping a search field is a compile error at every read and write site. There is no typed "loader search arg" — type safety covers links, navigation, and reads, not the loader directly.
- id: tanstack-router-typesafety-01
  answer: |
    File-based routing (`src/routes`) builds a typed route tree where each route's path, params, and search are known statically; `routeTree.gen.ts` is the generated artifact encoding that tree. It powers end-to-end type safety: `to`, `params`, and `search` in `Link`/`navigate`/`useSearch`/`useLoaderData` are checked against the actual route definitions.
- id: tanstack-router-typesafety-02
  answer: |
    `<Link to="/posts/$postId" params={{ postId: '5' }} />` checks at compile time that the route exists, that `postId` is provided and matches the route's param type, and that there are no missing or extra params. A missing `postId`, wrong type, or unknown route is a compile error; if the route has validated search, the `search` must also match the schema.
