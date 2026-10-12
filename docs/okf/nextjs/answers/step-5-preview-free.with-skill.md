# step-5-preview-free — WITH-SKILL validation answers for nextjs (opencode-go, 2026-10-12); closed-book,
# derived/nextjs.step-5-preview-free.SKILL.md body prepended to _prompts/nextjs.md as active guidance.
- id: nextjs-app-router-conventions-01
  answer: |
    - `page.tsx` — the UI for a route segment and what makes that route publicly accessible; one per segment.
    - `layout.tsx` — shared wrapper for the segment and its children; persists across navigations and does not re-render (or lose state) when children change.
    - `loading.tsx` — loading UI; mechanically it wraps the segment's page in a Suspense boundary and enables streaming.
    - `error.tsx` — a Client Component Error Boundary for the segment; catches render errors in its children and exposes `reset()`.
    - `route.ts` — a Route Handler: an API endpoint for the segment exporting HTTP method functions; it renders no UI and does not participate in the layout tree.
    Folder mapping: a folder is a URL segment, nesting folders nests the path (`app/blog/[slug]` → `/blog/x`). Dynamic segments use brackets, `(group)` route groups and `@named` parallel slots are excluded... route groups are excluded from the URL, and `_private` folders are not routable. Only folders containing `page.tsx`/`route.ts` are addressable.
- id: nextjs-app-router-layout-02
  answer: |
    Nested layouts compose outer → inner. During a navigation, only the segment(s) whose contents change re-render; layouts above the changed segment keep their state (DOM state, scroll position, in-progress form inputs) and do not remount. That layout persistence is the main reason App Router shells feel fast.
    `template.tsx` behaves like a layout but a NEW instance is mounted for every navigation — state is reset, effects re-run, and lifecycle events fire per navigation. Use templates when you specifically need per-navigation behavior (enter/exit animations, logging, re-running an effect); use a layout for shared persistent UI. Both can coexist; the template renders inside the layout.
- id: nextjs-app-router-error-03
  answer: |
    It must be a Client Component (`"use client"`) and default-export a React component receiving `{ error, reset }`. It renders as the error boundary for its own segment's children.
    It catches runtime errors thrown during rendering of its page/nested children (client or server components). It does NOT catch: errors in the root layout (use `global-error.tsx`, which must render its own `<html>`/`<body>`); errors in an ancestor layout above this boundary (it protects children, not parents); errors thrown in event handlers or async effects (those aren't caught by React error boundaries at all); and errors thrown by the boundary itself. In production the real error message is redacted and you only get a `digest`.
- id: nextjs-app-router-loading-04
  answer: |
    It is sugar for wrapping the segment's page (and children) in `<Suspense fallback={<YourLoadingUI/>}>`. The server flushes the shell/layout immediately and streams the segment's HTML/RSC payload once rendering finishes. During navigation between sibling routes, the fallback shows in place of the not-yet-ready segment while the shared layout stays — that's the instant transition UI. It creates one route-level boundary covering the whole segment's page.
- id: nextjs-server-components-default-01
  answer: |
    All components in `app/` are React Server Components by default; they render only on the server and ship no component JS. `"use client"` marks a client-bundle boundary: the file it's placed in and its entire import graph are bundled for the browser and may use hooks, state, event handlers, and browser APIs. It is a boundary marker on a module, not a flag on a single "component" — a client component can still render server-rendered children passed as props.
- id: nextjs-server-components-hooks-02
  answer: |
    Server Components render exactly once on the server; they never hydrate or re-render on the client. With no client renderer there is no render/re-render cycle, so `useState`/`useEffect` have no meaning, no DOM exists to attach an `onClick` listener to, and `window`/`document`/`localStorage` don't exist in the server environment. The fix is to split the interactive part into a Client Component (`"use client"`), render it from the server component, and pass in serializable data.
- id: nextjs-server-components-props-03
  answer: |
    Props crossing the server → client boundary must be serializable: primitives, plain objects/arrays, `Date`, and React elements. Critically, a plain function (`onClick`, a callback, a class method) does NOT cross — handing an unmarked function to a Client Component is a runtime error, not an implicit conversion to an action. The only exceptions are functions marked `"use server"` (Server Actions), which travel as an action reference.
    The idiomatic way to keep a server-rendered subtree inside a client component is composition: pass the subtree as `children` (or an element prop), so it renders server-side and the client component just interpolates it, instead of making the client component re-render that data on the client.
- id: nextjs-data-fetching-rsc-01
  answer: |
    Fetch directly in the component that needs the data — Server Components are `async` and simply `await fetch(...)`, or query your ORM/database directly server-side (no need to route everything through an API route). `getServerSideProps`/`getStaticProps` no longer exist; fetch behavior per request/static is controlled by fetch cache options and route segment config. `getStaticPaths` is replaced by `generateStaticParams`.
- id: nextjs-data-fetching-nogssp-02
  answer: |
    Two tools, not one. (1) `Promise.all([fetchA, fetchB, ...])` — start every promise before awaiting, when one component needs all the data before it renders. (2) For genuinely independent sources, the stronger pattern: move each slow fetch into its own `async` Server Component and wrap each in its own `<Suspense fallback>` so they stream in parallel — neither blocks the other or the page shell. Always offer the Suspense-isolation option alongside `Promise.all`; don't stop at parallel awaits. Also remember `fetch` memoization only dedupes identical requests, it does not parallelize sequential awaits.
- id: nextjs-caching-fetch-default-01
  answer: |
    Version-specific: In Next.js 13 and 14, `fetch(url)` was cached by default in Server Components (Data Cache, uncached only with `cache: 'no-store'`). In Next.js 15+, the default changed: `fetch` is NOT cached by default — it behaves like `no-store`; you opt in with `fetch(url, { cache: 'force-cache' })` (or route segment config `force-static`). So "cached by default" is only true for the older major versions.
- id: nextjs-caching-layers-02
  answer: |
    1. **Request Memoization** — React-level dedupe of `fetch` calls with the same URL + options within a single render pass (one request = one round-trip); lifetime is one render.
    2. **Data Cache** — server-side, persistent cache of fetch results across requests and deployments until revalidated.
    3. **Full Route Cache** — server-side cache of the rendered HTML/RSC payload for statically rendered routes; invalidated by revalidation or redeploy.
    4. **Router Cache** — client-side in-memory cache of visited segments' RSC payloads used for soft navigations.
- id: nextjs-caching-revalidate-03
  answer: |
    `revalidate = N` sets a time-based freshness window in seconds: within it the cached response is served; after it expires, the next request serves the stale entry while Next regenerates it in the background (stale-while-revalidate style), so subsequent requests get the fresh build. That is ISR: content that is mostly static but can change after deployment without a rebuild. Fully static content is generated once at build and served byte-identical until the next deploy — no regeneration window at all.
- id: nextjs-caching-ondemand-04
  answer: |
    `revalidatePath(path)` purges the cached content for a route path; `revalidateTag(tag)` purges every Data Cache entry fetched with `next: { tags: [tag] }`. Call them from server code after a mutation — inside a Server Action or Route Handler — so the next request re-renders with fresh data (e.g. `revalidatePath('/posts')` after creating a post). They mark entries stale; they do not themselves re-render synchronously.
- id: nextjs-caching-segment-config-05
  answer: |
    `export const dynamic` controls rendering mode: `'auto'` (default — static unless a dynamic API is used), `'force-static'`, `'force-dynamic'`, and `'error'` (build error if dynamic APIs appear). `export const revalidate = N` sets the segment's default ISR window. Set `dynamic = 'force-dynamic'` when content must be computed per request: personalized/user-specific pages, data that must always be fresh, or when you're intentionally reading request-time information and don't want build-time prerendering. It opts the route out of static optimization and the Full Route Cache.
- id: nextjs-rendering-static-dynamic-01
  answer: |
    Next attempts to prerender a route at build time. If rendering needs request-time information — `cookies()`, `headers()`, `searchParams`, `connection()`, `dynamic = 'force-dynamic'`, a `no-store`/uncached fetch — the route is switched to dynamic rendering (per request, added to the "dynamic" bucket). If everything resolves from cached/static data, the route is static and its payload goes into the Full Route Cache. The decision is made per segment while attempting the render, and any access to a dynamic API opts the route into dynamic rendering.
- id: nextjs-rendering-static-params-02
  answer: |
    `generateStaticParams()` returns the param combinations to prerender (e.g. `[{slug:'a'},{slug:'b'}]` for `app/blog/[slug]`), so those pages exist at build time; their data can come from cached fetches inside the page. Params not listed are still renderable on demand at request time by default (`dynamicParams: true`); with `dynamicParams: false` unknown slugs return a 404. Pages Router equivalent: `getStaticPaths` (paired with `getStaticProps` for the data).
- id: nextjs-rendering-dynamic-apis-03
  answer: |
    Next.js 15 made `cookies()`, `headers()`, `draftMode()` (and `params`/`searchParams` props on pages, layouts, and route handlers) async — they return Promises and must be awaited (`const slug = (await params).slug`). This is part of aligning with React's move toward request APIs being explicitly request-scoped, and reading one is a signal that the route depends on live request information, so the route becomes dynamically rendered (per-request) and can't be statically generated. In Next.js 14 and below these APIs were synchronous.
- id: nextjs-server-actions-useserver-01
  answer: |
    `"use server"` marks a **Server Action**: an async function that executes on the server but is callable from Client Components and forms. It can sit on top of a single async function body (an inline action, including one defined inside a Server Component and passed as a prop) or on top of a module to make every export an action. It compiles the function into a server endpoint plus an action reference; arguments and return values must be serializable.
    `"use client"` is the opposite-direction boundary: it pushes the module (and its imports) to the browser. Both are boundary marking directives, but `"use server"` sends a function to the server; `"use client"` sends code to the client.
- id: nextjs-server-actions-mutation-02
  answer: |
    1. Define an async Server Action (`"use server"`) that validates input, writes to the database, then calls `revalidatePath`/`revalidateTag` and `redirect(path)` on success.
    2. Wire it to `<form action={createPost}>` — with progressive enhancement, submission works even before JS hydrates.
    3. For pending UI and error state in a Client Component: `useFormStatus` (in a child of the form) gives `pending`, or `useActionState(action, initialState)` gives `[state, formAction]` for returning validation errors back into the form.
    4. Update the UI: revalidation refreshes the cached route data automatically, and `redirect()` navigates the user to the new page once the action completes.
- id: nextjs-server-actions-security-03
  answer: |
    A Server Action feels like a local call but is really a public HTTP endpoint: anyone can capture the action reference and POST arbitrary arguments to it — there is no authentication, authorization, or rate limiting by default. So every action must do what an API endpoint would: authenticate and authorize on the server (session lookup, role/ownership checks — never trust client-supplied identity); validate and sanitize all arguments as hostile untrusted input; never return sensitive data to the caller; and be careful with `return` values and error messages leaking internal details. Treat the action surface as public API.
- id: nextjs-route-handlers-basics-01
  answer: |
    A Route Handler is a `route.ts`/`route.js` file exporting async functions named after HTTP methods (`GET`, `POST`, ...). Each receives a standard Web `Request` (with `NextRequest`/`NextResponse` extensions) and returns a `Response`. It replaces the Pages Router's `pages/api/*` routes, lives inside the `app/` folder tree (so it gets nested folders/dynamic segments), and unlike pages it uses Web standard Request/Response rather than Node's `req`/`res`. Handlers render no UI and do not sit inside the layout tree.
- id: nextjs-route-handlers-caching-02
  answer: |
    Version-specific: In Next.js 13/14, `GET` Route Handlers were cached by default (unless they used dynamic APIs). In Next.js 15+, `GET` handlers are NOT cached by default — they are evaluated per request. To opt into caching, use route segment config `export const dynamic = 'force-static'` (optionally with `revalidate`), or use dynamic APIs/fetch options to force per-request evaluation. Non-`GET` methods have never been cached.
- id: nextjs-route-handlers-methods-03
  answer: |
    - Body: `await request.json()`, `await request.text()`, `await request.formData()`, or `arrayBuffer()`/`blob()`.
    - Query params: `const { searchParams } = new URL(request.url)`, or `request.nextUrl.searchParams`.
    - Dynamic route params: come as the second argument — `export async function GET(request: Request, { params }: { params: Promise<{ slug: string }> })` and then `const { slug } = await params` (the param object is a Promise in Next.js 15+).
    - Headers/cookies: `request.headers`, `request.cookies`.
- id: nextjs-streaming-ssr-01
  answer: |
    Streaming SSR means the server renders and flushes HTML progressively: the shell (layout/nav/static chrome) is sent first, and Suspense boundary content is flushed as its data finishes rendering. The browser streams it in and renders pieces as they arrive (out-of-order streaming into placeholders). The benefit over render-everything-then-send: the user sees and can interact with meaningful UI immediately instead of waiting for the slowest data on the page; slow parts don't block the whole response, and first byte/first paint happen much earlier.
- id: nextjs-streaming-suspense-02
  answer: |
    Wrap the slow section in its own `<Suspense fallback={<Skeleton />}>` — ideally by extracting the slow fetch into its own `async` child Server Component and throwing only that piece behind the boundary. The fast parts of the page flush immediately with the shell, the skeleton renders inline, and the slow section streams in when ready — no blocking on the slow data source. (For whole-segment transitions you'd instead use `loading.tsx`; for a section within one page, a manual boundary is the right granularity.)
- id: nextjs-streaming-boundary-03
  answer: |
    `loading.tsx` is a prebuilt **route-level** boundary: it wraps the entire segment's page in Suspense, so the fallback replaces the whole page content while that segment loads (and reappears on client navigation into that route). Your own `<Suspense>` is **component-level** granularity: you choose exactly which subtree streams, keep the rest of the page rendering/streaming interactively, and can have multiple independent boundaries so different sections fall back and arrive independently. `loading.tsx` = one segment-wide boundary; manual Suspense = fine-grained, multiple boundaries.
- id: nextjs-metadata-static-01
  answer: |
    Export a static `metadata` object (or `generateMetadata`) from a `page`/`layout`: `export const metadata: Metadata = { title: '...', description: '...', openGraph: {...} }` — Next renders it into the `<head>` server-side. File conventions (favicon, icon, opengraph-image) can add to `<head>` too. Note the App Router has no `next/head` (that was the Pages Router), and a route cannot export both static `metadata` and `generateMetadata`.
- id: nextjs-metadata-dynamic-02
  answer: |
    Export `generateMetadata` from the page: an `async` function receiving `{ params, searchParams }` that fetches the record and returns a `Metadata` object (e.g. title/description of the post). In Next.js 15 `params` is a Promise, so `await params`. Fetching the same URL + options inside `generateMetadata` and the page is deduped automatically by Request Memoization — one render pass, one round-trip, no opt-in and no second network call. `React.cache()` is for deduping non-fetch (ORM/db) calls, not a prerequisite here; and on statically rendered routes this evaluation happens at build/revalidate time rather than per request.
- id: nextjs-metadata-inherit-03
  answer: |
    Parent layout metadata flows down to children, and child segments override per key: top-level fields a child does not set are inherited; fields it does set win. BUT nested objects such as `openGraph`, `twitter`, `icons`, `robots`, and `alternates` are **replaced wholesale**, not deep-merged — a child `openGraph: { title }` drops the parent's `openGraph.description`/siteName/images, and icons do not accumulate. To keep parent fields, export a shared base object and spread it: `export const metadata = { ...openGraphBase, title }`, or hoist shared values into `metadataBase`/`title.template`.
    `title.template`: set in a parent as `title: { template: '%s | My Site', default: 'My Site' }`; a child's `title: 'About'` renders as "About | My Site". `title.absolute` on a child opts out of the template.
- id: nextjs-metadata-files-04
  answer: |
    File-based conventions in the segment folder: `favicon.ico` (plus `icon.(ico|png|svg)` and `apple-icon.png`) for icons; `opengraph-image.(png|jpg|...)` or a dynamic `opengraph-image.tsx` using `next/og`'s `ImageResponse`; `twitter-image` similarly; `sitemap.xml`/`sitemap.ts` (exporting a default function returning URL entries) for `/sitemap.xml`; `robots.txt`/`robots.ts` for `/robots.txt`; and `manifest.json`/`manifest.ts` for the web app manifest. These are colocated with the route segment they apply to and inherit down.
- id: nextjs-navigation-link-01
  answer: |
    Navigate with `<Link href="...">` from `next/link`. It performs a client-side soft navigation: prefetches the route (in production, links entering the viewport are prefetched automatically), pushes the URL with history state, and re-renders only the segments that changed — so shared layouts stay mounted, client state and scroll are preserved, and no full document download occurs. A plain `<a>` triggers a hard navigation: a full page reload, all assets re-fetched, state lost. `<Link>` renders an `<a>` element and forwards its props.
- id: nextjs-navigation-hooks-02
  answer: |
    The App Router navigation hooks — `useRouter`, `usePathname`, `useSearchParams`, `useParams`, `useSelectedLayoutSegment(s)` — come from `next/navigation`. They fail in a Server Component because they are client hooks that depend on client-side React context (the router context only exists in the browser bundle) and Server Components render once on the server with no client runtime. Any component using them must be a Client Component (`"use client"`), reading data passed down from the server.
- id: nextjs-navigation-redirect-03
  answer: |
    `redirect(url)` tells Next to navigate to a different route (307 by default; `permanentRedirect` for 308) — usable in Server Components, Server Actions, and Route Handlers. `notFound()` triggers rendering of the closest `not-found.tsx` (falling back to the global 404). The gotcha: both work by **throwing** a special control-flow error (e.g. `NEXT_REDIRECT`) that Next catches internally — so a surrounding `try/catch` (or any catch-and-rethrow mistake) will swallow the signal and silently break the redirect, and code placed after the call never runs. Keep `redirect` outside try blocks.
- id: nextjs-navigation-action-redirect-04
  answer: |
    Call `redirect('/new-record')` from `next/navigation` inside the Server Action after the database write and any `revalidatePath`/`revalidateTag` succeed — the user is navigated once the action completes, and the fresh data renders because the path was revalidated. Watch out: `redirect()` throws (NEXT_REDIRECT), so it must not be inside a `try/catch` that would swallow it — do the risky mutation in the try and call the redirect after it, or rethrow framework signals. On failure, skip the redirect and return validation errors instead (via `useActionState`). Prefer the server-side `redirect` over client `router.push` for post-mutation navigation when you also need revalidation to have happened server-side.
