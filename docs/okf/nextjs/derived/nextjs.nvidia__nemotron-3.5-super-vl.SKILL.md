---
name: nextjs-tuning-nvidia-nemotron-3.5-super-vl
description: Corrective Next.js App Router guidance for the areas nvidia/nemotron-3.5-super-vl tests weak on (caching, route-handlers). Loaded only in Next.js repos when this exact model is active.
when_to_use: The active model's canonical id is exactly nvidia/nemotron-3.5-super-vl (provider-stripped, e.g. `openrouter/nvidia/nemotron-3.5-super-vl` → `nvidia/nemotron-3.5-super-vl`) AND the repo uses Next.js (`next` dep in package.json or a `next.config.*` file; see docs/okf/_schema/stack-detection.md). Do not load for other models or non-Next.js repos.
# --- Kaizen metadata ---
tuned_for: nvidia/nemotron-3.5-super-vl
tuned_version: "3.5"
stack: nextjs
source_scorecard: ../scores/nvidia__nemotron-3.5-super-vl.md
threshold: 0.85
revalidate_when: model_version changes   # STALE on any version bump — re-benchmark
---
# Next.js tuning — nvidia/nemotron-3.5-super-vl

<!-- kaizen:digest -->
## Caching

- **The App Router has exactly four caching layers.** Request Memoization
  (per-render dedup of identical `fetch` calls, automatic, lives only for one
  render pass), Data Cache (persistent, cross-request `fetch` /
  `unstable_cache` results, controlled by `revalidate` and tags), Full Route
  Cache (the prerendered HTML + RSC payload of static routes), and the
  client-side Router Cache (visited/prefetched segments in the browser). Always
  name Request Memoization first; it is the layer that makes a `fetch` in
  `generateMetadata` and in the page cost one round-trip. Browser/CDN caching of
  hashed static assets is NOT one of these layers; do not list it as one.
- **`export const dynamic` takes `'auto' | 'force-dynamic' | 'error' |
  'force-static'`.** There is no `'static'` value. `force-dynamic` renders on
  every request and disables the Data and Full Route caches for the segment
  (every fetch behaves as `no-store`); `force-static` forces a fully static
  render and errors on dynamic APIs (`cookies()`, `headers()`, `searchParams`).
  Name both when explaining the export.
- **`revalidate = false` and `revalidate = 0` are opposites.** `false` means
  cache indefinitely (fully static until on-demand invalidation or redeploy);
  `0` means never cache (always dynamic). Never describe them together as
  "no revalidation".
- **A cached GET route handler needs an explicit opt-in on Next 15+.** Write
  `export const dynamic = 'force-static'` in the `route.ts` (add
  `export const revalidate = N` for periodic regeneration). Response headers do
  not opt a handler into Next's Data or Full Route cache.

## Route handlers

- **GET handlers are uncached by default on Next 15 and 16** (Next 14 cached
  them). When asked how to cache one, give the concrete opt-in, not "use
  segment config": `export const dynamic = 'force-static'` plus an optional
  `export const revalidate = N`. Non-GET methods are never cached, and any use
  of `cookies()`, `headers()` or the `Request` body keeps a GET dynamic even with
  the opt-in.
- **A `route.ts` cannot share a segment with a `page.tsx`.** State this whenever
  describing route handlers or the special-file conventions; it is the one
  coexistence rule that produces a build error.
- **Handlers receive `(request, context)`.** Dynamic segment values come from
  `context.params` (a Promise on Next 15+: `const { id } = await context.params`).
  There is no `searchParams` prop in a route handler; read the query from
  `new URL(request.url).searchParams` or `request.nextUrl.searchParams`.
<!-- /kaizen:digest -->
