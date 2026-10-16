# nvidia/nemotron-3.5-super-vl — closed-book answers for tanstack (openrouter, 2026-10-16); answerer saw only _prompts/tanstack.md
- id: tanstack-query-keys-01
  answer: |
    A query key is a serializable identifier (usually an array like ['todos', 5]) that names a query in TanStack Query's cache. Two keys refer to the same cache entry when they are deeply equal — same entries in the same order — compared via stable serialization/hash, not array identity. So ['todos', 5] built in two different components maps to exactly one cache slot.
- id: tanstack-query-keys-02
  answer: |
    Hierarchical keys share a prefix, so one invalidation can address a whole family of queries. invalidateQueries matches by prefix: { queryKey: ['todos'] } hits ['todos'], ['todos', 1], ['todos', 1, 'details'], and so on. That lets you refresh a list and all its per-item/detail queries with a single call instead of tracking every key individually.
- id: tanstack-query-keys-03
  answer: |
    Every query keyed ['projects'] shares a single cache slot regardless of userId, so different users' results collide and refetches overwrite each other with the wrong user's data. The rule is that anything queryFn uses to decide what to fetch must be part of the query key (e.g. ['projects', userId]); the cache keys on the key alone, never on variables captured inside queryFn.
- id: tanstack-query-keys-04
  answer: |
    One request. TanStack Query dedupes concurrent observers on the same key: while a fetch is in flight, additional useQuery calls with ['user', 5] subscribe to the same in-flight promise/cache entry instead of starting new network requests. Both components receive the same data.
- id: tanstack-caching-01
  answer: |
    staleTime is how long fetched data counts as fresh (default 0, i.e. immediately stale); while fresh, mounts and focus refetches skip the network. gcTime — renamed from cacheTime in v5 — is how long an inactive, unsubscribed cache entry is kept before garbage collection (default 5 minutes). Staleness controls refetching while data is in use; gcTime controls memory retention after nobody is watching.
- id: tanstack-caching-02
  answer: |
    With staleTime: 0 the cached data is stale the instant it arrives. On remount TanStack Query shows the cached data immediately (no loading state if the entry is still in cache) and fires a background refetch, so the user sees old content right away that may quietly update a moment later. If gcTime has elapsed, the entry is gone and the component loads fresh from scratch with a loading state.
- id: tanstack-invalidation-01
  answer: |
    It marks every query whose key matches ['todos'] as a prefix as stale, then refetches the ones that are currently active (have observers). It does not delete entries from the cache, and inactive queries only refetch when they next become active — it is a "refresh the matching active queries" operation, not a cache clear.
- id: tanstack-invalidation-02
  answer: |
    Use setQueryData when you know exactly how the write changes the data (e.g. append the new todo) and want the UI to update instantly without waiting for the network; the tradeoff is that it is a client-side guess that can diverge from the server, so you usually pair it with invalidateQueries afterward to reconcile. invalidateQueries guarantees truth via a server refetch but costs a request and briefly shows stale/loading data.
- id: tanstack-mutations-01
  answer: |
    useQuery reads data declaratively and auto-fetches by key; useMutation performs server writes and only runs when you call mutate or mutateAsync. Trigger with mutate(variables, { onSuccess, onError, onSettled }) and track it via the returned mutation object: isPending (v5 rename of isLoading), isSuccess, isError, isIdle, plus error and reset(). mutateAsync returns a promise instead of being fire-and-forget.
- id: tanstack-mutations-02
  answer: |
    Calling queryClient.invalidateQueries in onSuccess marks related queries stale so they refetch from the server after the write. That makes the UI reflect the mutation (new/updated/deleted items appear in lists) without manually recomputing every cache entry via setQueryData, at the cost of one or more background refetches.
- id: tanstack-mutations-03
  answer: |
    onMutate: cancel conflicting refetches, snapshot the current cache data, apply the optimistic update with setQueryData, and return the snapshot as context. onError: roll back by restoring the snapshot, then refetch to reconcile. onSettled: always run cleanup — refetch/invalidate so the cache matches the server whether the mutation succeeded or failed. The context returned from onMutate is passed to onError and onSettled.
- id: tanstack-mutations-04
  answer: |
    mutate is fire-and-forget (returns void); mutateAsync returns a promise that resolves on success or rejects with the error, so callers can await it. A mutation has failed when its mutation function throws or rejects; the error lands in mutation.error, isError becomes true, and the onError callback fires.
- id: tanstack-query-fn-01
  answer: |
    fetch only rejects on network errors; an HTTP 500 resolves normally with ok: false, so Query sees a successful response and never surfaces an error. The fix is to check response.ok (or status) inside queryFn and throw — e.g. throw new Error(response.statusText) — before calling .json().
- id: tanstack-query-fn-02
  answer: |
    queryFn must be a pure data fetcher: return the data and nothing else. setState or other side effects inside it run outside the cache, break deduping and refetch assumptions, and can cause loops or stale closures. Transform or react to the data in the component (or with select) instead.
- id: tanstack-query-fn-03
  answer: |
    Declare it as a dependent query with enabled: !!userId in the useQuery options. Until userId is set and enabled becomes true, the query stays idle — no fetch, no data, no error — and becomes active only once the dependency exists.
- id: tanstack-suspense-01
  answer: |
    useSuspenseQuery throws a promise while loading instead of returning isLoading state, so React suspends and shows the nearest fallback rather than rendering an empty state. The component tree must be wrapped in <Suspense fallback={...}>, and errors must be caught by an error boundary.
- id: tanstack-suspense-02
  answer: |
    Fetch errors propagate to the nearest React Error Boundary instead of appearing as query error state; there is no try/catch in the component. Handle them with an error boundary/fallback UI — in TanStack Router, the route's errorComponent/errorElement.
- id: tanstack-suspense-03
  answer: |
    Siblings suspend one after another as each renders, so the second fetch does not start until the first resolves — a serial waterfall for independent data. Avoid it by starting both fetches before render, e.g. ensureQueryData/prefetchQuery in the router loader or a shared parent, so the data is ready before either component suspends.
- id: tanstack-suspense-04
  answer: |
    No. useSuspenseQuery does not support enabled: false — it must always resolve data, so there is no "off" switch, since a conditionally disabled query could suspend forever. For dependent suspense queries, gate rendering so the query only starts once the dependency exists, or load the data in a router loader before the component mounts.
- id: tanstack-prefetch-01
  answer: |
    Use ensureQueryData: it guarantees the query has data in cache (fetching if missing or stale) and returns/resolves it before the component renders. prefetchQuery only fires an opportunistic background fetch that respects staleTime and does not guarantee fresh data is available synchronously.
- id: tanstack-prefetch-02
  answer: |
    prefetchQuery fetches only if needed (respects staleTime) and records failures into the cache rather than rejecting; fetchQuery always performs a network request and its returned promise rejects on error, so the caller must handle failures. Error handling matters when a failed fetch should surface to the caller (fetchQuery) versus being silently recorded (prefetchQuery).
- id: tanstack-router-loaders-01
  answer: |
    A route loader is an async function declared on the route that runs before the component renders on navigation and returns data. The component reads it with the router's useLoaderData hook, typed to the loader's return value. Unlike a useEffect fetch, there is no empty/loading first paint — data exists before render and it works during SSR.
- id: tanstack-router-loaders-02
  answer: |
    Inside the loader, call ensureQueryData (or fetchQuery into queryClient) with the same query key the component will use in useQuery. The loader seeds the shared query cache; the component's useQuery reads from it and refetches only if stale, so the data is fetched once instead of twice.
- id: tanstack-router-loaders-03
  answer: |
    Hovering a Link preloads the route ahead of the click — primarily downloading the route's lazy module/component code so the transition is instant. The route loader does not run during preload; it runs when the route is actually entered.
- id: tanstack-router-loaders-04
  answer: |
    beforeLoad runs before loader and is used to guard or redirect navigations (auth checks, aborting, redirects); loader runs after and produces the component's data. Router context is a shared, typed object passed to beforeLoad/loader (e.g. auth session or queryClient) so handlers do not have to close over globals.
- id: tanstack-router-search-01
  answer: |
    Declare validateSearch on the route (e.g. a zod parse of the raw search object) and read params with the router's useSearchParams hook; validation coerces and type-checks URL params. You validate so search params are guaranteed well-formed and typed — invalid URLs are rejected and values like page numbers come out as numbers, not strings.
- id: tanstack-router-search-02
  answer: |
    Keep URL-worthy UI state (page, filter) in search params and update it with router.navigate or setSearchParams from useSearchParams, so it is bookmarkable, shareable, and survives the back button. The anti-pattern is keeping such state only in useState, where it lives nowhere in the URL.
- id: tanstack-router-search-03
  answer: |
    By default a loader only receives route path params and context; search params arrive as an opaque query string unless you opt in. Add validateSearch to the route (e.g. zod parse), which parses search params and passes the typed search object into the loader, so the loader can fetch based on e.g. page.
- id: tanstack-router-search-04
  answer: |
    Links and router.navigate to that route are compile-time checked: to must be a real route, and search params must match validateSearch's shape (right names, right types; missing required params or wrong types are errors). useSearchParams and loader search args are typed throughout the app, so you cannot construct or read an invalid URL.
- id: tanstack-router-typesafety-01
  answer: |
    Route files under src/routes are scanned by TanStack Router's generator, which emits routeTree.gen.ts — a typed tree of every route with its path, params, search schema, and loader data types. The Router is typed with this tree, so links, params, useLoaderData, useSearchParams, and loader returns are checked against one generated source of truth end to end.
- id: tanstack-router-typesafety-02
  answer: |
    It checks that /posts/$postId is a defined route, that postId is required and provided, and that its value matches the declared param type (string). Missing param, unknown param, wrong type, or extra params are compile-time errors; any search params must additionally match validateSearch.
