---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: tanstack
stack_corpus_rev: 1
threshold: 0.85
validation: with-skill
---

# Scorecard — cat-jam-free on tanstack (with-skill validation)

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark. (opencode-go free/alpha model; no version published.)

> **WITH-SKILL VALIDATION RUN.** Answers: `tanstack/answers/cat-jam-free.with-skill.md`,
> produced closed-book with the current derived skill
> `derived/tanstack.cat-jam-free.SKILL.md` (`tanstack-tuning-cat-jam-free`)
> prepended to the answerer prompt as active guidance (session audited: 1 user
> message, 1 assistant message, 0 tool calls). Baseline: `scores/cat-jam-free.md`.
> No new derived skill is produced from this run.

> Target tags for this skill (baseline < 0.85): **query-keys (0.78), caching (0.78),
> query-fn (0.846), router-typesafety (0.82)**.

Graded against `tanstack/questions.yaml` corpus_rev 1 (Query v5 + Router v1 scope).
`meta.yaml` sets no `threshold:`, so the default 0.85 applies. Grading rule (same
strictness as the baseline): a `point` whose concept is genuinely present earns full
credit (minor missing sub-items are noted); a `point` accompanied by a materially wrong
claim about the same construct earns 0. Points are whole; fractions come only from a
listed partial. Compound points ("X; Y") are awarded when their distinguishing concept
is present with one sub-half missing, applied uniformly (susp-02, ts-02), matching the
step-5-preview-free graders' precedent. Pivotal calls are marked "(judgment)".

## History

- Earlier with-skill scorecard: none (this is the first with-skill validation for
  cat-jam-free on tanstack).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| tanstack-query-keys-01 | query-keys | 2 | 2 | 2 | 1.00 | deterministic hash; array order matters, object key order does not, with the `{a,b}`/`{b,a}` example. Baseline's wrong "same order" claim is gone (baseline 0.50) |
| tanstack-query-keys-02 | query-keys, invalidation | 3 | 2 | 2 | 1.00 | prefix invalidation of descendants. (judgment) point 2: generic→specific shown only through the examples; ordering and precise single-item invalidation never stated, slightly weaker than baseline, which earned it on "broad-first/specific-last". Awarded for consistency with baseline. Withheld → query-keys 7.5/9 = 0.833 (FAIL); not a baseline loss and not addressed by the skill, so a downgrade would be resampling noise |
| tanstack-query-keys-03 | query-keys, query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-query-keys-04 | query-keys, caching | 2 | 2 | 2 | 1.00 | one request + in-flight dedup; cache, staleTime clock and gcTime timer shared across observers of the key (baseline 0.50) |
| tanstack-caching-01 | caching | 3 | 3 | 3 | 1.00 | independent axes (refetching vs retention) + defaults `staleTime: 0` / `gcTime: 5 minutes` (baseline 0.67) |
| tanstack-caching-02 | caching | 2 | 2 | 2 | 1.00 | stale-while-revalidate + staleTime 0 = always stale; "raising staleTime suppresses the refetch" not restated (minor, same as baseline) |
| tanstack-invalidation-01 | invalidation | 2 | 2 | 2 | 1.00 | inactive-refetch-on-next-use and data-not-removed now explicit |
| tanstack-invalidation-02 | invalidation, caching | 2 | 2 | 2 | 1.00 | |
| tanstack-mutations-01 | mutations | 2 | 2 | 2 | 1.00 | isPending (not isLoading); lifecycle callbacks not listed (minor, same as baseline) |
| tanstack-mutations-02 | mutations, invalidation | 2 | 2 | 2 | 1.00 | |
| tanstack-mutations-03 | mutations | 3 | 3 | 3 | 1.00 | snapshot returned as context now explicit |
| tanstack-mutations-04 | mutations, query-fn | 2 | 2 | 2 | 1.00 | throw-to-fail contract stated as the cause ("only when mutationFn throws or returns a rejected Promise; error object / bad-status response is success") (baseline 0.50) |
| tanstack-query-fn-01 | query-fn | 3 | 2 | 2 | 1.00 | |
| tanstack-query-fn-02 | query-fn | 2 | 2 | 2 | 1.00 | now "derive from returned status/data" instead of baseline's "move to effects" |
| tanstack-query-fn-03 | query-fn | 2 | 2 | 2 | 1.00 | |
| tanstack-suspense-01 | suspense | 2 | 2 | 2 | 1.00 | |
| tanstack-suspense-02 | suspense, query-fn | 2 | 2 | 2 | 1.00 | ErrorBoundary destination + "queryFn must throw or the boundary never fires". Suspense-for-loading pairing not restated here (it is in suspense-01); compound point awarded per the uniform rule. Withheld → query-fn 12/13 = 0.923, still PASS (baseline 0.50) |
| tanstack-suspense-03 | suspense, prefetch | 2 | 2 | 2 | 1.00 | |
| tanstack-suspense-04 | suspense | 2 | 2 | 2 | 1.00 | |
| tanstack-prefetch-01 | prefetch | 2 | 2 | 2 | 1.00 | |
| tanstack-prefetch-02 | prefetch | 2 | 2 | 2 | 1.00 | "both respect staleTime" not stated (minor, same as baseline) |
| tanstack-router-loaders-01 | router-loaders, router-typesafety | 3 | 2 | 2 | 1.00 | |
| tanstack-router-loaders-02 | router-loaders, prefetch | 2 | 2 | 2 | 1.00 | QueryClient on router context now named here |
| tanstack-router-loaders-03 | router-loaders, prefetch | 2 | 2 | 2 | 1.00 | caching/staleTime gating not mentioned (minor, same as baseline) |
| tanstack-router-loaders-04 | router-loaders, router-typesafety | 2 | 2 | 2 | 1.00 | |
| tanstack-router-search-01 | router-search | 3 | 2 | 2 | 1.00 | "URL is untrusted input" not explicit (minor, same as baseline) |
| tanstack-router-search-02 | router-search | 2 | 2 | 2 | 1.00 | reading via `useSearch` not named; URL-as-source-of-truth + navigate/Link update present |
| tanstack-router-search-03 | router-search, router-loaders | 2 | 2 | 2 | 1.00 | |
| tanstack-router-search-04 | router-search, router-typesafety | 2 | 2 | 2 | 1.00 | Link/navigate/useSearch compile-checked; schema-change breaks call sites not stated (minor, same as baseline) |
| tanstack-router-typesafety-01 | router-typesafety | 2 | 2 | 1 | 0.50 | point 1 earned (`createFileRoute` → `routeTree.gen.ts` with paths/params/search, near-verbatim from the skill). Point 2 withheld: "the router is typed from that generated tree" never says Link/navigate/params are compile-checked against real routes. Same gap as baseline; the skill's "spell out the payoff" bullet was not absorbed here |
| tanstack-router-typesafety-02 | router-typesafety | 2 | 2 | 2 | 1.00 | path exists + required `postId` typed. (judgment) point 2: `search` checked against the validated schema is now present; `navigate` and refactor propagation still absent. Baseline withheld for "nothing on navigate/search or refactors", and step-5-preview-free's graders awarded on search alone. Withheld → router-typesafety 9/11 = 0.818 (FAIL) (baseline 0.50) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | delta | n | trust | target? |
|-----|---------:|-----------:|------:|--:|-------|---------|
| query-keys | 0.78 | 1.00 | +0.22 | 4 | ok | **yes** |
| caching | 0.78 | 1.00 | +0.22 | 4 | ok | **yes** |
| invalidation | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| mutations | 0.89 | 1.00 | +0.11 | 4 | ok | no |
| query-fn | 0.85 (0.846) | 1.00 | +0.15 | 6 | ok | **yes** |
| suspense | 0.88 | 1.00 | +0.12 | 4 | ok | no |
| prefetch | 1.00 | 1.00 | 0.00 | 5 | ok | no |
| router-loaders | 1.00 | 1.00 | 0.00 | 5 | ok | no |
| router-search | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| router-typesafety | 0.82 | 0.91 (0.909) | +0.09 | 5 | ok | **yes** |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: query-keys 9/9; caching 9/9; invalidation 9/9; mutations 9/9;
query-fn 13/13; suspense 8/8; prefetch 10/10; router-loaders 11/11;
router-search 9/9; router-typesafety 10/11.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 67 / 68 = 98.5%
```

(Only loss: router-typesafety-01 −1.0.) Baseline 61 / 68 = 89.7%.

## Target-tag verdict

| tag | baseline | with-skill | pass (≥ 0.85)? |
|-----|---------:|-----------:|----------------|
| query-keys | 0.78 | 1.00 | **PASS** |
| caching | 0.78 | 1.00 | **PASS** |
| query-fn | 0.846 | 1.00 | **PASS** |
| router-typesafety | 0.82 | 0.909 | **PASS** |

**PASS.** Every target tag reaches 0.85. The skill is validated for cat-jam-free @ alpha.

Sensitivity (read before relying on this pass): the verdict rests on two whole-point
judgment calls. Withholding ts-02 point 2 drops router-typesafety to 0.818, and
withholding qk-02 point 2 drops query-keys to 0.833. Either one flips the verdict to FAIL.
Both awards follow the baseline grader's and the step-5 graders' precedent, as noted per row.

Residual inside a target tag (not a failure; the iterate signal for router-typesafety):
on router-typesafety-01 the model reproduced the skill's first bullet (createFileRoute →
routeTree.gen.ts) nearly verbatim but dropped its "spell out the payoff" bullet
(Link/navigate/params compile-checked, type error not runtime 404). That question stays
at 1/2, exactly the baseline gap. If a later run fails, sharpen that directive, e.g.
"whenever routeTree.gen.ts is mentioned, also name what it type-checks".

Non-target drift: no non-target tag moved down. mutations (0.89 → 1.00) and suspense
(0.88 → 1.00) moved up because their shared questions (mutations-04, suspense-02) carry
the query-fn target. Per HOW-TO-EVALUATE.md, non-target movement is not acted on.

## Answer anomalies

- No truncation: all 31 ids answered once, no duplicated list and no leaked thinking
  line (unlike the baseline re-run).
- Heavy near-verbatim overlap with the skill (query-keys-01 example, query-keys-04,
  caching-01, mutations-04, suspense-02, typesafety-01). That is expected absorption of
  the injected guidance, not answer-key contamination: the overlap traces to SKILL.md
  wording, and answers on non-target tags keep the baseline's own phrasing.

## Provenance

Closed-book (Rule 0). Answers `../answers/cat-jam-free.with-skill.md` were produced by
`ocode run -m opencode-go/cat-jam-free -effort med` with the derived skill body (front
matter stripped) prepended to the `_prompts/tanstack.md` sheet as active guidance,
using an isolated HOME with every tool denied and a cwd holding only the sheet. The session
audit shows 1 user message, 1 assistant message and 0 tool calls, so no repo file,
answer key or web source was consulted. Grader: a separate Claude session that produced
no answer and did not modify the derived skill.
