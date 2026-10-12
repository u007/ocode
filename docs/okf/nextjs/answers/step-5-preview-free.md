# step-5-preview-free — closed-book answers for nextjs (opencode-go, 2026-10-12); answerer saw only _prompts/nextjs.md
# cleanup: the model answered 14 questions TWICE (a second, differently-worded answer appended after the list); the second copies were dropped, first answer kept: nextjs-route-handlers-basics-01 nextjs-route-handlers-caching-02 nextjs-route-handlers-methods-03 nextjs-streaming-ssr-01 nextjs-streaming-suspense-02 nextjs-streaming-boundary-03 nextjs-metadata-static-01 nextjs-metadata-dynamic-02 nextjs-metadata-inherit-03 nextjs-metadata-files-04 nextjs-navigation-link-01 nextjs-navigation-hooks-02 nextjs-navigation-redirect-03 nextjs-navigation-action-redirect-04
- id: nextjs-app-router-conventions-01
  answer: |
    - layout.tsx: shared UI shell for a route segment and all of its children. It receives `page` (and nested route UIs) as the `children` prop and does NOT re-render on navigation inside its subtree (state is preserved). `app/layout.tsx` is the root layout and is the only one required — it must render `<html>` and `<body>`.
    - page.tsx: makes the segment publicly routable and renders the UI for that path. Every leaf of a route tree needs a page; it is what you see at that URL.
    - loading.tsx: defines the `<Suspense fallback>` UI for that segment (wraps the page + children).
    - error.tsx: React error boundary for the segment; must be a client component and renders instead of the segment UI when a render error is thrown (see the error question).
    - route.ts: a non-UI Route Handler (API route) for the segment. It exports HTTP method functions (GET/POST/...) instead of a component and can't coexist with page.tsx in the same folder.
    - (template.tsx also exists — like a layout but remounts per navigation.)
    - Folder-to-URL mapping: the `app` folder is a rootless prefix. Each folder inside it is a route segment, and the folder nesting order maps directly to the URL path. `app/blog/[slug]/page.tsx` serves `/blog/:slug`. `[param]` and `[...slug]` (catch-all) and `[[...slug]]` (optional catch-all) are dynamic segments; `(groupName)` route groups organize files without adding a path segment; parallel routes `@slot` and intercepted routes `(.)` are advanced exceptions to the 1:1 mapping.

- id: nextjs-app-router-layout-02
  answer: |
    Nested layouts compose from the root down: each layout wraps the layouts and pages below it inside its `children` prop. On navigation between routes that share a layout, React reuses (preserves) the already-mounted layout and its state — only the segments that changed are re-rendered. Layouts never re-mount, so the same layout instance (and its `useState`/DOM state) survives navigations inside it.
    `template.tsx` is like `layout.tsx` but a new instance is created for each navigation into the segment: on every child navigation the component remounts — state is lost and effects re-run. Use template only when you deliberately need per-navigation behavior (e.g., enter/exit animations, logging on every route change, resetting `useState` counters). Everything else that "wraps a segment" should be a layout.

- id: nextjs-app-router-error-03
  answer: |
    Requirements: `error.tsx` must be a Client Component — you include `"use client"` at the top — and must default-export a React component. That component receives `{ error, reset }`, where `error` is the next-formatted error instance and `reset` is a callback that re-renders the boundary to try recovery.
    It catches: errors thrown during rendering of the segment's `page.tsx` and its nested children — including errors thrown inside Server Components that render within that subtree (the server error is passed to the client component). It also catches errors thrown during a render triggered by client-side navigation into that segment.
    It does NOT catch: errors thrown in the segment's own `layout.tsx` or `template.tsx` (the boundary sits below the layout so the layout stays mounted around the error UI — those errors bubble to the parent segment's error boundary); errors thrown in the root `layout.tsx` (for those you need `app/global-error.tsx`, which must render its own `<html>`/`<body>`); errors thrown outside React rendering — event handlers, async callbacks outside render, or errors thrown in `route.ts` handlers (handle those in the handler and return a Response). Also note: nothing else in `error.tsx` is used (no other named exports) and `not-found.tsx` is a separate mechanism (it is not matched by error.tsx unless you throw inside the segment).

- id: nextjs-app-router-loading-04
  answer: |
    Mechanically, adding `loading.tsx` to a segment makes Next.js implicitly wrap that segment's `page` and its nested `children` inside a React `<Suspense boundary={...} fallback={<Loading />}>`. The default-exported component in loading.tsx becomes the fallback.
    Consequences: (1) the segment's RSC/HTML stream isn't delayed — the fallback is emitted in the initial HTML shell and the real segment content is streamed in later; (2) because the loading shell (fallback) is small and static, it is part of the prefetched RSC payload, so on `<Link>` navigations the fallback paints immediately and gives instant navigation feedback; (3) the boundary is segment-scoped: a `loading.tsx` covers the whole page + descendants of that folder. The boundary disappears when the content resolves (fallback is replaced, not stored). If loading.tsx and a page's own inner `<Suspense>` both exist, all boundaries stream independently.

- id: nextjs-server-components-default-01
  answer: |
    In the App Router, everything in the `app` directory is a Server Component by default — layouts, pages, and components are rendered only on the server and produce no client JS. You opt a component (and everything it transitively imports) into the client by putting `"use client"` at the top of the file.
    `"use client"` is a module/serialization boundary marker, not a label meaning "browser-only". It declares: (a) this module and its imports are part of the client bundle; (b) those components may use `useState`/`useEffect`/event props/browser APIs; (c) any component from the server rendered below that boundary is passed via props — React serializes props and renders the client boundary with its children; (d) the components are also prerendered on the server to produce initial HTML (they run during SSR too) — `"use client"` doesn't skip server rendering.close
    Practical consequences: a Server Component can import and render a Client Component (serializable props only), but a Client Component cannot import a Server Component as a module import — you pass server-rendered content as `children` instead. Keep `"use client"` as far down the tree (leaf components) as possible.

- id: nextjs-server-components-hooks-02
  answer: |
    A Server Component is executed on the server, produces the RSC payload (a serializable tree description), and is never mounted in a real React runtime in the browser. Hooks like `useState`/`useEffect` require a browser environment and a component instance that can re-render, subscribe to events, and run effects — there is no event loop, no DOM, no interactivity lifecycle during server rendering. `onClick`/`onChange` props are function values that can't be serialized into the payload for a server component. `window`/`document` don't exist during server rendering, so touching them crashes (workaround: read them in an effect after hydration, or guard with `typeof window !== "undefined"`).
    The fix is boundary splitting: extract the interactive part into its own file with `"use client"` and render it from the Server Component, passing serializable data as props. E.g., keep `app/page.tsx` (server) doing the data fetch, and make `<LikeButton onClick={...}>` a client leaf. Alternatively, pass a whole server-rendered subtree as `children` to a client wrapper so layout/interactivity lives client-side while the content stays server-rendered.

- id: nextjs-server-components-props-03
  answer: |
    Props passed from a Server Component into a Client Component must be serializable (React/RSC-serializable): primitives, plain arrays/objects, strings, booleans, null/undefined, Date, BigInt, typed arrays, and (Next-specific) values a Server Component already owns and can serialize. Functions and class instances are generally NOT serializable — functions are the notable exception in Next: when a Server Component passes a function to a Client Component, it is turned into a reference to a Server Action, so that is allowed; other function props are not. Also avoid passing `Symbol`s and non-serializable ORM entities.
    Idiomatic way to keep a server-rendered subtree inside a client component: composition — pass the server-rendered JSX as the `children` prop (a "slot"). The server renders the subtree itself, and the client component that receives `children` just renders it, so only the small interactive wrapper runs on the client. Example: `<ClientProvider>{serverRenderedContent}</ClientProvider>` rather than importing a server component INTO a client module (which would silently become client-side). The same trick applies to `Modal`/`Dialog`-style components where a client wrapper opens around server-rendered content.

- id: nextjs-data-fetching-rsc-01
  answer: |
    In the App Router you fetch inside the components that need the data — typically in async Server Components: `export default async function Page() { const res = await fetch(...); const data = await res.json(); }`. You may also call your ORM/DB, `fetch`, or a typed client directly (RSC runs on the server). Use `unstable_cache`/`use cache` to memoize non-fetch data, and `Promise.all`/`Promise.allSettled` for parallel requests.
    `getServerSideProps` and `getStaticProps` no longer exist. Replacements: data-fetch-with-fetch-cache-options plus route segment config. For static-generation-per-request semantics use `generateStaticParams` in place of `getStaticPaths` with fallback; for per-request dynamic SSR use `fetch(..., { cache: "no-store" })`, dynamic APIs (`cookies()`/`headers()`), or `export const dynamic = "force-dynamic"`. For ISR use `fetch(url, { next: { revalidate: n } })` or `export const revalidate = n`. Client-side data can still use TanStack Query/useEffect/SWR, and mutations go through Server Actions or Route Handlers.

- id: nextjs-data-fetching-nogssp-02
  answer: |
    A waterfall happens when you `await` one request before starting the next — the second request only begins after the first resolves. The fix is to start all the requests first, then await them together:
      const [user, posts, comments] = await Promise.all([
        fetchA(), fetchB(), fetchC(),
      ]);
    Equivalently, kick off promises before awaiting (you don't have to inline: `const p = fetch(...)`; then `await p`). Use `Promise.allSettled` if you want partial success instead of failing all. Note that in the App Router identical fetch calls within one render pass are also de-duplicated (request memoization), so calling the same fetch in several child components doesn't cause duplicate requests — but that's a different problem from the sequential waterfall.
    Also watch for accidental waterfalls: a parent awaiting data before rendering a child that fetches (hoist the child's fetch or parallelize), and awaiting transformed data sequentially when a single query/join could do it. If the sources must begin as early as possible, fire them in the parent (or a shared module) and pass the promises down; `use()` unwraps a passed promise in a client child if you want streaming.

- id: nextjs-caching-fetch-default-01
  answer: |
    It depends on the major version, and this is a classic trap:
    - Next.js 13.x through 14.x: yes — `fetch` was cached by default (equivalent to `cache: "force-cache"`), stored in the persistent Data Cache and shared across requests/deployments, with no automatic invalidation.
    - Next.js 15 and later (16 included): no — the default is now `cache: "no-store"` (uncached; each render fetches fresh), aligning with `dynamic` defaults. To cache in 15+: `fetch(url, { cache: "force-cache" })`, or ISR-style `fetch(url, { next: { revalidate: 3600, tags: ["posts"] } })`.
    Within one render pass, even uncached duplicate fetches are still de-duplicated by request memoization in all versions.

- id: nextjs-caching-layers-02
  answer: |
    There are four documented caching layers in the App Router:
    1. Request Memoization — dedupes identical `fetch` calls (same input/options) within a single React render pass, so repeated calls don't hit the network. Lifetime: one request. Not configurable; opt out with `AbortController` signal or POST requests.
    2. Data Cache — the persistent, server-side cache of `fetch` responses, keyed by URL + options, surviving across requests and deployments (`force-cache` / `next.revalidate` / `next.tags`). Invalidated by `revalidatePath`/`revalidateTag`/deploy. This is what ISR uses.
    3. Full Route Cache — the server-side cache of the rendered HTML + RSC payload for statically rendered routes (including routes with `generateStaticParams`), persisted to the server and serving as the prefetch target. Invalidated by deploy or by data-cache invalidation.
    4. Client Router Cache — an in-memory, per-session client cache of RSC payloads for visited/prefetched routes; it makes back/forward and `<Link>` transitions instant. In Next 15 the automatic invalidation semantics changed (staleTime 0 for dynamic pages, and `dynamic`/`revalidate` revalidation on navigation), so stale data after a mutation is handled by `router.refresh()` or by returning to a link refresh.
    Additionally the _browser HTTP disk cache_ sometimes mentioned is the "full route cache on the client" edge — the four above are the ones that matter.

- id: nextjs-caching-revalidate-03
  answer: |
    `revalidate` is a cache-lifetime control. At the fetch level, `fetch(url, { next: { revalidate: 60 } })` stores the response in the Data Cache and declares it fresh for 60 seconds. After that it's stale: the next request still serves the cached (stale) response immediately while Next revalidates in the background — stale-while-revalidate. A second request after the fresh response lands serves the new data. There is also a route segment config `export const revalidate = 60` which sets the default revalidation period for the segment (its data and full route cache).
    ISR vs fully static: with a full-route/static cache, the HTML is generated once at build (or on first request for non-`generateStaticParams` routes) and never regenerated until a new deploy — revalidate is effectively "never", and content changes require an on-demand `revalidatePath`/`revalidateTag` or redeploy. ISR additionally gives the route a lifetime: fresh pages serve the prebuilt shell, stale pages trigger background regeneration, so content updates roll out over time without a deploy, and users never wait on regeneration. At build time, only the `generateStaticParams` paths (or first-hit routes) are emitted; others render on demand and then enter the cache.

- id: nextjs-caching-ondemand-04
  answer: |
    `revalidatePath` and `revalidateTag` (from `next/cache`) purge cached data on demand so a subsequent request re-fetches/regenerates.
    - `revalidatePath(path, type?)` — invalidates the Full Route Cache (and the RSC payload prefetched for the client router) for the route path, e.g. `revalidatePath("/blog/[slug]", "page")` or `revalidatePath("/blog")` with `type: "page" | "layout"` (Next 14+). Use it when you know the URL that changed but not which cached fetches contributed.
    - `revalidateTag(tag)` — invalidates every Data Cache entry that was fetched with `{ next: { tags: ["..."] } }`, e.g. `revalidateTag("posts")`. Use it when the same data appears on many routes and you don't want to enumerate paths.
    Where to call them: inside a mutation handler — a Server Action or a Route Handler — after the write completes, never during render or while fetching in a component. Calling during the render/paint phase is a render-time side effect that Next treats as an error context. Both are fire-and-forget: the next request sees fresh data.

- id: nextjs-caching-segment-config-05
  answer: |
    - `export const dynamic` controls the rendering/caching strategy of a route segment. Values: `"auto"` (default — static unless Dynamic APIs like `cookies()`/`headers()`/uncached fetches force dynamic), `"force-dynamic"` (always render per request; disables the Full Route Cache for the segment, streaming HTML), `"error"` (force static rendering; if something dynamic is used, throw a build error — a safety net for pages that should be static), and `"force-static"` (force static rendering and coerce dynamic APIs to empty/default values, useful inside Suspense for partial rendering).
      Set `force-dynamic` when the route must be rendered per request: it reads per-user data (cookies/headers/auth), shows real-time data, needs fresh uncached queries, or should never be served from cache/prefetched. Remember that reading `cookies()`, `headers()`, `draftMode()`, or a `no-store` fetch already implies dynamic rendering — explicit `force-dynamic` mainly matters when you want to force it without those signals, or to disable caching of a segment whose fetches are otherwise cached.
    - `export const revalidate = 3600 | false` sets the default revalidation period (seconds) for the segment's Data Cache and Full Route Cache. It applies globally to fetches in that segment that don't set their own `revalidate` (and `false` means the default of "no time-based revalidation"). ISR for the whole segment is expressed here; per-fetch overrides take precedence.
    Related segment configs: `dynamicParams` (serve non-`generateStaticParams` params on demand vs 404) and `fetchCache`, `runtime = "edge" | "nodejs"`.

- id: nextjs-rendering-static-dynamic-01
  answer: |
    Next decides automatically at build/request time based on whether the route tree uses dynamic inputs:
    - Statically rendered (prerendered at build / first request, cacheable) when it doesn't use any Dynamic APIs and its fetches are cacheable. Static inputs are: cached `fetch` (`force-cache`/`next.revalidate`), params provided via `generateStaticParams`, static modules.
    - Dynamically rendered (per request, streaming) when the tree touches a Dynamic API: `cookies()`, `headers()`, `draftMode()`, `connection()` (awaited), a fetch with `cache: "no-store"`, an uncached slow fetch, `export const dynamic = "force-dynamic"`, a dynamic route whose params weren't generated at build time (or `dynamicParams` opted out), or on-demand revalidation behavior.
    Automatic detection uses module usage: if a dynamic API appears anywhere in the tree, the nearest appropriate segment becomes dynamic (it doesn't have to be the whole page). With Suspense boundaries, Next can prerender the static shell + fallbacks and stream the dynamic parts — partial rendering. You can observe/override intent with the segment configs (`dynamic`, `revalidate`) or `export const dynamic = "error"` to fail a build that unexpectedly becomes dynamic. Uncached user-specific DB/ORM queries are not visible to static analysis — that's why RSC asks you to either use cached fetch/`unstable_cache` or signal dynamism.

- id: nextjs-rendering-static-params-02
  answer: |
    For a dynamic route like `app/blog/[slug]/page.tsx`, `export async function generateStaticParams()` returns a list of the segment params you want prerendered, e.g. `[{ slug: "hello-world" }, { slug: "ssg-101" }]`. At build time Next renders and caches the HTML + RSC payload for each returned value, so those pages are fully static like `getStaticProps` pages. Params not returned are handled by `export const dynamicParams`: if `true` (default), they're rendered on demand and then cached; if `false`, they 404. Contradicting the name slightly: `generateStaticParams` is also what enables prerendering of the segment — without it, the whole route renders per request.
    Pages Router equivalent: `getStaticPaths` returning `{ paths: [{ params: { slug } }], fallback: true | false | "blocking" }` — App Router merges the two concerns, and `fallback` maps roughly to `dynamicParams` (on-demand rendering vs 404). The RSC payload is also prerendered, so navigation to those routes is instant via `<Link>` prefetch.

- id: nextjs-rendering-dynamic-apis-03
  answer: |
    In Next.js 15+, `cookies()`, `headers()`, `draftMode()` and the page/layout `params`/`searchParams` objects are async — you must await them: `const c = await cookies()`, `const { slug } = await params`, `const q = await searchParams`. (In 14 they were synchronous objects; sync access in 15 is deprecated with a warning and removed later.)
    Why the change: Next moved rendering to an explicitly async model so the render environment prepares before any request-specific value is read, making dynamic-read sites explicit, enabling better partial prerendering/streaming, and avoiding the "sync getter that secretly forces dynamic rendering" problem. It also lets these belong to the same Request-scoped API as `draftMode()`.
    Effect of reading them in a page: reading `cookies()`/`headers()`/`draftMode()`/`connection()` (and `searchParams`) opts the route into dynamic rendering — the segment can't be statically cached and is rendered per request. `params` by itself is different: for routes with `generateStaticParams`, awaiting `params` is cheap and doesn't force dynamic rendering (it selects which statically generated route output you render); it only matters at build/validation time.

- id: nextjs-server-actions-useserver-01
  answer: |
    `"use server"` marks Server Actions: either a whole module (top of the file — every exported async function is an action) or a single function (first line inside the function body — inline actions). An action always executes on the server; the client receives a reference (ID) it can call over HTTP POST, and Next handles serializing arguments and the return value for you (so it feels like a function call).
    Differences from `"use client"`:
    - `"use client"` changes the runtime environment: that module and its imports are bundled for and rendered in the browser (they still get SSR'd for initial HTML). It's a UI/serialization boundary.
    - `"use client"` grants access to hooks/DOM/events; `"use server"` grants access to server-only things (secrets, DB, file system) and hides them from the client bundle.
    - A `"use server"` module exports actions (functions) — not UI/components — and every export becomes a public HTTP endpoint. You can pair them: `"use server"` files are imported by client components (as actions) or by server components (as direct function calls).
    - In a `"use server"` file you can't export constants or non-async functions — only actions (and types).

- id: nextjs-server-actions-mutation-02
  answer: |
    Standard flow with progressive enhancement (works without JS):
    1. Define the action in a `"use server"` file/function, e.g.
       `export async function createPost(formData: FormData) {`
       `  const raw = Object.fromEntries(formData);`
       `  const parsed = createSchema.safeParse(raw); // validate (e.g. zod)`
       `  if (!parsed.success) return { error: "..." };`
       `  const post = await db.insert(parsed.data); // write`
       `  revalidatePath("/posts"); // purge cached data/pages`
       `  redirect(`/posts/${post.id}`); // optional navigation`
       `}`
    2. Wire it to a form: `<form action={createPost}>` — Next serializes the action reference and the form posts FormData to it automatically (no `onSubmit` handler needed).
    3. Read and validate input server-side (never trust client values), then write, then `revalidatePath`/`revalidateTag` so the Full Route Cache and Data Cache entries are purged.
    4. Updating the UI: after the action completes, Next re-renders the affected RSC tree; because the show pages are server components reading the same data, they now render fresh data. `revalidatePath`/`revalidateTag` also refresh the client router cache; if you re-fetch with a client-library, return the new data from the action and setState, or call `router.refresh()`.
    5. For pending/error states: `<form action={formAction}>` with `useFormStatus` (must be in a child component) for `pending`, or `useActionState(action, initialState)` for stateful results and field errors. Server-side validation failures are returned to the client instead of throwing.
    Return values from actions must also be serializable (plain data), and the same function can be called directly from client event handlers (e.g. `onClick={() => createPost(data)}`).

- id: nextjs-server-actions-security-03
  answer: |
    Because a Server Action is compiled into a public HTTP endpoint, "looks local, runs remote" is the trap: any client can invoke the action with arbitrary arguments. Next doesn't make actions private — there is no auth/authorization layer, and the action ID is embedded in the client bundle, so treating it as "an internal function only my form calls" is wrong.
    What you must do inside every action:
    1. Authenticate: verify the session/cookie/token yourself (`cookies()`/`headers()`/your auth lib). There's no automatic "this came from my page" trust — for any action invoked per-user you must check who is calling.
    2. Authorize: check permissions and resource ownership for the specific operation (e.g., that the user owns the requested row) — never assume the ID/form field the client sent is legitimate.
    3. Validate and sanitize ALL inputs: parse the FormData/JSON against a schema (e.g. Zod), enforce types, lengths, ranges, and never spread raw user input into queries. Client-side validation is UX only.
    4. Treat the action like a public API endpoint: no leaking internal data in errors, don't put secrets in closures you send to the client, avoid passing sensitive objects as props, mind that the same endpoint can be replayed (add idempotency/rate limiting if that matters), and consider Origin/Host header checks (Next does some CSRF protections, but not authorization).
    In short: every action must re-verify authn/authz and validate input — it's an RPC endoint, not a private function.

- id: nextjs-route-handlers-basics-01
  answer: |
    A Route Handler is a `route.(ts|js)` file inside a route segment in `app/`. It replaces the Pages Router's `pages/api/*` API routes: instead of exporting a default handler with Node `req`/`res`, you export named functions per HTTP method — `export async function GET(request: Request) {...}` (also POST, PUT, PATCH, DELETE, HEAD, OPTIONS). Each receives the Web standard `Request` (fetch API) and returns a Web `Response` (you can use `Response.json(...)`, `new Response(...)`, or the `NextResponse` helper for cookies/headers/redirects).
    Details: it lives in the segment folder, so `app/api/users/route.ts` serves `/api/users` (app folder nesting maps as usual; note the filesystem path isn't the URL only if using route groups). It cannot coexist with `page.tsx`, `layout.tsx`, `loading.tsx` etc. in the same segment — Route Handlers are the leaf for API routes. Dynamic APIs (`cookies()`, `headers()`, `{ params }`) are available inside handlers, and handlers can call Server Actions/DALs. There is no `config.api.bodyParser` — use `request.json()` / `request.formData()` (for form uploads use `<form>` to post to the action directly).

- id: nextjs-route-handlers-caching-02
  answer: |
    Version-specific answer for a `GET` Route Handler:
    - Next.js 14 and earlier: a GET handler could be statically evaluated and cached by default — if the handler doesn't use Dynamic APIs (`cookies()`, `headers()`, `request`-dependent info) and doesn't `POST`/write, the generated `Response` was cached like static content. Opt out with `export const dynamic = "force-dynamic"`, or opt into timed revalidation with `fetch(url, { next: { revalidate: n } })` or `export const revalidate = n` (the handler as a whole is then treated according to the segment config).
    - Next.js 15+: GET Route Handlers are NOT cached by default (`no-store` semantics), matching the fetch default change. `fetch` calls inside a GET handler don't store into the Data Cache unless you ask.
    To opt into caching on 15+: mark the fetch itself — `fetch(url, { cache: "force-cache", next: { revalidate: 60, tags: ["x"] } })` — or mark the handler segment: `export const dynamic = "force-static"` (with `export const revalidate = 60` for ISR). Non-GET methods (POST etc.) are never cached; returning dynamic responses (random/Date.now()) inside a "static" handler will make it dynamic again.

- id: nextjs-route-handlers-methods-03
  answer: |
    - Body: `await request.json()` for JSON, `await request.formData()` for `FormData` (multipart/urlencoded, use `.get("field")`), `await request.text()`, `await request.arrayBuffer()`/`request.blob()` for binary. (POST bodies are arbitrarily large unless you limit them; that's your job.)
    - Query params: `request.nextUrl.searchParams` (e.g. `request.nextUrl.searchParams.get("q")`) — `nextUrl` is the parsed URL of the request with `pathname`, `searchParams`, `basePath`, etc. `new URL(request.url).searchParams` works too, but `nextUrl` is Next-aware (locale, basePath).
    - Dynamic route params: the second argument to the handler, e.g.
      `export async function GET(request: Request, { params }: { params: Promise<{ id: string }> }) {`
      `  const { id } = await params; // Next 15+: params is a Promise`
      In 14 and earlier `params` was a plain object. Same rule as pages/layouts.
    - Request headers: `request.headers.get("authorization")` (or `await headers()` from `next/headers` for a typed API). Cookies: `request.cookies.get("name")` or `await cookies()`.
    - Context object also carries `params` only (no `request` distinction changes); responses: `Response.json({ ok: true }, { status: 201, headers: { ... } })` or `NextResponse.json`, `NextResponse.redirect(...)`, `NextResponse.rewrite(...)`.

- id: nextjs-streaming-ssr-01
  answer: |
    Streaming SSR means Next doesn't buffer the whole rendered HTML response before sending it. The server sends the shell of the document immediately — head, static layout/flashed content, and `<Suspense>` fallbacks — and then flushes each boundary's real content as its async work finishes, progressively filling the page over a single HTTP response (using React 18 `renderToPipeableStream` under the hood, plus RSC streamed chunks).
    What you get over render-everything-then-send:
    1. Much faster TTFB and first paint — the browser can parse/style/display the shell and skeletons while slow queries run instead of showing a blank page waiting for the slowest join.
    2. Slow sections don't block fast sections (per-boundary streaming), and long requests can't tie up the server from serving other requests while they think.
    3. Hydration starts earlier and partial hydration happens per island/boundary, improving TTI for the interactive parts.
    4. SEO/the initial HTML still contains the fallbacks (real content arrives progressively); data is also replayed into the RSC payload so client components sync up.
    Practically this is what makes `loading.tsx` and `<Suspense>` boundaries works — they are the stream's chunk boundaries.

- id: nextjs-streaming-suspense-02
  answer: |
    Wrap only the slow part in a `<Suspense>` boundary with a fast fallback, and make sure nothing above it awaits the slow data:
    `export default function Page() {`  // page stays fast, does NOT await slow
    `  return (`
    `    <section><FastSection /></section>`
    `    <Suspense fallback={<SlowSkeleton />}>`
    `      <SlowSection />`  // async component that awaits its own fetch
    `    </Suspense>`
    `  );`
    `}`
    Key mechanics: the fast page/segment shell is sent immediately; the fallback is sent in its place; when the slow component resolves, React streams the replacement (with its data embedded) and swaps it in client-side without hiding the rest of the page. If the fast content would also be blocked by an awaited parent, hoist the awaits down into children so the segment can be prerendered with `dynamic = "force-static"`/PPR style. A `loading.tsx` achieves the same for the whole segment if the granularity is acceptable. Never make the page itself `await` the slow request, or watch out that fetching inside a child component (rather than the page) is what enables the boundary to be independent.

- id: nextjs-streaming-boundary-03
  answer: |
    Both ultimately create React Suspense boundaries, but with different granularity and behavior:
    - `loading.tsx` is a file convention: it implicitly wraps the segment's whole `page` and its nested children in one Suspense boundary with your component as the fallback. It's segment-scoped (one fallback for the entire page), and because the loading shell is tiny and static it ships in the prefetched RSC payload — so on `<Link>` navigation the fallback paints instantly, giving immediate navigation feedback. On a full page load the fallback is the first HTML flushed.
    - Your own `<Suspense>` is component-scoped: you choose where the boundary goes, so (a) you can have several boundaries on one page with different fallbacks (e.g. per card/section), (b) static content outside the boundary doesn't get replaced by a fallback, and (c) boundaries can be nested around a single slow leaf so the rest of the page keeps rendering. They don't come for free in the prefetched shell in the same way (the static shell contains your fallbacks — same mechanism — but there's no segment-level convention).
    Rule of thumb: `loading.tsx` for route-level instant feedback on navigation; explicit `<Suspense>` for fine-grained loading of parts inside a page. `loading.tsx` is also applied per segment, so nested segments each get their own.

- id: nextjs-metadata-static-01
  answer: |
    Export a `metadata` object from a Server Component (`page.tsx` or `layout.tsx`) in the App Router:
    `import type { Metadata } from "next";`
    `export const metadata: Metadata = {`
    `  title: "Dashboard",`
    `  description: "Manage your workspace",`
    `  openGraph: { title: "Dashboard", ... },`
    `  twitter: { card: "summary_large_image" },`
    `  robots: { index: false },`
    `};`
    `export default function Page() { return <h1>Dashboard</h1>; }`
    Next merges this into `<head>` (via React 19's metadata hoisting, with title dedupe and ordering rules) for both initial HTML and client navigations; metadata only works in Server Components (client components can't export static `metadata` — pass it via a parent or use `generateMetadata`). For anything dynamic, use `generateMetadata` (see next question). You can also set `<head>` tags directly with React 19 `<title>`/`<meta>`/`<link>` tags rendered in the tree, which Next hoists — but the `metadata`/`generateMetadata` API is the supported way and merges/dedupes properly.

- id: nextjs-metadata-dynamic-02
  answer: |
    Use `generateMetadata` instead of a static `metadata` export:
    `export async function generateMetadata(props: { params: Promise<{ slug: string }> }): Promise<Metadata> {`
    `  const { slug } = await props.params;  // Next 15+: params is a Promise`
    `  const post = await fetch(`https://api/posts/${slug}`).then(r => r.json());`
    `  return {`
    `    title: post.title,`
    `    description: post.excerpt,`
    `    openGraph: { title: post.title, images: [post.cover], type: "article" },`
    `  };`
    `}`
    Notes: it can live in `page.tsx` or `layout.tsx` (layouts typically use it for parent-dependent metadata), receives the same props as the segment (`params`, `searchParams`), is awaited on the server before the page renders, and supports streaming? Not directly — generateMetadata blocks the initial render, so keep its fetches fast or cached (revalidate) to avoid delaying TTFB. It runs for every request to that route, and you can reuse the data with React `cache()`/memoized fetch to avoid a second roundtrip if the page fetches the same thing. `params`/`searchParams` are async in Next 15+ (deprecated sync in 15, removed later). If you don't provide one, static parent metadata applies.

- id: nextjs-metadata-inherit-03
  answer: |
    Metadata composes segment-by-segment from the root layout down to the page: each segment's `metadata`/`generateMetadata` output is merged into the accumulated result, with the closest (deepest) definition winning per key. Plain fields are shallow-merged per key: a child's `title` replaces the parent's `title`; if the child omits `description`, the parent's inherited `description` remains. Object fields like `openGraph`/`twitter` merge field-by-field, so a child can override `openGraph.images` while inheriting `openGraph.siteName`. `<link rel="canonical">`-type fields are replaced, not merged into duplicates (except things like icons, which accumulate).
    `title.template` is declared in a parent (usually the root layout) as `title: { template: "%s | Acme", default: "Acme" }`. Child segments that set `title: "Dashboard"` get `"Dashboard | Acme"` substituted into `%s` automatically — you don't repeat the brand suffix in each page. `%s` is replaced by the nearest child title; a nested `template` on an inner segment overrides the outer template for its subtree. `title.default` is used when a child doesn't define a title (the actual text shown), and `title.absolute` in a child bypasses the parent template entirely (use it for the homepage title). Also note: a segment's `generateMetadata` result merges the same way, and defaults flow downward only if the child doesn't define the key.

- id: nextjs-metadata-files-04
  answer: |
    The Metadata Files API: instead of hand-writing `<link>` tags, you drop specially named files (or `.tsx`/`.ts` generators) into app directory route segments, and Next picks them up and emits the correct tags:
    - `favicon.ico` (root app dir) — the favicon.
    - `icon.(ico|jpg|jpeg|png|svg)` / `icon-dynamic.tsx` — app icons; `apple-icon.png` / `apple-icon.tsx` — Apple touch icon.
    - `opengraph-image.(jpg|png|gif)` / `opengraph-image.tsx` — OG image. The `.tsx` form exports a component that renders with `ImageResponse` from `next/og`, so it's a generated OG image; you can export `size`, `contentType`, or a dynamic function (e.g. `alt`, per-route). Same for `twitter-image.(png|jpg|gif)` / `twitter-image.tsx` and `twitter-card` config.
    - `sitemap.ts` (or static `sitemap.xml`) — default-exports a function returning `MetadataRoute.Sitemap`, an array of `{ url, lastModified?, changeFrequency?, priority? }`; it's generated at build/request and served at `/sitemap.xml`.
    - `robots.ts` — default-exports a function returning `MetadataRoute.Robots` (rules, sitemap URL, host), served at `/robots.txt`.
    - `manifest.ts`/`manifest.json` — web app manifest (`MetadataRoute.Manifest`) with name/icons/theme_color; link tag auto-emitted.
    Files placed in a nested segment apply to that route segment (they can be dynamic by generating from the same params). They coexist with the `metadata` export — Next merges the tags and doesn't duplicate them; you generally shouldn't also write `<link rel="icon">` by hand when using these conventions. Dynamic generators can be async and read the request (with the same dynamic-render caveats as pages).

- id: nextjs-navigation-link-01
  answer: |
    `import Link from "next/link"` and use `<Link href="/about">About</Link>`. Dynamic values: `<Link href={`/posts/${slug}`}>` (or `href={{ pathname: "/posts/[slug]", query: { slug } }}`). For in-code logic, `useRouter().push/replace/prefetch` from `next/navigation`.
    What `<Link>` adds over `<a>`: client-side navigation. Next intercepts clicks and renders the destination route's RSC payload client-side — no full document reload, so there's no request for a new HTML document, no loss of React state, no re-download of JS/CSS, and shared layouts are not re-remounted. It also prefetches the route's payload: by default, on `<Link>` appearing in the viewport (and on hover/focus) Next prefetches the code/data (statically rendered full route cache payloads), so navigation feels instant. The `prefetch` prop controls this (`prefetch={true | false | "auto" | null} | "unstable_forceStale"`), plus `replace`, `scroll` (default `true` to scroll to top on new segments; set `scroll={false}` to preserve), and `onNavigate` (Next 15+) for intercepting. Caveats: plain anchors are still internal-page-full-navigations; external links will full-navigate unless you use `<Link>` with an absolute URL (still fine); `<Link>` also participates in parallel/intercepted routes (modals via `@slot` + `(.)`-intercepting folder) which plain anchors can't do.

- id: nextjs-navigation-hooks-02
  answer: |
    The hooks live in `next/navigation`: `useRouter` (client-side router — `router.push()`, `replace()`, `refresh()`, `prefetch()`, `back()`, `forward()`), `usePathname` (current path `/blog/hello`), `useSearchParams` (a read-only `URLSearchParams` of the query string), `useSelectedLayoutSegment(s)` (which child segment of a layout is active), `useParams` (route params), plus server-only helpers like `redirect`/`notFound`, `useServerInsertedHTML` (removed in App Router → not needed).
    They fail in Server Components because they are React hooks that read client runtime state — the current router location, history stack, and browsing session only exist in the browser after (and during hydration). A Server Component has no hook state, no event loop, and can't re-run when the URL changes; importing `next/navigation` hooks into a server module throws or refuses at build time ("only allowed in Client Components"). Fix: put the component that needs them in a file with `"use client"`. Server-side equivalents: read `params`/`searchParams` as props in the page/layout, and use `redirect()`, `notFound()`, `cookies()`, `headers()` from server contexts. (`useSearchParams` in a client component inside a statically rendered route also needs a `<Suspense>` boundary above it, otherwise the whole page is forced to client-side suspend rendering; note in Next 15, otherwise the route deopts to CSR at build time.)

- id: nextjs-navigation-redirect-03
  answer: |
    - `redirect(url, type?)` (from `next/navigation`): programmatically sends the user elsewhere — usable in Server Components, Route Handlers, and Server Actions. `type` is `"push"` (default) or `"replace"` (`RedirectType`). It issues a temporary HTTP redirect (307 in modern versions) during the initial load, and for client-side/Server Action flows it renders the target route instead (the document navigates client-side).
    - `notFound()`: renders the nearest `not-found.tsx` boundary (with HTTP 404 status) instead of the page — the component-choice version of `404`. Place `app/not-found.tsx` (root), `app/blog/not-found.tsx` (segment-specific). Also useful in Route Handlers by throwing/returning; you can call `notFound()` inside a server component or action after a failed lookup.
    Gotchas:
    - They are implemented by throwing special errors (`NEXT_REDIRECT`, `NEXT_NOT_FOUND`) — so don't call them inside a `try/catch` that swallows/transforms the error (or you must rethrow), and code after them doesn't run.
    - Since they throw, you cannot "await then decide to redirect" inside a catch-all that logs errors — wrap carefully (`catch (e) { ...; throw e }`).
    - In Server Actions, `redirect`/`notFound` must be called in the action, not after it returns; make sure all awaits and writes complete first. Don't wrap the redirect in a try/catch or `Promise.all`.
    - Also, `redirect` during the render of a static page: reading cookies/headers in that component makes the route dynamic, so redirects based on per-request state need dynamic rendering or middleware.
    - They're not the same as `NextResponse.redirect` (used in Route Handlers/middleware, returns a 307 Response to the browser).

- id: nextjs-navigation-action-redirect-04
  answer: |
    Inside the Server Action itself, after the write succeeds: call `redirect("/new-path")` — Next handles the navigation after the action resolves (client-side navigation when invoked from a form, HTTP redirect on a full page POST). Example: create the record, `revalidatePath("/records")` (so the destination shows fresh data), then `redirect("/records")` — or `redirect(`/records/${id}`)`.
    Watch out for:
    - `redirect()` throws internally — never put it inside a `try/catch` that swallows errors (catch/transform logic around DB calls must `throw e` on unknown errors, or place the redirect outside the try); nothing after it runs.
    - Revalidate before redirecting: the destination page (and any list below) will otherwise be served from the stale Full Route Cache.
    - Return values and redirect are mutually exclusive on the same code path — if you need to return validation errors, branch: errors → `return { error }`, success → `redirect()`.
    - Use a proper `<form action={...}>` (or `formAction`) rather than an `onClick` navigation so the action runs with the right FormData and progressive enhancement; don't navigate with `router.push()` before the action is done (that produces a race — the navigation happens before the mutation, or double-submits on refresh).
    - The user-facing state: any pending states (`useFormStatus`/`useTransition`) are reset by the navigation; don't rely on local React state for success feedback after the redirect — show it on the page you redirect to, or use `router.refresh()` when staying put.

