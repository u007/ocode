---
model_id: claude-opus-4-8
model_version: "4.8"
evaluated_via: anthropic
evaluated_on: 2026-07-11
stack: react
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — claude-opus-4-8 on react

> **ILLUSTRATIVE / WORKED EXAMPLE.** These awarded scores demonstrate the
> scorecard shape and how subscores drive derivation. They are **not** a real
> evaluation — re-grade against `../questions.yaml` before trusting them.
>
> Valid ONLY for `claude-opus-4-8` @ `4.8`. A version bump invalidates it.

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
| react-hooks-use-05 | hooks, rsc, suspense | 1 | 2 | 1 | 0.50 | missed conditional-call point |
| react-effects-deps-01 | effects | 3 | 2 | 2 | 1.00 | |
| react-effects-cleanup-02 | effects | 2 | 2 | 2 | 1.00 | |
| react-effects-misuse-03 | effects, state | 3 | 3 | 2 | 0.67 | gave one case only |
| react-effects-strictmode-04 | effects | 2 | 2 | 1 | 0.50 | missed "fix cleanup not disable" |
| react-rsc-boundary-01 | rsc | 3 | 3 | 2 | 0.67 | described "use client" as per-component |
| react-rsc-props-02 | rsc | 2 | 2 | 1 | 0.50 | serializable, missed no-functions |
| react-rsc-data-03 | rsc, suspense | 2 | 2 | 1 | 0.50 | no contrast to useEffect timing |
| react-context-rerender-01 | context, perf | 2 | 2 | 2 | 1.00 | |
| react-context-usage-02 | context, state | 1 | 2 | 1 | 0.50 | no frequency caveat |
| react-perf-memo-01 | perf | 2 | 2 | 2 | 1.00 | |
| react-perf-list-02 | perf, reconciliation | 2 | 2 | 1 | 0.50 | led with memo, not windowing |
| react-refs-useref-01 | refs | 2 | 2 | 2 | 1.00 | |
| react-refs-forward-02 | refs | 1 | 2 | 1 | 0.50 | missed React 19 ref-as-prop |
| react-suspense-01 | suspense | 2 | 2 | 1 | 0.50 | vague on what suspends |
| react-suspense-transition-02 | suspense, perf | 2 | 2 | 1 | 0.50 | "makes it async" framing |
| react-state-batching-01 | state | 2 | 2 | 2 | 1.00 | |
| react-state-lifting-02 | state | 1 | 2 | 2 | 1.00 | |
| react-state-derived-03 | state, effects | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| hooks | 0.94 | 5 | ok | omit (strong) |
| reconciliation | 0.90 | 4 | ok | omit (strong) |
| state | 0.94 | 7 | ok | omit (strong) |
| effects | 0.82 | 5 | ok | omit (strong) |
| perf | 0.80 | 5 | ok | omit (strong) |
| context | 0.83 | 2 | low-n | omit (strong, low-n) |
| refs | 0.67 | 2 | low-n | **derive** (low-n) |
| rsc | 0.58 | 4 | ok | **derive** |
| suspense | 0.55 | 4 | ok | **derive** |

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) ≈ 84%
```

## Derivation targets

Tags below threshold (`< 0.75`): **rsc, suspense, refs (low-n)** →
`../derived/react.claude-opus-4-8.SKILL.md`. Strong tags (hooks, reconciliation,
state, effects, perf, context) are omitted from the skill — the model already
knows them.
