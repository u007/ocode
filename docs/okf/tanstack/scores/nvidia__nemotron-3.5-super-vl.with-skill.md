---
model_id: nvidia/nemotron-3.5-super-vl
model_version: "3.5"
evaluated_via: openrouter
evaluated_on: 2026-10-16
stack: tanstack
stack_corpus_rev: 1
threshold: 0.85
validation: with-skill
---

# Scorecard — nvidia/nemotron-3.5-super-vl on tanstack (with-skill validation)

> Valid ONLY for `nvidia/nemotron-3.5-super-vl` @ `3.5`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers: `tanstack/answers/nvidia__nemotron-3.5-super-vl.with-skill.md`,
> produced closed-book with the current derived skill
> `derived/tanstack.nvidia__nemotron-3.5-super-vl.SKILL.md`
> (`tanstack-tuning-nvidia-nemotron-3.5-super-vl`) body prepended to the answerer
> prompt as active guidance (session audited: 1 user message, 1 assistant message,
> 0 tool calls). Baseline: `scores/nvidia__nemotron-3.5-super-vl.md`. No new derived
> skill is produced from this run and the derived skill was not modified.

> Target tags for this skill (baseline < 0.85): **query-keys (0.78), suspense (0.81),
> prefetch (0.70), router-loaders (0.64), router-search (0.72)**.

Graded against `tanstack/questions.yaml` corpus_rev 1 (Query v5 + Router v1 scope).
Grading rule applied (same strictness as the baseline): a `point` whose concept is
genuinely present earns full credit; a `point` whose second half is absent earns a
half point, noted; a `point` accompanied by a materially wrong claim about the same
construct earns 0, marked "(judgment)".

## History

- Earlier with-skill scorecard: none (this is the first with-skill validation for
  nvidia/nemotron-3.5-super-vl on tanstack).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| tanstack-query-keys-01 | query-keys | 2 | 2 | 2 | 1.00 | deterministic hash, array order matters, object keys sorted, with the `{a,b}`/`{b,a}` example — baseline's "same order" deep-equality claim is gone (baseline 0.50) |
| tanstack-query-keys-02 | query-keys, invalidation | 3 | 2 | 2 | 1.00 | also notes `exact: true` disables prefix matching |
| tanstack-query-keys-03 | query-keys, query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-query-keys-04 | query-keys, caching | 2 | 2 | 2 | 1.00 | one request + dedup, and cache/staleTime/gcTime tracked per key across observers — baseline gap closed (baseline 0.50) |
| tanstack-caching-01 | caching | 3 | 3 | 3 | 1.00 | |
| tanstack-caching-02 | caching | 2 | 2 | 2 | 1.00 | stale-while-revalidate + always-stale on remount; "raising staleTime suppresses the refetch" not spelled out (minor, same as baseline) |
| tanstack-invalidation-01 | invalidation | 2 | 2 | 2 | 1.00 | also contrasts with `removeQueries` |
| tanstack-invalidation-02 | invalidation, caching | 2 | 2 | 2 | 1.00 | |
| tanstack-mutations-01 | mutations | 2 | 2 | 2 | 1.00 | |
| tanstack-mutations-02 | mutations, invalidation | 2 | 2 | 2 | 1.00 | |
| tanstack-mutations-03 | mutations | 3 | 3 | 3 | 1.00 | |
| tanstack-mutations-04 | mutations, query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-query-fn-01 | query-fn | 3 | 2 | 2 | 1.00 | |
| tanstack-query-fn-02 | query-fn | 2 | 2 | 2 | 1.00 | aside: lists "StrictMode double-invoke" among queryFn re-runs; the fetch is deduped, StrictMode does not double-run a queryFn (tangential) |
| tanstack-query-fn-03 | query-fn | 2 | 2 | 1 | 0.50 | `enabled: !!userId` + key inclusion + no fetch while gated credited; point 2 withheld: claims `isPending` is `false` while disabled — a disabled query with no data is `status: 'pending'` (`isPending: true`, `fetchStatus: 'idle'`), which is the very state the point asks for (judgment). Baseline had no wrong claim here (baseline 1.00). Non-target tag |
| tanstack-suspense-01 | suspense | 2 | 2 | 2 | 1.00 | states the type-level `data`-is-defined guarantee explicitly; Suspense + ErrorBoundary pairing — baseline half point recovered (baseline 0.75) |
| tanstack-suspense-02 | suspense, query-fn | 2 | 2 | 2 | 1.00 | pairs Suspense (loading) + ErrorBoundary (failure) and states the queryFn-must-still-throw precondition; names `errorComponent`, not `errorElement` — baseline gap closed (baseline 0.50) |
| tanstack-suspense-03 | suspense, prefetch | 2 | 2 | 2 | 1.00 | |
| tanstack-suspense-04 | suspense | 2 | 2 | 2 | 1.00 | |
| tanstack-prefetch-01 | prefetch | 2 | 2 | 2 | 1.00 | now names the `void` return and that ensureQueryData reads cache first, fetches only if stale |
| tanstack-prefetch-02 | prefetch | 2 | 2 | 1.5 | 0.75 | throws-vs-swallows and when-to-pick correct; the baseline's wrong "fetchQuery always performs a network request" is gone, but "both respect staleTime" is never stated (half point, same standard as baseline suspense-01). Baseline 0.50 |
| tanstack-router-loaders-01 | router-loaders, router-typesafety | 3 | 2 | 2 | 1.00 | aside: offers `route.data` alongside `useLoaderData()` as the read; the typed read is `Route.useLoaderData()` (tangential) |
| tanstack-router-loaders-02 | router-loaders, prefetch | 2 | 2 | 2 | 1.00 | QueryClient on router context + ensureQueryData in loader + same queryOptions in useSuspenseQuery — baseline gap closed (baseline 0.75) |
| tanstack-router-loaders-03 | router-loaders, prefetch | 2 | 2 | 2 | 1.00 | beforeLoad + loader run on intent, data warmed, guards run — baseline's "loader does not run during preload" is gone (baseline 0.25). Caching/staleTime gating not restated here (minor) |
| tanstack-router-loaders-04 | router-loaders, router-typesafety | 2 | 2 | 2 | 1.00 | ordering, guards/redirects, typed context supplied at createRouter and beforeLoad's return merged for descendants — baseline gap closed (baseline 0.75) |
| tanstack-router-search-01 | router-search | 3 | 2 | 2 | 1.00 | reads via `Route.useSearch()` / `useSearch({ from })`; the baseline's `useSearchParams` is gone. "URL is untrusted input" expressed as "invalid values coerced/rejected instead of leaking into components" |
| tanstack-router-search-02 | router-search | 2 | 2 | 2 | 1.00 | read via `useSearch()`, write via typed `navigate({ search: prev => … })` / `<Link search>`, no useState mirroring — `setSearchParams` is gone (baseline 0.75) |
| tanstack-router-search-03 | router-search, router-loaders | 2 | 2 | 2 | 1.00 | path-params-only default, `validateSearch` never feeds the loader, `loaderDeps` → `deps` + cache identity + reload — baseline's wrong mechanism is gone (baseline 0.25) |
| tanstack-router-search-04 | router-search, router-typesafety | 2 | 2 | 2 | 1.00 | Link/navigate schema-matching search, typed `useSearch()`, compile error at every read/write site on rename; explicitly retracts the invented "loader search arg" (baseline 0.75) |
| tanstack-router-typesafety-01 | router-typesafety | 2 | 2 | 2 | 1.00 | names `useSearch`, not `useSearchParams`, this time |
| tanstack-router-typesafety-02 | router-typesafety | 2 | 2 | 2 | 1.00 | search-prop check named; navigate/refactor propagation not spelled out (same as baseline) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | delta | n | trust | target? |
|-----|---------:|-----------:|------:|--:|-------|---------|
| query-keys | 0.78 | 1.00 | +0.22 | 4 | ok | **yes** |
| caching | 0.89 | 1.00 | +0.11 | 4 | ok | no |
| invalidation | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| mutations | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| query-fn | 0.92 | 0.92 | 0.00 | 6 | ok | no |
| suspense | 0.81 | 1.00 | +0.19 | 4 | ok | **yes** |
| prefetch | 0.70 | 0.95 | +0.25 | 5 | ok | **yes** |
| router-loaders | 0.64 | 1.00 | +0.36 | 5 | ok | **yes** |
| router-search | 0.72 | 1.00 | +0.28 | 4 | ok | **yes** |
| router-typesafety | 0.91 | 1.00 | +0.09 | 5 | ok | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: query-keys 9/9; caching 9/9; invalidation 9/9; mutations 9/9;
query-fn 12/13; suspense 8/8; prefetch 9.5/10; router-loaders 11/11;
router-search 9/9; router-typesafety 11/11.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 66.5 / 68 = 97.8%
```

(Losses: query-fn-03 −1.0, prefetch-02 −0.5.) Baseline 58.5 / 68 = 86.0%.

## Target-tag verdict

| tag | baseline | with-skill | pass (≥ 0.85)? |
|-----|---------:|-----------:|----------------|
| query-keys | 0.78 | 1.00 | **PASS** |
| suspense | 0.81 | 1.00 | **PASS** |
| prefetch | 0.70 | 0.95 | **PASS** |
| router-loaders | 0.64 | 1.00 | **PASS** |
| router-search | 0.72 | 1.00 | **PASS** |

Every target tag reaches 0.85. The skill is validated for
nvidia/nemotron-3.5-super-vl @ 3.5. Every baseline failure the skill addressed is
gone: the React Router API names (`useSearchParams`/`setSearchParams`/`errorElement`),
the invented loader search arg, "loader does not run during preload", "fetchQuery
always hits the network", and the `validateSearch`-feeds-the-loader mechanism.

Residual inside a target tag (not a failure, recorded for the next iteration):
prefetch-02 still omits that `fetchQuery` and `prefetchQuery` both respect
`staleTime`, although the skill's prefetch section states it in its first bullet
and prefetch-01 in the same run does say ensureQueryData "fetches only if stale".
The model dropped the wrong claim but did not restate the parity.

Non-target drift: query-fn held at 0.92 but swapped one loss for another —
mutations-04 and suspense-02 gained, while query-fn-03 newly claims `isPending` is
false for a disabled query. That question is not a skill target and the skill says
nothing about `enabled`; per HOW-TO-EVALUATE.md, single-sample non-target movement
is not acted on. caching and router-typesafety rose only through shared questions
(query-keys-04, router-loaders-04, router-search-04) that are the skill's own targets.

## Provenance

Closed-book (Rule 0). Answers `../answers/nvidia__nemotron-3.5-super-vl.with-skill.md`
were produced by `openrouter/nvidia/nemotron-3.5-super-vl:free` (the `:free` route
variant is provenance only, not part of the model id) with the derived skill body
(front matter stripped) prepended to the `_prompts/tanstack.md` sheet as active
guidance, under an isolated HOME whose config denies every tool; the session audit
shows 1 user message, 1 assistant message and 0 tool calls, so no repo file, answer
key or web source was consulted. All 31 ids answered once, none empty; no answer
cites a tool or skill the question never named. Grader: a separate Claude session
that produced no answer and did not modify the derived skill.
