- id: tanstack-query-keys-01
  answer: |
    A query key is a serializable value, usually an array, that identifies the data a query represents. The QueryClient deterministically hashes the key and uses that hash to look up a Query in its cache. Thus, keys are compared structurally rather than by JavaScript reference; structurally equal keys normally refer to the same cache entry.

- id: tanstack-query-keys-02
  answer: |
    A hierarchy lets related queries share a meaningful prefix, such as `['todos']` and `['todos', todoId]`. Invalidation uses partial matching by default, so invalidating `['todos']` matches all queries beginning with that prefix. An exact flag can restrict invalidation to one complete key, allowing both broad and targeted updates.

- id: tanstack-query-keys-03
  answer: |
    The cache will treat the query as the same request for every `userId`, so changing `userId` can leave the previous user's data visible and can prevent the needed refetch. Every input that determines the request result belongs in the query key, for example `['projects', { userId }]`, and the `queryFn` should derive its request from that key.

- id: tanstack-query-keys-04
  answer: |
    Normally one network request is made, provided they use the same QueryClient and cache. The first observer creates the Query and its in-flight promise; the second observer subscribes to that same Query and the promise is deduplicated. If the cached data is fresh or fetching is otherwise disabled, there may be no new request.

- id: tanstack-caching-01
  answer: |
    `staleTime` controls freshness: it is how long cached data is considered fresh before normal refetch triggers may apply. `gcTime`, formerly `cacheTime`, controls retention: after a query has no observers, it remains in the cache for that duration before garbage collection. The usual defaults are `staleTime: 0` and `gcTime` of five minutes.

- id: tanstack-caching-02
  answer: |
    If the data is still cached, the component immediately receives the old value, but because it is already stale it starts a background refetch on mount. The UI therefore shows stale data rather than a loading state, with `isFetching` true, and then updates when the refetch completes. If the cache entry was garbage-collected, it instead starts in the pending state.

- id: tanstack-invalidation-01
  answer: |
    It marks every matching query as invalidated and stale; it does not delete the cached data. By default, active queries are refetched in the background, while inactive queries remain cached but stale and are refetched when they are next used. The returned promise can be awaited for the active refetches.

- id: tanstack-invalidation-02
  answer: |
    Use `setQueryData` when the mutation response is the authoritative value for a specific query, or when the next cache value can be calculated reliably. It updates that exact cache entry immediately and avoids a network request, but the application must maintain cache consistency and update every related entry itself. Invalidation is preferable when the server is the source of truth or several queries may be affected.

- id: tanstack-mutations-01
  answer: |
    `useQuery` is a declarative read operation that runs from its query key and shares cached data. `useMutation` is an imperative write or side-effect operation and does not run until triggered with `mutate(variables)` or `mutateAsync(variables)`. In v5, its status is `idle`, `pending`, `success`, or `error`, and the corresponding flags include `isPending`, `isSuccess`, and `isError`.

- id: tanstack-mutations-02
  answer: |
    A mutation may change server data that is not fully represented in its response, or it may affect several related queries. Invalidating those queries in `onSuccess` marks them stale and refetches active ones, replacing local state with the server's canonical result. It is a common safeguard against stale caches, at the cost of additional requests.

- id: tanstack-mutations-03
  answer: |
    In `onMutate`, cancel any in-flight fetch for the affected key, snapshot its previous data, and write the optimistic value with `setQueryData`; return the snapshot in the mutation context. In `onError`, use that context to restore the snapshot and report the failure. In `onSettled`, invalidate or refetch the query so the server result replaces the optimistic value or the rolled-back state.

- id: tanstack-mutations-04
  answer: |
    `mutate` starts the mutation and returns no promise; failures are exposed through mutation state and callbacks rather than being thrown to the caller. `mutateAsync` returns a promise that resolves with the mutation result and rejects on failure, so it should be awaited inside a `try/catch`. A mutation is considered failed when its `mutationFn` throws or returns a rejected promise.

- id: tanstack-query-fn-01
  answer: |
    `fetch` resolves for HTTP responses, including 500 responses; it generally rejects only for network-level failures. The query therefore sees a fulfilled promise and treats the response as successful data. The query function must check `response.ok` and throw an error for non-2xx responses, or otherwise translate the response into a rejected promise; `throwOnError` only controls how an actual query error is exposed.

- id: tanstack-query-fn-02
  answer: |
    A query function can run on the server, run again for retries or refetches, and be invoked by multiple observers, so side effects can be duplicated, repeated, or applied out of order. It should return the requested data and keep request-specific work inside it. Render derived values with `data` or `select`, use effects with cleanup for genuine client-side effects, and use a mutation for writes or other intentional side effects.

- id: tanstack-query-fn-03
  answer: |
    Put the identifier in the key and disable the query until it exists, for example `queryKey: ['projects', { userId }]`, `enabled: Boolean(userId)`, with a query function that fetches that user's projects. With no cached or placeholder data, the query remains `pending` with `fetchStatus` idle and does not call `queryFn`; it starts when `userId` becomes available.

- id: tanstack-suspense-01
  answer: |
    `useSuspenseQuery` uses the same QueryClient cache and deduplication, but it suspends the component while the required data is unavailable instead of exposing an ordinary loading result. Its data is returned once available, and an ancestor React `<Suspense>` boundary with a fallback must wrap the component. Fetch errors are sent to an error boundary rather than handled as a normal `isError` result.

- id: tanstack-suspense-02
  answer: |
    A rejected query is thrown during render to the nearest error boundary, or to the route's error component in a Router application. Handle it there and expose an appropriate error or reset state; do not expect the hook to return an `isError` branch in the ordinary way. A route error boundary or `QueryErrorResetBoundary` is commonly used for this.

- id: tanstack-suspense-03
  answer: |
    Suspense waits during rendering, so a query in a child that is not reached until an earlier query resolves cannot even be created until later. This can serialize otherwise independent requests, especially with conditional or nested rendering. Start both queries in the same render pass, use `useQueries`, or prefetch both in a parent or route loader before rendering; truly sibling queries rendered in one pass can normally start in parallel, so the waterfall comes from render ordering rather than an inherent rule.

- id: tanstack-suspense-04
  answer: |
    No. `useSuspenseQuery` does not support `enabled: false` for a dependent query, because a disabled query has no active fetch and the component could remain suspended forever. Conditionally render a child component that has the required identifier, or use regular `useQuery` when an explicitly disabled state is needed.

- id: tanstack-prefetch-01
  answer: |
    Use `ensureQueryData` in a loader that must return usable data. It returns cached data when available or fetches it, returns the data, and rejects on failure. `prefetchQuery` is also cache-warming and returns after the attempt, but it intentionally swallows query errors, so the loader cannot treat its completion as proof that data is available.

- id: tanstack-prefetch-02
  answer: |
    `prefetchQuery` populates the cache for speculative work and returns a promise that resolves without throwing when the query fails. `fetchQuery` is the imperative fetch-and-return API: it resolves with the data and rejects if the query fails. Use prefetching for optional hover or background warming; use `fetchQuery` or `ensureQueryData` when a critical path or router loader must receive data and handle failure.

- id: tanstack-router-loaders-01
  answer: |
    A route loader is a function associated with a route that the router runs before that route's component renders. It can return arbitrary data or a promise, and a file-route component commonly reads it with `Route.useLoaderData()` (or a typed `useLoaderData` helper). Unlike a `useEffect` fetch, it participates in navigation and can be awaited before the route data is needed, avoiding an initial render followed by a client-side fetch and loading state.

- id: tanstack-router-loaders-02
  answer: |
    Create one QueryClient and put it in the router's `context`. The route loader should call that client's `ensureQueryData` or `prefetchQuery` with the exact key and query function, and the component should use the same key and function—or consume the loader result directly. This makes the loader and component share one QueryCache; align `staleTime` or `refetchOnMount` as appropriate so mounting the component does not deliberately refetch data the loader just supplied.

- id: tanstack-router-loaders-03
  answer: |
    With the default intent preload, hovering or focusing a link starts preloading the matched route after the configured delay. It loads lazy route code and, unless disabled or overridden, runs the route and parent `beforeLoad` and `loader` functions, including any configured custom preload work, so Query data can be warmed. The route component is not mounted or rendered and navigation is not committed until the user actually navigates.

- id: tanstack-router-loaders-04
  answer: |
    `beforeLoad` runs before the route loader and component, commonly for authentication checks, redirects, or setup that must happen before loading. `loader` runs afterward and is intended to fetch or compute route data, receiving route context and declared dependencies. Router `context` is the shared application object supplied when creating the router—such as a QueryClient, auth service, or localization service—and is available to route lifecycle functions and components.

- id: tanstack-router-search-01
  answer: |
    Define `validateSearch` on the route, either as a typed function or with a schema adapter, and read the result with `Route.useSearch()` or a typed `useSearch` call. Validation supplies defaults and coercion and rejects or narrows malformed URL input before it reaches application code. The inferred return type also gives navigations and links type-checked search objects.

- id: tanstack-router-search-02
  answer: |
    Store the page or filter in the route's validated search parameters, read it from the route search hook, and update it through the router's `navigate` method, preserving other search values as needed. For example, update the page with `navigate({ search: previous => ({ ...previous, page: nextPage }) })`. Keeping it only in component state, or synchronizing local state with the URL afterward, is the anti-pattern because it breaks deep links, sharing, and back/forward navigation.

- id: tanstack-router-search-03
  answer: |
    Search values are not automatically declared as loader dependencies, so the loader should add `loaderDeps`, typically after validating the search. For example, use `loaderDeps: ({ search }) => ({ page: search.page })` and read `deps.page` in the loader. The dependency declaration tells Router when to rerun the loader and provides the relevant search value; a loader could alternatively read `location.search` directly, but that is less idiomatic.

- id: tanstack-router-search-04
  answer: |
    The route's inferred search type is carried into the generated route tree and exposed through `Link`, `navigate`, and search hooks. Linking code is checked against the target route's valid search shape, including required fields and value types where the schema defines them, while runtime validation still protects against malformed URLs.

- id: tanstack-router-typesafety-01
  answer: |
    The file-based routing plugin scans the route directory, maps filenames and directories to URL paths, and generates `routeTree.gen.ts`. That generated module contains the typed route objects, route IDs, nesting, and route APIs, and is passed to `createRouter`. TypeScript and the router's generated types then connect file paths, parameters, and search schemas to typed links and hooks; the generated file should not be edited by hand.

- id: tanstack-router-typesafety-02
  answer: |
    `<Link to="/posts/$postId" params={{ postId: '5' }} />` is checked against the generated route tree: the target path must exist, the dynamic `postId` parameter must be supplied, and its type must be compatible with the route's parameter type. The same checking applies to required or permitted search values, so renames and invalid links are caught at compile time rather than becoming stringly typed runtime errors.
