- id: nextjs-app-router-conventions-01
  answer: |
    `layout.tsx` defines shared UI for a route segment and wraps its children; a root layout must contain `<html>` and `<body>`. `page.tsx` defines the segment’s unique UI and makes that URL publicly accessible. `loading.tsx` provides an automatic Suspense fallback for the segment and its children. `error.tsx` provides an error boundary for the segment’s content and nested child segments; it must be a Client Component. `route.ts` defines an HTTP Route Handler instead of a page UI and cannot coexist with `page.tsx` at the same segment.

    Folders form URL segments, so `app/blog/[slug]/page.tsx` serves paths such as `/blog/my-post`. Dynamic segments use brackets, route groups such as `(marketing)` organize files without adding a URL segment, and private folders prefixed with `_` are excluded from routing. A folder becomes a page route through `page.tsx` or an endpoint through `route.ts`.

- id: nextjs-app-router-layout-02
  answer: |
    Nested layouts are persistent across navigation. When only a descendant segment changes, shared ancestor layouts remain mounted, preserve their client state, and do not rerender just because the child changed.

    A `template.tsx` behaves like a layout but is given a distinct instance for each navigation into that segment. It is remounted on every visit, so component state and effects reset. Templates can also wrap a `layout.tsx`, and the layout remains persistent around them.

- id: nextjs-app-router-error-03
  answer: |
    An `error.tsx` file must be a React Client Component, normally beginning with `"use client"`. It receives an `error` object and a `reset` callback that attempts to rerender the boundary.

    It catches rendering and related runtime errors from its nested child segments and their contents. It does not catch errors thrown by the same segment’s `layout.tsx`, because that layout is outside the boundary; place an `error.tsx` in the parent segment to catch it. React error boundaries also do not catch ordinary errors from event handlers or detached asynchronous callbacks. In production, detailed error information is hidden from the client.

- id: nextjs-app-router-loading-04
  answer: |
    Adding `loading.tsx` automatically wraps the route segment’s page and nested children in a Suspense boundary. While that segment is being rendered, streamed, or navigated to, Next.js displays the loading file’s fallback instead of blocking the whole route. A nested `loading.tsx` applies only from its segment downward and can replace the loading UI for that part of the tree.

- id: nextjs-server-components-default-01
  answer: |
    Components in the App Router are Server Components by default. They run on the server, may be asynchronous, and can access server-side data and secrets without exposing those values to the browser.

    `"use client"` marks a module as the entry point of a Client Component boundary. That module and its imported dependencies are included in the client bundle and may use React state, effects, event handlers, and browser APIs. It does not mean the component is rendered only in the browser: Next.js can still server-render its initial HTML.

- id: nextjs-server-components-hooks-02
  answer: |
    Server Components execute during server rendering and do not have a client React runtime, state, effects, or event handlers. Therefore hooks such as `useState` and `useEffect` are unavailable, event props such as `onClick` require the client environment, and globals such as `window` and `document` do not exist on the server.

    Put `"use client"` in the smallest interactive component that needs those capabilities, then render that component from a Server Component and pass it data through serializable props. Keep data access, secrets, and the rest of the page on the server.

- id: nextjs-server-components-props-03
  answer: |
    A Server Component may pass values that React’s Server-to-Client serialization boundary supports, including primitives, arrays, plain objects, and other serializable values. It must not pass arbitrary classes, functions, or unserializable server objects. Server Actions are a supported special case and can be passed as function references.

    The idiomatic composition pattern is to make a Client Component a wrapper that accepts `children`, then pass a Server Component or server-rendered JSX as that child. The server component is rendered on the server and its resulting RSC payload is supplied through the client boundary rather than making the server component part of the client bundle.

- id: nextjs-data-fetching-rsc-01
  answer: |
    Fetch data directly inside an async Server Component, preferably with the platform `fetch` API, or call a server-side data-access function. Nested async components and Suspense boundaries can fetch different regions of a page independently.

    `getServerSideProps` is replaced by fetching and reading request data in a Server Component or a Route Handler, with dynamic APIs or an explicit dynamic/no-cache configuration when required. `getStaticProps` is replaced by rendering data into a component at build time, using `generateStaticParams` for known dynamic paths, and using revalidation for later refreshes. Client-side libraries can still be used where post-hydration or client-only data is appropriate.

- id: nextjs-data-fetching-nogssp-02
  answer: |
    Start all independent operations before awaiting any of them, then wait for them together:

    `const usersPromise = getUsers(); const postsPromise = getPosts(); const [users, posts] = await Promise.all([usersPromise, postsPromise]);`

    Awaiting `getUsers()` completely before calling `getPosts()` creates a waterfall. For larger pages, put each source in separate async child components with their own Suspense boundaries so Next.js can stream one result without blocking the others. React’s `cache` helper can deduplicate repeated reads, but it does not replace parallel execution.

- id: nextjs-caching-fetch-default-01
  answer: |
    In Next.js 15, `await fetch(url)` in a Server Component is not cached by default. The changed default makes requests dynamic by default; this should not be confused with the older behavior.

    In Next.js 14 and earlier, GET fetches in Server Components were cached by default under the older caching model. To opt into caching in Next.js 15, set `cache: "force-cache"` on the fetch request. A fetch-level time window can be set with `next: { revalidate: 60 }`, while `cache: "no-store"` explicitly requests no fetch cache. POST requests are not cached.

- id: nextjs-caching-layers-02
  answer: |
    The App Router’s main caching layers are:

    - Request Memoization: short-lived, server-side deduplication of identical GET fetches during one React render pass.
    - Data Cache: persistent server-side storage for cached `fetch` results and data produced with supported cache APIs, shared across requests.
    - Full Route Cache: server-side storage of rendered HTML and React Server Component payloads for static routes and opted-in static Route Handlers.
    - Router Cache: client-side, in-memory caching of visited routes, especially to make back and forward navigation feel instantaneous.

- id: nextjs-caching-revalidate-03
  answer: |
    `export const revalidate = 60` sets a time-based revalidation window, in seconds, for cached data and route output in that segment. Once the content is older than the window, a request can receive the stale cached response while Next.js regenerates it in the background; later requests receive the newly generated version.

    Fully static content is normally generated at build time and reused indefinitely unless explicitly revalidated or the deployment changes. ISR uses the same static-rendering machinery but regenerates output after a specified interval, optionally regenerating on demand. `revalidate = 0` opts out of caching for the route and uses dynamic rendering.

- id: nextjs-caching-ondemand-04
  answer: |
    `revalidatePath(path, type?)` invalidates cached content associated with a route path, such as `/products` or `/products/[id]`; the optional type can target a page or layout. `revalidateTag(tag)` invalidates cached data associated with a tag, such as data fetched with `next: { tags: ["products"] }`.

    Call them after a successful mutation from a Server Action or Route Handler, not while rendering a Server Component. Revalidation affects subsequent renders; it does not by itself update DOM already displayed. For immediate UI refresh, return the new state to the client, call `router.refresh()` where appropriate, or redirect and render the target with the newly revalidated data.

- id: nextjs-caching-segment-config-05
  answer: |
    `export const dynamic` controls the segment’s rendering mode. It can be `"auto"`, `"force-dynamic"`, `"force-static"`, or `"error"`. Setting it to `"force-dynamic"` opts the route into request-time rendering rather than the Full Route Cache or a static regeneration cycle.

    `export const revalidate` controls how many seconds statically cached content may be reused before it becomes stale and is regenerated.

    Use `dynamic = "force-dynamic"` when a route must be personalized or request-specific and should not be cached, such as an account page depending on the current session. In Next.js 15, reading request-only data such as cookies, headers, or search params often already makes a route dynamic, so the explicit setting is mainly an enforcement and intent declaration. It should not be used when shared or time-based caching is safe.

- id: nextjs-rendering-static-dynamic-01
  answer: |
    Next.js attempts to statically prerender a route when it can determine the output without request-specific information. A dynamic route segment is not automatically dynamically rendered: if `generateStaticParams` provides the parameter values, the resulting instances can be prerendered.

    Request-time APIs such as `cookies()`, `headers()`, and page `searchParams`, explicitly dynamic configuration, or uncached data opt the relevant route into dynamic rendering. Time-based revalidation keeps the route in an incremental static regeneration model. Unknown dynamic parameters may also be generated on demand and cached, depending on `dynamicParams`.

- id: nextjs-rendering-static-params-02
  answer: |
    `generateStaticParams` supplies the parameter values Next.js should use to prerender a dynamic segment. For `app/blog/[slug]`, returning values such as `{ slug: "first-post" }` allows Next.js to generate static HTML and RSC output for those known blog posts at build time. Returned paths need not cover every possible value; by default, other values can be generated on demand and cached, while `dynamicParams = false` makes them return 404.

    The Pages Router equivalent is `getStaticPaths` together with `getStaticProps`. `fallback` in `getStaticPaths` broadly corresponds to the App Router’s `dynamicParams` behavior. A Pages Router route using `getServerSideProps` is instead analogous to request-time rendering.

- id: nextjs-rendering-dynamic-apis-03
  answer: |
    Next.js 15 made `cookies()`, `headers()`, `draftMode()`, and the page props `params` and `searchParams` asynchronous. Their values are Promises, so code must use `await cookies()`, `await headers()`, and `const { slug } = await params` rather than treating them as plain objects.

    These APIs expose values that cannot generally be known before a request. Reading request-time values such as cookies, headers, or search params opts the relevant rendering path into dynamic behavior rather than fully static output. Routes with build-time `generateStaticParams` can still prerender known parameter instances because Next.js can supply those params without an arbitrary user-specific request.

- id: nextjs-server-actions-useserver-01
  answer: |
    `"use server"` marks a module, or an individual function body, as server-side executable code. Exported async functions from such a module are Server Actions: they execute on the server and can be called from a Server Component or imported into and passed to a Client Component, commonly as a form action.

    It is not the opposite of `"use client"` in the rendering sense. `"use client"` creates a Client Component boundary and affects the client module graph. `"use server"` provides callable server references while allowing the server action to use databases, secrets, and server-only libraries. Server Actions are still public endpoints and must authenticate and validate every invocation.

- id: nextjs-server-actions-mutation-02
  answer: |
    Define an async action in a server module:

    `export async function createPost(formData: FormData) {`
    `  const session = await requireUser();`
    `  const values = validatePost(formData);`
    `  const post = await db.post.create({ data: { ...values, authorId: session.userId } });`
    `  revalidatePath("/posts");`
    `  redirect(`/posts/${post.id}`);`
    `}`

    Put `"use server"` at the top of that module. In a Client Component, import the action and use it as a form action, or pass it to `useActionState`/`useFormStatus` when progress and validation feedback are needed. React sends the form data to the server action, the action writes the record, invalidates affected cache entries, and then redirects to the new page. If no redirect is desired, the action can return a serializable result and update the client state; for same-page server data, it can revalidate and have the client call `router.refresh()`.

- id: nextjs-server-actions-security-03
  answer: |
    A Server Action looks local only in source code. At runtime it is reachable through a server endpoint and can be invoked without going through the intended UI. An attacker can forge requests, alter arguments, guess identifiers, or call an action directly even if no visible form imports it.

    Every action must independently authenticate the caller, authorize access to the specific resource, and validate all untrusted input, including `FormData`, IDs, uploads, and hidden fields. Add abuse controls such as rate limits where appropriate, use parameterized data access, and return only safe errors. Client-side validation, hidden fields, and `"use server"` are not security controls.

- id: nextjs-route-handlers-basics-01
  answer: |
    A `route.ts` file defines an App Router HTTP endpoint inside an `app` directory. It exports one or more HTTP-method functions such as `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `HEAD`, or `OPTIONS`, receiving a standard `Request` or Next.js `NextRequest` and returning a `Response` or `NextResponse`. It does not render React UI.

    `app/**/route.ts` replaces Pages Router API routes under `pages/api/**`. A route handler is a terminal segment and cannot be defined at the same level as a conflicting `page.tsx`.

- id: nextjs-route-handlers-caching-02
  answer: |
    In Next.js 15 and later, GET Route Handlers are not cached by default. In Next.js 14 and earlier, a GET handler using `Response` without dynamic APIs or dynamic segment configuration was cached by default.

    To opt a Next.js 15 handler into the Full Route Cache, export `dynamic = "force-static"`:

    `export const dynamic = "force-static";`

    Add `export const revalidate = 60` for time-based regeneration. A `fetch` option such as `cache: "force-cache"` caches the underlying fetch, but should not be assumed to make the entire handler response static unless the route configuration also opts into static rendering.

- id: nextjs-route-handlers-methods-03
  answer: |
    Use methods on the request object for the body: `await request.json()`, `await request.text()`, `await request.formData()`, `await request.arrayBuffer()`, or consume `request.body`. Read query values with `new URL(request.url).searchParams`, or from `request.nextUrl.searchParams` when using `NextRequest`.

    Dynamic route params are provided in a second function argument. In Next.js 15 they are asynchronous:

    `export async function POST(request: Request, { params }: { params: Promise<{ id: string }> }) {`
    `  const { id } = await params;`
    `  const body = await request.json();`
    `}`

- id: nextjs-streaming-ssr-01
  answer: |
    Streaming SSR renders the server output incrementally. Next.js sends an initial HTML shell and React Server Component payload as soon as they are ready, rather than waiting for every data-dependent part of the page. Suspense boundaries are the streaming points; slower boundaries later send fallback markup and, once resolved, their content and the corresponding RSC instructions.

    This can improve time to first byte and first contentful paint, let fast regions appear immediately, and allow independent data requests to run in parallel. One slow section therefore does not force the whole page to remain blank.

- id: nextjs-streaming-suspense-02
  answer: |
    Put the slow data work in a nested async Server Component and wrap that component in a React `<Suspense>` boundary with a suitable fallback:

    `export default function Page() {`
    `  return <>`
    `    <FastContent />`
    `    <Suspense fallback={<ArticleSkeleton />}>`
    `      <SlowArticle />`
    `    </Suspense>`
    `  </>;`
    `}`

    Next.js can render and send `FastContent` immediately, run independent fetches concurrently, and stream `SlowArticle` when its data is ready. Starting the slow request only after the fast content has finished would still create a waterfall.

- id: nextjs-streaming-boundary-03
  answer: |
    `loading.tsx` is a route-level convention. Next.js automatically places a Suspense boundary around the segment’s page and descendants using that file as the fallback. It is especially useful for showing navigation feedback while a whole route segment loads.

    An explicit `<Suspense>` boundary is chosen and placed in the component tree, so it can wrap only one slow region, work conditionally, or appear in any server-rendered component. Both mechanisms enable streaming; the difference is convention-driven segment-wide behavior versus manual, granular control.

- id: nextjs-metadata-static-01
  answer: |
    Export a `metadata` object from a route’s `page.tsx` or `layout.tsx`:

    `export const metadata = {`
    `  title: "Products",`
    `  description: "Browse our products",`
    `  openGraph: { title: "Products", description: "Browse our products" },`
    `};`

    Next.js renders the corresponding tags in `<head>`, including title, description, Open Graph, Twitter, icons, and other supported metadata. Manually adding a separate `<title>` or `<meta>` tree for these values is unnecessary and can conflict with the Metadata API.

- id: nextjs-metadata-dynamic-02
  answer: |
    Export an async `generateMetadata` function from the route segment:

    `export async function generateMetadata({ params }: { params: Promise<{ slug: string }> }): Promise<Metadata> {`
    `  const { slug } = await params;`
    `  const post = await getPost(slug);`
    `  return {`
    `    title: post.title,`
    `    description: post.description,`
    `    openGraph: { title: post.title, description: post.description, images: [post.image] },`
    `  };`
    `}`

    Next.js awaits this function and uses the returned `Metadata`. In Next.js 15, segment `params` is a Promise and must be awaited. A segment should export either `metadata` or `generateMetadata`, not both. A memoized data function or React `cache` can deduplicate the post lookup if the page fetches the same record.

- id: nextjs-metadata-inherit-03
  answer: |
    Metadata is inherited from parent layouts, then child layouts and the page can override fields. Resolution proceeds through the segment hierarchy, and the closest definition of a conflicting field wins; metadata objects are not generally deep-merged, so overriding one parent object replaces that object before the child can refine it.

    `title.template` supplies a suffix for descendant title strings. For example, a layout can define `title: { template: "%s | Acme", default: "Acme" }`. A child with `title: "About"` renders as `About | Acme`. Use `title.absolute` when a descendant title must bypass the template, and `title.default` for the title when a child does not provide one.

- id: nextjs-metadata-files-04
  answer: |
    Next.js supports metadata file conventions in `app` directories. These include `favicon.ico`, `icon.*`, and `apple-icon.*`; `opengraph-image.*` and `twitter-image.*` for social images; and `sitemap.*` plus `robots.*` for crawler files. It also supports `manifest.*` for web-app manifests. Static files are added automatically; dynamic variants can be TypeScript or JavaScript files exporting the corresponding `MetadataRoute` value.

    For example, `app/sitemap.ts` can default-export `MetadataRoute.Sitemap`, while `app/robots.ts` can default-export `MetadataRoute.Robots`. The conventions can be placed in the root app segment or a route segment to apply to that subtree, and `metadataBase` helps resolve relative social-image and other URLs.

- id: nextjs-navigation-link-01
  answer: |
    Use `next/link`:

    `import Link from "next/link";`
    `return <Link href="/dashboard">Dashboard</Link>;`

    `<Link>` uses App Router client-side navigation, updates browser history, and requests and renders the relevant RSC route instead of performing a full browser document reload. It can prefetch route data, particularly for links entering the viewport, and provides App Router controls for replace, prefetching, and scrolling. A plain `<a href>` performs ordinary full-page navigation; use it for external URLs, downloads, or cases where a full document load is intentional. For programmatic client navigation, use `router.push()` or `router.replace()` from `next/navigation`.

- id: nextjs-navigation-hooks-02
  answer: |
    App Router navigation hooks come from `next/navigation`, including `useRouter`, `usePathname`, `useSearchParams`, `useParams`, and the selected-layout-segment hooks.

    They are Client Component hooks because they depend on React client contexts and the browser navigation state. A Server Component has no client hook runtime or navigation context, so calling them there fails; mark the component containing the hook with `"use client"`. In a Server Component, use `<Link>` for navigable links or `redirect()` for control flow. The page’s `searchParams` prop can be awaited on the server when the query string is needed there.

- id: nextjs-navigation-redirect-03
  answer: |
    `redirect(url)` interrupts the current render or Server Action and navigates to the supplied URL, using the appropriate server response or client navigation mechanism.

    `notFound()` interrupts rendering, produces a 404 response, and displays the closest applicable `not-found.tsx` or the root not-found UI. In ordinary server-side JavaScript it is also conventional to write `throw redirect(...)` and `throw notFound()`.

    Both functions throw control-flow exceptions. Wrapping them in a broad `try/catch` can catch the exception and accidentally continue rendering or return a normal response; such a catch must rethrow the control-flow error. They are not values that should ordinarily be returned with `return redirect(...)`.

- id: nextjs-navigation-action-redirect-04
  answer: |
    Import `redirect` from `next/navigation` and call it only after the mutation has succeeded:

    `const record = await createRecord(input);`
    `revalidatePath('/records');`
    `redirect(`/records/${record.id}`);`

    A Server Action can return a redirect response that makes Next.js navigate the client to the new page. An absolute URL can be supplied for an external destination. The main gotcha is that `redirect` throws: do not swallow it inside a broad `try/catch`; either call it outside that block or rethrow it. If the user should stay on the current page, return updated state or revalidate and call `router.refresh()` from the client instead, since revalidation alone does not alter already-rendered output.
