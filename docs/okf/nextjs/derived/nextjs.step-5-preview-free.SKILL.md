---
name: nextjs-tuning-step-5-preview-free
description: Corrective Next.js App Router guidance for the areas step-5-preview-free tests weak on (data-fetching, metadata). Loaded only in Next.js repos when this exact model is active.
when_to_use: The active model's canonical id is exactly step-5-preview-free AND the repo uses Next.js (see docs/okf/_schema/stack-detection.md). Do not load for other models or non-Next.js repos.
# --- Kaizen metadata ---
tuned_for: step-5-preview-free
tuned_version: "5-preview"
stack: nextjs
source_scorecard: ../scores/step-5-preview-free.md
threshold: 0.85
revalidate_when: model_version changes   # STALE on any version bump — re-benchmark
---
# Next.js tuning — step-5-preview-free

## Data fetching

- **Functions do not cross the server→client prop boundary.** Passing a plain
  function (an `onClick`, a callback, a class method) from a Server Component to a
  Client Component is a runtime error, not an implicit conversion. The ONLY
  functions that may cross are ones already marked `"use server"` (Server
  Actions), which travel as an action reference. Never describe or write code
  that hands an unmarked function to a client component expecting Next to turn
  it into an action. Everything else must be serializable: primitives, plain
  objects/arrays, Date, and React elements. Keep the server subtree server-side
  by passing it as `children` / an element prop.
- **Avoiding a request waterfall has two tools, not one.** `Promise.all` /
  start-then-await is the fix when one component needs all the data before it
  renders. When the data sources are independent, the stronger pattern is to
  move each slow fetch into its own `async` Server Component and wrap each in
  its own `<Suspense fallback>` so they stream in parallel and neither blocks
  the other or the shell. Always offer the Suspense-isolation option alongside
  `Promise.all`; do not stop at parallel awaits.

## Metadata API

- **`generateMetadata` fetches are deduped with the page automatically.** A
  `fetch` with the same URL and options in `generateMetadata` and in the page
  (or layout) resolves once per render pass via Request Memoization — there is
  no second round-trip and no opt-in needed. Say this explicitly. `React.cache()`
  is the tool for deduping NON-fetch calls (ORM/db), not a prerequisite for
  fetch. Do not present dedup as something the developer must wire up, and do
  not say it "runs for every request" without qualifying that static routes
  evaluate it at build/revalidate time.
- **Nested metadata objects are replaced, not deep-merged.** Across nested
  layouts/pages, top-level keys from a child override the parent's, and a
  nested object such as `openGraph`, `twitter`, `robots`, `alternates`, or
  `icons` is overwritten **wholesale** when the child sets that key — a child
  `openGraph: { title }` drops the parent's `openGraph.description`,
  `openGraph.siteName`, and images. Icons do not accumulate. To keep parent
  fields, spread a shared object into the child (e.g. export a shared
  `openGraph` base and `{ ...base, title }`), or move shared values into
  `title.template` / `metadataBase`. Never tell a user that nested metadata
  fields merge key-by-key.
