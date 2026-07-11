# tanstack knowledge benchmark — answers
# Model: novita-ai/tencent/hy3   (provider-stripped id: tencent/hy3)
# CLOSED-BOOK via ocode run -dir <empty> (no corpus access). corpus_rev 1.

- id: tanstack-query-keys-01
  answer: |
    A query key is the unique identifier TanStack Query uses to store and look up a query's data in its cache. It is a serializable array (typically something like `['todos', 10]`). The client decides two keys refer to the same cache entry by deterministically serializing the key (stable stringify that sorts object keys and respects array order) and hashing it. Two keys map to the same entry only when their serialized form is identical — so array element order matters, values must match, and the same logical entity must always be represented identically. Keys are also how you target a query for actions like invalidation, cancellation, or manual updates.

- id: tanstack-query-keys-02
  answer: |
    Keys are structured hierarchically so that more specific queries are nested "under" a general prefix (e.g. `['todos']` is the parent of `['todos', todoId]`). `invalidateQueries`, `removeQueries`, and similar methods match on a key *prefix*: passing `['todos']` matches `['todos']`, `['todos', 1]`, `['todos', 2]`, etc., because the matcher checks "does the cached key start with this array." This lets you refetch an entire collection with one call, or refetch just one item with a more specific key, without enumerating every entry.

- id: tanstack-query-keys-03
  answer: |
    Two different users would resolve to the same cache entry, so one user's data would be cached and then shown to the other (or mixed across users) — stale/wrong data, and any refetch for user B clobbers user A's result. The rule: every variable that influences the query's *result* (inputs like IDs, filters, user/session context, pagination) MUST be part of the query key. The key is the query's "input signature," so `queryFn` must read only from the key (or from variables derived from it), never from an outside variable that isn't also in the key.

- id: tanstack-query-keys-04
  answer: |
    Exactly one network request fires. TanStack Query dedupes requests by query key: when the first `useQuery({ queryKey: ['user', 5] })` mounts it starts the fetch, and the second component subscribes to the *same* in-flight query instance rather than issuing its own request. The same happens across remounts and across components — one active observer group per key, one fetch. (Independent keys, or different argument values, would each fetch once.)

- id: tanstack-caching-01
  answer: |
    `staleTime` is how long after a successful fetch the data is considered "fresh." While fresh, the query will NOT refetch on mount, window focus, or reconnect — it just returns cached data. `gcTime` (formerly `cacheTime`) is how long *unused* (no active observers) query data is kept in the cache before it is garbage-collected and removed. They are orthogonal: `staleTime` controls refetch behavior; `gcTime` controls memory retention. You can have fresh-but-cached (won't refetch) and stale-but-cached (will refetch) and stale-but-gced (gone).

- id: tanstack-caching-02
  answer: |
    With `staleTime: 0` (the default) data is considered stale the instant it's fetched. When the component remounts it reads the cached data immediately (so there is no loading spinner — the old data renders instantly from cache), but because it's stale the query immediately triggers a background refetch and updates the UI when the new data arrives. User-visible behavior: instant render from cache, then a silent background refresh (no flicker of a spinner unless you choose to show one).

- id: tanstack-invalidation-01
  answer: |
    `invalidateQueries({ queryKey: ['todos'] })` marks every cached query whose key starts with `['todos']` as stale and (if the query is currently observed/active) immediately refetches it in the background. It does NOT delete the data — the old data stays visible until the refetch resolves. It also unpauses any queries that were paused. Think of it as "tell the cache this data is no longer trustworthy, and go re-fetch it if anyone is looking."

- id: tanstack-invalidation-02
  answer: |
    Use `setQueryData` when you already know the exact new value from the mutation's response and want to update the cache synchronously without a network round-trip (faster, no loading flash, useful for mutations whose effect is predictable). The tradeoff: you must compute the correct new cache shape yourself, and it can drift from the server if your local computation diverges from truth, whereas `invalidateQueries` always re-reads from the server (simpler, always authoritative, but costs another request and may flash). Often you use both: optimistic `setQueryData` during the mutation plus `invalidateQueries` in `onSettled` to reconcile.

- id: tanstack-mutations-01
  answer: |
    `useQuery` is for reads: it runs automatically on mount (subject to `enabled`) and tracks a remote *query* via `queryFn`. `useMutation` is for writes (create/update/delete) via a `mutationFn`; it does NOT run automatically — you trigger it by calling `mutate(variables)` (fire-and-forget) or `mutateAsync(variables)` (returns a promise). You track it via `isPending` (v5; this replaced v4's `isLoading`), `isError`, `isSuccess`, `error`, `data`, and the `onMutate/onSuccess/onError/onSettled` callbacks. In v5 the mutation status field is `isPending`, and `variables` holds the last input.

- id: tanstack-mutations-02
  answer: |
    Calling `queryClient.invalidateQueries(...)` in `onSuccess` tells TanStack Query to refetch the query/queries affected by the mutation so the cache reflects the new server state. It accomplishes keeping lists/details consistent after a write without manually computing new cache values — e.g. after adding a todo you invalidate `['todos']` so the list re-fetches and shows the new item. It trades a network request for correctness and simplicity.

- id: tanstack-mutations-03
  answer: |
    Correct optimistic update with rollback:
    - `onMutate(variables)`: 1) cancel outstanding queries for the affected key (`cancelQueries`), 2) snapshot the previous cache value so you can restore it, 3) optimistically call `setQueryData` to apply the expected change immediately, 4) return a context object containing the snapshot.
    - `onError(error, variables, context)`: restore the previous value via `setQueryData(context.previous..., snapshot)` so a failed mutation doesn't leave bogus data on screen.
    - `onSettled()`: invalidate the affected queries (`invalidateQueries`) to reconcile the cache with the server, regardless of success or failure.
    Each step's responsibility: `onMutate` = apply + snapshot, `onError` = rollback, `onSettled` = reconcile.

- id: tanstack-mutations-04
  answer: |
    `mutate(variables, options?)` fires the mutation and does NOT return a usable promise (it calls your `onSuccess`/`onError`/`onSettled` callbacks instead); it's convenient in event handlers. `mutateAsync(variables)` returns a Promise that *resolves* on success and *rejects* on failure, so you can `await`/`try-catch` it. A mutation knows it failed when its `mutationFn` throws (or returns a rejected promise) — that caught error becomes the mutation's `error` and flips `isError`/`isPending`. Note `fetch` does NOT throw on 4xx/5xx by itself, so you must `throw` on `!res.ok` inside the mutation/query fn for failures to register.

- id: tanstack-query-fn-01
  answer: |
    `fetch` resolves (does not reject) for HTTP 500 and other non-2xx responses — it only rejects on network failure. So the `queryFn` returns the 500 body as a normal value, and TanStack Query treats the fetch as *successful*, rendering the error payload as if it were data. Fix: inside the `queryFn`, check the response and throw on failure, e.g. `if (!res.ok) throw new Error(await res.text())` (or `await res.json()` then `throw`). Throwing is what makes Query transition to the error state (`isError`/`error`).

- id: tanstack-query-fn-02
  answer: |
    A `queryFn` should be a pure data-fetching function (input from the key/variables, output the data). Causing side effects like `setState` there is a mistake because it can run multiple times (retries, refocus, invalidation), runs outside React's render/commit cycle, and breaks the assumption that the query fn is deterministic per key. Instead, let the `queryFn` just return data, and perform side effects in `onSuccess`, the component body, a `useEffect` keyed on the data, or via `select` for derived values.

- id: tanstack-query-fn-03
  answer: |
    Express the dependency with the `enabled` option: `useQuery({ queryKey: ['user', userId], queryFn: ..., enabled: !!userId })`. Until `userId` is truthy the query is disabled. In that disabled state the query's `status` is `'pending'` (v5) / `'loading'` (v4) and its `fetchStatus` is `'idle'` — it does not run the `queryFn` and shows no data until enabled flips true. This avoids fetching with `undefined`/null and the "key doesn't include the variable" problem.

- id: tanstack-suspense-01
  answer: |
    `useSuspenseQuery` drops the loading/error states from the returned value and instead *suspends* (throws a promise) while fetching and *throws an error* on failure, while returning the data directly on success (no `data`/`isPending` fields to unwrap). Because it throws, the component using it MUST be wrapped in a React `<Suspense>` boundary (with a `fallback`) so React can show a loading state while the promise resolves. Error handling moves to an error boundary rather than the hook.

- id: tanstack-suspense-02
  answer: |
    With `useSuspenseQuery`, fetch errors are thrown out of the hook and surface at the nearest React error boundary (an `<ErrorBoundary>` / `componentDidCatch` or a library like `react-error-boundary`), not via a `isError`/`error` return value. You handle them by wrapping the component in an error boundary; the boundary's fallback receives the error and you can offer a retry (e.g. resetting the boundary and/or calling `queryClient.resetQueries`/invalidating).

- id: tanstack-suspense-03
  answer: |
    A request waterfall happens when one suspending component is nested inside another that also suspends: the parent suspends, its child can't start fetching until the parent's promise resolves and it renders, so fetches happen sequentially instead of in parallel. (Two siblings under a single Suspense boundary actually fetch in parallel because React renders them together.) To avoid the waterfall, fetch the independent data in parallel before render — e.g. kick off both in a router loader / `ensureQueryData`, or use `useSuspenseQueries` (which fetches all listed queries concurrently) instead of multiple separate `useSuspenseQuery` calls in a nested layout.

- id: tanstack-suspense-04
  answer: |
    No — you cannot build a *conditional* dependent query with `useSuspenseQuery({ enabled: false })` the way you would with `useQuery`. Suspense queries must be called unconditionally (React's rules of hooks require stable hook order, and a suspense query always suspends when it has no data), and `enabled: false` isn't a supported escape hatch for deciding whether to suspend. The correct pattern is to conditionally *render* the child component (and thus its `useSuspenseQuery`) only once the prerequisite is known, often gated by a parent that itself uses a normal `useQuery`/`enabled` check, or to restructure so the dependency is expressed before the Suspense boundary.

- id: tanstack-prefetch-01
  answer: |
    To guarantee the data is available before the component renders, use `queryClient.ensureQueryData(...)`. `ensureQueryData` returns a promise that resolves with the data (fetching only if it isn't already cached and fresh), so you can `await` it in the loader and be certain the cache is populated. `prefetchQuery` only *kicks off* a background fetch and resolves immediately/does not guarantee completion or return data — so if you need the data guaranteed-present before render you want `ensureQueryData`.

- id: tanstack-prefetch-02
  answer: |
    `fetchQuery` returns a promise that resolves with the data on success and **rejects** if the query function throws (e.g. a network/HTTP error) — so you must handle rejection. `prefetchQuery` is a "best-effort" wrapper: it calls `fetchQuery` but **catches and swallows** errors (logs them) so the caller never sees a rejection. The difference matters when a load failure should block navigation or be surfaced to the user: with `fetchQuery`/`ensureQueryData` you get the error to handle; with `prefetchQuery` the error is hidden and the route may render with empty/missing data.

- id: tanstack-router-loaders-01
  answer: |
    A route `loader` is an async function defined on a route that runs *during navigation, before the component renders*, and returns the data that route needs. The component reads it with `useLoaderData()` (typed to the loader's return). Unlike a `useEffect` fetch, the loader prevents the render until data is ready (no loading flash), runs on the server/browser consistently, participates in preloading and navigation blocking, and integrates with the router's error handling — so the UI never mounts in an empty "fetching" state the way a `useEffect` approach does.

- id: tanstack-router-loaders-02
  answer: |
    Inside the loader, fetch the data through the *same* `QueryClient` instance the components use (e.g. call `queryClient.ensureQueryData({ queryKey, queryFn })` or `fetchQuery`). The loader populates the shared Query cache; then the component's `useQuery`/`useSuspenseQuery` with the same key reads that cached entry instead of refetching — one cache, one request. To make this work across SSR/hydration you also bridge with `dehydrate`/`Hydrate` (or the router's integration), but the key idea is "fetch into the QueryClient in the loader, read from it in the component."

- id: tanstack-router-loaders-03
  answer: |
    TanStack Router preloads a route when the user hovers/focuses a `<Link>` (and on intent), fetching the route's data and code so that clicking navigates instantly with data already in hand. During a preload, the route's `loader` runs (and the route module/component is loaded) so the data is warmed in the cache. `beforeLoad` also runs on preload. The preloaded data sits in the cache until the user actually navigates (or is GC'd), so the real navigation then renders without a fetch delay.

- id: tanstack-router-loaders-04
  answer: |
    `beforeLoad` runs *before* the `loader` on every navigation/preload and is used for things like auth/guard checks, redirects, and contributing to the router **context** (it can return values that are merged into `context`). `loader` runs after `beforeLoad` and is where you fetch the route's data. "Router context" is a shared, typed object threaded through the router (`createRouter({ context })` and built up via each route's `beforeLoad` return), accessible to `beforeLoad`, `loader`, and components — useful for injecting things like an authenticated user or the QueryClient without prop drilling.

- id: tanstack-router-search-01
  answer: |
    You define a `validateSearch` function on the route that takes the raw search record (string values) and returns a parsed, validated object — commonly with Zod (`validateSearch: z.object({ page: z.number().catch(1) })`). TanStack Router runs it to produce typed, parsed `search` for that route, which flows into `useSearch()` and loader args. Validate because raw URL search params are always strings and untrusted; validating coerces types, supplies defaults, and rejects/normalizes bad input so the rest of the app can rely on a known shape instead of re-parsing everywhere.

- id: tanstack-router-search-02
  answer: |
    Store UI filters that belong in the URL as search params and update them via router navigation — e.g. `navigate({ search: (prev) => ({ ...prev, page: 2 }) })` or `setSearchParams`/`useNavigate` with `search`. This keeps the URL shareable/bookmarkable and back/forward works. The anti-pattern is keeping the filter in local React component state (`useState`): the URL and UI desync, links can't deep-link to a filter, and the browser back button won't restore previous filters.

- id: tanstack-router-search-03
  answer: |
    The loader does receive the search object once `validateSearch` is defined (the loader's argument includes `search`), so the premise is really "you must opt in." The reason the loader won't *see a typed/parsed* search param automatically is that without `validateSearch` the router doesn't know the shape, and even with it you must read it from the loader's `({ search })` argument rather than relying on a generic param. So you add `validateSearch` to the route and then read `search` (e.g. `search.page`) from the loader's context object. (If you need the search inside `beforeLoad`/deeper context, pass it through router context via `beforeLoad`'s return.)

- id: tanstack-router-search-04
  answer: |
    Once `validateSearch` is configured, any `<Link>` or `navigate` to that route is compile-time checked for its search params: the required search fields must be present with the correct types, optional ones are typed, and providing unknown or wrongly-typed search keys is a TypeScript error. This gives end-to-end type safety from the URL definition through links and `useSearch()`, so a missing/wrong `page` or similar is caught at build time.

- id: tanstack-router-typesafety-01
  answer: |
    File-based routing auto-discovers your route files and generates `routeTree.gen.ts`, a single typed tree that enumerates every route, its path params (`$param`), its `validateSearch` shape, and its loader data. The router is created from this generated tree, so TypeScript can infer the full route graph. `routeTree.gen.ts` is the generated (committed or built) file that powers compile-time checking of `to`, dynamic `params`, `search`, and `useLoaderData` across the whole app — whenever you add/rename routes you regenerate it and the types update everywhere.

- id: tanstack-router-typesafety-02
  answer: |
    That `<Link to="/posts/$postId" params={{ postId: '5' }} />` is checked at compile time to ensure: (1) `to="/posts/$postId"` is a valid, known route in the generated route tree; (2) the required dynamic param `postId` is provided (and has the correct type — here a string, so `'5'` is valid while omitting it or passing a wrong shape is an error); (3) no unknown/extra params are passed; and (4) the target route's search params (if any required) are satisfied. Unknown routes, missing params, or wrong types all fail the build.
