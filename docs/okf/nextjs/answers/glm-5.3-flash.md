```yaml
- id: nextjs-app-router-conventions-01
  answer: |
    Folder names map 1:1 to URL path segments, e.g. app/blog/[slug]/page.tsx → /blog/:slug; nested folders nest URLs. Special files:
    - page.tsx: the public UI for a segment — without a page, no child segment is URL-addressable.
    - layout.tsx: wraps the segment's page and child segments, receives children and params, persists across navigations.
    - loading.tsx: instant loading fallback; Next wraps the segment in a Suspense boundary.
    - error.tsx: error UI boundary (must be a client component) for the segment's subtree.
    - route.ts: an API endpoint (Route Handler) using the Web Request/Response API; mutually exclusive with page.tsx in the same segment.
- id: nextjs-app-router-layout-02
  answer: |
    Nested layouts wrap deeper layouts/pages like nested React components. On navigation, layouts that are still "above" the changed segment are preserved: they don't re-render, their state and effects persist; only the changed subtree re-renders. template.tsx looks like layout.tsx (same props) but a new instance is mounted on every navigation — children remount, local state and DOM are reset, effects re-run. Use layout for persistent chrome (nav, sidebar), template for enter/exit animations or "reset on navigate" behavior.
- id: nextjs-app-router-error-03
  answer: |
    error.tsx must be a Client Component ("use client" at top) and export a default component receiving { error, reset } (reset re-renders the segment). It catches errors thrown during rendering/data fetching of the segment's children — i.e. its page and deeper segments. It does NOT catch: errors thrown in the same segment's layout.tsx (those bubble to the parent segment's error boundary), errors in itself, event handlers or other client-side async code (use try/catch). For errors in the root layout, you need app/global-error.tsx, which replaces the root html/body.
- id: nextjs-app-router-loading-04
  answer: |
    Mechanically, adding loading.tsx makes Next automatically wrap that segment's content in a <Suspense> boundary with your fallback UI. On navigation the fallback shows immediately while the async Server Component / RSC payload for the new segment loads, letting the server start rendering and stream the real content when ready — it's syntactic sugar for a route-level Suspense boundary.
- id: nextjs-server-components-default-01
  answer: |
    Everything in the App Router is a Server Component by default. "use client" marks the entry point of the client graph: that file and everything it imports (transitively) becomes a Client Component bundle — it's a boundary declaration, not a per-component switch. Server Components above it stay server-rendered; children passed into the client component can remain Server Components.
- id: nextjs-server-components-hooks-02
  answer: |
    Server Components run only on the server in the RSC runtime: they never hydrate, so there's no client-side React instance for useState/useEffect lifecycle, no DOM to attach onClick handlers to, and no window/document — those APIs only exist in the browser. The fix: extract the interactive/browser-dependent part into a separate file marked "use client" and compose: render the client component from the server component, passing serializable props or server-rendered children.
- id: nextjs-server-components-props-03
  answer: |
    Props must be serializable by the RSC payload: strings, numbers, booleans, null/undefined, plain objects/arrays, Date, Map, Set, BigInt, Promises, and React elements (children). NOT allowed: functions, class instances (beyond supported built-ins), symbols, DOM nodes. Idiomatic pattern for keeping a server-rendered subtree inside a client component: pass Server Components as the children prop (or any React element prop) of the Client Component — e.g. <ClientModal>{<ServerChart/>}</ClientModal> — so the subtree is rendered on the server and shipped through the client component.
- id: nextjs-data-fetching-rsc-01
  answer: |
    Fetch directly in async Server Components: export default async function Page() { const data = await fetch(...) / await db.query(...) }. There is no getServerSideProps/getStaticProps in the App Router — they're replaced by data fetching inside RSC, with static vs dynamic determined by caching behavior (cached fetches / generateStaticParams → static; dynamic APIs or uncached data → rendered per request). Client-side fetching is still possible but no longer necessary for most pages.
- id: nextjs-data-fetching-nogssp-02
  answer: |
    Don't await sequentially. Options: (1) Promise.all([...]) for all independent fetches in the page; (2) kick off the promises (module-level or at the top of the component) before awaiting, then await them in parallel; (3) the "preload" pattern — pass promise-returning functions down and unwrap with React's use() inside child components, each under its own Suspense boundary, so slow sections stream in without blocking the page. Also dedupes automatically: identical fetches within a render pass are memoized.
- id: nextjs-caching-fetch-default-01
  answer: |
    It's version-dependent and this is the key gotcha: in Next.js 13 and 14, fetch() in a Server Component WAS cached by default (equivalent to cache: 'force-cache'). In Next.js 15, fetch is NOT cached by default — you must opt in with { cache: 'force-cache' } or { next: { revalidate } }. (I believe 15's default applies going forward; I'm not certain of any later reversal.)
- id: nextjs-caching-layers-02
  answer: |
    Four layers: (1) Request Memoization — dedupes identical fetches within a single server render pass (per-request). (2) Data Cache — persists fetch results on the server across requests and deploys-independent, keyed by URL+options, controlled by cache/revalidate/tags. (3) Full Route Cache — caches the fully rendered static route (HTML + RSC payload) at build time, invalidated by revalidation/dynamic APIs. (4) Router Cache — the client-side, in-memory cache of RSC payloads for visited/prefetched routes during a session (this is why stale content appears after mutations unless you revalidate).
- id: nextjs-caching-revalidate-03
  answer: |
    revalidate sets a time-to-live for cached data: fetch(url, { next: { revalidate: 60 } }) or export const revalidate = 60 on the segment. Cached content is served for that period; after expiry the next request gets served stale content while Next regenerates it in the background (stale-while-revalidate, i.e. ISR). Fully static content is generated once at build time and served unchanged forever (until on-demand invalidation); ISR adds the time-based staleness window and background regeneration.
- id: nextjs-caching-ondemand-04
  answer: |
    revalidatePath('/path') and revalidateTag('tag') (both from next/cache) purge cached data on demand: they invalidate both the Data Cache and the Full Route Cache for the affected path/tag so the next request re-renders fresh. revalidateTag works with fetch(..., { next: { tags: ['posts'] } }) and invalidates every fetch/route tagged accordingly. You call them in Server Actions or Route Handlers (server-side contexts) right after the mutation.
- id: nextjs-caching-segment-config-05
  answer: |
    export const dynamic controls route-level rendering: 'auto' (default — static if possible), 'force-dynamic' (always render per request, skip the Full Route Cache), 'force-static', 'error' (force static; using dynamic APIs errors). export const revalidate sets the segment-wide ISR interval (or false to cache indefinitely). Set dynamic = 'force-dynamic' when the page must always reflect request-time state — e.g. personalized/user-specific content, reading uncached per-request data — and you don't want any static optimization.
- id: nextjs-rendering-static-dynamic-01
  answer: |
    By default Next statically renders a route if nothing forces dynamic behavior: no dynamic APIs (cookies, headers, searchParams — and params used in certain ways), no uncached data fetches, and (for dynamic routes) params covered by generateStaticParams. In that case it prerenders HTML+RSC payload at build. If the route reads dynamic APIs, uses uncached fetches, or sets force-dynamic, it's rendered per request. In newer versions with PPR (experimental), it can split: static shell prerendered, dynamic holes streamed.
- id: nextjs-rendering-static-params-02
  answer: |
    export async function generateStaticParams() on app/blog/[slug]/page.tsx returns an array of param objects (e.g. [{ slug: 'hello' }, { slug: 'world' }]); Next prerenders each of those paths at build time (each entry can also contain nested params for child segments; values not returned can be rendered on demand unless dynamicParams = false). It's the App Router replacement for the Pages Router's getStaticPaths (paired with getStaticProps there).
- id: nextjs-rendering-dynamic-apis-03
  answer: |
    In Next 15 cookies() and headers() return Promises (async request APIs) to support streaming/PPR and future async request handling, and params/searchParams became Promises too — so you must await them (or unwrap with use() in client components). Reading them opts the route (or from PPR's perspective, the affected subtree) into dynamic rendering: the content can no longer be fully prerendered, so you trade static caching for per-request rendering.
- id: nextjs-server-actions-useserver-01
  answer: |
    "use server" marks functions (or, at file top, all async exports of a module) as Server Actions: server-only async functions exposed as callable RPC endpoints that client code can invoke. It does NOT affect rendering or make components client/server — unlike "use client", which draws the bundle boundary making that file and its imports client-side. Server Actions can even be defined inline in client component files; the function body still runs only on the server, and it must be async.
- id: nextjs-server-actions-mutation-02
  answer: |
    1) Define an async function marked "use server" (e.g. actions.ts) taking FormData. 2) Wire it to a form: <form action={createPost}> — no onSubmit/handler code needed on the client. 3) Inside the action: validate/authenticate (FormData fields are untrusted input), perform the DB write. 4) Refresh the UI: call revalidatePath('/posts') or revalidateTag('posts') so cached routes refetch, then optionally redirect(). 5) For stateful UX use useActionState (Next 15; formerly useFormState) to return errors/messages from the action, and useFormStatus for pending/disabled button states while the action runs.
- id: nextjs-server-actions-security-03
  answer: |
    Each Server Action is a public HTTP POST endpoint with a stable ID; anyone can POST to it directly with arbitrary arguments — the "function call" is an illusion. Treat every action like an unauthenticated API route: verify authentication AND authorization inside every action (never trust hidden form fields, client-held IDs, or "the page wouldn't render otherwise"), validate all inputs with a schema (e.g. zod), and avoid returning sensitive data. Also note args come from the client, so re-derive permissions from the session server-side.
- id: nextjs-route-handlers-basics-01
  answer: |
    A Route Handler is an endpoint defined in route.ts (or .js) inside the app directory, exporting functions named after HTTP methods (export async function GET(request) {}, POST, PUT, DELETE, etc.) using the Web standard Request/Response APIs. They replace the Pages Router's pages/api/* files (API Routes). Unlike API routes, they can also serve as cached/static responses, and one route.ts per segment (can't coexist with page.tsx there).
- id: nextjs-route-handlers-caching-02
  answer: |
    Version-specific: in Next 13/14, GET Route Handlers were cached by default when they didn't use dynamic functions (cookies/headers) — same default as fetch. In Next 15 this changed: GET Route Handlers are NOT cached by default. To opt in, use route segment config: export const dynamic = 'force-static', or export const revalidate = N, or rely on cached fetches inside; dynamic methods (POST etc.) are never cached.
- id: nextjs-route-handlers-methods-03
  answer: |
    Body: await request.json() or await request.formData() (Web Request API). Query params: request.nextUrl.searchParams (or new URL(request.url).searchParams) — .get('q'). Dynamic params: the second handler argument — export async function GET(request, { params }) — and in Next 15 params is a Promise, so const { slug } = await params. (In Next 14 and earlier params was a plain object.)
- id: nextjs-streaming-ssr-01
  answer: |
    Streaming SSR sends the HTML/RSC payload progressively instead of buffering the entire response: the shell (layout, fast parts) is sent immediately, and async sections are streamed in as their data resolves, with fallbacks replaced in place via Suspense. Benefits over render-then-flush: much faster TTFB/FCP (user sees the page before the slowest query finishes), no all-or-nothing wait on the slowest data source, better perceived performance and loading UX, while the server still does a single coordinated render.
- id: nextjs-streaming-suspense-02
  answer: |
    Move the slow fetch into its own component and wrap it in <Suspense fallback={<SlowSkeleton/>}> — crucially the await must happen inside that suspended component, not in the parent page, otherwise the whole page still blocks. The page shell renders immediately; the slow section streams in when its data resolves. (Alternatively give the whole segment a loading.tsx, or use the use()-based preload pattern.)
- id: nextjs-streaming-boundary-03
  answer: |
    loading.tsx is automatic, segment-level sugar: Next wraps the whole segment's page/children in one Suspense boundary with your fallback, and it also applies during navigations (Router Cache can show it instantly). Your own <Suspense> gives manual, granular control inside a page: multiple boundaries at different nesting levels, custom placement around just one slow component, different fallbacks. loading.tsx = one coarse boundary per route segment; Suspense = as many fine-grained boundaries as you design.
- id: nextjs-metadata-static-01
  answer: |
    Export a metadata object from a Server Component layout.tsx or page.tsx: export const metadata: Metadata = { title: 'About', description: '...' } (from the 'next' type import). Next merges it into <head>. Only works in Server Components — client components can't export metadata. For simple cases there's also the icon/favicon file conventions.
- id: nextjs-metadata-dynamic-02
  answer: |
    Export async function generateMetadata({ params }) from the page: await params (Next 15), fetch the data (e.g. the post by slug), and return a Metadata object ({ title: post.title, description: post.summary }). Identical fetch requests are memoized across generateMetadata and the page, so you don't fetch twice. Use generateMetadata instead of static metadata whenever metadata depends on dynamic data.
- id: nextjs-metadata-inherit-03
  answer: |
    Metadata is evaluated per route and composed shallowly from the segment chain: each segment's metadata/generateMetadata is merged with its parents, and a child's fields override the parent's same fields; fields the child doesn't set are inherited. title.template on a parent layout (e.g. { title: { template: '%s | Acme', default: 'Acme' } }) lets child pages export plain titles ('About') and get them expanded ('About | Acme'); title.absolute bypasses the template.
- id: nextjs-metadata-files-04
  answer: |
    File conventions placed in app (per segment): favicon.ico, icon.png / apple-icon.png (multiple sizes via .+size suffixes), opengraph-image.png/jpg/gif or opengraph-image.tsx (dynamically generated OG images using ImageResponse from next/og), twitter-image.*, sitemap.ts|js|xml, robots.ts|js|txt, manifest.ts. Next wires them into <head> / the appropriate URLs automatically, scoped to the segment they're in.
- id: nextjs-navigation-link-01
  answer: |
    Use <Link href="/path"> from next/link. Unlike a plain <a> (which forces a full document load), Link performs client-side navigation: it fetches only the changed RSC payload, preserves layout/state across the tree, scrolls to top (restoring scroll on back), and prefetches route payloads for links in the viewport (in production builds) into the Router Cache, making subsequent navigation near-instant. You can add onClick for side effects but navigation itself is declarative via href.
- id: nextjs-navigation-hooks-02
  answer: |
    App Router navigation hooks come from next/navigation (NOT next/router, which is the Pages Router): useRouter, usePathname, useSearchParams (also useParams). They fail in Server Components because they're client-side React hooks — they depend on the client React runtime, context and re-render lifecycle that Server Components don't have. Use them in a "use client" component; in server code use props/params and the async dynamic APIs (cookies/headers) instead.
- id: nextjs-navigation-redirect-03
  answer: |
    redirect(url) (from next/navigation) aborts rendering and navigates; notFound() aborts and renders the nearest not-found.tsx with a 404. Both work in Server Components, Route Handlers, and Server Actions. The big gotcha: they work by THROWING a special internal error (NEXT_REDIRECT / NEXT_NOT_FOUND) — so if you call them inside a try/catch, your catch will swallow them; call them outside try blocks or rethrow unrecognized errors. Another gotcha: redirect's status code differs by context (307/308 default, 303 in Server Actions).
- id: nextjs-navigation-action-redirect-04
  answer: |
    Call redirect(`/posts/${newId}`) from next/navigation inside the Server Action, after the write succeeds — the thrown redirect error is handled by Next and the client navigates. Watch out for: (1) don't wrap it in try/catch (it throws internally; either redirect outside the try or rethrow), (2) revalidate affected paths/tags BEFORE redirecting so the destination doesn't serve stale cache, (3) only redirect after the mutation actually committed (it aborts execution), and (4) in Server Actions redirect issues a 303 (see other/GET) so the browser follows up with a GET of the new page.
```
