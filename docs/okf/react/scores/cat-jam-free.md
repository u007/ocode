---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: react
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — cat-jam-free on react

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

opencode-go free/alpha model; no version published (recorded as `"alpha"`). The
model self-identifies as "CatJam, developed by LitterBox AI".

Closed-book: the answerer was `ocode run -m opencode-go/cat-jam-free -effort med`
in an empty scratch cwd with only the `_prompts/react.md` sheet attached; the
session sqlite shows exactly 1 user + 1 assistant message and 0 tool calls.
Headless runs inject ocode's bundled skill-catalogue digest into the system
prompt. Graded from `../answers/cat-jam-free.md`. Answers are terse (2–4
sentences each), in the model's own wording, and carry no factual errors on the
constructs the rubric tests — no sign of answer-key contamination.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| react-recon-keys-01 | reconciliation | 2 | 2 | 2 | 1.00 | |
| react-recon-diff-02 | reconciliation | 2 | 3 | 3 | 1.00 | |
| react-recon-remount-03 | reconciliation, state | 2 | 2 | 2 | 1.00 | |
| react-hooks-rules-01 | hooks | 3 | 3 | 3 | 1.00 | |
| react-hooks-updater-02 | hooks, state | 2 | 2 | 2 | 1.00 | |
| react-hooks-memo-03 | hooks, perf | 2 | 3 | 3 | 1.00 | |
| react-hooks-reducer-04 | hooks, state | 1 | 2 | 2 | 1.00 | "easier testing" covers the pure/testable point |
| react-hooks-use-05 | hooks, rsc, suspense | 1 | 2 | 2 | 1.00 | |
| react-effects-deps-01 | effects | 3 | 2 | 2 | 1.00 | |
| react-effects-cleanup-02 | effects | 2 | 2 | 2 | 1.00 | |
| react-effects-misuse-03 | effects, state | 3 | 3 | 2 | 0.67 | derived-state and event-handler cases both correct; never states that effects exist to synchronize with external systems |
| react-effects-strictmode-04 | effects | 2 | 2 | 2 | 1.00 | |
| react-rsc-boundary-01 | rsc | 3 | 3 | 2.5 | 0.83 | client side and module-level `"use client"` boundary (module + its imports) correct; server side gives "ships no component code" and server-resource access, but never says Server Components can be `async` or can't use state/effects (only implied by contrast) — half credit on the server point |
| react-rsc-props-02 | rsc | 2 | 2 | 2 | 1.00 | correctly notes server action references can cross |
| react-rsc-data-03 | rsc, suspense | 2 | 2 | 2 | 1.00 | |
| react-context-rerender-01 | context, perf | 2 | 2 | 2 | 1.00 | says "memoized consumers may re-render" — understates (every consumer re-renders) but the identity → re-render mechanism and the fix are right |
| react-context-usage-02 | context, state | 1 | 2 | 2 | 1.00 | |
| react-perf-memo-01 | perf | 2 | 2 | 2 | 1.00 | |
| react-perf-list-02 | perf, reconciliation | 2 | 2 | 2 | 1.00 | |
| react-refs-useref-01 | refs | 2 | 2 | 2 | 1.00 | |
| react-refs-forward-02 | refs | 1 | 2 | 2 | 1.00 | |
| react-suspense-01 | suspense | 2 | 2 | 2 | 1.00 | |
| react-suspense-transition-02 | suspense, perf | 2 | 2 | 2 | 1.00 | "pending flag" = isPending |
| react-state-batching-01 | state | 2 | 2 | 2 | 1.00 | |
| react-state-lifting-02 | state | 1 | 2 | 1 | 0.50 | lift to closest common ancestor + pass value/callbacks down; never names the parent as the single source of truth |
| react-state-derived-03 | state, effects | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| hooks | 1.00 | 5 | ok | omit (strong) |
| reconciliation | 1.00 | 4 | ok | omit (strong) |
| state | 0.89 | 8 | ok | omit (strong) |
| effects | 0.92 | 5 | ok | omit (strong) |
| rsc | 0.94 | 4 | ok | omit (strong) |
| context | 1.00 | 2 | low-n | omit (strong, low-n) |
| perf | 1.00 | 5 | ok | omit (strong) |
| refs | 1.00 | 2 | low-n | omit (strong, low-n) |
| suspense | 1.00 | 4 | ok | omit (strong) |

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 49/51 ≈ 96%
```

## Derivation targets

No tag fell below the 0.85 threshold for corpus_rev 1. No derived skill is
warranted — `derived/react.cat-jam-free.SKILL.md` is intentionally **not
created**. The three imperfect answers (`react-effects-misuse-03`,
`react-rsc-boundary-01`, `react-state-lifting-02`) are omissions of a framing
point in otherwise-correct answers, inside tags that cleared threshold.
