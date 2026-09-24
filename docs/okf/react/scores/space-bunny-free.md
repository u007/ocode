---
model_id: space-bunny-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-09-24
stack: react
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — space-bunny-free on react

> Valid ONLY for `space-bunny-free` @ `alpha`. A version bump invalidates this
> scorecard — re-benchmark.

Closed-book answers graded from `../answers/space-bunny-free.md`. Answers were
in the model's own wording, consistently longer than the reference, and carry a
few small factual errors of their own — no sign of answer-key contamination.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| react-recon-keys-01 | reconciliation | 2 | 2 | 2 | 1.00 | |
| react-recon-diff-02 | reconciliation | 2 | 3 | 3 | 1.00 | |
| react-recon-remount-03 | reconciliation, state | 2 | 2 | 2 | 1.00 | |
| react-hooks-rules-01 | hooks | 3 | 3 | 3 | 1.00 | |
| react-hooks-updater-02 | hooks, state | 2 | 2 | 2 | 1.00 | |
| react-hooks-memo-03 | hooks, perf | 2 | 3 | 3 | 1.00 | |
| react-hooks-reducer-04 | hooks, state | 1 | 2 | 2 | 1.00 | |
| react-hooks-use-05 | hooks, rsc, suspense | 1 | 2 | 1.5 | 0.75 | Promise/Context read and conditional call covered, but wrongly says `use` "should not be placed in a loop" and that it is "normally used in a Server Component" — half credit on the conditional/loop point |
| react-effects-deps-01 | effects | 3 | 2 | 2 | 1.00 | |
| react-effects-cleanup-02 | effects | 2 | 2 | 2 | 1.00 | |
| react-effects-misuse-03 | effects, state | 3 | 3 | 2 | 0.67 | derived-state case and "effects sync external systems" covered; chose data-fetching as the other case, never mentioned event-driven logic belongs in the handler |
| react-effects-strictmode-04 | effects | 2 | 2 | 2 | 1.00 | |
| react-rsc-boundary-01 | rsc | 3 | 3 | 2.5 | 0.83 | server/client split and module-level `"use client"` boundary correct; never states that Server Components ship no JS for themselves |
| react-rsc-props-02 | rsc | 2 | 2 | 2 | 1.00 | |
| react-rsc-data-03 | rsc, suspense | 2 | 2 | 2 | 1.00 | |
| react-context-rerender-01 | context, perf | 2 | 2 | 2 | 1.00 | |
| react-context-usage-02 | context, state | 1 | 2 | 2 | 1.00 | |
| react-perf-memo-01 | perf | 2 | 2 | 2 | 1.00 | |
| react-perf-list-02 | perf, reconciliation | 2 | 2 | 2 | 1.00 | |
| react-refs-useref-01 | refs | 2 | 2 | 2 | 1.00 | |
| react-refs-forward-02 | refs | 1 | 2 | 2 | 1.00 | |
| react-suspense-01 | suspense | 2 | 2 | 2 | 1.00 | |
| react-suspense-transition-02 | suspense, perf | 2 | 2 | 2 | 1.00 | |
| react-state-batching-01 | state | 2 | 2 | 2 | 1.00 | |
| react-state-lifting-02 | state | 1 | 2 | 2 | 1.00 | |
| react-state-derived-03 | state, effects | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| hooks | 0.97 | 5 | ok | omit (strong) |
| reconciliation | 1.00 | 4 | ok | omit (strong) |
| state | 0.93 | 8 | ok | omit (strong) |
| effects | 0.92 | 5 | ok | omit (strong) |
| rsc | 0.91 | 4 | ok | omit (strong) |
| context | 1.00 | 2 | low-n | omit (strong, low-n) |
| perf | 1.00 | 5 | ok | omit (strong) |
| refs | 1.00 | 2 | low-n | omit (strong, low-n) |
| suspense | 0.96 | 4 | ok | omit (strong) |

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 49.25/51 ≈ 97%
```

## Derivation targets

No tag fell below the 0.75 threshold for corpus_rev 1. No derived skill is
warranted — `derived/react.space-bunny-free.SKILL.md` is intentionally **not
created**. The three imperfect answers (`react-hooks-use-05`,
`react-effects-misuse-03`, `react-rsc-boundary-01`) sit inside tags (hooks,
effects, rsc) that otherwise cleared threshold comfortably; restating those
points would waste prompt/cache budget on a model that already knows this stack
well.
