---
model_id: step-5-preview-free
model_version: "5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-12
stack: python
stack_corpus_rev: 1
threshold: 0.85
---

<!-- Filename: model_id "step-5-preview-free" has no "/" — no flattening needed. -->

# Scorecard — step-5-preview-free on python

> Valid ONLY for `step-5-preview-free` @ `5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book (Rule 0): answerer was `ocode run -m opencode-go/step-5-preview-free -effort med`
in an empty scratch dir with only the `_prompts/python.md` sheet attached; the session
sqlite shows exactly 1 user + 1 assistant message and 0 tool calls. Answers file:
`../answers/step-5-preview-free.md` (33/33 ids present, valid YAML).

Grading note: several answers contain mid-sentence self-corrections ("... rather:",
"... a better direct example is") and one garbled fragment (`python-types-union-01`
renders the PEP 604 form as a bare `` `int` `` before restating it correctly as
`int | None`). These are decoding artifacts, not missing concepts; graded on the
concepts actually present. No answer reads as a copy of the reference text — every
answer is longer, differently structured, and carries detail the reference does not
(`__type_params__`, `weakref_slot`, `task.uncancel()`, `_replace`, `NotRequired`).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| python-types-union-01 | types-hints | 2 | 2 | 2 | 1.00 | one garbled `` `int` `` token, but `int | None` / `int | str` stated explicitly |
| python-types-generics-02 | types-hints | 2 | 2 | 2 | 1.00 |  |
| python-types-protocol-03 | types-hints | 2 | 2 | 2 | 1.00 |  |
| python-types-self-04 | types-hints | 1 | 2 | 2 | 1.00 |  |
| python-dataclasses-basics-01 | dataclasses | 2 | 2 | 2 | 1.00 | minor: claims `__ne__` is generated (it is derived from `__eq__`); not rubric-relevant |
| python-dataclasses-frozen-02 | dataclasses | 2 | 2 | 2 | 1.00 |  |
| python-dataclasses-slots-03 | dataclasses | 1 | 2 | 2 | 1.00 |  |
| python-dataclasses-vs-04 | dataclasses | 2 | 3 | 3 | 1.00 |  |
| python-async-await-01 | async | 3 | 2 | 2 | 1.00 |  |
| python-async-taskgroup-02 | async | 3 | 3 | 3 | 1.00 |  |
| python-async-blocking-03 | async | 3 | 2 | 2 | 1.00 | stray "rewrite as `async for`" advice is off, but both points present |
| python-async-cancel-04 | async, errors-exceptions | 2 | 2 | 2 | 1.00 | re-raise point fully made; does not mention `CancelledError` is a `BaseException` |
| python-itergen-yield-01 | iterators-generators | 3 | 2 | 2 | 1.00 |  |
| python-itergen-genexpr-02 | iterators-generators | 2 | 2 | 2 | 1.00 |  |
| python-itergen-itertools-03 | iterators-generators | 1 | 2 | 2 | 1.00 |  |
| python-itergen-protocol-04 | iterators-generators, data-model | 2 | 2 | 2 | 1.00 |  |
| python-context-with-01 | context-managers | 3 | 2 | 2 | 1.00 |  |
| python-context-contextmanager-02 | context-managers, decorators | 2 | 2 | 2 | 1.00 | try/finally shown in code and explained (exception thrown into generator at yield) |
| python-context-exitstack-03 | context-managers | 1 | 2 | 2 | 1.00 |  |
| python-context-async-04 | context-managers, async | 2 | 2 | 2 | 1.00 |  |
| python-decorators-basics-01 | decorators | 3 | 2 | 2 | 1.00 |  |
| python-decorators-args-02 | decorators | 2 | 2 | 2 | 1.00 |  |
| python-decorators-stacking-03 | decorators | 2 | 2 | 2 | 1.00 |  |
| python-decorators-class-04 | decorators | 1 | 2 | 2 | 1.00 | self-corrects an `ABCMeta` tangent; `@dataclass` / `@total_ordering` with what they inject |
| python-datamodel-eqhash-01 | data-model | 3 | 2 | 2 | 1.00 | one abandoned fragment ("`obj = frozen(...)`... rather:"); both concepts present |
| python-datamodel-slots-02 | data-model | 2 | 2 | 2 | 1.00 |  |
| python-datamodel-mutable-03 | data-model | 3 | 2 | 2 | 1.00 |  |
| python-datamodel-is-04 | data-model | 2 | 2 | 2 | 1.00 |  |
| python-datamodel-descriptor-05 | data-model | 1 | 2 | 2 | 1.00 | contradicts itself once (lists "function types" among data descriptors after correctly calling functions non-data); core mechanism and property/method binding correct |
| python-errors-elsefinally-01 | errors-exceptions | 2 | 2 | 2 | 1.00 | two incoherent fragments ("cannot be combined with except absent...", "A finally that catches infinitely..."); both rubric points stated clearly elsewhere |
| python-errors-custom-02 | errors-exceptions | 2 | 2 | 2 | 1.00 | garbled parenthetical ("never bare Exception problem..."); points present |
| python-errors-raisefrom-03 | errors-exceptions | 2 | 2 | 2 | 1.00 |  |
| python-errors-group-04 | errors-exceptions | 2 | 2 | 2 | 1.00 |  |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| async | 1.00 | 5 | ok | omit (strong) |
| context-managers | 1.00 | 4 | ok | omit (strong) |
| data-model | 1.00 | 6 | ok | omit (strong) |
| dataclasses | 1.00 | 4 | ok | omit (strong) |
| decorators | 1.00 | 5 | ok | omit (strong) |
| errors-exceptions | 1.00 | 5 | ok | omit (strong) |
| iterators-generators | 1.00 | 4 | ok | omit (strong) |
| types-hints | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 68 / 68 = 100%
```

## Derivation targets

Tags below threshold (`< 0.85`): **none**. No derived skill is warranted; no
`derived/python.step-5-preview-free.SKILL.md` was created.

Context: this corpus sits near its ceiling for current models (other python
scorecards in this directory range 97.1%–100%, two of them also at 100%), so a
flat 100% here reflects corpus difficulty rather than contamination. Verbatim
overlap with the reference answers was checked and not found (see Provenance).
