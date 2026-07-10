# Next.js (App Router) Benchmark — Human Render

> **Generated from `questions.yaml` (corpus_rev 1). Do not grade from this file
> — `questions.yaml` is the source of truth.** Hand-synced; if they disagree,
> the YAML wins.

Legend: **W** = weight (1–3), **D** = difficulty. Rubric shows scored points
(`•`) and partial-credit levels (`~`). Caching answers target Next 15+ (fetch/GET
uncached by default, unchanged through Next 16); the Next 14 default is noted for
contrast.

---

### nextjs-app-router-conventions-01 · app-router · W3 · easy
**Q:** What do `layout`, `page`, `loading`, `error`, `route` do, and how does a folder map to a URL?
**A:** A folder = a route segment; `page.tsx` makes it routable, folder path = URL (`[id]` dynamic). `layout` = shared persistent wrapper; `loading` = Suspense fallback; `error` = error boundary; `route.ts` = API endpoint (can't share a segment with `page`).
• page = routable, folder = URL • layout/loading/error roles • route.ts = API, exclusive with page ~ names files, no folder→URL mapping

### nextjs-app-router-layout-02 · app-router · W2 · medium
**Q:** How do nested layouts behave on navigation, and how does `template.tsx` differ from `layout.tsx`?
**A:** Layouts nest and persist across navigation — they don't re-mount, state is preserved, only the changed segment re-renders. `template.tsx` re-mounts a fresh instance every navigation (resets state/effects) — for enter animations / per-nav effects.
• layouts persist / don't re-mount • template re-mounts fresh each nav ~ layouts only, misses template

### nextjs-app-router-error-03 · app-router · W2 · medium
**Q:** What must an `error.tsx` be, and what does it catch vs not?
**A:** Must be a Client Component; gets `error` + `reset()`. Catches render errors in its segment/children, but NOT its sibling layout (needs a parent boundary) or event handlers. `global-error.tsx` covers the root layout.
• client component, error+reset props • catches segment renders, not sibling layout/handlers ~ "shows error UI" with no boundary scope

### nextjs-app-router-loading-04 · app-router, streaming · W2 · easy
**Q:** What does `loading.tsx` do mechanically?
**A:** Next auto-wraps the segment's page in `<Suspense>` with `loading.tsx` as the fallback; the fallback streams instantly (layout already rendered) then swaps in real content.
• Next wraps page in Suspense w/ loading.tsx fallback • instant loading UI / streaming ~ "shows a spinner" no Suspense

### nextjs-server-components-default-01 · server-components, app-router · W3 · hard
**Q:** Server or Client by default, and what does `"use client"` mark?
**A:** Server Components by default (render on server, ship no JS, can be async). `"use client"` marks the module boundary into the client bundle — that file + everything it imports become client. Module-level, not per-component.
• server by default in app dir • "use client" = module boundary (file + imports) • server=no JS/async, client=interactivity ~ says client-default or per-component

### nextjs-server-components-hooks-02 · server-components · W3 · medium
**Q:** Why can't you use `useState`/`onClick`/`window` in a Server Component, and the fix?
**A:** Server components render only on the server and never hydrate — no instance, no browser, no event loop for hooks/handlers/browser APIs. Fix: extract the interactive part into a small `"use client"` component and pass data as props.
• server-only / no hydration → no hooks/events/browser APIs • fix: extract 'use client' component ~ "not allowed" with no reason

### nextjs-server-components-props-03 · server-components, data-fetching · W2 · hard
**Q:** What props can cross Server→Client, and how to keep a server subtree inside a client component?
**A:** Serializable only (primitives, plain objects/arrays, elements) — no functions/handlers/class instances. Pass the server-rendered element as `children` so the subtree stays server-rendered instead of entering the client bundle.
• props serializable, no functions/instances • pass server subtree as children/element prop ~ "serializable" without children slot

### nextjs-data-fetching-rsc-01 · data-fetching, server-components · W3 · medium
**Q:** How do you fetch data in the App Router; what replaces `getServerSideProps`/`getStaticProps`?
**A:** An `async` Server Component `await`s data during render on the server (no data JS shipped). `getServerSideProps`/`getStaticProps` don't exist in the app dir (Pages Router only); static vs dynamic is driven by caching/dynamic APIs.
• async server component awaits data on server • gSSP/gSP gone in app dir ~ still references getServerSideProps in app dir

### nextjs-data-fetching-nogssp-02 · data-fetching, rendering · W2 · medium
**Q:** Several independent data sources for one page — avoid a waterfall in a Server Component?
**A:** Sequential awaits waterfall. Run them in parallel (`Promise.all`, or start promises then await), or isolate slow independent fetches behind separate `<Suspense>` boundaries to stream in parallel.
• parallelize (Promise.all / start-then-await) • or separate Suspense boundaries ~ spots waterfall, no fix

### nextjs-caching-fetch-default-01 · caching, data-fetching · W3 · hard
**Q:** Is `await fetch(url)` in a Server Component cached by default? Be version-explicit.
**A:** Next 14: cached by default (`force-cache`, Data Cache) — stale-data foot-gun. Next 15+ (through 16): NOT cached by default (`no-store`); opt in with `cache: 'force-cache'` or `next: { revalidate: N }`.
• Next 14 cached by default • Next 15+ uncached by default • opt in via force-cache/revalidate ~ one default, no version

### nextjs-caching-layers-02 · caching · W2 · hard
**Q:** Name the caching layers and what each caches.
**A:** Request Memoization (per-render fetch dedup), Data Cache (persistent cross-request fetch/data results), Full Route Cache (static HTML+RSC payload), client Router Cache (visited segments in the browser).
• Request Memoization • Data Cache • Full Route Cache + Router Cache ~ one layer only

### nextjs-caching-revalidate-03 · caching, rendering · W3 · medium
**Q:** What does `revalidate` do, and how is ISR different from fully static?
**A:** `revalidate = N` caches data/route but marks it stale after N seconds; next request serves stale then regenerates in background (ISR). Fully static is built once, never re-fetched until deploy/on-demand invalidation.
• revalidate = staleness window • ISR: serve-stale-then-regenerate vs static built-once ~ "sets a cache time" no SWR/ISR

### nextjs-caching-ondemand-04 · caching, server-actions · W2 · medium
**Q:** After a mutation, make cached pages reflect new data now — `revalidatePath` vs `revalidateTag`?
**A:** On-demand invalidation from `next/cache`, called in a Server Action/Route Handler after a write. `revalidatePath('/posts')` purges a path; `revalidateTag('posts')` purges fetches tagged `posts` (tag via `next: { tags }`). Tag-based preferred.
• on-demand invalidation after a write • path vs tag ~ mentions revalidate, misses after-mutation / path-vs-tag

### nextjs-caching-segment-config-05 · caching, rendering · W3 · hard
**Q:** What do `export const dynamic` / `export const revalidate` control, and when `force-dynamic`?
**A:** Per-segment overrides for caching/rendering. `revalidate` = segment revalidation window. `force-dynamic` = render every request, no Data/Route cache (fetches → no-store); `force-static` = fully static. Use `force-dynamic` for always-fresh per-request data.
• segment-level caching/render overrides • force-dynamic = per-request/no cache; force-static = static ~ names exports, no force-dynamic effect

### nextjs-rendering-static-dynamic-01 · rendering · W3 · hard
**Q:** How does Next decide static vs dynamic rendering?
**A:** Static (prerendered) by default unless something forces dynamic: a dynamic API (`cookies()`/`headers()`/`draftMode()`/`searchParams`) or opting out of caching (`no-store`, `force-dynamic`, `revalidate:0`). No gSSP flag — inferred from code.
• static by default unless forced dynamic • triggered by dynamic APIs or no-cache ~ "you choose" without triggers

### nextjs-rendering-static-params-02 · rendering · W2 · medium
**Q:** What does `generateStaticParams` do for `app/blog/[slug]`, and the Pages Router equivalent?
**A:** Returns the param values (slugs) to prerender at build (SSG). Replaces `getStaticPaths`. Params not returned render on-demand then cache (controlled by `dynamicParams`).
• returns params to prerender dynamic pages (SSG) • replaces getStaticPaths ~ "generates static pages" no params/getStaticPaths

### nextjs-rendering-dynamic-apis-03 · rendering, data-fetching · W2 · hard
**Q:** In Next 15+, why do `cookies()`/`headers()`/`params`/`searchParams` need `await`, and the effect?
**A:** They became async in Next 15 (to support streaming/deferred rendering) — `await cookies()`, `const { slug } = await params`. Reading any of them forces dynamic rendering (can't prerender without the request).
• request APIs async in Next 15 (await) • reading them forces dynamic render ~ request-scoped but misses async/dynamic effect

### nextjs-server-actions-useserver-01 · server-actions · W3 · hard
**Q:** What does `"use server"` mark, and how does it differ from `"use client"`?
**A:** Marks Server Actions — async server functions callable from the client (form `action`/handlers). File-level marks all exports; inline marks one function. Opposite concern from `"use client"`: RPC/function boundary vs client-component boundary.
• 'use server' = server fns callable from client • file-level vs inline • distinct from 'use client' ~ "runs on server" no callable-action concept

### nextjs-server-actions-mutation-02 · server-actions, caching · W2 · medium
**Q:** Walk through a Server Action handling a form submit that writes data and updates UI.
**A:** Async `"use server"` fn as `<form action={...}>`; receives `FormData`, validates, writes. Then `revalidatePath`/`revalidateTag` (or `redirect`) so cached pages refetch. Works without JS (progressive enhancement); `useActionState`/`useFormStatus` for pending/errors.
• 'use server' fn wired to form action, does write • revalidate/redirect after to refresh ~ handles submit, never revalidates

### nextjs-server-actions-security-03 · server-actions, route-handlers · W3 · hard
**Q:** A Server Action feels like a local call — why is that a security trap, and what's required?
**A:** Every action compiles to a public HTTP endpoint — callable directly with arbitrary args. Treat all input as untrusted: authenticate, authorize the operation, validate/parse args inside the action. Don't trust that only your UI calls it.
• action = public HTTP endpoint • must authn + authz + validate inputs ~ "validate inputs" without public-endpoint insight

### nextjs-route-handlers-basics-01 · route-handlers · W2 · medium
**Q:** What is a Route Handler (`route.ts`), and what replaces `pages/api`?
**A:** `route.ts` exports functions named by HTTP method (`GET`/`POST`/…) taking a Web `Request`, returning a Web `Response`. Replaces `pages/api`; can't share a segment with `page.tsx`. Prefer Server Actions for internal mutations.
• route.ts exports method fns w/ Web Request/Response • replaces pages/api, exclusive with page ~ "an API route" no Request/Response

### nextjs-route-handlers-caching-02 · route-handlers, caching · W3 · hard
**Q:** Is a `GET` Route Handler cached by default? Version-specific + opt-in.
**A:** Next 14: GET cached by default. Next 15+ (through 16): GET NOT cached by default. Opt in with `export const dynamic = 'force-static'` (+ `revalidate = N`). Non-GET never cached.
• Next 14 cached, Next 15+ not • opt in via dynamic = 'force-static' ~ default w/o version, or misses opt-in

### nextjs-route-handlers-methods-03 · route-handlers · W1 · easy
**Q:** How to read body, query params, and dynamic params in a Route Handler?
**A:** Body: `await request.json()`/`formData()`. Query: `new URL(request.url).searchParams` / `request.nextUrl.searchParams`. Dynamic params from the context arg (`const { id } = await ctx.params` in Next 15). Return `Response.json(...)`/`NextResponse`.
• body via json/formData; query via URL.searchParams • dynamic params from context (awaited in 15) ~ Request/Response but no body/params how

### nextjs-streaming-ssr-01 · streaming · W2 · medium
**Q:** What is streaming SSR, and what does it buy over render-then-send?
**A:** Server flushes HTML in chunks: static shell first, slower parts stream as data resolves. Lowers TTFB and shows/interacts with ready content while slow data loads. Driven by Suspense (incl. `loading.tsx`).
• shell first, stream slower parts (chunked) • better TTFB / show ready content ~ "progressive HTML" no shell/Suspense

### nextjs-streaming-suspense-02 · streaming, server-components · W2 · medium
**Q:** Fast page, one slow section — keep it fast without blocking?
**A:** Move the slow fetch into its own async Server Component wrapped in `<Suspense fallback>`. The rest streams immediately; the slow chunk streams in and replaces the fallback when ready.
• isolate slow part in async component + Suspense • rest streams now, slow swaps in ~ "use Suspense" without isolation

### nextjs-streaming-boundary-03 · streaming, app-router · W1 · easy
**Q:** `loading.tsx` vs your own `<Suspense>` boundary?
**A:** `loading.tsx` wraps the whole segment's page in one route-level Suspense (whole-page loading). Manual `<Suspense>` targets specific subtrees so fast parts render and only slow parts show fallbacks. Use loading.tsx for the shell, inner Suspense for granular streaming.
• loading.tsx = one boundary around whole segment • manual Suspense = granular subtrees ~ "both show loading" no coarse/granular

### nextjs-metadata-static-01 · metadata · W2 · easy
**Q:** How to set title and `<head>` tags for a route?
**A:** Export a `metadata` object from `layout`/`page` (`export const metadata = { title, description }`); Next renders the head tags. Not available in `"use client"` files.
• export static metadata object from layout/page • Next generates head tags, server-only ~ hand-writes head or client component

### nextjs-metadata-dynamic-02 · metadata · W2 · medium
**Q:** Title/description depends on fetched data — how to generate metadata?
**A:** Export async `generateMetadata({ params })` that fetches and returns `Metadata`. Runs on server; fetches dedupe with the page's via Request Memoization (no double fetch). Use instead of static `metadata` when it depends on params/data.
• async generateMetadata returning Metadata from params/data • server-run, fetch deduped ~ "compute title dynamically" no generateMetadata

### nextjs-metadata-inherit-03 · metadata · W1 · medium
**Q:** How does metadata compose across nested layouts/pages, and what is `title.template`?
**A:** Evaluated root→leaf and merged; deeper fields override/extend parents (root sets defaults, pages override). `title.template` (`{ template: '%s | Site', default }`) lets child pages supply just their title, slotted into `%s`.
• metadata merges root→leaf, deeper overrides • title.template wraps child titles ~ "set per page" no merge/template

### nextjs-metadata-files-04 · metadata · W1 · easy
**Q:** Besides the `metadata` export, how to define favicon, OG image, `sitemap.xml`, `robots.txt`?
**A:** File-based metadata conventions: special files auto-served/linked — `favicon.ico`/`icon`, `opengraph-image`/`twitter-image` (static or generated), `sitemap.ts`→`sitemap.xml`, `robots.ts`→`robots.txt`, `manifest.ts`. Next wires routes/headers/tags.
• file-based conventions auto-served (og-image/icon/sitemap/robots/manifest) • static or code-generated, Next wires tags ~ names one, no general idea

### nextjs-navigation-link-01 · navigation · W2 · easy
**Q:** How to navigate, and what does `<Link>` do that `<a>` doesn't?
**A:** `<Link href>` from `next/link` (declarative) or `useRouter().push()` from `next/navigation` (imperative). `<Link>` does client-side nav (no reload), prefetches routes in the viewport (production), updates the Router Cache. `<a>` = full reload.
• Link/useRouter().push = client-side nav no reload • Link prefetches in-viewport ~ "use Link" no prefetch/CSR detail

### nextjs-navigation-hooks-02 · navigation, server-components · W2 · medium
**Q:** Which module do nav hooks come from, and why do they fail in a Server Component?
**A:** `next/navigation` (NOT `next/router` = Pages Router, errors here). They're client hooks needing state → require `"use client"`. Server Components have no hooks; read the URL from `params`/`searchParams` props instead.
• hooks from next/navigation (not next/router) • client-only ('use client'); server reads params/searchParams ~ next/router, or misses client-only

### nextjs-navigation-redirect-03 · navigation, rendering · W2 · medium
**Q:** What do `redirect()` and `notFound()` do, and the gotcha?
**A:** Server-side helpers (Server Components/Actions/Route Handlers). `redirect('/login')` navigates; `notFound()` renders nearest `not-found.tsx` (404). Gotcha: both THROW a special error, so a surrounding `try/catch` swallows them — call outside try or re-throw.
• redirect navigates, notFound → not-found.tsx (404), server-side • they throw → don't wrap in try/catch ~ what they do, misses throw gotcha

### nextjs-navigation-action-redirect-04 · navigation, server-actions · W1 · medium
**Q:** After a successful Server Action, send the user to the new page — how, and watch out for?
**A:** Call `redirect('/items/123')` at the end, after the write and any `revalidate*`. Since `redirect` throws, keep it OUTSIDE try/catch and don't put code after it (won't run). Client then navigates.
• redirect at end, after write/revalidate • throws → outside try/catch, nothing after runs ~ "use redirect" no ordering/throw caveat
