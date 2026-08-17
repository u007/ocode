---
model_id: mimo-v2.5
model_version: "2.5"
evaluated_via: opencode-go
evaluated_on: 2026-08-17
stack: nextjs
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — mimo-v2.5 on nextjs

> Valid ONLY for `mimo-v2.5` @ `2.5`. A version bump invalidates this
> scorecard — re-benchmark.
>
> Graded from closed-book answers (`../answers/mimo-v2.5.md`) against
> `../questions.yaml` (corpus_rev 1).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| nextjs-app-router-conventions-01 | app-router | 3 | 3 | 3 | 1.00 | folder→URL, page/layout/loading/error/route all correct |
| nextjs-app-router-layout-02 | app-router | 2 | 2 | 2 | 1.00 | layout-persist + template re-mount both nailed |
| nextjs-app-router-error-03 | app-router | 2 | 2 | 2 | 1.00 | client component, error/reset, layout-above exclusion covered |
| nextjs-app-router-loading-04 | app-router, streaming | 2 | 2 | 2 | 1.00 | Suspense wrap + streaming shell |
| nextjs-server-components-default-01 | server-components, app-router | 3 | 3 | 2 | 0.67 | server-by-default + module boundary covered; MISSED "no JS shipped / can be async" |
| nextjs-server-components-hooks-02 | server-components | 3 | 2 | 2 | 1.00 | no-hydration reason + extract-client fix |
| nextjs-server-components-props-03 | server-components, data-fetching | 2 | 2 | 2 | 1.00 | serializable props + children-slot pattern |
| nextjs-data-fetching-rsc-01 | data-fetching, server-components | 3 | 2 | 1 | 0.50 | async RSC await covered; treats getServerSideProps/getStaticProps as "replaced" instead of stating they don't exist in app dir |
| nextjs-data-fetching-nogssp-02 | data-fetching, rendering | 2 | 2 | 1 | 0.50 | Promise.all covered; MISSED isolating slow fetches behind separate Suspense boundaries |
| nextjs-caching-fetch-default-01 | caching, data-fetching | 3 | 3 | 3 | 1.00 | v14 cached / v15 no-store / opt-in — version-correct |
| nextjs-caching-layers-02 | caching | 2 | 3 | 3 | 1.00 | all four layers named and distinguished |
| nextjs-caching-revalidate-03 | caching, rendering | 3 | 2 | 2 | 1.00 | stale-while-revalidate ISR vs built-once static |
| nextjs-caching-ondemand-04 | caching, server-actions | 2 | 2 | 2 | 1.00 | path-vs-tag split, called from action/handler after write |
| nextjs-caching-segment-config-05 | caching, rendering | 3 | 2 | 1 | 0.50 | force-dynamic behavior covered well; MISSED force-static entirely |
| nextjs-rendering-static-dynamic-01 | rendering | 3 | 2 | 2 | 1.00 | static-by-default + dynamic API / opt-out triggers |
| nextjs-rendering-static-params-02 | rendering | 2 | 2 | 2 | 1.00 | prerender param list (SSG) replaces getStaticPaths |
| nextjs-rendering-dynamic-apis-03 | rendering, data-fetching | 2 | 2 | 2 | 1.00 | async in v15 + forces dynamic render |
| nextjs-server-actions-useserver-01 | server-actions | 3 | 3 | 2 | 0.67 | action = server fn callable from client + vs use-client covered; MISSED file-level-vs-inline scoping |
| nextjs-server-actions-mutation-02 | server-actions, caching | 2 | 2 | 2 | 1.00 | form action + FormData + revalidatePath |
| nextjs-server-actions-security-03 | server-actions, route-handlers | 3 | 2 | 2 | 1.00 | public endpoint + authn/authz/validate inside action |
| nextjs-route-handlers-basics-01 | route-handlers | 2 | 2 | 1 | 0.50 | method exports + Web Request/Response covered; MISSED can't-coexist-with-page.tsx |
| nextjs-route-handlers-caching-02 | route-handlers, caching | 3 | 2 | 2 | 1.00 | v14 cached GET / v15+ not / force-static opt-in |
| nextjs-route-handlers-methods-03 | route-handlers | 1 | 2 | 2 | 1.00 | body/query/params incl. awaited params in v15 |
| nextjs-streaming-ssr-01 | streaming | 2 | 2 | 2 | 1.00 | chunked shell-first + TTFB benefit |
| nextjs-streaming-suspense-02 | streaming, server-components | 2 | 2 | 2 | 1.00 | isolate slow in Suspense, rest streams |
| nextjs-streaming-boundary-03 | streaming, app-router | 1 | 2 | 2 | 1.00 | loading.tsx whole-segment vs granular manual Suspense |
| nextjs-metadata-static-01 | metadata | 2 | 2 | 1 | 0.50 | static metadata export covered; MISSED not-available-in-client-components caveat |
| nextjs-metadata-dynamic-02 | metadata | 2 | 2 | 1 | 0.50 | async generateMetadata covered; MISSED server-side fetch dedup (Request Memoization) |
| nextjs-metadata-inherit-03 | metadata | 1 | 2 | 2 | 1.00 | root→leaf merge + title.template |
| nextjs-metadata-files-04 | metadata | 1 | 2 | 2 | 1.00 | file conventions (og/icon/sitemap/robots/manifest) |
| nextjs-navigation-link-01 | navigation | 2 | 2 | 2 | 1.00 | client-side nav + prefetch vs full reload |
| nextjs-navigation-hooks-02 | navigation, server-components | 2 | 2 | 1 | 0.50 | next/navigation not next/router covered; MISSED server components read params/searchParams props instead |
| nextjs-navigation-redirect-03 | navigation, rendering | 2 | 2 | 2 | 1.00 | redirect/notFound server-side + throws/try-catch gotcha |
| nextjs-navigation-action-redirect-04 | navigation, server-actions | 1 | 2 | 1 | 0.50 | call-at-end-of-action covered; WRONG on the throw caveat — told to wrap redirect in try/catch (opposite of correct: keep it outside) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| streaming | 1.00 | 4 | ok | omit (strong) |
| app-router | 0.92 | 6 | ok | omit (strong) |
| caching | 0.92 | 7 | ok | omit (strong) |
| rendering | 0.85 | 7 | ok | omit (strong) |
| route-handlers | 0.89 | 4 | ok | omit (strong) |
| server-actions | 0.83 | 4 | ok | omit (strong) |
| navigation | 0.79 | 4 | ok | omit (strong) |
| data-fetching | 0.79 | 5 | ok | omit (strong) |
| server-components | 0.77 | 6 | ok | omit (strong) |
| metadata | 0.67 | 4 | ok | **derive** |

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 63.5 / 74 ≈ 86%
```

## Derivation targets

Tags below threshold (`< 0.75`): **metadata** → feed into
`derived/nextjs.mimo-v2.5.SKILL.md`.
</content>
