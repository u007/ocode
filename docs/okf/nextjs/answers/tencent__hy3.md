# nextjs knowledge benchmark — answers
# Model: novita-ai/tencent/hy3   (provider-stripped id: tencent/hy3)
# CLOSED-BOOK via ocode run -dir <empty> (no corpus access). corpus_rev 1.

- id: nextjs-app-router-conventions-01
  answer: |
    In the App Router, a folder defines a URL segment by its name (e.g. `app/blog/settings` maps to `/blog/settings`). Special files inside a folder add behavior to that segment:
    - `page.tsx`: the UI unique to a route. A folder is only publicly accessible if it has a `page` (or `route`).
    - `layout.tsx`: shared UI that wraps the segment and its children; it persists across navigations within that subtree and does not re-mount.
    - `loading.tsx`: a Suspense fallback for the segment, shown while the page/segment loads (mechanically wraps the segment in a `<Suspense>` boundary).
    - `error.tsx`: a React error boundary for the segment; must be a Client Component and catches render errors in its subtree.
    - `route.ts`: a Route Handler (API endpoint) that handles HTTP requests and returns a `Response`; it has no UI and makes the segment an endpoint rather than a page.

- id: nextjs-app-router-layout-02
  answer: |
    Nested layouts compose: a request to a deep route renders the root layout, then each intermediate layout, then the page. On client-side navigation, only the layouts whose segments actually change are re-rendered; shared (ancestor) layouts are preserved and do NOT re-mount, so they keep their state (e.g., scroll position, local component state) and their child stays mounted.
    `template.tsx` differs in that it creates a NEW instance of its subtree on every navigation to that segment. It re-mounts and re-runs effects/initialization each time, so it does not preserve state the way a `layout.tsx` does. Use a template when you want effects to re-run or state to reset on each navigation.

- id: nextjs-app-router-error-03
  answer: |
    An `error.tsx` MUST be a Client Component (it starts with `"use client"`) and MUST export a default React component that receives `error` and `reset` props.
    It catches errors thrown during rendering in its own segment and below (including in Server and Client Components of that subtree), and provides a `reset()` to retry.
    It does NOT catch: errors in the layout of the same segment (the wrapping `layout.tsx` is above the boundary, so to catch layout errors you must place an `error.tsx` in the parent segment), errors thrown in the `error.tsx` itself, and errors in event handlers (those are not caught by error boundaries).

- id: nextjs-app-router-loading-04
  answer: |
    Adding `loading.tsx` to a route segment automatically wraps the segment's content (the page and its children) in a React `<Suspense>` boundary. Mechanically, Next generates a fallback UI from `loading.tsx` and uses it to stream a partial response: when you navigate to the segment, the surrounding layout/shell renders immediately while the (potentially slow) page/segment data is being prepared, and the fallback is swapped for the real content once it is ready. It is the same mechanism as a manual `<Suspense>` boundary but applied to the whole segment.

- id: nextjs-server-components-default-01
  answer: |
    In the App Router, components are Server Components by default — you do not need any directive to make them server components. They render on the server (and can be prerendered at build) and ship minimal/no JS to the client.
    `"use client"` marks the module as the boundary to a Client Component: everything in that file (and the component tree it imports) is then rendered and hydrated on the client and may use hooks, state, effects, browser APIs, and event handlers. It is a boundary, not a per-file flag for "this one component only."

- id: nextjs-server-components-hooks-02
  answer: |
    You cannot call `useState`/`useEffect`, attach `onClick`, or access `window` in a Server Component because server components are rendered only on the server (at request or build time) with no browser runtime and no DOM/window — there is no client-side hydration for them, so React hooks, event handlers, and browser-only globals are unavailable.
    The fix is to mark the component (or the part that needs interactivity) as a Client Component by adding `"use client"` at the top of the file, then it can use hooks, handlers, and browser APIs.

- id: nextjs-server-components-props-03
  answer: |
    A Server Component can pass props into a Client Component only if those props are serializable across the server→client boundary: primitives, plain objects, arrays, `Date`, `undefined`/`null`, and — as a special case — Server Actions / functions created with `"use server"`. You cannot pass class instances, functions (non-action), or other non-serializable values.
    The idiomatic way to keep a server-rendered subtree inside a client component is the "children as a prop" / composition pattern: the Client Component accepts `children`, and the Server Component renders `<ClientComp><ServerRenderedStuff/></ClientComp>`. The children are rendered on the server and passed down as already-rendered React nodes, which is serializable.

- id: nextjs-data-fetching-rsc-01
  answer: |
    In the App Router you fetch data directly inside Server Components (and route handlers) — they can be `async` functions, so you `await fetch(url)` or call a database/ORM directly in the component body. There is no special data-fetching export.
    `getServerSideProps` and `getStaticProps` are Pages Router APIs and have no equivalent in the App Router; they are replaced by simply fetching inside the server component. For client-side data fetching you use Route Handlers (or third-party libs like SWR/React Query) and call them from client components.

- id: nextjs-data-fetching-nogssp-02
  answer: |
    To avoid a request waterfall, issue the independent fetches in parallel rather than awaiting them sequentially. In a Server Component you do this by kicking off all fetches and awaiting them together with `await Promise.all([fetchA(), fetchB(), fetchC()])`, or by starting each `fetch` (which returns a promise) at the same scope before awaiting. Because each fetch is memoized per request, parallel issuing removes the sequential dependency and they run concurrently.

- id: nextjs-caching-fetch-default-01
  answer: |
    Version-specific:
    - Next.js 13/14: `await fetch(url)` in a Server Component is cached by default (equivalent to `cache: 'force-cache'`). It is stored in the Data Cache and reused across requests.
    - Next.js 15: the default changed — `fetch` is NOT cached by default (it behaves like `no-store`), so you must opt in explicitly via `cache: 'force-cache'` or `{ next: { revalidate: <seconds> } }` to get caching.

- id: nextjs-caching-layers-02
  answer: |
    The App Router has several caching layers:
    1. Request Memoization (React's `cache()` / fetch dedup) — deduplicates the same `fetch`/data call within a single render pass / request so it's only executed once.
    2. Data Cache — persists the results of `fetch` (and `unstable_cache`) across requests and even deployments; supports `revalidate` and `tags` for invalidation.
    3. Full Route Cache — caches the rendered HTML (and RSC payload) of statically-rendered route segments at build time.
    4. Router Cache (client-side) — an in-memory cache in the browser of the RSC payload for visited route segments, used for instant client navigation.

- id: nextjs-caching-revalidate-03
  answer: |
    `revalidate` (set on a `fetch` via `{ next: { revalidate: N } }` or as `export const revalidate` on a segment) marks cached data as fresh for N seconds; after that it is "stale" and the next request triggers a background refresh while still serving the stale copy (stale-while-revalidate / ISR), so subsequent requests then get fresh data.
    Time-based revalidation (ISR) differs from fully static content in that static content is generated exactly once at build and never changes until the next build, whereas ISR content auto-regenerates after the interval without a rebuild. ISR still serves cached content in between; static never re-fetches.

- id: nextjs-caching-ondemand-04
  answer: |
    After a mutation you purge the cache on demand with `revalidatePath(path)` and `revalidateTag(tag)` (both from `next/cache`). `revalidateTag` invalidates every cached `fetch`/data entry tagged with that string (tags are set via `fetch(url, { next: { tags: [...] } })`); `revalidatePath` invalidates a whole route's Full Route Cache and its data cache.
    You call them inside a Server Action or a Route Handler (server-side only), after the write succeeds, so the next render reflects the new data immediately instead of waiting for time-based revalidation.

- id: nextjs-caching-segment-config-05
  answer: |
    `export const dynamic` controls the rendering strategy of a route segment: `'auto'` (default), `'force-dynamic'` (always render at request time, never statically cached), `'force-static'`, or `'error'` (fail if dynamic APIs are used). `export const revalidate` sets the default ISR/time-based revalidation interval (in seconds) for that segment's data and route cache.
    You set `dynamic = 'force-dynamic'` when a route must be rendered per-request — e.g., it reads cookies/headers/searchParams or you otherwise want to opt out of static generation and always run on the server at request time.

- id: nextjs-rendering-static-dynamic-01
  answer: |
    Next.js decides static vs dynamic rendering by what a route does. A route is statically rendered (prerendered at build) when it uses no dynamic APIs and its data fetching is cacheable/static. It is dynamically rendered (per request) when it reads dynamic functions such as `cookies()`, `headers()`, `searchParams`, or `connection()`, when it uses `fetch` with `no-store`/`cache: 'no-store'`, or when its segment config forces dynamic. In Next.js 15, with `fetch` uncached by default, many routes become dynamic unless you opt into caching/static behavior.

- id: nextjs-rendering-static-params-02
  answer: |
    `generateStaticParams` is an async export from a dynamic-segment page (e.g. `app/blog/[slug]/page.tsx`) that returns an array of parameter objects (`[{ slug: 'a' }, { slug: 'b' }]`). Next uses these to prerender those specific paths at build time (and can also render others on-demand at first request). The Pages Router equivalent is `getStaticPaths`, which likewise returns the list of paths to prerender for a dynamic page.

- id: nextjs-rendering-dynamic-apis-03
  answer: |
    In Next.js 15 these APIs became asynchronous: `cookies()`, `headers()`, and the `params`/`searchParams` props now return Promises, so they must be `await`ed. This was done to enable better static analysis and to let Next know whether a route depends on request-time data.
    Reading any of them in a page makes the route opt into dynamic rendering — the page can no longer be statically prerendered and is rendered at request time — because the values are only known per request.

- id: nextjs-server-actions-useserver-01
  answer: |
    `"use server"` marks a function (or all exports of a file) as a Server Action: an async server-side function that can be called from client components and from forms, and is used for mutations. Unlike `"use client"`, which marks a boundary as running in the browser, `"use server"` marks code that runs on the server but is invokable from the client. It can appear at the top of a file (making every export an action) or inline at the top of an individual async function.

- id: nextjs-server-actions-mutation-02
  answer: |
    Define an async function with `"use server"` (in a separate `actions.ts` or inline). Pass it to a `<form action={myAction}>` (this gives progressive enhancement — the form works without JS) or call it from an event handler. The action receives `FormData` (or typed args), performs the write (DB/API), then calls `revalidatePath`/`revalidateTag` (or `redirect`) to update the UI. React provides pending state automatically: `useFormStatus` (in a child of the form) exposes `pending`, and `useTransition` can wrap programmatic calls. Because the action is server-side, the page revalidates and the UI reflects new data without manual client state management.

- id: nextjs-server-actions-security-03
  answer: |
    The trap: a Server Action looks like a local function call but Next.js actually exposes it as a public, unauthenticated POST endpoint (it has a generated ID/URL), so anyone on the internet can invoke it directly. You must treat every action as a public API:
    - Authenticate and authorize inside the action (check the user session / permissions) — never assume it's only called from your UI.
    - Validate and sanitize all incoming input (FormData/args).
    - Guard against CSRF where relevant (Next ships same-origin checks, but you still should not rely on obscurity).
    - Don't trust client-supplied IDs to identify the user/resource.

- id: nextjs-route-handlers-basics-01
  answer: |
    A Route Handler is a `route.ts` file (e.g. `app/api/hello/route.ts`) that exports functions named after HTTP methods (`GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `HEAD`, `OPTIONS`), each receiving a `Request` and returning a `Response`. It is the App Router replacement for the Pages Router's `pages/api/*` API routes — instead of `req`/`res`, you use the Web `Request`/`Response` APIs and return JSON or other responses.

- id: nextjs-route-handlers-caching-02
  answer: |
    Version-specific:
    - Next.js 14: a `GET` Route Handler is cached by default (treated like a static route) unless it reads dynamic APIs like `cookies()`/`headers()`.
    - Next.js 15: a `GET` Route Handler is NOT cached by default; you must opt in explicitly.
    To opt into caching, set `export const dynamic = 'force-static'` (or `revalidate`/`fetch` caching) in the route file; to opt out, set `export const dynamic = 'force-dynamic'` or read `cookies()`/`headers()`.

- id: nextjs-route-handlers-methods-03
  answer: |
    In a Route Handler:
    - Request body: `const data = await request.json()` (or `await request.text()`, `await request.formData()`) from the `Request` argument.
    - Query params: read from `request.nextUrl.searchParams` (e.g. `request.nextUrl.searchParams.get('q')`) or `new URL(request.url).searchParams`.
    - Dynamic route params: from the second handler argument — `export async function GET(req, { params })`, where `params` is an object like `{ id }` for a `app/users/[id]/route.ts` segment (in Next.js 15 `params` is a Promise and must be `await`ed).

- id: nextjs-streaming-ssr-01
  answer: |
    Streaming SSR is the ability for the server to send the rendered HTML to the browser in chunks as each part becomes ready, instead of blocking until the entire page is rendered and then sending one big response. In the App Router this is powered by React Suspense + the RSC streaming protocol.
    It buys you a much faster first paint / TTFB and better perceived performance: the page shell (layout, fast sections) appears immediately, and slower data-dependent sections stream in and pop into place, rather than the user staring at a blank page until everything is ready.

- id: nextjs-streaming-suspense-02
  answer: |
    Wrap just the slow data section in its own `<Suspense fallback={<Skeleton/>}>` boundary. The fast parts of the page render and stream immediately, while the slow section shows its fallback and then fills in when its data resolves — so the page stays fast and interactive instead of blocking on the one slow fetch. (This is the same mechanism `loading.tsx` uses, but applied granularly to a single component rather than the whole segment.)

- id: nextjs-streaming-boundary-03
  answer: |
    `loading.tsx` auto-creates a Suspense boundary around the ENTIRE route segment — it's an all-or-nothing, file-based loading state for that segment, shown on navigation to it. Your own `<Suspense>` boundary is granular and explicit: you choose exactly which component(s) are wrapped, provide a localized fallback for just that part, and the rest of the page renders normally without a full-page loading state. Use `loading.tsx` for segment-level loading UX, and manual `<Suspense>` when you want to isolate one slow piece within an otherwise-fast page.

- id: nextjs-metadata-static-01
  answer: |
    Export a `metadata` object (or `generateMetadata` function) from a `layout.tsx` or `page.tsx`. Next.js reads it and injects the corresponding `<title>`, `<meta description>`, and other tags into the document `<head>` automatically. Example: `export const metadata = { title: 'Home', description: '...' }`. You can also set `metadataBase`, `openGraph`, `twitter`, `viewport`, `robots`, etc., all declaratively without manually writing `<head>` tags.

- id: nextjs-metadata-dynamic-02
  answer: |
    When the metadata depends on data, export an async `generateMetadata` function from the page (or layout): `export async function generateMetadata({ params, searchParams }) { ... }`. Inside it you `await` the same data (e.g. fetch the blog post) and return a metadata object with the dynamic `title`/`description`/etc. Next.js awaits it before rendering and injects the tags. (You can share/await the same fetch because of request memoization, so it isn't duplicated.)

- id: nextjs-metadata-inherit-03
  answer: |
    Metadata merges down the tree: a page's `metadata` is layered on top of (and overrides) the `metadata` of its parent layouts. Child values replace parent values for the same keys. `title.template` is set on a parent layout to define a format like `%s | MySite`; when a child sets a plain `title`, it is substituted into the template (e.g. child `title: 'Blog'` + template `'%s | MySite'` => `Blog | MySite`). This gives consistent, automatically-suffixed titles without repeating the site name everywhere.

- id: nextjs-metadata-files-04
  answer: |
    Beyond the `metadata` export, Next.js supports file-based metadata conventions placed in the `app` directory (or segment), which it auto-detects and turns into the right `<link>`/`<meta>` tags:
    - Icons: `favicon.ico`, `icon.png` / `icon.svg`, `apple-icon.png`.
    - Social images: `opengraph-image.png`, `twitter-image.png` (can also be dynamic `.ts`/`.js` generators).
    - `sitemap.ts` / `sitemap.xml` and `robots.ts` / `robots.txt` for SEO crawlers.
    - `manifest.ts` / `manifest.json` for web app manifests.
    These special filenames are picked up automatically without an explicit `metadata` export.

- id: nextjs-navigation-link-01
  answer: |
    Use the `<Link>` component from `next/link` to navigate between routes (e.g. `<Link href="/about">About</Link>`). Unlike a plain `<a>`, `<Link>` does client-side navigation: it intercepts the click, fetches only the changed route segment's RSC payload, and updates the page without a full browser reload — preserving the React tree/layout state and giving instant, SPA-like transitions. `<Link>` also prefetches the destination by default (in production) for even faster navigation. A plain `<a>` always triggers a full page reload to the server.

- id: nextjs-navigation-hooks-02
  answer: |
    The App Router navigation hooks (`useRouter`, `usePathname`, `useSearchParams`, `useParams`, `usePathname`, etc.) come from the `next/navigation` module (NOT `next/router`, which is for the Pages Router).
    They fail/fail to work in a Server Component because they rely on client-side browser context (the router state, current URL, etc.) and are Client Component hooks. Server Components render on the server without that runtime, so you must use these hooks only inside components marked `"use client"`.

- id: nextjs-navigation-redirect-03
  answer: |
    `redirect()` (from `next/navigation`) navigates to a different route by throwing a special `NEXT_REDIRECT` error that Next catches to perform the redirect; it can be called in Server Components, Server Actions, and Route Handlers. `notFound()` throws a `NEXT_NOT_FOUND` error to render the nearest `not-found.tsx` UI.
    Gotcha: both work by THROWING, so if you call them inside a `try/catch` block, the catch will swallow the special error and the redirect/not-found will silently fail. Also, `redirect()` must be called before any response is returned, and in a Server Action it should not be wrapped in a try/catch that catches the redirect error.

- id: nextjs-navigation-action-redirect-04
  answer: |
    After a successful Server Action (e.g. creating a record), call `redirect('/new-path')` from `next/navigation` at the end of the action, after the write and any `revalidatePath`/`revalidateTag`. This sends the user to the new page (often the created resource's URL built from the returned id/slug).
    Watch out: `redirect()` throws a special error, so do NOT wrap it in a `try/catch` that would swallow it; also ensure the action returns/throws the redirect rather than returning a value that the client treats as success data. In Next.js 15 remember `params`/dynamic APIs are async if you build the target URL from them. And only call `redirect` in a server context (Server Action / Route Handler / Server Component), not in a Client Component directly.
