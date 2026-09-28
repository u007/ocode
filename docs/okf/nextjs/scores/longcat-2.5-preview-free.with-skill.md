---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: nextjs
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — longcat-2.5-preview-free on nextjs (with-skill)

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump
> invalidates this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** These answers were produced closed-book with
> the derived tuning skill `derived/nextjs.longcat-2.5-preview-free.SKILL.md`
> (`nextjs-tuning-longcat-2.5-preview-free`) prepended to the answerer prompt
> as active guidance (`../answers/longcat-2.5-preview-free.with-skill.md`,
> produced via `ocode run` from an isolated dir; session audited: 1 user + 1
> assistant turn, zero tool calls). The answerer never saw `questions.yaml` or
> the rubric. Graded against `../questions.yaml` (corpus_rev 1) to the same
> strict standard as the baseline `scores/longcat-2.5-preview-free.md`. No
> derived skill is written or modified from this run.
>
> Target tags for this skill: **metadata, data-fetching**.

Contamination check: no answer copies the reference. The skill's directives
surface nearly verbatim where they apply (caching-fetch-default-01 repeats
"'Next 15 caches fetch by default' is false — it is the reverse";
data-fetching-nogssp-02 reuses the skill's two numbered fixes;
metadata-static-01 / -dynamic-02 / -inherit-03 echo the Server-Component-only,
Request Memoization and shallow-merge lines). That is expected absorption, not
answer-key access. Non-target answers still carry model-own errors absent from
the key (`route.tsx` instead of `route.ts`; error.tsx "catches its own
segment's layout"; "children … unless they have their own `use server`
boundary"; no inline `"use server"` form), consistent with a blind run.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| nextjs-app-router-conventions-01 | app-router | 3 | 3 | 3 | 1.00 | all five files, folder→URL incl. `[slug]`; route = API endpoint (calls it `route.tsx`; page-exclusivity not stated, same leniency as baseline route-handlers-basics-01) |
| nextjs-app-router-layout-02 | app-router | 2 | 2 | 2 | 1.00 | layout persists/state kept; template new instance per navigation |
| nextjs-app-router-error-03 | app-router | 2 | 2 | 1 | 0.50 | now correctly "must be a Client Component" (baseline said Server) but still no `error`/`reset` props; scope point: excludes parent layouts and event handlers, but again wrongly claims it catches its own segment's `layout`. Net 1 point, same as baseline |
| nextjs-app-router-loading-04 | app-router, streaming | 2 | 2 | 2 | 1.00 | auto Suspense boundary; streaming, rest renders immediately |
| nextjs-server-components-default-01 | server-components, app-router | 3 | 3 | 2 | 0.67 | server by default; `"use client"` boundary covering imports. MISSED point 3: never says server components ship no JS / can be async (baseline had it) |
| nextjs-server-components-hooks-02 | server-components | 3 | 2 | 2 | 1.00 | server-only, no DOM/state → no hooks/events; `"use client"` or extract small client child |
| nextjs-server-components-props-03 | server-components, data-fetching | 2 | 2 | 2 | 1.00 | serializable props, no functions/class instances; children-slot pattern with example |
| nextjs-data-fetching-rsc-01 | data-fetching, server-components | 3 | 2 | 2 | 1.00 | async RSC await; "those functions **do not exist in the `app/` directory**" stated (baseline 0.50) |
| nextjs-data-fetching-nogssp-02 | data-fetching, rendering | 2 | 2 | 2 | 1.00 | `Promise.all` AND separate async components in their own `<Suspense>` streaming independently (baseline 0.50) |
| nextjs-caching-fetch-default-01 | caching, data-fetching | 3 | 3 | 3 | 1.00 | Next 13/14 cached (`force-cache`), Next 15+/16 `no-store`, opt-in via `force-cache`/`next.revalidate` — direction now correct (baseline 0.33, inverted) |
| nextjs-caching-layers-02 | caching | 2 | 3 | 3 | 1.00 | all four layers named and distinguished |
| nextjs-caching-revalidate-03 | caching, rendering | 3 | 2 | 2 | 1.00 | stale window; serve-stale-regenerate-in-background ISR vs built-once static |
| nextjs-caching-ondemand-04 | caching, server-actions | 2 | 2 | 2 | 1.00 | in Server Action/Route Handler after mutation; path vs tag |
| nextjs-caching-segment-config-05 | caching, rendering | 3 | 2 | 2 | 1.00 | `dynamic`/`revalidate` route exports; force-dynamic = fresh render per request, no caching (force-static only listed, not explained) |
| nextjs-rendering-static-dynamic-01 | rendering | 3 | 2 | 2 | 1.00 | static by default; dynamic via cookies/headers/searchParams, force-dynamic, `no-store` |
| nextjs-rendering-static-params-02 | rendering | 2 | 2 | 2 | 1.00 | param list → build-time prerender; `getStaticPaths` equivalent |
| nextjs-rendering-dynamic-apis-03 | rendering, data-fetching | 2 | 2 | 2 | 1.00 | async in 15+, must await; reading forces dynamic |
| nextjs-server-actions-useserver-01 | server-actions | 3 | 3 | 2 | 0.67 | server-run, client-callable (POST); contrasted with `"use client"`. MISSED inline function-body `"use server"` — only file-level (same as baseline) |
| nextjs-server-actions-mutation-02 | server-actions, caching | 2 | 2 | 2 | 1.00 | `"use server"` fn on form action with FormData → write → `revalidatePath`/`redirect` |
| nextjs-server-actions-security-03 | server-actions, route-handlers | 3 | 2 | 2 | 1.00 | public POST endpoint; authn/authz + input validation in every action |
| nextjs-route-handlers-basics-01 | route-handlers | 2 | 2 | 2 | 1.00 | method exports, Web Request/Response, replaces `pages/api` |
| nextjs-route-handlers-caching-02 | route-handlers, caching | 3 | 2 | 2 | 1.00 | Next 14 GET cached, 15+ not — direction now correct (baseline inverted, 0.50); `force-static` / `revalidate` opt-in |
| nextjs-route-handlers-methods-03 | route-handlers | 1 | 2 | 2 | 1.00 | json/formData/text; `new URL(request.url).searchParams`; awaited `params` from context arg |
| nextjs-streaming-ssr-01 | streaming | 2 | 2 | 2 | 1.00 | chunks as parts resolve, Suspense decides what streams; TTFB / no head-of-line blocking |
| nextjs-streaming-suspense-02 | streaming, server-components | 2 | 2 | 2 | 1.00 | slow part as its own async `SlowSection` inside `<Suspense>`; rest sent immediately |
| nextjs-streaming-boundary-03 | streaming, app-router | 1 | 2 | 2 | 1.00 | segment-wide vs granular |
| nextjs-metadata-static-01 | metadata | 2 | 2 | 2 | 1.00 | static `metadata` export from page/layout; "Server Component only — a `"use client"` file cannot export `metadata`" (baseline 0.50). Head-tag generation implied by the export, not spelled out |
| nextjs-metadata-dynamic-02 | metadata | 2 | 2 | 2 | 1.00 | async `generateMetadata` with awaited params; deduped with page fetches via Request Memoization (baseline 0.50) |
| nextjs-metadata-inherit-03 | metadata | 1 | 2 | 2 | 1.00 | root→leaf shallow merge, nested objects/arrays replaced (baseline's "arrays merge" inaccuracy fixed); `title.template` `%s` + default |
| nextjs-metadata-files-04 | metadata | 1 | 2 | 2 | 1.00 | icon/apple-icon/opengraph-image/sitemap/robots/manifest; static file or `.ts` generator, auto-served |
| nextjs-navigation-link-01 | navigation | 2 | 2 | 2 | 1.00 | client-side nav, no reload; automatic prefetch in production (viewport trigger not named, as baseline) |
| nextjs-navigation-hooks-02 | navigation, server-components | 2 | 2 | 2 | 1.00 | `next/navigation`; client-only → `"use client"`; no `next/router` contrast or server-props alternative (as baseline) |
| nextjs-navigation-redirect-03 | navigation, rendering | 2 | 2 | 2 | 1.00 | redirect / notFound (404 UI), server-side; throw internally, don't call inside try/catch |
| nextjs-navigation-action-redirect-04 | navigation, server-actions | 1 | 2 | 2 | 1.00 | redirect after write + revalidate; `NEXT_REDIRECT` swallowed by try/catch caveat |

`normalized = min(awarded, full) / full`

## Per-tag subscores

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

| tag | baseline | with-skill | n | trust | target? |
|-----|---------:|-----------:|--:|-------|---------|
| data-fetching | 0.63 | 1.00 | 5 | ok | **yes** |
| metadata | 0.67 | 1.00 | 4 | ok | **yes** |
| caching | 0.81 | 1.00 | 7 | ok | no |
| route-handlers | 0.83 | 1.00 | 4 | ok | no |
| rendering | 0.94 | 1.00 | 7 | ok | no |
| streaming | 1.00 | 1.00 | 4 | ok | no |
| navigation | 1.00 | 1.00 | 4 | ok | no |
| server-components | 0.90 | 0.93 | 6 | ok | no |
| server-actions | 0.91 | 0.91 | 5 | ok | no |
| app-router | 0.92 | 0.85 | 6 | ok | no |

Per-tag arithmetic (with-skill): data-fetching 12/12; metadata 6/6; caching
18/18; route-handlers 9/9; rendering 17/17; streaming 7/7; navigation 7/7;
server-components 14/15; server-actions 10/11; app-router 11/13.

## Target tags verdict

| tag | baseline | with-skill | verdict |
|-----|---------:|-----------:|---------|
| data-fetching | 0.63 | 1.00 | **PASS** (≥ 0.75, saturated) |
| metadata | 0.67 | 1.00 | **PASS** (≥ 0.75, saturated) |

Validation verdict: **SUCCESS** — both target tags cross 0.75 and saturate.
Every baseline miss the skill named landed:

- data-fetching-rsc-01: now states gSSP/gSP "do not exist in the `app/`
  directory".
- data-fetching-nogssp-02: now gives the separate-Suspense-component fix
  alongside `Promise.all`.
- caching-fetch-default-01: Next 14 vs 15+ default no longer inverted.
- metadata-static-01: Server-Component-only restriction stated; the garbled
  `template` wording is gone.
- metadata-dynamic-02: Request Memoization dedup stated.
- metadata-inherit-03: shallow-merge correction absorbed (no "arrays merge").

No FAILs.

Non-target drift (not acted on, single-sample variance):

- caching +0.19, route-handlers +0.17: the skill's `fetch`-direction line
  generalized to the GET Route Handler default, which the baseline also
  inverted (route-handlers-caching-02 0.50 → 1.00). This is a real spillover
  gain, not noise, but those tags weren't targeted.
- app-router −0.07: server-components-default-01 dropped the
  "no JS shipped / can be async" point (baseline had it). error-03 flipped
  its client-component claim to correct but still omits `error`/`reset` and
  still misstates the scope, so it stays 0.50.
- server-components +0.03 (net of the default-01 drop), server-actions
  unchanged (inline `"use server"` still missing), rendering +0.06.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 71 / 74 ≈ 95.9%
```

(Baseline: 64 / 74 ≈ 86%.)

## Derivation targets

None — this is a validation run; no derived skill is written from it. All
tags are ≥ 0.75.
