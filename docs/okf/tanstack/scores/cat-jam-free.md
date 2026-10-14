---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: tanstack
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — cat-jam-free on tanstack

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark. (opencode-go free/alpha model; no version
> published. The model self-identifies as "CatJam, developed by LitterBox AI".)

## Provenance

Closed-book (Rule 0). The answerer was `ocode run -m opencode-go/cat-jam-free -effort med`
in an empty scratch directory with only the `_prompts/tanstack.md` sheet attached. The
session sqlite shows exactly 1 user message, 1 assistant message and 0 tool calls, so
no repo file, answer key or web source was consulted. Headless runs inject ocode's
bundled skill-catalogue digest into the system prompt.

This scorecard grades a **re-run**. The first attempt called bash/read on its own
question sheet (no answer-key access) and was discarded; the re-run used an isolated
HOME with every tool denied and a cwd holding only the sheet. The re-run emitted the
YAML answer list twice, byte-identical, separated by a leaked thinking-aloud line
("Need perhaps final only. Good."); one copy was kept (see the answers-file header).
Answers: `../answers/cat-jam-free.md`. Grader: a separate Claude session that never
produced any answer.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| tanstack-query-keys-01 | query-keys | 2 | 2 | 1 | 0.50 | "cache identity, typically arrays" credited; no deterministic hashing, and "structurally equal in the same order" implies object key order matters — it does not (object keys are sorted when hashed) |
| tanstack-query-keys-02 | query-keys, invalidation | 3 | 2 | 2 | 1.00 | prefix invalidation + broad-first/specific-last ordering; precise single-item invalidation only implied |
| tanstack-query-keys-03 | query-keys, query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-query-keys-04 | query-keys, caching | 2 | 2 | 1 | 0.50 | one request + dedup of the in-flight promise correct; never says freshness/gc are tracked per key and shared by observers |
| tanstack-caching-01 | caching | 3 | 3 | 2 | 0.67 | staleTime/gcTime each defined correctly; no independent-axes framing and no defaults (0 / 5 min) |
| tanstack-caching-02 | caching | 2 | 2 | 2 | 1.00 | stale-while-revalidate correct; did not add that raising staleTime suppresses the refetch |
| tanstack-invalidation-01 | invalidation | 2 | 2 | 2 | 1.00 | inactive-refetch-on-next-use not spelled out |
| tanstack-invalidation-02 | invalidation, caching | 2 | 2 | 2 | 1.00 | |
| tanstack-mutations-01 | mutations | 2 | 2 | 2 | 1.00 | isPending named; lifecycle callbacks not listed |
| tanstack-mutations-02 | mutations, invalidation | 2 | 2 | 2 | 1.00 | |
| tanstack-mutations-03 | mutations | 3 | 3 | 3 | 1.00 | cancel, snapshot, optimistic write, rollback, onSettled invalidate; returning the snapshot as context only implied |
| tanstack-mutations-04 | mutations, query-fn | 2 | 2 | 1 | 0.50 | void-vs-Promise contrast present; "how does it know it failed" answered with the observable symptoms (isError, rejection, onError) — never states the cause: mutationFn must throw/reject |
| tanstack-query-fn-01 | query-fn | 3 | 2 | 2 | 1.00 | |
| tanstack-query-fn-02 | query-fn | 2 | 2 | 2 | 1.00 | aside: "move side effects to effects" is weaker than "read from data / derive" (tangential) |
| tanstack-query-fn-03 | query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-suspense-01 | suspense | 2 | 2 | 2 | 1.00 | |
| tanstack-suspense-02 | suspense, query-fn | 2 | 2 | 1 | 0.50 | ErrorBoundary destination correct; no Suspense + ErrorBoundary pairing and no "queryFn must still throw" precondition |
| tanstack-suspense-03 | suspense, prefetch | 2 | 2 | 2 | 1.00 | also names useSuspenseQueries |
| tanstack-suspense-04 | suspense | 2 | 2 | 2 | 1.00 | |
| tanstack-prefetch-01 | prefetch | 2 | 2 | 2 | 1.00 | |
| tanstack-prefetch-02 | prefetch | 2 | 2 | 2 | 1.00 | "both respect staleTime" not stated |
| tanstack-router-loaders-01 | router-loaders, router-typesafety | 3 | 2 | 2 | 1.00 | |
| tanstack-router-loaders-02 | router-loaders, prefetch | 2 | 2 | 2 | 1.00 | ensureQueryData + same key/queryFn, one cache; QueryClient-on-router-context not named here (named in loaders-04) |
| tanstack-router-loaders-03 | router-loaders, prefetch | 2 | 2 | 2 | 1.00 | thin; caching/staleTime gating of preloads not mentioned |
| tanstack-router-loaders-04 | router-loaders, router-typesafety | 2 | 2 | 2 | 1.00 | |
| tanstack-router-search-01 | router-search | 3 | 2 | 2 | 1.00 | transform + typed + safe defaults; "URL is untrusted input" not named explicitly |
| tanstack-router-search-02 | router-search | 2 | 2 | 2 | 1.00 | |
| tanstack-router-search-03 | router-search, router-loaders | 2 | 2 | 2 | 1.00 | |
| tanstack-router-search-04 | router-search, router-typesafety | 2 | 2 | 2 | 1.00 | refactor-breaks-call-sites not stated |
| tanstack-router-typesafety-01 | router-typesafety | 2 | 2 | 1 | 0.50 | names routeTree.gen.ts generated from files; never says what the typed tree checks (Link/navigate/params compile errors against real routes); no createFileRoute |
| tanstack-router-typesafety-02 | router-typesafety | 2 | 2 | 1 | 0.50 | path-exists + required-param check correct; nothing on navigate/search or refactors surfacing at every call site |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| query-keys | 0.78 | 4 | ok | **derive** |
| caching | 0.78 | 4 | ok | **derive** |
| invalidation | 1.00 | 4 | ok | omit (strong) |
| mutations | 0.89 | 4 | ok | omit (strong) |
| query-fn | 0.85 (0.846) | 6 | ok | **derive** |
| suspense | 0.88 | 4 | ok | omit (strong) |
| prefetch | 1.00 | 5 | ok | omit (strong) |
| router-loaders | 1.00 | 5 | ok | omit (strong) |
| router-search | 1.00 | 4 | ok | omit (strong) |
| router-typesafety | 0.82 | 5 | ok | **derive** |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.
query-keys 7/9, caching 7/9, query-fn 11/13, router-typesafety 9/11. query-fn is
below 0.85 by one judgment call (mutations-04's missing throw-to-fail cause); it is
derived per the rule, but treat it as marginal when validating.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 61 / 68 = 89.7%
```

## Derivation targets

Tags below threshold (`< 0.85`): **query-keys, caching, query-fn, router-typesafety** →
feed into `derived/tanstack.cat-jam-free.SKILL.md`.

## Answer anomalies

- No truncation: all 31 ids answered, one or two sentences each. The losses are
  mostly omissions from terseness (missing second halves of points), not wrong
  claims; the one materially wrong statement is the query-key equality rule
  (query-keys-01).
- No contamination signal: wording diverges from the reference throughout, and no
  answer reproduces reference-specific details (no defaults, no `createFileRoute`,
  no `queryOptions`).
- Harness artifacts, not model output: duplicated YAML list and leaked
  thinking line in the re-run (one copy kept); first attempt discarded for tool use.
