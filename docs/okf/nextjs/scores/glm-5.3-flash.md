---
model_id: glm-5.3-flash
model_version: "5.3"
evaluated_via: aihubmix
evaluated_on: 2026-09-01
stack: nextjs
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — glm-5.3-flash on nextjs

> Valid ONLY for `glm-5.3-flash` @ `5.3`. A version bump invalidates this
> scorecard — re-benchmark.
>
> Graded from closed-book answers (`../answers/glm-5.3-flash.md`) against
> `../questions.yaml` (corpus_rev 1).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| nextjs-app-router-conventions-01 | app-router | 3 | 3 | 3 | 1.00 | folder→URL, page/layout/loading/error, route.ts mutually exclusive with page |
| nextjs-app-router-layout-02 | app-router | 2 | 2 | 2 | 1.00 | layouts persist state; template re-mounts per navigation |
| nextjs-app-router-error-03 | app-router | 2 | 2 | 2 | 1.00 | client component + error/reset; excludes same-level layout and event handlers; global-error |
| nextjs-app-router-loading-04 | app-router, streaming | 2 | 2 | 2 | 1.00 | Suspense wrap + instant fallback while segment streams |
| nextjs-server-components-default-01 | server-components, app-router | 3 | 3 | 2 | 0.67 | server-by-default + module boundary incl. imports covered; MISSED "server = no JS shipped / can be async; client = interactivity" |
| nextjs-server-components-hooks-02 | server-components | 3 | 2 | 2 | 1.00 | never hydrate → no hooks/DOM/window; extract 'use client' piece |
| nextjs-server-components-props-03 | server-components, data-fetching | 2 | 2 | 2 | 1.00 | serializable props (no functions/class instances); children-as-slot pattern |
| nextjs-data-fetching-rsc-01 | data-fetching, server-components | 3 | 2 | 2 | 1.00 | async RSC await; states gSSP/gSP do not exist in app dir |
| nextjs-data-fetching-nogssp-02 | data-fetching, rendering | 2 | 2 | 2 | 1.00 | Promise.all / start-then-await + separate Suspense boundaries |
| nextjs-caching-fetch-default-01 | caching, data-fetching | 3 | 3 | 3 | 1.00 | v13/14 cached, v15 uncached, opt-in via force-cache/revalidate (hedged on post-15 but correct) |
| nextjs-caching-layers-02 | caching | 2 | 3 | 3 | 1.00 | all four layers distinguished |
| nextjs-caching-revalidate-03 | caching, rendering | 3 | 2 | 2 | 1.00 | TTL + stale-while-revalidate ISR vs built-once static |
| nextjs-caching-ondemand-04 | caching, server-actions | 2 | 2 | 2 | 1.00 | path-vs-tag split; called from action/handler after mutation |
| nextjs-caching-segment-config-05 | caching, rendering | 3 | 2 | 2 | 1.00 | segment-level exports; force-dynamic and force-static both explained |
| nextjs-rendering-static-dynamic-01 | rendering | 3 | 2 | 2 | 1.00 | static by default; dynamic APIs / uncached fetch / force-dynamic triggers |
| nextjs-rendering-static-params-02 | rendering | 2 | 2 | 2 | 1.00 | prerender param list; replaces getStaticPaths; dynamicParams |
| nextjs-rendering-dynamic-apis-03 | rendering, data-fetching | 2 | 2 | 2 | 1.00 | async in v15; reading opts into dynamic rendering |
| nextjs-server-actions-useserver-01 | server-actions | 3 | 3 | 3 | 1.00 | server-run callable fns; file-level vs inline; RPC vs client-bundle boundary |
| nextjs-server-actions-mutation-02 | server-actions, caching | 2 | 2 | 2 | 1.00 | form action + FormData + revalidate/redirect; useActionState/useFormStatus |
| nextjs-server-actions-security-03 | server-actions, route-handlers | 3 | 2 | 2 | 1.00 | public POST endpoint; authn + authz + schema validation inside action |
| nextjs-route-handlers-basics-01 | route-handlers | 2 | 2 | 2 | 1.00 | method exports on Web Request/Response; replaces pages/api; can't coexist with page.tsx |
| nextjs-route-handlers-caching-02 | route-handlers, caching | 3 | 2 | 2 | 1.00 | v14 cached GET / v15 not; force-static + revalidate opt-in |
| nextjs-route-handlers-methods-03 | route-handlers | 1 | 2 | 2 | 1.00 | json()/formData(); nextUrl.searchParams; awaited ctx.params in v15 |
| nextjs-streaming-ssr-01 | streaming | 2 | 2 | 2 | 1.00 | shell-first chunked streaming; TTFB/FCP benefit |
| nextjs-streaming-suspense-02 | streaming, server-components | 2 | 2 | 2 | 1.00 | isolate slow await inside Suspense-wrapped component; rest streams |
| nextjs-streaming-boundary-03 | streaming, app-router | 1 | 2 | 2 | 1.00 | loading.tsx = one coarse segment boundary; Suspense = granular |
| nextjs-metadata-static-01 | metadata | 2 | 2 | 2 | 1.00 | static metadata export; Next merges into head; Server Components only |
| nextjs-metadata-dynamic-02 | metadata | 2 | 2 | 2 | 1.00 | async generateMetadata; fetch memoized with page |
| nextjs-metadata-inherit-03 | metadata | 1 | 2 | 2 | 1.00 | root→leaf shallow merge; title.template / absolute |
| nextjs-metadata-files-04 | metadata | 1 | 2 | 2 | 1.00 | file conventions incl. static vs generated (ImageResponse); auto-wired |
| nextjs-navigation-link-01 | navigation | 2 | 2 | 2 | 1.00 | client-side nav vs full reload; viewport prefetch into Router Cache |
| nextjs-navigation-hooks-02 | navigation, server-components | 2 | 2 | 2 | 1.00 | next/navigation not next/router; client hooks; server reads props/params instead |
| nextjs-navigation-redirect-03 | navigation, rendering | 2 | 2 | 2 | 1.00 | redirect/notFound server-side; throw-based → try/catch swallows; 303 in actions |
| nextjs-navigation-action-redirect-04 | navigation, server-actions | 1 | 2 | 2 | 1.00 | redirect after write + revalidate; keep outside try/catch; aborts execution |

`normalized = min(awarded, full) / full`

## Per-tag subscores

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| caching | 1.00 | 7 | ok | omit (strong) |
| rendering | 1.00 | 7 | ok | omit (strong) |
| data-fetching | 1.00 | 5 | ok | omit (strong) |
| server-actions | 1.00 | 5 | ok | omit (strong) |
| route-handlers | 1.00 | 4 | ok | omit (strong) |
| streaming | 1.00 | 4 | ok | omit (strong) |
| metadata | 1.00 | 4 | ok | omit (strong) |
| navigation | 1.00 | 4 | ok | omit (strong) |
| server-components | 0.93 | 6 | ok | omit (strong) |
| app-router | 0.92 | 6 | ok | omit (strong) |

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 73 / 74 ≈ 98.6%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none** → no derived skill written for
`glm-5.3-flash` on nextjs.

> Grader note: a 98.6% sweep is high enough to warrant a contamination sanity
> check per Rule 0. The answers read as the model's own (hedged wording on
> post-15 caching defaults, extra detail not in the key such as 303 redirect
> status and `title.absolute`, one genuine conceptual miss on RSC "no JS
> shipped / async") rather than copies of the reference, so the run is treated
> as valid.
