---
name: nextjs-tuning-cat-jam-free
description: Corrective Next.js App Router guidance for the areas cat-jam-free tests weak on (app-router client/server boundary, metadata). Loaded only in Next.js repos when this exact model is active.
when_to_use: The active model's canonical id is exactly cat-jam-free AND the repo uses Next.js (see docs/okf/_schema/stack-detection.md). Do not load for other models or non-Next.js repos.
# --- Kaizen metadata ---
tuned_for: cat-jam-free
tuned_version: "alpha"
stack: nextjs
source_scorecard: ../scores/cat-jam-free.md
threshold: 0.85
revalidate_when: model_version changes   # STALE on any version bump — re-benchmark
---
# Next.js tuning — cat-jam-free

## App Router: the server/client module boundary

- **`"use client"` is a module-graph entry point, not a per-file label.** Putting
  it at the top of a file makes that module AND every module it imports part of
  the client bundle. Files imported below the boundary do not need their own
  `"use client"` — and they become Client Components whether marked or not. Never
  say "everything not marked is server-rendered": a component with no directive
  is a Client Component when it is imported from a client module.
- **Keep the boundary at the leaves.** Because the directive pulls in the whole
  import subtree, mark only the small interactive component, not a page or
  layout. Server content that must appear inside a client component is passed in
  as `children` / an element prop, not imported by the client file.
- **State what Server Components give you.** When explaining the default, say
  explicitly: Server Components render only on the server, ship **no JavaScript**
  for themselves, and **can be `async`** (await data directly in the component
  body). Client Components are the ones that ship JS and hydrate for state,
  effects, event handlers and browser APIs.

## Metadata API

- **`metadata` and `generateMetadata` are Server-Component-only.** They may be
  exported from a server `layout` or `page`; a file with `"use client"` cannot
  export either. Next renders the `<head>` tags itself — never hand-write
  `<head>`/`<title>`. If a page must be a client component, move the metadata
  export into a server parent (layout or a server page that renders the client
  component).
- **`generateMetadata` fetches are deduped with the page automatically.**
  `generateMetadata` runs on the server; a `fetch` with the same URL and options
  in `generateMetadata` and in the page/layout resolves once per render via
  Request Memoization, with no opt-in. Say this explicitly instead of "reuse
  cache behavior to avoid duplicate work". `React.cache()` is only needed to
  dedupe non-`fetch` calls (ORM/db queries).
