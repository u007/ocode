# tanstack knowledge benchmark — answers
# Model: opencode-go/muse-spark-1.2   (provider-stripped id: muse-spark-1.2)
# CLOSED-BOOK via ocode run -dir <empty> (no corpus access). corpus_rev 1.

```yaml
- id: tanstack-query-keys-01
  answer: |
    A query key is the unique identifier for a cached query. It is always an array (e.g. ['todos'] or ['todos', { status: 'done' }]) that TanStack Query hashes to a stable string (deep/structural equality via JSON-stable hash, order matters).
    Two keys refer to the same cache entry iff their hashed values are deeply equal - same length, same values in same positions, with objects compared deeply. ['user', 5] === ['user', 5] but !== ['user', '5'] and !== ['user', 5, 'extra']. The query function is not part of the equality; only the key determines the cache slot.

- id: tanstack-query-keys-02
  answer: |
    Structure keys hierarchically from general to specific so you can target groups with fuzzy/prefix matching.
    e.g. ['todos'] = list, ['todos', todoId] = detail, ['todos', todoId, 'comments'] = sub-resource.
    APIs like invalidateQueries({ queryKey: ['todos'] }), removeQueries, cancelQueries, getQueriesData default to prefix/exact:false matching, so one call invalidates the whole subtree. If you use flat unrelated keys like ['allTodos'] and ['todo-5'] you lose that and must invalidate each explicitly. Also enables partial matching with exact: true when you want only one level.

- id: tanstack-query-keys-03
  answer: |
    Breaks cache correctness / stale data bug. The fetch depends on userId but the key does not include it, so ['projects'] for user A and ['projects'] for user B map to the same entry. Switching users will show wrong user's cached data with no refetch, or you won't refetch when userId changes.
    Rule: Every variable that is read inside queryFn (depends on) MUST be included in queryKey. Correct: ['projects', userId] and queryFn: ({ queryKey: [, id] }) => fetchProjects(id). ESLint plugin @tanstack/query enforces this.

- id: tanstack-query-keys-04
  answer: |
    Only ONE network request. TanStack Query deduplicates in-flight requests by queryKey hash. Within the same QueryClient, concurrent subscribers to the same key share the single promise; both components get the same loading -> success transition. Deduplication is windowed (configurable). If staleTime and cache allow, the second mount may not even trigger a refetch at all.

- id: tanstack-caching-01
  answer: |
    This is the #1 confusion: they control orthogonal lifetimes.
    staleTime: how long fetched data is considered FRESH. While fresh, no background refetch on mount/focus/reconnect. Default 0 = immediately stale, so every mount causes a background refetch (stale-while-revalidate). staleTime: Infinity = never stale.
    gcTime (was cacheTime in v4, default 5 minutes): how long an INACTIVE cache entry (no subscribers) stays in memory before garbage collection. After gcTime, data is evicted and next mount is a hard loading state with no cached data to show. gcTime > staleTime semantics: staleTime = freshness, gcTime = memory retention.
    Example: staleTime 30s, gcTime 5m -> remount within 30s shows cached data silently, remount after 2m shows cached data + background refetch, remount after 10m shows loading spinner.

- id: tanstack-caching-02
  answer: |
    With staleTime: 0 (default), the cached data is considered stale instantly after fetching.
    On remount: the UI immediately shows the stale cached data (no loading spinner if cache exists), but the query is flagged stale and triggers a background refetch. You will see data -> isFetching=true -> updated data. This is stale-while-revalidate. If gcTime hasn't evicted it, users never see an empty loading state, just potentially a fetching indicator. Only if there is no cache entry at all do you see pending/isLoading.

- id: tanstack-invalidation-01
  answer: |
    It does NOT delete data. It marks every matching query (prefix match on ['todos'] unless exact:true) as invalid/stale and schedules a refetch.
    Active queries (with mounted subscribers) refetch immediately in the background. Inactive queries are just marked stale and will refetch on next mount/focus. Can be scoped with predicate, type: 'active'|'inactive'|'all', exact, refetchType. Often used as queryClient.invalidateQueries({ queryKey: ['todos'] }).

- id: tanstack-invalidation-02
  answer: |
    Use setQueryData when you already have the authoritative new server response and want an INSTANT, network-free UI update. Use invalidateQueries when you want to re-derive from server truth.
    Tradeoff: setQueryData is optimistic/instant, no extra request, good for lists/toggles, but you must manually keep normalization correct and you risk drift if server applied extra logic. invalidateQueries is simpler/correct-by-refetch but costs a request and causes a loading/fetch flicker. Large apps often do both: setQueryData optimistically then invalidate onSettled to reconcile.

- id: tanstack-mutations-01
  answer: |
    useQuery is declarative, cached, keyed, auto-fetched and deduplicated. useMutation is for side effects/creates/updates/deletes: not cached, not keyed, imperative - nothing happens until you call mutate.
    Trigger: const { mutate, mutateAsync, isPending, isSuccess, isError, error, data } = useMutation({ mutationFn: (vars) => api.update(vars) })
    v5 status naming: isPending (was isLoading in v4), isSuccess, isError, isIdle. mutate(vars, { onSuccess }) is fire-and-forget void; mutateAsync returns Promise for await/try-catch.

- id: tanstack-mutations-02
  answer: |
    Because mutations typically change server state that existing queries are caching. Query caches are not automatically updated after a POST/PUT/DELETE.
    Calling invalidateQueries({ queryKey: ['todos'] }) in onSuccess marks the related queries stale so TanStack Query refetches them, bringing UI back in sync with server. Alternative is manual setQueryData, but invalidation is the simple correct default when you don't want to manually merge.

- id: tanstack-mutations-03
  answer: |
    Correct pattern:
    1. onMutate: optimistic step, runs BEFORE mutationFn. Call await queryClient.cancelQueries({ queryKey }) to stop races, snapshot previous data via getQueryData, then setQueryData with optimistic value. Return context { previousData }.
    2. onError: rollback step - if mutation fails, restore with queryClient.setQueryData(key, context.previousData). Signature (error, vars, context).
    3. onSettled: reconciliation step - always runs on success or error. Invalidate to resync with server: queryClient.invalidateQueries({ queryKey }).
    This gives instant UI, rollback on failure, and eventual consistency.

- id: tanstack-mutations-04
  answer: |
    mutate is fire-and-forget, returns void, errors handled via onError callback / isError state; you cannot await it. mutateAsync returns a Promise that resolves/rejects, so you can await it or try/catch.
    A mutation knows it failed when the mutationFn throws or returns a rejected Promise. TanStack does not inspect HTTP status itself; if you use fetch you must throw on !res.ok. The error propagates to onError and to the rejected promise of mutateAsync.

- id: tanstack-query-fn-01
  answer: |
    Because fetch only rejects on network failure, not on HTTP 4xx/5xx. A 500 still resolves to a Response with ok:false, so TanStack sees a fulfilled promise and caches it as data.
    Fix: check res.ok and throw: queryFn: async () => { const res = await fetch(url); if (!res.ok) throw new Error(`${res.status}`); return res.json() } or use a wrapper like axios which throws.

- id: tanstack-query-fn-02
  answer: |
    queryFn may be called many times, out of React lifecycle, deduplicated, retried, cancelled, and run on focus/reconnect. Putting setState/side effects inside makes renders non-deterministic, causes infinite loops (setState -> render -> refetch -> setState...), and breaks suspense/caching.
    QueryFn must be pure: fetch and return data only. Move side effects to useEffect watching data, or to select/ onSuccess callbacks (removed in v5 -> useEffect), or to queryClient callbacks.

- id: tanstack-query-fn-03
  answer: |
    Express dependency with enabled flag:
    const query = useQuery({ queryKey: ['user', userId], queryFn: () => fetchUser(userId), enabled: !!userId })
    Until userId is truthy, the query does NOT fire. In v5 state is pending (isPending true, fetchStatus: 'idle', data undefined, no error). Previously isLoading true. You can also use enable: Boolean(userId). The dependent query will automatically enable and fetch when userId becomes defined.

- id: tanstack-suspense-01
  answer: |
    useSuspenseQuery guarantees data is defined - it never returns undefined/isLoading pending state. Instead it SUSPENDS rendering until the fetch resolves.
    What must wrap it: a React <Suspense fallback={...}> boundary (and it requires suspense: true behavior). Also typically an ErrorBoundary for errors. The API omits isLoading/isPending disabled states; you only get data, error via boundary, and isFetching for background refetches.

- id: tanstack-suspense-02
  answer: |
    With useSuspenseQuery fetch errors are NOT returned as query.error; they are THROWN as promises/errors and propagate to the nearest Error Boundary (React error handling), like other Suspense errors.
    Handle with <ErrorBoundary fallbackRender> around the <Suspense> island. For per-query error UI without boundary, you would need to use plain useQuery. You can still catch via useSuspenseQuery's error is not populated; but you can use queryOptions + ErrorBoundary.

- id: tanstack-suspense-03
  answer: |
    Each useSuspenseQuery suspends its component independently. If parent renders SibA then SibB sequentially, SibB's render/fetch doesn't start until SibA's Suspense resolves, causing waterfall: fetchA -> renderA -> fetchB -> renderB.
    Avoid by starting fetches in parallel BEFORE suspending: hoist both queries to a common parent, or prefetch/ensureQueryData in a route loader, or use useSuspenseQueries combined. TanStack also offers prefetching so both promises are in-flight before any Suspense boundary suspends.

- id: tanstack-suspense-04
  answer: |
    No. useSuspenseQuery deliberately does NOT support enabled:false / conditional skip. Suspense contract requires the component suspends until data is available; a disabled query would suspend forever with no way to resolve. For dependent/conditional fetching use plain useQuery with enabled, or conditionally render the Suspense child only when the prerequisite is ready. If you need enabled logic with suspense, prefetch conditionally in loader.

- id: tanstack-prefetch-01
  answer: |
    Use ensureQueryData when you need guaranteed data before rendering (router loader / SSR).
    prefetchQuery is fire-and-forget: starts fetch, does not return/throw data, swallows errors. EnsureQueryData returns the data (fetching if missing, or returning cached if fresh) and throws if error, so awaiting it guarantees the cache is populated for the subsequent useSuspenseQuery. In a loader: await queryClient.ensureQueryData(options) then component's useSuspenseQuery reads instantly without suspend waterfall.

- id: tanstack-prefetch-02
  answer: |
    Both populate cache without a subscriber, but error handling differs:
    fetchQuery returns data and THROWS on error (promise rejects), so caller must try/catch; it fails the loader if uncaught.
    prefetchQuery never throws - it catches and stores error in cache state but resolves void, so awaiting it never rejects. Use fetchQuery/ensureQueryData when you need data or want errors to bubble; use prefetchQuery for best-effort warm-up where failure should not block navigation/render.

- id: tanstack-router-loaders-01
  answer: |
    A route loader is an async function on the route definition (createFileRoute('/posts')({ loader: async ({ params, context }) => fetch... })) that runs BEFORE the component renders, during matching/preload. Component reads result via Route.useLoaderData() or loaderData prop. Unlike useEffect fetch inside component (runs AFTER render, causes loading spinner, waterfall, no blocking), loader blocks navigation until done (with pendingComponent), can run in parallel during preload, and supports caching/invalidation via router.

- id: tanstack-router-loaders-02
  answer: |
    Use the same QueryClient instance via router context. In loader, call queryClient.ensureQueryData(queryOptions) or fetchQuery. In component, call useSuspenseQuery/useQuery with SAME key/options. Because they share the client cache, loader populates cache and component reads without double fetch. Pattern: export const postQueryOptions = (id) => ({ queryKey: ['posts', id], queryFn: ... }); loader: ({ context: { queryClient }, params }) => queryClient.ensureQueryData(postQueryOptions(params.id)); component: useSuspenseQuery(postQueryOptions(id)).

- id: tanstack-router-loaders-03
  answer: |
    Default preload: 'intent' means on link hover / touchStart the router prefetches that route's matches without navigating: it runs loaders, beforeLoad, and ensures data so navigation is instant.
    Functions run during preload: beforeLoad and loader (and validateSearch). Components are NOT rendered until actual navigation. You can control with <Link preload="intent"|"render"|false> or routeOptions preload.

- id: tanstack-router-loaders-04
  answer: |
    beforeLoad runs BEFORE loader and before search validation, intended for auth, context injection, redirects, and providing data to child routes. loader runs after beforeLoad and is for data fetching.
    router context is a typed dependency-injection object passed down the route tree (e.g. { queryClient, auth }). beforeLoad can augment it via return { context: { user } } or read it, enabling loaders/components to access shared clients without globals. beforeLoad can throw redirect() to abort navigation.

- id: tanstack-router-search-01
  answer: |
    On file route define validateSearch: (search) => zodSchema.parse(search) or with zod: validateSearch: z.object({ page: z.number().int().min(1).catch(1), q: z.string().optional() })
    TanStack Router calls it on every navigation to parse raw URLSearchParams into typed object. Why validate: URL is untrusted user input - need defaults, coercion (string->number), and protection against malformed values; validation gives typed search object and prevents crashes, plus enables typed links/navigate.

- id: tanstack-router-search-02
  answer: |
    Store UI filter in validated search params, not component state. Update via router navigation: const navigate = useNavigate({ from: '/posts' }); navigate({ search: (prev) => ({...prev, page: nextPage }) }) or <Link search={{ page: 2 }}>.
    Anti-pattern is const [page, setPage] = useState() that is disconnected from URL -> not bookmarkable, back button breaks, loader unaware, and reload loses state. Keep URL as source of truth; derive filter from Route.useSearch().

- id: tanstack-router-search-03
  answer: |
    Loader is decoupled from search by design to avoid over-fetching on every search change. It does not auto-rerun when search changes.
    To make loader depend on search, add loaderDeps: ({ search }) => ({ page: search.page }) - this declares that search.page is a dependency; when it changes the router invalidates and reruns loader. Then loader reads deps: loader: async ({ deps: { page } }) => fetchPage(page).

- id: tanstack-router-search-04
  answer: |
    Once validateSearch defines the schema for /posts, all navigation APIs become strictly typed: <Link to="/posts" search={{ page: 2, q: 'hi' }} /> and navigate({ to: '/posts', search: ... }) autocomplete required keys, enforce types (page must be number), and error if missing required fields or passing unknown keys. Also useSearch() and loaderDeps are typed to that schema. This gives end-to-end URL type safety.

- id: tanstack-router-typesafety-01
  answer: |
    File-based routing: files under src/routes (e.g. routes/posts.$postId.tsx) map to routes via Vite plugin @tanstack/router-plugin. On generation it creates src/routeTree.gen.ts - a generated manifest that imports every route module and builds the typed route tree with params, search, context, loaders.
    This file is the single source of truth enabling compile-time checks for Link to/params/search, useParams, useSearch, and route context across the app.

- id: tanstack-router-typesafety-02
  answer: |
    It checks at compile time that: 1) "/posts/$postId" is a real registered route (typo fails), 2) params object exactly matches dynamic segments - { postId: string } required, no extra keys, correct type (string), and 3) search (if validated) must match route's search schema. So <Link to="/posts/$postId" params={{ postId: '5' }} /> passes, but params={{ id: '5' }} or to="/post/5" or missing postId is a TypeScript error, and params={{ postId: 5 }} (number) may also error per config. Navigate has same checks.
```
