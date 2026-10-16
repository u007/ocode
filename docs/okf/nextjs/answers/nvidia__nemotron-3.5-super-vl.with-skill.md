# nvidia/nemotron-3.5-super-vl — with-skill answers for nextjs (openrouter, 2026-10-16); answerer saw only _prompts/nextjs.md; derived/nextjs.nvidia__nemotron-3.5-super-vl.SKILL.md body (front matter stripped) prepended to the sheet as active guidance
- id: nextjs-app-router-conventions-01
  answer: |
    `layout.tsx` is shared shell UI for a route segment: it wraps child pages/layouts and is preserved (not remounted) across navigation. `page.tsx` is a segment's unique UI; each segment has at most one page, exported as default. `loading.tsx` is a streaming fallback shown while the segment's content loads. `error.tsx` is an error-boundary UI for the segment (a Client Component receiving `error` and `reset`). `route.ts` is a Route Handler exporting HTTP method handlers (`GET`, `POST`, …); a segment cannot contain both `page.tsx` and `route.ts`. A folder under `app/` becomes a URL segment: `app/blog/page.tsx` → `/blog`, `app/blog/[slug]/page.tsx` → `/blog/:slug`. Parenthesized folders `(group)` are route groups and are excluded from the URL path.
- id: nextjs-app-router-layout-02
  answer: |
    Nested layouts wrap inward (root layout → child layout → page). On client-side navigation between routes that share a layout, the layout is not remounted, so React state, DOM, and listeners inside it are preserved; only the page below changes. `template.tsx` is like `layout.tsx` except it does not persist across navigation: each route change remounts the template and its children, resetting state and re-running effects. Use `layout.tsx` for persistent shells (sidebars, auth state) and `template.tsx` when you want fresh state or re-animation on every route change.
- id: nextjs-app-router-error-03
  answer: |
    `error.tsx` must be a Client Component (`"use client"`) and export a default component accepting `(error, reset)`; it acts as a React error boundary and `reset()` (from `next/navigation`) re-attempts rendering. It catches errors thrown while rendering its own segment's subtree — `page.tsx` and any child layouts/templates below it — but not errors in `error.tsx` itself, not errors in an ancestor layout, and not errors in `route.ts` handlers or directly in Server Actions called from the client.
- id: nextjs-app-router-loading-04
  answer: |
    Next.js wraps that route segment's content in a `<Suspense>` boundary whose fallback is the `loading.tsx` component. The layout shell and loading UI render immediately while the page and its data stream in from the server; once loaded, the content replaces the fallback. The loading UI is sticky — on later client-side navigations it generally does not reappear unless the underlying content changes — so it enables streaming without flashing a loader on every click.
- id: nextjs-server-components-default-01
  answer: |
    Components are Server Components by default in the App Router; every `.tsx`/`.ts` file in `app/` renders on the server unless marked otherwise. `"use client"` marks the file as a Client Component boundary: the module and its client-import graph are bundled for the browser, so components there can use hooks, event handlers, refs, and browser APIs (`window`, `localStorage`) and hydrate on the client. Server Components still render to HTML on the server; a Client Component used inside one renders as a server placeholder and hydrates client-side.
- id: nextjs-server-components-hooks-02
  answer: |
    Server Components render only on the server: there is no mounted component instance, no DOM, no event system, and no browser globals, so `useState`/`useEffect` have nothing to hook into, `onClick` handlers cannot be attached, and `window` is undefined. Fix: move the interactive piece into a Client Component (`"use client"`) — keep data fetching in the Server Component, pass the interactive part down as `children`/props, and attach state and handlers there.
- id: nextjs-server-components-props-03
  answer: |
    Props must be serializable across the server/client boundary: strings, numbers, booleans, null, plain objects and arrays of those, and React elements/JSX (`children`). You cannot pass functions, class instances, or anything with circular references. The idiomatic way to keep server-rendered content inside a client component is to pass it as `children`: `<ClientWrapper><serverRenderedSubtree /></ClientWrapper>` — the subtree stays on the server (rendered to HTML) and only the wrapper client component hydrates.
- id: nextjs-data-fetching-rsc-01
  answer: |
    Fetch directly in Server Components with `await fetch(url)` (or any async client such as Prisma) inside the component body; there is no special data-fetching API. `getServerSideProps` and `getStaticProps` are replaced by the component itself: routes render on demand or are prerendered, and data is fetched where it is used. Use `generateStaticParams` to enumerate dynamic routes at build time, and `export const revalidate` / `revalidatePath` / `revalidateTag` for ISR and on-demand invalidation.
- id: nextjs-data-fetching-nogssp-02
  answer: |
    Start all independent fetches at once instead of awaiting them serially: `const [a, b, c] = await Promise.all([fetchA(), fetchB(), fetchC()])`. Fetch calls begin immediately, and identical fetch URLs within one render pass are memoized (Request Memoization), so calling the same fetch in multiple components does not cause extra round trips. Never await each source one after another at the top of the page.
- id: nextjs-caching-fetch-default-01
  answer: |
    It depends on the Next.js version. Next.js 13 and 14: yes — `GET` `fetch` in a Server Component is cached by default (`force-cache`) in the Data Cache. Next.js 15 (and 16): no — `fetch` is uncached by default (`no-store`) and re-fetches each request; opt into caching with `{ cache: 'force-cache' }` on `fetch` or via `export const revalidate` / Data Cache controls. Always state the version; do not assume `fetch` is cached.
- id: nextjs-caching-layers-02
  answer: |
    Four App Router cache layers. Request Memoization: in-memory dedup of identical `fetch`/`read` calls within a single server render pass (automatic, discarded after the render). Data Cache: persistent, cross-request cache of `fetch` and `unstable_cache` results, controlled by `revalidate`, tags, and cache options. Full Route Cache: the prerendered HTML and RSC payload of static routes, reused across requests until invalidated or rebuilt. Router Cache: client-side (browser) cache of visited/prefetched route segments for instant back/forward and prefetching. Browser/CDN caching of hashed static assets is not one of these layers.
- id: nextjs-caching-revalidate-03
  answer: |
    `revalidate` (seconds) marks cached fetch/Data Cache data as stale after that interval; the next request serves the stale value while Next revalidates in the background (stale-while-revalidate) and updates the cache — that is ISR: the page stays statically served but is refreshed on a timer without redeploy. Fully static content (`revalidate = false`) is generated once at build and served indefinitely until on-demand invalidation or redeploy; it is never refreshed on a timer.
- id: nextjs-caching-ondemand-04
  answer: |
    `revalidatePath(path)` purges the Full Route Cache for a path (optionally its layout/subtree) so the page re-renders on the next request. `revalidateTag(tag)` purges Data Cache entries fetched with that tag (`fetch(..., { next: { tags } })`). Call them after the mutation succeeds — inside a Server Action, Route Handler, or webhook handler (server side), never during render — e.g. after writing to a database, `revalidatePath('/blog')` or `revalidateTag('posts')` so cached pages reflect the new data immediately.
- id: nextjs-caching-segment-config-05
  answer: |
    `export const dynamic` (`'auto' | 'force-dynamic' | 'error' | 'force-static'`) controls render mode: `auto` lets Next prerender if possible, `force-dynamic` renders on every request and disables the Data and Full Route caches (fetches behave as `no-store`), `force-static` prerenders and errors on dynamic APIs, and `error` errors on dynamic usage. `export const revalidate` (`false | 0 | N` seconds) sets cache lifetime: `false` caches indefinitely until on-demand invalidation, `0` never caches (always dynamic), `N` enables ISR after `N` seconds. Set `dynamic = 'force-dynamic'` when the segment must render per request — user-specific data, `cookies()`/`headers()`/`searchParams`, draft mode — and you want to opt out of static prerendering and caching.
- id: nextjs-rendering-static-dynamic-01
  answer: |
    By default Next tries to statically prerender a route; if during render the code touches any dynamic API (`cookies()`, `headers()`, unresolved `params`/`searchParams` in Next 15, `fetch` with `cache: 'no-store'`, `draftMode()`), the segment falls back to dynamic rendering (SSR on each request). Route segment config (`export const dynamic = 'force-static' | 'force-dynamic'`) can force either behavior, and `generateStaticParams` can prerender dynamic routes. So a route is static unless something in its tree forces it dynamic.
- id: nextjs-rendering-static-params-02
  answer: |
    `generateStaticParams` (exported from `page.tsx`/`layout.tsx` of `app/blog/[slug]`) returns an array of `{ slug: '...' }` values that Next prerenders at build time, so those dynamic routes exist in the Full Route Cache instead of being rendered per request. Pages Router equivalent: `getStaticPaths` (returning `paths: [{ params: { slug } }]` plus `fallback`), used with `getStaticProps`.
- id: nextjs-rendering-dynamic-apis-03
  answer: |
    In Next.js 15+, `cookies()`, `headers()`, `params`, and `searchParams` return Promises because their values are request-specific and Next wants concurrent, parallel rendering without blocking on them; you must await them: `const cookieStore = await cookies(); const { slug } = await params;`. Reading them in a page/layout marks the segment dynamically rendered (it cannot be fully statically prerendered at build), so that route opts into SSR per request unless you force-static (which errors on these APIs) or statically enumerate params.
- id: nextjs-server-actions-useserver-01
  answer: |
    `"use server"` marks a Server Action: an async function that runs on the server but can be invoked from the client (via `<form action>` or direct calls from Client Components). It is the opposite of `"use client"`: `"use client"` marks code that executes in the browser (interactivity, hooks), while `"use server"` marks code that executes only on the server (an RPC endpoint). A file can export server actions; client components import and call them like functions, but the body never runs in the browser.
- id: nextjs-server-actions-mutation-02
  answer: |
    Write the action in a `"use server"` file: `async function createPost(formData) { const title = formData.get('title'); validate it; await db.post.create({ title }); revalidatePath('/posts'); redirect('/posts'); }`. Then use it: `<form action={createPost}>…</form>` (works from a Server Component too, with progressive enhancement) or call it from a Client Component event handler. On submit the action runs server-side, writes data, and you call `revalidatePath` to purge stale cache or `redirect` to send the user to the new page; use `useActionState`/`formAction` if you need pending or error UI in the client.
- id: nextjs-server-actions-security-03
  answer: |
    It is a trap because it looks like an in-process function call but is actually an exposed HTTP endpoint: anyone can call it with forged inputs, so `formData` and arguments are fully user-controlled and not type-safe at runtime. Inside every action you must authenticate the user, authorize the operation, validate and sanitize all inputs (e.g. with Zod), check permissions before writing, never trust the client, and avoid leaking secrets — treat it exactly like a route handler.
- id: nextjs-route-handlers-basics-01
  answer: |
    A Route Handler (`app/**/route.ts`) handles raw HTTP requests for a route segment using the Web `Request`/`Response` API: you export functions named `GET`, `POST`, `PUT`, `DELETE`, `PATCH`, `HEAD`, `OPTIONS`, each receiving `(request, context)` and returning a `Response`. It is the App Router replacement for `pages/api` API routes; a segment can have either `page.tsx` or `route.ts`, not both.
- id: nextjs-route-handlers-caching-02
  answer: |
    Version-specific: Next.js 13 and 14 cache `GET` Route Handlers by default; Next.js 15 and 16 do not — `GET` handlers are uncached by default. To opt into Next's Data/Full Route caching, write `export const dynamic = 'force-static'` in `route.ts` (optionally add `export const revalidate = N` for periodic regeneration). Setting response headers (`Cache-Control`) does not opt into Next's caches; non-GET methods are never cached, and using `cookies()`, `headers()`, or reading the `Request` body keeps a `GET` dynamic even with the opt-in.
- id: nextjs-route-handlers-methods-03
  answer: |
    Body: `await request.json()` / `await request.text()` / `await request.formData()` (reading the body makes the handler dynamic). Query params: `new URL(request.url).searchParams` or `request.nextUrl.searchParams` — there is no `searchParams` prop on a route handler. Dynamic segment values: `context.params` — on Next 15+ it is a Promise, so `const { id } = await context.params`.
- id: nextjs-streaming-ssr-01
  answer: |
    Streaming SSR sends the page to the browser in chunks as they finish rendering (through `<Suspense>` boundaries), instead of buffering the entire HTML/RSC payload and sending it all at once. That buys a faster time-to-first-byte and first paint: the shell and fast sections appear immediately while slow data sections stream in later, so a slow query does not block the whole page and the user sees content sooner.
- id: nextjs-streaming-suspense-02
  answer: |
    Move the slow data section into its own component and wrap it (or its route segment) in a `<Suspense>` boundary with a fallback — or add a `loading.tsx` to that segment — so the rest of the page renders and streams immediately while the slow section streams in separately. Do not await the slow data at the top of the page before rendering anything else.
- id: nextjs-streaming-boundary-03
  answer: |
    `loading.tsx` is a route-level, file-based Suspense boundary: Next supplies the fallback UI from the file and applies it to that segment; it is integrated with streaming and metadata and is sticky across navigations. A custom `<Suspense>` boundary is component-level: you place it anywhere in the tree and supply your own fallback, giving granular control over which subtree suspends, but you manage the fallback yourself and it re-shows on client-side state-driven suspensions.
- id: nextjs-metadata-static-01
  answer: |
    Export a `metadata` object from `page.tsx` or `layout.tsx` (or a `generateMetadata` function) with fields such as `title`, `description`, `icons`, `openGraph`, `twitter`, `robots`, `manifest`, and `other` (for arbitrary `<head>` tags). Next.js automatically renders the corresponding `<head>` elements; the metadata export is the declarative way to set title and head tags per route.
- id: nextjs-metadata-dynamic-02
  answer: |
    Export an async `generateMetadata({ params, searchParams }, parent)` from the page or layout, fetch the data there (the same fetch is request-memoized with the page's fetch, so it costs one round trip), and return `{ title, description, … }` derived from the data. Because it can read `params`/`searchParams`, the route becomes dynamically rendered unless you statically enumerate params with `generateStaticParams`.
- id: nextjs-metadata-inherit-03
  answer: |
    Metadata merges from the outermost layout inward to the page: root layout fields are inherited by child layouts and pages, and a child can override scalar fields (`title`, `description`) or extend them. `title.template` lets a parent wrap every child title: if the layout sets `title.template: '%s | Site'` and the page sets `title: 'Post'`, the rendered title is `Post | Site` — useful for consistent branding across nested routes.
- id: nextjs-metadata-files-04
  answer: |
    Place convention-named files in the route segment: `favicon.ico` (favicon), `opengraph-image.png`/`.jpg` (`og:image`), `apple-icon.jpg` (`apple-touch-icon`), and icon files for `<link rel="icon">` — Next maps them to the right `<head>` tags automatically, and you can also reference `public/` files via `metadata.icons` / `metadata.openGraph.images`. For `sitemap.xml` and `robots.txt`, add `app/sitemap.ts` (export `generateSitemap`) and `app/robots.ts` (export `generateRobots`), or static `sitemap.xml` / `robots.txt` files in `app`.
- id: nextjs-navigation-link-01
  answer: |
    Navigate with `<Link href="/about">` from `next/link` for declarative client-side navigation, or `router.push` / `router.replace` from `next/navigation` inside Client Component event handlers; Server Actions can redirect with `redirect()`. `<Link>` renders an `<a>` but intercepts clicks for client-side navigation (no full reload), prefetches the route's code and data on hover/in viewport, preserves client state, and handles scroll — a plain `<a>` triggers a hard browser navigation (full document reload).
- id: nextjs-navigation-hooks-02
  answer: |
    They come from `next/navigation` (`useRouter`, `usePathname`, `useSearchParams`, `useParams`). They fail in Server Components because they read client-only React router context / browser state that does not exist during server rendering; use them only in Client Components (`"use client"`). In Server Components, read route data from the awaited `params`/`searchParams` props passed to the page or layout instead.
- id: nextjs-navigation-redirect-03
  answer: |
    `redirect(url)` aborts rendering and sends the client to another URL (permanent only with `{ permanent: true }`); `notFound()` renders the nearest `not-found.tsx` / 404. Gotcha: both are implemented by throwing, so code after them never runs and wrapping them in `try/catch` will swallow the redirect/404 — do not catch them. They are server APIs (Server Components, Server Actions, Route Handlers); for client-side navigation use `router.push`/`router.replace` instead.
- id: nextjs-navigation-action-redirect-04
  answer: |
    Call `redirect('/posts/new-id')` from inside the Server Action after the write succeeds (optionally `revalidatePath` first so the destination is fresh). Watch out: `redirect` throws, so do not put it in `try/catch` and do not run code after it; navigation happens after the action completes, so validate and authorize before mutating, and make sure the target route exists. If you need to stay on the page and just refresh it, use `revalidatePath` instead; for client-side navigation after an action, use `router.push` from a Client Component.
