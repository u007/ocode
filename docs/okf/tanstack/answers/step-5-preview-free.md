# step-5-preview-free — closed-book answers for tanstack (opencode-go, 2026-10-12); answerer saw only _prompts/tanstack.md
## Assumptions stated
- Answered purely from memory (no corpus, no web, no repo access). Where APIs changed between versions I defaulted to TanStack Query **v5** and TanStack Router **v1**.
- Where I am uncertain I say so inside the answer rather than guessing to look complete.

- id: tanstack-query-keys-01
  answer: |
    A query key is the array a query is cached and observed under, e.g. ['todos', 5].
    The top level must be an array; inside you can use primitives or plain objects.
    To decide whether two keys are the same entry, the QueryClient serializes each key
    into a stable string hash using deterministic JSON (object properties are sorted),
    so ['todos', {a: 1, b: 2}] and ['todos', {b: 2, a: 1}] hash identically.
    Same hash = same cache entry = shared fetch promise and shared cache handling.

- id: tanstack-query-keys-02
  answer: |
    Because invalidation and filtering use prefix (partial) matching by default.
    A key ['todos'] matches ['todos'], ['todos', 5], ['todos', {status: 'done'}] and
    any longer key that starts with it — so invalidating the broad key cascades to
    every specific variation without enumerating them. Structure keys from most
    general to most specific so you can invalidate at whatever granularity you need.
    Pass exact: true when you want only the literal key.

- id: tanstack-query-keys-03
  answer: |
    Every user of the app shares the single cache entry ['projects'], so switching
    users shows the previous user's data (cross-contamination), and invalidation
    cannot be scoped per user.
    Rule: every variable the queryFn uses that can change the response must be part
    of the key — here ['projects', userId] (plus filters, page, locale, etc.).
    Anything not in the key is invisible to the cache.

- id: tanstack-query-keys-04
  answer: |
    One network request. The second useQuery attaches as an observer to the same cache
    entry; a fetch already in flight is deduplicated, and both components await the
    same promise and receive the same data.

- id: tanstack-caching-01
  answer: |
    staleTime: how long cached data is considered fresh. While fresh, the query will
    not refetch on mount, window refocus, reconnect etc. Default 0 = data is stale
    the instant it arrives.
    gcTime (called cacheTime in v4): how long a query entry with no observers
    (nothing mounted) is retained before garbage collection. Default 5 minutes.
    staleTime is about whether to re-fetch; gcTime is about how long to keep data
    nobody is looking at anymore.

- id: tanstack-caching-02
  answer: |
    With staleTime 0 the data is stale immediately, so a remount is treated as a
    remount of an out-of-date query. If the remount happens within gcTime (5 min
    default), the cached data is served synchronously in the same render pass —
    the component shows the old content right away (status 'success', no spinner),
    and a background refetch fires (isFetching true). User-visible: stale-then-updated
    content, with an indicator only if you render isFetching. If gcTime has elapsed,
    the entry was garbage-collected and it is a cold fetch with a real loading state.

- id: tanstack-invalidation-01
  answer: |
    It prefix-matches all queries whose key starts with ['todos'] (exact defaults to
    false), marks them stale, and refetches the ones that currently have active
    observers (default refetchType: 'active'). Inactive matching queries are only
    marked stale, so they refetch on next mount. It returns a promise that resolves
    once those refetches settle. You can narrow with exact, predicate, or type filters.

- id: tanstack-invalidation-02
  answer: |
    Use setQueryData when you already know the resulting server state (the mutation
    response contains the updated entity) and want the UI updated instantly with no
    network cost.
    Tradeoff: you are asserting what the server state is. If you compute it wrong,
    or if other data also changed (list counts, related entities), the cache drifts
    from the server. invalidateQueries costs a request and a brief refetch window
    but re-derives from the authoritative source, so the cache is guaranteed correct.
    Common hybrid: set the single entity from the response, invalidate the lists.

- id: tanstack-mutations-01
  answer: |
    useQuery is declarative — it runs its queryFn automatically and refetches based
    on its policy. useMutation runs nothing until you call mutate(variables) or
    mutateAsync(variables) from an event handler.
    v5 status naming: mutation.status is 'idle' | 'pending' | 'success' | 'error'
    (v4 used 'loading' instead of 'pending'), with the matching booleans
    isPending/isError/isSuccess/isIdle, plus data, error, reset(), and the callbacks
    onMutate, onSuccess, onError, onSettled.

- id: tanstack-mutations-02
  answer: |
    A successful mutation changes server state, so everything derived from that state
    on the client is now stale. Calling queryClient.invalidateQueries marks the
    affected queries stale and refetches those with active observers, so all
    subscribers re-sync with the authoritative server data instead of the client
    guessing or hand-patching what changed.

- id: tanstack-mutations-03
  answer: |
    onMutate: cancel in-flight refetches for the affected queries
    (queryClient.cancelQueries) so they cannot overwrite your optimistic value;
    snapshot the current data (getQueryData); write the optimistic value
    (setQueryData); return the snapshot as the rollback context.
    onError(context): roll back by setQueryData-ing the snapshot back in.
    onSettled (and/or onSuccess): invalidate the query so the next refetch reconciles
    with the server regardless of success or failure.
    Responsibility split: onMutate = optimistic write + record how to undo;
    onError = undo; onSettled = reconcile with the source of truth.

- id: tanstack-mutations-04
  answer: |
    mutate is fire-and-forget driven by callbacks — you cannot .catch() it; failure
    surfaces via onError, mutation.status 'error' and mutation.error (and an
    unhandled rejection if there is no handler). mutateAsync returns a promise that
    resolves with the data or rejects with the error, so you can await it or attach
    .then/.catch yourself — useful in handlers or loaders that need the result.
    A mutation knows it failed when mutationFn throws or returns a rejected promise:
    status flips pending → error, error is populated, onError runs.

- id: tanstack-query-fn-01
  answer: |
    Because fetch only rejects on network/transport failure — an HTTP 500 resolves
    normally to a Response whose ok is false. The query therefore sees a successful
    promise and hands the error response body to the UI as data.
    Fix: inspect the response in the queryFn and throw, e.g.
    if (!res.ok) throw new Error('Request failed'). Anything thrown or rejected
    inside a queryFn becomes the query error.

- id: tanstack-query-fn-02
  answer: |
    A queryFn is a cacheable, shared, idempotent function keyed by the query key: it
    may run once for many observers, re-run on refetches, or not run at all when
    fresh data exists. Side effects (setState, toasts, navigation) attached to it
    therefore fire the wrong number of times or at the wrong moments and couple
    caching to UI. Do UI work from the component (derive from data/status) or in the
    sanctioned side-effect hooks onSuccess/onError/onSettled.

- id: tanstack-query-fn-03
  answer: |
    Use enabled: const q = useQuery({ queryKey: ['profile', userId],
    queryFn: fetchProfile, enabled: Boolean(userId) }). Until userId is set no
    request fires and the query is not loading — it sits in status 'pending' with
    fetchStatus 'idle' (in v4 this was surfaced as the 'idle' status). Once enabled
    flips true it fetches normally.

- id: tanstack-suspense-01
  answer: |
    useSuspenseQuery removes the loading branch: while fetching it suspends the
    component through React Suspense, and the returned data is typed non-undefined
    (there is no isPending, and status is effectively 'success'). Because it suspends
    render, a <Suspense> boundary with a fallback must wrap the component using it.
    It also never returns an error object — failures are thrown.

- id: tanstack-suspense-02
  answer: |
    Fetch errors are thrown out of render/suspend resolution and propagate to the
    nearest React Error Boundary — that is where you handle them (e.g. wrap in
    react-error-boundary and offer a retry via resetErrorBoundary).
    useSuspenseQuery deliberately gives you no .error to inspect; conversation of
    errors belongs to the boundary. useQueryErrorResetBoundary is the companion for
    resetting when retrying.

- id: tanstack-suspense-03
  answer: |
    A fetch only starts when the component calling useSuspenseQuery renders. If the
    siblings sit under separate Suspense boundaries, or a suspending parent delays
    the commit of the second sibling subtree, one fetch must finish before the other
    component renders and starts its own fetch — requests serialize into a
    waterfall. Avoid it by having both fetches in flight before those components
    render: prefetch/ensure them in a router loader, kick off usePrefetchQuery in a
    parent that does not suspend, or prefetch in parallel (e.g. a single Promise.all
    pass / shared boundary) so the requests overlap.

- id: tanstack-suspense-04
  answer: |
    No. A useSuspenseQuery with enabled: false has no data and never resolves, so the
    component suspends indefinitely — and the option types disallow enabled: false
    for useSuspenseQuery at the TypeScript level. For a dependent query, make sure the
    dependency resolves before the dependent component mounts: gate in beforeLoad /
    the loader, render the child only once the parent data exists, or use a plain
    useQuery for that conditional hop instead.

- id: tanstack-prefetch-01
  answer: |
    Use ensureQueryData in a router loader when the data must be guaranteed available
    before render. It resolves with the data: from the cache if still fresh
    (no fetch), otherwise it fetches, awaits, and returns it, and it propagates
    errors. prefetchQuery is fire-and-forget: it starts a fetch only when needed,
    returns a void promise, and swallows errors. So: prefetch to warm a cache you can
    tolerate being cold; ensure when the route actually depends on the data.

- id: tanstack-prefetch-02
  answer: |
    Both are "fetch only if stale or missing" primitives. The difference is the
    contract: fetchQuery returns the data promise and rejects on failure, so you can
    consume the value and must handle the error. prefetchQuery returns a void promise
    and catches errors internally, so it can never produce an unhandled rejection.
    This matters in loaders/SSR, where a rejected fetchQuery can (and should) be
    handled deliberately — bubbled to the router's error component or caught and
    swapped for a fallback — whereas prefetchQuery failing ought to leave you on the
    normal loading state rather than blowing up navigation.

- id: tanstack-router-loaders-01
  answer: |
    A loader is a per-route async function the router awaits before that route's
    component renders — it runs during navigation (and on the server in SSR). The
    component reads it with Route.useLoaderData() (or
    useLoaderData({ from: '/route/path' })), typed from the loader's return value.
    Unlike a useEffect fetch: data loading starts during navigation, not after the
    component mounts (no fetch-after-render waterfall); the loader can short-circuit
    with throw redirect() and can run ahead of time during preloading; and it only
    sees route params/search/context — not component state, refs or hooks.

- id: tanstack-router-loaders-02
  answer: |
    Create one QueryClient and pass it in through router context (plus a provider for
    the components), then have the loader call
    queryClient.ensureQueryData({ queryKey, queryFn }) with the same key the
    component's useQuery uses. The loader awaits the query, so by the time the
    component renders the data is already in the shared cache and the useQuery hits
    it instead of double-fetching. For SSR, dehydrate on the server into the HTML
    payload and HydrationBoundary + hydrate on the client.

- id: tanstack-router-loaders-03
  answer: |
    Preloading warms a route before the user actually navigates. Default preload is
    'intent' (hover/pointer-in, with preloadStaleTime so recent work is reused), so
    hovering a link runs the matched route chain's route functions — beforeLoad and
    loader — ahead of the click. It runs route functions only: the component itself
    is never rendered on preload, so no useEffect or component-side code runs. The
    payoff is that the actual navigation finds the data cached and feels instant.

- id: tanstack-router-loaders-04
  answer: |
    beforeLoad runs before loaders, for every route in the matched chain (including
    during preload) — its job is gating and context: auth/role checks, redirects
    (throw redirect()), reading cookies/headers, and returning a context object that
    flows down to child loaders. loader is the per-route data step: it receives
    { params, search (only if validateSearch is defined), context } and returns the
    data the component consumes. Router context is an app-wide shared bag configured
    on createRouter and updated at runtime with router.updateContext() — it is how
    loaders and hooks reach shared clients (QueryClient, auth, etc.) without module
    singletons.

- id: tanstack-router-search-01
  answer: |
    Add a validateSearch function to the route, e.g.
    validateSearch: (search: Record<string, unknown>) => ({ page: Number(search.page) || 0 }).
    Its return type becomes the route's search type, so useSearch(), Link search={{ }},
    navigate({ search }) and the loader's search argument are all derived from it.
    Why validate: URL search params are untyped strings. The validator is the single
    place to coerce (string to number), apply defaults, whitelist allowed params, and
    normalize or reject bad values — and it is what unlocks end-to-end compile-time
    types everywhere else.

- id: tanstack-router-search-02
  answer: |
    Keep it in the URL as the single source of truth: read with useSearch() and
    write with navigate({ search: (prev) => ({ ...prev, page: nextPage }) }) using the
    function updater so you merge instead of replace. You get back/forward, refreshed
    loaders, and shareable links for free.
    The anti-patterns: mirroring the page number in useState while the URL lags behind
    (or vice versa), and hand-rolling history.pushState / query-string concatenation,
    which bypasses validateSearch typing, route matching, and router history.

- id: tanstack-router-search-03
  answer: |
    Search params are only parsed and passed to a loader once the route has declared
    them: until you define validateSearch, the route has no search schema, so the
    router does not know which params to keep or how to type them and the loader
    receives no usable search object. Adding validateSearch (a Zod schema or a plain
    function) makes search a validated, typed argument the loader can read:
    loader: ({ deps }) => ... with search fields declared in deps or read directly.

- id: tanstack-router-search-04
  answer: |
    The validateSearch return type flows across the app: Link search={{ ... }},
    navigate({ search }), useSearch() and the loader's search are all typed from it,
    with editor autocomplete for the allowed params. Missing required params, unknown
    or extra keys, and wrong value types become compile-time errors — combined with
    the typed `to` and `params`, navigating to that route is checked end-to-end
    before you run anything.

- id: tanstack-router-typesafety-01
  answer: |
    File-based routing is code-generated by the TanStack Router plugin (vite/webpack):
    the plugin scans the routes directory and emits routeTree.gen.ts, a static tree
    of route definitions with their ids, full paths, and parent/child relationships.
    createRouter consumes that generated tree, and because the tree encodes the
    literal route paths, TypeScript infers the whole API surface from it — `to`,
    params, search, and route-specific hooks are generics over the concrete tree, so
    typos, unknown paths, missing params, and wrong param types are compile errors.
    routeTree.gen.ts is generated output: commit it so CI can typecheck, never
    hand-edit it, and regenerate via the plugin or CLI.

- id: tanstack-router-typesafety-02
  answer: |
    Compile-time checks for to="/posts/$postId": (1) the literal string is checked
    against the generated route tree — a typo or non-existent route is an error;
    (2) $postId is inferred as a required path param, so params={{ postId: '5' }} is
    checked for the required keys — a missing postId, an extra/unknown param key, or
    a value of the wrong type are errors; (3) if that target route defines
    validateSearch, a search prop would additionally be checked against the route's
    validated search type. So navigation is validated against the real route
    signature before the code ever runs.
```

Note on confidence: highest confidence on query keys/caching/mutations/invalidation; moderately confident on `enabled: false` being disallowed for `useSuspenseQuery` (I stated the behavior and the TS restriction from memory), and on the exact default preload mode being 'intent' for Router links. I would not stake anything on fine details like default timeout values beyond the 5-minute gcTime default.
