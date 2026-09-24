---
model_id: space-bunny-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-09-24
stack: nextjs
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — space-bunny-free on nextjs

> Valid ONLY for `space-bunny-free` @ `alpha`. A version bump invalidates this
> scorecard — re-benchmark.
>
> Graded from closed-book answers (`../answers/space-bunny-free.md`) against
> `../questions.yaml` (corpus_rev 1).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| nextjs-app-router-conventions-01 | app-router | 3 | 3 | 3 | 1.00 | all five files, folder→URL, `[slug]`, route/page exclusivity; adds route groups + `_private` |
| nextjs-app-router-layout-02 | app-router | 2 | 2 | 2 | 1.00 | layouts persist state; template fresh instance per navigation |
| nextjs-app-router-error-03 | app-router | 2 | 2 | 2 | 1.00 | client component + error/reset; sibling-layout exclusion + event handlers not caught |
| nextjs-app-router-loading-04 | app-router, streaming | 2 | 2 | 2 | 1.00 | auto Suspense wrap; fallback streamed instead of blocking route |
| nextjs-server-components-default-01 | server-components, app-router | 3 | 3 | 3 | 1.00 | server by default, may be async; `"use client"` = module boundary incl. imports; correctly notes client comps still SSR |
| nextjs-server-components-hooks-02 | server-components | 3 | 2 | 2 | 1.00 | no client runtime/state/events on server; extract smallest `"use client"` leaf |
| nextjs-server-components-props-03 | server-components, data-fetching | 2 | 2 | 2 | 1.00 | serializable props (correctly excepts Server Actions); children-slot pattern keeps server subtree out of client bundle |
| nextjs-data-fetching-rsc-01 | data-fetching, server-components | 3 | 2 | 1 | 0.50 | async RSC fetch covered; frames gSSP/gSP as "replaced by" but never states they do not exist in the app dir / are Pages Router only |
| nextjs-data-fetching-nogssp-02 | data-fetching, rendering | 2 | 2 | 2 | 1.00 | start-then-await + Promise.all; separate Suspense-wrapped children to stream in parallel |
| nextjs-caching-fetch-default-01 | caching, data-fetching | 3 | 3 | 3 | 1.00 | v15 uncached by default, v14 cached, opt-in force-cache / next.revalidate — version-correct |
| nextjs-caching-layers-02 | caching | 2 | 3 | 3 | 1.00 | all four layers named and distinguished |
| nextjs-caching-revalidate-03 | caching, rendering | 3 | 2 | 2 | 1.00 | stale window, serve-stale-then-regenerate ISR vs built-once static |
| nextjs-caching-ondemand-04 | caching, server-actions | 2 | 2 | 2 | 1.00 | path vs tag; after mutation from action/handler. Minor inaccuracy: claims revalidatePath inside a Server Action does not refresh the already-rendered UI (it does — the action response carries the refreshed RSC payload) |
| nextjs-caching-segment-config-05 | caching, rendering | 3 | 2 | 2 | 1.00 | segment-level exports; force-dynamic = request-time, bypasses Full Route Cache; force-static enumerated but not explained |
| nextjs-rendering-static-dynamic-01 | rendering | 3 | 2 | 2 | 1.00 | prerender when no request-specific info; cookies/headers/searchParams, explicit config, uncached data force dynamic |
| nextjs-rendering-static-params-02 | rendering | 2 | 2 | 2 | 1.00 | param list → build-time SSG; getStaticPaths equivalent; dynamicParams ↔ fallback |
| nextjs-rendering-dynamic-apis-03 | rendering, data-fetching | 2 | 2 | 2 | 1.00 | async in v15 (Promises, must await); reading forces dynamic |
| nextjs-server-actions-useserver-01 | server-actions | 3 | 3 | 3 | 1.00 | server-run fn callable from client; module-level (all exports) vs function-body scoping; RPC boundary vs client-component boundary |
| nextjs-server-actions-mutation-02 | server-actions, caching | 2 | 2 | 2 | 1.00 | `"use server"` fn as form action, FormData, write, revalidatePath + redirect, useActionState/useFormStatus |
| nextjs-server-actions-security-03 | server-actions, route-handlers | 3 | 2 | 2 | 1.00 | reachable endpoint / forged requests; authn + authz + validate every input inside the action |
| nextjs-route-handlers-basics-01 | route-handlers | 2 | 2 | 2 | 1.00 | method exports on Web Request/Response; replaces pages/api; cannot coexist with page.tsx |
| nextjs-route-handlers-caching-02 | route-handlers, caching | 3 | 2 | 2 | 1.00 | v15+ GET uncached, v14 cached; `dynamic = 'force-static'` + revalidate opt-in |
| nextjs-route-handlers-methods-03 | route-handlers | 1 | 2 | 2 | 1.00 | json/formData/text; URL searchParams / nextUrl; awaited ctx.params in v15 |
| nextjs-streaming-ssr-01 | streaming | 2 | 2 | 2 | 1.00 | shell + RSC payload flushed first, Suspense boundaries as stream points; TTFB/FCP benefit |
| nextjs-streaming-suspense-02 | streaming, server-components | 2 | 2 | 2 | 1.00 | slow part in nested async component under `<Suspense>`; fast content streams immediately |
| nextjs-streaming-boundary-03 | streaming, app-router | 1 | 2 | 2 | 1.00 | loading.tsx = segment-wide boundary; manual Suspense = granular placement |
| nextjs-metadata-static-01 | metadata | 2 | 2 | 1 | 0.50 | static `metadata` export, Next renders head; MISSED that it is unavailable in `"use client"` files |
| nextjs-metadata-dynamic-02 | metadata | 2 | 2 | 2 | 1.00 | async generateMetadata with awaited params; dedup via React `cache` (does not mention automatic fetch memoization, but the no-double-fetch concept is present) |
| nextjs-metadata-inherit-03 | metadata | 1 | 2 | 2 | 1.00 | root→leaf override (correctly notes shallow, not deep, merge); title.template with %s, default, absolute |
| nextjs-metadata-files-04 | metadata | 1 | 2 | 2 | 1.00 | icon/opengraph-image/sitemap/robots/manifest; static or code files exporting MetadataRoute |
| nextjs-navigation-link-01 | navigation | 2 | 2 | 2 | 1.00 | client-side nav, no full reload, viewport prefetch; `<a>` = full document load; router.push |
| nextjs-navigation-hooks-02 | navigation, server-components | 2 | 2 | 2 | 1.00 | next/navigation (does not explicitly contrast next/router); client hooks need `"use client"`; server reads searchParams prop |
| nextjs-navigation-redirect-03 | navigation, rendering | 2 | 2 | 2 | 1.00 | redirect / notFound → not-found.tsx; throw control-flow errors, broad try/catch must rethrow. Minor inaccuracy: claims `throw redirect(...)` is conventional (that is a Remix/TanStack idiom, not Next) |
| nextjs-navigation-action-redirect-04 | navigation, server-actions | 1 | 2 | 2 | 1.00 | redirect after write + revalidatePath; throws → keep outside try/catch or rethrow |

`normalized = min(awarded, full) / full`

## Per-tag subscores

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| app-router | 1.00 | 6 | ok | omit (strong) |
| caching | 1.00 | 7 | ok | omit (strong) |
| rendering | 1.00 | 7 | ok | omit (strong) |
| server-actions | 1.00 | 5 | ok | omit (strong) |
| route-handlers | 1.00 | 4 | ok | omit (strong) |
| streaming | 1.00 | 4 | ok | omit (strong) |
| navigation | 1.00 | 4 | ok | omit (strong) |
| server-components | 0.90 | 6 | ok | omit (strong) |
| data-fetching | 0.88 | 5 | ok | omit (strong) |
| metadata | 0.83 | 4 | ok | omit (strong) |

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 71.5 / 74 ≈ 97%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none** → no derived skill written for this
model on nextjs.

## Contamination check

Answers are in the model's own wording and consistently carry details absent
from the reference (route groups and `_private` folders, `title.absolute`,
`MetadataRoute` types, `dynamicParams` ↔ `fallback`, the Server-Actions-as-props
exception, shallow metadata merge). Two rubric points were genuinely missed and
two answers contain small factual errors not present in the key (see notes on
`caching-ondemand-04` and `navigation-redirect-03`). No answer reads as a copy
of the reference. The score is high but not the flat 100% sweep that signals
key exposure.
