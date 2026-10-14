# cat-jam-free — closed-book answers for nextjs (opencode-go, 2026-10-14); answerer saw only _prompts/nextjs.md
# Delivery: one `ocode run -m opencode-go/cat-jam-free -effort med` session, sheet attached via -f, empty scratch cwd;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: nextjs-app-router-conventions-01
  answer: |
    A folder under `app/` becomes a URL segment: nested folders map to nested routes (e.g. `app/blog/[slug]` => `/blog/:slug`); route groups `(group)` and parallel slots do not affect the URL. Special files: `page` makes the segment publicly routable and provides its UI; `layout` wraps the segment and nested children with shared UI that persists across navigations; `loading` provides a Suspense fallback while the segment loads; `error` defines a client error boundary around the segment's children; `route` defines an HTTP Route Handler instead of a page.

- id: nextjs-app-router-layout-02
  answer: |
    Nested layouts compose from root down, and a shared layout stays mounted during navigation between its nested routes, preserving layout state and children without remounting. `template.tsx` looks similar but creates a fresh instance on navigation, so its state resets and effects rerun; use it for transitions, per-navigation effects, or resetting client state. A layout persists; a template does not.

- id: nextjs-app-router-error-03
  answer: |
    `error.tsx` must be a Client Component (`"use client"`) and usually exports a default component receiving `error` and `reset`. It catches rendering/runtime errors in its segment's children, including nested pages and child segments. It does not catch errors in its own layout/template, the root layout, or errors outside that boundary; root-level failures need `global-error.tsx`.

- id: nextjs-app-router-loading-04
  answer: |
    Adding `loading.tsx` automatically wraps the route segment in a React `Suspense` boundary using that file's UI as fallback. The layout shell and fallback can stream immediately while the segment's data/UI resolves, avoiding a blank wait for that segment.

- id: nextjs-server-components-default-01
  answer: |
    In the App Router, components are Server Components by default. `"use client"` marks a client boundary in that module and allows hooks, event handlers, browser APIs, and client-only dependencies; everything not marked is server-rendered unless specifically client.

- id: nextjs-server-components-hooks-02
  answer: |
    Server Components run on the server and emit an RSC payload, so they have no browser environment and no React state/effects/event handlers. Accessing `window`, `useState`, `useEffect`, or `onClick` is invalid there. The fix is to isolate interactivity in a Client Component (`"use client"`), often a small leaf component passed props from the server parent.

- id: nextjs-server-components-props-03
  answer: |
    A Server Component can pass only serializable values to a Client Component (primitives, plain objects/arrays, and in many cases dates), plus Server Actions. It can also pass React elements such as children. The idiomatic way to keep server-rendered content inside a client component is the children/slot pattern: render the server subtree in the server parent and pass it as `children` or a named prop.

- id: nextjs-data-fetching-rsc-01
  answer: |
    In the App Router, fetch directly inside async Server Components with `await`, and use an ORM/database client directly when appropriate. `getServerSideProps` and `getStaticProps` are not used; caching, revalidation, and dynamic behavior are controlled by fetch options, route segment config, and `generateStaticParams`.

- id: nextjs-data-fetching-nogssp-02
  answer: |
    Avoid a waterfall by starting all independent requests before awaiting, e.g. `const [a, b] = await Promise.all([fetchA(), fetchB()])`. Do not await each fetch sequentially. Separate async components wrapped in `Suspense` can also let independent sections stream without blocking each other.

- id: nextjs-caching-fetch-default-01
  answer: |
    Version matters: in Next.js 14 and earlier, `fetch` in the App Router is cached by default (`force-cache` unless `cache: 'no-store'` is set). In Next.js 15 and later, fetch is not cached by default; set `cache: 'force-cache'` or otherwise configure revalidation to opt into caching.

- id: nextjs-caching-layers-02
  answer: |
    The App Router caching layers are: Request Memoization (dedupes identical fetches during one render); Data Cache (persistent server-side fetch cache); Full Route Cache (prerendered HTML/RSC payload for static routes); and Router Cache (temporary client-side navigation cache). Together they avoid duplicate work across render, request, deployment, and navigation.

- id: nextjs-caching-revalidate-03
  answer: |
    `revalidate` controls cache lifetime and background regeneration: cached data is served, then re-fetched/revalidated after the specified interval or under ISR rules. Time-based revalidation (ISR) can update content after deployment without a rebuild, while fully static content is effectively fixed until a redeploy or on-demand revalidation.

- id: nextjs-caching-ondemand-04
  answer: |
    Use `revalidatePath(path)` to invalidate a route's cached output or `revalidateTag(tag)` to invalidate fetches tagged with that tag. Import them from `next/cache` and call them server-side, typically in a Server Action or Route Handler after a successful mutation, before or alongside redirect/refresh.

- id: nextjs-caching-segment-config-05
  answer: |
    `export const dynamic` controls render strategy (`auto`, `force-dynamic`, `force-static`, `error`); `export const revalidate` sets ISR seconds or disables time-based revalidation. Use `dynamic = 'force-dynamic'` when a route depends on request-specific data such as cookies, headers, `searchParams`, or uncached/no-store data and cannot be prerendered.

- id: nextjs-rendering-static-dynamic-01
  answer: |
    Next.js renders a route statically if it can prerender all of it at build time without request-specific data; it becomes dynamic if it uses dynamic APIs such as cookies/headers, reads `searchParams`, or performs uncached/no-store fetches. For dynamic segments, `generateStaticParams` provides the params to prerender; other params may render on demand depending on `dynamicParams`.

- id: nextjs-rendering-static-params-02
  answer: |
    `generateStaticParams` returns an array of parameter objects to prerender for a dynamic route such as `app/blog/[slug]` at build time. The Pages Router equivalent is `getStaticPaths` (with `getStaticProps`).

- id: nextjs-rendering-dynamic-apis-03
  answer: |
    In Next.js 15+, `cookies()`, `headers()`, `params`, and `searchParams` are asynchronous, so you must `await` them; this supports improved rendering/data access and PPR. Reading cookies/headers makes a route dynamic; `params` can still be statically generated for known values, while `searchParams` is request-specific and typically makes the page dynamic.

- id: nextjs-server-actions-useserver-01
  answer: |
    `"use server"` marks an async function or file as server-only callable code, often a Server Action, executed on the server. Unlike `"use client"`, which establishes a client-rendering boundary for interactive UI, it keeps code off the client and exposes a server endpoint rather than a component.

- id: nextjs-server-actions-mutation-02
  answer: |
    Define an async function with `"use server"`, validate/authenticate, mutate the database, call `revalidatePath`/`revalidateTag`, optionally `redirect`, and trigger it from `<form action={action}>` or a client onClick. React handles the transition; use `useActionState`/`useFormStatus` for pending UI, and server components re-render with updated data.

- id: nextjs-server-actions-security-03
  answer: |
    Server Actions are publicly invokeable HTTP endpoints even if only used internally, so treating them like local trusted calls is a security trap. Every action must independently validate and authorize input, check authentication/ownership, and enforce business rules before mutating data.

- id: nextjs-route-handlers-basics-01
  answer: |
    A `route.ts` file exports HTTP method functions (`GET`, `POST`, etc.) that return `Response`/`NextResponse` from a server endpoint; it replaces the Pages Router's `pages/api` routes. It cannot coexist with a page in the same segment and is used for APIs, webhooks, uploads, proxies, or streaming.

- id: nextjs-route-handlers-caching-02
  answer: |
    Version matters: in Next.js 15+, `GET` Route Handlers are not cached by default. In earlier versions, a static handler could be cached when it did not use dynamic APIs. To opt into caching, keep it static and export `revalidate` and/or `dynamic = 'force-static'`.

- id: nextjs-route-handlers-methods-03
  answer: |
    Read the body with `await request.json()`, `await request.formData()`, or `await request.text()`. Read query params via `request.nextUrl.searchParams` (or `new URL(request.url).searchParams`). Read dynamic params from the second argument `context.params` (await it in Next.js 15).

- id: nextjs-streaming-ssr-01
  answer: |
    Streaming SSR renders the shell immediately and streams additional HTML/RSC chunks as they become ready, using Suspense boundaries. It improves time-to-first-byte and perceived performance, lets layouts render before slow data, and allows independent sections to arrive progressively.

- id: nextjs-streaming-suspense-02
  answer: |
    Wrap the slow component in its own `<Suspense fallback={...}>` while keeping the fast UI outside it. The page shell and fast content stream immediately, and the slow component's promise no longer blocks first render.

- id: nextjs-streaming-boundary-03
  answer: |
    `loading.tsx` creates a coarse boundary around an entire route segment and swaps that segment/fallback whole. A custom `<Suspense>` boundary is more granular and can wrap just a subtree within a page, leaving the rest renderable immediately; multiple custom boundaries can load independent sections separately.

- id: nextjs-metadata-static-01
  answer: |
    Export a static `metadata` object from `layout.tsx` or `page.tsx` (or `generateMetadata`) in the App Router. Next.js merges it into document head tags such as title, description, and Open Graph. Root layout metadata often provides defaults.

- id: nextjs-metadata-dynamic-02
  answer: |
    Export an async `generateMetadata({ params, searchParams })` from the page/layout and fetch the needed data inside it, returning an object with dynamic title/description. Await `params` in Next.js 15, and reuse fetch/cache behavior to avoid duplicate work.

- id: nextjs-metadata-inherit-03
  answer: |
    Metadata composes from root to leaf: nested segments merge with parent metadata, and child values override parent values for the same field. `title.template` in a parent formats child page titles (e.g. `%s | Site`) unless a child supplies an absolute title.

- id: nextjs-metadata-files-04
  answer: |
    Next.js supports file conventions in `app/` such as `favicon.ico`, `icon.*`, `apple-icon.*`, `opengraph-image.*`, `twitter-image.*`, plus `sitemap.ts`/`sitemap.xml`, `robots.ts`/`robots.txt`, and `manifest.ts`/`manifest.webmanifest`. It automatically generates related tags and serves/renders the corresponding files.

- id: nextjs-navigation-link-01
  answer: |
    Use `<Link href="...">` from `next/link`. It performs client-side navigation and prefetches route segments in the viewport, avoiding a full reload and preserving client state. A plain `<a>` causes a full browser navigation/reload.

- id: nextjs-navigation-hooks-02
  answer: |
    App Router navigation hooks come from `next/navigation` (`useRouter`, `usePathname`, `useSearchParams`, `useParams`). They depend on client routing, so they must be used in Client Components and fail in Server Components; Server Components should receive route values via props instead.

- id: nextjs-navigation-redirect-03
  answer: |
    `redirect(path)` triggers an immediate navigation, and `notFound()` renders the nearest not-found UI and 404. Both work by throwing internally, so they terminate the current action/render; avoid catching that throw accidentally, and ensure a not-found boundary exists. Code after `redirect()` does not run.

- id: nextjs-navigation-action-redirect-04
  answer: |
    In a Server Action, perform the mutation, revalidate affected caches, then call `redirect('/new-page')` from `next/navigation`. Watch out that redirect throws internally and halts further code, so revalidation must happen before it; in client flows, call `router.push` after the action resolves if redirect is not used server-side.
