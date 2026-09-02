```yaml
- id: tanstack-query-keys-01
  answer: |
    A query key is an array (e.g. ['todos'] or ['todos', 5]) that uniquely identifies a query and its cached data. TanStack Query deterministically serializes/hashes the key (stable JSON-like hash; object keys sorted; nested structures handled depth-first) and treats two keys as the same cache entry if and only if their hashed keys are identical. Keys must be arrays from the root — top-level primitives or strings alone aren't valid.
- id: tanstack-query-keys-02
  answer: |
    Because hierarchical keys enable partial (prefix) matching: invalidateQueries({ queryKey: ['todos'] }) matches ['todos'] itself and every child key like ['todos', todoId]. The client walks the key tree and invalidates everything below the prefix. This lets you invalidate a whole collection and its detail views in one call; with flat, ad-hoc keys you'd have to track and invalidate each entry manually. The same prefix matching applies to setQueryData's filtering and refetch scoping.
- id: tanstack-query-keys-03
  answer: |
    The key is what identifies the cache entry, so all users share one ['projects'] entry: switching userId serves whatever was cached under the same key (wrong user's data), and no refetch is triggered because the key never changed. Rule: every variable the queryFn reads must be part of the key — e.g. ['projects', userId]. If the queryFn reads it, the key must contain it.
- id: tanstack-query-keys-04
  answer: |
    One. TanStack Query deduplicates concurrent fetches for the same key: the first observer starts the request, and the second observer attaches to the same in-flight query instead of issuing its own fetch. Both components receive the same data, and the request completes once.
- id: tanstack-caching-01
  answer: |
    staleTime is how long data is considered fresh: during that window no automatic refetch happens on mount, window focus, or reconnect. gcTime (formerly cacheTime) is how long an inactive cache entry (no observers, i.e. after the last component using it unmounts) is kept in memory before garbage collection. Defaults: staleTime 0, gcTime 5 minutes. Key distinction: "stale" doesn't mean deleted — stale data is still served from cache, it's just eligible for background refetch. And fresh data isn't kept forever — it still gets GC'd once inactive past gcTime.
- id: tanstack-caching-02
  answer: |
    With staleTime 0 the cached data is immediately stale the moment it exists. On remount (within gcTime), the cached data is rendered instantly while a background refetch fires — classic stale-while-revalidate. So the user sees the previous data right away (no loading spinner, since data exists), isFetching is true, and the UI quietly swaps in the new data when the response arrives. If the entry were garbage-collected (past gcTime), it would instead be a cold fetch with a visible loading state.
- id: tanstack-invalidation-01
  answer: |
    It marks every query whose key prefix-matches 'todos' as stale (regardless of active/inactive), then immediately refetches in the background only the queries that currently have active observers. Inactive ones just stay flagged stale and will refetch the next time they're used (e.g. on mount). The cached data isn't deleted — it keeps rendering until refetched data arrives. The call returns a promise that resolves when the triggered refetches finish.
- id: tanstack-invalidation-02
  answer: |
    Use setQueryData when the mutation's response is exactly the authoritative new state and shape for a known query key — you write it straight into the cache for an instant, flicker-free update with no extra round trip. Tradeoff: you're trusting your client-side construction to match server truth; if the server does extra normalization or side effects, the cache silently diverges from the backend. invalidateQueries is always correct (it refetches server truth) but costs network requests and can flash refetching states. Common combo: optimistic setQueryData in onMutate plus invalidateQueries in onSettled as the reconciliation safety net.
- id: tanstack-mutations-01
  answer: |
    useQuery is a declarative read: it auto-runs based on its queryKey, is cached/deduped, and refetches on mount/focus etc. useMutation is an imperative write: it never runs on its own, has no queryKey or cache entry, and executes once per call. Trigger it by calling mutate(variables) (typically in an event handler) or await mutateAsync(variables). Track it via the returned state — isPending (v5 renamed isLoading → isPending), isError, error, isSuccess, data, variables — or via the onMutate/onError/onSuccess/onSettled callbacks. Lifecycle is idle → pending → success/error (resets to idle per call in v5).
- id: tanstack-mutations-02
  answer: |
    A mutation changes server state, which silently invalidates everything the client derived from the old state — the client has no way to know which caches are now wrong. Calling queryClient.invalidateQueries in onSuccess marks the affected queries stale and refetches the active ones, pulling the UI back to server truth after the write. It accomplishes "reconcile by refetch" — you avoid hand-patching cache entries and the bugs that come from a wrong manual update.
- id: tanstack-mutations-03
  answer: |
    onMutate: runs before the mutationFn — cancelQueries the affected query (stop in-flight refetches clobbering your optimistic write), snapshot the previous value with getQueryData, write the optimistic value with setQueryData, and return a context object containing the snapshot. onError: receives that context — roll back by setQueryData(key, context.previousValue) and surface an error UI. onSettled: runs on success or failure — invalidateQueries the key so the cache is reconciled with actual server state either way. Caveat: overlapping mutations of the same key can clobber each other's optimistic state, so this pattern needs care with concurrent writes.
- id: tanstack-mutations-04
  answer: |
    mutate is fire-and-forget: it returns nothing, never throws, and routes failures to your onError callback (safe in event handlers). mutateAsync returns the mutation's promise so you can await or compose it (e.g. chain navigation after success), but rejections propagate — you must catch them yourself or you get an unhandled promise rejection. A mutation knows it failed when the promise returned by mutationFn rejects — either a thrown Error or a network failure; a fetch-based mutationFn still has to check response.ok and throw itself, since fetch doesn't reject on HTTP error statuses.
- id: tanstack-query-fn-01
  answer: |
    fetch only rejects on network-level failures (DNS, offline, aborted); an HTTP 500 still resolves — with the error body as the resolved value. So TanStack Query sees a successful result, puts the error payload in data, and the UI never shows an error. Fix: check res.ok inside the queryFn and throw an Error when it's false; the query then transitions to isError with that error. (Libraries like axios throw on non-2xx automatically, which is why the bug usually only appears with raw fetch.)
- id: tanstack-query-fn-02
  answer: |
    A queryFn must be a pure fetcher of data. TanStack Query calls it at unpredictable times and possibly many times — retries, refetch on focus/reconnect, dedup races across observers — so a setState inside it runs repeatedly and out of order. The fetched data is owned by the cache, not the component: side-effect state duplicates cache state, diverges on refetch, and is lost when the component unmounts/remounts. Instead: let the query own the data (key + queryFn), render from the returned data, and do side effects in event handlers or in an effect that reacts to the query's data in the component. (v5 removed the per-query onSuccess/onSettled options, so effects/callbacks in the component are the intended place.)
- id: tanstack-query-fn-03
  answer: |
    Use enabled: !!userId (a dependent/conditional query); v5 also offers passing skipToken as the queryFn for the same effect. Until userId exists, the query is created but never fetches: status is 'pending' with fetchStatus 'idle' and data is undefined. Consequence: you can't treat isPending alone as "loading" for a dependent query — pending + idle means "waiting to be enabled", while pending + fetching means actually loading. When userId arrives, enabled flips true and the query fetches automatically.
- id: tanstack-suspense-01
  answer: |
    useSuspenseQuery throws the fetch promise up to React, so the component suspends until data resolves, and it throws errors to error boundaries. Because of that, it guarantees data is defined — no undefined handling or TS narrowing needed — and drops loading-state handling from the component. The component must be wrapped in a <Suspense> boundary (which renders the fallback while suspended) and, for failures, an error boundary above it.
- id: tanstack-suspense-02
  answer: |
    Errors don't appear in the component's isError — the error is thrown (after retries exhaust, per the default throwOnError behavior for suspense queries) to the nearest error boundary above the Suspense boundary. You handle them with a class error boundary or react-error-boundary's <ErrorBoundary>, rendering a fallback UI, typically with a retry/reset action that refetches via queryClient (e.g. refetchQueries or reset on the boundary).
- id: tanstack-suspense-03
  answer: |
    Each component's query starts when that component renders. Under Suspense, any serialization in rendering cascades the fetches: a parent that suspends delays children, nested/lazy components render only after their ancestors resolve, and SSR streaming boundaries flush sequentially — so query N+1 doesn't start until query N resolves, producing a request waterfall. (Literal siblings inside one Suspense boundary generally do start in parallel in React 18, but the whole boundary shows a fallback until all resolve.) Avoid it by hoisting data fetching to the highest common parent — one component issues the independent queries in the same render so they run concurrently — combining into a single query, or kicking off the fetches before render (e.g. router loaders / prefetch).
- id: tanstack-suspense-04
  answer: |
    No. useSuspenseQuery doesn't support conditional fetching: with enabled: false nothing ever starts the fetch, so the promise the query throws never resolves and the component suspends forever. Suspense's whole contract is "when this component renders, data exists." For dependent queries use useQuery with enabled (or skipToken), where a pending/idle query with undefined data is representable.
- id: tanstack-prefetch-01
  answer: |
    Use await queryClient.ensureQueryData(...) in the loader. prefetchQuery is fire-and-forget: its promise always resolves (errors are swallowed, result is undefined) so awaiting it guarantees nothing, and it's meant for opportunistic cache warming. ensureQueryData resolves with the actual data and throws on error, and it short-circuits the network if a cached entry exists that's still fresh per staleTime — so when the component mounts, its useQuery with the same key hits the cache and renders with data immediately.
- id: tanstack-prefetch-02
  answer: |
    fetchQuery fetches and resolves with the data itself (rejecting on error) — use it when the loader/async code needs the value directly, independent of the hook cache lifecycle. prefetchQuery only warms the cache, never throws (a failure just resolves to undefined), and is for opportunistic warming like hover preload. Error-handling behavior matters when the caller must react to failure — a router loader that should redirect or render an error route on failure needs fetchQuery/ensureQueryData so the rejection is observable; with prefetchQuery a failed background prefetch would look like success and the component would just fetch on mount.
- id: tanstack-router-loaders-01
  answer: |
    A loader is an async function defined on a route that runs during navigation, before the route component renders; whatever it returns becomes loaderData, read in the component via useLoaderData() (or Route.useLoaderData() with file-based routing). Unlike a useEffect fetch, it starts before the component mounts — so there's no mount-then-flash-of-loading, no render-then-fetch waterfall — and it's tied to navigation: the router can show pending UI, and navigation is blocked/aborted-aware while it runs. The component renders with data already present on first paint.
- id: tanstack-router-loaders-02
  answer: |
    Put the queryClient in router context (createRouter({ context: { queryClient } }), typed via the root route's context) and in the loader call await context.queryClient.ensureQueryData({ queryKey, queryFn }) using exactly the same queryKey the component's useQuery uses. The loader populates the cache; the component's useQuery then finds that entry (kept fresh via matching staleTime) and reads from cache without issuing a second request. The shared key is the contract that makes loader and hook one cache instead of two fetches.
- id: tanstack-router-loaders-03
  answer: |
    With the default preload: 'intent', hovering/focusing (or touchstart on) a Link starts loading the destination route before the click, so navigation feels instant. During preload the router loads the target route's code (lazy chunks) and runs its data functions — beforeLoad and loader — to warm data and route context; the component itself doesn't render. There's a small intent delay (I believe ~50ms by default, though I'm not certain of the exact number) so brushing over a link doesn't trigger fetches. Should-be-skipped behavior (staleTime-based) still applies to whether the loader refetches.
- id: tanstack-router-loaders-04
  answer: |
    beforeLoad runs before the loader on navigation to the route and is for gating and preparation: auth/permission checks, throwing redirect() or notFound(), or computing values. Its return value is not loaderData — it's merged into the route's context, available to the loader, child routes, and useRouteContext. loader then fetches the route's data and returns loaderData (read via useLoaderData), with its re-running controllable via staleTime/shouldReload. Router context is the typed dependency container supplied when creating the router — createRouter({ context: { queryClient } }), typed via createRootRouteWithContext<{ queryClient: QueryClient }>() — so loaders/beforeLoad receive shared services like the queryClient.
- id: tanstack-router-search-01
  answer: |
    Define validateSearch on the route: a function receiving the raw search object and returning the parsed, validated one — hand-written checks or a schema library like zod (schema.parse). The router runs it on every navigation and exposes the validated result (typed) through useSearch()/Link typing; invalid input throws, which you can catch/redirect. Validate because the URL is untrusted external input: validation gives you coercion ('3' → 3), defaults, stripping of garbage keys, and end-to-end types instead of string | undefined everywhere.
- id: tanstack-router-search-02
  answer: |
    Model it as route search state: declare the field (e.g. page) in the route's validateSearch, read it with Route.useSearch(), and update it via navigate({ search: (prev) => ({ ...prev, page: next }) }) or a Link's search prop — the router keeps URL and state in sync, so it's shareable, survives refresh, and works with back/forward. The anti-pattern is keeping the filter in useState (or a store) so the URL never reflects it — the state is lost on refresh, unshareable, and breaks history navigation.
- id: tanstack-router-search-03
  answer: |
    Loaders deliberately receive only params (and context, etc.) — search params are excluded so loaders don't re-run on every unrelated search-param change. You opt in explicitly with loaderDeps on the route: a function that picks the search values the loader needs, e.g. loaderDeps: ({ search }) => ({ page: search.page }); those deps are then passed to the loader and used as its re-run trigger. So changing page re-runs the loader, while unrelated search params don't. (Fairly confident about loaderDeps; the precise destructuring shape I could be slightly off on.)
- id: tanstack-router-search-04
  answer: |
    Navigation becomes type-checked against the validated shape: <Link> / navigate to that route get a typed search prop where required validated keys must be present with correct types, wrong types or misspelled keys are compile errors, and you get autocomplete for the keys. Reading is typed too — useSearch() on that route returns the validated type (e.g. page: number, not string | undefined). In short, the URL contract declared in validateSearch is enforced at compile time across every link, navigate call, and hook usage.
- id: tanstack-router-typesafety-01
  answer: |
    The file-based routing plugin (Vite/Webpack/etc.) watches the routes directory and generates routeTree.gen.ts: an explicit route tree where each node is a route with its own types — path, params, validateSearch shape, loader data, context. That generated tree is passed to createRouter, and the resulting router type flows through RouterProvider so TypeScript can infer every route's contract everywhere: Link `to` values, useParams, useSearch, useLoaderData all type-check against the tree. routeTree.gen.ts is auto-generated — never hand-edit it; it's regenerated on dev/build (teams differ on committing it vs. gitignoring it).
- id: tanstack-router-typesafety-02
  answer: |
    It checks at compile time that the literal path '/posts/$postId' exists in the generated route tree (a typo or unknown route is a type error), that the params object supplies postId as a string, and that no required params are missing or extraneous ones added; if the route declared required search params via validateSearch, those are enforced too. You also get path autocompletion. Net effect: broken links and missing/misspelled params fail at build time instead of resolving to dead URLs at runtime.
```

**Assumptions & uncertainty notes (non-interactive run):** Answered all 31 questions closed-book from my own knowledge of TanStack Query v5 and TanStack Router v1 conventions; no tools, web, or repo files were used. I flagged lower-confidence details inline: the exact preload intent-delay value (~50ms, question `tanstack-router-loaders-03`), the precise `loaderDeps` destructuring shape (`tanstack-router-search-03`), and the parallel-vs-serial nuance for sibling suspense queries (`tanstack-suspense-03`). Where v5 renamed things (isLoading→isPending, cacheTime→gcTime), I answered with v5 naming.
