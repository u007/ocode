---
model_id: step-5-preview-free
model_version: "5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-12
stack: tanstack
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — step-5-preview-free on tanstack

> Valid ONLY for `step-5-preview-free` @ `5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book (Rule 0). The answerer was `ocode run -m opencode-go/step-5-preview-free -effort med`
in an empty scratch directory with only the `_prompts/tanstack.md` sheet attached. The
session sqlite shows exactly 1 user message, 1 assistant message and 0 tool calls, so
no repo file, answer key or web source was consulted. A first attempt failed at the
provider (HTTP 429) with no output and was re-run; this scorecard grades the re-run.
Answers: `../answers/step-5-preview-free.md`. Grader: a separate Claude session that
never produced any answer.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| tanstack-query-keys-01 | query-keys | 2 | 2 | 2 | 1.00 | object-key-order-sorted hashing stated; array-order sensitivity not spelled out |
| tanstack-query-keys-02 | query-keys, invalidation | 3 | 2 | 2 | 1.00 | |
| tanstack-query-keys-03 | query-keys, query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-query-keys-04 | query-keys, caching | 2 | 2 | 1 | 0.50 | one request + dedup correct; did not say staleTime/gc are tracked per key and shared across observers |
| tanstack-caching-01 | caching | 3 | 3 | 3 | 1.00 | |
| tanstack-caching-02 | caching | 2 | 2 | 2 | 1.00 | also correctly covers the gcTime-elapsed cold-fetch case |
| tanstack-invalidation-01 | invalidation | 2 | 2 | 2 | 1.00 | |
| tanstack-invalidation-02 | invalidation, caching | 2 | 2 | 2 | 1.00 | |
| tanstack-mutations-01 | mutations | 2 | 2 | 2 | 1.00 | |
| tanstack-mutations-02 | mutations, invalidation | 2 | 2 | 2 | 1.00 | |
| tanstack-mutations-03 | mutations | 3 | 3 | 3 | 1.00 | |
| tanstack-mutations-04 | mutations, query-fn | 2 | 2 | 1 | 0.50 | void-vs-Promise contrast present but claims `mutate` yields "an unhandled rejection if there is no handler" — `mutate` swallows internally (`.catch(noop)`); that is the whole reason the split exists (judgment) |
| tanstack-query-fn-01 | query-fn | 3 | 2 | 2 | 1.00 | |
| tanstack-query-fn-02 | query-fn | 2 | 2 | 2 | 1.00 | aside: recommends per-query onSuccess/onError callbacks, removed from useQuery in v5 (tangential) |
| tanstack-query-fn-03 | query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-suspense-01 | suspense | 2 | 2 | 2 | 1.00 | |
| tanstack-suspense-02 | suspense, query-fn | 2 | 2 | 1 | 0.50 | ErrorBoundary + useQueryErrorResetBoundary credited; never pairs Suspense with ErrorBoundary and never says the queryFn must still throw for the boundary to fire |
| tanstack-suspense-03 | suspense, prefetch | 2 | 2 | 2 | 1.00 | |
| tanstack-suspense-04 | suspense | 2 | 2 | 2 | 1.00 | |
| tanstack-prefetch-01 | prefetch | 2 | 2 | 2 | 1.00 | |
| tanstack-prefetch-02 | prefetch | 2 | 2 | 2 | 1.00 | |
| tanstack-router-loaders-01 | router-loaders, router-typesafety | 3 | 2 | 2 | 1.00 | |
| tanstack-router-loaders-02 | router-loaders, prefetch | 2 | 2 | 2 | 1.00 | |
| tanstack-router-loaders-03 | router-loaders, prefetch | 2 | 2 | 2 | 1.00 | aside: says `'intent'` is the router default; `defaultPreload` is `false` unless set (question premise invited this; tangential) |
| tanstack-router-loaders-04 | router-loaders, router-typesafety | 2 | 2 | 2 | 1.00 | aside: says loader receives `search` when validateSearch is defined; the loader arg carries `deps`, not `search` (tangential) |
| tanstack-router-search-01 | router-search | 3 | 2 | 2 | 1.00 | |
| tanstack-router-search-02 | router-search | 2 | 2 | 2 | 1.00 | |
| tanstack-router-search-03 | router-search, router-loaders | 2 | 2 | 0.5 | 0.25 | attributes the gap to a missing `validateSearch` schema (typing) instead of loader dependency tracking; never names `loaderDeps` or reload-on-change; "fields declared in deps" only gestures at it → partial |
| tanstack-router-search-04 | router-search, router-typesafety | 2 | 2 | 2 | 1.00 | |
| tanstack-router-typesafety-01 | router-typesafety | 2 | 2 | 2 | 1.00 | |
| tanstack-router-typesafety-02 | router-typesafety | 2 | 2 | 2 | 1.00 | search-prop check named; navigate/refactor propagation not spelled out |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| query-keys | 0.89 | 4 | ok | omit (strong) |
| caching | 0.89 | 4 | ok | omit (strong) |
| invalidation | 1.00 | 4 | ok | omit (strong) |
| mutations | 0.89 | 4 | ok | omit (strong) |
| query-fn | 0.85 (0.846) | 6 | ok | **derive** |
| suspense | 0.88 | 4 | ok | omit (strong) |
| prefetch | 1.00 | 5 | ok | omit (strong) |
| router-loaders | 0.86 | 5 | ok | omit (strong) |
| router-search | 0.83 | 4 | ok | **derive** |
| router-typesafety | 1.00 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.
query-fn is 11/13 = 0.846, below 0.85 by one judgment call (the `mutate`
unhandled-rejection claim); it is derived per the rule, but treat it as marginal
when validating.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 63.5 / 68 = 93.4%
```

## Derivation targets

Tags below threshold (`< 0.85`): **query-fn, router-search** → feed into
`derived/tanstack.step-5-preview-free.SKILL.md`.

## Answer anomalies

- No truncation: all 31 ids answered, file ends with the model's own confidence note.
- No contamination signal: wording diverges from the reference throughout, and the
  two biggest losses (router-search-03, mutations-04) contradict the reference
  rather than echo it. The model self-flagged uncertainty on `enabled: false` for
  `useSuspenseQuery` and on the preload default, both of which it got right / nearly right.
- Harness artifacts, not model output: the leading `## Assumptions stated` block and
  trailing confidence note wrap the YAML list; ids matched cleanly.
