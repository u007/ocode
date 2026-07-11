---
model_id: tencent/hy3
model_version: "3.0"
evaluated_via: novita
evaluated_on: 2026-07-11
stack: react
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — tencent/hy3 on react

> Valid ONLY for `tencent/hy3` @ `3.0`. A version bump invalidates this
> scorecard — re-benchmark.

Grader is an independent model (Opus 4.8); answers were produced closed-book
(the model never saw the rubric).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| react-recon-keys-01 | reconciliation | 2 | 2 | 2 | 1.00 | |
| react-recon-diff-02 | reconciliation | 2 | 3 | 3 | 1.00 | type + key + remount-on-change all present |
| react-recon-remount-03 | reconciliation, state | 2 | 2 | 2 | 1.00 | |
| react-hooks-rules-01 | hooks | 3 | 3 | 3 | 1.00 | call-order reason correct |
| react-hooks-updater-02 | hooks, state | 2 | 2 | 2 | 1.00 | |
| react-hooks-memo-03 | hooks, perf | 2 | 3 | 3 | 1.00 | incl. "not free" overhead point |
| react-hooks-reducer-04 | hooks, state | 1 | 2 | 2 | 1.00 | testable/centralized covered |
| react-hooks-use-05 | hooks, rsc, suspense | 1 | 2 | 2 | 1.00 | conditional-call point present |
| react-effects-deps-01 | effects | 3 | 2 | 2 | 1.00 | |
| react-effects-cleanup-02 | effects | 2 | 2 | 2 | 1.00 | before-re-run AND unmount both stated |
| react-effects-misuse-03 | effects, state | 3 | 3 | 2 | 0.67 | MISS: never states positive "effects are for synchronizing with external systems"; covers derived-state + (parenthetically) event-handler only |
| react-effects-strictmode-04 | effects | 2 | 2 | 2 | 1.00 | |
| react-rsc-boundary-01 | rsc | 3 | 3 | 3 | 1.00 | server "no hooks/state" only implied by contrast, but correct |
| react-rsc-props-02 | rsc | 2 | 2 | 2 | 1.00 | "dates via serialization" slightly overclaims vs rubric caveat; not docked |
| react-rsc-data-03 | rsc, suspense | 2 | 2 | 2 | 1.00 | |
| react-context-rerender-01 | context, perf | 2 | 2 | 2 | 1.00 | |
| react-context-usage-02 | context, state | 1 | 2 | 2 | 1.00 | |
| react-perf-memo-01 | perf | 2 | 2 | 2 | 1.00 | |
| react-perf-list-02 | perf, reconciliation | 2 | 2 | 2 | 1.00 | |
| react-refs-useref-01 | refs | 2 | 2 | 2 | 1.00 | |
| react-refs-forward-02 | refs | 1 | 2 | 2 | 1.00 | React 19 ref-as-prop change present |
| react-suspense-01 | suspense | 2 | 2 | 2 | 1.00 | |
| react-suspense-transition-02 | suspense, perf | 2 | 2 | 2 | 1.00 | |
| react-state-batching-01 | state | 2 | 2 | 2 | 1.00 | 17→18 scope change correct |
| react-state-lifting-02 | state | 1 | 2 | 2 | 1.00 | |
| react-state-derived-03 | state, effects | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| reconciliation | 1.00 | 4 | ok | omit (strong) |
| hooks | 1.00 | 5 | ok | omit (strong) |
| effects | 0.92 | 5 | ok | omit (strong) |
| rsc | 1.00 | 4 | ok | omit (strong) |
| context | 1.00 | 2 | low-n | omit (strong) |
| perf | 1.00 | 5 | ok | omit (strong) |
| refs | 1.00 | 2 | low-n | omit (strong) |
| suspense | 1.00 | 4 | ok | omit (strong) |
| state | 0.93 | 8 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 50.00 / 51 = 98.0%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none** — every tag scored ≥ 0.75
(lowest: effects 0.92, state 0.93). **No derivation** — no
`derived/react.tencent__hy3.SKILL.md` is produced.
