---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: python
stack_corpus_rev: 1
threshold: 0.85
---

<!-- Filename: model_id "cat-jam-free" has no "/" — no flattening needed. -->

# Scorecard — cat-jam-free on python

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book (Rule 0): answerer was `ocode run -m opencode-go/cat-jam-free -effort med`
in an empty scratch cwd with only the `_prompts/python.md` sheet attached; the session
sqlite shows exactly 1 user + 1 assistant message and 0 tool calls. Headless runs inject
ocode's bundled skill-catalogue digest into the system prompt. Answers file:
`../answers/cat-jam-free.md` (33/33 ids present). `model_version` is `alpha`: this is an
opencode-go free/alpha model with no published version; the model self-identifies as
"CatJam, developed by LitterBox AI".

Harvest note: after the closing YAML fence the model leaked a paragraph of
thinking-aloud text about whether to add a recap block; the harvester excluded it. It
contained no answer content and was not graded.

Grading note: answers are terse (one to three sentences each) and hit most rubric
points directly. No answer reads as a copy of the reference text. Points were withheld
where the answer states the construct wrongly or leaves the tested invariant unstated
(see notes).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| python-types-union-01 | types-hints | 2 | 2 | 2 | 1.00 | `Union[int, None]`, `int \| None`; no "3.10" or `int \| str`, concept present |
| python-types-generics-02 | types-hints | 2 | 2 | 2 | 1.00 | also names `type Alias[T]` |
| python-types-protocol-03 | types-hints | 2 | 2 | 2 | 1.00 |  |
| python-types-self-04 | types-hints | 1 | 2 | 2 | 1.00 | pre-3.11 bound `TypeVar` named |
| python-dataclasses-basics-01 | dataclasses | 2 | 2 | 2 | 1.00 |  |
| python-dataclasses-frozen-02 | dataclasses | 2 | 2 | 2 | 1.00 |  |
| python-dataclasses-slots-03 | dataclasses | 1 | 2 | 2 | 1.00 |  |
| python-dataclasses-vs-04 | dataclasses | 2 | 3 | 3 | 1.00 | NamedTuple only "tuple-like", no indexable/hashable detail; concept present |
| python-async-await-01 | async | 3 | 2 | 2 | 1.00 |  |
| python-async-taskgroup-02 | async | 3 | 3 | 2 | 0.67 | missed gather's failure semantics (one failure propagates, siblings keep running); says only "runs concurrently and collects results". ExceptionGroup point awarded, though "often ... when multiple fail" understates it (TaskGroup always raises an ExceptionGroup) |
| python-async-blocking-03 | async | 3 | 2 | 2 | 1.00 |  |
| python-async-cancel-04 | async, errors-exceptions | 2 | 2 | 2 | 1.00 | names the `BaseException` catch as the swallowing risk |
| python-itergen-yield-01 | iterators-generators | 3 | 2 | 2 | 1.00 |  |
| python-itergen-genexpr-02 | iterators-generators | 2 | 2 | 2 | 1.00 |  |
| python-itergen-itertools-03 | iterators-generators | 1 | 2 | 2 | 1.00 | `chain`, `islice` |
| python-itergen-protocol-04 | iterators-generators, data-model | 2 | 2 | 2 | 1.00 | no `StopIteration` / `__iter__` returns self; core distinction and single-pass point present |
| python-context-with-01 | context-managers | 3 | 2 | 2 | 1.00 |  |
| python-context-contextmanager-02 | context-managers, decorators | 2 | 2 | 1 | 0.50 | setup/yield/teardown correct, but claims "teardown runs for both normal exit and exceptions" with no `try/finally`. Wrong: without `try/finally` the exception is thrown at the `yield` and code after it never runs. Point withheld for the wrong claim |
| python-context-exitstack-03 | context-managers | 1 | 2 | 2 | 1.00 |  |
| python-context-async-04 | context-managers, async | 2 | 2 | 2 | 1.00 |  |
| python-decorators-basics-01 | decorators | 3 | 2 | 2 | 1.00 |  |
| python-decorators-args-02 | decorators | 2 | 2 | 2 | 1.00 | factory model and "`@retry(times=3)` is evaluated before decoration" present; closure capture not named |
| python-decorators-stacking-03 | decorators | 2 | 2 | 2 | 1.00 |  |
| python-decorators-class-04 | decorators | 1 | 2 | 2 | 1.00 |  |
| python-datamodel-eqhash-01 | data-model | 3 | 2 | 1 | 0.50 | `__hash__ = None` point present. The invariant (a == b implies equal hashes; base both on the same fields) is never stated, only "must remain consistent". Too vague to award |
| python-datamodel-slots-02 | data-model | 2 | 2 | 2 | 1.00 |  |
| python-datamodel-mutable-03 | data-model | 3 | 2 | 2 | 1.00 |  |
| python-datamodel-is-04 | data-model | 2 | 2 | 2 | 1.00 |  |
| python-datamodel-descriptor-05 | data-model | 1 | 2 | 2 | 1.00 |  |
| python-errors-elsefinally-01 | errors-exceptions | 2 | 2 | 2 | 1.00 |  |
| python-errors-custom-02 | errors-exceptions | 2 | 2 | 2 | 1.00 |  |
| python-errors-raisefrom-03 | errors-exceptions | 2 | 2 | 2 | 1.00 |  |
| python-errors-group-04 | errors-exceptions | 2 | 2 | 2 | 1.00 | no 3.11 / multiple-clauses-fire detail; bundling and split-by-type present |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| async | 0.92 | 5 | ok | omit (strong) |
| context-managers | 0.88 | 4 | ok | omit (strong) |
| data-model | 0.88 | 6 | ok | omit (strong) |
| dataclasses | 1.00 | 4 | ok | omit (strong) |
| decorators | 0.90 | 5 | ok | omit (strong) |
| errors-exceptions | 1.00 | 5 | ok | omit (strong) |
| iterators-generators | 1.00 | 4 | ok | omit (strong) |
| types-hints | 1.00 | 4 | ok | omit (strong) |

Exact values: async 12/13 = 0.923; context-managers 7/8 = 0.875; data-model
11.5/13 = 0.885; decorators 9/10 = 0.900.

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 64.5 / 68 = 94.9%
```

## Derivation targets

Tags below threshold (`< 0.85`): **none**. No derived skill is warranted; no
`derived/python.cat-jam-free.SKILL.md` was created.

Near-misses, for a later re-benchmark: `context-managers` (0.875) and `data-model`
(0.885) each sit one withheld point above the threshold. The `@contextmanager`
teardown error is a false claim (it says teardown runs on exception without
`try/finally`), not just a missing detail. One more miss of that kind would put the
tag below threshold.
