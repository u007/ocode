# cat-jam-free — WITH-SKILL closed-book answers for nextjs (opencode-go, 2026-10-14)
# Delivery: derived/nextjs.cat-jam-free.SKILL.md body (front matter stripped) prepended to _prompts/nextjs.md; one
# `ocode run -m opencode-go/cat-jam-free -effort med` session, isolated HOME with every tool denied, cwd holding only the sheet;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: nextjs-app-router-conventions-01
  answer: |
    In the App Router, folders define URL segments, so app/blog/[slug]/page.tsx maps to /blog/:slug. layout.tsx wraps a segment and its descendants and persists across child navigations. page.tsx is the leaf UI for that route. loading.tsx creates the route segment's loading state. error.tsx is that segment's error boundary. route.ts is an HTTP API endpoint (Route Handler), not a page. The root app/layout.tsx is required.

- id: nextjs-app-router-layout-02
  answer: |
    Nested layouts compose around nested routes and are preserved during navigation within their subtree, so their state persists while only changed child segments update. template.tsx has the same nesting role but creates a new instance on every navigation, remounting children and resetting state/effects, which is useful for transitions/animations.

- id: nextjs-app-router-error-03
  answer: |
    error.tsx must be a Client Component ("use client") because error boundaries run on the client. It catches rendering errors from its route segment and descendants, including thrown errors during render, and can reset via reset(). It does not typically catch errors in its own layout or the root layout; those propagate upward to a parent/global error boundary, and it cannot catch all async/server failures outside rendering.

- id: nextjs-app-router-loading-04
  answer: |
    Adding loading.tsx automatically wraps the route segment's page/content in a Suspense boundary using loading.tsx as the fallback. Next shows that fallback immediately during navigation/data loading, then streams in the resolved content, enabling segment-level progressive rendering.

- id: nextjs-server-components-default-01
  answer: |
    App Router components are Server Components by default. "use client" is a boundary marker: it makes that module and everything it imports part of the client bundle, and all components reached through it become Client Components whether individually marked or not.

- id: nextjs-server-components-hooks-02
  answer: |
    Server Components run only on the server and render to a serializable React tree; they have no client runtime state, effects, event system, or browser APIs. useState/useEffect, onClick, and window require client interactivity, so move the interactive part into a small "use client" component or split it out as a leaf Client Component.

- id: nextjs-server-components-props-03
  answer: |
    A Server Component may pass serializable values to a Client Component: primitives, plain objects/arrays, Dates, and Server Actions. To keep server-rendered UI inside a client component, compose by passing already-rendered Server Component subtrees as children or element props rather than importing them from the client file.

- id: nextjs-data-fetching-rsc-01
  answer: |
    Fetch data directly in async Server Components using await fetch() or a database call in the component body. There are no getServerSideProps/getStaticProps; their roles are replaced by Server Components, generateStaticParams for static paths, and Route Handlers for HTTP endpoints.

- id: nextjs-data-fetching-nogssp-02
  answer: |
    Avoid sequential awaits; start independent requests together with Promise.all([...]) and await once, or initiate separate fetches before awaiting them. Identical fetches in one render are automatically deduped by request memoization; use React.cache() for non-fetch (database/ORM) calls.

- id: nextjs-caching-fetch-default-01
  answer: |
    Version-sensitive: in Next.js 14 and earlier, GET fetch() defaults to cached behavior (force-cache semantics). In Next.js 15+, fetch() is not cached by default (no-store semantics); explicitly set cache: "force-cache" or next.revalidate to opt into caching.

- id: nextjs-caching-layers-02
  answer: |
    The main layers are Request Memoization (dedupes identical fetches/React.cache calls during one render), the Data Cache (persisted fetch results/tags/revalidation across requests), the Full Route Cache (rendered HTML/RSC payload for static routes), and the Router Cache (client-side cache of visited/prefetched RSC payloads).

- id: nextjs-caching-revalidate-03
  answer: |
    revalidate sets a time window after which cached data may be regenerated. With time-based revalidation, a request can serve the stale cached content while Next regenerates in the background, updating later. Fully static content has no revalidation window and is not regenerated on a schedule.

- id: nextjs-caching-ondemand-04
  answer: |
    revalidatePath(path) invalidates cached route/output for a path; revalidateTag(tag) invalidates fetches tagged with next.tags. Call them on the server after a successful mutation, such as in a Server Action or Route Handler, to purge the Data/Full Route Cache and trigger fresh rendering.

- id: nextjs-caching-segment-config-05
  answer: |
    export const dynamic controls whether a segment is auto, force-dynamic, force-static, or error on dynamic APIs. export const revalidate sets the segment's revalidation interval for cached rendering. Use dynamic = "force-dynamic" when the route must render per request, such as when reading cookies, headers, searchParams, or uncached request-specific data.

- id: nextjs-rendering-static-dynamic-01
  answer: |
    Next statically prerenders a route when it can resolve its output at build time without dynamic request data; it opts into dynamic rendering if it uses dynamic functions/APIs (cookies, headers, searchParams, certain params), uncached fetches, force-dynamic, etc. Static generation can also be driven by generateStaticParams.

- id: nextjs-rendering-static-params-02
  answer: |
    generateStaticParams returns the parameter objects to prerender for a dynamic route such as app/blog/[slug], producing static pages for those paths at build. The Pages Router equivalent is getStaticPaths (with getStaticProps).

- id: nextjs-rendering-dynamic-apis-03
  answer: |
    In Next.js 15+, cookies(), headers(), params, and searchParams are asynchronous/Promise-based to support async request scope and progressive rendering. Reading them requires await; accessing these request-derived APIs generally forces the route to be dynamically rendered rather than fully statically generated.

- id: nextjs-server-actions-useserver-01
  answer: |
    "use server" marks functions as Server Actions executed on the server and callable from client code through an RPC-like mechanism. It does not create a client bundle boundary; "use client" is the client bundle boundary, while "use server" moves execution to the server.

- id: nextjs-server-actions-mutation-02
  answer: |
    Define an async function with "use server", pass it to a form's action prop, parse and validate FormData, authorize the user, mutate the store/database, then call revalidatePath/revalidateTag and optionally redirect. For UI state, use useActionState and useFormStatus for pending/error states; the form posts to the server without a manual fetch.

- id: nextjs-server-actions-security-03
  answer: |
    A Server Action is exposed as a network endpoint even when it looks like a local call, so arbitrary clients can invoke it. Every action must validate its input, authenticate the caller, enforce authorization and ownership, avoid trusting client-provided values, and keep secrets/server-only logic on the server.

- id: nextjs-route-handlers-basics-01
  answer: |
    A Route Handler is a route.ts file in the App Router exporting HTTP methods like GET or POST. It replaces the Pages Router's pages/api/* routes and handles endpoint logic, returning Web Response/NextResponse objects at a filesystem-defined URL.

- id: nextjs-route-handlers-caching-02
  answer: |
    Route Handler GET responses are not cached by default and are treated dynamically. To opt into caching/static behavior, export dynamic = "force-static" (and optionally revalidate = N) or use cache/revalidation options inside the handler; request-derived dynamic APIs make it dynamic.

- id: nextjs-route-handlers-methods-03
  answer: |
    Read the request body with await request.json() or request.formData(); read query parameters from request.nextUrl.searchParams (or request.url). Dynamic route params arrive in the handler's second argument as a params promise in Next.js 15+, so await params after destructuring.

- id: nextjs-streaming-ssr-01
  answer: |
    Streaming SSR renders the shell and streams HTML/RSC chunks as Suspense boundaries resolve instead of waiting for the entire page. It improves TTFB and perceived performance by showing usable UI sooner and letting slow sections load progressively.

- id: nextjs-streaming-suspense-02
  answer: |
    Keep the fast page content outside a boundary and wrap only the slow data-driven subtree in <Suspense fallback={...}>. The parent renders immediately with the fallback; the slow section streams in when its data finishes. Revalidate boundary placement based on loading units.

- id: nextjs-streaming-boundary-03
  answer: |
    loading.tsx creates one automatic Suspense boundary for a whole route segment with a default loading fallback. A custom Suspense boundary gives finer-grained control inside the page, allows multiple independently loading sections, and can use tailored fallback UI.

- id: nextjs-metadata-static-01
  answer: |
    Export a static metadata object from a server page or layout; Next renders the appropriate head tags itself. Do not hand-write head/title tags. You can set title, description, icons, openGraph, canonical, and similar fields there.

- id: nextjs-metadata-dynamic-02
  answer: |
    Export async generateMetadata with params/searchParams, fetch the needed data, and return a metadata object. Fetches used there are deduped with page/layout fetches by request memoization when URL and options match.

- id: nextjs-metadata-inherit-03
  answer: |
    Metadata merges from the root layout down through nested layouts and pages, with child values overriding parent values. title.template in a parent layout formats descendant page titles, e.g. "%s | Site Name"; title.default provides the fallback when no title is set.

- id: nextjs-metadata-files-04
  answer: |
    Next supports file-based metadata conventions in the app directory: favicon.ico, icon files, apple-icon, opengraph-image, twitter-image, plus sitemap and robots as TS/JS exports. Next automatically emits corresponding head tags or serves generated XML files.

- id: nextjs-navigation-link-01
  answer: |
    Use <Link> from next/link for client-side navigation. It avoids a full page reload, preserves app state, enables prefetching, and updates the route; a plain <a> triggers a full document navigation/reload.

- id: nextjs-navigation-hooks-02
  answer: |
    App Router hooks such as useRouter, usePathname, and useSearchParams come from next/navigation. They are client-only because they depend on React/router client context and browser location state, so they throw or are unavailable in Server Components; put them in a "use client" component.

- id: nextjs-navigation-redirect-03
  answer: |
    redirect() throws an internal redirect to navigate to a URL; notFound() triggers the nearest not-found/404 UI. A gotcha is that redirect is implemented by throwing, so code after it will not run and broad try/catch blocks can accidentally swallow or mishandle it; validate/revalidate before calling it.

- id: nextjs-navigation-action-redirect-04
  answer: |
    In the Server Action, after mutation, call revalidatePath/revalidateTag as needed, then call redirect() to the destination page. Watch that redirect throws and stops execution afterward, so perform cleanup/validation first and do not rely on code after redirect running.
