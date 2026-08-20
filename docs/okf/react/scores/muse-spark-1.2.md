---
model_id: muse-spark-1.2
model_version: "1.2"
evaluated_via: opencode-go
evaluated_on: 2026-08-20
stack: react
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — muse-spark-1.2 on react

> Valid ONLY for `muse-spark-1.2` @ `1.2`. A version bump invalidates this
> scorecard — re-benchmark.

## Contamination check

Stack score is 100% (26/26 questions full marks). Verified NOT contamination:

- Answerer ran as an isolated `ocode2` subprocess in an empty
  `/tmp/kaizen-react-answer` directory with `-yolo`, given only the
  closed-book `_prompts/react.md` sheet (id + question text, no
  answers/rubric). Captured log (`/tmp/kaizen-react-answer/output.log`) shows
  a single `[LLM]` call (`in=22580 out=4702`) and **zero tool invocations** —
  no `read`, `bash`, `grep`, `webfetch`, or `websearch` calls appear despite
  44 tools being exposed. The model never touched the repo or the network.
- Answer wording diverges from `questions.yaml`'s reference `answer` field on
  every question (different phrasing, different examples, extra correct
  detail not in the rubric at all — e.g. `Object.is` semantics for context
  re-renders, the RSC Flight wire format, naming `react-window`/TanStack
  Virtual, the React 17→18 batching-scope contrast) — not a verbatim/
  near-verbatim copy of the rubric, which is the signature of the
  contamination case this runbook warns about.
- This corpus tests standard, extensively documented public React semantics
  (keys/reconciliation, rules of hooks, effects, RSC, context, Suspense,
  batching) that a strong model plausibly already knows cold from training
  data. A 100% here is a materially different signal than a 100% would be on
  this repo's un-learnable house-specific rules (e.g. the `conduct` stack).
- Consistent with sibling react scorecards on the same corpus: `mimo-v2.5`
  scored 92% and `tencent__hy3` scored 98% closed-book — both fell short of
  full marks on the same questions, so this isn't an eval-wide anomaly, just
  this model doing unusually well.

Two minor inaccuracies worth recording (rubric has no penalty mechanism, so
neither changes a score):
- `react-refs-forward-02` says `forwardRef` is "deprecated/removed" — it is
  deprecated in React 19 but still functional, not removed.
- `react-hooks-use-05` claims `use` can be called "even after early returns"
  — an overclaim; `use` relaxes the top-level/conditional-call restriction
  but still must run during render, not after a return.

Conclusion: legitimate closed-book result, not contamination.

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
| react-hooks-use-05 | hooks, rsc, suspense | 1 | 2 | 2 | 1.00 | overclaims `use` works "after early returns" — doesn't affect award |
| react-effects-deps-01 | effects | 3 | 2 | 2 | 1.00 | |
| react-effects-cleanup-02 | effects | 2 | 2 | 2 | 1.00 | |
| react-effects-misuse-03 | effects, state | 3 | 3 | 3 | 1.00 | both cases + external-sync framing |
| react-effects-strictmode-04 | effects | 2 | 2 | 2 | 1.00 | |
| react-rsc-boundary-01 | rsc | 3 | 3 | 3 | 1.00 | |
| react-rsc-props-02 | rsc | 2 | 2 | 2 | 1.00 | |
| react-rsc-data-03 | rsc, suspense | 2 | 2 | 2 | 1.00 | |
| react-context-rerender-01 | context, perf | 2 | 2 | 2 | 1.00 | cites `Object.is` reference-check mechanism |
| react-context-usage-02 | context, state | 1 | 2 | 2 | 1.00 | |
| react-perf-memo-01 | perf | 2 | 2 | 2 | 1.00 | |
| react-perf-list-02 | perf, reconciliation | 2 | 2 | 2 | 1.00 | names react-window/TanStack Virtual |
| react-refs-useref-01 | refs | 2 | 2 | 2 | 1.00 | |
| react-refs-forward-02 | refs | 1 | 2 | 2 | 1.00 | says forwardRef "deprecated/removed" — still functional in 19, minor overclaim |
| react-suspense-01 | suspense | 2 | 2 | 2 | 1.00 | |
| react-suspense-transition-02 | suspense, perf | 2 | 2 | 2 | 1.00 | |
| react-state-batching-01 | state | 2 | 2 | 2 | 1.00 | correctly contrasts React 17 vs 18 batching scope |
| react-state-lifting-02 | state | 1 | 2 | 2 | 1.00 | |
| react-state-derived-03 | state, effects | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| reconciliation | 1.00 | 4 | ok | omit (strong) |
| hooks | 1.00 | 5 | ok | omit (strong) |
| state | 1.00 | 8 | ok | omit (strong) |
| effects | 1.00 | 5 | ok | omit (strong) |
| rsc | 1.00 | 4 | ok | omit (strong) |
| context | 1.00 | 2 | low-n | omit (strong, mark low-n) |
| perf | 1.00 | 5 | ok | omit (strong) |
| refs | 1.00 | 2 | low-n | omit (strong, mark low-n) |
| suspense | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 51/51 = 100%
```

## Derivation targets

No tag falls below threshold (`< 0.75`). **No derived skill file was
created** — per `HOW-TO-EVALUATE.md` step 5, a derived skill is only written
for below-threshold tags, and covering already-strong tags would waste
prompt/cache budget.
