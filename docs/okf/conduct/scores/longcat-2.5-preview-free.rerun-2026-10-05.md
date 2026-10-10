---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-05
stack: conduct
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — longcat-2.5-preview-free on conduct (2026-10-05 full re-run)

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump invalidates
> this scorecard — re-benchmark. Baseline rerun 2026-10-05; an earlier 2026-09-28 scorecard exists beside it.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| conduct-validation-01 | validation | 3 | 2 | 2 | 1.00 |  |
| conduct-validation-02 | validation | 2 | 2 | 2 | 1.00 |  |
| conduct-validation-03 | validation, lifecycle | 2 | 2 | 2 | 1.00 |  |
| conduct-failfast-01 | fail-fast | 3 | 2 | 2 | 1.00 |  |
| conduct-failfast-02 | fail-fast | 3 | 2 | 2 | 1.00 |  |
| conduct-failfast-03 | fail-fast, testing | 2 | 2 | 2 | 1.00 |  |
| conduct-failfast-04 | fail-fast, error-handling | 2 | 2 | 1 | 0.50 | missed should-be-present vs genuinely-optional distinction |
| conduct-error-01 | error-handling | 3 | 2 | 2 | 1.00 |  |
| conduct-error-02 | error-handling | 3 | 2 | 2 | 1.00 |  |
| conduct-error-03 | error-handling | 2 | 2 | 2 | 1.00 |  |
| conduct-error-04 | error-handling | 2 | 2 | 2 | 1.00 |  |
| conduct-halluc-01 | hallucination | 3 | 2 | 2 | 1.00 |  |
| conduct-halluc-02 | hallucination | 3 | 2 | 2 | 1.00 |  |
| conduct-halluc-03 | hallucination, verification | 2 | 2 | 2 | 1.00 |  |
| conduct-halluc-04 | hallucination | 2 | 2 | 2 | 1.00 |  |
| conduct-testing-01 | testing | 3 | 2 | 2 | 1.00 |  |
| conduct-testing-02 | testing | 3 | 2 | 1 | 0.50 | said delete only if test wrong/redundant; missed refactor/changed-behavior/removed-feature and ask-if-unsure |
| conduct-testing-03 | testing, error-handling | 2 | 2 | 2 | 1.00 |  |
| conduct-testing-04 | testing, verification | 2 | 2 | 1 | 0.50 | passes before/after ok; missed add-coverage-first if untested |
| conduct-simplicity-01 | simplicity | 2 | 2 | 2 | 1.00 |  |
| conduct-simplicity-02 | simplicity | 3 | 2 | 2 | 1.00 |  |
| conduct-simplicity-03 | simplicity | 2 | 2 | 2 | 1.00 |  |
| conduct-surgical-01 | surgical-changes | 2 | 2 | 1 | 0.50 | left adjacent code alone; did not say match existing style |
| conduct-surgical-02 | surgical-changes | 2 | 2 | 2 | 1.00 |  |
| conduct-surgical-03 | surgical-changes | 1 | 1 | 1 | 1.00 |  |
| conduct-lifecycle-01 | lifecycle | 3 | 2 | 2 | 1.00 |  |
| conduct-lifecycle-02 | lifecycle | 2 | 2 | 2 | 1.00 |  |
| conduct-lifecycle-03 | lifecycle | 2 | 2 | 1 | 0.50 | told user; did not name TODO.md |
| conduct-verify-01 | verification | 3 | 2 | 2 | 1.00 |  |
| conduct-verify-02 | verification | 2 | 2 | 2 | 1.00 |  |
| conduct-safety-01 | safety | 3 | 2 | 1 | 0.50 | confirm ok; missed inspect target before delete/overwrite |
| conduct-safety-02 | safety | 3 | 2 | 2 | 1.00 |  |
| conduct-safety-03 | safety | 2 | 2 | 2 | 1.00 |  |
| conduct-review-01 | code-review | 3 | 2 | 2 | 1.00 |  |
| conduct-review-02 | code-review | 2 | 2 | 2 | 1.00 |  |
| conduct-review-03 | code-review, verification | 2 | 2 | 2 | 1.00 |  |
| conduct-debug-01 | debugging | 3 | 2 | 2 | 1.00 |  |
| conduct-debug-02 | debugging | 3 | 2 | 2 | 1.00 |  |
| conduct-debug-03 | debugging | 2 | 2 | 1 | 0.50 | gather evidence ok; missed one-variable-at-a-time isolation |
| conduct-validation-04 | validation | 2 | 2 | 2 | 1.00 |  |
| conduct-simplicity-04 | simplicity | 2 | 2 | 2 | 1.00 |  |
| conduct-surgical-04 | surgical-changes | 2 | 2 | 2 | 1.00 |  |
| conduct-safety-04 | safety | 3 | 2 | 2 | 1.00 |  |
| conduct-review-04 | code-review | 2 | 2 | 2 | 1.00 |  |
| conduct-debug-04 | debugging | 2 | 2 | 2 | 1.00 |  |
| conduct-surgical-05 | surgical-changes, verification | 3 | 2 | 2 | 1.00 |  |
| conduct-surgical-06 | surgical-changes | 2 | 2 | 2 | 1.00 |  |
| conduct-surgical-07 | surgical-changes | 2 | 2 | 2 | 1.00 |  |
| conduct-context-01 | context-accuracy | 2 | 2 | 2 | 1.00 |  |
| conduct-safety-05 | safety | 3 | 2 | 2 | 1.00 |  |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| code-review | 1.00 | 4 | ok | omit (strong) |
| context-accuracy | 1.00 | 1 | low-n | omit (strong) |
| debugging | 0.90 | 4 | ok | omit (strong) |
| error-handling | 0.93 | 6 | ok | omit (strong) |
| fail-fast | 0.90 | 4 | ok | omit (strong) |
| hallucination | 1.00 | 4 | ok | omit (strong) |
| lifecycle | 0.89 | 4 | ok | omit (strong) |
| safety | 0.89 | 5 | ok | omit (strong) |
| simplicity | 1.00 | 4 | ok | omit (strong) |
| surgical-changes | 0.93 | 7 | ok | omit (strong) |
| testing | 0.79 | 5 | ok | omit (strong) |
| validation | 1.00 | 4 | ok | omit (strong) |
| verification | 0.93 | 6 | ok | omit (strong) |

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 93.3%
```

## Derivation targets

Tags below threshold (`< 0.75`): none. No derived skill written (per task instruction, none requested). Closest to threshold: testing (0.79).
