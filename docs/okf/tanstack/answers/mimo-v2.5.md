- id: tanstack-query-keys-01
  answer: |
    A query key is an array of serializable values that uniquely identifies a piece of cached data. The client uses a deep equality comparison (by default) across the entire key array to determine whether two keys refer to the same cache entry. Keys are JSON-serialised internally for lookup, so `[1, 2]` and `[1, 2]` match but `[1]` and `[1, undefined]` do not. You can provide a custom `queryKeyHashFunction` to override this comparison if needed.

- id: tanstack-query-keys-02
  answer: |
    Hierarchical keys let you match at different levels of specificity during invalidation. For example, `queryClient.invalidateQueries({ queryKey: ['todos'] })` invalidates every query whose key starts with `['todos']`, including `['todos', todoId]`, `['todos', { filter: 'done' }]`, etc. This is a prefix match. So a broad key acts as a namespace: you can invalidate everything in a category with the parent key, or target a single entry with the full key. This pattern keeps invalidation composable and avoids manual bookkeeping.

- id: tanstack-query-keys-03
  answer: |
    The query will always return the same cached (or refetched) data regardless of which `userId` is current, because the cache lookup uses the key `['projects']`, not the `userId` variable. Two different users would silently share one cache entry. The rule is: every value that the `queryFn` depends on must appear in the query key so the cache is keyed by the full set of inputs.

- id: tanstack-query-keys-04
  answer: |
    Only one network request fires. TanStack Query deduplicates in-flight requests by key. Both components share the same `['user', 5]` cache entry; a single fetch is initiated and both components subscribe to the same promise/result. When the first `useQuery` mount triggers the fetch, the second mount sees the query is already fetching and piggybacks on the existing request.

- id: tanstack-caching-01
  answer: |
    `staleTime` controls how long a query's data is considered fresh after a fetch. During that window, no automatic refetch is triggered (e.g. on remount, window refocus, etc.). `gcTime` (formerly `cacheTime`) controls how long inactive (unsubscribed) query data remains in memory before being garbage-collected. After `gcTime` elapses with zero subscribers, the data is removed from cache entirely. A common misconception is confusing the two: `staleTime` = "don't re-fetch yet"; `gcTime` = "don't throw the data away yet". By default, `staleTime` is 0 (always stale) and `gcTime` is 5 minutes.

- id: tanstack-caching-02
  answer: |
    With `staleTime: 0` (the default), the query is immediately considered stale. On remount, the component first renders with the cached data instantly, then automatically triggers a background refetch. So the user sees the old data briefly, then it updates if anything changed on the server. This is the "stale-while-revalidate" pattern: fast perceived performance with eventual consistency.

- id: tanstack-invalidation-01
  answer: |
    It marks every query whose key starts with `['todos']` as stale. If any of those queries currently have active subscribers (are mounted), they are immediately refetched in the background. Queries matching the key but with no active subscribers are just marked stale — they will refetch the next time they are subscribed to (i.e. a component mounts with that key). The invalidation itself does not delete data; it only flags freshness and triggers refetches for active queries.

- id: tanstack-invalidation-02
  answer: |
    Use `setQueryData` when you already know the new data from the mutation response and want to update the cache immediately, without an extra network round-trip. This is an optimistic or "just-in-time" update — the UI updates instantly. The tradeoff is that `setQueryData` is a manual cache write, so you must get the cache structure right yourself and there's no guarantee it matches what the server would return. `invalidateQueries` is simpler and ensures consistency by re-fetching from the server, but it costs an extra request and the UI only updates after the fetch completes. In practice, many codebases do both: `setQueryData` for instant UI, then `invalidateQueries` on `onSettled` for eventual consistency.

- id: tanstack-mutations-01
  answer: |
    `useQuery` is declarative and data-reading: it automatically fetches, caches, deduplicates, and refetches based on the key and config. `useMutation` is imperative and data-writing: it does nothing until you call `mutate()` or `mutateAsync()`, and it doesn't have a cache key. You trigger a mutation by calling `mutate(variables)` (fire-and-forget with callbacks) or `mutateAsync(variables)` (returns a promise). You track its status via the returned object. In v5, the status values are `idle`, `pending` (was `loading`), `success`, and `error`. The `isPending` flag (was `isLoading`) tracks whether the mutation is in flight.

- id: tanstack-mutations-02
  answer: |
    `onSuccess` runs after the mutation function resolves. Calling `queryClient.invalidateQueries` there ensures the cached data that was optimistically updated (or is now stale due to the mutation) is re-fetched from the server, guaranteeing eventual consistency. This is important even after optimistic updates: the server is the source of truth, and a refetch confirms the data is correct. Without invalidation, the cache may drift from reality if the mutation response doesn't contain the full updated entity.

- id: tanstack-mutations-03
  answer: |
    1. `onMutate`: Called before the mutation function. Cancel any outgoing refetches for the affected key (`await queryClient.cancelQueries(...)`), then snapshot the current cache value (`queryClient.getQueryData(...)`) and optimistically write the new value to the cache (`queryClient.setQueryData(...)`). Return the snapshot so `onError` can use it. 2. `onError`: The mutation failed. Roll back the optimistic update by restoring the snapshot returned from `onMutate` (`queryClient.setQueryData(..., previousData)`). Optionally show an error notification. 3. `onSettled`: Runs regardless of success or failure. Call `queryClient.invalidateQueries(...)` for the affected key to re-sync with the server's source of truth.

- id: tanstack-mutations-04
  answer: |
    `mutate` is the fire-and-forget API: it accepts `onSuccess`, `onError`, and `onSettled` callbacks and does not return a promise. `mutateAsync` returns a promise that resolves with the mutation data or rejects on error, letting you use `await` and `try/catch`. A mutation knows it failed because: with `mutate`, the `onError` callback fires and the `error` status is set; with `mutateAsync`, the returned promise rejects. In both cases, the mutation object exposes `isError` and `error`.

- id: tanstack-query-fn-01
  answer: |
    `fetch` does not throw on HTTP error status codes (4xx, 5xx); it only throws on network failures. So a 500 response is silently cached as a successful result. The fix is to check `response.ok` inside the `queryFn` and throw manually: `if (!response.ok) throw new Error(...)`. Alternatively, you can use a wrapper or library that handles this. The thrown error then becomes the `error` state of the query.

- id: tanstack-query-fn-02
  answer: |
    The `queryFn` should be a pure data-fetching function that returns the data (or throws). Setting React state or causing side effects inside it can lead to bugs: the query may refetch multiple times (stale-while-revalidate, window focus, retry, etc.), causing duplicate state updates; the side effects run outside the React component lifecycle in ways that are hard to predict; and it breaks the mental model of the cache as the source of truth. Instead, derive UI state from the query's returned data in the component body, or use the query result itself as the source of truth.

- id: tanstack-query-fn-03
  answer: |
    Use the `enabled` option: `enabled: !!userId`. When `enabled` is false, the query does not fetch and remains in a `pending` status (no `data`, no `error`). Once `userId` becomes truthy, `enabled` flips to true and the query fires. The component can check `isPending` to show a loading skeleton or nothing at all. This is TanStack Query's built-in mechanism for dependent/conditional queries.

- id: tanstack-suspense-01
  answer: |
    `useSuspenseQuery` suspends the component's render until data is available, instead of returning `isPending`/`isLoading` states. It always guarantees `data` is defined (never null/undefined) when the component renders. It throws a promise to React's Suspense boundary when data is not yet ready. You must wrap the component (or a parent) in a `<Suspense>` boundary with a fallback. This eliminates loading-state boilerplate in the component.

- id: tanstack-suspense-02
  answer: |
    Fetch errors are thrown as rejected promises (or regular errors) that bubble up to the nearest React Error Boundary. So you handle them with an Error Boundary, not with `isError`/`error` on the hook (those don't exist on `useSuspenseQuery`). You can also provide an `errorBoundary` option or use TanStack Query's `QueryErrorResetBoundary` to reset the error state when retrying.

- id: tanstack-suspense-03
  answer: |
    Because each `useSuspenseQuery` suspends its own component, and React suspends components sequentially during rendering, the second component's fetch doesn't begin until the first component's suspense resolves. This creates a waterfall: component A suspends and fetches → resolves → component B suspends and fetches → resolves. To avoid this, prefetch both queries in a parent or in a router loader before the components render, so both cache entries are populated simultaneously. You can use `prefetchQuery` or `Promise.all` with `queryClient.prefetchQuery` at the parent level.

- id: tanstack-suspense-04
  answer: |
    No. `useSuspenseQuery` does not accept an `enabled` option — it always suspends and always fetches. If you need a conditional/dependent query with suspense, you have to restructure: either use `enabled` with regular `useQuery` and handle the loading state manually, or ensure the dependency is resolved before the component that calls `useSuspenseQuery` mounts (e.g. via a router loader or parent-level prefetch). The suspense contract is unconditional: the component either has data or suspends.

- id: tanstack-prefetch-01
  answer: |
    Use `ensureQueryData`. `prefetchQuery` fetches in the background and populates the cache, but if the component renders before the prefetch finishes, it may still suspend (or show stale data). `ensureQueryData` returns a promise that resolves only when the data is actually in the cache — it fetches if needed or returns the cached data immediately. This guarantees the data is available synchronously when the component renders after the loader. In a router loader, this is typically what you want.

- id: tanstack-prefetch-02
  answer: |
    `prefetchQuery` is the public API: it returns a promise, handles caching, deduplicates, and respects `staleTime`. `fetchQuery` is an internal/lower-level method that also returns a promise and fetches, but it is not the recommended public API — `prefetchQuery` subsumes it. The key difference: if the query fails, `prefetchQuery` silently swallows the error (the cache is not populated with error data), while `fetchQuery` propagates the rejection. In a router loader, you typically want the error to propagate so the router can handle it (show an error page), so you might prefer `fetchQuery` or `ensureQueryData` with your own error handling. In component-level prefetching, `prefetchQuery`'s silent failure is usually fine.

- id: tanstack-router-loaders-01
  answer: |
    A route `loader` is an async function defined on a route that runs before the route's component renders. It receives the match object (including params, context, etc.) and can fetch data or perform redirects. The component reads the loader's result via `useLoaderData()`, which returns the value the loader returned. Unlike a `useEffect` fetch, the loader runs during navigation — before the component renders — so there is no loading flash, no conditional rendering, and no race condition. The data is guaranteed to be available when the component mounts. `useEffect` fetches happen after render, requiring loading states and handling the async lifecycle.

- id: tanstack-router-loaders-02
  answer: |
    Inject the `QueryClient` into the router's context, then call `queryClient.ensureQueryData` (or `fetchQuery`) inside the loader. The loader populates the TanStack Query cache, and the component uses `useQuery` with the same key to read from that cache. Because the data is already in the cache from the loader, `useQuery` returns it immediately with no additional fetch. This gives you one shared cache, the loader's guaranteed-before-render semantics, and `useQuery`'s refetching/caching/staleness management all together.

- id: tanstack-router-loaders-03
  answer: |
    When you hover over a `<Link>`, TanStack Router calls `router.preload()` for the target route. This triggers the route's `loader` (and `beforeLoad`) ahead of time, so when the user actually clicks, the data is already cached and navigation is instant. Only the `loader` and `beforeLoad` functions run during preload — the component does not render. This is different from prefetching at the component level; it's integrated with the router's navigation lifecycle.

- id: tanstack-router-loaders-04
  answer: |
    `beforeLoad` runs before the `loader` and is intended for side effects that affect the navigation context: redirects, authentication checks, setting router `context` values, or throwing errors to prevent navigation. It receives the match and can return a new `context` object that is merged into the router context and made available to the `loader` and downstream. `loader` runs after `beforeLoad` and is for data fetching. Router `context` is a typed object that flows through the entire route tree — set in `beforeLoad` or root routes, read in loaders, components, and child routes. This separation keeps auth/logic concerns out of data fetching.

- id: tanstack-router-search-01
  answer: |
    Define a `validateSearch` function (or use a standard search schema library like Zod) on the route definition. It receives the raw URL search params and returns a typed, validated object. For example, `validateSearch: (search) => z.object({ page: z.number().default(1) }).parse(search)`. This gives you fully typed search params via `useSearch()`. You validate at the route level because URL search params are always strings — without validation you'd have to parse/cast them manually everywhere, risking runtime errors and losing type inference.

- id: tanstack-router-search-02
  answer: |
    Store the filter in the URL search params using `useNavigate` with `search` updates (e.g. `navigate({ search: { page: 2 } })`). The search param setter merges with existing params. This makes the filter shareable, bookmarkable, and back-button-friendly. The anti-pattern is storing URL-meaningful state in React component state (useState/useReducer) — it disconnects the UI from the URL, breaks deep linking, and makes the back button not work for that state.

- id: tanstack-router-search-03
  answer: |
    The loader receives a `search` property on its argument (e.g. `({ search }) => ...`) but you must explicitly type it using the route's search schema. By default, the loader's type doesn't know about search params unless you've defined `validateSearch`. Once `validateSearch` is in place, the loader's argument is typed to include the validated search params object. Without `validateSearch`, you'd have to manually parse `new URLSearchParams(location.search)` inside the loader.

- id: tanstack-router-search-04
  answer: |
    `Link` components to that route get fully typed `search` props. TypeScript enforces that you pass the correct search param types — wrong types, missing required params, or extra params all produce compile-time errors. For example, if the route requires `{ page: number; filter: string }`, `<Link to="/posts" search={{ page: 1 }} />` errors because `filter` is missing. This propagates through `navigate()` calls and `useNavigate()` as well, giving end-to-end type safety for URL state across the entire application.

- id: tanstack-router-typesafety-01
  answer: |
    File-based routing generates a route tree file (`routeTree.gen.ts`) by scanning the filesystem for route files and producing a TypeScript module that defines the full route tree with all types inferred. This generated file is the single source of truth for route params, search params, loader data types, and context. The file is regenerated whenever route files change (via the Vite plugin or CLI). Because every route's types are derived from this single generated tree, all navigation, params, and data references are type-checked end-to-end.

- id: tanstack-router-typesafety-02
  answer: |
    At compile time, it checks that: `to` is a valid route path; `params` includes all required dynamic segments for that route (and no extras); the param types match (e.g. `postId` must be a string, not a number, if the route param is typed as string); and any required `search` params are present and correctly typed. If you misspell the route, omit a required param, or pass a wrong type, TypeScript produces a compile error before the code ever runs.
