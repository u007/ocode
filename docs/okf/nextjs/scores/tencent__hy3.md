---
model_id: tencent/hy3
model_version: "3.0"
evaluated_via: novita
evaluated_on: 2026-07-11
stack: nextjs
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — tencent/hy3 on nextjs

> Valid ONLY for `tencent/hy3` @ `3.0`. A version bump invalidates this
> scorecard — re-benchmark.
>
> Graded independently (Opus 4.8), closed-book: answers were produced with no
> corpus access and graded against `../questions.yaml` (corpus_rev 1).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| nextjs-app-router-conventions-01 | app-router | 3 | 3 | 3 | 1.00 | folder→URL, page/layout/loading/error/route all correct |
| nextjs-app-router-layout-02 | app-router | 2 | 2 | 2 | 1.00 | layout-persist + template re-mount both nailed |
| nextjs-app-router-error-03 | app-router | 2 | 2 | 2 | 1.00 | client component, error/reset, sibling-layout + event-handler exclusions |
| nextjs-app-router-loading-04 | app-router, streaming | 2 | 2 | 2 | 1.00 | Suspense wrap + streaming shell |
| nextjs-server-components-default-01 | server-components, app-router | 3 | 3 | 3 | 1.00 | server default, module boundary, no-JS vs interactivity |
| nextjs-server-components-hooks-02 | server-components | 3 | 3 | 3 | 1.00 | no-hydration reason + extract-client fix |
| nextjs-server-components-props-03 | server-components, data-fetching | 2 | 2 | 2 | 1.00 | serializable props (correct use-server exception) + children-slot |
| nextjs-data-fetching-rsc-01 | data-fetching, server-components | 3 | 3 | 3 | 1.00 | async RSC await + no gSSP/gSP in app dir |
| nextjs-data-fetching-nogssp-02 | data-fetching, rendering | 2 | 2 | 1 | 0.50 | Promise.all covered; MISSED isolating slow fetches behind separate Suspense boundaries |
| nextjs-caching-fetch-default-01 | caching, data-fetching | 3 | 3 | 3 | 1.00 | v14 cached / v15+ no-store / opt-in — version-correct |
| nextjs-caching-layers-02 | caching | 2 | 2 | 2 | 1.00 | all four layers named and distinguished |
| nextjs-caching-revalidate-03 | caching, rendering | 3 | 3 | 3 | 1.00 | stale-while-revalidate ISR vs built-once static |
| nextjs-caching-ondemand-04 | caching, server-actions | 2 | 2 | 2 | 1.00 | path-vs-tag split, called from action/handler after write |
| nextjs-caching-segment-config-05 | caching, rendering | 3 | 3 | 3 | 1.00 | segment exports + force-dynamic per-request/no-cache |
| nextjs-rendering-static-dynamic-01 | rendering | 3 | 3 | 3 | 1.00 | static-by-default + dynamic API / no-store triggers |
| nextjs-rendering-static-params-02 | rendering | 2 | 2 | 2 | 1.00 | prerender param list (SSG) replaces getStaticPaths |
| nextjs-rendering-dynamic-apis-03 | rendering, data-fetching | 2 | 2 | 2 | 1.00 | async in v15 + forces dynamic render |
| nextjs-server-actions-useserver-01 | server-actions | 3 | 3 | 3 | 1.00 | action = server fn callable from client; file vs inline; vs use-client |
| nextjs-server-actions-mutation-02 | server-actions, caching | 2 | 2 | 2 | 1.00 | form action + FormData + revalidate/redirect |
| nextjs-server-actions-security-03 | server-actions, route-handlers | 3 | 3 | 3 | 1.00 | public endpoint + authn/authz/validate inside action |
| nextjs-route-handlers-basics-01 | route-handlers | 2 | 2 | 2 | 1.00 | method exports, Web Request/Response, replaces pages/api |
| nextjs-route-handlers-caching-02 | route-handlers, caching | 3 | 3 | 3 | 1.00 | v14 cached GET / v15+ not / force-static opt-in |
| nextjs-route-handlers-methods-03 | route-handlers | 1 | 2 | 2 | 1.00 | body/query/params incl. awaited params in v15 |
| nextjs-streaming-ssr-01 | streaming | 2 | 2 | 2 | 1.00 | chunked shell-first + TTFB benefit |
| nextjs-streaming-suspense-02 | streaming, server-components | 2 | 2 | 2 | 1.00 | isolate slow in Suspense, rest streams |
| nextjs-streaming-boundary-03 | streaming, app-router | 1 | 2 | 2 | 1.00 | loading.tsx whole-segment vs granular manual Suspense |
| nextjs-metadata-static-01 | metadata | 2 | 2 | 2 | 1.00 | static metadata export, Next generates head (client-only caveat omitted, minor) |
| nextjs-metadata-dynamic-02 | metadata | 2 | 2 | 2 | 1.00 | async generateMetadata + memoized dedup |
| nextjs-metadata-inherit-03 | metadata | 1 | 2 | 2 | 1.00 | root→leaf merge + title.template |
| nextjs-metadata-files-04 | metadata | 1 | 2 | 2 | 1.00 | file conventions (og/icon/sitemap/robots/manifest) static or generators |
| nextjs-navigation-link-01 | navigation | 2 | 2 | 2 | 1.00 | client-side nav + prefetch vs full reload |
| nextjs-navigation-hooks-02 | navigation, server-components | 2 | 2 | 2 | 1.00 | next/navigation not next/router; client-only (params-props alt omitted, minor) |
| nextjs-navigation-redirect-03 | navigation, rendering | 2 | 2 | 2 | 1.00 | redirect/notFound server-side + throws/try-catch gotcha |
| nextjs-navigation-action-redirect-04 | navigation, server-actions | 1 | 2 | 2 | 1.00 | redirect after write/revalidate + throw/try-catch caveat |

`normalized = min(awarded, full) / full`

## Per-tag subscores

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| app-router | 1.00 | 6 | ok | omit (strong) |
| server-components | 1.00 | 6 | ok | omit (strong) |
| caching | 1.00 | 7 | ok | omit (strong) |
| server-actions | 1.00 | 5 | ok | omit (strong) |
| route-handlers | 1.00 | 4 | ok | omit (strong) |
| streaming | 1.00 | 4 | ok | omit (strong) |
| metadata | 1.00 | 4 | ok | omit (strong) |
| navigation | 1.00 | 4 | ok | omit (strong) |
| rendering | 0.94 | 7 | ok | omit (strong) |
| data-fetching | 0.92 | 5 | ok | omit (strong) |

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 73 / 74 ≈ 99%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none**. Every tag scored ≥ 0.92, so no
corrective skill is derived — restating strong areas would only waste prompt
tokens and prefix-cache budget. **No derivation.**
</content>
</invoke>
