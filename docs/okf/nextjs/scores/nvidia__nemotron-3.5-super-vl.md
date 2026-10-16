---
model_id: nvidia/nemotron-3.5-super-vl
model_version: "3.5"
evaluated_via: openrouter
evaluated_on: 2026-10-16
stack: nextjs
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — nvidia/nemotron-3.5-super-vl on nextjs

> Valid ONLY for `nvidia/nemotron-3.5-super-vl` @ `3.5`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book (Rule 0). The answerer was `ocode run -m openrouter/nvidia/nemotron-3.5-super-vl:free -effort med`
under an isolated HOME whose config denies every tool (no tools were advertised), in a
scratch cwd containing only the `_prompts/nextjs.md` sheet, one fresh session; the session
sqlite shows exactly 1 user + 1 assistant message and 0 tool calls. All 34 ids came back
once, none empty. Headless runs inject the bundled skill-catalogue digest into the system
prompt; no answer cites a tool or skill the question never named. The OpenRouter `:free`
route variant is provenance only and is not part of the model id.

Anomalies in the raw output:

- No duplicated ids, no thinking-aloud, no leaked tool-call frames, no fabricated tool
  output. Every record is a single compact paragraph.
- Fabricated or misnamed APIs, all inside otherwise-correct answers: `redirect(url,
  { permanent: true })` "for 308" (the real second arg is `type`; 308 is
  `permanentRedirect()`); `notFound.tsx` as the 404 file (it is `not-found.tsx`);
  `export const dynamic` enumerated as `'force-dynamic' | 'static' | 'error'` (`'static'`
  is not a value; `'force-static'` and `'auto'` are). These are graded as materially wrong
  claims where they touch a rubric point (see notes).
- Uses the pre-React-19 name `useFormState` (now `useActionState`); not penalised.
- No contamination signal: wording is independent of the reference answers, and the model
  makes errors the key does not contain (a "static asset cache" as a fourth caching layer,
  manual `<Suspense>` described as "not navigation-aware").

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| nextjs-app-router-conventions-01 | app-router | 3 | 3 | 3 | 1.00 | (lenient) route.ts = endpoint stated; page/route mutual exclusion not mentioned |
| nextjs-app-router-layout-02 | app-router | 2 | 2 | 2 | 1.00 | |
| nextjs-app-router-error-03 | app-router | 2 | 2 | 2 | 1.00 | |
| nextjs-app-router-loading-04 | app-router, streaming | 2 | 2 | 2 | 1.00 | |
| nextjs-server-components-default-01 | server-components, app-router | 3 | 3 | 2.5 | 0.83 | (judgment) default + module-boundary correct; third point only gives the client half (hooks/handlers/window) and never states that server components ship no JS / can be async |
| nextjs-server-components-hooks-02 | server-components | 3 | 2 | 2 | 1.00 | |
| nextjs-server-components-props-03 | server-components, data-fetching | 2 | 2 | 2 | 1.00 | |
| nextjs-data-fetching-rsc-01 | data-fetching, server-components | 3 | 2 | 2 | 1.00 | |
| nextjs-data-fetching-nogssp-02 | data-fetching, rendering | 2 | 2 | 1 | 0.50 | Promise.all / start-then-await correct; never offers isolating slow fetches behind separate Suspense boundaries (tangent on React.cache dedup instead) |
| nextjs-caching-fetch-default-01 | caching, data-fetching | 3 | 3 | 3 | 1.00 | |
| nextjs-caching-layers-02 | caching | 2 | 3 | 2 | 0.67 | Data Cache, Full Route Cache, Router Cache correct; Request Memoization missing, replaced by a "static asset cache" that is not one of the four layers |
| nextjs-caching-revalidate-03 | caching, rendering | 3 | 2 | 2 | 1.00 | |
| nextjs-caching-ondemand-04 | caching, server-actions | 2 | 2 | 2 | 1.00 | |
| nextjs-caching-segment-config-05 | caching, rendering | 3 | 2 | 1.5 | 0.75 | (judgment) segment-override idea and force-dynamic = per request correct; `force-static` never mentioned, enum given as `'force-dynamic' | 'static' | 'error'` (wrong value), and "revalidate false/0 means no revalidation" conflates `false` (cache forever) with `0` (never cache) |
| nextjs-rendering-static-dynamic-01 | rendering | 3 | 2 | 2 | 1.00 | tangential: lists `await params` as a dynamic trigger (params alone does not force dynamic) |
| nextjs-rendering-static-params-02 | rendering | 2 | 2 | 2 | 1.00 | |
| nextjs-rendering-dynamic-apis-03 | rendering, data-fetching | 2 | 2 | 2 | 1.00 | |
| nextjs-server-actions-useserver-01 | server-actions | 3 | 3 | 2.5 | 0.83 | (judgment) action concept and file-vs-inline present; the contrast with `"use client"` is reduced to "runs in the browser vs runs on the server" — mischaracterises `"use client"` (client components also prerender on the server) and misses the function/RPC boundary vs component boundary distinction |
| nextjs-server-actions-mutation-02 | server-actions, caching | 2 | 2 | 2 | 1.00 | uses the older `useFormState` name |
| nextjs-server-actions-security-03 | server-actions, route-handlers | 3 | 2 | 2 | 1.00 | |
| nextjs-route-handlers-basics-01 | route-handlers | 2 | 2 | 2 | 1.00 | (lenient) route/page mutual exclusion not mentioned |
| nextjs-route-handlers-caching-02 | route-handlers, caching | 3 | 2 | 1 | 0.50 | version split (14 cached, 15+ not) correct; the opt-in `export const dynamic = 'force-static'` (+ `revalidate`) is never named, only "control via headers/segment config" |
| nextjs-route-handlers-methods-03 | route-handlers | 1 | 2 | 2 | 1.00 | tangential: mentions awaiting `searchParams` in a handler (handlers have no searchParams prop) |
| nextjs-streaming-ssr-01 | streaming | 2 | 2 | 2 | 1.00 | |
| nextjs-streaming-suspense-02 | streaming, server-components | 2 | 2 | 2 | 1.00 | |
| nextjs-streaming-boundary-03 | streaming, app-router | 1 | 2 | 1.5 | 0.75 | (judgment) coarse segment boundary vs place-anywhere boundary present, but claims a manual `<Suspense>` "applies mainly to initial load" and "is not navigation-aware" — it shows its fallback on navigation too whenever the wrapped server component suspends |
| nextjs-metadata-static-01 | metadata | 2 | 2 | 1.5 | 0.75 | `metadata` export and auto-generated head tags correct; never states it is unavailable in `"use client"` files |
| nextjs-metadata-dynamic-02 | metadata | 2 | 2 | 2 | 1.00 | tangential: "streams the `<title>` early" |
| nextjs-metadata-inherit-03 | metadata | 1 | 2 | 2 | 1.00 | |
| nextjs-metadata-files-04 | metadata | 1 | 2 | 2 | 1.00 | |
| nextjs-navigation-link-01 | navigation | 2 | 2 | 2 | 1.00 | tangential: says prefetch happens "on hover" as well as in viewport |
| nextjs-navigation-hooks-02 | navigation, server-components | 2 | 2 | 2 | 1.00 | |
| nextjs-navigation-redirect-03 | navigation, rendering | 2 | 2 | 1.5 | 0.75 | (judgment) throw/try-catch gotcha correct; first point docked for two wrong claims about the same constructs: `redirect(url, { permanent: true })` for 308 (that is `permanentRedirect()`; the second arg is `type`) and `notFound.tsx` as the 404 file (it is `not-found.tsx`) |
| nextjs-navigation-action-redirect-04 | navigation, server-actions | 1 | 2 | 1.5 | 0.75 | after-write ordering and "nothing after it runs" present; the try/catch caveat is not stated here |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| app-router | 0.94 | 6 | ok | omit (strong) |
| server-components | 0.97 | 6 | ok | omit (strong) |
| data-fetching | 0.92 | 5 | ok | omit (strong) |
| caching | 0.84 | 7 | ok | **derive** |
| rendering | 0.87 | 7 | ok | omit (strong) |
| server-actions | 0.93 | 5 | ok | omit (strong) |
| route-handlers | 0.83 | 4 | ok | **derive** |
| streaming | 0.96 | 4 | ok | omit (strong) |
| metadata | 0.92 | 4 | ok | omit (strong) |
| navigation | 0.89 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Exact fractions: caching 15.08/18 = 0.838; route-handlers 7.5/9 = 0.833; rendering
14.75/17 = 0.868; navigation 6.25/7 = 0.893.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 67.58/74 = 91.3%
```

## Derivation targets

Tags below threshold (`< 0.85`): **caching** (0.84, n=7), **route-handlers** (0.83, n=4)
→ `derived/nextjs.nvidia__nemotron-3.5-super-vl.SKILL.md`.

Observed failures feeding the skill: (caching) omitted Request Memoization from the
caching layers and substituted a "static asset cache"; gave `export const dynamic` a
non-existent `'static'` value and never mentioned `'force-static'`; conflated
`revalidate = false` with `revalidate = 0`; never named the GET route-handler caching
opt-in. (route-handlers) the same missing `dynamic = 'force-static'` opt-in for a cached
GET handler, which is the shared question with caching and the only route-handlers miss.
Both tags sit within 0.02 of the threshold; the rendering tag (0.87) passed on the same
segment-config answer and is not derived.
