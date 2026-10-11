---
name: nextjs-tuning-longcat-2.5-preview-free
description: Corrective Next.js App Router guidance for the exact areas longcat-2.5-preview-free tests weak on (data-fetching, metadata). Loaded only in Next.js repos when this exact model is active.
when_to_use: The active model id is exactly longcat-2.5-preview-free AND the repo uses Next.js (see docs/okf/_schema/stack-detection.md). Do not load for other models or non-Next.js repos.
# --- Kaizen metadata ---
tuned_for: longcat-2.5-preview-free
tuned_version: "2.5-preview"
stack: nextjs
source_scorecard: ../scores/longcat-2.5-preview-free.md
threshold: 0.85
revalidate_when: model_version changes   # STALE on any version bump — re-benchmark
---
# Next.js tuning — longcat-2.5-preview-free

## Data fetching

- **`fetch` default caching, get the direction right:** Next 13/14 cached
  `fetch` by default (`force-cache`, Data Cache). Next 15+ (unchanged through
  16) does **NOT** cache `fetch` by default (behaves like `no-store`). In 15+,
  opt in per request with `cache: 'force-cache'` or `next: { revalidate: N }`.
  Never say "Next 15 caches fetch by default". It is the reverse.
- `getServerSideProps` / `getStaticProps` / `getInitialProps` **do not exist in
  the `app/` directory**. They are Pages Router only. Say this explicitly rather
  than just calling them "replaced". In `app/`, an async Server Component awaits
  data during render, and static vs dynamic follows from caching options and
  dynamic API use.
- Avoiding waterfalls has two fixes. Name both:
  1. start independent requests together and `await Promise.all([...])`;
  2. move each slow, independent fetch into its own async Server Component
     wrapped in its own `<Suspense>`, so the pieces stream in parallel and don't
     block each other or the page shell.

## Metadata API

- The static `metadata` export and `generateMetadata` are **Server Component
  only**. A `"use client"` file cannot export them. State this constraint
  whenever explaining how to set `<head>` tags.
- Fetches inside `generateMetadata` are **deduped with the page's own fetches
  via Request Memoization** (same URL and options in one render pass resolve
  once). Say so explicitly: there is no double fetch for head tags and body.
- `title` accepts a string or `{ default, template, absolute }`. `template` uses
  `%s` for the child title. There is no `app` key.
- Metadata merges root→leaf **shallowly**. A child that sets a nested object
  (e.g. `openGraph`) replaces the parent's whole object, and arrays such as
  `openGraph.images` are replaced, not merged.

## Caching

- **Version direction, never reverse it:** Next 13/14 cached `fetch` by default
  (`force-cache`, Data Cache). Next 15+ (through 16) does **NOT** cache `fetch`
  by default (`no-store` behavior). Always name the version a default applies to.
- Opt in per request with `cache: 'force-cache'` or `next: { revalidate: N }`.
  Opt out (14) with `cache: 'no-store'` or `export const dynamic = 'force-dynamic'`.
- Four layers, keep them distinct: **Request Memoization** (dedupes identical
  `fetch` calls within one render pass), **Data Cache** (persistent server-side
  results of `fetch`/`unstable_cache`, cross-request), **Full Route Cache**
  (HTML + RSC payload of statically rendered routes), **Router Cache**
  (client-side in-memory cache of visited/prefetched segments).
- `revalidate = N` is stale-while-revalidate (ISR): after N seconds the next
  request still gets the cached copy and triggers a background regeneration.
  Fully static content is built once and stays until redeploy or on-demand
  invalidation.
- On-demand invalidation: `revalidateTag(tag)` / `revalidatePath(path)`, callable
  from Server Actions and Route Handlers.

## Route handlers

- A Route Handler is a `route.ts`/`route.js` file exporting functions named after
  HTTP methods (`GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `HEAD`, `OPTIONS`) that
  take a Web `Request` and return a Web `Response`. It replaces `pages/api`.
  A `route.ts` and a `page.tsx` cannot coexist in the same segment.
- **`GET` handler caching by version:** Next 14 cached `GET` handlers by default.
  Next 15+ (through 16) does **NOT** cache them; every request runs the handler.
  Opt in with `export const dynamic = 'force-static'`, optionally with
  `export const revalidate = N`. Non-`GET` methods are never cached.
- Prefer Server Actions for mutations from your own UI; use Route Handlers for
  webhooks, non-HTML responses and external consumers.
