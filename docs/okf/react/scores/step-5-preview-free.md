---
model_id: step-5-preview-free
model_version: "5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-12
stack: react
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — step-5-preview-free on react

> Valid ONLY for `step-5-preview-free` @ `5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book: the answerer was `ocode run -m opencode-go/step-5-preview-free -effort med`
in an empty scratch dir with only the `_prompts/react.md` sheet attached; the
session sqlite shows exactly 1 user + 1 assistant message and 0 tool calls.
Graded from `../answers/step-5-preview-free.md`. Answers are in the model's own
wording, consistently 2–4× longer than the reference, and carry a few small
factual slips of their own (noted below) — no sign of answer-key contamination.

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
| react-hooks-use-05 | hooks, rsc, suspense | 1 | 2 | 2 | 1.00 | both points present; incidental slip "you call it inside an `async` component" (hooks are not for async components) not penalized because it does not contradict either rubric point |
| react-effects-deps-01 | effects | 3 | 2 | 2 | 1.00 | |
| react-effects-cleanup-02 | effects | 2 | 2 | 2 | 1.00 | |
| react-effects-misuse-03 | effects, state | 3 | 3 | 2 | 0.67 | derived-state → compute during render covered; event-handler case present only as a trailing "other common cases" aside (`onSubmit`, not an effect watching a flag); picked data-fetching as its second headline case and never states that effects exist to synchronize with external systems |
| react-effects-strictmode-04 | effects | 2 | 2 | 2 | 1.00 | |
| react-rsc-boundary-01 | rsc | 3 | 3 | 2.5 | 0.83 | server/client split correct, "ships no JS" present; `"use client"` correctly called a boundary between server and client module graphs, but the surrounding explanation is muddled — opens by denying it makes the file client-only and inverts the import direction ("everything imported from that module into the client graph must be a client component"), rather than stating that the module and everything it imports become client code — half credit on the boundary point |
| react-rsc-props-02 | rsc | 2 | 2 | 2 | 1.00 | |
| react-rsc-data-03 | rsc, suspense | 2 | 2 | 2 | 1.00 | |
| react-context-rerender-01 | context, perf | 2 | 2 | 2 | 1.00 | |
| react-context-usage-02 | context, state | 1 | 2 | 2 | 1.00 | |
| react-perf-memo-01 | perf | 2 | 2 | 2 | 1.00 | |
| react-perf-list-02 | perf, reconciliation | 2 | 2 | 2 | 1.00 | |
| react-refs-useref-01 | refs | 2 | 2 | 2 | 1.00 | garbled trailing aside ("refs cannot be passed as render props … not serialized/stable") ignored; both points present |
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
| reconciliation | 1.00 | 4 | ok | omit (strong) |
| state | 0.93 | 8 | ok | omit (strong) |
| effects | 0.92 | 5 | ok | omit (strong) |
| rsc | 0.94 | 4 | ok | omit (strong) |
| context | 1.00 | 2 | low-n | omit (strong, low-n) |
| perf | 1.00 | 5 | ok | omit (strong) |
| refs | 1.00 | 2 | low-n | omit (strong, low-n) |
| suspense | 1.00 | 4 | ok | omit (strong) |

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 49.5/51 ≈ 97%
```

## Derivation targets

No tag fell below the 0.85 threshold for corpus_rev 1. No derived skill is
warranted — `derived/react.step-5-preview-free.SKILL.md` is intentionally **not
created**. The two imperfect answers (`react-effects-misuse-03`,
`react-rsc-boundary-01`) sit inside tags (effects, rsc) that otherwise cleared
threshold comfortably; restating those points would waste prompt/cache budget on
a model that already knows this stack well.
