```yaml
- id: nextjs-app-router-conventions-01
  answer: |
    - `layout.tsx` wraps its children and persists across navigations (won't re-render on route change). Each segment can have one.
    - `page.tsx` defines the unique UI for a route segment. Required to make a segment publicly accessible.
    - `loading.tsx` shows an instant loading UI (typically a skeleton/spinner) while the page's server component is streaming in. It wraps the page in a `<Suspense>` boundary automatically.
    - `error.tsx` catches runtime errors for its segment (and children). It must be a Client Component ("use client"). It renders a fallback UI and provides a `reset()` to retry.
    - `route.ts` defines a server-side API endpoint (GET, POST, etc.) for that segment. Cannot coexist with `page.tsx`.
    Folders map to URL segments: `app/dashboard/settings/page.tsx` → `/dashboard/settings`.

- id: nextjs-app-router-layout-02
  answer: |
    On navigation, nested layouts **do not re-render**. They persist — only the `page.tsx` below the shared layout re-renders. This is great for keeping shared UI (nav, sidebar) stable.

    `template.tsx` is like `layout.tsx` but **re-renders on every navigation**. It receives a new `key` prop on each render (just like React's `<template>` concept). Use it when you need fresh mount/unmount behavior per navigation (e.g. animations, scroll-to-top, resetting form state) rather than persistence.

- id: nextjs-app-router-error-03
  answer: |
    An `error.tsx` file **must** be a Client Component — it needs `"use client"` at the top, because it handles runtime errors which can only be caught in the client boundary. It typically renders a fallback UI and exposes `reset()`.

    It catches errors thrown during rendering of its segment and any nested segments below it (the `page.tsx` and its children). It does **not** catch errors in the layout above it — a layout's error boundary is its parent's `error.tsx`.

    It also does not catch errors during server-side data fetching that happens before rendering (those become HTTP error responses or are caught by `notFound()` / error boundaries higher up), nor does it catch errors in `generateMetadata` or route handlers.

- id: nextjs-app-router-loading-04
  answer: |
    Adding `loading.tsx` to a route segment automatically wraps the segment's content in a React `<Suspense>` boundary. When the page (or nested layouts) are still loading/awaiting on the server, the `loading.tsx` component is streamed to the client immediately as an instant loading indicator. Once the content is ready, it streams in and replaces the loading UI. Mechanically it's just `<Suspense fallback={<Loading />}>` — Next.js generates that wrapper for you.

- id: nextjs-server-components-default-01
  answer: |
    All components in the App Router are **Server Components by default**. You don't need to add any directive to make a component a Server Component.

    `"use client"` marks the boundary where a component becomes a Client Component. It tells the bundler: "this file and everything it imports (until the next boundary) runs on the client." It essentially opts that component (and its subtree below the boundary) into the client-side React runtime.

- id: nextjs-server-components-hooks-02
  answer: |
    `useState`, `useEffect`, `onClick`, and `window` are all client-only APIs. Server Components render once on the server and are sent as serialized HTML/RSC payload — there's no client React runtime, no re-renders, no event handlers, no browser APIs available.

    The fix: add `"use client"` to the top of the component file that needs these hooks/events/DOM APIs. Alternatively, extract the interactive part into a separate Client Component file and import it from your Server Component. This keeps the server/client boundary clean.

- id: nextjs-server-components-props-03
  answer: |
    A Server Component can only pass **serializable** props to a Client Component: strings, numbers, booleans, plain objects, arrays, and Server Components (via the `children` prop or props). You **cannot** pass functions, classes, or non-serializable values (like Date objects, Maps, etc.).

    The idiomatic pattern for keeping a server-rendered subtree inside a client component is to use `children`: the Client Component accepts `children` as a prop, and you nest Server Components inside it. This works because Server Components passed as children are pre-rendered on the server and serialized — the client component never needs to execute them.

- id: nextjs-data-fetching-rsc-01
  answer: |
    In the App Router you fetch data directly inside Server Components using `async/await` with any data-fetching method (fetch, DB query, etc.). You just make the component `async` and await your data at the top.

    `getServerSideProps` is replaced by just fetching data in the Server Component itself (which runs on the server every request by default in dynamic mode).

    `getStaticProps` is replaced by `fetch` in Server Components with caching + `revalidate`, or by using `generateStaticParams` for dynamic routes, or by `export const revalidate = N` for ISR. The App Router's fetch cache and `revalidate` options handle what `getStaticProps` + `revalidate` did before.

- id: nextjs-data-fetching-nogssp-02
  answer: |
    Use `Promise.all()`. Since Server Components are just async functions, you kick off all the fetch promises concurrently before awaiting them:

    ```tsx
    export default async function Page() {
      const [users, posts, comments] = await Promise.all([
        fetchUsers(),
        fetchPosts(),
        fetchComments(),
      ]);
      // render
    }
    ```

    This avoids the sequential waterfall. Without `Promise.all`, each `await` blocks before starting the next fetch.

- id: nextjs-caching-fetch-default-01
  answer: |
    **Next.js 14 and earlier**: Yes, `fetch(url)` in a Server Component is cached by default (the "full" route cache was on by default).

    **Next.js 15**: No. `fetch` requests are **no longer cached by default**. You must explicitly opt into caching with `{ next: { revalidate: N } }` or `cache: 'force-cache'` on the fetch call. This was a deliberate change to reduce confusion around stale data.

- id: nextjs-caching-layers-02
  answer: |
    The main caching layers in the App Router:

    1. **Request Memoization** — Within a single server render pass, duplicate `fetch()` calls with the same URL/options are deduplicated. Scoped to one request.
    2. **Data Cache (Router Cache)** — Persists the results of `fetch` calls across requests and deployments. Configured via `revalidate` or `cache` options on fetch. Stored server-side.
    3. **Full Route Cache** — Caches the rendered output (RSC payload + HTML) of static routes at build time or after first render. Served from the edge/CDN. Invalidated by `revalidatePath`/`revalidateTag`.
    4. **Client-side Router Cache** — Caches RSC payloads in the browser for instant navigation. Pre-fetches on `<Link>` hover. Invalidated on navigation or hard refresh.

- id: nextjs-caching-revalidate-03
  answer: |
    `revalidate` sets a time window (in seconds) after which a cached entry becomes stale. Once stale, the next request will serve the old data **and** trigger a background re-render to refresh the cache (stale-while-revalidate pattern).

    Time-based revalidation (ISR) differs from fully static content in that fully static pages are generated once at build time and never update unless you rebuild. ISR pages are generated on first request, cached, then automatically regenerated in the background after the `revalidate` interval expires.

- id: nextjs-caching-ondemand-04
  answer: |
    `revalidatePath('/path')` immediately invalidates the cached render for a specific route (or all routes if called without args). `revalidateTag('tag-name')` invalidates all cached fetches that used that tag (you tag fetches with `{ next: { tags: ['tag-name'] } }`).

    Call them inside a **Server Action** or **Route Handler** after your mutation completes. They are synchronous and must be called before the response is sent:

    ```ts
    'use server';
    import { revalidatePath, revalidateTag } from 'next/cache';

    export async function createPost(formData: FormData) {
      await db.posts.create({ ... });
      revalidatePath('/posts');    // or revalidateTag('posts');
    }
    ```

- id: nextjs-caching-segment-config-05
  answer: |
    - `export const dynamic = 'force-dynamic'` tells Next.js to always render the route dynamically (on every request), disabling the full route cache. Useful when the page depends on auth, cookies, headers, or user-specific data that changes per request.
    - `export const revalidate = N` sets the ISR revalidation interval for that route segment — the page is statically generated and revalidated every N seconds.

    `dynamic = 'force-dynamic'` is the explicit override when Next.js would otherwise try to statically render a page (e.g. it detects `cookies()` or `headers()` usage and switches to dynamic, but this makes it explicit).

- id: nextjs-rendering-static-dynamic-01
  answer: |
    Next.js decides statically if it can determine the content at build time or after the first request with no dynamic dependencies. A route is **dynamically** rendered if it:

    - Uses `cookies()`, `headers()`, or `searchParams` in a Server Component
    - Uses `export const dynamic = 'force-dynamic'`
    - Calls a Server Action
    - Uses `next/headers` APIs
    - Has an uncached `fetch` (in Next.js 15+)
    - Is in development mode (dynamic by default in dev)

    Otherwise, it's **statically** rendered — the HTML/RSC is generated once and cached.

- id: nextjs-rendering-static-params-02
  answer: |
    `generateStaticParams` returns an array of param objects that tell Next.js which pages to pre-render at build time for dynamic routes. For `app/blog/[slug]`, you'd return `[{ slug: 'post-1' }, { slug: 'post-2' }]` and Next.js builds those two pages statically.

    The Pages Router equivalent is `getStaticPaths`, which served the same purpose of enumerating which paths to pre-render for dynamic `getStaticProps` routes.

- id: nextjs-rendering-dynamic-apis-03
  answer: |
    In Next.js 15+, `cookies()`, `headers()`, `params`, and `searchParams` are **async** and must be awaited. This is because they now read from the underlying request in a way that requires dynamic I/O — they can't be resolved synchronously at the module level.

    The effect of reading them in a page is that it **switches that route segment to dynamic rendering**. It opts out of the full route cache for that segment, because the content now depends on request-specific data that changes per request.

- id: nextjs-server-actions-useserver-01
  answer: |
    `"use server"` marks a function (or all exports in a file) as a Server Action. It tells the bundler to create a server-side endpoint for that function, allowing Client Components to call it directly — the function runs on the server, not the client.

    `"use client"` marks a component boundary where code runs in the browser (client-side React runtime). It's about where the component lives and renders.

    Key difference: `"use server"` is about **server-side function execution** callable from the client. `"use client"` is about **client-side component rendering**.

- id: nextjs-server-actions-mutation-02
  answer: |
    1. Create a Server Action file (e.g. `app/actions.ts`) with `'use server'` at the top.
    2. Write the async function: validate input, write to DB, call `revalidatePath('/posts')` to bust cache.
    3. In a Client Component, wire it up with `useFormState` (or `useActionState` in Next.js 15) for feedback, or call it directly from `onSubmit` / a button `onClick`.
    4. The action runs on the server, the UI automatically re-renders with the updated data (thanks to `revalidatePath` invalidating the cache, which triggers a re-fetch of the Server Component).

    ```tsx
    // app/actions.ts
    'use server';
    import { revalidatePath } from 'next/cache';
    export async function createPost(prev: any, formData: FormData) {
      await db.posts.create({ title: formData.get('title') });
      revalidatePath('/posts');
    }

    // app/new-post/page.tsx (client)
    'use client';
    import { useActionState } from 'react';
    import { createPost } from '../actions';
    export default function NewPost() {
      const [state, formAction] = useActionState(createPost, null);
      return <form action={formAction}><input name="title" /><button>Add</button></form>;
    }
    ```

- id: nextjs-server-actions-security-03
  answer: |
    Server Actions are exposed as **public HTTP endpoints** — anyone can POST to them directly, even without going through your UI. They're not hidden behind authentication just because they're called from your app.

    **Gotcha**: You must **validate all inputs** inside every action. Never trust that the caller is your own form. Always check authentication/authorization inside the action. Think of every Server Action as a public API endpoint:

    ```ts
    'use server';
    export async function deletePost(postId: string) {
      const user = await getUser(); // auth check
      if (!user) throw new Error('Unauthorized');
      // validate postId format
      await db.posts.delete(postId);
      revalidatePath('/posts');
    }
    ```

- id: nextjs-route-handlers-basics-01
  answer: |
    A Route Handler is a file named `route.ts` (or `route.js`) inside the App Router that exports named functions for HTTP methods: `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `HEAD`, `OPTIONS`.

    It replaces `pages/api/*.ts` from the Pages Router. Unlike Pages Router API routes which exported a default function receiving `(req, res)`, Route Handlers export named method handlers and use the Web `Request`/`Response` APIs (standard Fetch API):

    ```ts
    // app/api/posts/route.ts
    export async function GET() { return Response.json({ posts: [] }); }
    export async function POST(request: Request) { /* ... */ }
    ```

- id: nextjs-route-handlers-caching-02
  answer: |
    **Next.js 14 and earlier**: `GET` Route Handlers with a static URL are cached by default (similar to static page rendering).

    **Next.js 15**: `GET` Route Handlers are **no longer cached by default**. They run dynamically. To opt into caching, use the `dynamic` segment config: `export const dynamic = 'force-static'` or `export const revalidate = 60`.

- id: nextjs-route-handlers-methods-03
  answer: |
    ```ts
    import { NextRequest } from 'next/server';

    export async function GET(
      request: NextRequest,
      { params }: { params: Promise<{ slug: string }> }
    ) {
      // Request body (for POST/PUT):
      const body = await request.json();

      // Query params:
      const searchParams = request.nextUrl.searchParams;
      const q = searchParams.get('q');

      // Dynamic route params (Next.js 15+, params is a Promise):
      const { slug } = await params;
    }
    ```

    Use `NextRequest` for convenience (it extends `Request` with `nextUrl`), or use the standard `Request`. Params are passed as the second argument and are a `Promise` in Next.js 15+.

- id: nextjs-streaming-ssr-01
  answer: |
    Streaming SSR means the server starts sending HTML/RSC payload as soon as the initial shell is ready, **without waiting for all data fetching to complete**. Slow data sections load in later chunks.

    Benefits over traditional SSR (waiting for everything):
    - **Lower TTFB (Time to First Byte)**: The user sees the page skeleton/layout immediately instead of staring at a blank screen.
    - **Better perceived performance**: Loading indicators appear instantly.
    - **Progressive rendering**: Fast parts render first; slow parts fill in via Suspense.

    It's enabled automatically via `loading.tsx` (Suspense) or manual `<Suspense>` boundaries.

- id: nextjs-streaming-suspense-02
  answer: |
    Wrap the slow section in `<Suspense fallback={<Skeleton />}>`. The surrounding page renders and streams immediately, while the slow component loads in the background and replaces the skeleton when ready:

    ```tsx
    export default async function Page() {
      return (
        <div>
          <Header /> {/* fast */}
          <Suspense fallback={<Skeleton />}>
            <SlowDataSection /> {/* async, streams in later */}
          </Suspense>
        </div>
      );
    }
    ```

    Or use `loading.tsx` at the route segment level for the whole page.

- id: nextjs-streaming-boundary-03
  answer: |
    `loading.tsx` is automatically applied by Next.js to the route segment — it wraps the segment's content (page + nested layouts) in a `<Suspense>` boundary. You get it for free.

    Manual `<Suspense>` boundaries let you be **granular** — you choose exactly which components are deferred while others render immediately. You can have multiple Suspense boundaries on one page, each with its own loading state, so fast parts appear instantly and only the specific slow parts show spinners.

    Use `loading.tsx` for the whole page; use `<Suspense>` when you want fine-grained streaming within a page.

- id: nextjs-metadata-static-01
  answer: |
    Export a `metadata` object or `generateMetadata` function from any `layout.tsx` or `page.tsx`:

    ```tsx
    export const metadata: Metadata = {
      title: 'My Page',
      description: 'A description',
      openGraph: { title: 'OG Title' },
    };
    ```

    Or use `generateMetadata` for dynamic values. Metadata from child routes merges with (and overrides) parent layout metadata. You can also use the `generateMetadata` async function.

- id: nextjs-metadata-dynamic-02
  answer: |
    Use `generateMetadata` — an async function that can fetch data before returning the metadata object:

    ```tsx
    export async function generateMetadata({ params }: { params: Promise<{ slug: string }> }) {
      const post = await fetchPost((await params).slug);
      return {
        title: post.title,
        description: post.excerpt,
        openGraph: { title: post.title, images: [post.coverImage] },
      };
    }
    ```

    It runs on the server, can be async, and is called before the page renders (or at build time for static routes).

- id: nextjs-metadata-inherit-03
  answer: |
    Metadata **merges recursively** across nested layouts and pages. A child `page.tsx` metadata overrides the same fields from its parent `layout.tsx`, but non-conflicting fields are inherited.

    `title.template` lets you define a template in a parent layout that child routes fill in:
    ```tsx
    export const metadata = { title: { template: '%s | My Site', default: 'My Site' } };
    ```
    A child page with `title: 'About'` produces `<title>About | My Site</title>`. The `default` is used when no child sets a title.

- id: nextjs-metadata-files-04
  answer: |
    Use the special **Metadata File Conventions** — place these files directly in `app/`:

    - `favicon.ico`, `icon.svg`, `apple-icon.png` → auto-detected as favicons/icons
    - `opengraph-image.png` (or `.jpg`, `.webp`) → OG image
    - `twitter-image.png` → Twitter card image
    - `sitemap.ts` / `sitemap.xml` → sitemap generation
    - `robots.ts` / `robots.txt` → robots.txt generation
    - `manifest.json` / `manifest.webmanifest` → PWA manifest
    - `sitemap.xml` static file works too

    These are file-based conventions — no configuration needed, just place the file in `app/`.

- id: nextjs-navigation-link-01
  answer: |
    Use the `next/link` component: `<Link href="/about">About</Link>`. For programmatic navigation, use `useRouter()` from `next/navigation`: `router.push('/about')`.

    `<Link>` does client-side navigation (soft navigation) — it prefetches the route's RSC payload on hover/viewport entry and navigates without a full page reload. A plain `<a>` tag causes a full page reload (hard navigation), loses client state, and doesn't prefetch.

- id: nextjs-navigation-hooks-02
  answer: |
    They come from **`next/navigation`** (not `next/router` which was the Pages Router):

    ```tsx
    import { useRouter, usePathname, useSearchParams } from 'next/navigation';
    ```

    They fail in Server Components because they rely on client-side React state (the current URL, navigation history, etc.) which only exists in the browser. Server Components execute once on the server with no browser context. The fix: use `"use client"` in the component, or extract these hooks into a Client Component.

- id: nextjs-navigation-redirect-03
  answer: |
    - `redirect('/new-path')` throws an error internally and **redirects** the user to the specified URL. It can be called from Server Components, Server Actions, and Route Handlers.
    - `notFound()` throws an error that triggers the closest `not-found.tsx` page (or the default 404).

    **Gotcha**: Both `redirect()` and `notFound()` work by **throwing** under the hood. If you call `redirect()` inside a try/catch, the catch block runs, which can be confusing. Also, you can't `return redirect(...)` — it throws before returning. In Server Actions, `redirect()` after a mutation triggers a fresh server render of the target page.

- id: nextjs-navigation-action-redirect-04
  answer: |
    Call `redirect('/posts/' + newPost.id)` at the end of your Server Action. It works inside Server Actions just like in Server Components:

    ```ts
    'use server';
    export async function createPost(formData: FormData) {
      const post = await db.posts.create({ ... });
      redirect('/posts/' + post.id);
    }
    ```

    **Watch out**: `redirect()` throws, so if you have cleanup code or logic after it, wrap in try/catch. Also, calling `redirect` in a Server Action triggered from a form does a server-side redirect — the browser navigates to the new URL and re-fetches the Server Component tree for that route. Don't put `redirect` before your mutation — it skips the DB write if it throws first.
```
