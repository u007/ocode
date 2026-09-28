---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: tanstack
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — longcat-2.5-preview-free on tanstack

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump
> invalidates this scorecard — re-benchmark.

Answers graded from `../answers/longcat-2.5-preview-free.md`, produced
closed-book via `ocode run` (only `_prompts/tanstack.md` as input; session
audited: 1 user + 1 assistant turn, zero tool calls). Half-point awards mark a
rubric `point` whose primary concept was present but whose secondary clause
was missing; the note names the missing clause.

Contamination check: no answer is a verbatim or near-verbatim copy of the
reference. Phrasing is the model's own, and several answers diverge from the
key in substance: query-keys-01 says two keys match when their
`JSON.stringify` output matches, with no mention of object-key sorting
(which would make property order significant), router-search-03 says
the loader gets search params through `validateSearch` and never names
`loaderDeps`, and query-fn-03 hedges a non-existent `'paused'` disabled state.
Consistent with a genuinely blind run. Verdict: clean.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| tanstack-query-keys-01 | query-keys | 2 | 2 | 1 | 0.50 | array identifying the cache entry present; says the key is serialized via `JSON.stringify` but never mentions the plain-object key sorting — object-key-order-ignored / array-order-matters absent |
| tanstack-query-keys-02 | query-keys, invalidation | 3 | 2 | 1.5 | 0.75 | prefix invalidation correct; only the broad-subtree side — never says the hierarchy also allows precise single-item invalidation |
| tanstack-query-keys-03 | query-keys, query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-query-keys-04 | query-keys, caching | 2 | 2 | 1 | 0.50 | one request / dedup correct; per-key staleTime/gc tracking shared across observers absent |
| tanstack-caching-01 | caching | 3 | 3 | 2.5 | 0.83 | staleTime and gcTime correct, independence shown; defaults (0 / 5min) not stated |
| tanstack-caching-02 | caching | 2 | 2 | 1.5 | 0.75 | stale-while-revalidate on remount correct; never said raising staleTime suppresses the background refetch |
| tanstack-invalidation-01 | invalidation | 2 | 2 | 2 | 1.00 | |
| tanstack-invalidation-02 | invalidation, caching | 2 | 2 | 2 | 1.00 | |
| tanstack-mutations-01 | mutations | 2 | 2 | 1.5 | 0.75 | imperative-vs-auto and v5 isPending correct; onSuccess/onError/onSettled not mentioned as a tracking surface |
| tanstack-mutations-02 | mutations, invalidation | 2 | 2 | 2 | 1.00 | |
| tanstack-mutations-03 | mutations | 3 | 3 | 2.5 | 0.83 | snapshot, optimistic setQueryData, context, rollback, onSettled invalidate all present; no `cancelQueries` before snapshotting |
| tanstack-mutations-04 | mutations, query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-query-fn-01 | query-fn | 3 | 2 | 2 | 1.00 | |
| tanstack-query-fn-02 | query-fn | 2 | 2 | 1.5 | 0.75 | pure fetch / called multiple times correct; recommends a `useEffect` reacting to data (state mirroring) instead of reading `data` / deriving during render |
| tanstack-query-fn-03 | query-fn | 2 | 2 | 2 | 1.00 | `enabled: !!userId`, pending + fetchStatus idle; spurious "'paused' in some versions" hedge not penalized |
| tanstack-suspense-01 | suspense | 2 | 2 | 1.5 | 0.75 | Suspense boundary + no loading checks present; never states `data` is guaranteed defined |
| tanstack-suspense-02 | suspense, query-fn | 2 | 2 | 1 | 0.50 | errors to ErrorBoundary correct; no Suspense+ErrorBoundary pairing, no "queryFn must still throw" |
| tanstack-suspense-03 | suspense, prefetch | 2 | 2 | 2 | 1.00 | |
| tanstack-suspense-04 | suspense | 2 | 2 | 2 | 1.00 | |
| tanstack-prefetch-01 | prefetch | 2 | 2 | 2 | 1.00 | |
| tanstack-prefetch-02 | prefetch | 2 | 2 | 1.5 | 0.75 | throws vs swallows and when-to-pick correct; never said both respect staleTime |
| tanstack-router-loaders-01 | router-loaders, router-typesafety | 3 | 2 | 1.5 | 0.75 | loader-before-render and useEffect contrast correct; `useLoaderData()` never described as typed (loader return type flowing to the component) |
| tanstack-router-loaders-02 | router-loaders, prefetch | 2 | 2 | 1.5 | 0.75 | ensureQueryData with the same key correct; shared QueryClient via router context never mentioned |
| tanstack-router-loaders-03 | router-loaders, prefetch | 2 | 2 | 1.5 | 0.75 | beforeLoad + loader on hover, guards run correct; never said preload respects staleTime/loader caching |
| tanstack-router-loaders-04 | router-loaders, router-typesafety | 2 | 2 | 1.5 | 0.75 | ordering and auth/guard/redirect correct; context described as passed to "loader and component" — not a typed object threaded down to descendant routes |
| tanstack-router-search-01 | router-search | 3 | 2 | 1.5 | 0.75 | validateSearch parse/coerce/defaults/typed correct; rationale is "strings are untyped", never that the URL is untrusted user-editable input |
| tanstack-router-search-02 | router-search | 2 | 2 | 1.5 | 0.75 | write via Link/navigate `search` and useState anti-pattern correct; reading via `useSearch()` never named |
| tanstack-router-search-03 | router-search, router-loaders | 2 | 2 | 0.5 | 0.25 | wrong mechanism: claims the loader receives validated search via validateSearch; no `loaderDeps`, no path-params-only tracking — partial only |
| tanstack-router-search-04 | router-search, router-typesafety | 2 | 2 | 1 | 0.50 | Link requires schema-matching search (compile error) correct; `useSearch()` typing and schema-change-breaks-call-sites absent |
| tanstack-router-typesafety-01 | router-typesafety | 2 | 2 | 2 | 1.00 | |
| tanstack-router-typesafety-02 | router-typesafety | 2 | 2 | 1 | 0.50 | route exists + param required/typed correct; nothing on navigate/search or refactors surfacing at every call site |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| query-keys | 0.69 | 4 | ok | **derive** |
| caching | 0.78 | 4 | ok | omit (strong) |
| invalidation | 0.92 | 4 | ok | omit (strong) |
| mutations | 0.89 | 4 | ok | omit (strong) |
| query-fn | 0.88 | 6 | ok | omit (strong) |
| suspense | 0.81 | 4 | ok | omit (strong) |
| prefetch | 0.85 | 5 | ok | omit (strong) |
| router-loaders | 0.66 | 5 | ok | **derive** |
| router-search | 0.58 | 4 | ok | **derive** |
| router-typesafety | 0.70 | 5 | ok | **derive** |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Per-tag arithmetic: query-keys 6.25/9; caching 7/9; invalidation 8.25/9;
mutations 8/9; query-fn 11.5/13; suspense 6.5/8; prefetch 8.5/10;
router-loaders 7.25/11; router-search 5.25/9; router-typesafety 7.75/11.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 53.75 / 68 = 79.0%
```

## Derivation targets

Tags below threshold (`< 0.75`): **query-keys (0.69), router-loaders (0.66),
router-search (0.58), router-typesafety (0.70)** → feed into
`derived/tanstack.longcat-2.5-preview-free.SKILL.md`.

Root failures: key matching described as plain `JSON.stringify` equality
with no object-key normalization, and no per-key freshness/gc model;
`validateSearch` conflated with loader reload tracking (no `loaderDeps`);
the read side of search state (`useSearch()`) and its compile-time propagation
omitted; router context not described as a typed tree-threaded object carrying
the shared `QueryClient`; typed navigation/loader data described only for the
single `Link` asked about, never the `useLoaderData`/`navigate`/`useSearch`
surface or refactor propagation to every call site. Near-threshold tag left
out: caching (0.78).
