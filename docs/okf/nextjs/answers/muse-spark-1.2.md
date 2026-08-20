<!-- Closed-book answers from opencode-go/muse-spark-1.2. Produced via an
     isolated `ocode2 run` subprocess given ONLY docs/okf/_prompts/nextjs.md
     (id+question, no answer key). Zero tool invocations in the raw log —
     pure LLM completion. See docs/okf/HOW-TO-EVALUATE.md Rule 0. -->

```yaml
- id: nextjs-app-router-conventions-01
  answer: |
    App Router is file-system routing inside `app/`:
    Folder path = URL path. `app/dashboard/settings/page.tsx` -> `/dashboard/settings`. `page.tsx` is required to make a segment routable; its default export is the UI for that URL. Folders without a `page`/`route` are just route groups/layout segments.

    Special files per segment:
    - `layout.tsx` - Shared UI wrapping `children`. Layouts persist across navigation, don't remount, keep state. Takes `{children}` + must render them. Can be nested; root `app/layout.tsx` is required and must contain `<html><body>`.
    - `page.tsx` - Leaf UI for the route. `export default function Page()`.
    - `loading.tsx` - Suspense fallback for the segment. Exported component is shown instantly while the segment's Server Components are loading/streaming. Automatically wraps `page.tsx` and children in `<Suspense>`.
    - `error.tsx` - Error boundary for the segment. Must be a Client Component (`'use client'`). Catches runtime errors in children of that segment, renders `({error, reset})`.
    - `route.ts`/`route.js` - Route Handler - API endpoint, not a page. Export HTTP verb functions `export async function GET(request, {params}) { return NextResponse.json(...) }`. Maps same folder path to API; `page` and `route` cannot coexist in same folder. Others: `not-found.tsx`, `template.tsx`, `default.tsx`.

- id: nextjs-app-router-layout-02
  answer: |
    Nested layouts compose: `app/layout.tsx` wraps `app/dashboard/layout.tsx` wraps `app/dashboard/page.tsx` via `children` prop. On client-side navigation within the same layout subtree, Next.js does soft navigation - the common layouts stay mounted, state and DOM preserved, only the changed segment re-renders/fetches.

    `template.tsx` has identical API to `layout.tsx` (`{children}`) but different behavior on navigation: template creates a new instance on every navigation, remounts, state is reset, `useEffect` re-runs, and enter/exit animations re-trigger. Choice: `layout` for persistent chrome (sidebar, navbar), `template` for analytics `useEffect` per navigation, focus-reset, or Framer Motion enter animations.

- id: nextjs-app-router-error-03
  answer: |
    `error.tsx` must be a Client Component: `'use client'` at top, `export default function Error({error, reset}: {error: Error & {digest?:string}, reset: () => void})`. It is a React Error Boundary.

    Catches: uncaught runtime/rendering errors thrown during rendering or in Server/Client Components inside that segment's subtree, including async errors in Server Components.

    Does NOT catch: errors thrown in the *same segment's* `layout.tsx` (it catches children, so need parent's `error.tsx` for layout errors), errors in event handlers (use try/catch), errors thrown in `root` layout, 404s (`notFound()` shows `not-found.tsx`, not `error.tsx`), or errors outside React rendering. To catch root errors use `app/global-error.tsx` which must include its own `<html><body>`.

- id: nextjs-app-router-loading-04
  answer: |
    Adding `app/.../loading.tsx` automatically wraps that segment's `page.tsx` (and nested children) in a `<Suspense fallback={<Loading/>}>` boundary.

    Mechanically: on initial load and on client navigation, Next.js streams the UI. The fallback is sent immediately (instant navigation), then when the Server Component's async work (fetch, DB) resolves, the real content streams in and replaces the fallback. It avoids blocking the whole app on slow data, enables streaming SSR. It's a file-convention shorthand for manual Suspense; you don't write Suspense yourself.

- id: nextjs-server-components-default-01
  answer: |
    In `app/` every component is a Server Component by default. No directive needed. They render only on the server, are never hydrated, can be `async`, can directly access DB/filesystem, and send only serialized RSC payload + HTML to client (zero JS by default).

    `"use client"` is a directive at the top of a file that marks a client boundary. That file and all its imports become Client Components: they are rendered on server for SSR *and* hydrated/executed in browser, can use state, effects, events, browser APIs. It does NOT mean "only client" - it's "also on client". You push it as low/leaves as possible to keep bundle small.

- id: nextjs-server-components-hooks-02
  answer: |
    Server Components run once on server to produce HTML/RSC, they have no React state lifecycle, no hydration, no browser environment. `useState`/`useEffect`/`useReducer`/`useRef` require client-side React runtime and re-render; `onClick` requires event listener hydration; `window`/`document`/`localStorage` don't exist on server.

    Fix: extract the interactive part into a Client Component:
    ```
    'use client'
    // Counter.tsx uses useState, onClick
    ```
    then import it into the Server Component. Server Component can still fetch data and pass it as props to the Client Component. Alternative: add `'use client'` to the whole file if entire component needs interactivity, but prefer leaf boundaries.

- id: nextjs-server-components-props-03
  answer: |
    Server -> Client props cross the RSC serialization boundary, so they must be serializable: primitives, plain objects/arrays, serialized Date, ReactNode (JSX), Promise. You cannot pass non-serializable values: functions/closures/event handlers, class instances, Symbols, or objects with prototype methods.

    Idiomatic way to keep a server-rendered subtree inside a client component without making it client: composition via `children` slot:
    ```tsx
    // ServerComponent.tsx (server)
    <ClientWrapper><ServerChild /></ClientWrapper>
    // ClientWrapper.tsx ('use client')
    export default function ClientWrapper({children}:{children:React.ReactNode}) {
      const [open,setOpen]=useState(false)
      return <div>{children}</div>
    }
    ```
    `children` is already rendered on server and passed as serializable ReactNode, so `ServerChild` stays server, zero JS, even though its parent is client.

- id: nextjs-data-fetching-rsc-01
  answer: |
    Fetch directly in Server Components - make the component `async`:
    ```tsx
    export default async function Page() {
      const res = await fetch('https://.../posts', {next:{revalidate:60}})
      const data = await res.json()
      // or await db.query(...)
      return <List data={data}/>
    }
    ```
    No `getServerSideProps`/`getStaticProps`/`getInitialProps` in `app/`. Those are Pages Router only. App Router replaces them with: async Server Components + `fetch` options (`cache`, `next.revalidate`, `next.tags`) + Route Segment Config (`export const dynamic`, `revalidate`, `fetchCache`) + `generateStaticParams`. Fetch on server supports streaming/Suspense natively.

- id: nextjs-data-fetching-nogssp-02
  answer: |
    Avoid sequential awaits = waterfall.
    Bad:
    ```ts
    const a = await fetchA()
    const b = await fetchB() // waits for a
    ```
    Good: fire in parallel then await:
    ```ts
    const aPromise = fetchA()
    const bPromise = fetchB()
    const [a,b] = await Promise.all([aPromise,bPromise])
    ```
    or `await Promise.all([fetch(url1), fetch(url2)])`. For components, do parallel fetches at same level, or wrap each independent slow component in its own `<Suspense>` so they stream in parallel. Next also memoizes `fetch` with same URL in one render so duplicate calls are deduped.

- id: nextjs-caching-fetch-default-01
  answer: |
    Version-specific:
    - Next.js 13-14: `await fetch(url)` in a Server Component WAS cached by default - `cache: 'force-cache'` / indefinite, like `getStaticProps`. Data cached in Data Cache and Full Route Cache until `revalidate` or `revalidateTag/Path`. Had to opt OUT with `fetch(url,{cache:'no-store'})` or `{next:{revalidate:0}}`.
    - Next.js 15+: changed default to UNCACHED - `cache: 'no-store'` / `fetch` is dynamic by default. No caching unless you opt IN with `fetch(url,{cache:'force-cache'})` or `fetch(url,{next:{revalidate:60}})` or `next.tags`.

    If unsure, be explicit: always set `cache` or `next.revalidate`.

- id: nextjs-caching-layers-02
  answer: |
    Four layers:
    1. Request Memoization - per-request, per-render dedup: same `fetch` URL/options in one render only fetches once (React cache, memory).
    2. Data Cache - persistent server-side cache of `fetch` results (and `unstable_cache`). Survives requests, shared across users, lives until revalidated/expired. Controlled by `fetch` `cache`/`revalidate`/`tags`.
    3. Full Route Cache (Static Render) - HTML + RSC payload of statically rendered routes at build time (or after ISR). Served until revalidated.
    4. Router Cache - client-side in-memory cache (in browser) of RSC payload for visited/prefetched routes, used for instant soft navigation and prefetch via `<Link prefetch>`. Separate from server caches, cleared on hard reload.

- id: nextjs-caching-revalidate-03
  answer: |
    `revalidate` sets Time-To-Live for Data Cache / Full Route Cache. e.g. `fetch(url,{next:{revalidate:60}})` or `export const revalidate = 60`. After 60s, data is stale; next request triggers background regeneration (stale-while-revalidate) - stale data served immediately while fresh data re-fetched/ re-rendered.

    Difference from fully static: `revalidate = false` / no revalidate = never expires, built once, never updates until redeploy. Time-based ISR = static for speed (served from cache) but periodically refreshed, so content stays semi-fresh without building every request. Fully dynamic (`revalidate:0` / `dynamic='force-dynamic'`) renders on every request.

- id: nextjs-caching-ondemand-04
  answer: |
    For immediate updates after a mutation, use on-demand revalidation:

    - `revalidatePath(path, type?)` - purges Data Cache + Full Route Cache for that route path: `revalidatePath('/blog')` or `revalidatePath('/blog/[slug]','page')`.
    - `revalidateTag(tag)` - purges all `fetch` entries tagged with that tag: `fetch(url,{next:{tags:['posts']}})` then `revalidateTag('posts')` invalidates all.

    Where: only on server - inside Server Actions or Route Handlers (after DB write), not in Client Components. Call after mutation before redirect. Requires that data was fetched with tags or is associated with path.

- id: nextjs-caching-segment-config-05
  answer: |
    Route Segment Config exports in `page.tsx`/`layout.tsx`/`route.ts`:
    - `export const dynamic = 'auto' | 'force-dynamic' | 'force-static' | 'error'` - controls static vs dynamic rendering. `auto` (default) lets Next decide, `force-dynamic` forces dynamic (SSR every request, no Full Route Cache), `force-static` forces static (errors if dynamic APIs used).
    - `export const revalidate = number | false` - sets default ISR revalidation time for the segment (seconds, `false` = infinite). Overrides fetch defaults.

    Other configs: `fetchCache`, `runtime`, `preferredRegion`.

    Use `dynamic = 'force-dynamic'` when route must always be fresh and never cached: personalized dashboards, authenticated pages using `cookies()`/`headers()`, or when using request-time data that cannot be cached.

- id: nextjs-rendering-static-dynamic-01
  answer: |
    Next decides per-route:

    Static (prerendered at build): route has no dynamic APIs, no `fetch` with `no-store`, no `dynamic='force-dynamic'`, no `cookies()`/`headers()`/`searchParams`/`params` read. Eligible for Data Cache + Full Route Cache.

    Dynamic (rendered per request): route uses any dynamic API (`cookies()`, `headers()`, `unstable_noStore()`, `searchParams`, `params` in Next 15 async form), or `fetch` with `cache:'no-store'` / `revalidate:0`, or `export const dynamic='force-dynamic'`, or Route Handler uses `request`.

    Hybrid: static shell can stream dynamic parts inside `<Suspense>` (partial prerender in Next 14+).

- id: nextjs-rendering-static-params-02
  answer: |
    `export async function generateStaticParams()` in `app/blog/[slug]/page.tsx` tells Next which dynamic route params to prerender statically at build time.

    ```ts
    export async function generateStaticParams() {
      const posts = await getPosts()
      return posts.map(p=>({slug:p.slug}))
    }
    // renders /blog/a, /blog/b at build
    ```

    Pages Router equivalent: `getStaticPaths` (with `fallback` handling via `dynamicParams`). In App Router, `export const dynamicParams = true/false` controls whether params not returned by `generateStaticParams` are rendered on-demand (`true`) or 404 (`false`).

- id: nextjs-rendering-dynamic-apis-03
  answer: |
    In Next 15+, `cookies()`, `headers()`, `params` and `searchParams` are async - they return Promises, so you must `await`:

    ```ts
    export default async function Page({params, searchParams}) {
      const {slug} = await params
      const {q} = await searchParams
      const cookieStore = await cookies()
    }
    ```

    Reason: Next is moving to async Dynamic APIs to allow the rendering to know at the right time that the route is dynamic and enable partial prerender / better performance tracking.

    Effect of reading them: opts the route (or the suspense boundary that reads them) into dynamic rendering - it cannot be fully statically prerendered, Full Route Cache is bypassed, route renders per request. Reading `cookies()`/`headers()` makes the whole route dynamic; `params`/`searchParams` awaiting makes that page dynamic.

- id: nextjs-server-actions-useserver-01
  answer: |
    `"use server"` marks a Server Action - code guaranteed to run only on server and exposed as an RPC endpoint callable from client/server.

    Placement: 1) top of file - all exports become Server Actions, file is server-only; 2) top of a function - inline Server Action inside Server or Client Component: `async function create(){'use server'; ...}`.

    Difference from `"use client"`:
    - `"use client"` = client boundary, code runs in browser (and server for SSR), can use hooks/DOM.
    - `"use server"` = server boundary, code runs ONLY on server, can access DB/secrets, never shipped to browser, callable via `action`/`formAction` or JS RPC. They are complementary, not opposites; a Client Component can import/call a Server Action.

- id: nextjs-server-actions-mutation-02
  answer: |
    1. Define action:
    ```ts
    // actions.ts
    'use server'
    import {revalidatePath, redirect} from 'next/cache' // or next/navigation for redirect
    export async function createPost(prevState, formData:FormData) {
      const title = formData.get('title')
      // validate
      await db.post.create({title})
      revalidatePath('/posts')
      redirect('/posts')
    }
    ```
    2. Wire in form:
    ```tsx
    // page.tsx (server or client)
    <form action={createPost}>
      <input name="title"/>
      <button type="submit">Create</button>
    </form>
    ```
    Or in Client Component with `useActionState` for pending/errors:
    ```tsx
    'use client'
    const [state, formAction, isPending] = useActionState(createPost, null)
    return <form action={formAction}>...
    ```

    On submit, Next POSTs to Server Action endpoint (progressively enhanced - works without JS), runs on server, writes DB, revalidates cache, then redirects or returns state to update UI. No manual `fetch` needed.

- id: nextjs-server-actions-security-03
  answer: |
    It looks like a direct function call from client, but it's actually a public HTTP POST endpoint anyone can invoke with arbitrary arguments, outside your UI flow, bypassing client-side checks.

    Trap: trusting caller, forgetting auth/zod validation, leaking secrets via closure.

    Must do inside EVERY action: authenticate (`await auth()` / check session), authorize (does user own resource/permitted role), validate/sanitize all inputs server-side (e.g. `zod` parse `FormData`), handle errors, and never expose sensitive env vars or DB handles via closure. Assume attacker can call action directly with `curl` with any payload. Also add rate limiting/CSRF is handled by Next but don't rely on client checks alone.

- id: nextjs-route-handlers-basics-01
  answer: |
    Route Handler is `app/api/**/route.ts` (or any `app/*/route.ts`) that defines HTTP handlers:

    ```ts
    import {NextResponse} from 'next/server'
    export async function GET(request:Request, {params}) { return NextResponse.json({hello:1}) }
    export async function POST(request:Request) { ... }
    ```

    It handles `GET`/`POST`/`PUT`/`PATCH`/`DELETE`/`HEAD`/`OPTIONS`. Returns `Response`/`NextResponse`. Replaces Pages Router `pages/api/*.ts` (`export default function handler(req,res)`). Unlike pages/api, Route Handlers live alongside UI in `app/`, use Web Request/Response APIs, and respect Route Segment Config for caching.

- id: nextjs-route-handlers-caching-02
  answer: |
    Version-specific:
    - Next 13-14: `GET` Route Handlers were cached by default (treated as static `force-cache`) if they didn't use dynamic APIs. To prevent caching you had to `export const dynamic='force-dynamic'` or `export const revalidate=0` or read `cookies()`/`request`.
    - Next 15+: aligns with fetch change - `GET` is NOT cached by default (dynamic), you opt IN to caching with `export const dynamic='force-static'` or `export const revalidate=60` or `export const fetchCache`.

    To opt into caching explicitly: `export const dynamic = 'force-static'` + `export const revalidate = 3600`. To opt out / force dynamic: `export const dynamic='force-dynamic'` or `export const revalidate=0` or use `request`/`cookies()`.

    Non-GET methods never cached.

- id: nextjs-route-handlers-methods-03
  answer: |
    Handler signature: `export async function POST(request: Request, {params}: {params: Promise<{id:string}>})`

    - Body: `await request.json()`, `await request.formData()`, `await request.text()`, `await request.arrayBuffer()`. Use `request.headers`.
    - Query params: `new URL(request.url).searchParams.get('q')` or `request.nextUrl.searchParams` if using `NextRequest` from `next/server`.
    - Dynamic route params: second argument `context.params`. In Next 15+ it's a Promise: `const {id} = await params` for `app/api/users/[id]/route.ts`. For `app/shop/[slug]/route.ts` -> `params.slug`.
    Use `NextRequest` type for helpers: `import {NextRequest} from 'next/server'`.

- id: nextjs-streaming-ssr-01
  answer: |
    Streaming SSR renders the Server Component tree on server and streams chunks (HTML + RSC payload) to client as soon as each `<Suspense>` boundary resolves, instead of waiting for all data then sending one HTML string.

    Benefit vs blocking render: better TTFB and FCP - shell arrives instantly, progressive rendering; slow data doesn't block fast shell; browser can start parsing/painting earlier; less waterfall; improved perceived performance and Core Web Vitals. Enabled automatically via Suspense/`loading.tsx`.

- id: nextjs-streaming-suspense-02
  answer: |
    Keep page as Server Component but isolate slow part in its own async component wrapped in Suspense:

    ```tsx
    import {Suspense} from 'react'
    export default function Page() {
      return <>
        <FastHeader/>
        <Suspense fallback={<SlowSkeleton/>}>
          <SlowData /> {/* async component that awaits slow fetch */}
        </Suspense>
      </>
    }
    async function SlowData() {
      const data = await fetchSlow()
      return <div>{data}</div>
    }
    ```

    Fast page streams immediately, slow section shows skeleton then streams in. Don't await slow fetch at page top level - let it be suspended inside child. `loading.tsx` would block whole page, Suspense is granular.

- id: nextjs-streaming-boundary-03
  answer: |
    - `loading.tsx` is a file convention that creates an automatic `<Suspense>` boundary around the entire segment (`page.tsx` + children). Coarse, one per folder, triggered on navigation, shows for any async in that segment.
    - Manual `<Suspense>` is explicit, granular: you place it around specific components only, can have multiple boundaries with different fallbacks, can wrap a single slow component while rest of page stays interactive. Manual Suspense doesn't create a route-level loading state, just for that subtree.

    Use `loading.tsx` for simple route-level skeleton; use manual Suspense for partial rendering / to avoid blocking fast content with one slow component.

- id: nextjs-metadata-static-01
  answer: |
    In `app/` export a `metadata` object (or `generateMetadata`) from `layout.tsx`/`page.tsx`:

    ```ts
    import type {Metadata} from 'next'
    export const metadata: Metadata = {
      title: 'My Page',
      description: '...',
      openGraph: {title: '...', images:['/og.png']},
      robots: 'index, follow',
    }
    ```

    Next renders appropriate `<head>` tags automatically. No need to write `<Head>` component (Pages Router) or manual `<title>`. Works in Server Components only.

- id: nextjs-metadata-dynamic-02
  answer: |
    Export async `generateMetadata` that receives `params`/`searchParams` and can fetch:

    ```ts
    export async function generateMetadata({params}:Props): Promise<Metadata> {
      const {slug} = await params // Next 15
      const post = await fetchPost(slug)
      return {
        title: post.title,
        description: post.excerpt,
        openGraph: {images:[post.image]},
      }
    }
    ```

    It runs on server before rendering, can be async, deduped with page fetch via request memoization. Use in `page.tsx` or `layout.tsx` for data-dependent titles.

- id: nextjs-metadata-inherit-03
  answer: |
    Metadata merges/composes across nested layouts/pages - child metadata overrides / extends parent. Deduplication by field.

    `title` has special handling:
    ```ts
    export const metadata = {
      title: { default: 'Site', template: '%s | Site' }
    }
    // in child page: title: 'Blog' -> renders 'Blog | Site'
    ```
    `template` is defined in parent layout, `%s` replaced by child's `title`. If child has no title, `default` is used. Other fields shallow merge; `openGraph` etc replace. `metadataBase` helps resolve relative URLs.

- id: nextjs-metadata-files-04
  answer: |
    File-based Metadata via convention in `app/`:

    - Favicon/App Icon: `app/favicon.ico`, `app/icon.png` / `icon.tsx`, `apple-icon.png`
    - Open Graph/Twitter image: `app/opengraph-image.tsx` or `opengraph-image.png`, `twitter-image.tsx` (can be dynamic component that returns `ImageResponse`)
    - `app/sitemap.ts` / `sitemap.xml` -> generates `/sitemap.xml` (`export default function sitemap(): MetadataRoute.Sitemap`)
    - `app/robots.ts` / `robots.txt` -> `/robots.txt` (`MetadataRoute.Robots`)
    - `app/manifest.ts` -> `/manifest.webmanifest`

    Placing the file at a route segment makes it the metadata for that segment. No `metadata` export needed.

- id: nextjs-navigation-link-01
  answer: |
    Use `import Link from 'next/link'` and `<Link href="/about">About</Link>`. Programmatically `router.push('/about')` from `'next/navigation'`.

    `<Link>` vs plain `<a>`: `<Link>` does client-side soft navigation via App Router, prefetches target route in viewport (with Router Cache), preserves client state/layouts, no full page reload, faster transitions, and integrates with streaming/Suspense. Plain `<a>` triggers full browser navigation/reload, loses state, slower. Use `<a>` only for external URLs.

- id: nextjs-navigation-hooks-02
  answer: |
    App Router hooks come from `'next/navigation'` (not `'next/router'` which is Pages Router).

    Hooks: `useRouter()` (push/replace/refresh/back), `usePathname()`, `useSearchParams()`, `useParams()`.

    They fail in Server Components because they are React hooks requiring client context: they read browser history/router state and use `useState`/`useContext` internally. Server Components have no client state, no hydration, no hook runtime. Must be used in Client Components (`'use client'`).

- id: nextjs-navigation-redirect-03
  answer: |
    `import {redirect, notFound} from 'next/navigation'` (server-only).

    - `redirect('/path')` / `permanentRedirect()` throws a redirect response, navigates to new URL (307 temporary or 308 permanent), aborts current render.
    - `notFound()` throws to render nearest `not-found.tsx` (or root 404), sets 404 status.

    Gotcha: they work by throwing an exception, not returning. Code after them is unreachable - don't put in try/catch that swallows it, and ensure you `return`/`throw` handling: `if (!user) notFound()` or `if (!user) redirect('/login')`. They only work in Server Components, Server Actions, Route Handlers - not in Client Component event handlers (use `router.push` there). Also they must be called during render/action, not in `useEffect`.

- id: nextjs-navigation-action-redirect-04
  answer: |
    Inside Server Action after creating record:

    ```ts
    'use server'
    import {redirect} from 'next/navigation'
    export async function create(formData:FormData) {
      const {id} = await db.create(...)
      redirect(`/posts/${id}`) // throws, navigates
    }
    ```

    Used as `<form action={create}>`. Gotcha: `redirect()` throws - don't try to also `return` or `revalidatePath` after it (do revalidation before), don't wrap in try/catch that catches it. In Client Components with `useActionState`, you cannot call `redirect` and return state simultaneously; redirect ends action. If you need client handling, return `{id}` from action and do `router.push` in client. Note `redirect` inside action causes NEXT_REDIRECT error if caught.
```
