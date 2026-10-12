---
model_id: step-5-preview-free
model_version: "5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-12
stack: nextjs
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — step-5-preview-free on nextjs

> Valid ONLY for `step-5-preview-free` @ `5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book (Rule 0). Answerer: `ocode run -m opencode-go/step-5-preview-free -effort med`
in an empty scratch dir with only `_prompts/nextjs.md` attached; the session sqlite
shows exactly 1 user + 1 assistant message and 0 tool calls. A first attempt produced
only a one-line preamble (the model ran a pointless `echo` via bash and stopped) and was
re-run; the graded run is the re-run.

Anomalies in the raw output, all recorded in the answers-file header:

- The model emitted a second, differently-worded answer for 14 questions after the
  list (route-handlers ×3, streaming ×3, metadata ×4, navigation ×4); second copies
  dropped, first answer graded. These were duplicate ids, not invented ones, so
  `answers/step-5-preview-free.md` holds exactly 34 records and its header lists the 14. The dropped copies were lower quality: visible thinking-aloud
  ("Let me keep it accurate:", "? no — it's the opposite", "Actually..."), and in two
  places they contradicted the kept answer (claimed `generateMetadata` fetches are
  never deduped with the page; gave the opposite default for `redirect()`'s `type`).
- The raw reply ended with a leaked tool-call envelope (`</parameter></function></tool_call>`),
  i.e. the model wrapped its YAML in a tool-call frame the harness did not parse. Removed.
- One stray token survives in the kept text (`...doesn't skip server rendering.close`),
  cosmetic only.
- No contamination signal: answers are far longer than and worded independently of the
  reference answers, and the model makes errors the key does not contain.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| nextjs-app-router-conventions-01 | app-router | 3 | 3 | 3 | 1.00 | |
| nextjs-app-router-layout-02 | app-router | 2 | 2 | 2 | 1.00 | |
| nextjs-app-router-error-03 | app-router | 2 | 2 | 2 | 1.00 | |
| nextjs-app-router-loading-04 | app-router, streaming | 2 | 2 | 2 | 1.00 | |
| nextjs-server-components-default-01 | server-components, app-router | 3 | 3 | 3 | 1.00 | stray "close" token at end of a sentence (cosmetic) |
| nextjs-server-components-hooks-02 | server-components | 3 | 2 | 2 | 1.00 | |
| nextjs-server-components-props-03 | server-components, data-fetching | 2 | 2 | 1 | 0.50 | (judgment) serializability stated, but claims a function passed server→client "is turned into a reference to a Server Action" — plain functions are a runtime error; only `"use server"` functions cross. Children-as-slot correct |
| nextjs-data-fetching-rsc-01 | data-fetching, server-components | 3 | 2 | 2 | 1.00 | |
| nextjs-data-fetching-nogssp-02 | data-fetching, rendering | 2 | 2 | 1 | 0.50 | Promise.all / start-then-await correct; never mentions isolating independent slow fetches behind separate Suspense boundaries to stream in parallel (only "pass the promise, `use()` in a client child") |
| nextjs-caching-fetch-default-01 | caching, data-fetching | 3 | 3 | 3 | 1.00 | |
| nextjs-caching-layers-02 | caching | 2 | 3 | 3 | 1.00 | |
| nextjs-caching-revalidate-03 | caching, rendering | 3 | 2 | 2 | 1.00 | |
| nextjs-caching-ondemand-04 | caching, server-actions | 2 | 2 | 2 | 1.00 | |
| nextjs-caching-segment-config-05 | caching, rendering | 3 | 2 | 2 | 1.00 | |
| nextjs-rendering-static-dynamic-01 | rendering | 3 | 2 | 2 | 1.00 | tangential: lists "an uncached slow fetch" as a dynamic trigger |
| nextjs-rendering-static-params-02 | rendering | 2 | 2 | 2 | 1.00 | tangential: "without it the whole route renders per request" (it renders on demand then caches) |
| nextjs-rendering-dynamic-apis-03 | rendering, data-fetching | 2 | 2 | 2 | 1.00 | correctly notes `params` alone does not force dynamic |
| nextjs-server-actions-useserver-01 | server-actions | 3 | 3 | 3 | 1.00 | |
| nextjs-server-actions-mutation-02 | server-actions, caching | 2 | 2 | 2 | 1.00 | |
| nextjs-server-actions-security-03 | server-actions, route-handlers | 3 | 2 | 2 | 1.00 | |
| nextjs-route-handlers-basics-01 | route-handlers | 2 | 2 | 2 | 1.00 | tangential: says route.ts cannot coexist with layout/loading either (only page is the conflict) |
| nextjs-route-handlers-caching-02 | route-handlers, caching | 3 | 2 | 2 | 1.00 | |
| nextjs-route-handlers-methods-03 | route-handlers | 1 | 2 | 2 | 1.00 | |
| nextjs-streaming-ssr-01 | streaming | 2 | 2 | 2 | 1.00 | |
| nextjs-streaming-suspense-02 | streaming, server-components | 2 | 2 | 2 | 1.00 | |
| nextjs-streaming-boundary-03 | streaming, app-router | 1 | 2 | 2 | 1.00 | |
| nextjs-metadata-static-01 | metadata | 2 | 2 | 2 | 1.00 | |
| nextjs-metadata-dynamic-02 | metadata | 2 | 2 | 1 | 0.50 | `generateMetadata` correct; dedup with the page's fetch presented as a manual opt-in ("reuse the data with React cache()/memoized fetch"), never states fetch is automatically memoized across generateMetadata and page; also says it "runs for every request" |
| nextjs-metadata-inherit-03 | metadata | 1 | 2 | 1 | 0.50 | (judgment) root→leaf merge stated, but claims nested objects like `openGraph`/`twitter` deep-merge key-by-key and `icons` accumulate — Next replaces nested objects wholesale. `title.template` correct |
| nextjs-metadata-files-04 | metadata | 1 | 2 | 2 | 1.00 | tangential: invents `icon-dynamic.tsx` |
| nextjs-navigation-link-01 | navigation | 2 | 2 | 2 | 1.00 | |
| nextjs-navigation-hooks-02 | navigation, server-components | 2 | 2 | 2 | 1.00 | tangential: says `useServerInsertedHTML` was "removed in App Router" (it is an App Router hook) |
| nextjs-navigation-redirect-03 | navigation, rendering | 2 | 2 | 2 | 1.00 | tangential: `redirect()` `type` default given as "push" (default is replace; push only in Server Actions) |
| nextjs-navigation-action-redirect-04 | navigation, server-actions | 1 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| app-router | 1.00 | 6 | ok | omit (strong) |
| server-components | 0.93 | 6 | ok | omit (strong) |
| data-fetching | 0.83 | 5 | ok | **derive** |
| caching | 1.00 | 7 | ok | omit (strong) |
| rendering | 0.94 | 7 | ok | omit (strong) |
| server-actions | 1.00 | 5 | ok | omit (strong) |
| route-handlers | 1.00 | 4 | ok | omit (strong) |
| streaming | 1.00 | 4 | ok | omit (strong) |
| metadata | 0.75 | 4 | ok | **derive** |
| navigation | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 70.5/74 = 95.3%
```

## Derivation targets

Tags below threshold (`< 0.85`): **data-fetching** (0.83, n=5), **metadata** (0.75, n=4)
→ `derived/nextjs.step-5-preview-free.SKILL.md`.

Observed failures feeding the skill: (data-fetching) misstated the server→client
function-prop rule as auto-conversion into a Server Action; gave only `Promise.all` for
waterfalls with no Suspense-isolation alternative. (metadata) did not state that
`generateMetadata` fetches are deduped with the page's via Request Memoization;
claimed nested metadata objects deep-merge across segments.
