# cat-jam-free — WITH-SKILL closed-book answers for tanstack (opencode-go, 2026-10-14)
# Delivery: derived/tanstack.cat-jam-free.SKILL.md body (front matter stripped) prepended to _prompts/tanstack.md; one
# `ocode run -m opencode-go/cat-jam-free -effort med` session, isolated HOME with every tool denied, cwd holding only the sheet;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: tanstack-query-keys-01
  answer: |
    A query key is usually a serializable array (often starting with a string scope) that identifies a cached query. Two keys refer to the same cache entry when their deterministic hashes match: array order matters, but object key order does not because object keys are sorted before hashing, so `['todos', { a: 1, b: 2 }]` and `['todos', { b: 2, a: 1 }]` hash to the same entry.

- id: tanstack-query-keys-02
  answer: |
    Hierarchical keys create natural groups, so partial invalidation works by prefix matching: invalidating `['todos']` also affects descendants such as `['todos', todoId]` or `['todos', 'list', filters]`. This lets code invalidate a broad family of related queries without enumerating every exact key.

- id: tanstack-query-keys-03
  answer: |
    The cache key does not include `userId`, so different users' data can be served from the same cache entry and the UI can show the wrong user's data. The rule is that every variable, parameter, or dependency the `queryFn` reads must be included in the query key.

- id: tanstack-query-keys-04
  answer: |
    Only one network request fires. Both observers use the same key, so they share one in-flight request/promise (request deduplication), one cached result, one `staleTime` freshness clock, and one `gcTime` timer. Cache, freshness, and garbage collection are shared across all observers of that key.

- id: tanstack-caching-01
  answer: |
    `staleTime` controls how long data is considered fresh and therefore whether mounts, window focus, or reconnects trigger background refetches. `gcTime` (v4 `cacheTime`) controls how long an inactive query's cached data remains in memory after its last observer unmounts. They are independent: one governs refetching, the other retention. Data can be stale but still cached, and fresh data is never garbage-collected while observed. Defaults are `staleTime: 0` and `gcTime: 5 minutes`.

- id: tanstack-caching-02
  answer: |
    With the default `staleTime: 0`, data is always stale. On remount, TanStack Query immediately returns the cached data if still retained, while refetching in the background, so the user sees cached content right away and then an update when the refetch completes. If the cache entry was already garbage-collected, it fetches normally.

- id: tanstack-invalidation-01
  answer: |
    `invalidateQueries({ queryKey: ['todos'] })` marks matching queries (prefix match by default) as stale and refetches the active matching queries in the background. Inactive matching queries are marked stale and refetched later when they become active. It does not remove their cached data.

- id: tanstack-invalidation-02
  answer: |
    Use `setQueryData` when you already know the exact new cached value, such as data returned by a successful mutation, and want an immediate cache update without waiting for a refetch. The tradeoff is that it can drift from server truth and requires maintaining the cache shape correctly, while invalidation guarantees server consistency at the cost of an extra fetch.

- id: tanstack-mutations-01
  answer: |
    `useMutation` is for write operations, not automatically managed reads. You trigger it with `mutate(variables)` or `mutateAsync(variables)`. In v5, track it with `isPending`, `isError`, `isSuccess`, and `isIdle` (not `isLoading`); `mutateAsync` returns a promise that resolves with the result or rejects on failure.

- id: tanstack-mutations-02
  answer: |
    A mutation changes server state, so cached read queries for affected data may now be stale. Calling `invalidateQueries` in `onSuccess` marks those queries stale and refetches active ones, keeping the UI consistent with the server.

- id: tanstack-mutations-03
  answer: |
    In `onMutate`, cancel outgoing refetches for affected keys, snapshot previous cached data, and set optimistic data with `setQueryData`, returning the snapshot as context. In `onError`, use that context to roll back the cache to the snapshot. In `onSettled`, invalidate the affected queries so the client eventually syncs with the server regardless of success or failure.

- id: tanstack-mutations-04
  answer: |
    `mutate` does not return a promise and is commonly used with callbacks; `mutateAsync` returns a promise and is useful with async/await. A mutation knows it failed only when its `mutationFn` throws or returns a rejected Promise; returning an error object or a resolved response with a bad status is treated as success.

- id: tanstack-query-fn-01
  answer: |
    `fetch` resolves for HTTP error responses such as 500, so TanStack Query sees a successful resolved value rather than an error. The fix is to check `response.ok` (or another success condition) in the `queryFn` and throw an Error when the response is bad, optionally parsing the error message first.

- id: tanstack-query-fn-02
  answer: |
    A `queryFn` may be invoked multiple times, reused, cached, or run outside direct component lifecycle, so causing side effects such as `setState` can create stale or unexpected state. Instead, keep the `queryFn` focused on fetching and transforming data; react to query results through component state derived from the returned query status and data.

- id: tanstack-query-fn-03
  answer: |
    Use `enabled: !!userId` in `useQuery` options so the query only runs once `userId` is known. Until then it stays pending with fetchStatus idle, does not fire a request, and reports no data/error.

- id: tanstack-suspense-01
  answer: |
    `useSuspenseQuery` suspends the component while data is loading and returns data as defined rather than exposing loading flags. The component must be wrapped in `<Suspense fallback=...>` for loading; a good pattern also wraps it with an `<ErrorBoundary>` for failures.

- id: tanstack-suspense-02
  answer: |
    Fetch errors thrown during render propagate to the nearest `<ErrorBoundary>`. Handle them there with an error fallback, and ensure the `queryFn` throws on bad responses or the boundary will never fire. Error boundaries can also provide reset/refetch behavior.

- id: tanstack-suspense-03
  answer: |
    Each sibling can suspend independently, causing a sequential waterfall where later queries start only after earlier suspense resolves. Avoid this by fetching independent data in parallel with `useSuspenseQueries`, or by prefetching/ensuring the data in a router loader before rendering.

- id: tanstack-suspense-04
  answer: |
    No. `useSuspenseQuery` is always enabled and is not suitable for dependent/conditional queries. For queries that must wait for another value, use `useQuery` with `enabled: false`/a condition instead.

- id: tanstack-prefetch-01
  answer: |
    Use `ensureQueryData` in a loader when the data must be available before rendering. It returns cached data if available and fresh, otherwise fetches and returns the data. `prefetchQuery` warms the cache and returns `Promise<void>`; it is better for opportunistic performance and does not provide the data directly.

- id: tanstack-prefetch-02
  answer: |
    `prefetchQuery` returns `void` and does not propagate fetch errors prominently, so it is appropriate for warming cache. `fetchQuery` returns the data and throws on error, so use it when the caller needs the result or when errors must propagate, such as in loader error handling.

- id: tanstack-router-loaders-01
  answer: |
    A route `loader` runs during the route transition before the component renders, usually to fetch or ensure data. The component reads the result with `useLoaderData()`. Unlike a `useEffect` fetch, the loader completes before rendering the route, avoiding a post-render loading flash and data waterfall.

- id: tanstack-router-loaders-02
  answer: |
    Put the shared `QueryClient` in router context and call `ensureQueryData`/`prefetchQuery` in the loader with the same query key/options. The component then calls `useQuery` with matching keys/options, so both use the same cache and no duplicate fetch occurs.

- id: tanstack-router-loaders-03
  answer: |
    Default preloading on link hover/focus starts loading the target route before navigation, including running `beforeLoad` and `loader` as needed, without rendering the route component. This makes navigation feel faster because data and code are already available.

- id: tanstack-router-loaders-04
  answer: |
    `beforeLoad` runs before `loader` and is used for auth checks, redirects, and enriching context. `loader` fetches route data. Router `context` is the typed dependency-injection object passed to routes, such as auth state and a `QueryClient`.

- id: tanstack-router-search-01
  answer: |
    Define `validateSearch` on the route using a validator such as a Zod schema or function. TanStack Router parses and validates URL search params into a typed object. This coerces values, applies defaults, rejects invalid input, and gives type safety to `useSearch`, `Link`, and navigation.

- id: tanstack-router-search-02
  answer: |
    Store the filter in URL search params and update it through `useNavigate` or `<Link search=...>` so the URL remains the source of truth. The anti-pattern is keeping it only in component state, which causes desync, loses shareable/bookmarkable state, and resets on navigation.

- id: tanstack-router-search-03
  answer: |
    The loader does not automatically receive search params; its inputs are mainly params and context. Add `loaderDeps` to derive and pass the needed search values, for example `loaderDeps: ({ search }) => ({ page: search.page })`, and read them as `deps` in the loader. This also ensures the loader reruns when those dependencies change.

- id: tanstack-router-search-04
  answer: |
    With validation in place, `Link`, `navigate`, `useSearch`, and related calls are compile-checked against the route's validated search type. Invalid, missing required, or mistyped search values become type errors rather than runtime 404s or runtime validation failures.

- id: tanstack-router-typesafety-01
  answer: |
    File-based routing derives routes from files, each exporting `createFileRoute('/path')`. The Router plugin/CLI generates `routeTree.gen.ts`, registering every path with its params and validated search types. The router is typed from that generated tree, and the generated file should not be edited manually.

- id: tanstack-router-typesafety-02
  answer: |
    It checks at compile time that the path exists, that required params such as `postId` are supplied with the expected type, and that any `search` values match the route's validated search schema. Missing or wrong params, unknown paths, or invalid search objects are type errors.
