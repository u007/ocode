# nvidia/nemotron-3.5-super-vl — closed-book answers for nextjs (openrouter, 2026-10-16); answerer saw only _prompts/nextjs.md
- id: nextjs-app-router-conventions-01
  answer: |
    layout.tsx wraps a route segment and persists across navigations (state is preserved; only the page below it changes). page.tsx is the route's unique UI — each route needs one default export. loading.tsx is a Suspense fallback shown while that segment loads. error.tsx is an error boundary for the segment (a Client Component receiving error + reset props). route.ts is a Route Handler (an HTTP endpoint: GET/POST/etc.), not UI. A folder maps to a URL path segment: app/blog/page.tsx → /blog, app/blog/[slug]/page.tsx → /blog/hello-world; special files (layout, loading, error, route) do not add URL segments.
- id: nextjs-app-router-layout-02
  answer: |
    Nested layouts nest: an outer layout receives children containing the inner layout plus the page, so multiple layers wrap the content. On navigation between segments that share a layout, Next.js keeps that layout mounted (client-side navigation), so its state, scroll position, and Client Components survive; only the page changes. template.tsx is like layout.tsx but is re-mounted on every navigation, so it loses state and re-runs effects — use it when you want fresh per-navigation behavior (e.g., animations), while layout.tsx persists.
- id: nextjs-app-router-error-03
  answer: |
    error.tsx must be a Client Component ('use client') and export a default component receiving (error, reset) props; an optional root app/error.tsx catches errors escaping other boundaries. It catches errors thrown while rendering its own route segment's descendants during server or client render (layouts, pages, loading.tsx, Server Components, and Client Components inside it). It does NOT catch errors in event handlers/client interactivity (use try/catch or React error boundaries there), errors in route.ts Route Handlers, errors outside its segment, or errors thrown inside error.tsx itself (those bubble to the nearest parent boundary).
- id: nextjs-app-router-loading-04
  answer: |
    loading.tsx creates a Suspense boundary around the route segment. On first load and on navigation into the segment, Next.js streams the loading UI immediately as a fallback while the segment's data and page finish rendering server-side, then swaps in the real content when ready — so the page never blocks waiting for that segment. It is a default-export component only and wraps the segment's children.
- id: nextjs-server-components-default-01
  answer: |
    Components are Server Components by default in the App Router. "use client" at the top of a file marks a module boundary: that file is compiled to client code and may use hooks, event handlers, window, etc.; anything imported by it is treated as client code. Server Components can import and render Client Components, but a Client Component cannot import a Server Component directly — server and client code are split at that boundary.
- id: nextjs-server-components-hooks-02
  answer: |
    A Server Component renders on the server into an RSC payload: there is no browser, no window/document, no commit phase, and no client effect system, so useState/useEffect have nothing to hook into and onClick handlers cannot be serialized and delivered to the client. Fix: move interactivity into a separate file marked 'use client' (a Client Component) and render it from the Server Component, passing serializable props or children.
- id: nextjs-server-components-props-03
  answer: |
    Props from a Server Component to a Client Component must be RSC-serializable: JSON-able data (strings, numbers, booleans, null, arrays, plain objects) and React elements/JSX. Functions, class instances, refs, and browser APIs cannot be passed. The idiomatic way to keep server-rendered content inside a Client Component is to render the server part and pass it as the Client Component's children prop (or another JSX prop) — the server renders that subtree and the client component only renders {children}, so it is not re-rendered on the client.
- id: nextjs-data-fetching-rsc-01
  answer: |
    Fetch data directly in async Server Components using native fetch (Next.js extends it) or any data library, with await — components themselves can be async. There is no getServerSideProps/getStaticProps: data fetching collapses into the component, and static generation is opted into through cached fetch / segment revalidation instead of a special exported function.
- id: nextjs-data-fetching-nogssp-02
  answer: |
    Start all independent fetches without awaiting them one at a time (collect the promises, then Promise.all/Promise.allSettled), because Server Components are async and each fetch begins when called. Awaiting sequentially creates a waterfall; launching them concurrently keeps total time near the slowest source. React.cache/unstable_cache can dedupe identical fetches across components.
- id: nextjs-caching-fetch-default-01
  answer: |
    Version-dependent. Next.js 13.x–14.x App Router: yes, await fetch(url) is cached by default (force-cache in the Data Cache) for GET unless you opt out ({ cache: 'no-store' } or { next: { revalidate: 0 } }). Next.js 15+: no — fetch is uncached by default (no-store) unless you opt in ({ cache: 'force-cache' } or next.revalidate).
- id: nextjs-caching-layers-02
  answer: |
    Data Cache (server): caches fetch results and unstable_cache data across requests until revalidated. Full Route Cache: caches the statically rendered HTML/RSC payload of a route until invalidated. Router Cache (client/browser): caches RSC payloads of visited and prefetched routes for instant back/forward and client navigation. Static asset cache: immutable hashed JS/CSS/images served from CDN/browser cache.
- id: nextjs-caching-revalidate-03
  answer: |
    revalidate (seconds) sets time-based ISR: once content is stale, the next request gets the cached response while Next.js regenerates in the background, so following requests see fresh data. Fully static content is produced once at build/deploy and stays until a rebuild or on-demand revalidate; ISR updates pages at runtime without rebuilding the whole site.
- id: nextjs-caching-ondemand-04
  answer: |
    revalidatePath(path) and revalidateTag(tag) from next/cache trigger on-demand revalidation: revalidatePath purges the Full Route Cache for a path (and optionally its subtree), revalidateTag purges Data Cache entries tagged with that tag. Call them server-side after a mutation — typically inside a Server Action or Route Handler — so cached pages/data immediately reflect the change (not in Client Components or during render).
- id: nextjs-caching-segment-config-05
  answer: |
    export const dynamic ('force-dynamic' | 'static' | 'error') opts a route segment out of or into static rendering; force-dynamic renders per request always. export const revalidate sets ISR revalidation seconds for the route's output (false/0 means no revalidation). Use dynamic = 'force-dynamic' when the route reads request-time data (cookies, headers, searchParams, geo) or uses dynamic APIs and must never be statically rendered at build.
- id: nextjs-rendering-static-dynamic-01
  answer: |
    If a segment only uses cacheable/static APIs and has no dynamic opt-outs, Next.js statically renders it at build (Full Route Cache). If it uses dynamic functions (cookies(), headers(), await params/searchParams in Next.js 15), dynamic segment config, or uncached data with no static params, it renders dynamically per request (SSR). generateStaticParams can pre-render specific dynamic values statically.
- id: nextjs-rendering-static-params-02
  answer: |
    generateStaticParams returns [{ slug: '...' }, ...] at build time so Next.js pre-renders /blog/<slug> for each entry as static pages instead of generating them on demand. Pages Router equivalent: getStaticPaths (paired with getStaticProps).
- id: nextjs-rendering-dynamic-apis-03
  answer: |
    In Next.js 15 these are async because their values are request-scoped and can differ per request/user; awaiting keeps them compatible with caching and partial prerendering and makes the dynamic intent explicit. Reading them in a page/layout marks that route dynamically rendered (not statically at build), since its output can vary by request.
- id: nextjs-server-actions-useserver-01
  answer: |
    "use server" marks an async function (or a whole file) as a Server Action — callable from the client/forms but executing only on the server. It is the opposite of "use client": client marks code that runs in the browser; server marks code that runs on the server and is never sent to the client.
- id: nextjs-server-actions-mutation-02
  answer: |
    Define an async action with "use server" (file-level directive or above the function), validate input, await the data write (e.g., db.insert), call revalidatePath/revalidateTag to refresh caches, and optionally redirect() to the new page. Import it into a Client Component and use <form action={createPost}> (optionally with useFormState/useFormStatus for messages and pending state). On submit, Next.js POSTs the form data to the action; it runs server-side, then revalidate/redirect updates the UI without a full reload.
- id: nextjs-server-actions-security-03
  answer: |
    Because it looks like a local function call, it is easy to forget it is an exposed HTTP endpoint anyone can invoke. Inside every action you must authenticate and authorize the current user, validate and sanitize all inputs (schema), never trust arguments, and avoid leaking secrets or internal logic.
- id: nextjs-route-handlers-basics-01
  answer: |
    A Route Handler (route.ts) exports named handlers (GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS) receiving a Request and returning a Response using the web fetch API; app/x/route.ts is served at /x. It replaces pages/api API routes, with the same URL-segment routing as the App Router.
- id: nextjs-route-handlers-caching-02
  answer: |
    Next.js 13.x–14.x: GET Route Handlers are cached by default (static) unless they use cookies/headers/etc. or export const dynamic = 'force-dynamic'. Next.js 15+: no, they are uncached/dynamic by default; do not assume caching — control behavior via headers/segment config and treat them as per-request unless explicitly cached.
- id: nextjs-route-handlers-methods-03
  answer: |
    Body: await request.json() / .text() / .formData(). Query params: new URL(request.url).searchParams (or NextRequest.nextUrl.searchParams). Dynamic route params: the second argument context.params (e.g., context.params.slug); in Next.js 15 await params/searchParams where they are promises.
- id: nextjs-streaming-ssr-01
  answer: |
    Streaming SSR renders the server output in chunks over HTTP as they finish instead of buffering the whole page; Suspense boundaries let fast sections stream first while slow sections stream later. It buys lower TTFB/FCP, the user sees content immediately, no full-page wait, and progressive hydration.
- id: nextjs-streaming-suspense-02
  answer: |
    Isolate the slow section behind its own <Suspense> (or its own loading.tsx segment) so it does not block the rest of the page; start its fetch concurrently/early and do not await it at the top of the page. The shell streams immediately and the slow section fills in when ready.
- id: nextjs-streaming-boundary-03
  answer: |
    loading.tsx is a file-based, route-segment Suspense boundary managed by Next.js: it shows on initial load and on client navigation into the segment, and is auto-wired to the route. A manual <Suspense> is a component-level boundary you place anywhere; it applies to its own render (mainly initial load unless you force a re-render) and is not navigation-aware.
- id: nextjs-metadata-static-01
  answer: |
    export const metadata = { title, description, openGraph: {...}, icons, robots, ... } in layout.tsx or page.tsx; Next.js renders these into <title> and <meta> tags automatically. For dynamic data use export async function generateMetadata({ params, searchParams }, parent) { ... return { title, description } }.
- id: nextjs-metadata-dynamic-02
  answer: |
    generateMetadata can await data (fetch by params.slug) and return title/description; Next.js calls it during rendering and streams the <title> early, and it shares the fetch cache with the page so the data is not fetched twice.
- id: nextjs-metadata-inherit-03
  answer: |
    Child metadata merges into parent layout metadata; a page's title wins over layout defaults unless title.template is used. title.template (a string with %s, or a function) on a parent layout wraps the child's title, e.g. '%s | SiteName', so every child page gets 'Post title | SiteName'. Icons/openGraph/robots from parents are inherited unless overridden; generateMetadata receives the resolved parent metadata via its parent argument to merge.
- id: nextjs-metadata-files-04
  answer: |
    favicon: metadata.icons (or favicon.ico in the root route/public). Open Graph image: metadata.openGraph.images, or opengraph-image.png/.jpg placed in a route segment folder. sitemap.xml: app/sitemap.ts exporting async function sitemap() returning URL entries. robots.txt: app/robots.ts exporting async function robots() returning rules. These are served at /sitemap.xml and /robots.txt.
- id: nextjs-navigation-link-01
  answer: |
    Use <Link href="/about"> from next/link for navigation, or router.push/replace/prefetch from useRouter (next/navigation) programmatically. <Link> performs client-side navigation without a browser reload, prefetches the linked route's JS and RSC payload on hover/in viewport, preserves React state, and handles scroll — a plain <a> triggers a full document load.
- id: nextjs-navigation-hooks-02
  answer: |
    All come from next/navigation (useRouter, usePathname, useSearchParams, useParams). They fail in a Server Component because they read client router state/hooks (window location, suspenseful router context) that do not exist on the server; use them only in Client Components, and read route data (params/searchParams) as props in Server Components.
- id: nextjs-navigation-redirect-03
  answer: |
    redirect(url) aborts rendering and sends the client to another URL (second arg { permanent: true } for 308, default 307); notFound() renders the nearest notFound.tsx (404). Gotcha: both throw internally (Next.js uses a special thrown signal), so treat them as terminal — call as the final/returned statement, do not wrap in try/catch expecting the rest to run, and use them only in Server Components/Actions/Route Handlers (not client event handlers; use router.push there).
- id: nextjs-navigation-action-redirect-04
  answer: |
    Call redirect('/new-page') from next/navigation inside the Server Action after the mutation (plus revalidatePath/revalidateTag); Next.js performs client-side navigation on completion. Watch out: redirect throws, so make it the final/returned statement and do not put code after it expecting to run; use it only in server code (not client onClick — use router.push there); ensure the target route is available/revalidated so the user does not land on stale content.
