---
model_id: nvidia/nemotron-3.5-super-vl
model_version: "3.5"
evaluated_via: openrouter
evaluated_on: 2026-10-16
stack: react
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — nvidia/nemotron-3.5-super-vl on react

> Valid ONLY for `nvidia/nemotron-3.5-super-vl` @ `3.5`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book (Rule 0). The answerer was
`ocode run -m openrouter/nvidia/nemotron-3.5-super-vl:free -effort med` under an
isolated HOME whose config denies every tool (no tools were advertised), in a
scratch cwd containing only the `_prompts/react.md` sheet, one fresh session; the
session sqlite shows exactly 1 user + 1 assistant message and 0 tool calls. All 26
ids came back once, none empty. Headless runs inject the bundled skill-catalogue
digest into the system prompt; no answer cites a tool or skill the question never
named (the only external names are the virtualization libraries
`react-window` / `TanStack Virtual` / `react-virtuoso` in `react-perf-list-02`,
which are ordinary domain knowledge, not catalogue entries). Graded from
`../answers/nvidia__nemotron-3.5-super-vl.md`. Answers are in the model's own
wording, roughly 1.5–3× the reference length, with two small factual slips of
their own (noted below) — no sign of answer-key contamination, no duplicated
answers, no thinking-aloud, no fabricated tool output.

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
| react-hooks-use-05 | hooks, rsc, suspense | 1 | 2 | 2 | 1.00 | |
| react-effects-deps-01 | effects | 3 | 2 | 2 | 1.00 | |
| react-effects-cleanup-02 | effects | 2 | 2 | 2 | 1.00 | |
| react-effects-misuse-03 | effects, state | 3 | 3 | 2 | 0.67 | derived-state → compute during render covered; its two headline cases are both variants of derived state (compute-from-props, mirror-prop-into-state), and the event-handler case appears only as a one-line trailing aside ("call the function directly in the handler") — awarded, since the concept is stated; never says effects exist to synchronize with external systems |
| react-effects-strictmode-04 | effects | 2 | 2 | 2 | 1.00 | |
| react-rsc-boundary-01 | rsc | 3 | 3 | 3 | 1.00 | `"use client"` correctly called a module-level boundary whose code is bundled for the client; omits that everything the module imports also becomes client code, and "does not make every descendant a Client Component" is true only in the children-as-props sense it goes on to name — not docked, noted |
| react-rsc-props-02 | rsc | 2 | 2 | 1.5 | 0.75 | serializable set and no-functions/class-instances both present; judgment call: lists Promises among the values that cannot cross, which is wrong for React 19 (a Server Component may pass a Promise to a Client Component, which reads it with `use`) — a materially wrong claim about the same construct, half credit on the second point |
| react-rsc-data-03 | rsc, suspense | 2 | 2 | 2 | 1.00 | |
| react-context-rerender-01 | context, perf | 2 | 2 | 2 | 1.00 | |
| react-context-usage-02 | context, state | 1 | 2 | 2 | 1.00 | |
| react-perf-memo-01 | perf | 2 | 2 | 2 | 1.00 | |
| react-perf-list-02 | perf, reconciliation | 2 | 2 | 2 | 1.00 | |
| react-refs-useref-01 | refs | 2 | 2 | 2 | 1.00 | |
| react-refs-forward-02 | refs | 1 | 2 | 2 | 1.00 | |
| react-suspense-01 | suspense | 2 | 2 | 2 | 1.00 | mechanism given as "throwing a thenable"; does not name `use`/`lazy`/a data library, but the pending-resource mechanism is the point |
| react-suspense-transition-02 | suspense, perf | 2 | 2 | 2 | 1.00 | |
| react-state-batching-01 | state | 2 | 2 | 2 | 1.00 | incidental slip: says `flushSync` is imported from `react-dom/client` (it is exported by `react-dom`); import-path detail, concept intact, not docked |
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
warranted — `derived/react.nvidia__nemotron-3.5-super-vl.SKILL.md` is
intentionally **not created**. The two imperfect answers
(`react-effects-misuse-03`, `react-rsc-props-02`) sit inside tags (effects, rsc)
that otherwise cleared threshold comfortably; restating those points would waste
prompt/cache budget on a model that already knows this stack well.
