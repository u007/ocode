- id: nextjs-app-router-conventions-01
  answer: |
    In the App Router, folders under `app/` map directly to URL segments — a folder `blog/` becomes `/blog`, `blog/[slug]` becomes `/blog/:slug`, and special files define behavior for that segment:
    - `page.tsx` — the UI rendered when the URL matches; makes the route publicly accessible.
    - `layout.tsx` — shared UI wrapping the page and its children; persists across navigation.
    - `loading.tsx` — a loading UI (React Suspense boundary) shown while the page's data or code loads.
    - `error.tsx` — an error boundary that catches render errors in that segment.
    - `route.ts` — an API endpoint (REST) exposed at the URL path instead of a page.
- id: nextjs-app-router-layout-02
  answer: |
    Nested layouts wrap child segments and persist across navigations — only the inner page re-renders while the outer layout's state is preserved. A `layout.tsx` does NOT remount on navigation, keeping state and avoiding re-fetches. `template.tsx`, by contrast, is like a layout but it remounts on every navigation into the segment — useful for entering/exiting animations or resetting state that must not persist. Use `layout` for stable shell UI; use `template` when you need a fresh instance per navigation.
- id: nextjs-app-router-error-03
  answer: |
    An `error.tsx` file must export a React component (and cannot be a Client Component by default — it must be a Server Component; if you need interactivity like a "retry" button, wrap that in a separate Client Component). It catches errors thrown during rendering of that segment's page/layout at runtime. It does NOT catch errors in the root `layout.js`/`template.js` (use `global-error.tsx` for those) or errors in event handlers/effects (use `error-boundary` patterns or the component itself).
- id: nextjs-app-router-loading-04
  answer: |
    Adding a `loading.tsx` to a route segment automatically wraps the segment's page in a `<Suspense>` boundary. When the segment or its children are loading (e.g. waiting on a Suspense-bound async component or route switch), Next.js immediately renders the loading UI as the fallback. This enables streaming SSR and instant navigation feedback without writing manual Suspense code.
- id: nextjs-server-components-default-01
  answer: |
    In the App Router, components are Server Components by default — they run only on the server, have no client-side JS bundle, and can directly access databases/filesystems. The `"use client"` directive marks a boundary: everything in that file and everything it imports (unless also a child boundary) becomes a Client Component sent to the browser. It marks the entry point into the client module graph, not the whole tree above it.
- id: nextjs-server-components-hooks-02
  answer: |
    Server Components render only on the server — there is no React runtime on the client, so `useState`/`useEffect` hooks, event handlers like `onClick`, and browser APIs like `window` simply don't exist in that environment. To use them, add `"use client"` at the top of a component file (making it a Client Component) and either move the interactive logic there or pass server-rendered content as children/props into the client component.
- id: nextjs-server-components-props-03
  answer: |
    Server Components can pass serializable props (strings, numbers, arrays, plain objects, promises) into Client Components. Non-serializable values like functions or class instances cannot be passed as props. The idiomatic way to keep a server-rendered subtree inside a client component is to pass the server component as `children` (or a prop) — e.g. `<ClientWrapper><ServerList /></ClientWrapper>` — so the server part renders on the server and only the wrapper ships to the client.
- id: nextjs-data-fetching-rsc-01
  answer: |
    In the App Router, you fetch data directly inside async Server Components using `await fetch(...)` or database queries — no special data-fetching functions are needed. This replaces the Pages Router's `getServerSideProps` (SSR) and `getStaticProps` (SSG): the App Router's async components + caching/revalidation handle both cases. Client-side fetching typically uses libraries like SWR or TanStack Query.
- id: nextjs-data-fetching-nogssp-02
  answer: |
    To avoid a request waterfall in a Server Component, fire independent requests concurrently instead of awaiting them sequentially. The idiomatic pattern is to start all promises first without awaiting, then await them together — e.g. `const [a, b] = await Promise.all([fetchA(), fetchB()])` — or use `Promise.all` in the parent before passing data down. This way parallel requests execute simultaneously rather than one-after-another.
- id: nextjs-caching-fetch-default-01
  answer: |
    In Next.js 13 and 14 (Pages/App Router with the default Pages Router behavior), `fetch` in a Server Component was NOT cached by default — each request was unique unless you set `cache: 'force-cache'` or a `next: { revalidate }` option. In Next.js 15, the default changed: `fetch` requests ARE now cached by default (equivalent to `cache: 'force-cache'`), and you must explicitly opt out with `cache: 'no-store'` for dynamic data. So the answer depends on version: 14 and earlier — no cache by default; 15+ — cached by default.
- id: nextjs-caching-layers-02
  answer: |
    The App Router has four main caching layers:
    1. Request Memoization — dedupes identical `fetch` calls within a single render pass (React cache).
    2. Data Cache — persists `fetch` results and Server Action results across requests and deployments, keyed by URL + options + tags.
    3. Full Route Cache — caches the rendered HTML and RSC payload of statically rendered routes at build time.
    4. Router Cache — client-side cache of RSC payloads for previously visited routes, enabling instant back/forward navigation.
- id: nextjs-caching-revalidate-03
  answer: |
    `revalidate` sets a time (in seconds) after which a cached entry is considered stale — the next request triggers a re-render/re-fetch in the background while serving the stale content until the fresh one is ready (ISR). Time-based revalidation (ISR) differs from fully static content because static content is generated once at build time and never changes until the next build, whereas ISR allows the cached page to be regenerated on-demand after the revalidate window expires, keeping content fresh without a full rebuild.
- id: nextjs-caching-ondemand-04
  answer: |
    `revalidatePath(path)` clears the cache for a specific route path, forcing it to re-render on the next request. `revalidateTag(tag)` clears all cache entries tagged with the given string (set via `fetch` options or `next.tags`). You call them inside Server Actions or Route Handlers after a mutation (e.g. after a POST/PUT/DELETE) so the UI reflects the new data immediately. `revalidatePath` is coarse (whole page); `revalidateTag` is granular (specific data sets).
- id: nextjs-caching-segment-config-05
  answer: |
    `export const dynamic` controls whether a route is statically or dynamically rendered: `'force-static'` forces static rendering, `'force-dynamic'` forces per-request rendering. `export const revalidate` sets the ISR revalidation interval (in seconds) for the route's cached data. You'd set `dynamic = 'force-dynamic'` when the page must reflect real-time data on every request (e.g. a dashboard reading `cookies()`/`headers()` for the current user) and caching would serve stale or personalized-wrong content.
- id: nextjs-rendering-static-dynamic-01
  answer: |
    Next.js decides at build time: if a route has no dynamic functions (no `cookies()`, `headers()`, uncached `fetch`, etc.) and its data is static or cacheable, it is pre-rendered statically at build time. If it uses dynamic functions or uncached data, it is rendered on-demand per request (server-side rendering). You can override this per-segment with `export const dynamic = 'force-static' | 'force-dynamic'`. The presence of `generateStaticParams` also makes a dynamic route statically pre-rendered for the listed params.
- id: nextjs-rendering-static-params-02
  answer: |
    `generateStaticParams` exports a list of param values (e.g. `[{ slug: 'a' }, { slug: 'b' }]`) for a dynamic route like `app/blog/[slug]`, causing Next.js to pre-render static pages for each value at build time. The Pages Router equivalent is `getStaticPaths` (with `paths` and `fallback`), which serves the same purpose — defining which paths are pre-rendered and how to handle non-listed paths (`fallback: true` | `false` | `'blocking'`).
- id: nextjs-rendering-dynamic-apis-03
  answer: |
    In Next.js 15+, `cookies()`, `headers()`, `params`, and `searchParams` became asynchronous — they return Promises and must be `await`ed. This is because they now depend on async request context (AsyncLocalStorage) rather than being synchronously available. Reading them (or any dynamic function) in a page opts that route into dynamic rendering — it will be server-rendered on each request rather than statically pre-rendered, since the values are only known at request time.
- id: nextjs-server-actions-useserver-01
  answer: |
    The `"use server"` directive marks a file's exported functions as Server Actions — they run only on the server and are exposed to the client as callable endpoints (referenced by their function identity). Unlike `"use client"` (which marks the client-side boundary and ships the module graph to the browser), `"use server"` marks the server-side boundary: the functions never run on the client; instead, the client invokes them via a generated RPC-like call that sends arguments over the network.
- id: nextjs-server-actions-mutation-02
  answer: |
    1. Define a Server Action with `"use server"` (or in a `actions.ts` file) that performs the mutation (e.g. inserts into a database).
    2. In a Client Component form, pass the action directly to `<form action={myAction}>` or call it in an event handler.
    3. After the mutation succeeds, call `revalidatePath()` / `revalidateTag()` inside the action to refresh cached data.
    4. Optionally call `redirect()` to navigate the user to a new page.
    5. For progressive enhancement, the form works even before JS loads — Next.js intercepts the submission and runs the action.
    6. Use `useTransition` / `useActionState` for pending state and error handling.
- id: nextjs-server-actions-security-03
  answer: |
    A Server Action feels like a local function call, but it's actually an HTTP endpoint exposed to the world — anyone can call it directly, bypassing your UI. The security trap is treating it as a trusted internal function. Inside every action you must: (1) validate authorization (check the user is logged in and allowed), (2) validate and sanitize all inputs (never trust client-supplied data), (3) use parameterized queries to prevent injection, and (4) rate-limit where appropriate. Treat every action like a public API endpoint because it is one.
- id: nextjs-route-handlers-basics-01
  answer: |
    A Route Handler (`route.ts`) defines a REST API endpoint at a URL path in the App Router. You export async functions named after HTTP methods (`GET`, `POST`, `PUT`, `DELETE`, etc.) that receive a `Request` and return a `Response`. This replaces the Pages Router's `pages/api/*` directory, where each file exported a default `(req, res)` handler. Route Handlers are by default edge-compatible and support streaming, dynamic params, and standard Web APIs.
- id: nextjs-route-handlers-caching-02
  answer: |
    In Next.js 14 and earlier, `GET` Route Handlers were NOT cached by default. In Next.js 15, `GET` handlers ARE cached by default (like `fetch`). To opt into caching in versions where it's not default, add `export const dynamic = 'force-static'` or `export const revalidate = <seconds>`. To opt out (always dynamic), set `export const dynamic = 'force-dynamic'`. `POST` handlers are never cached.
- id: nextjs-route-handlers-methods-03
  answer: |
    In a Route Handler: (1) Body — `await req.json()` (or `.text()`, `.formData()`) on the incoming `Request`; (2) Query params — `req.nextUrl.searchParams.get('key')` or `new URL(req.url).searchParams`; (3) Dynamic route params — passed as the second argument's `params` property (e.g. `{ params: { id } }`), and in Next.js 15+ `params` is a Promise so you `await` it.
- id: nextjs-streaming-ssr-01
  answer: |
    Streaming SSR sends the HTML to the client in chunks as the server renders, rather than waiting for the full page to render before sending anything. The shell (layout, static parts) is sent immediately, and dynamic sections stream in as their data resolves. This improves Time to First Byte (TTFB) and Largest Contentful Paint (LCP) — the user sees meaningful content faster, especially on slow data sources, rather than staring at a blank page.
- id: nextjs-streaming-suspense-02
  answer: |
    Wrap the slow section in a `<Suspense fallback={<Loading />}>` boundary inside the page. The rest of the page renders and streams to the client immediately; the slow section's fallback is sent initially and swapped out when its data resolves. This keeps the fast parts interactive without waiting for the slow data — the page is never blocked by the slowest component.
- id: nextjs-streaming-boundary-03
  answer: |
    `loading.tsx` creates an automatic Suspense boundary at the route-segment level that wraps the entire page — the whole segment is suspended and the loading UI shows until everything inside resolves. A custom `<Suspense>` boundary is finer-grained: you place it around a specific component, so the rest of the page streams normally and only that component shows its fallback. Use `loading.tsx` for whole-segment loading states; use `<Suspense>` for partial-page streaming where the shell is already visible.
- id: nextjs-metadata-static-01
  answer: |
    Export a `metadata` object from a `page.tsx` or `layout.tsx`:
    ```ts
    export const metadata: Metadata = {
      title: 'My Page',
      description: 'A description',
    }
    ```
    Next.js injects these into the `<head>`. You can also set `title` as a string or a `template` object with `default` and `app`/`%s` patterns.
- id: nextjs-metadata-dynamic-02
  answer: |
    Export an async `generateMetadata` function that receives `{ params, searchParams }` (both Promises in Next.js 15+), fetches the data, and returns a `Metadata` object. For example, fetch the blog post title/description for `app/blog/[slug]` and return it as the page's metadata. This runs at build time (for static pages) or on-demand (for dynamic pages).
- id: nextjs-metadata-inherit-03
  answer: |
    Metadata composes across nested layouts and pages — values defined in a layout are inherited by child pages unless overridden. Arrays (like `openGraph.images`) merge; scalars (like `description`) are overridden by the child. `title.template` lets child pages inject their title into a pattern set by a parent layout, e.g. `{ default: 'My Site', template: '%s | My Site' }` — a child with `title: 'Blog'` renders as "Blog | My Site".
- id: nextjs-metadata-files-04
  answer: |
    Next.js supports file-based metadata conventions: `icon.tsx` / `favicon.ico` for favicons, `opengraph-image.tsx` / `apple-icon.tsx` for images, `sitemap.ts` for a sitemap route handler returning `Sitemap`, and `robots.ts` for a route handler returning robots config. These files are detected by convention and handled automatically without needing to manually wire them into `<head>`.
- id: nextjs-navigation-link-01
  answer: |
    Use the `<Link>` component from `next/link`: `<Link href="/about">About</Link>`. Unlike a plain `<a>`, `<Link>` does client-side navigation (no full page reload), prefetches the target route in the background (when visible in the viewport), preserves scroll position by default, and enables the Router Cache for instant back/forward transitions. It's the idiomatic way to navigate between routes in the App Router.
- id: nextjs-navigation-hooks-02
  answer: |
    The App Router navigation hooks come from `next/navigation` — `useRouter`, `usePathname`, `useSearchParams`, `useParams`. They fail in a Server Component because they rely on client-side React context (the router state) that only exists on the client. To use them, mark the component (or a parent) with `"use client"` so it becomes a Client Component with access to the navigation context.
- id: nextjs-navigation-redirect-03
  answer: |
    `redirect(path)` sends the user to a new URL (throws a special NEXT_REDIRECT error internally — you don't catch it). `notFound()` renders the closest `notFound` UI (404 page) and throws a NEXT_NOT_FOUND error. Gotchas: both are implemented via thrown errors, so they must be called outside try/catch blocks; and they work in Server Components, Server Actions, and Route Handlers, but not in client event handlers (use `useRouter().push()` for client-side navigation instead).
- id: nextjs-navigation-action-redirect-04
  answer: |
    After a successful Server Action, call `redirect(newPath)` from `next/navigation` inside the action (or in the `.then()` chain if using `useTransition`). Next.js handles the navigation client-side without a full reload. Watch out for: (1) calling `redirect` inside a try/catch — the thrown redirect error can be caught and suppressed, so either re-throw or call it after the try/catch; (2) the path must be an absolute path or a valid relative route; (3) ensure the redirect happens after the data mutation is confirmed, not before.
