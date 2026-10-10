---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-05
stack: nextjs
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — longcat-2.5-preview-free on nextjs (2026-10-05 full re-run)

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump invalidates
> this scorecard — re-benchmark. Baseline rerun 2026-10-05; an earlier 2026-09-28
> scorecard exists beside it (not consulted).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| nextjs-app-router-conventions-01 | app-router | 3 | 3 | 3 | 1.00 |  |
| nextjs-app-router-layout-02 | app-router | 2 | 2 | 2 | 1.00 |  |
| nextjs-app-router-error-03 | app-router | 2 | 2 | 1 | 0.50 | claimed error.tsx catches event handlers/effects; confused layout-scope explanation |
| nextjs-app-router-loading-04 | app-router, streaming | 2 | 2 | 2 | 1.00 |  |
| nextjs-server-components-default-01 | server-components, app-router | 3 | 3 | 3 | 1.00 |  |
| nextjs-server-components-hooks-02 | server-components | 3 | 2 | 2 | 1.00 |  |
| nextjs-server-components-props-03 | server-components, data-fetching | 2 | 2 | 2 | 1.00 |  |
| nextjs-data-fetching-rsc-01 | data-fetching, server-components | 3 | 2 | 2 | 1.00 |  |
| nextjs-data-fetching-nogssp-02 | data-fetching, rendering | 2 | 2 | 2 | 1.00 | id malformed in file (idjs-...) plus a later correctly-id'd short record; graded as the union |
| nextjs-caching-fetch-default-01 | caching, data-fetching | 3 | 3 | 1 | 0.33 | version defaults REVERSED (said 14 uncached, 15 cached); opt-in mechanisms named |
| nextjs-caching-layers-02 | caching | 2 | 3 | 3 | 1.00 |  |
| nextjs-caching-revalidate-03 | caching, rendering | 3 | 2 | 2 | 1.00 |  |
| nextjs-caching-ondemand-04 | caching, server-actions | 2 | 2 | 2 | 1.00 |  |
| nextjs-caching-segment-config-05 | caching, rendering | 3 | 2 | 2 | 1.00 |  |
| nextjs-rendering-static-dynamic-01 | rendering | 3 | 2 | 2 | 1.00 |  |
| nextjs-rendering-static-params-02 | rendering | 2 | 2 | 2 | 1.00 |  |
| nextjs-rendering-dynamic-apis-03 | rendering, data-fetching | 2 | 2 | 2 | 1.00 |  |
| nextjs-server-actions-useserver-01 | server-actions | 3 | 3 | 3 | 1.00 |  |
| nextjs-server-actions-mutation-02 | server-actions, caching | 2 | 2 | 2 | 1.00 |  |
| nextjs-server-actions-security-03 | server-actions, route-handlers | 3 | 2 | 2 | 1.00 |  |
| nextjs-route-handlers-basics-01 | route-handlers | 2 | 2 | 2 | 1.00 |  |
| nextjs-route-handlers-caching-02 | route-handlers, caching | 3 | 2 | 1 | 0.50 | version defaults REVERSED (said 14 uncached, 15 cached); force-static opt-in named |
| nextjs-route-handlers-methods-03 | route-handlers | 1 | 2 | 2 | 1.00 |  |
| nextjs-streaming-ssr-01 | streaming | 2 | 2 | 2 | 1.00 |  |
| nextjs-streaming-suspense-02 | streaming, server-components | 2 | 2 | 2 | 1.00 |  |
| nextjs-streaming-boundary-03 | streaming, app-router | 1 | 2 | 2 | 1.00 |  |
| nextjs-metadata-static-01 | metadata | 2 | 2 | 1 | 0.50 | omitted: client components cannot export metadata |
| nextjs-metadata-dynamic-02 | metadata | 2 | 2 | 1 | 0.50 | omitted: fetch dedup with page via Request Memoization |
| nextjs-metadata-inherit-03 | metadata | 1 | 2 | 2 | 1.00 |  |
| nextjs-metadata-files-04 | metadata | 1 | 2 | 2 | 1.00 |  |
| nextjs-navigation-link-01 | navigation | 2 | 2 | 2 | 1.00 |  |
| nextjs-navigation-hooks-02 | navigation, server-components | 2 | 2 | 2 | 1.00 |  |
| nextjs-navigation-redirect-03 | navigation, rendering | 2 | 2 | 1 | 0.50 | missed that try/catch swallows redirect; advised wrapping in try/catch |
| nextjs-navigation-action-redirect-04 | navigation, server-actions | 1 | 2 | 2 | 1.00 | mixed advice on try/catch but correct end state |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| app-router | 0.92 | 6 | ok | omit (strong) |
| streaming | 1.00 | 4 | ok | omit (strong) |
| server-components | 1.00 | 6 | ok | omit (strong) |
| data-fetching | 0.83 | 5 | ok | omit (strong) |
| rendering | 0.94 | 7 | ok | omit (strong) |
| caching | 0.81 | 7 | ok | omit (strong) |
| server-actions | 1.00 | 5 | ok | omit (strong) |
| route-handlers | 0.83 | 4 | ok | omit (strong) |
| metadata | 0.67 | 4 | ok | **derive** |
| navigation | 0.86 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 66.5/74 = 89.9%
```

## Derivation targets

Tags below threshold (`< 0.75`): **metadata** (0.67, n=4). Observed failures: omitted that `metadata` cannot be exported from a client component, and omitted that `generateMetadata` fetches are deduped with the page's own fetches. (Not derived in this run, per instructions.)
Near-threshold weak spots outside the flagged tag: caching version defaults (Next 14 vs 15 fetch and GET route handler) were stated reversed.
