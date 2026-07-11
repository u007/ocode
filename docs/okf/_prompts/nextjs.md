# Nextjs — Kaizen blind answer sheet (questions only)

> **CLOSED-BOOK.** Answer every question from your own knowledge alone. You MUST
> NOT open, search, or otherwise access the Kaizen corpus — `questions.yaml`,
> `questions.md`, `scores/`, `derived/`, `meta.yaml`, or any file in this repo —
> nor look the answers up online. Doing so invalidates the evaluation.
>
> Answer each question **independently** (treat every item as a fresh context —
> no memory of earlier answers). If you are unsure, say so; do not guess to look
> complete. This measures what you actually know, not what you can retrieve.
>
> **Return format** — one YAML record per question so the grader can map answers
> back by id:
>
> ```yaml
> - id: <question-id>
>   answer: |
>     <your answer>
> ```

Total questions: 34

---

### nextjs-app-router-conventions-01

In the App Router, what do the special files `layout`, `page`, `loading`, `error`, and `route` each do, and how does a folder map to a URL?

### nextjs-app-router-layout-02

How do nested layouts behave on navigation, and how does `template.tsx` differ from `layout.tsx`?

### nextjs-app-router-error-03

What must be true of an `error.tsx` file, and what does it catch versus what it doesn't?

### nextjs-app-router-loading-04

What does adding a `loading.tsx` to a route segment do, mechanically?

### nextjs-server-components-default-01

In the App Router, are components Server or Client by default, and what does `"use client"` actually mark?

### nextjs-server-components-hooks-02

Why can't you call `useState`/`useEffect`, use `onClick`, or access `window` in a Server Component, and what's the fix?

### nextjs-server-components-props-03

What kinds of props can a Server Component pass into a Client Component, and what's the idiomatic way to keep a server-rendered subtree inside a client component?

### nextjs-data-fetching-rsc-01

How do you fetch data in the App Router, and what replaces `getServerSideProps` / `getStaticProps`?

### nextjs-data-fetching-nogssp-02

You have several independent data sources for one page. How do you avoid a request waterfall in a Server Component?

### nextjs-caching-fetch-default-01

Is `await fetch(url)` in a Server Component cached by default? Be explicit about the Next.js version.

### nextjs-caching-layers-02

Name the caching layers in the App Router and what each one caches.

### nextjs-caching-revalidate-03

What does `revalidate` do for cached data, and how is time-based revalidation (ISR) different from serving fully static content?

### nextjs-caching-ondemand-04

After a mutation you need cached pages to reflect the new data immediately. What are `revalidatePath` and `revalidateTag`, and where do you call them?

### nextjs-caching-segment-config-05

What do the route segment configs `export const dynamic` and `export const revalidate` control, and when would you set `dynamic = 'force-dynamic'`?

### nextjs-rendering-static-dynamic-01

How does Next.js decide whether a route is statically or dynamically rendered?

### nextjs-rendering-static-params-02

What does `generateStaticParams` do for a dynamic route like `app/blog/[slug]`, and what's the Pages Router equivalent?

### nextjs-rendering-dynamic-apis-03

In Next.js 15+, why do `cookies()`, `headers()`, `params`, and `searchParams` need `await`, and what's the effect of reading them in a page?

### nextjs-server-actions-useserver-01

What does the `"use server"` directive mark, and how is it different from `"use client"`?

### nextjs-server-actions-mutation-02

Walk through using a Server Action to handle a form submission that writes data and updates the UI.

### nextjs-server-actions-security-03

A Server Action feels like a local function call. Why is that a security trap, and what must you do inside every action?

### nextjs-route-handlers-basics-01

What is a Route Handler (`route.ts`), and what replaces the Pages Router's `pages/api` routes?

### nextjs-route-handlers-caching-02

Is a `GET` Route Handler cached by default? Give the version-specific answer and how to opt into caching.

### nextjs-route-handlers-methods-03

How do you read the request body, query params, and dynamic route params in a Route Handler?

### nextjs-streaming-ssr-01

What is streaming SSR in the App Router, and what does it buy you over rendering the whole page and then sending it?

### nextjs-streaming-suspense-02

You have a fast page with one slow data section. How do you keep the page fast without blocking on the slow part?

### nextjs-streaming-boundary-03

What's the difference between using `loading.tsx` and wrapping a component in your own `<Suspense>` boundary?

### nextjs-metadata-static-01

How do you set the page title and other `<head>` tags for a route in the App Router?

### nextjs-metadata-dynamic-02

When the title/description depends on fetched data (e.g. a blog post), how do you generate metadata?

### nextjs-metadata-inherit-03

How does metadata compose across nested layouts and pages, and what is `title.template` for?

### nextjs-metadata-files-04

Besides the `metadata` export, how does Next.js let you define things like the favicon, Open Graph image, `sitemap.xml`, and `robots.txt`?

### nextjs-navigation-link-01

How do you navigate between routes in the App Router, and what does `<Link>` do that a plain `<a>` doesn't?

### nextjs-navigation-hooks-02

Which module do the App Router navigation hooks come from, and why do `useRouter`/`usePathname`/`useSearchParams` fail in a Server Component?

### nextjs-navigation-redirect-03

What do `redirect()` and `notFound()` from `next/navigation` do, and what's a gotcha about how they work?

### nextjs-navigation-action-redirect-04

After a successful Server Action (e.g. creating a record) you want to send the user to the new page. How, and what should you watch out for?
