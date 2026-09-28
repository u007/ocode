- id: nextjs-app-router-conventions-01
  answer: |
    Each special file has a distinct role:
    - `layout.tsx`: Persists UI shared across a route segment and all its children (e.g., nav, sidebar). Not re-rendered on navigation within that segment.
    - `page.tsx`: The unique UI for a route segment. The presence of a `page` file is what makes a folder a publicly accessible URL.
    - `loading.tsx`: Creates an automatic Suspense boundary that shows a loading fallback while the segment's page content is loading or streaming.
    - `error.tsx`: Creates an error boundary that catches rendering errors in that segment and its children, showing a fallback UI.
    - `route.tsx`: Defines an API endpoint (HTTP method handlers) for that segment.

    A folder maps directly to a URL path: `app/blog/[slug]/page.tsx` maps to `/blog/:slug`. Nested folders create nested URL segments. A folder without a `page` file is not directly reachable as a URL.

- id: nextjs-app-router-layout-02
  answer: |
    Nested layouts persist across navigation. When you navigate between routes that share a parent layout, that parent layout is not re-rendered (React keeps its state). Only the child segment (the `page`) re-renders. For example, `app/dashboard/layout.tsx` and `app/dashboard/settings/layout.tsx` — navigating from `/dashboard` to `/dashboard/settings` preserves the outer layout.

    `template.tsx` differs from `layout.tsx` in that it creates a **new instance** on every navigation. This means state is reset and effects re-run. Use `template` when you need a fresh mount (e.g., animations that should replay), and `layout` when you need persistence.

- id: nextjs-app-router-error-03
  answer: |
    An `error.tsx` file **must be a Client Component** (it needs to handle errors interactively and maintain its own error state).

    It catches errors that occur in the **same segment's** `page`, `layout`, and `loading` files — and in their child components during rendering.

    It does **not** catch:
    - Errors in parent layouts (those are caught by the parent's `error.tsx`)
    - Errors in `error.tsx` itself (you need a higher-level boundary)
    - Errors in route handlers or server actions (those throw and return error responses)
    - Errors in client components during event handlers (those are caught by React error boundaries)

- id: nextjs-app-router-loading-04
  answer: |
    Adding `loading.tsx` to a route segment automatically wraps that segment in a **Suspense boundary**. While the segment's `page` (or nested content) is loading — whether fetching data, waiting for dynamic imports, or streaming — React renders the `loading.tsx` fallback instead of blocking the entire page. The rest of the page (parent layouts, sibling segments) can still render immediately. This gives you per-segment loading states without manual Suspense management.

- id: nextjs-server-components-default-01
  answer: |
    Components in the App Router are **Server Components by default**. There is no directive needed.

    `"use client"` marks the **boundary** between server and client code. It doesn't make the whole file a client component in isolation — it tells Next.js "everything imported from this point onward should be client-rendered." The file itself can still import and render server components (passed as children), but the component where the directive is placed and its children (unless they have their own `"use server"` boundary) will be client components.

- id: nextjs-server-components-hooks-02
  answer: |
    Server Components render **on the server, once, to HTML**. They have no browser runtime, no event loop, and no persistent state between renders. `useState`/`useEffect` require a stateful, long-lived client component instance. `onClick` and `window` require a DOM that doesn't exist on the server.

    The fix: add `"use client"` at the top of the component file. This marks it (and its children) as a Client Component that runs in the browser, where hooks, interactivity, and browser APIs work. Alternatively, keep the interactive part as a small child Client Component and pass it as `children` or a prop to the Server Component.

- id: nextjs-server-components-props-03
  answer: |
    Server Components can pass **serializable props** to Client Components: strings, numbers, booleans, null, undefined, plain objects, arrays, dates (serialized as strings), and React elements (JSX). They **cannot** pass functions (unless via Server Actions with `"use server"`), class instances, or non-serializable objects.

    The idiomatic way to keep a server-rendered subtree inside a client component is to **pass the server component as `children`** (or as a regular prop that happens to be JSX):

    ```tsx
    // ClientComponent.tsx
    "use client";
    export default function ClientComponent({ children }) {
      return <div className="interactive">{children}</div>;
    }

    // ServerComponent.tsx (server)
    export default function ServerComponent() {
      return <ClientComponent><ServerContent /></ClientComponent>;
    }
    ```

    The `<ServerContent />` is rendered on the server and passed as a prop, preserving server rendering without breaking the client boundary.

- id: nextjs-data-fetching-rsc-01
  answer: |
    You fetch data **directly inside an async Server Component** using standard `async/await`:

    ```tsx
    async function Page() {
      const data = await fetch('https://api.example.com/data').then(r => r.json());
      return <div>{data.title}</div>;
    }
    ```

    This replaces `getServerSideProps` / `getStaticProps` / `getInitialProps` from the Pages Router. Those functions **do not exist in the `app/` directory**. Instead:
    - Static rendering happens when the fetch result is cacheable (or explicitly cached).
    - Dynamic rendering happens when dynamic APIs (`cookies()`, `headers()`, etc.) are used or caching is disabled.
    - You control caching via fetch options (`cache: 'force-cache'`, `next: { revalidate: N }`) or route segment config.

- id: nextjs-data-fetching-nogssp-02
  answer: |
    Two approaches to avoid request waterfalls with independent data sources:

    1. **Start independent requests together and `await Promise.all([...])`**: If all the data is needed in one component, fire all fetches concurrently rather than sequentially awaiting each one.

    2. **Move each slow, independent fetch into its own async Server Component wrapped in its own `<Suspense>` boundary**: This lets each piece load and stream independently. The fast parts render immediately without waiting for the slow parts. Each component manages its own data fetching and loading state.

- id: nextjs-caching-fetch-default-01
  answer: |
    **Version-dependent:**

    - **Next.js 13 and 14**: `fetch` is cached **by default** (`force-cache`). Data is stored in the Data Cache and reused across requests unless explicitly opted out.

    - **Next.js 15+ (including 16)**: `fetch` is **NOT cached by default**. It behaves like `no-store` — a fresh request on every render. You must opt in per request with `cache: 'force-cache'` or `next: { revalidate: N }` to enable caching.

    This is a critical difference: "Next 15 caches fetch by default" is false — it is the reverse.

- id: nextjs-caching-layers-02
  answer: |
    1. **Request Memoization**: Deduplicates fetches with the same URL and options during a single render pass (one request, multiple consumers). Lives for the duration of one render.

    2. **Data Cache**: Persists the results of `fetch` (and other data) across requests and deployments. This is the persistent server-side cache that `revalidate` and `revalidateTag` operate on.

    3. **Full Route Cache**: Caches the rendered HTML and RSC payload of entire routes at build time (for statically generated pages). Bypassed when a route is dynamic.

    4. **Router Cache**: Client-side cache of route segments that have been visited. Enables instant client-side navigation for previously seen routes. Invalidated by revalidation.

- id: nextjs-caching-revalidate-03
  answer: |
    `revalidate` sets a **time-based revalidation** period (in seconds) on cached data or an entire route. After the stale period expires, the next request triggers a background revalidation: the cached (stale) data is served immediately, and the data is refreshed in the background for subsequent requests.

    This is **Incremental Static Regeneration (ISR)** — different from serving fully static content because:
    - **Fully static**: Built once at build time and served forever until the next deployment. No revalidation.
    - **ISR with revalidate**: Built once, then periodically regenerated. You get static-like performance with the ability to update content without a full rebuild.

- id: nextjs-caching-ondemand-04
  answer: |
    After a mutation, you call one of these inside a **Server Action** or **Route Handler**:

    - `revalidatePath(path, 'page' | 'layout')`: Invalidates the cached data and Router Cache for a specific path. Use when you know which URL was affected but not which fetch tags were involved.

    - `revalidateTag(tag)`: Invalidates all cached fetches that were tagged with the given tag (via `next: { tags: [...] }` in the fetch options). Use when multiple routes depend on the same data and you want to invalidate all of them at once.

    Example: after creating a blog post in a Server Action, call `revalidateTag('posts')` to invalidate all pages that fetched posts, or `revalidatePath('/blog')` to refresh the blog index.

- id: nextjs-caching-segment-config-05
  answer: |
    - `export const dynamic`: Controls whether a route is statically or dynamically rendered. Values include `'force-static'`, `'force-dynamic'`, `'error'` (fail if dynamic APIs are used), and `'auto'` (default — let Next.js decide).

    - `export const revalidate`: Sets the time-based revalidation period (in seconds) for the route. After this period, the route is regenerated in the background (ISR).

    You'd set `dynamic = 'force-dynamic'` when you want to **opt out of static rendering entirely** — for example, when the page depends on per-request data like `cookies()` or `headers()`, or when you explicitly want a fresh server render on every request without caching.

- id: nextjs-rendering-static-dynamic-01
  answer: |
    Next.js uses **static rendering by default**. A route is statically rendered at build time if it doesn't use dynamic data.

    A route becomes **dynamically rendered** (on-demand, per request) when:
    - It uses dynamic APIs: `cookies()`, `headers()`, `searchParams` (without `generateStaticParams`)
    - It uses `export const dynamic = 'force-dynamic'`
    - Its fetches are uncached (`cache: 'no-store'`)

    The decision is made per-route at build time. If a route has no dynamic characteristics, it's pre-rendered. If it reads request-time data, it's rendered on each request.

- id: nextjs-rendering-static-params-02
  answer: |
    `generateStaticParams` pre-generates a list of static paths at build time for a dynamic route like `app/blog/[slug]/page.tsx`. You export an async function that returns an array of objects (e.g., `[{ slug: 'hello' }, { slug: 'world' }]`), and Next.js renders static HTML for each one.

    The Pages Router equivalent is **`getStaticPaths`**, which returns `{ paths: [...], fallback: ... }`. The key difference is that `generateStaticParams` is simpler (no `fallback` option in the same way) and integrates with the App Router's caching model.

- id: nextjs-rendering-dynamic-apis-03
  answer: |
    In Next.js 15+, `cookies()`, `headers()`, `params`, and `searchParams` are now **async functions** (they return Promises). You must `await` them:

    ```tsx
    const cookieStore = await cookies();
    const { slug } = await params;
    ```

    The effect of reading them in a page: the route is **opted into dynamic rendering**. The page will be rendered on-demand for each request rather than statically generated at build time, because the values are only known at request time.

- id: nextjs-server-actions-useserver-01
  answer: |
    `"use server"` marks functions as **Server Actions** — functions that execute on the server but can be called directly from client code. They are invoked via form `action` attributes or programmatic calls, and Next.js exposes them as POST endpoints.

    This is different from `"use client"`, which marks the **client-side boundary** — telling the bundler that this component (and its children) should be hydrated and run in the browser. `"use server"` does the opposite: it ensures the function runs only on the server, never in the browser.

    A file with `"use server"` at the top makes every exported function in that file a Server Action.

- id: nextjs-server-actions-mutation-02
  answer: |
    1. Create an async function with `"use server"`:

    ```tsx
    "use server";
    export async function createPost(formData: FormData) {
      const title = formData.get('title');
      await db.post.create({ data: { title } });
      revalidatePath('/posts');
    }
    ```

    2. Call it from a form in a Client Component:

    ```tsx
    "use client";
    import { createPost } from './actions';
    export default function NewPostForm() {
      return (
        <form action={createPost}>
          <input name="title" />
          <button type="submit">Create</button>
        </form>
      );
    }
    ```

    3. Use `useTransition` or `useFormStatus` to show pending state during the action.

    4. After the mutation succeeds, `revalidatePath` ensures the UI reflects the new data. You can also call `redirect()` to navigate.

- id: nextjs-server-actions-security-03
  answer: |
    The trap: a Server Action feels like calling a local function, but it's actually exposed as a **public POST endpoint** on your server. Anyone can call it directly — not just your UI. This means you cannot trust that the caller is authorized or that the input is valid.

    Inside every Server Action you **must**:
    - **Verify authorization**: Check that the current user is authenticated and permitted to perform the action (e.g., using `cookies()` for session data or your auth library).
    - **Validate and sanitize input**: Never trust client input. Validate types, ranges, and formats server-side.
    - **Use parameterized queries**: Prevent SQL/NoSQL injection.

    Treat every Server Action as a public API endpoint because that's what it is.

- id: nextjs-route-handlers-basics-01
  answer: |
    A Route Handler (`route.ts` or `route.js`) defines API endpoints for a route segment in the App Router. Instead of `pages/api` directory, you place a `route.ts` file inside any folder under `app/`.

    ```tsx
    app/api/route.ts        → /api
    app/api/users/route.ts  → /api/users
    ```

    You export named functions for each HTTP method (`GET`, `POST`, `PUT`, `DELETE`, `PATCH`, `HEAD`, `OPTIONS`). This replaces the Pages Router's `pages/api` routes. Route Handlers give you full access to the Web `Request`/`Response` API and can be used for webhooks, form handling, and external API proxies.

- id: nextjs-route-handlers-caching-02
  answer: |
    **Version-specific:**

    - **Next.js 14 and earlier**: `GET` Route Handlers are **cached by default** (like `force-cache`).
    - **Next.js 15+**: `GET` Route Handlers are **NOT cached by default**. They are dynamically rendered unless you explicitly opt in.

    To opt into caching for a `GET` handler, use the route segment config:

    ```tsx
    export const dynamic = 'force-static';
    // or
    export const revalidate = 3600; // revalidate every hour
    ```

    Or use a fetch inside the handler with caching options.

- id: nextjs-route-handlers-methods-03
  answer: |
    - **Request body**: `await request.json()` for JSON bodies (or `request.formData()` for form submissions, `request.text()` for raw text).
    - **Query params**: Parse from the URL: `const { searchParams } = new URL(request.url); const q = searchParams.get('q');`
    - **Dynamic route params**: Passed as the second argument: `export async function GET(request: Request, { params }: { params: Promise<{ slug: string }> })` — note that in Next.js 15+, `params` is a Promise and must be awaited.

- id: nextjs-streaming-ssr-01
  answer: |
    Streaming SSR sends the HTML response to the client **in chunks** as each part of the page finishes rendering, rather than waiting for the entire page to render before sending anything.

    What it buys you:
    - **Faster Time to First Byte (TTFB)**: The browser receives and starts parsing HTML immediately, even if some data is still loading on the server.
    - **Progressive rendering**: Users see content sooner, improving perceived performance.
    - **No head-of-line blocking**: Slow data fetches don't delay the entire page.

    In the App Router, streaming is the default behavior for Server Components. Suspense boundaries determine what streams when.

- id: nextjs-streaming-suspense-02
  answer: |
    Wrap the slow data section in its own `<Suspense>` boundary with a loading fallback:

    ```tsx
    async function Page() {
      const fastData = await getFastData();
      return (
        <div>
          <FastSection data={fastData} />
          <Suspense fallback={<Skeleton />}>
            <SlowSection />
          </Suspense>
        </div>
      );
    }

    async function SlowSection() {
      const slowData = await getSlowData();
      return <div>{slowData}</div>;
    }
    ```

    The fast content renders and is sent to the client immediately. The slow section streams in later when its data resolves, without blocking the page shell or the fast content.

- id: nextjs-streaming-boundary-03
  answer: |
    - `loading.tsx`: Automatically creates a Suspense boundary for the **entire route segment** (the page and all its children). It's a convention-based, zero-config way to show loading state. The fallback replaces the whole page content.

    - Your own `<Suspense>` boundary: Gives you **fine-grained control** over which part of the UI shows a loading state. You can wrap a specific component, choose a custom fallback, and keep the rest of the page interactive and visible. Multiple Suspense boundaries can coexist on one page.

    Use `loading.tsx` for full-page or full-segment loading states. Use manual `<Suspense>` for partial-page loading where you want to preserve the rest of the UI.

- id: nextjs-metadata-static-01
  answer: |
    Export a `metadata` object from a `page.tsx` or `layout.tsx`:

    ```tsx
    export const metadata = {
      title: 'My Page',
      description: 'A description of my page',
      openGraph: {
        title: 'My Page',
        description: 'A description',
        images: ['/og.png'],
      },
    };
    ```

    This is a **Server Component only** feature — a `"use client"` file cannot export `metadata`. The `metadata` export and `generateMetadata` are only valid in Server Components.

- id: nextjs-metadata-dynamic-02
  answer: |
    Export an async function called `generateMetadata`:

    ```tsx
    export async function generateMetadata({ params }): Promise<Metadata> {
      const post = await getPost((await params).slug);
      return {
        title: post.title,
        description: post.excerpt,
        openGraph: {
          images: [post.image],
        },
      };
    }
    ```

    Fetches inside `generateMetadata` are **deduped with the page's own fetches** via Request Memoization — same URL and options in one render pass resolve once, so there's no double fetch.

- id: nextjs-metadata-inherit-03
  answer: |
    Metadata composes from **root to leaf, shallowly**. A child's `metadata` merges with the parent's, but only one level deep. If a child sets a nested object like `openGraph`, it **replaces** the parent's entire `openGraph` object (arrays like `openGraph.images` are replaced, not merged).

    `title.template` lets child titles inherit a prefix/suffix from the parent:

    ```tsx
    // Root layout
    export const metadata = {
      title: { default: 'My Site', template: '%s | My Site' },
    };

    // Child page
    export const metadata = { title: 'About' };
    // Renders as: "About | My Site"
    ```

    The `%s` placeholder is replaced by the child's title. This ensures consistent branding across nested routes.

- id: nextjs-metadata-files-04
  answer: |
    Next.js uses **file conventions** for metadata assets:
    - **Favicon**: `app/icon.png` (or `.ico`, `.svg`, `.jpg`) — automatically served at `/icon`.
    - **Open Graph image**: `app/opengraph-image.png` — referenced via metadata.
    - **Apple touch icon**: `app/apple-icon.png`.
    - **sitemap.xml**: `app/sitemap.ts` exporting a `sitemap` function (or a static `sitemap.xml` file).
    - **robots.txt**: `app/robots.ts` exporting a `robots` function (or static `robots.txt`).
    - **manifest.json**: `app/manifest.ts` exporting a `manifest` function (or static file).

    These files are served at their respective root paths automatically.

- id: nextjs-navigation-link-01
  answer: |
    Use the `Link` component from `next/link`:

    ```tsx
    import Link from 'next/link';
    <Link href="/about">About</Link>
    ```

    `<Link>` does things a plain `<a>` doesn't:
    - **Client-side navigation**: Transitions between routes without a full page reload (using the App Router's client-side routing).
    - **Prefetching**: Automatically prefetches linked pages (in production) so navigation is instant.
    - **Scroll management**: Preserves or resets scroll position as appropriate.
    - **Partial rendering**: Only re-renders the changed segments, not the entire page.

- id: nextjs-navigation-hooks-02
  answer: |
    The App Router navigation hooks (`useRouter`, `usePathname`, `useSearchParams`, `useParams`) come from **`next/navigation`**.

    They fail in Server Components because they rely on **client-side React state and the browser's history API** — they need to subscribe to navigation events and read the current URL, which only exists in the browser. Server Components render once on the server with no client-side runtime, so these hooks have no context to operate in.

    The fix: use them in Client Components (files with `"use client"`).

- id: nextjs-navigation-redirect-03
  answer: |
    - `redirect(path)`: Sends an HTTP redirect to the specified path. Used in Server Components, Server Actions, and Route Handlers.
    - `notFound()`: Renders the nearest `notFound` UI (usually a 404 page). Used in Server Components when a resource doesn't exist.

    **Gotcha**: Both functions **throw an internal error** that React catches to interrupt rendering. You cannot call them inside a `try/catch` block — the error is caught by Next.js's internal mechanism, not your catch block. Also, `redirect()` in a Server Action should be called after the mutation succeeds, and you should handle the case where the redirect target itself might fail.

- id: nextjs-navigation-action-redirect-04
  answer: |
    Call `redirect()` inside the Server Action after the mutation succeeds:

    ```tsx
    "use server";
    export async function createPost(formData: FormData) {
      const post = await db.post.create({ data: {...} });
      revalidatePath('/posts');
      redirect(`/posts/${post.id}`);
    }
    ```

    What to watch out for:
    - **Error handling**: If you wrap the action in `try/catch`, the `redirect()` call will throw a `NEXT_REDIRECT` error that your catch block might intercept. Use a flag or check the error type to avoid swallowing it.
    - **Revalidation order**: Call `revalidatePath` before `redirect` to ensure the target page has fresh data.
    - **Race conditions**: If the user submits multiple times, ensure your action is idempotent or use a pending state to prevent double submission.
