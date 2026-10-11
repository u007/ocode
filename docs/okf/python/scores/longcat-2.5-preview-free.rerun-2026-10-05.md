---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-05
stack: python
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — longcat-2.5-preview-free on python (2026-10-05 full re-run)

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump invalidates
> this scorecard — re-benchmark. Baseline rerun 2026-10-05; an earlier 2026-09-28
> scorecard exists beside it.

Grading note: one answer record (`python-context-contextmanager-02`) was malformed YAML
(mis-indented `answer:`); graded by `- id:` record anyway.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| python-types-union-01 | types-hints | 2 | 2 | 2 | 1.00 |  |
| python-types-generics-02 | types-hints | 2 | 2 | 2 | 1.00 |  |
| python-types-protocol-03 | types-hints | 2 | 2 | 2 | 1.00 |  |
| python-types-self-04 | types-hints | 1 | 2 | 2 | 1.00 |  |
| python-dataclasses-basics-01 | dataclasses | 2 | 2 | 2 | 1.00 |  |
| python-dataclasses-frozen-02 | dataclasses | 2 | 2 | 2 | 1.00 |  |
| python-dataclasses-slots-03 | dataclasses | 1 | 2 | 2 | 1.00 |  |
| python-dataclasses-vs-04 | dataclasses | 2 | 3 | 3 | 1.00 |  |
| python-async-await-01 | async | 3 | 2 | 2 | 1.00 |  |
| python-async-taskgroup-02 | async | 3 | 3 | 3 | 1.00 |  |
| python-async-blocking-03 | async | 3 | 2 | 2 | 1.00 |  |
| python-async-cancel-04 | async, errors-exceptions | 2 | 2 | 2 | 1.00 |  |
| python-itergen-yield-01 | iterators-generators | 3 | 2 | 2 | 1.00 |  |
| python-itergen-genexpr-02 | iterators-generators | 2 | 2 | 2 | 1.00 |  |
| python-itergen-itertools-03 | iterators-generators | 1 | 2 | 2 | 1.00 |  |
| python-itergen-protocol-04 | iterators-generators, data-model | 2 | 2 | 2 | 1.00 |  |
| python-context-with-01 | context-managers | 3 | 2 | 2 | 1.00 |  |
| python-context-contextmanager-02 | context-managers, decorators | 2 | 2 | 1 | 0.50 | setup/teardown around yield correct; no try/finally for exception safety |
| python-context-exitstack-03 | context-managers | 1 | 2 | 2 | 1.00 |  |
| python-context-async-04 | context-managers, async | 2 | 2 | 2 | 1.00 |  |
| python-decorators-basics-01 | decorators | 3 | 2 | 2 | 1.00 |  |
| python-decorators-args-02 | decorators | 2 | 2 | 2 | 1.00 |  |
| python-decorators-stacking-03 | decorators | 2 | 2 | 2 | 1.00 |  |
| python-decorators-class-04 | decorators | 1 | 2 | 2 | 1.00 |  |
| python-datamodel-eqhash-01 | data-model | 3 | 2 | 2 | 1.00 |  |
| python-datamodel-slots-02 | data-model | 2 | 2 | 2 | 1.00 |  |
| python-datamodel-mutable-03 | data-model | 3 | 2 | 2 | 1.00 |  |
| python-datamodel-is-04 | data-model | 2 | 2 | 2 | 1.00 |  |
| python-datamodel-descriptor-05 | data-model | 1 | 2 | 2 | 1.00 |  |
| python-errors-elsefinally-01 | errors-exceptions | 2 | 2 | 2 | 1.00 |  |
| python-errors-custom-02 | errors-exceptions | 2 | 2 | 2 | 1.00 |  |
| python-errors-raisefrom-03 | errors-exceptions | 2 | 2 | 2 | 1.00 |  |
| python-errors-group-04 | errors-exceptions | 2 | 2 | 2 | 1.00 |  |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| async | 1.00 | 5 | ok | omit (strong) |
| context-managers | 0.88 | 4 | ok | omit (strong) |
| data-model | 1.00 | 6 | ok | omit (strong) |
| dataclasses | 1.00 | 4 | ok | omit (strong) |
| decorators | 0.90 | 5 | ok | omit (strong) |
| errors-exceptions | 1.00 | 5 | ok | omit (strong) |
| iterators-generators | 1.00 | 4 | ok | omit (strong) |
| types-hints | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 67/68 = 98.5%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none**. No derived skill is warranted.
