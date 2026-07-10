# TanStack Benchmark (Query v5 + Router v1) — Human Render

> **Generated from `questions.yaml` (corpus_rev 1). Do not grade from this file
> — `questions.yaml` is the source of truth.** Hand-synced; if they disagree,
> the YAML wins.

Legend: **W** = weight (1–3), **D** = difficulty. Rubric shows scored points
(`•`) and partial-credit levels (`~`). Version scope: TanStack Query v5
(`gcTime`, `isPending`, `useSuspenseQuery`) + TanStack Router v1 (file-based,
validated typesafe search).

---

### tanstack-query-keys-01 · query-keys · W2 · easy
**Q:** What is a query key, and how are two keys judged to be the same cache entry?
**A:** A serializable array identifying a query's data (`['todos', {status:'done'}]`). Hashed deterministically — array order matters, object key order doesn't — so equal hashes share one entry.
• serializable array identifies the cache entry • deterministic hash (object order ignored, array order matters) ~ "id for the query" without the hash detail

### tanstack-query-keys-02 · query-keys, invalidation · W3 · medium
**Q:** Why structure keys hierarchically (`['todos']`, `['todos', id]`), and how does that interact with invalidation?
**A:** Invalidation matches by prefix, so `['todos']` also hits `['todos', 1]`. Generic→specific keys let you invalidate a subtree with one call or one item precisely.
• invalidation matches by prefix (parent invalidates children) • hierarchy enables broad + precise invalidation ~ "keeps keys organized" without the payoff

### tanstack-query-keys-03 · query-keys, query-fn · W2 · medium
**Q:** `queryFn` reads `userId` but the key is `['projects']`. What breaks, and the rule?
**A:** The key is the dependency array — every fetch input belongs in it. Fixed key collides different users in one entry and won't refetch when `userId` changes. Use `['projects', userId]`.
• the key is the dependency array (every fetch input in it) • omission → cache collision / no refetch ~ "add userId" without the dependency-array reasoning

### tanstack-query-keys-04 · query-keys, caching · W2 · medium
**Q:** Two components mount `useQuery(['user', 5])` at once — how many requests, and why?
**A:** One. Same key deduplicates concurrent observers into one in-flight request; staleTime and GC are tracked per key, not per component.
• one request — same key deduplicates • cache/staleTime/gc tracked per key ~ "they share data" without naming dedup

### tanstack-caching-01 · caching · W3 · hard
**Q:** `staleTime` vs `gcTime` (formerly `cacheTime`) — the classic confusion.
**A:** staleTime = how long data is fresh (controls refetching); after it, data is stale and refetch-eligible. gcTime = how long inactive data stays cached after the last observer unmounts before GC. Independent axes. Defaults 0 / 5min.
• staleTime = freshness (controls refetch) • gcTime = retention of inactive data before GC • independent axes; defaults 0 / 5min ~ conflates the two

### tanstack-caching-02 · caching · W2 · medium
**Q:** With default `staleTime: 0`, what happens on remount of a just-fetched query?
**A:** Data is immediately stale, so it returns cached data instantly (no spinner) and refetches in the background — stale-while-revalidate. Raising staleTime suppresses the refetch while fresh.
• returns cache instantly AND background-refetches (SWR) • staleTime:0 = always stale; raising suppresses ~ "uses the cache" without the background-refetch half

### tanstack-invalidation-01 · invalidation · W2 · medium
**Q:** What does `invalidateQueries({ queryKey: ['todos'] })` do to matching queries?
**A:** Marks all prefix-matching queries stale and immediately refetches the active (mounted) ones; inactive ones just flag stale and refetch on next use. Cached data isn't deleted — stale data shows during refetch.
• marks matching queries stale (prefix) • refetches active now, inactive on next use (data not removed) ~ "refetches the data" without the nuance

### tanstack-invalidation-02 · invalidation, caching · W2 · medium
**Q:** When use `setQueryData` instead of `invalidateQueries`, and the tradeoff?
**A:** `setQueryData` writes the new value straight to cache (instant, no request) when the mutation response is authoritative; `invalidateQueries` refetches (safer, costs a request). Often do both. Trap: trusting setQueryData when the server computed fields you didn't set.
• setQueryData writes cache directly (have the data) • invalidateQueries refetches (authoritative, costs a request) ~ one without the tradeoff

### tanstack-mutations-01 · mutations · W2 · easy
**Q:** `useMutation` vs `useQuery`; how to trigger/track (watch v5 naming)?
**A:** Mutations are imperative writes fired with `mutate()`/`mutateAsync()`; they don't run on mount or cache by key. Track via `isPending` (v5, not `isLoading`), `isError`, `isSuccess`, and `onSuccess`/`onError`/`onSettled`.
• mutation imperative (mutate); query auto-runs + caches • v5 in-flight is isPending (not isLoading); has lifecycle callbacks ~ uses v4 isLoading for the mutation

### tanstack-mutations-02 · mutations, invalidation · W2 · medium
**Q:** Why call `invalidateQueries` in a mutation's `onSuccess`?
**A:** The write changed server state, so cached reads are now stale; invalidating the affected keys refetches them and resyncs the UI without threading the response into every view.
• write changed server state → cached reads stale • invalidate in onSuccess refetches to resync ~ "refresh data" without the write→stale link

### tanstack-mutations-03 · mutations · W3 · hard
**Q:** Correct optimistic update with rollback via `onMutate`/`onError`/`onSettled`?
**A:** onMutate: `cancelQueries` (so a late response can't clobber), snapshot with `getQueryData`, optimistically `setQueryData`, return the snapshot as context. onError: roll back via `setQueryData` from the snapshot. onSettled: `invalidateQueries` to reconcile.
• onMutate cancelQueries then snapshots • onMutate optimistic setQueryData + returns snapshot • onError rolls back, onSettled invalidates ~ optimistic write with no cancelQueries/rollback

### tanstack-mutations-04 · mutations, query-fn · W2 · medium
**Q:** `mutate` vs `mutateAsync`, and how does a mutation know it failed?
**A:** `mutate` is fire-and-forget (errors via `onError`/`isError`); `mutateAsync` returns an awaitable Promise for sequencing. Failure = the `mutationFn` throws or rejects — same contract as a queryFn.
• mutate returns void (onError/isError); mutateAsync returns awaitable Promise • failure = mutationFn throws/rejects ~ names both without the throw-to-fail contract

### tanstack-query-fn-01 · query-fn · W3 · medium
**Q:** A `fetch` queryFn never shows 500s as errors. Why, and the fix?
**A:** A query errors only if the queryFn throws/rejects. `fetch` doesn't reject on 4xx/5xx (resolves with `res.ok===false`), so it's treated as data. Check `res.ok` and `throw`.
• query errors only when queryFn throws/rejects • fetch doesn't reject on 4xx/5xx → check res.ok and throw ~ "handle the error" without the must-throw insight

### tanstack-query-fn-02 · query-fn · W2 · medium
**Q:** Why not `setState`/side-effects inside a `queryFn`, and what instead?
**A:** queryFn should be a pure fetch returning data; TanStack Query calls it on its own schedule (refetch/retry/dedup), so setState there causes duplicated/surprising renders. Read from `data`, derive in render, or use lifecycle callbacks.
• queryFn just fetches + returns data (QC controls when it runs) • setState there = unpredictable/duplicated effects; read from data instead ~ "no side effects" without the QC-controls-invocation reason

### tanstack-query-fn-03 · query-fn · W2 · medium
**Q:** Express a dependent query that runs only once `userId` is known — and its state until then?
**A:** `enabled: !!userId`. While disabled it doesn't call queryFn and stays pending with `fetchStatus: 'idle'`; guard render on data. When enabled flips true it fetches.
• use enabled (!!userId) to gate • disabled → no queryFn, pending/idle (fetchStatus idle) ~ names enabled without the pending/idle state

### tanstack-suspense-01 · suspense · W2 · medium
**Q:** What does `useSuspenseQuery` change vs `useQuery`, and what must wrap it?
**A:** It suspends until data exists, so `data` is guaranteed defined and there's no `isPending`/`isLoading` branch. Loading goes to an ancestor `<Suspense>`, errors to an `<ErrorBoundary>`; it must render under a Suspense boundary.
• data guaranteed defined; no loading branch • loading handled by ancestor <Suspense> (errors by ErrorBoundary) ~ "uses Suspense" without the data-never-undefined guarantee

### tanstack-suspense-02 · suspense, query-fn · W2 · medium
**Q:** With `useSuspenseQuery`, where do fetch errors surface and how to handle?
**A:** With no `isError` branch, a thrown/rejected queryFn (after retries) throws during render to the nearest `<ErrorBoundary>`. Pair `<Suspense>` (loading) + `<ErrorBoundary>` (error); the queryFn still must throw.
• errors thrown to nearest ErrorBoundary (no isError branch) • pair Suspense + ErrorBoundary; queryFn must throw ~ "try/catch in the component" — wrong mechanism

### tanstack-suspense-03 · suspense, prefetch · W2 · hard
**Q:** Two sibling `useSuspenseQuery` calls — why a waterfall, and how to avoid?
**A:** If the second mounts only after the first resolves, fetches run sequentially. Kick both off up front — prefetch/`ensureQueryData` (usually in a loader) or hoist the queries so they suspend in parallel.
• sequential mount/suspend → waterfall • prefetch/ensureQueryData up front or hoist to parallelize ~ notices waterfall, no concrete fix

### tanstack-suspense-04 · suspense · W2 · medium
**Q:** Can you use `enabled: false` on `useSuspenseQuery` to build a dependent query?
**A:** No — `useSuspenseQuery` has no `enabled` (a disabled one would suspend forever). Restructure: only mount the suspense component once inputs exist, or use `useQuery` + `enabled`.
• useSuspenseQuery has no enabled — can't gate that way • restructure: mount only once inputs exist (or useQuery+enabled) ~ "just set enabled: false" — the wrong answer (score 0)

### tanstack-prefetch-01 · prefetch · W2 · medium
**Q:** For guaranteed data before render in a loader — `prefetchQuery` or `ensureQueryData`?
**A:** `ensureQueryData` — returns cached-or-fetched data you can `await`, so the loader guarantees readiness. `prefetchQuery` is fire-and-forget and returns void. Pairs with `useSuspenseQuery`.
• ensureQueryData returns data (cached-or-fetched), awaitable • prefetchQuery returns void; ensureQueryData for guaranteed-ready loader data ~ picks ensureQueryData without the return/await why

### tanstack-prefetch-02 · prefetch · W2 · medium
**Q:** Contrast `prefetchQuery` with `fetchQuery`. When does error handling matter?
**A:** Both cache by key and respect staleTime. `fetchQuery` returns data and throws on failure (awaitable, handle errors); `prefetchQuery` returns void and swallows errors (best-effort warming). Pick fetchQuery when you need the value/failure.
• fetchQuery returns data + throws; prefetchQuery returns void + swallows errors • both respect staleTime; pick per need ~ "both prefetch" without the distinction

### tanstack-router-loaders-01 · router-loaders, router-typesafety · W3 · medium
**Q:** What is a route `loader`, how does the component read it, and vs `useEffect` fetch?
**A:** The loader runs before the component renders and returns data read via typed `useLoaderData()`. Data is ready at first render — no mount-then-fetch spinner or waterfall like `useEffect`. Loaders also cache/preload.
• loader runs before render; read via typed useLoaderData() • data ready at first render (vs useEffect fetch-after-mount) ~ "loads route data" without the before-render/useEffect contrast

### tanstack-router-loaders-02 · router-loaders, prefetch · W2 · medium
**Q:** Integrate TanStack Query with a loader so both share one cache (no double fetch)?
**A:** Put the `QueryClient` on router `context`, call `context.queryClient.ensureQueryData(queryOptions)` in the loader, and `useSuspenseQuery(queryOptions)` with the same options in the component. One fetch, one cache, data ready; Query owns revalidation.
• loader ensureQueryData; component same queryOptions • shared QueryClient via context → one cache, no double fetch ~ "call the query in the loader" without ensureQueryData/shared-options

### tanstack-router-loaders-03 · router-loaders, prefetch · W2 · medium
**Q:** Router preloads on link hover by default — what happens, which functions run?
**A:** With `defaultPreload: 'intent'`, hovering a `<Link>` runs the target's `beforeLoad` + `loader` early, warming data (and ensureQueryData caches) so navigation is instant. Respects staleTime; guards also run during preload.
• intent preloading runs beforeLoad + loader on hover to warm data • instant nav; respects caching/staleTime (guards run too) ~ "it preloads the page" without naming loader/beforeLoad

### tanstack-router-loaders-04 · router-loaders, router-typesafety · W2 · medium
**Q:** What is `beforeLoad` for, how does it differ from `loader`, and what is `context`?
**A:** `beforeLoad` runs before `loader` (top-down) for auth checks, `throw redirect(...)`, and building/extending the typed `context` (e.g. `{ queryClient, auth }`) threaded to descendants. `loader` fetches data; what beforeLoad returns merges into context.
• beforeLoad runs before loader; auth/guards + throw redirect() • context is typed, threaded down; beforeLoad extends it (loader = data) ~ describes one but not the ordering/merge

### tanstack-router-search-01 · router-search · W3 · hard
**Q:** How do you get typed, validated search params with `validateSearch`, and why validate?
**A:** `validateSearch` (a Zod schema or function) parses the raw query into a typed object — what `useSearch()` returns and what typed navigation requires. Validate because the URL is untrusted input: coerce types, apply defaults/`.catch()`, get end-to-end types.
• validateSearch (e.g. Zod) parses raw query → typed search • URL is untrusted → coerce/default + end-to-end types ~ "define a schema" without the untrusted-input/typing rationale

### tanstack-router-search-02 · router-search · W2 · medium
**Q:** How to store/update a URL-belonging UI filter (e.g. page), and the anti-pattern?
**A:** Search params are the state: read via `useSearch()`, update via typed `navigate({ search: prev => ... })` or `<Link search>`. URL is the single source of truth (shareable/bookmarkable). Anti-pattern: mirroring into `useState` and hand-syncing.
• read via useSearch, update via typed navigate/Link (URL = source of truth) • don't mirror into useState + hand-sync ~ "put it in the URL" without the read/update mechanism

### tanstack-router-search-03 · router-search, router-loaders · W2 · medium
**Q:** A loader must fetch by a search param — why doesn't it see search automatically, and the fix?
**A:** Loaders track path params by default, not search, so they won't reload on search change. Declare `loaderDeps: ({ search }) => ({ page: search.page })`; deps become the loader's `deps` and part of its cache identity, so it receives them and reloads.
• search isn't a loader dep by default (only path params tracked) • add loaderDeps → loader gets them and reloads on change ~ "read search in the loader" without loaderDeps

### tanstack-router-search-04 · router-search, router-typesafety · W2 · medium
**Q:** With `validateSearch` in place, what type-safety do you get when linking to the route?
**A:** The validated shape joins the route's types, so `<Link to>`/`navigate()` require a schema-matching `search` (missing/mis-typed = compile error) and `useSearch()` is fully typed. Changing a field breaks every nav/read site.
• Link/navigate require schema-matching search (type-checked) • useSearch fully typed; schema change breaks call sites ~ "it's typed" without naming enforcement

### tanstack-router-typesafety-01 · router-typesafety · W2 · medium
**Q:** How does file-based routing produce end-to-end type safety, and what is `routeTree.gen.ts`?
**A:** Route files (each `createFileRoute('/path')`) compile into a generated `routeTree.gen.ts` registering every path/params/search type. The router is typed from that tree, so `<Link>`, `navigate`, params, and search are checked against real routes — a path typo is a compile error.
• route files (createFileRoute) generate routeTree.gen.ts with paths/params/search • typed tree makes Link/navigate/params compile-checked ~ "files become routes" without the generated-tree payoff

### tanstack-router-typesafety-02 · router-typesafety · W2 · medium
**Q:** What does `<Link to="/posts/$postId" params={{ postId: '5' }} />` check at compile time?
**A:** That `/posts/$postId` is a real route and that you pass exactly its declared params (`postId`) with the right types — omitting or adding one is a type error. Same for `navigate` and required `search`, so path/param refactors surface at every call site.
• real route + required params ($postId) enforced by type • same for navigate/search; refactors surface at call sites ~ "links are typed" without the params-required/refactor detail
