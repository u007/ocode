---
model_id: muse-spark-1.2
model_version: "1.2"
evaluated_via: opencode-go
evaluated_on: 2026-08-20
stack: nextjs
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — muse-spark-1.2 on nextjs

> Valid ONLY for `muse-spark-1.2` @ `1.2`. A version bump invalidates this
> scorecard — re-benchmark.
>
> Graded from closed-book answers (`../answers/muse-spark-1.2.md`) against
> `../questions.yaml` (corpus_rev 1). Answers were produced by an isolated
> `ocode2 run` subprocess given only `docs/okf/_prompts/nextjs.md`; the raw
> transcript shows zero tool invocations (pure LLM completion, no repo/web
> access) — see `docs/okf/HOW-TO-EVALUATE.md` Rule 0.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| nextjs-app-router-conventions-01 | app-router | 3 | 3 | 3 | 1.00 | page/layout/loading/error/route all correct, folder→URL covered |
| nextjs-app-router-layout-02 | app-router | 2 | 2 | 2 | 1.00 | layout persists/no remount + template fresh-instance-per-nav both nailed |
| nextjs-app-router-error-03 | app-router | 2 | 2 | 2 | 1.00 | client component + error/reset, correct catch/no-catch scope incl. global-error.tsx |
| nextjs-app-router-loading-04 | app-router, streaming | 2 | 2 | 2 | 1.00 | auto-Suspense wrap + instant fallback/streaming |
| nextjs-server-components-default-01 | server-components, app-router | 3 | 3 | 3 | 1.00 | server-by-default, use client as module boundary, zero-JS + async covered |
| nextjs-server-components-hooks-02 | server-components | 3 | 2 | 2 | 1.00 | no-hydration reason + extract-to-client fix |
| nextjs-server-components-props-03 | server-components, data-fetching | 2 | 2 | 2 | 1.00 | serializable props + children-slot pattern with example |
| nextjs-data-fetching-rsc-01 | data-fetching, server-components | 3 | 2 | 2 | 1.00 | async RSC await + explicit "no gSSP/gSP in app dir, Pages Router only" |
| nextjs-data-fetching-nogssp-02 | data-fetching, rendering | 2 | 2 | 2 | 1.00 | Promise.all parallelization + Suspense-isolation alternative |
| nextjs-caching-fetch-default-01 | caching, data-fetching | 3 | 3 | 3 | 1.00 | v13-14 cached / v15+ uncached / explicit opt-in mechanism |
| nextjs-caching-layers-02 | caching | 2 | 3 | 3 | 1.00 | all four layers named and correctly distinguished |
| nextjs-caching-revalidate-03 | caching, rendering | 3 | 2 | 2 | 1.00 | stale-while-revalidate ISR vs never-expires static |
| nextjs-caching-ondemand-04 | caching, server-actions | 2 | 2 | 2 | 1.00 | path-vs-tag split, called from Server Action/Route Handler after write |
| nextjs-caching-segment-config-05 | caching, rendering | 3 | 2 | 2 | 1.00 | dynamic values + revalidate + correct force-dynamic use case |
| nextjs-rendering-static-dynamic-01 | rendering | 3 | 2 | 2 | 1.00 | static-by-default + full list of dynamic triggers, bonus PPR mention |
| nextjs-rendering-static-params-02 | rendering | 2 | 2 | 2 | 1.00 | prerender param list (SSG) + getStaticPaths equivalence + dynamicParams |
| nextjs-rendering-dynamic-apis-03 | rendering, data-fetching | 2 | 2 | 2 | 1.00 | async in v15 + forces dynamic render |
| nextjs-server-actions-useserver-01 | server-actions | 3 | 3 | 3 | 1.00 | server-run callable fn + file-vs-inline scoping + distinct from use client |
| nextjs-server-actions-mutation-02 | server-actions, caching | 2 | 2 | 2 | 1.00 | form action + FormData + revalidatePath/redirect after write |
| nextjs-server-actions-security-03 | server-actions, route-handlers | 3 | 2 | 2 | 1.00 | public endpoint + authn/authz/validate inside action, thorough |
| nextjs-route-handlers-basics-01 | route-handlers | 2 | 2 | 1 | 0.50 | method exports + Web Request/Response covered; MISSED can't-coexist-with-page.tsx |
| nextjs-route-handlers-caching-02 | route-handlers, caching | 3 | 2 | 2 | 1.00 | v13-14 cached / v15+ not / force-static opt-in |
| nextjs-route-handlers-methods-03 | route-handlers | 1 | 2 | 2 | 1.00 | body/query/params incl. awaited params in v15 |
| nextjs-streaming-ssr-01 | streaming | 2 | 2 | 2 | 1.00 | chunked shell-first + TTFB/FCP benefit |
| nextjs-streaming-suspense-02 | streaming, server-components | 2 | 2 | 2 | 1.00 | isolate slow in own async component + Suspense, rest streams immediately |
| nextjs-streaming-boundary-03 | streaming, app-router | 1 | 2 | 2 | 1.00 | loading.tsx whole-segment vs granular manual Suspense |
| nextjs-metadata-static-01 | metadata | 2 | 2 | 2 | 1.00 | static metadata export + auto head tags + server-component-only caveat |
| nextjs-metadata-dynamic-02 | metadata | 2 | 2 | 2 | 1.00 | async generateMetadata + server-side + request-memoization dedup |
| nextjs-metadata-inherit-03 | metadata | 1 | 2 | 2 | 1.00 | root→leaf merge + title.template with default/`%s` behavior |
| nextjs-metadata-files-04 | metadata | 1 | 2 | 2 | 1.00 | file conventions (icon/opengraph-image/sitemap/robots/manifest) |
| nextjs-navigation-link-01 | navigation | 2 | 2 | 2 | 1.00 | client-side nav + prefetch-in-viewport vs full reload |
| nextjs-navigation-hooks-02 | navigation, server-components | 2 | 2 | 1 | 0.50 | next/navigation not next/router covered; MISSED server components read params/searchParams props instead |
| nextjs-navigation-redirect-03 | navigation, rendering | 2 | 2 | 2 | 1.00 | redirect/notFound server-side + throws/try-catch gotcha |
| nextjs-navigation-action-redirect-04 | navigation, server-actions | 1 | 2 | 2 | 1.00 | call after write/revalidate, correctly keeps redirect OUTSIDE try/catch |

`normalized = min(awarded, full) / full`

## Per-tag subscores

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| app-router | 1.00 | 6 | ok | omit (strong) |
| caching | 1.00 | 7 | ok | omit (strong) |
| data-fetching | 1.00 | 5 | ok | omit (strong) |
| metadata | 1.00 | 4 | ok | omit (strong) |
| rendering | 1.00 | 7 | ok | omit (strong) |
| server-actions | 1.00 | 4 | ok | omit (strong) |
| streaming | 1.00 | 4 | ok | omit (strong) |
| server-components | 0.93 | 6 | ok | omit (strong) |
| route-handlers | 0.89 | 4 | ok | omit (strong) |
| navigation | 0.86 | 4 | ok | omit (strong) |

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 72 / 74 ≈ 97%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none**. Every tag scored ≥ 0.86, so no
`derived/nextjs.muse-spark-1.2.SKILL.md` is generated.

## Contamination check

Score is very high (97%, 32/34 questions perfect). Assessed as **plausible,
not leakage**:

- The answerer ran in an isolated `/tmp/kaizen-nextjs-answer` subprocess given
  only `_prompts/nextjs.md` (id+question, no answer key); the raw transcript
  shows zero tool invocations — no `read`/`grep`/`websearch`/`webfetch` calls,
  pure text completion.
- `tencent/hy3` scored **99%** (73/74) on this exact same closed-book nextjs
  corpus (see `../scores/tencent__hy3.md`) — a second, independently-run model
  landing in the same high range corroborates that this corpus is simply
  well-covered standard material (App Router conventions, the widely-blogged
  Next 14→15 `fetch`-caching-default flip, Server Actions/RSC boundaries),
  not a per-run artifact.
- The two misses that did occur (`route-handlers-basics-01`,
  `navigation-hooks-02`) are narrow, specific omissions (a mutual-exclusivity
  detail; the "read params/searchParams as props in Server Components"
  alternative) — exactly the kind of granular slip a genuinely-answering
  model makes, not what wholesale rubric-copying would look like. No
  near-verbatim rubric phrasing was observed in the answers.
