---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: nextjs
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — longcat-2.5-preview-free on nextjs

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump
> invalidates this scorecard — re-benchmark.
>
> Graded from closed-book answers (`../answers/longcat-2.5-preview-free.md`,
> produced via `ocode run` from an isolated dir; session audited: 1 user + 1
> assistant turn, zero tool calls) against `../questions.yaml` (corpus_rev 1).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| nextjs-app-router-conventions-01 | app-router | 3 | 3 | 3 | 1.00 | all five files, folder→URL incl. `[slug]`; route.ts "instead of a page" conveys exclusivity |
| nextjs-app-router-layout-02 | app-router | 2 | 2 | 2 | 1.00 | layout persists/no remount; template remounts per navigation |
| nextjs-app-router-error-03 | app-router | 2 | 2 | 1 | 0.50 | WRONG: claims error.tsx "cannot be a Client Component… must be a Server Component" (it must be `"use client"`); no `error`/`reset` props. Scope point awarded: excludes event handlers and root layout (`global-error.tsx`), though it wrongly says it catches its own segment's layout |
| nextjs-app-router-loading-04 | app-router, streaming | 2 | 2 | 2 | 1.00 | auto `<Suspense>` wrap; streaming/instant feedback |
| nextjs-server-components-default-01 | server-components, app-router | 3 | 3 | 3 | 1.00 | server by default, no client JS; `"use client"` = module boundary incl. imports |
| nextjs-server-components-hooks-02 | server-components | 3 | 2 | 2 | 1.00 | server-only → no hooks/events/browser APIs; extract to `"use client"` |
| nextjs-server-components-props-03 | server-components, data-fetching | 2 | 2 | 2 | 1.00 | serializable props, no functions/class instances; children-slot pattern |
| nextjs-data-fetching-rsc-01 | data-fetching, server-components | 3 | 2 | 1 | 0.50 | async RSC fetch covered; frames gSSP/gSP as "replaced by" but never states they do not exist in the app dir |
| nextjs-data-fetching-nogssp-02 | data-fetching, rendering | 2 | 2 | 1 | 0.50 | start-then-await / `Promise.all`; MISSED separate Suspense-wrapped components streaming in parallel |
| nextjs-caching-fetch-default-01 | caching, data-fetching | 3 | 3 | 1 | 0.33 | INVERTED version defaults: says Next 14 fetch uncached and Next 15 cached (reverse of reality). Only the opt-in mechanism (`force-cache` / `next.revalidate`) credited |
| nextjs-caching-layers-02 | caching | 2 | 3 | 3 | 1.00 | all four layers named and distinguished (minor: says Data Cache stores Server Action results) |
| nextjs-caching-revalidate-03 | caching, rendering | 3 | 2 | 2 | 1.00 | stale window; serve-stale-then-regenerate ISR vs built-once static |
| nextjs-caching-ondemand-04 | caching, server-actions | 2 | 2 | 2 | 1.00 | path vs tag; after mutation in action/handler |
| nextjs-caching-segment-config-05 | caching, rendering | 3 | 2 | 2 | 1.00 | segment exports; force-dynamic per-request, force-static static |
| nextjs-rendering-static-dynamic-01 | rendering | 3 | 2 | 2 | 1.00 | static unless cookies/headers/uncached fetch; segment override |
| nextjs-rendering-static-params-02 | rendering | 2 | 2 | 2 | 1.00 | param list → build-time prerender; getStaticPaths equivalent |
| nextjs-rendering-dynamic-apis-03 | rendering, data-fetching | 2 | 2 | 2 | 1.00 | async in v15, must await; reading forces dynamic |
| nextjs-server-actions-useserver-01 | server-actions | 3 | 3 | 2 | 0.67 | server-run callable fn + RPC vs client boundary; MISSED inline (function-body) `"use server"` form — only file-level described |
| nextjs-server-actions-mutation-02 | server-actions, caching | 2 | 2 | 2 | 1.00 | form action → write → revalidate/redirect; progressive enhancement; useActionState |
| nextjs-server-actions-security-03 | server-actions, route-handlers | 3 | 2 | 2 | 1.00 | public endpoint; authz + validation inside every action |
| nextjs-route-handlers-basics-01 | route-handlers | 2 | 2 | 2 | 1.00 | method exports on Request/Response; replaces pages/api. Minor inaccuracy: "by default edge-compatible" (default runtime is Node) |
| nextjs-route-handlers-caching-02 | route-handlers, caching | 3 | 2 | 1 | 0.50 | INVERTED again: says Next 14 GET uncached, Next 15 cached. force-static / revalidate opt-in credited |
| nextjs-route-handlers-methods-03 | route-handlers | 1 | 2 | 2 | 1.00 | json/text/formData; nextUrl / URL searchParams; awaited params in v15 |
| nextjs-streaming-ssr-01 | streaming | 2 | 2 | 2 | 1.00 | shell first, sections stream in; TTFB/LCP benefit |
| nextjs-streaming-suspense-02 | streaming, server-components | 2 | 2 | 2 | 1.00 | slow section behind `<Suspense>`, rest streams; does not say the slow part must be its own async component |
| nextjs-streaming-boundary-03 | streaming, app-router | 1 | 2 | 2 | 1.00 | segment-wide vs granular |
| nextjs-metadata-static-01 | metadata | 2 | 2 | 1 | 0.50 | static `metadata` export, Next injects head; MISSED Server-Component-only restriction; garbled `template` description ("`app`/`%s` patterns") |
| nextjs-metadata-dynamic-02 | metadata | 2 | 2 | 1 | 0.50 | async `generateMetadata` with awaited params; MISSED fetch dedup with the page (Request Memoization) |
| nextjs-metadata-inherit-03 | metadata | 1 | 2 | 2 | 1.00 | parent→child override + `title.template` with `%s`/default. Minor inaccuracy: claims arrays like `openGraph.images` merge (nested objects are replaced, not deep-merged) |
| nextjs-metadata-files-04 | metadata | 1 | 2 | 2 | 1.00 | icon/opengraph-image/sitemap/robots conventions, static or code, auto-wired |
| nextjs-navigation-link-01 | navigation | 2 | 2 | 2 | 1.00 | client-side nav, viewport prefetch. Minor inaccuracy: "preserves scroll position by default" |
| nextjs-navigation-hooks-02 | navigation, server-components | 2 | 2 | 2 | 1.00 | `next/navigation` hooks, client-only → `"use client"`; no `next/router` contrast or server-side props alternative |
| nextjs-navigation-redirect-03 | navigation, rendering | 2 | 2 | 2 | 1.00 | redirect / notFound, thrown control-flow errors, keep outside try/catch |
| nextjs-navigation-action-redirect-04 | navigation, server-actions | 1 | 2 | 2 | 1.00 | redirect after confirmed write; try/catch swallow caveat. Minor oddity: redirect "in the `.then()` chain" with useTransition |

`normalized = min(awarded, full) / full`

## Per-tag subscores

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| streaming | 1.00 | 4 | ok | omit (strong) |
| navigation | 1.00 | 4 | ok | omit (strong) |
| rendering | 0.94 | 7 | ok | omit (strong) |
| app-router | 0.92 | 6 | ok | omit (strong) |
| server-actions | 0.91 | 5 | ok | omit (strong) |
| server-components | 0.90 | 6 | ok | omit (strong) |
| route-handlers | 0.83 | 4 | ok | omit (strong) |
| caching | 0.81 | 7 | ok | omit (above threshold) |
| metadata | 0.67 | 4 | ok | **derive** |
| data-fetching | 0.63 | 5 | ok | **derive** |

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 64 / 74 ≈ 86%
```

## Derivation targets

Tags below threshold (`< 0.75`): **metadata, data-fetching** → feed into
`derived/nextjs.longcat-2.5-preview-free.SKILL.md`.

Note: `caching` clears the threshold (0.81) despite the model inverting the
Next 14 vs 15 default for both `fetch` and GET Route Handlers — the other five
caching questions are perfect. The `fetch` inversion also counts toward
`data-fetching`, so it is covered by that tag's section. The Route Handler GET
inversion is the same misconception; because `caching`/`route-handlers` are above
threshold it gets no dedicated section. Watch for it on re-evaluation.

## Contamination check

No leak. Answers are in the model's own wording and diverge from the reference
throughout: two version-default answers are inverted relative to the key, the
error.tsx answer contradicts the key (Server Component claim), and several answers
carry inaccuracies absent from the key (edge-compatible-by-default route
handlers, array merging in metadata, scroll preservation on `<Link>`). No answer
is a near-verbatim match to a reference answer; score (86%) is not a sweep.
