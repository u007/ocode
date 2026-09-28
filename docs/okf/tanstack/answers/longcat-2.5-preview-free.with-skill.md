  - id: tanstack-query-keys-01
    answer: |
      A query key in TanStack Query is a serializable array that uniquely identifies a query's data in the cache. It acts as the cache entry's identity. The client decides that two keys refer to the same cache entry by serializing them into a stable hash. The serialization sorts plain-object keys before stringifying (so `{ a: 1, b: 2 }` and `{ b: 2, a: 1 }` produce the same hash), while preserving array order (so `[1, 2]` and `[2, 1]` are different entries). This means object property order never creates a new cache entry, but array element order does. A key maps to one Query object in the cache, and all observers of the same key share that query's state, freshness, and garbage-collection timers.

  - id: tanstack-query-keys-02
    answer: |
      Query keys should be structured hierarchically (e.g. `['todos']` for a list, `['todos', 'detail', id]` for a single item) so that they mirror the shape of your data. This enables two levels of invalidation. A broad prefix like `['todos']` invalidates every query that starts with that prefix, which is useful when you know the whole list or subtree is stale (e.g. after a mutation). A precise key like `['todos', 'detail', id]` targets exactly one item for invalidation or data-setting without affecting siblings. The hierarchy also helps developers reason about what data will be affected by an invalidation call.

  - id: tanstack-query-keys-03
    answer: |
      If a `queryFn` reads a `userId` variable but the key is just `['projects']`, the cache will return stale or wrong data. When `userId` changes from 5 to 6, the key stays `['projects']`, so TanStack Query sees the same cache entry and returns the old user's projects (or does not refetch). The rule is: any variable that affects what the query fetches must be part of the query key. The correct key would be `['projects', userId]`. The query key is the cache's identity — if a dependency changes, the key must change too, so a new entry is created and fetched.

  - id: tanstack-query-keys-04
    answer: |
      Only one network request fires. When two components mount `useQuery` with the same key `['user', 5]` at the same time, TanStack Query deduplicates the in-flight request. The first call creates a Query entry and starts fetching; the second call sees the existing entry and subscribes to it, so both components share the same single network request. This is a core benefit of the shared cache — the cache state is per key, not per component, so N observers of one key share one in-flight request.

  - id: tanstack-caching-01
    answer: |
      `staleTime` (default 0) controls how long a query is considered fresh. While a query is fresh, TanStack Query will NOT refetch it — even if the component remounts or a new observer subscribes — and it serves cached data immediately. `gcTime` (formerly `cacheTime`, default 5 minutes) controls how long a query's data is kept in the cache after the last observer unmounts (i.e. after it becomes inactive/unused). Once `gcTime` elapses with no observers, the data is garbage collected from memory. The key distinction: `staleTime` is about freshness and whether refetches happen; `gcTime` is about memory management and how long unused data lingers.

  - id: tanstack-caching-02
    answer: |
      With the default `staleTime: 0`, a query is considered stale immediately after it becomes inactive (or immediately, period). When a component remounts and reads that query, the data is served from cache instantly (no loading flash), but TanStack Query will also trigger a background refetch because the data is already stale. The user-visible behavior is: the old data appears instantly, then the UI updates when the fresh data arrives. This can cause a brief flash of outdated content followed by a content update. If the network is slow, the user sees stale data for a duration before the fresh response replaces it.

  - id: tanstack-invalidation-01
    answer: |
      Calling `queryClient.invalidateQueries({ queryKey: ['todos'] })` marks every query whose key matches the prefix `['todos']` as stale. For any matching queries that are currently active (have at least one observer mounted), TanStack Query will trigger a background refetch of their `queryFn`. For matching queries that are inactive (no observers), they are simply marked stale; no fetch fires now, but the next time a component subscribes, the query will be refetched. Invalidation does not immediately remove data from the cache — it just marks it stale and schedules refetches for active queries.

  - id: tanstack-invalidation-02
    answer: |
      Use `setQueryData` when you already have the new data (e.g. from the mutation response) and can update the cache synchronously without a refetch. The benefit is instant UI updates — no loading state, no network wait. The tradeoff is that the cache is now ahead of the server; if the mutation response doesn't reflect the full server state (e.g. computed fields, side effects, or other clients' changes), the cache may be inconsistent with the server until the next refetch or invalidation. `invalidateQueries` is safer but causes a refetch and a loading state. `setQueryData` is an optimistic-or-immediate cache write that skips the refetch.

  - id: tanstack-mutations-01
    answer: |
      `useQuery` is for fetching (read) operations — it manages caching, deduping, retries, and status tracking automatically. `useMutation` is for create/update/delete (write) operations — it does not cache or dedupe, and it gives you imperative control over when the mutation runs. You trigger a mutation by calling the `mutate` (or `mutateAsync`) function returned by `useMutation`. You track it via the mutation object's `status` field, which in TanStack Query v5 is one of: `'idle'` (not yet run), `'pending'` (in flight), `'success'` (resolved), or `'error'` (rejected). In v5, the boolean flags like `isLoading`, `isSuccess`, `isError` are also available, but the canonical status values are the string literals.

  - id: tanstack-mutations-02
    answer: |
      After a mutation succeeds, calling `queryClient.invalidateQueries` in `onSuccess` marks the affected cache entries as stale and triggers background refetches for any active observers. This is common because a mutation on the server typically changes data that one or more queries depend on, but the mutation response often doesn't return the full updated entities in the exact shape the queries expect. Invalidating ensures the client refetches and reconciles with the server's authoritative state. It keeps the cache consistent without manually reconstructing query data from the mutation response.

  - id: tanstack-mutations-03
    answer: |
      A correct optimistic update with rollback uses three callbacks. `onMutate` runs before the mutation function; its job is to cancel any in-flight queries for the affected key, snapshot the current cache value (for rollback), and then use `setQueryData` to write the optimistic new value to the cache immediately. `onError` runs if the mutation rejects; its job is to restore the snapshot taken in `onMutate` via `setQueryData`, effectively rolling back the optimistic change. `onSettled` runs after the mutation resolves or rejects (in both cases); its job is typically to invalidate the affected queries so the client reconciles with the server, and to clean up any optimistic state. The flow is: onMutate (snapshot + optimistic write) → mutation runs → onError (rollback) or onSuccess (optional) → onSettled (invalidate).

  - id: tanstack-mutations-04
    answer: |
      `mutate` is fire-and-forget: you call it with variables, it kicks off the mutation, and it returns `void`. You react to the outcome via the callbacks (`onSuccess`, `onError`, `onSettled`) or by watching the mutation's `status`. `mutateAsync` is the promise-based variant: it returns a Promise that resolves on success or rejects on error, so you can `await` it and handle the result inline. A mutation knows it failed when its `queryFn` (the mutation function) throws an error or returns a rejected Promise. This causes the mutation's `status` to become `'error'` (or `isError: true`), the `error` field is populated, and the `onError` callback fires. With `mutateAsync`, the returned Promise rejects with that error.

  - id: tanstack-query-fn-01
    answer: |
      `fetch` only rejects on network failure, not on HTTP error status codes like 500 or 404. A 500 response resolves successfully from `fetch`'s perspective, so the `queryFn` does not throw, TanStack Query sees a successful resolution, and the error never surfaces. The fix is to check the response's `ok` status or status code inside the `queryFn` and throw manually. The standard pattern is: `const res = await fetch(url); if (!res.ok) throw new Error('Request failed'); return res.json();`. Alternatively, you can throw an `Error` with more detail, or use a custom `queryFn` wrapper that handles HTTP errors uniformly.

  - id: tanstack-query-fn-02
    answer: |
      Calling `setState` or causing side effects inside a `queryFn` is a mistake because TanStack Query may call the `queryFn` multiple times (retries, refetches, deduplication re-runs). Side effects would fire multiple times, causing bugs like duplicate toasts, duplicate API calls, or inconsistent state. The `queryFn` should be a pure function that fetches data and returns it (or throws on error). State updates and side effects belong in the component, in `onSuccess`/`onError` callbacks of a mutation, or in a `useEffect` that reacts to query state changes — never inside the `queryFn` itself.

  - id: tanstack-query-fn-03
    answer: |
      You express a dependent query with the `enabled` option: `useQuery({ queryKey: ['projects', userId], queryFn: () => fetchProjects(userId), enabled: !!userId })`. When `userId` is falsy (null, undefined, etc.), `enabled` is false, so the query does not run. The query is in a `'pending'` state with `fetchStatus: 'idle'` (in v5, it shows as `status: 'pending'` and `fetchStatus: 'idle'`). Once `userId` becomes truthy, `enabled` flips to true, the query starts fetching, `fetchStatus` becomes `'fetching'`, and on success the query transitions to `'success'`. This is the recommended pattern for dependent queries in v5 — do not use `useSuspenseQuery` conditionally for this; use `enabled`.

  - id: tanstack-suspense-01
    answer: |
      `useSuspenseQuery` changes the rendering behavior: when the query is loading, it throws a Promise (suspends) instead of returning a `{ isLoading: true }` state. The component does not render at all until the data is available — the nearest React `<Suspense>` boundary catches the thrown Promise and renders its `fallback`. The component must be wrapped in a `<Suspense boundary>` (with a `fallback` prop), otherwise the promise will bubble up and the app will crash. In contrast, `useQuery` returns state objects and lets you handle loading/error in the component body. `useSuspenseQuery` also handles errors via React Error Boundaries rather than returning an `error` field.

  - id: tanstack-suspense-02
    answer: |
      With `useSuspenseQuery`, fetch errors surface as thrown errors (like render errors) rather than as a returned `error` property. You handle them with React Error Boundaries — wrap the component (or a section of the tree) in an `<ErrorBoundary>` that catches the error and renders a fallback UI (e.g. an error message with a retry button). The Error Boundary's `fallback` or `fallbackRender` prop receives the error. This is conceptually different from `useQuery`, where errors are returned in the render result. You can also use `useErrorBoundary` option on the hook to opt in/out of error propagation, but by default errors are thrown to the nearest Error Boundary.

  - id: tanstack-suspense-03
    answer: |
      Two sibling components each calling `useSuspenseQuery` for independent data create a request waterfall because both components suspend simultaneously. React renders sibling Suspense boundaries one at a time (or the parent Suspense boundary waits for all). If wrapped in a single parent `<Suspense>`, the parent boundary waits for all child promises, so the fallback shows until BOTH are done — the effective wait is the sum of both latencies. Even with separate boundaries, React may not start fetching the second until the first resolves, depending on the tree. To avoid a waterfall, you should either prefetch both queries in a parent loader (so data is already cached before render), or use a single query that fetches both resources, or use `Promise.all` in a `queryFn` so one request fires instead of two sequential ones.

  - id: tanstack-suspense-04
    answer: |
      No, you cannot make `useSuspenseQuery` conditional with `enabled: false` to build a dependent query. `useSuspenseQuery` does not support the `enabled` option because hooks must be called unconditionally (React's rules of hooks), and the entire point of the suspense variant is to always suspend until data is ready — conditionally disabling it would break the suspense contract. For a dependent query (where you don't have the key until some prerequisite is known), use the regular `useQuery` with `enabled: !!prerequisite`. The `useQuery` hook supports `enabled` and will keep the query in a pending/idle state until `enabled` becomes true.

  - id: tanstack-prefetch-01
    answer: |
      In a router loader you use `ensureQueryData` when you want the data guaranteed available before the component renders. `ensureQueryData` checks the cache first: if the data exists and is fresh (within `staleTime`), it returns it immediately without fetching; if not, it fetches and caches. It returns a Promise that resolves when data is ready, so you can `await` it in the loader. `prefetchQuery` is different: it always fires the `queryFn` (unless already in-flight) but does not return the data and does not wait for it — it's fire-and-forget, useful for warming the cache on hover or navigation intent. In a loader, `ensureQueryData` is the right choice because the loader needs the data resolved before rendering proceeds.

  - id: tanstack-prefetch-02
    answer: |
      `prefetchQuery` is fire-and-forget: it triggers the query function to warm the cache but does not return a Promise you can await and does not throw errors to the caller. If the fetch fails, the error is swallowed (or stored on the query state for later observers). `fetchQuery` is awaitable: it returns a Promise that resolves with the data or rejects with the error, and it throws on failure. The error-handling behavior matters when you need to handle failures: with `prefetchQuery` you cannot catch errors in the caller (you must rely on the query's error state or a global handler), while with `fetchQuery` you can try/catch. Use `prefetchQuery` when you don't need the result immediately and don't care about failures at the call site; use `fetchQuery` when you need the data or need to handle the error.

  - id: tanstack-router-loaders-01
    answer: |
      A route `loader` in TanStack Router is a function defined on a route that runs before the component renders, typically to fetch data the route needs. It receives a context object (with params, search, context, etc.) and returns data. The component reads the result via the typed `useLoaderData()` hook — the loader's return type flows into `useLoaderData` with no manual annotation. This differs from a `useEffect` fetch in several ways: the loader runs on the server during SSR, runs before navigation commits (so the component doesn't render until data is ready, no loading state), can be preloaded on link hover, and integrates with the router's error boundaries and caching. A `useEffect` fetch runs after the component mounts, causes a loading flash, and doesn't benefit from router-level caching or preloading.

  - id: tanstack-router-loaders-02
    answer: |
      To integrate TanStack Query with a Router loader sharing one cache: put the single shared `QueryClient` on the router context (via `createRootRouteWithContext`). In the loader, call `await context.queryClient.ensureQueryData(opts)` with the exact same `queryOptions` (same `queryKey` and `queryFn`) that the component will use. In the component, call `useQuery(opts)` or `useSuspenseQuery(opts)` with those identical options. Because both the loader and the component use the same client and the same key, there is one cache entry — the loader fetches (or reads from cache), the component reads the same entry, and no double fetch occurs. The loader's `ensureQueryData` populates the cache before the component renders.

  - id: tanstack-router-loaders-03
    answer: |
      TanStack Router preloads routes on link hover (or on viewport enter for some configurations) to make navigation instant. During a preload, the router runs the route's `beforeLoad` function and then its `loader` function, fetching and caching the data before the user clicks. The component does not render yet. If the user clicks, the data is already available (respecting `staleTime`), so navigation feels instant. The functions that run during preload are: `beforeLoad` (if defined), then `loader` (if defined). The component render is deferred until actual navigation. Preloading also respects the loader cache and `staleTime`, so fresh data is not refetched on every hover.

  - id: tanstack-router-loaders-04
    answer: |
      `beforeLoad` runs before the loader and the component. It is used for guards (e.g. auth checks), redirects, and for computing values that the loader or component needs. It runs on every navigation to the route, and its return value is merged into the router `context` for descendant routes — that is how you inject services, auth state, or computed values. `loader` runs after `beforeLoad` and is responsible for fetching the data the component will read via `useLoaderData`. Router `context` is a typed dependency object created at `createRouter` (via `createRootRouteWithContext`) and threaded down the entire route tree to every child's `beforeLoad`, `loader`, and component. It's a dependency-injection mechanism for cross-cutting concerns like the QueryClient, auth service, etc.

  - id: tanstack-router-search-01
    answer: |
      You get typed, validated search params by defining `validateSearch` on the route. `validateSearch` takes the raw `URLSearchParams`-derived object (all strings) and returns a validated, typed object — typically using a Zod schema with defaults and `.catch()` for resilience. The validated type flows to `Route.useSearch()`, `navigate({ search })`, and `<Link search>`. You validate because the URL is untrusted, user-editable input — users can type anything in the address bar. Validation coerces strings to the right types (e.g. `"2"` → `2` for a number), recovers from garbage or missing values (via defaults or `.catch()`), and prevents malformed params from reaching your loader or component. Without validation, you'd manually parse and guard every search param at every site.

  - id: tanstack-router-search-02
    answer: |
      A UI filter that belongs in the URL (e.g. a page number) should be stored as a search param and updated with typed `navigate({ search: prev => ({ ...prev, page: 2 }) })` or `<Link search={{ page: 2 }}>`. Read it with `Route.useSearch()`. The URL is the single source of truth — it's shareable, back-button-friendly, and survives refresh. The anti-pattern is mirroring the URL param into `useState` and hand-syncing between the two (e.g. `useEffect` that pushes state to the URL). This creates two sources of truth, leads to sync bugs, and loses the benefits of URL-driven state (shareability, browser history). Another anti-pattern is using `useSearch` without `validateSearch`, leaving the params untyped and unvalidated.

  - id: tanstack-router-search-03
    answer: |
      A loader does not see search params automatically because, by default, TanStack Router keys loaders on path params only — the loader re-runs only when the route's path params change. Search params are not part of the loader's dependency tracking by default. To make a search param (like `page`) a loader dependency, you declare `loaderDeps: ({ search }) => ({ page: search.page })` on the route. The loader then receives those values in its `deps` argument and re-runs when they change. This is separate from `validateSearch`, which types the search object but does not make the loader reactive to it. Without `loaderDeps`, changing the URL search param will not trigger a loader refetch.

  - id: tanstack-router-search-04
    answer: |
      Once `validateSearch` is in place, you get end-to-end type safety for search params across the app. `Route.useSearch()` returns the validated TypeScript type (not a string map), so reading params in components is typed and autocomplete works. `navigate({ search: ... })` and `<Link search={...}>` are checked against the schema — you cannot pass a wrong type or unknown field without a compile error. Renaming or removing a field in the schema produces compile errors at every read and navigation site simultaneously. This is the same compile-time safety that `params` get, extended to the search object.

  - id: tanstack-router-typesafety-01
    answer: |
      File-based routing in TanStack Router means each route is defined in a file (e.g. `routes/posts/$postId.tsx`), and the file system structure maps to the URL structure. At build time (or dev time), TanStack Router scans these files and generates `routeTree.gen.ts`, a fully typed representation of the route tree. This generated file encodes every route's path, params, search schema, and parent-child relationships as TypeScript types. Because the types are derived from the same files that define the routes, they're always in sync. `routeTree.gen.ts` is the source of truth for all type checking: `Link`, `navigate`, `useParams`, `useSearch`, `useLoaderData`, and `beforeLoad` are all typed against it.

  - id: tanstack-router-typesafety-02
    answer: |
      `<Link to="/posts/$postId" params={{ postId: '5' }} />` is checked at compile time against the generated route tree. The compiler verifies that: (1) the path `/posts/$postId` exists in the route tree, (2) the `$postId` param is declared and its type matches — here `'5'` is a string and if `$postId` is typed as `string`, it passes; if it were typed as `number`, this would be a compile error, and (3) any required search params for that route are also provided with correct types. If you rename the route path, the param, or the param's type, every `Link`, `navigate`, `useParams`, and `useLoaderData` that references it becomes a compile error. You get this for free from the file-based route definitions — no manual type annotations needed.
