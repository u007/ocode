- id: nextjs-app-router-conventions-01
  answer: |
    In the App Router, each folder under `app/` maps to a URL segment. The special files are:
    - `page.tsx`: Makes the route publicly accessible. Exports a React component that is the UI for that segment. Required for a route to be routable.
    - `layout.tsx`: A UI component shared across a segment and its children. It wraps `page` and all child layouts. Persists across navigations (state is preserved). Required at the root.
    - `loading.tsx`: Wraps a `page` (or layout) in a Suspense boundary. Shows a loading UI while the page's content is being fetched/rendered. Enables streaming SSR.
    - `error.tsx`: Wraps a `page` (or layout) in an error boundary. Catches runtime errors in the subtree and shows a fallback UI. Must be a Client Component.
    - `route.ts`: Defines an API endpoint for that segment (GET, POST, etc.). Exports HTTP method handlers. Does not create a page.
    - `template.tsx`: Like layout but remounts on navigation (new component instance per navigation).
    - `not-found.tsx`: UI shown when `notFound()` is called or no route matches.
    - `default.tsx`: Parallel route fallback.

    Folder-to-URL: `app/about/page.tsx` → `/about`. Dynamic segments use brackets: `app/blog/[slug]/page.tsx` → `/blog/:slug`. Route groups `(group)` don't affect the URL. Private segments `_folder` are excluded from routing.

- id: nextjs-app-router-layout-02
  answer: |
    Nested layouts wrap each other like HTML. When you navigate between routes that share a parent layout, the parent layout does NOT re-render or remount — only the `page` and any changed child segments update. This preserves state in the layout (e.g., a sidebar's scroll position, a form's input). The layout tree is preserved across navigations; React reconciles only the changed leaf.

    `template.tsx` differs from `layout.tsx` in that it creates a new component instance on every navigation. State inside a template is reset when you navigate. Use template when you need effects to re-run or state to reset on navigation (e.g., analytics page-view tracking, animations that should replay). Layout is the default choice for persistent UI; template is opt-in for remounting behavior.

- id: nextjs-app-router-error-03
  answer: |
    An `error.tsx` file must:
    1. Be a Client Component (it uses error boundaries which require client-side React).
    2. Accept a `prop` called `error` (an Error object) and optionally a `reset` function.
    3. Export a default React component.

    What it catches: Runtime errors (thrown exceptions) in the rendering of the subtree it wraps — errors in the `page`, child layouts, and components during rendering, in event handlers, and in effects. It catches errors in both Server and Client Components below it.

    What it doesn't catch:
    - Errors in the `layout.tsx` that wraps the `error.tsx` itself (the error boundary must be ABOVE the layout to catch layout errors — so you place `error.tsx` at the same level or above the layout you want to protect).
    - Errors in `template.tsx` (templates are above the error boundary in the tree).
    - Errors during the initial static generation / build time.
    - Errors in Route Handlers (`route.ts`) — those are separate.
    - Errors in `loading.tsx` (use a separate error boundary or the loading file's own error handling).

- id: nextjs-app-router-loading-04
  answer: |
    Adding a `loading.tsx` to a route segment automatically wraps that segment's `page.tsx` (and any nested children) in a React `<Suspense>` boundary. Mechanically:
    - Next.js creates a Suspense boundary at the segment level.
    - The `loading.tsx` content is the `fallback` of that Suspense boundary.
    - While the page's async data is being fetched (during SSR or during client-side navigation), the loading fallback is shown instead of the page.
    - Once the page's data resolves, the fallback is replaced with the actual page content.
    - This enables streaming SSR: the shell (layout) can be sent immediately, and the loading state is streamed while data loads.
    - On client-side navigation, the loading UI appears instantly while the new page's data is fetched in the background.

- id: nextjs-server-components-default-01
  answer: |
    In the App Router, components are Server Components by default. Every component in the `app/` directory is a Server Component unless it has `"use client"` at the top.

    `"use client"` marks a component (and all its imports) as a Client Component. It tells Next.js to render this component on the client (in the browser). The directive must be the first line of the file. It creates a "client boundary" — everything imported into a client component also becomes client-side, unless it's explicitly a server component passed as a child/prop.

    Server Components: run only on the server, can access DB/filesystem directly, can use async/await, cannot use hooks or browser APIs.
    Client Components: hydrated in the browser, can use hooks, state, effects, browser APIs.

- id: nextjs-server-components-hooks-02
  answer: |
    Server Components run only on the server — there is no React runtime, no browser, no persistent state between renders. `useState`/`useEffect` require a client-side React reconciler and a DOM. `onClick` and `window` are browser-only concepts. Server Components are rendered to HTML (and a special flight protocol) on the server; they don't hydrate or re-render on the client.

    The fix: Add `"use client"` to the component that needs hooks/interactivity. This makes it a Client Component. You can then pass server-rendered content as `children` or props to the client component, keeping the interactive parts client-side and the rest server-rendered. Alternatively, extract the interactive part into a separate client component and import it into the server component.

- id: nextjs-server-components-props-03
  answer: |
    Server Components can pass serializable props to Client Components: strings, numbers, booleans, null, arrays, plain objects, dates, maps, sets. They cannot pass functions, class instances, or non-serializable objects (unless the client component is passed as a child/prop and the function is defined in a `"use server"` file).

    The idiomatic way to keep a server-rendered subtree inside a client component is to pass the server component as `children` (or a prop) to the client component. The server component renders on the server first, and its HTML is sent to the client component as children. The client component then wraps that pre-rendered HTML with its interactive logic. Example:
    ```tsx
    // ClientComponent.tsx ("use client")
    export default function ClientComponent({ children }) {
      const [count, setCount] = useState(0);
      return <div><button onClick={() => setCount(count + 1)}>{count}</button>{children}</div>;
    }
    // page.tsx (Server Component)
    <ClientComponent><ServerSubtree /></ClientComponent>
    ```
    The `children` are rendered on the server and passed to the already-hydrated client component.

- id: nextjs-data-fetching-rsc-01
  answer: |
    In the App Router, you fetch data directly inside Server Components using `async/await`:
    ```tsx
    // app/page.tsx
    export default async function Page() {
      const data = await fetch('https://api.example.com/data').then(r => r.json());
      return <div>{data.name}</div>;
    }
    ```
    There is no `getServerSideProps` or `getStaticProps`. Those Pages Router functions are replaced by:
    - Direct `fetch()` in Server Components (with caching/revalidation controls).
    - Route Handlers (`route.ts`) for API endpoints.
    - Server Actions (`"use server"`) for mutations.
    - `generateStaticParams` for pre-rendering dynamic routes.

    The `fetch` function is extended by Next.js to support caching, deduplication, and revalidation. You can also use any data library (Prisma, Drizzle, etc.) directly in Server Components since they run on the server.

- idjs-data-fetching-nogssp-02
  answer: |
    To avoid a request waterfall (where one fetch waits for another sequentially), you should:
    1. Start all independent fetches at the same time using `Promise.all` or `Promise.allSettled`:
       ```tsx
       const [user, posts] = await Promise.all([fetchUser(), fetchPosts()]);
       ```
    2. Move data fetching as close to the component that needs it as possible, rather than fetching everything in a top-level layout and passing it down.
    3. Use `loading.tsx` and `<Suspense>` to stream slow sections independently so fast content renders first.
    4. Use React's `cache()` (or Next.js's built-in fetch deduplication) to deduplicate identical requests made in different components.
    5. Fetch in parallel at the component level — each component fetches its own data, and Next.js dedupes identical fetches automatically.

- id: nextjs-caching-fetch-default-01
  answer: |
    In Next.js 13 and 14 (Pages Router and early App Router), `fetch()` in a Server Component was NOT cached by default — it was `no-store` by default (dynamic). You had to opt in with `{ cache: 'force-cache' }` or `export const revalidate`.

    In Next.js 15, the default changed: `fetch()` is now cached by default (equivalent to `{ cache: 'force-cache' }`). You must explicitly opt out with `{ cache: 'no-store' }` or `export const dynamic = 'force-dynamic'` if you want fresh data on every request.

    So the answer depends on the version: Next.js 14 and earlier → not cached by default. Next.js 15+ → cached by default.

- id: nextjs-caching-layers-02
  answer: |
    The App Router has several caching layers:
    1. **Request Memoization** (React `cache()`): Deduplicates identical `fetch` calls made during a single render pass. Per-request, in-memory. Prevents duplicate DB queries when multiple components fetch the same data.
    2. **Data Cache** (Next.js fetch cache): The result of `fetch()` calls, persisted across requests. Controlled by `cache` option and `revalidate`. Stored on the server (in-memory or filesystem in dev, persistent in production with a cache handler).
    3. **Full Route Cache** (Router Cache / prerender cache): The rendered HTML and RSC payload for static or ISR routes. Stored after build or first request. Invalidated by `revalidatePath`, `revalidateTag`, or time-based revalidation.
    4. **Client Router Cache**: The client-side cache of RSC payloads for prefetched routes. Enables instant client-side navigation. Managed by Next.js router.

- id: nextjs-caching-revalidate-03
  answer: |
    `revalidate` sets the time (in seconds) after which a cached entry is considered stale. When a request comes in for a stale entry, Next.js serves the stale content and re-generates the cache in the background (stale-while-revalidate pattern). This applies to `fetch` cache, Full Route Cache, and ISR.

    Time-based revalidation (ISR) vs fully static:
    - **Fully static**: The page is rendered at build time and never re-rendered unless you manually invalidate. No `revalidate` or `revalidate: false`. The HTML is served as-is forever.
    - **ISR (time-based)**: The page is rendered at build time, but after `revalidate` seconds, the next request triggers a background re-render. Users see stale content until the new render completes. This gives you static performance with periodic freshness.

- id: nextjs-caching-ondemand-04
  answer: |
    `revalidatePath(path)` invalidates the Full Route Cache for a specific path. After a mutation (e.g., creating a post), you call `revalidatePath('/blog')` so the next request to `/blog` re-renders with fresh data. You call it inside a Server Action or Route Handler after the mutation succeeds.

    `revalidateTag(tag)` invalidates all `fetch` calls (and routes using them) that were tagged with `revalidateTag('tag')` when the fetch was made. This is more granular — you can invalidate all cached data related to a specific entity (e.g., all fetches tagged `'post-123'`).

    Both are called server-side, typically in Server Actions or Route Handlers, after a successful mutation. `revalidatePath` is path-based; `revalidateTag` is data-based and can invalidate multiple paths at once.

- id: nextjs-caching-segment-config-05
  answer: |
    `export const dynamic` controls whether a route is statically or dynamically rendered:
    - `'force-dynamic'`: Always render on demand (dynamic). No caching of the page.
    - `'force-static'`: Always render statically at build time.
    - `'error'`: Force static and error if dynamic functions are used.
    - `'auto'` (default): Let Next.js decide based on whether dynamic functions are used.

    `export const revalidate` sets the revalidation interval in seconds for the route's cached data and Full Route Cache. `0` means no caching (revalidate every request). A positive number means ISR with that interval.

    Set `dynamic = 'force-dynamic'` when the page must always render with fresh data — e.g., a dashboard that uses `cookies()` or `headers()` for personalized content, or a page that must never be cached.

- id: nextjs-rendering-static-dynamic-01
  answer: |
    Next.js decides at build time (or first request for dynamic routes) whether a route is static or dynamic:
    - If a route uses NO dynamic functions (`cookies()`, `headers()`, `searchParams`, `params` without `generateStaticParams`) and all fetches are cacheable, it is statically rendered at build time.
    - If a route uses dynamic functions (e.g., `cookies()`, `headers()`, or `searchParams` in a page that doesn't have `generateStaticParams`), it is dynamically rendered on each request.
    - You can force the behavior with `export const dynamic = 'force-static'` or `'force-dynamic'`.
    - For dynamic routes with `generateStaticParams`, the listed params are pre-rendered statically; additional params are rendered on demand (or 404, depending on `dynamicParams`).

- id: nextjs-rendering-static-params-02
  answer: |
    `generateStaticParams` is an async function exported from a dynamic route segment that returns an array of param objects. Next.js calls it at build time to pre-render all listed combinations statically:
    ```tsx
    // app/blog/[slug]/page.tsx
    export async function generateStaticParams() {
      const posts = await fetch('https://api.example.com/posts').then(r => r.json());
      return posts.map(post => ({ slug: post.slug }));
    }
    ```
    This generates `/blog/post-1`, `/blog/post-2`, etc. at build time. Without it, dynamic routes are rendered on demand.

    Pages Router equivalent: `getStaticProps` + `getStaticPaths`. `getStaticPaths` returns `{ paths: [...], fallback: ... }` which is the direct analog of `generateStaticParams`.

- id: nextjs-rendering-dynamic-apis-03
  answer: |
    In Next.js 15+, `cookies()`, `headers()`, `params`, and `searchParams` are asynchronous (return Promises) because they rely on dynamic request-time values that can only be accessed during rendering. You must `await` them:
    ```tsx
    export default async function Page({ params, searchParams }) {
      const { slug } = await params;
      const { q } = await searchParams;
      const cookieStore = await cookies();
    }
    ```
    Reading these in a page makes the route dynamically rendered (on-demand). The page cannot be statically optimized because the values are only known at request time. This is a breaking change from Next.js 14 where they were synchronous.

- id: nextjs-server-actions-useserver-01
  answer: |
    `"use server"` marks a function (or a file) as a Server Action. Functions marked with `"use server"` are executed on the server, even when called from a Client Component. They are exposed to the client as RPC-like endpoints (POST requests under the hood).

    Difference from `"use client"`:
    - `"use client"` marks a component as client-side (rendered in the browser). It's a component boundary directive.
    - `"use server"` marks a function as server-only. It's a function-level directive. The function can be imported and called from client components, but it always runs on the server. It cannot be called during server-side rendering (only in response to user interaction like form submission or button click).

    `"use server"` can be placed at the top of a file (making all exports server actions) or inline before a function declaration.

- id: nextjs-server-actions-mutation-02
    Walk through using a Server Action to handle a form submission that writes data and updates the UI.
  answer: |
    1. Define a Server Action in a `"use server"` file (or inline in a Server Component):
       ```tsx
       // app/actions.ts
       "use server";
       export async function createPost(formData: FormData) {
         const title = formData.get('title');
         await db.post.create({ data: { title } });
         revalidatePath('/posts');
       }
       ```
    2. In a Client Component, use a form with `action={createPost}`:
       ```tsx
       // app/new-post.tsx ("use client")
       import { createPost } from './actions';
       export default function NewPost() {
         return (
           <form action={createPost}>
             <input name="title" />
             <button type="submit">Create</button>
           </form>
         );
       }
       ```
    3. When the form is submitted, Next.js sends a POST request to the server, which runs `createPost`. The action writes to the DB, calls `revalidatePath('/posts')` to invalidate the cache, and Next.js re-renders the affected routes.
    4. The UI updates automatically because the router refreshes the current route after the action completes (or you can use `useTransition` for pending state and `useOptimistic` for instant UI updates).

- id: nextjs-server-actions-security-03
  answer: |
    Server Actions feel like local function calls but they are actually exposed as HTTP endpoints (POST routes). Anyone can call them directly, just like any API endpoint. The security trap: developers may assume the action is only callable from their UI and skip validation.

    Inside every Server Action you must:
    1. **Authenticate the caller** — verify the user is logged in (e.g., check session cookies).
    2. **Authorize** — verify the user has permission to perform the action (e.g., owns the resource).
    3. **Validate all input** — treat form data as untrusted. Validate types, lengths, formats, and sanitize.
    4. **Use parameterized queries** — never interpolate user input into SQL.
    5. **Implement rate limiting** — prevent abuse.
    6. **Return minimal data** — don't leak sensitive information in the response.

    Never trust the client. Server Actions are public endpoints; security must be enforced server-side.

- id: nextjs-route-handlers-basics-01
  answer: |
    A Route Handler is a file (`route.ts` or `route.js`) in the `app/` directory that defines API endpoints for that segment. It exports async functions named after HTTP methods:
    ```tsx
    // app/api/posts/route.ts
    export async function GET(request: Request) {
      return Response.json({ posts: [] });
    }
    export async function POST(request: Request) {
      // handle creation
    }
    ```
    It replaces the Pages Router's `pages/api/*` routes. Instead of `export default function handler(req, res)`, you export named functions (`GET`, `POST`, `PUT`, `DELETE`, `PATCH`, `HEAD`, `OPTIONS`). The `Request` and `Response` objects are the standard Web API objects (extended by Next.js). Route Handlers support streaming, cookies, headers, and dynamic params.

- id: nextjs-route-handlers-caching-02
  answer: |
    In Next.js 14 and earlier, `GET` Route Handlers were NOT cached by default (they were dynamic). You could opt into caching with `export const dynamic = 'force-static'` or by using the `next` option in `Response`.

    In Next.js 15, `GET` Route Handlers are cached by default (like `fetch`). You can opt out with `export const dynamic = 'force-dynamic'` or `export const revalidate = 0`.

    To opt into caching in older versions: `export const dynamic = 'force-static'` or set `revalidate` to a positive number. `POST` handlers are never cached.

- id: nextjs-route-handlers-methods-03
  answer: |
    In a Route Handler:
    - **Request body**: `const body = await request.json();` (for JSON) or `await request.formData()` (for form data) or `await request.text()` (for raw text).
    - **Query params**: `const { searchParams } = new URL(request.url); const q = searchParams.get('q');`
    - **Dynamic route params**: Passed as the second argument: `export async function GET(request: Request, { params }: { params: Promise<{ slug: string }> }) { const { slug } = await params; }` (Next.js 15+ makes params async).
    - **Headers**: `request.headers.get('authorization')`
    - **Cookies**: `request.cookies.get('session')` or `cookies()` from `next/headers`.

- id: nextjs-streaming-ssr-01
  answer: |
    Streaming SSR sends the HTML to the client in chunks as it's rendered, rather than waiting for the entire page to render before sending anything. The server sends the initial HTML shell immediately, then streams in additional content as data resolves.

    What it buys you:
    - **Faster Time to First Byte (TTFB)**: The browser receives and starts parsing HTML immediately.
    - **Faster Largest Contentful Paint (LCP)**: The main content can arrive earlier because the server doesn't wait for slow data.
    - **Better perceived performance**: Users see content progressively instead of a blank page.
    - **No blocking on slow data**: Slow components (wrapped in Suspense) don't block the rest of the page.

    Implemented via `loading.tsx` (automatic Suspense boundaries) and manual `<Suspense>` boundaries.

- id: nextjs-streaming-suspense-02
  answer: |
    Wrap the slow data section in its own `<Suspense>` boundary with a lightweight fallback:
    ```tsx
    // app/page.tsx (Server Component)
    export default function Page() {
      return (
        <div>
          <FastHeader />
          <FastContent />
          <Suspense fallback={<Skeleton />}>
            <SlowDataSection />
          </Suspense>
        </div>
      );
    }
    ```
    The fast parts render and stream to the client immediately. The slow section streams in later when its data resolves. The user sees the page structure and fast content right away, with a loading state for the slow part. This prevents the slow data from blocking the entire page's TTFB and LCP.

- id: nextjs-streaming-boundary-03
  answer: |
    `loading.tsx`:
    - Automatically wraps the entire `page.tsx` (and its children) in a Suspense boundary.
    - The loading UI replaces the ENTIRE page while it loads.
    - Applied at the segment level — it's a convention-based file.
    - Good for: full-page loading states, where the entire page depends on data.

    Manual `<Suspense>` boundary:
    - You choose exactly which component(s) to wrap.
    - Only the wrapped subtree shows the fallback; the rest of the page renders normally.
    - Applied at the component level — more granular.
    - Good for: partial-page loading, where only a section is slow and the rest should render immediately.

    Use `loading.tsx` for whole-page loading states. Use `<Suspense>` for fine-grained control over which parts of the page stream independently.

- id: nextjs-metadata-static-01
  answer: |
    Export a `metadata` object from a `page.tsx` or `layout.tsx`:
    ```tsx
    // app/about/page.tsx
    export const metadata = {
      title: 'About Us',
      description: 'Learn more about our company',
      openGraph: {
        title: 'About Us',
        description: 'Learn more about our company',
      },
    };
    ```
    This sets the `<title>`, `<meta name="description">`, and other `<head>` tags. The metadata is static (hardcoded). You can also export a `generateMetadata` function for dynamic metadata. Metadata in nested layouts composes with page metadata (page overrides layout for the same field).

- id: nextjs-metadata-dynamic-02
  answer: |
    Export an async `generateMetadata` function that fetches data and returns a metadata object:
    ```tsx
    // app/blog/[slug]/page.tsx
    export async function generateMetadata({ params }): Promise<Metadata> {
      const { slug } = await params;
      const post = await fetch(`https://api.example.com/posts/${slug}`).then(r => r.json());
      return {
        title: post.title,
        description: post.excerpt,
        openGraph: {
          images: [post.image],
        },
      };
    }
    ```
    `generateMetadata` runs on the server during rendering. It receives the same `params` and `searchParams` as the page. The returned metadata is used for that specific page instance. This is the dynamic equivalent of the static `metadata` export.

- id: nextjs-metadata-inherit-03
  answer: |
    Metadata composes across nested layouts and pages. If a layout defines `metadata.title = 'My App'` and a page defines `metadata.title = 'About'`, the page's title wins (page overrides layout). For fields not overridden, the layout's values are inherited. `openGraph` and `twitter` objects merge deeply.

    `title.template` allows you to define a title pattern in a layout that child segments can use:
    ```tsx
    // app/layout.tsx
    export const metadata = {
      title: {
        default: 'My App',
        template: '%s | My App',
      },
    };
    // app/about/page.tsx
    export const metadata = { title: 'About' };
    // Renders: "About | My App"
    ```
    The `%s` is replaced by the child's title. This lets you set a site-wide prefix/suffix without repeating it in every page.

- id: nextjs-metadata-files-04
  answer: |
    Next.js supports file-based metadata conventions in the `app/` directory:
    - **Favicon**: `app/icon.ico` (or `.png`, `.svg`, `.jpg`) — automatically used as the favicon.
    - **Apple icon**: `app/apple-icon.png`
    - **Open Graph image**: `app/opengraph-image.png` (or `.jpg`) — automatically used as the OG image. Can also use `opengraph-image.tsx` to generate it dynamically.
    - **Twitter image**: `app/twitter-image.png`
    - **Sitemap**: `app/sitemap.ts` (or `.js`, `.xml`) — exports a `sitemap` function returning an array of URLs. Next.js generates `/sitemap.xml`.
    - **Robots**: `app/robots.ts` (or `.js`, `.txt`) — exports a `robots` function returning rules. Next.js generates `/robots.txt`.
    - **Manifest**: `app/manifest.ts` — for PWA web app manifests.
    - **Viewport**: `app/viewport.ts` — for theme color, viewport settings.

    These files are automatically detected and served at the corresponding routes.

- id: nextjs-navigation-link-01
  answer: |
    Use the `<Link>` component from `next/link`:
    ```tsx
    import Link from 'next/link';
    <Link href="/about">About</Link>
    ```
    What `<Link>` does that a plain `<a>` doesn't:
    - **Client-side navigation**: Transitions between routes without a full page reload (no browser refresh). Only the changed segments re-render.
    - **Prefetching**: Automatically prefetches the target route's data in the background (when the link enters the viewport in production). This makes navigation instant.
    - **Router cache**: Uses the client-side router cache for prefetched routes, enabling instant back/forward navigation.
    - **Scroll preservation**: Maintains scroll position by default.
    - **Partial rendering**: Only the layout and page that change are re-rendered; shared layouts persist.

    Use `<a>` for external links or when you explicitly want a full page reload.

- id: nextjs-navigation-hooks-02
  answer: |
    The App Router navigation hooks come from `next/navigation`:
    - `useRouter()` — programmatic navigation (`router.push`, `router.replace`, `router.back`, `router.refresh`).
    - `usePathname()` — current pathname.
    - `useSearchParams()` — current query parameters.
    - `useParams()` — dynamic route params.

    These hooks fail in Server Components because they rely on client-side React context (the router state, browser history, etc.) which only exists in the browser. Server Components render on the server where there is no browser history or navigation state. The fix: use these hooks only in Client Components (add `"use client"`). For server-side access to URL data, use `headers()`, `cookies()`, or the `searchParams`/`params` props passed to pages.

- id: nextjs-navigation-redirect-03
  answer: |
    `redirect(path)` from `next/navigation` performs a navigation to the given path. It can be called in Server Components, Server Actions, and Route Handlers. It works by throwing a special error that Next.js catches to perform the redirect. After calling `redirect()`, the rest of the function does NOT execute (the throw stops execution).

    `notFound()` triggers the closest `not-found.tsx` UI (or the default 404 page). It also throws internally. Use it when a resource doesn't exist.

    Gotchas:
    - Both `redirect()` and `notFound()` throw internally — code after them won't run. Don't put logic after them.
    - `redirect()` in a Server Component only works during rendering, not in event handlers. In event handlers, use `router.push()` or return the redirect from a Server Action.
    - `redirect()` defaults to a 307 (temporary) status code. Use `redirect(path, 'permanent')` or `permanentRedirect()` for 308.
    - In Server Actions, `redirect()` should be called after the mutation succeeds, and you should wrap it in try/catch to avoid catching the redirect error.

- id: nextjs-navigation-action-redirect-04
  answer: |
    After a successful Server Action, call `redirect()` from `next/navigation` at the end of the action:
    ```tsx
    "use server";
    import { redirect } from 'next/navigation';
    export async function createPost(formData: FormData) {
      const post = await db.post.create({ data: { ... } });
      revalidatePath('/posts');
      redirect(`/posts/${post.id}`);
    }
    ```
    What to watch out for:
    - `redirect()` throws internally — wrap it in try/catch if you have cleanup code, or ensure it's the last statement.
    - If you catch errors in the action, don't accidentally catch the redirect error. Use `try/catch` around only the mutation, not the redirect, or re-throw the redirect error.
    - The redirect happens after the action completes and the UI updates. The user sees the new page.
    - In Next.js 15+, `redirect()` in a Server Action returns a promise that resolves when the redirect is processed — you can `await` it if needed.
    - If the action is called from a form, the router handles the redirect response automatically. If called from a client component event handler, the redirect response triggers a client-side navigation.
- id: nextjs-data-fetching-nogssp-02
  answer: >
    Use Promise.all (or Promise.allSettled) to fire all independent fetches concurrently
    instead of awaiting them sequentially. In an async Server Component, start each fetch
    without awaiting, collect the promises, then await them together. This ensures all
    requests are in flight simultaneously rather than waiting for each to complete before
    the next starts, eliminating the waterfall.
