- id: tanstack-query-keys-01
  answer: |
    A query key is a unique identifier for a query in the TanStack Query cache. It is
    typically an array of strings and/or numbers (e.g. ['todos', 5]). The client
    serializes the key (via JSON.stringify) to produce a cache key string. Two keys
    that serialize to the same string are treated as the same cache entry — they
    share the same data, status, and observers.

- id: tanstack-query-keys-02
  answer: |
    Hierarchical keys enable prefix-based invalidation. Because invalidation matches
    by prefix, calling invalidateQueries({ queryKey: ['todos'] }) will invalidate
    ['todos'], ['todos', 1], ['todos', 2], ['todos', 'active'], etc. This lets you
    invalidate all queries under a common namespace with a single call. The rule is
    that a shorter key is a prefix of longer keys, and invalidation cascades to all
    keys that start with the given prefix.

- id: tanstack-query-keys-03
  answer: |
    When userId changes, the query will not refetch because the key ['projects'] has
    not changed — the cache still holds the old data associated with the previous
    userId. The rule is: every variable that the queryFn depends on must be included
    in the query key. The key should be ['projects', userId] so that a change in
    userId produces a new cache entry and triggers a fresh fetch.

- id: tanstack-query-keys-04
  answer: |
    One network request fires. TanStack Query deduplicates concurrent requests with
    the same key. The second useQuery call subscribes to the same query observer and
    shares the in-flight request from the first call. Both components receive the
    same data when the single request resolves.

- id: tanstack-caching-01
  answer: |
    staleTime is the duration after a successful fetch during which the data is
    considered fresh and will not be refetched on mount, window focus, or reconnection.
    gcTime (formerly cacheTime) is the duration that unused (inactive — no observers)
    data persists in the cache before being garbage collected. With staleTime, even
    stale data is served immediately from cache and refetched in the background;
    gcTime only determines how long data with no subscribers is retained. A query can
    be stale but still cached (within gcTime), or fresh and about to be garbage
    collected if it has no observers.

- id: tanstack-caching-02
  answer: |
    With the default staleTime: 0, data is immediately stale. When a component
    remounts and reads a query fetched a moment ago (still within gcTime), the cached
    data is displayed immediately, but a background refetch is triggered. The user
    sees the old data flash briefly, then the UI updates when the refetch completes.
    This is the "stale-while-revalidate" behavior.

- id: tanstack-invalidation-01
  answer: |
    It marks all queries whose keys match the given prefix as stale. For any matching
    query that is currently active (has at least one observer), it triggers a refetch.
    Inactive matching queries are simply marked stale — they will refetch the next time
    they become active. Invalidation does not remove data from the cache; it only
    flags it as stale and schedules refetches for active queries.

- id: tanstack-invalidation-02
  answer: |
    Use setQueryData when you already have the new data (e.g. from the mutation
    response) and want to update the cache immediately without waiting for a refetch.
    This gives instant UI updates. The tradeoff is that you are responsible for cache
    consistency — if the server has additional changes you are unaware of, your cache
    will be stale until the next invalidation or refetch. invalidateQueries is safer
    because it always fetches the latest server state, but it costs an extra round-trip.

- id: tanstack-mutations-01
  answer: |
    useMutation is for operations that create, update, or delete data (side effects),
    while useQuery is for fetching/reading data. A mutation is triggered imperatively
    by calling mutate() or mutateAsync(), whereas a query runs automatically when a
    component mounts. In v5, mutation status is 'idle' | 'pending' | 'success' |
    'error' (not 'loading'). You track state via the returned isPending, isSuccess,
    isError, isIdle booleans and the status string, plus the data and error
    properties.

- id: tanstack-mutations-02
  answer: |
    After a mutation succeeds, the data in the cache for affected queries is now stale
    because the server state has changed. Calling queryClient.invalidateQueries in
    onSuccess marks those queries as stale and triggers refetches for active ones,
    ensuring the UI reflects the server's current state. Without this, the UI would
    show outdated cached data until the next natural refetch.

- id: tanstack-mutations-03
  answer: |
    onMutate: runs before the mutation function is called. It receives the mutation
    variables and is used to capture a snapshot of the current cache data (via
    queryClient.getQueryData) for potential rollback, and to optimistically update the
    cache (via queryClient.setQueryData) so the UI reflects the change immediately.
    onError: runs if the mutation fails. It receives the error and the context
    returned by onMutate. It is used to roll back the optimistic update by restoring
    the snapshot captured in onMutate.
    onSettled: runs after the mutation succeeds or fails (always). It receives data
    or error plus the context. It is used for cleanup that should happen regardless
    of outcome, such as invalidating related queries to ensure consistency.

- id: tanstack-mutations-04
  answer: |
    mutate is fire-and-forget — it returns void and does not let you await the result
    or catch errors directly. mutateAsync returns a Promise that resolves with the
    mutation data on success or rejects with the error on failure. A mutation knows it
    failed when the mutationFn throws an error (or returns a rejected promise). This
    sets the mutation status to 'error' and populates the error property. With
    mutateAsync, you can try/catch the promise; with mutate, you handle errors in the
    onError callback.

- id: tanstack-query-fn-01
  answer: |
    The fetch API does not throw on HTTP error statuses (4xx, 5xx) — it only rejects
    on network-level failures (DNS, CORS, connection refused). A 500 response resolves
    successfully with response.ok === false. TanStack Query only treats a thrown error
    or rejected promise as a query error, so the 500 response is silently treated as a
    successful result. The fix is to check response.ok inside the queryFn and throw an
    Error (or a custom error) when it is false, e.g.:
      if (!response.ok) throw new Error('Request failed');
    Many API client wrappers (like axios or a custom fetch wrapper) do this
    automatically.

- id: tanstack-query-fn-02
  answer: |
    A queryFn may be called multiple times — on retries, in React Strict Mode (double
    invocation in development), and when multiple components subscribe. Side effects
    like setState can cause infinite render loops, duplicate side effects, or
    inconsistent state. The queryFn should be a pure function that only fetches and
    returns data. Side effects should be handled in the component (e.g. in useEffect
    reacting to data changes) or in mutation callbacks, not inside the queryFn.

- id: tanstack-query-fn-03
  answer: |
    Use the enabled option set to false (or a function returning false) until userId
    is known: useQuery({ queryKey: ['projects', userId], queryFn: ..., enabled: !!userId }).
    Until enabled becomes true, the query is in status 'pending' with fetchStatus
    'idle' (or 'paused' in some versions). No request is fired. Once userId is
    available and enabled flips to true, the query transitions to 'pending' with
    fetchStatus 'fetching' and the request fires.

- id: tanstack-suspense-01
  answer: |
    useSuspenseQuery causes the component to suspend (throw a promise) while data is
    loading, so the component does not render until the data is ready. This eliminates
    the need for loading state checks in the component. A React <Suspense> boundary
    must wrap the component (or an ancestor) to catch the thrown promise and display
    a fallback UI (e.g. a spinner) while the query is pending.

- id: tanstack-suspense-02
  answer: |
    With useSuspenseQuery, fetch errors surface through React Error Boundaries, not
    through the query's error state. You wrap the component (or an ancestor) in an
    <ErrorBoundary> component that catches the error and renders a fallback UI (e.g.
    an error message with a retry button). The error boundary's fallback prop or
    componentDidCatch handles the error display.

- id: tanstack-suspense-03
  answer: |
    If component A suspends on query 1, React cannot render component B until A's
    suspense resolves. Only after query 1 completes does B mount and suspend on
    query 2, creating a sequential waterfall. To avoid this: prefetch both queries
    before rendering (e.g. in a parent component or router loader), or restructure so
    both queries are initiated in the same render pass before either suspends.

- id: tanstack-suspense-04
  answer: |
    No. useSuspenseQuery does not support the enabled option — it must always be
    enabled because the component is expected to suspend until data is ready. For
    dependent queries with suspense, the pattern is to use a regular useQuery with
    enabled for the dependent portion, or to split into a parent component that
    fetches the prerequisite data and a child component that only renders (and
    suspends) once the prerequisite is available.

- id: tanstack-prefetch-01
  answer: |
    Use ensureQueryData. It returns the cached data if it is fresh (within staleTime),
    otherwise it fetches and caches the data, returning a promise that resolves when
    the data is ready. This guarantees the data is available before the component
    renders. prefetchQuery only fetches and caches without returning data — it is a
    fire-and-forget cache warmer. In a router loader, you need ensureQueryData
    because you must await the data being ready before proceeding with navigation.

- id: tanstack-prefetch-02
  answer: |
    prefetchQuery swallows errors — they are caught and logged but not thrown, so the
    caller never sees them. fetchQuery throws errors, allowing the caller to handle
    them (e.g. redirect on 404, show an error page). The error-handling behavior
    matters when the loader needs to react to fetch failures: use fetchQuery when you
    need to catch and respond to errors; use prefetchQuery when you only want to warm
    the cache and do not care about the outcome.

- id: tanstack-router-loaders-01
  answer: |
    A route loader is a function defined on a route that runs before the route's
    component renders, typically used to fetch data the route needs. The component
    reads the loader's result via the useLoaderData() hook. Unlike a useEffect fetch,
    the loader runs during the navigation phase — before the component mounts — so
    the data is available on the very first render. This eliminates loading states,
    reduces layout shift, and ensures data is present before the component tries to
    use it.

- id: tanstack-router-loaders-02
  answer: |
    In the loader, call queryClient.ensureQueryData() (or queryClient.fetchQuery())
    with the same query key that the component's useQuery will use. This populates
    the TanStack Query cache during navigation. When the component mounts and calls
    useQuery with the same key, it finds the cached data already present and does not
    fire a duplicate request. The loader and component share one cache entry, avoiding
    double-fetching.

- id: tanstack-router-loaders-03
  answer: |
    Preloading on link hover runs the target route's beforeLoad and loader functions
    (and preload if defined) when the user hovers over a link, before they click. This
    means data fetching and auth checks start early, so by the time the user clicks,
    the data is often already cached and the navigation feels instant. During a
    preload, beforeLoad runs first (for guards/redirects/context), then loader runs
    (for data fetching).

- id: tanstack-router-loaders-04
  answer: |
    beforeLoad runs before the loader and is typically used for authentication checks,
    permission guards, redirects, and setting router context. It does not fetch data
    for the component — that is the loader's job. The loader runs after beforeLoad
    succeeds and is responsible for fetching the data the route component needs.
    Router context is an object that is initialized in beforeLoad (via the context
    option or by returning a value) and is available to the loader and route component
    via useRouteContext(). It is used to pass values (like auth info) from beforeLoad
    to the loader and component without prop drilling.

- id: tanstack-router-search-01
  answer: |
    validateSearch is a function defined on a route that receives the raw URL search
    params (always strings) and returns a typed, validated object. For example, it can
    parse a page string to a number, provide defaults, and validate allowed values.
    You validate because URL search params are untyped strings — without validation,
    you would need to manually parse and cast them everywhere, risking runtime errors.
    validateSearch centralizes this logic and produces a typed object that the loader
    and component can trust.

- id: tanstack-router-search-02
  answer: |
    Store UI filters in the URL search params by defining them in the route's
    validateSearch and passing them via the search prop on <Link> or navigate(). This
    makes filters shareable via URL, preserves them on refresh, and keeps back/forward
    navigation working correctly. The anti-pattern is storing URL-derived state in
    component state, a global store, or useState — this breaks shareability, causes
    desync between the URL and the UI, and loses the filter on page refresh.

- id: tanstack-router-search-03
  answer: |
    The loader does not see search params automatically because search params are
    parsed and validated by validateSearch before the loader runs. You add a
    validateSearch function to the route definition that returns the typed search
    params object. The loader then receives this validated object as part of its
    argument (the second parameter, or a property on the first), giving it access to
    the typed search params.

- id: tanstack-router-search-04
  answer: |
    Once validateSearch is in place, the <Link> component's to prop is typed to
    require the correct search params for that route. TypeScript will produce a compile
    error if you attempt to link to the route without providing required search
    params or if you provide params of the wrong type. This gives end-to-end type
    safety from the URL definition through to navigation.

- id: tanstack-router-typesafety-01
  answer: |
    File-based routing generates a typed route tree file (routeTree.gen.ts) that
    contains all routes with their hierarchical structure, parameter names, and types.
    This generated file powers the type-safe navigation API — the to prop on <Link>
    and the navigate() function are checked against the actual route definitions at
    compile time. If a route does not exist or a param is missing/mistyped,
    TypeScript reports an error.

- id: tanstack-router-typesafety-02
  answer: |
    It checks at compile time that: (1) the route /posts/$postId exists in the route
    tree, (2) postId is a required param for that route, and (3) the value '5' matches
    the expected type for postId (string). If the route does not exist, the param is
    missing, or the value type is wrong, TypeScript produces a compile-time error,
    preventing broken navigation before the code runs.
