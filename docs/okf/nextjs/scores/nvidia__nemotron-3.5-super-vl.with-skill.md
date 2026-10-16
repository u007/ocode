---
model_id: nvidia/nemotron-3.5-super-vl
model_version: "3.5"
evaluated_via: openrouter
evaluated_on: 2026-10-16
stack: nextjs
stack_corpus_rev: 1
threshold: 0.85
validation: with-skill
---

# Scorecard — nvidia/nemotron-3.5-super-vl on nextjs (with-skill validation)

> Valid ONLY for `nvidia/nemotron-3.5-super-vl` @ `3.5`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers:
> `nextjs/answers/nvidia__nemotron-3.5-super-vl.with-skill.md`, produced closed-book
> with the current derived skill `derived/nextjs.nvidia__nemotron-3.5-super-vl.SKILL.md`
> (`nextjs-tuning-nvidia-nemotron-3.5-super-vl`) prepended to the answerer prompt as
> active guidance (session audited: 1 user + 1 assistant message, 0 tool calls).
> Baseline: `scores/nvidia__nemotron-3.5-super-vl.md`. No new derived skill is
> produced from this run.

> Target tags for this skill (baseline < 0.85): **caching, route-handlers**.

## Provenance

Closed-book (Rule 0). Answerer: `ocode run -m openrouter/nvidia/nemotron-3.5-super-vl:free -effort med`
under the same tool-denying isolated HOME as the baseline, in a scratch cwd holding only
`_prompts/nextjs.md` with the derived skill body (front matter stripped) prepended; no
answer key in context. The session shows exactly 1 user + 1 assistant message and 0 tool
calls. All 34 ids came back once, none empty, no duplicates, no thinking-aloud, no
leaked tool-call frames, and no answer cites a tool or skill. Graded with the same
standards as the baseline (semicolon-separated rubric halves count half; a materially
wrong claim about the construct a point names withholds half of that point).

Remaining fabricated or misnamed APIs, all outside the target tags: `redirect(url,
{ permanent: true })` persists (that is `permanentRedirect()`); new this run are
`generateSitemap` / `generateRobots` as the export names for `sitemap.ts` / `robots.ts`
(the file's default export is what Next calls, conventionally `sitemap()` / `robots()`)
and `reset()` "from `next/navigation`" (it is a prop of `error.tsx`). Also new: the
claim that `loading.tsx` is "sticky" and "generally does not reappear" on later
navigations, and that reading `params` in `generateMetadata` makes a route dynamic.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| nextjs-app-router-conventions-01 | app-router | 3 | 3 | 3 | 1.00 | now states the page/route mutual exclusion |
| nextjs-app-router-layout-02 | app-router | 2 | 2 | 2 | 1.00 | |
| nextjs-app-router-error-03 | app-router | 2 | 2 | 2 | 1.00 | tangential: `reset()` attributed to `next/navigation` (it is a prop) |
| nextjs-app-router-loading-04 | app-router, streaming | 2 | 2 | 2 | 1.00 | tangential: calls the loading UI "sticky", claims it generally does not reappear on later navigations (it shows whenever the segment's payload is fetched) |
| nextjs-server-components-default-01 | server-components, app-router | 3 | 3 | 2.5 | 0.83 | (judgment, unchanged) client half of point 3 present; still never states server components ship no JS / can be async |
| nextjs-server-components-hooks-02 | server-components | 3 | 2 | 2 | 1.00 | |
| nextjs-server-components-props-03 | server-components, data-fetching | 2 | 2 | 2 | 1.00 | |
| nextjs-data-fetching-rsc-01 | data-fetching, server-components | 3 | 2 | 2 | 1.00 | |
| nextjs-data-fetching-nogssp-02 | data-fetching, rendering | 2 | 2 | 1 | 0.50 | unchanged: Promise.all correct, Suspense-isolation alternative still absent (Request Memoization tangent instead) |
| nextjs-caching-fetch-default-01 | caching, data-fetching | 3 | 3 | 3 | 1.00 | |
| nextjs-caching-layers-02 | caching | 2 | 3 | 3 | 1.00 | all four layers, Request Memoization first; static-asset cache explicitly excluded (fixed from baseline) |
| nextjs-caching-revalidate-03 | caching, rendering | 3 | 2 | 2 | 1.00 | |
| nextjs-caching-ondemand-04 | caching, server-actions | 2 | 2 | 2 | 1.00 | |
| nextjs-caching-segment-config-05 | caching, rendering | 3 | 2 | 2 | 1.00 | correct four-value enum, `force-static` described, `revalidate = false` vs `0` separated (fixed from baseline) |
| nextjs-rendering-static-dynamic-01 | rendering | 3 | 2 | 2 | 1.00 | tangential: "unresolved `params`" listed as a dynamic trigger |
| nextjs-rendering-static-params-02 | rendering | 2 | 2 | 2 | 1.00 | |
| nextjs-rendering-dynamic-apis-03 | rendering, data-fetching | 2 | 2 | 2 | 1.00 | |
| nextjs-server-actions-useserver-01 | server-actions | 3 | 3 | 3 | 1.00 | (judgment) now names the RPC-endpoint nature, so the contrast point is awarded; still simplifies `"use client"` to "executes in the browser" |
| nextjs-server-actions-mutation-02 | server-actions, caching | 2 | 2 | 2 | 1.00 | now uses `useActionState` |
| nextjs-server-actions-security-03 | server-actions, route-handlers | 3 | 2 | 2 | 1.00 | |
| nextjs-route-handlers-basics-01 | route-handlers | 2 | 2 | 2 | 1.00 | now states the page/route mutual exclusion |
| nextjs-route-handlers-caching-02 | route-handlers, caching | 3 | 2 | 2 | 1.00 | names `export const dynamic = 'force-static'` + `revalidate`, headers do not opt in, non-GET never cached (fixed from baseline) |
| nextjs-route-handlers-methods-03 | route-handlers | 1 | 2 | 2 | 1.00 | no longer mentions a `searchParams` prop |
| nextjs-streaming-ssr-01 | streaming | 2 | 2 | 2 | 1.00 | |
| nextjs-streaming-suspense-02 | streaming, server-components | 2 | 2 | 2 | 1.00 | |
| nextjs-streaming-boundary-03 | streaming, app-router | 1 | 2 | 2 | 1.00 | the "not navigation-aware" claim is gone; tangential: "sticky across navigations" for `loading.tsx` |
| nextjs-metadata-static-01 | metadata | 2 | 2 | 1.5 | 0.75 | unchanged: never states `metadata` is unavailable in `"use client"` files |
| nextjs-metadata-dynamic-02 | metadata | 2 | 2 | 2 | 1.00 | tangential: says reading `params` makes the route dynamic |
| nextjs-metadata-inherit-03 | metadata | 1 | 2 | 2 | 1.00 | |
| nextjs-metadata-files-04 | metadata | 1 | 2 | 1.5 | 0.75 | (judgment) file conventions present, but the code-file half names non-existent exports `generateSitemap` / `generateRobots` (Next calls the default export); baseline had this right |
| nextjs-navigation-link-01 | navigation | 2 | 2 | 2 | 1.00 | tangential: prefetch "on hover" |
| nextjs-navigation-hooks-02 | navigation, server-components | 2 | 2 | 2 | 1.00 | |
| nextjs-navigation-redirect-03 | navigation, rendering | 2 | 2 | 1.5 | 0.75 | (judgment) `not-found.tsx` now correct, but `redirect(url, { permanent: true })` persists (it is `permanentRedirect()`; the second arg is `type`); throw/try-catch gotcha correct |
| nextjs-navigation-action-redirect-04 | navigation, server-actions | 1 | 2 | 2 | 1.00 | try/catch caveat now stated (improved from baseline) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | Δ | n | trust | target? |
|-----|---------:|-----------:|--:|--:|-------|---------|
| app-router | 0.94 | 0.96 | +0.02 | 6 | ok | no |
| server-components | 0.97 | 0.97 | 0.00 | 6 | ok | no |
| data-fetching | 0.92 | 0.92 | 0.00 | 5 | ok | no |
| caching | 0.84 | 1.00 | +0.16 | 7 | ok | **yes** |
| rendering | 0.87 | 0.91 | +0.04 | 7 | ok | no |
| server-actions | 0.93 | 1.00 | +0.07 | 5 | ok | no |
| route-handlers | 0.83 | 1.00 | +0.17 | 4 | ok | **yes** |
| streaming | 0.96 | 1.00 | +0.04 | 4 | ok | no |
| metadata | 0.92 | 0.88 | -0.04 | 4 | ok | no |
| navigation | 0.89 | 0.93 | +0.04 | 4 | ok | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: app-router 12.5/13 (baseline 12.25/13); server-components 14.5/15;
data-fetching 11/12; caching 18/18 (baseline 15.08/18); rendering 15.5/17 (baseline
14.75/17); server-actions 11/11 (baseline 10.25/11); route-handlers 9/9 (baseline
7.5/9); streaming 7/7 (baseline 6.75/7); metadata 5.25/6 (baseline 5.5/6); navigation
6.5/7 (baseline 6.25/7).

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 71.25 / 74 = 96.3%
```

Baseline 67.58 / 74 = 91.3%. Recovered: caching-layers-02, caching-segment-config-05,
route-handlers-caching-02 (the three target losses), plus server-actions-useserver-01,
streaming-boundary-03 and navigation-action-redirect-04. One new loss:
metadata-files-04 (invented export names). Unchanged losses: server-components-default-01,
data-fetching-nogssp-02, metadata-static-01, navigation-redirect-03.

## Target-tag verdict

| tag | baseline | with-skill | pass (≥ 0.85)? |
|-----|---------:|-----------:|----------------|
| caching | 0.84 | 1.00 | **PASS** |
| route-handlers | 0.83 | 1.00 | **PASS** |

Both target tags reach the threshold; every caching and route-handlers question scores
full marks. The skill is validated for nvidia/nemotron-3.5-super-vl @ 3.5. Each failure
the skill cites is fixed in the skill's own terms: the four caching layers are named with
Request Memoization first and the static-asset cache excluded; `export const dynamic` is
given its real four values with `force-static` described; `revalidate = false` and `0`
are distinguished; the GET-handler opt-in is `export const dynamic = 'force-static'`
with `Cache-Control` headers explicitly ruled out; the page/route coexistence rule and
the absence of a `searchParams` prop in handlers both appear.

Non-target drift: metadata 0.92 → 0.88 on one weight-1 question where the model
invented `generateSitemap` / `generateRobots` export names, a sub-item the skill does
not touch and the baseline answer got right. It stays above threshold and is a
single-sample wobble, not a reason to extend this skill; if it recurs in a fresh
baseline it earns its own metadata section. Every other non-target tag moved up or held.
