# cat-jam-free — closed-book answers for tanstack (opencode-go, 2026-10-14); answerer saw only _prompts/tanstack.md
# Delivery: one `ocode run -m opencode-go/cat-jam-free -effort med` session, sheet attached via -f, empty scratch cwd;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.
# RE-RUN under an isolated HOME with every tool denied, cwd holding only its sheet: the first session called bash/read on its own
# question sheet (no key access) and was discarded. The re-run emitted the YAML list twice, byte-identical, separated by a
# leaked thinking-aloud line ('Need perhaps final only. Good.'); one copy kept.

- id: tanstack-query-keys-01
  answer: |
    A query key is the cache identity for a query. Two keys refer to the same cache entry when their values are structurally/deeply equal in the same order, typically as arrays.

- id: tanstack-query-keys-02
  answer: |
    Hierarchical keys let a parent key act as a prefix, so invalidating ['todos'] also affects ['todos', todoId]. Ordering matters: broader keys come first, specifics come last.

- id: tanstack-query-keys-03
  answer: |
    The cache ignores variables not in the key, so different userId values can share one stale/wrong result and updates will not refetch correctly. Rule: every value used inside queryFn must be part of the query key.

- id: tanstack-query-keys-04
  answer: |
    One network request fires because TanStack Query deduplicates concurrent queries with the same cache key and shares the in-flight promise.

- id: tanstack-caching-01
  answer: |
    staleTime controls how long data is considered fresh before refetch triggers. gcTime controls how long inactive cached data stays in memory before garbage collection.

- id: tanstack-caching-02
  answer: |
    With staleTime 0, cached data is stale, so a remount triggers a background refetch while immediately showing the previous data, then updates the UI when the new response arrives.

- id: tanstack-invalidation-01
  answer: |
    It marks matching queries stale and refetches active ones in the background. It does not immediately remove cached data.

- id: tanstack-invalidation-02
  answer: |
    Use setQueryData when you already know the exact new server result and want an instant cache update. The tradeoff is speed versus safety: invalidateQueries guarantees synchronization with the server but costs a request.

- id: tanstack-mutations-01
  answer: |
    useMutation is for writes and does not auto-run; call mutate(variables) or mutateAsync(variables). Track it with isPending, isError, isSuccess, data, and error, noting v5 prefers isPending over isLoading.

- id: tanstack-mutations-02
  answer: |
    After a successful mutation, server state may have changed. Invalidating affected queries refetches them, keeping the UI synchronized with the backend.

- id: tanstack-mutations-03
  answer: |
    onMutate cancels related refetches, snapshots previous cache, and optimistically updates it. onError rolls back to the snapshot. onSettled invalidates or refetches to sync with the server regardless of success or failure.

- id: tanstack-mutations-04
  answer: |
    mutate is fire-and-forget with callbacks, while mutateAsync returns a promise and can be awaited or caught. A mutation is known to fail via isError/error or by mutateAsync rejecting and calling onError.

- id: tanstack-query-fn-01
  answer: |
    fetch does not throw for HTTP error statuses, so TanStack Query sees a successful response. Check response.ok and throw an error for non-OK responses.

- id: tanstack-query-fn-02
  answer: |
    queryFn may run multiple times due to retries, refetches, or strict effects, so side effects there are unsafe and can cause loops or duplicate updates. Keep queryFn pure and move side effects to effects or router handlers.

- id: tanstack-query-fn-03
  answer: |
    Use enabled: !!userId on useQuery. Until then the query does not execute; it stays pending with an idle fetch status and returns undefined data.

- id: tanstack-suspense-01
  answer: |
    useSuspenseQuery suspends the component until data is loaded and guarantees non-undefined data. The component must be wrapped in Suspense.

- id: tanstack-suspense-02
  answer: |
    Errors surface to the nearest Error Boundary, not as an error property on the query. Handle them with an ErrorBoundary around the component.

- id: tanstack-suspense-03
  answer: |
    Independent suspense queries can resolve one after another, creating a waterfall. Avoid it by hoisting queries, using useSuspenseQueries, or prefetching/ensuring data before render.

- id: tanstack-suspense-04
  answer: |
    No. useSuspenseQuery must always suspend for its data, so enabled: false is incompatible; conditionally render the component or use useQuery instead.

- id: tanstack-prefetch-01
  answer: |
    Use ensureQueryData. It ensures the query exists with data, fetches if needed, and resolves before continuing; prefetchQuery warms the cache but does not provide the same guarantee or data result.

- id: tanstack-prefetch-02
  answer: |
    fetchQuery returns data and throws on error, which matters when you need the result or must handle failures. prefetchQuery warms the cache and swallows errors, making it suitable for optimistic warming.

- id: tanstack-router-loaders-01
  answer: |
    A route loader runs before the component renders to fetch route-critical data. The component reads it with Route.useLoaderData or useLoaderData. Unlike useEffect fetching, it runs during navigation/preload and blocks rendering.

- id: tanstack-router-loaders-02
  answer: |
    Call queryClient.ensureQueryData or fetchQuery inside the loader using the same query key and queryFn. The component then uses useQuery with that same key, sharing one cache and avoiding duplicate fetches.

- id: tanstack-router-loaders-03
  answer: |
    Hover preloading starts loading route resources early. During preload, the route's beforeLoad and loader run so data is ready before navigation.

- id: tanstack-router-loaders-04
  answer: |
    beforeLoad runs before loader for auth, redirects, validation, or context setup. loader fetches route data. Router context is shared structured data, often including queryClient, passed to route logic and components.

- id: tanstack-router-search-01
  answer: |
    Define validateSearch on the route with a validation schema or function. It validates and transforms URL search params, enabling typed search values and safe defaults.

- id: tanstack-router-search-02
  answer: |
    Keep the filter in the URL through useSearch and navigate with a search updater function. The anti-pattern is mirroring URL state in local useState or manipulating window.location directly.

- id: tanstack-router-search-03
  answer: |
    Loaders do not automatically rerun for search changes unless dependencies are declared. Add loaderDeps, such as loaderDeps: ({ search }) => ({ page: search.page }), to feed search-derived values into the loader.

- id: tanstack-router-search-04
  answer: |
    Search params become typed and checked across Links, useSearch, loaderDeps, and navigation. Linking to the route gets compile-time validation of search values and their types.

- id: tanstack-router-typesafety-01
  answer: |
    File-based routing maps the file structure to route definitions and generates routeTree.gen.ts, which provides the typed route tree used for end-to-end type safety.

- id: tanstack-router-typesafety-02
  answer: |
    It checks at compile time that the path exists, required params such as postId are provided, and their values match the expected type.
