---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: nextjs
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — cat-jam-free on nextjs

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

opencode-go free/alpha model; no version published (recorded as `"alpha"`). The
model self-identifies as "CatJam, developed by LitterBox AI".

Closed-book (Rule 0): the answerer was `ocode run -m opencode-go/cat-jam-free -effort med`
in an empty scratch cwd with only the `_prompts/nextjs.md` sheet attached; the
session sqlite shows exactly 1 user + 1 assistant message and 0 tool calls.
Headless runs inject ocode's bundled skill-catalogue digest into the system
prompt. Graded from `../answers/cat-jam-free.md` (34 records, ids match the
corpus 1:1). Answers are terse (2–4 sentences), worded independently of the
reference answers, and lose points mainly by omission rather than by wrong
claims — no sign of answer-key contamination.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| nextjs-app-router-conventions-01 | app-router | 3 | 3 | 3 | 1.00 | route "defines an HTTP Route Handler instead of a page" accepted as the page/route exclusivity |
| nextjs-app-router-layout-02 | app-router | 2 | 2 | 2 | 1.00 | |
| nextjs-app-router-error-03 | app-router | 2 | 2 | 2 | 1.00 | own-layout exclusion + global-error stated; event handlers not mentioned |
| nextjs-app-router-loading-04 | app-router, streaming | 2 | 2 | 2 | 1.00 | |
| nextjs-server-components-default-01 | server-components, app-router | 3 | 3 | 1 | 0.33 | (judgment) server-default correct. `"use client"` described as a boundary "in that module" with no statement that it pulls the module's imports into the client bundle, and "everything not marked is server-rendered unless specifically client" implies per-file marking — withheld. Never says server components ship no JS / can be async (only the client half of point 3) — withheld |
| nextjs-server-components-hooks-02 | server-components | 3 | 2 | 2 | 1.00 | |
| nextjs-server-components-props-03 | server-components, data-fetching | 2 | 2 | 2 | 1.00 | serializable list + Server Actions as the only function exception; no-functions rule implied, not stated |
| nextjs-data-fetching-rsc-01 | data-fetching, server-components | 3 | 2 | 2 | 1.00 | |
| nextjs-data-fetching-nogssp-02 | data-fetching, rendering | 2 | 2 | 2 | 1.00 | Promise.all + separate Suspense-wrapped components both given |
| nextjs-caching-fetch-default-01 | caching, data-fetching | 3 | 3 | 3 | 1.00 | |
| nextjs-caching-layers-02 | caching | 2 | 3 | 3 | 1.00 | |
| nextjs-caching-revalidate-03 | caching, rendering | 3 | 2 | 2 | 1.00 | |
| nextjs-caching-ondemand-04 | caching, server-actions | 2 | 2 | 2 | 1.00 | |
| nextjs-caching-segment-config-05 | caching, rendering | 3 | 2 | 1 | 0.50 | lists the `dynamic` values but only says WHEN to use `force-dynamic`, not what it does (render every request, bypass Data/Full Route cache); `force-static` named, never explained |
| nextjs-rendering-static-dynamic-01 | rendering | 3 | 2 | 2 | 1.00 | |
| nextjs-rendering-static-params-02 | rendering | 2 | 2 | 2 | 1.00 | |
| nextjs-rendering-dynamic-apis-03 | rendering, data-fetching | 2 | 2 | 2 | 1.00 | correctly notes `params` alone need not force dynamic |
| nextjs-server-actions-useserver-01 | server-actions | 3 | 3 | 3 | 1.00 | file-vs-function placement only named ("an async function or file"), accepted; thin |
| nextjs-server-actions-mutation-02 | server-actions, caching | 2 | 2 | 2 | 1.00 | FormData not named |
| nextjs-server-actions-security-03 | server-actions, route-handlers | 3 | 2 | 2 | 1.00 | |
| nextjs-route-handlers-basics-01 | route-handlers | 2 | 2 | 2 | 1.00 | |
| nextjs-route-handlers-caching-02 | route-handlers, caching | 3 | 2 | 2 | 1.00 | Next 14 default phrased as "a static handler could be cached" — accepted |
| nextjs-route-handlers-methods-03 | route-handlers | 1 | 2 | 2 | 1.00 | |
| nextjs-streaming-ssr-01 | streaming | 2 | 2 | 2 | 1.00 | |
| nextjs-streaming-suspense-02 | streaming, server-components | 2 | 2 | 2 | 1.00 | |
| nextjs-streaming-boundary-03 | streaming, app-router | 1 | 2 | 2 | 1.00 | |
| nextjs-metadata-static-01 | metadata | 2 | 2 | 1 | 0.50 | static `metadata` export + head-tag generation correct; never states it is Server-Component-only (a `"use client"` file cannot export `metadata`) |
| nextjs-metadata-dynamic-02 | metadata | 2 | 2 | 1 | 0.50 | `generateMetadata` correct; dedup with the page given only as "reuse fetch/cache behavior to avoid duplicate work" (developer action), never that fetch is automatically memoized across generateMetadata and page; "runs on the server" absent |
| nextjs-metadata-inherit-03 | metadata | 1 | 2 | 2 | 1.00 | |
| nextjs-metadata-files-04 | metadata | 1 | 2 | 2 | 1.00 | |
| nextjs-navigation-link-01 | navigation | 2 | 2 | 2 | 1.00 | |
| nextjs-navigation-hooks-02 | navigation, server-components | 2 | 2 | 2 | 1.00 | `next/router` contrast not named, but `next/navigation` source stated |
| nextjs-navigation-redirect-03 | navigation, rendering | 2 | 2 | 2 | 1.00 | |
| nextjs-navigation-action-redirect-04 | navigation, server-actions | 1 | 2 | 2 | 1.00 | throw + "halts further code" stated; try/catch placement implicit |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| app-router | 0.846 | 6 | ok | **derive** |
| server-components | 0.87 | 6 | ok | omit (strong) |
| data-fetching | 1.00 | 5 | ok | omit (strong) |
| caching | 0.92 | 7 | ok | omit (strong) |
| rendering | 0.91 | 7 | ok | omit (strong) |
| server-actions | 1.00 | 5 | ok | omit (strong) |
| route-handlers | 1.00 | 4 | ok | omit (strong) |
| streaming | 1.00 | 4 | ok | omit (strong) |
| metadata | 0.67 | 4 | ok | **derive** |
| navigation | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

app-router sits just under threshold (0.846) and the whole deficit is one
weight-3 question (`server-components-default-01`), which it shares with
server-components (0.867, just above). Borderline — revalidate with a with-skill run.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 68.5/74 = 92.6%
```

## Derivation targets

Tags below threshold (`< 0.85`): **app-router** (0.846, n=6), **metadata** (0.67, n=4)
→ `derived/nextjs.cat-jam-free.SKILL.md`.

Observed failures feeding the skill: (app-router) did not state that `"use client"`
is a module-graph entry point whose imports all become client code, implied every
client file must be marked, and never said Server Components ship no JS and can be
`async`. (metadata) omitted that `metadata`/`generateMetadata` are Server-Component-only;
treated `generateMetadata`/page fetch dedup as a manual concern rather than automatic
Request Memoization.
