- id: tanstack-query-keys-01
  answer: |
    A query key is a serializable array (usually of strings/numbers/objects) that
    uniquely identifies the data a query fetches. TanStack Query hashes it
    deterministically (a stable, order-sensitive JSON-ish serialization with
    sorted object keys) into a single string; two keys are the same cache entry
    when their hashes match. The hash is structural: ['todos', {page: 1}] and
    ['todos', {page: 1}] match, but ['todos', 1] and [1, 'todos'] do not
    (order matters). Anything non-serializable or non-deterministic in the key
    (functions, class instances, dates with unstable formatting) breaks matching.

- id: tanstack-query-keys-02
  answer: |
    Hierarchical keys let you invalidate at different granularities. A key is a
    prefix tree: ['todos'] is the parent of ['todos', id] and
    ['todos', id, 'comments']. invalidateQueries({ queryKey: ['todos'] }) matches
    by prefix, so it invalidates every query whose key starts with ['todos'].
    This means you can group related data (all lists, or all data for one entity)
    and invalidate the whole group with one call, while still keeping per-item
    cache entries separate. Ordering from general to specific is the convention
    that makes prefix matching useful.

- id: tanstack-query-keys-03
  answer: |
    The query's cache entry is keyed only by ['projects'], so it does not vary
    with userId. When userId changes, the key stays identical, so TanStack Query
    treats it as the same cache entry and returns the old data for the previous
    user (and may not refetch at all, or will overwrite the same entry). This
    leaks one user's data into another user's view. The rule: every variable the
    queryFn closes over and uses to produce the data must be part of the query
    key — ['projects', userId]. The key must uniquely describe the data.

- id: tanstack-query-keys-04
  answer: |
    One network request fires. TanStack Query dedupes in-flight requests by key:
    when the second observer subscribes to the same key while the first request
    is still pending, it attaches to the existing promise instead of starting a
    new fetch. Both components receive the same result. (After the request
    settles, subsequent remounts may trigger a background refetch depending on
    staleTime, but the simultaneous mount dedupes to a single request.)

- id: tanstack-caching-01
  answer: |
    staleTime controls freshness: how long fetched data is considered fresh and
    will not be refetched on mount/refocus/reconnect. gcTime (formerly cacheTime)
    controls garbage collection: how long an inactive/unobserved query's cache
    entry is kept in memory before being removed. They are independent. staleTime
    decides "do we refetch?"; gcTime decides "how long after nobody uses it do we
    forget it?". Default staleTime is 0 (immediately stale); default gcTime is
    5 minutes. Setting staleTime to Infinity keeps data fresh forever but the
    entry is still removed gcTime after it becomes unused.

- id: tanstack-caching-02
  answer: |
    With staleTime: 0, data is immediately considered stale. On remount, the
    query has cached data, so the UI renders it instantly from cache (no loading
    spinner), and simultaneously triggers a background refetch. When the refetch
    resolves, the component re-renders with fresh data. So the user sees the
    old/stale data flash immediately, then an update — "stale-while-revalidate".
    isFetching is true during the background refetch, but isLoading is false
    because there is cached data.

- id: tanstack-invalidation-01
  answer: |
    It marks all queries whose key matches ['todos'] (by prefix) as stale and
    triggers refetches for the ones that are currently active/mounted. Inactive
    matching queries are just marked stale (they'll refetch when next used,
    if they're still in cache). By default it refetches active queries; you can
    pass refetchType: 'none' to only mark stale without refetching, or 'all' to
    refetch inactive ones too. It does not remove the cache entries.

- id: tanstack-invalidation-02
  answer: |
    Use setQueryData when you already have the new data (e.g. the mutation
    response contains the updated object) and want to update the cache directly,
    avoiding a refetch. It's instant and saves a round trip. The tradeoff: you
    are responsible for producing correct data and keeping it consistent with
    the server; if the server does more than you model (computed fields, other
    entities, sorting), the cache can drift from truth. invalidateQueries is
    safer/less code because the server is the source of truth, but costs a
    refetch and is not instant. Often you combine them: setQueryData for an
    immediate update, then invalidateQueries to reconcile.

- id: tanstack-mutations-01
  answer: |
    useQuery is declarative and runs automatically based on the key; it models
    read/cached data and exposes isLoading/isError/data. useMutation models a
    write/side-effect; it does nothing until you call mutate/mutateAsync, and it
    is not cached or shared. You get back a mutate function plus status flags.
    In v5 the naming is `isPending` (the mutation is in flight), `isError`,
    `isSuccess`, and `status` is 'idle' | 'pending' | 'error' | 'success' —
    `isLoading` was renamed to `isPending` for mutations (and `isIdle` exists).
    `data`, `error`, and `reset()` are also available.

- id: tanstack-mutations-02
  answer: |
    A mutation changes server state, but the query cache still holds the old
    data, so the UI would show stale values. Calling invalidateQueries in
    onSuccess marks the affected queries stale and refetches the active ones,
    so the UI reconciles with the server after the write. It's the standard way
    to keep server-state caches consistent after a mutation without manually
    rewriting every cache entry.

- id: tanstack-mutations-03
  answer: |
    onMutate (before the request): cancel any outgoing refetches for the
    affected queries (await queryClient.cancelQueries), snapshot the previous
    cached data (queryClient.getQueryData), optimistically update the cache
    (setQueryData), and return the snapshot as the context so later callbacks can
    use it. onError(error, variables, context): roll back to the snapshot with
    setQueryData using context.previous, and usually show an error. onSettled
    (runs on both success and error): invalidate the affected queries to
    reconcile with the server, regardless of outcome. Each step: onMutate =
    optimistic write + snapshot; onError = rollback; onSettled = refetch/cleanup.

- id: tanstack-mutations-04
  answer: |
    mutate fires the mutation and returns void; you handle results through the
    options callbacks (onSuccess/onError/onSettled) or the hook's state. It does
    not return a promise, so errors are not thrown to the caller — you must use
    the error callback or hook state. mutateAsync returns a promise that resolves
    with data or rejects with the error, so you can await it and try/catch it.
    A mutation "knows" it failed when the mutationFn promise rejects; that sets
    status to 'error' and populates `error`, and triggers onError/onSettled.
    Caveat: an unhandled rejection from mutateAsync can surface as an unhandled
    promise rejection if you don't catch it.

- id: tanstack-query-fn-01
  answer: |
    fetch does not reject on HTTP error statuses — a 500 resolves normally with
    response.ok === false. TanStack Query only treats a rejected promise as an
    error, so the 500 looks like success. Fix: check response.ok (or status) in
    the queryFn and throw when it's not ok, e.g.
    `if (!response.ok) throw new Error('Request failed')` before parsing JSON.
    Then the query goes to error state.

- id: tanstack-query-fn-02
  answer: |
    queryFn must be a pure-ish data fetcher because TanStack Query may call it
    at unpredictable times (on mount, refocus, reconnect, retries, multiple
    observers), so side effects like setState cause redundant renders, infinite
    loops, state updates during render/after unmount, and surprising behavior
    under retries/dedup. The query result should be the single source of truth.
    Instead, derive UI from the query's returned data/status, or run side effects
    in a useEffect that reacts to the query data, or use query callbacks / the
    observer. In short: return data from queryFn, don't push state from it.

- id: tanstack-query-fn-03
  answer: |
    Use the `enabled` option, set from the dependency:
    `useQuery({ queryKey: ['user', userId], queryFn: ..., enabled: !!userId })`.
    While disabled, the query does not run and its status is 'pending' with
    fetchStatus 'idle' and isLoading true — in v5 you can distinguish it: status
    is 'pending', fetchStatus is 'idle'. The component renders in a loading/empty
    state until userId is available, then the query runs. (Note: in v5 the query
    is still "pending" not "idle" as a status; the idle signal is fetchStatus.)
    Include the dependency in the key too.

- id: tanstack-suspense-01
  answer: |
    useSuspenseQuery integrates with React Suspense: instead of exposing
    isLoading/isError for you to branch on, it suspends the component (throws the
    promise) while loading and throws to the nearest error boundary on failure.
    When it returns, `data` is guaranteed defined (non-undefined). The component
    must be wrapped in a <Suspense fallback={...}> boundary (and an error boundary
    for errors). It also does not support `enabled: false` in the same way.

- id: tanstack-suspense-02
  answer: |
    Errors surface by being thrown to the nearest React error boundary above the
    component. You handle them with an error boundary (e.g. React's
    ErrorBoundary or react-error-boundary), optionally combined with the query's
    retry behavior. There is no isError branch inside the component because the
    error never lets the component render. You can also provide error handling
    via the error boundary's fallback or useQueryErrorResetBoundary to retry.

- id: tanstack-suspense-03
  answer: |
    Suspense serializes rendering: a suspended sibling prevents the parent from
    committing, and in practice React renders the first suspending child, waits,
    then the next — so the second query's fetch doesn't start until the first
    resolves. That's a request waterfall. Avoid it by starting the fetches
    earlier and in parallel: use prefetchQuery/ensureQueryData in a router loader
    or parent, or queryClient.prefetchQuery for both before render, or hoist the
    queries to a parent and pass data down, or use React 19 / suspense-enabled
    parallel patterns (e.g. Promise.all in a loader). Prefetching both keys
    before rendering lets the cache serve both without serial suspension.

- id: tanstack-suspense-04
  answer: |
    No. useSuspenseQuery does not support `enabled: false` — the option is
    ignored/unsupported because Suspense queries must always have data to return
    (they can't return undefined). Making it conditional would break the
    guarantee that `data` is defined. For dependent queries you should instead
    split the component so the dependent query is only mounted once its
    dependency exists (render it conditionally), or use a normal useQuery with
    `enabled`, or prefetch/ensure in a loader. The rule: useSuspenseQuery is for
    unconditional, always-required data.

- id: tanstack-prefetch-01
  answer: |
    Use ensureQueryData. Both populate the cache, but prefetchQuery does not
    return the data and does not throw on error — it fires a fetch and resolves
    regardless. ensureQueryData returns the cached data if present and fresh,
    otherwise fetches and returns the data, and it rejects if the fetch fails.
    In a loader where you need the data guaranteed available before rendering,
    ensureQueryData gives you the data (and you can await it) and fails the
    loader on error; prefetchQuery only warms the cache and you'd have to read
    it separately, with no error propagation.

- id: tanstack-prefetch-02
  answer: |
    prefetchQuery fires a fetch to warm the cache but swallows errors (the
    returned promise resolves even if the fetch rejects) — good for
    fire-and-forget prefetching where a failure shouldn't break anything.
    fetchQuery returns a promise that resolves with the data and rejects on
    error, and it will use fresh cached data if available (respecting staleTime)
    otherwise fetch. The error-handling difference matters when the caller must
    react to failure — e.g. in a router loader you want fetchQuery/ensureQueryData
    so a failed fetch rejects the loader and routes to an error boundary, rather
    than silently rendering with no data.

- id: tanstack-router-loaders-01
  answer: |
    A route `loader` is an async function attached to a route that runs before
    the route's component renders (during navigation and on initial load). Its
    return value is placed in the route's loader data, and the component reads it
    via `Route.useLoaderData()` (or `useLoaderData({ from: ... })`). The router
    awaits the loader, so the component never renders in a "no data yet" state
    for that route. Unlike a useEffect fetch, which runs after render and causes
    a loading state and an extra render/waterfall, a loader runs before render,
    is part of navigation (can block/await, integrates with pending UI, route
    cancellation, error boundaries), and can access route params/search/context.

- id: tanstack-router-loaders-02
  answer: |
    Put a QueryClient in the router context and call
    `context.queryClient.ensureQueryData({ queryKey, queryFn })` inside the
    loader, and use the same queryKey in the component's useQuery (or
    useSuspenseQuery). Because the loader populated the same cache the component
    reads from, there is no double fetch. You expose the queryClient via
    createRootRouteWithContext / router context and typically add
    defaultPreloadStaleTime or set staleTime so the component doesn't immediately
    refetch. The key must match exactly between loader and component.

- id: tanstack-router-loaders-03
  answer: |
    By default TanStack Router preloads a route when the user hovers/focuses a
    link (defaultPreload: 'intent', with a short delay), so the target route's
    data and code are ready before the click, making navigation instant. During
    preload the router runs the route's loader (and beforeLoad, and lazy
    component loading) for the matched route, but it does not commit navigation
    or re-render the current UI. With TanStack Query integration, the loader's
    ensureQueryData/prefetchQuery warms the query cache. So loader (and
    beforeLoad) run, populating cache, without the navigation completing.

- id: tanstack-router-loaders-04
  answer: |
    beforeLoad runs before loader and before the route is matched/rendered; it's
    for guards, auth checks, redirects, and injecting/extending router context.
    It can throw redirect() or notFound(), and its return value is merged into
    the route context for that route and its children. loader runs after
    beforeLoad and is for fetching the data the route renders. Router context is
    a shared object passed through the router (createRouter({ context })) and
    extended/overridden per-route by beforeLoad, available to loaders and
    beforeLoad as `context` — used for things like an auth object or the
    QueryClient.

- id: tanstack-router-search-01
  answer: |
    Define `validateSearch` on the route: it's a function taking the raw
    search object and returning the parsed/typed search params, commonly using a
    schema library like Zod:
    `validateSearch: (search) => mySchema.parse(search)` (or `.catch()` for
    defaults). The route then has typed search params, read via
    `Route.useSearch()`. You validate because the URL is external, untrusted
    input: users edit it, links are shared, and values arrive as strings (or
    missing). Validation coerces types, applies defaults, rejects invalid values,
    and makes downstream code type-safe and predictable instead of guarding
    undefined/NaN everywhere.

- id: tanstack-router-search-02
  answer: |
    Store the filter in the URL as a search param and update it with the router
    (`navigate({ search: (prev) => ({ ...prev, page }) })` or
    `Route.useNavigate()`), reading it via `Route.useSearch()`. That makes the
    state shareable, bookmarkable, back/forward-able, and survives reloads. The
    anti-pattern is holding it in component useState (or a global store) and
    syncing it to the URL manually, or using useEffect to mirror it — that
    desyncs from the URL, breaks back/forward and deep links, and causes double
    renders. The URL should be the single source of truth for URL-worthy UI
    state.

- id: tanstack-router-search-03
  answer: |
    Loaders receive a context object that includes `params`, `context`,
    `deps`, etc., but search params are not automatically a dependency because
    the loader needs to know when to re-run and how to access them. You opt in
    via the route's `loaderDeps`: a function returning the subset of search
    params the loader cares about, e.g.
    `loaderDeps: ({ search: { page } }) => ({ page })`. The returned value is
    passed to the loader as `deps`, and changing it causes the loader to re-run.
    Then read `deps.page` in the loader. Without loaderDeps, the loader won't
    react to search changes (and search isn't guaranteed stable for it).

- id: tanstack-router-search-04
  answer: |
    Once validateSearch declares the route's search type, every `Link`, `navigate`,
    `useSearch`, and `redirect` that targets that route is type-checked against
    it. The `search` prop becomes typed: it must be a function (or object)
    returning the exact shape, required fields are enforced, values are typed
    (number vs string), and unknown/misspelled fields are compile errors.
    Updating search also gets the previous search as a typed argument, so
    spreading `(prev) => ({ ...prev, ... })` is type-safe. Renaming a search
    param surfaces as errors at every call site.

- id: tanstack-router-typesafety-01
  answer: |
    File-based routing generates a route tree from the filesystem: each file
    under the routes directory becomes a route, and the path segments and
    special filenames ($param, layout, index, etc.) define params and nesting.
    A generator parses these files and emits `routeTree.gen.ts`, which declares
    the concrete route types — every route's path, params, search schema, and
    loader data — and the `declare module` augmentation that teaches the router
    these types. Because the tree is a concrete TypeScript type, `Link to`,
    `navigate`, `useParams`, and `useSearch` are checked against the actual
    registered routes, giving end-to-end compile-time safety. routeTree.gen.ts is
    auto-generated (regenerated by the plugin/CLI) and should not be edited by
    hand.

- id: tanstack-router-typesafety-02
  answer: |
    It checks, at compile time, that: `to` is a valid registered route path
    (the literal '/posts/$postId' exists), that the route requires a `postId`
    param, that `params` provides exactly that param (missing or extra/misspelled
    keys are errors), and that the param value's type is acceptable (string).
    If the path were '/posts/$postId' but you omitted `params`, or used a
    nonexistent param name, or a route that doesn't exist, TypeScript errors.
    It also validates any required `search` for that route. So navigation typos
    and missing/incorrect params are caught before runtime.
