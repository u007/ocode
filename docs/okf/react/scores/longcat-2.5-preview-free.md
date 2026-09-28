---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: react
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — longcat-2.5-preview-free on react

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump
> invalidates this scorecard — re-benchmark.

Closed-book answers graded from `../answers/longcat-2.5-preview-free.md`
(produced via `ocode run` from an isolated dir; session audited: 1 user + 1
assistant turn, zero tool calls). Orchestrator housekeeping before grading:
stripped markdown fences, fixed a duplicated `id: id:` prefix. No answer text
was altered.

Contamination check: **clean**. No answer is a near-verbatim match to a
reference `answer`. Every answer is 2–4x longer than the key, in the model's own
wording, and carries material the key doesn't (Zustand/Jotai/Redux for
high-frequency state, `flushSync` nesting restriction, `react-window` /
`@tanstack/react-virtual`, Server Actions, Date/Map/Set as serializable RSC
props). The misses (no "effects sync external systems", no "ships no JS" /
"can be async" for Server Components) are the kind a blind run produces.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| react-recon-keys-01 | reconciliation | 2 | 2 | 2 | 1.00 | |
| react-recon-diff-02 | reconciliation | 2 | 3 | 2.5 | 0.83 | type comparison + type change unmounts/remounts, key matching present; key only framed as list matching, never says a changed key at the same position remounts (state reset) — half credit on the third point |
| react-recon-remount-03 | reconciliation, state | 2 | 2 | 2 | 1.00 | |
| react-hooks-rules-01 | hooks | 3 | 3 | 3 | 1.00 | |
| react-hooks-updater-02 | hooks, state | 2 | 2 | 2 | 1.00 | |
| react-hooks-memo-03 | hooks, perf | 2 | 3 | 3 | 1.00 | |
| react-hooks-reducer-04 | hooks, state | 1 | 2 | 2 | 1.00 | |
| react-hooks-use-05 | hooks, rsc, suspense | 1 | 2 | 2 | 1.00 | |
| react-effects-deps-01 | effects | 3 | 2 | 2 | 1.00 | |
| react-effects-cleanup-02 | effects | 2 | 2 | 2 | 1.00 | |
| react-effects-misuse-03 | effects, state | 3 | 3 | 2 | 0.67 | derived-state case present; event-handler case present (as a trailing "other example"); never states effects are for synchronizing with external systems. Its second headline case (prop→state sync) is a restatement of the first |
| react-effects-strictmode-04 | effects | 2 | 2 | 2 | 1.00 | |
| react-rsc-boundary-01 | rsc | 3 | 3 | 2.5 | 0.83 | client side and module-level `"use client"` boundary correct; server side lists no state/effects/browser APIs but never says Server Components can be `async` or ship no JS for themselves — half credit on the server point |
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
| hooks | 1.00 | 5 | ok | omit (strong) |
| reconciliation | 0.96 | 4 | ok | omit (strong) |
| state | 0.93 | 8 | ok | omit (strong) |
| effects | 0.92 | 5 | ok | omit (strong) |
| rsc | 0.94 | 4 | ok | omit (strong) |
| context | 1.00 | 2 | low-n | omit (strong, low-n) |
| perf | 1.00 | 5 | ok | omit (strong) |
| refs | 1.00 | 2 | low-n | omit (strong, low-n) |
| suspense | 1.00 | 4 | ok | omit (strong) |

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 49.17/51 ≈ 96%
```

## Derivation targets

No tag fell below the 0.75 threshold for corpus_rev 1. No derived skill is
warranted — `derived/react.longcat-2.5-preview-free.SKILL.md` is intentionally
**not created**. The three imperfect answers (`react-recon-diff-02`,
`react-effects-misuse-03`, `react-rsc-boundary-01`) sit inside tags
(reconciliation, effects/state, rsc) that otherwise cleared threshold
comfortably.
