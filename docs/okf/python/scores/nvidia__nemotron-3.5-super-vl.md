---
model_id: nvidia/nemotron-3.5-super-vl
model_version: "3.5"
evaluated_via: openrouter
evaluated_on: 2026-10-16
stack: python
stack_corpus_rev: 1
threshold: 0.85
---

<!-- Filename: model_id "nvidia/nemotron-3.5-super-vl" flattened "/" → "__". -->

# Scorecard — nvidia/nemotron-3.5-super-vl on python

> Valid ONLY for `nvidia/nemotron-3.5-super-vl` @ `3.5`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book (Rule 0). The answerer was `ocode run -m openrouter/nvidia/nemotron-3.5-super-vl:free -effort med`
under an isolated HOME whose config denies every tool (no tools were advertised), in a
scratch cwd containing only the `_prompts/python.md` sheet, one fresh session; the session
sqlite shows exactly 1 user + 1 assistant message and 0 tool calls. All 33 ids came back
once, none empty. Headless runs inject the bundled skill-catalogue digest into the system
prompt; no answer cites a tool or skill the question never named. Answers file:
`../answers/nvidia__nemotron-3.5-super-vl.md`. The OpenRouter `:free` route variant is
provenance only and not part of the model id.

Grading note: every answer is a single compact paragraph with no thinking-aloud,
self-corrections, duplicated records or fabricated tool output. No answer reads as a copy
of the reference text — each is structured differently and several carry detail the
reference does not (`__wrapped__` and `__qualname__` under `functools.wraps`,
`GeneratorExit` under bare `except:`, `typing_extensions.Self`, `unsafe_hash`,
`itertools.tee`/`product`, `loop.run_in_executor`). Three factual slips were noted
below; one of them (the missing `try/finally` in `@contextmanager`) withholds a point.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| python-types-union-01 | types-hints | 2 | 2 | 2 | 1.00 | |
| python-types-generics-02 | types-hints | 2 | 2 | 2 | 1.00 | |
| python-types-protocol-03 | types-hints | 2 | 2 | 2 | 1.00 | |
| python-types-self-04 | types-hints | 1 | 2 | 2 | 1.00 | pre-3.11 bound `TypeVar` given; does not spell out that `-> Base` widens on a subclass |
| python-dataclasses-basics-01 | dataclasses | 2 | 2 | 2 | 1.00 | per-instance factory vs shared default is right; omits that `@dataclass` rejects `= []` with `ValueError` (describes the leak as if it were allowed) |
| python-dataclasses-frozen-02 | dataclasses | 2 | 2 | 2 | 1.00 | |
| python-dataclasses-slots-03 | dataclasses | 1 | 2 | 2 | 1.00 | |
| python-dataclasses-vs-04 | dataclasses | 2 | 3 | 3 | 1.00 | |
| python-async-await-01 | async | 3 | 2 | 2 | 1.00 | names `await`/`create_task`, not `asyncio.run`; no "never awaited" warning — not rubric-relevant |
| python-async-taskgroup-02 | async | 3 | 3 | 3 | 1.00 | |
| python-async-blocking-03 | async | 3 | 2 | 2 | 1.00 | |
| python-async-cancel-04 | async, errors-exceptions | 2 | 2 | 2 | 1.00 | "cancel() sets a flag" is loose (it schedules a throw into the task), but next-await/cooperative, re-raise and `BaseException` all present |
| python-itergen-yield-01 | iterators-generators | 3 | 2 | 2 | 1.00 | |
| python-itergen-genexpr-02 | iterators-generators | 2 | 2 | 2 | 1.00 | one-pass consumer case covered as "chaining without materializing intermediate lists"; no `sum(...)` example |
| python-itergen-itertools-03 | iterators-generators | 1 | 2 | 2 | 1.00 | |
| python-itergen-protocol-04 | iterators-generators, data-model | 2 | 2 | 2 | 1.00 | |
| python-context-with-01 | context-managers | 3 | 2 | 2 | 1.00 | |
| python-context-contextmanager-02 | context-managers, decorators | 2 | 2 | 1 | 0.50 | setup/yield/teardown point full. Second point withheld: never mentions wrapping the `yield` in `try/finally`, and states teardown "runs when the block exits" unconditionally; the follow-up "exception is re-raised at the yield so the generator can catch it or run cleanup" shows the mechanism but not the required pattern (judgment call) |
| python-context-exitstack-03 | context-managers | 1 | 2 | 2 | 1.00 | enter-in-a-loop / conditional entry present; reverse-order unwinding not mentioned |
| python-context-async-04 | context-managers, async | 2 | 2 | 2 | 1.00 | |
| python-decorators-basics-01 | decorators | 3 | 2 | 2 | 1.00 | |
| python-decorators-args-02 | decorators | 2 | 2 | 2 | 1.00 | `retry(3)` returns the decorator which is then applied (= `retry(3)(f)`); does not name the three levels or closure capture explicitly |
| python-decorators-stacking-03 | decorators | 2 | 2 | 2 | 1.00 | |
| python-decorators-class-04 | decorators | 1 | 2 | 2 | 1.00 | |
| python-datamodel-eqhash-01 | data-model | 3 | 2 | 2 | 1.00 | |
| python-datamodel-slots-02 | data-model | 2 | 2 | 2 | 1.00 | |
| python-datamodel-mutable-03 | data-model | 3 | 2 | 2 | 1.00 | |
| python-datamodel-is-04 | data-model | 2 | 2 | 2 | 1.00 | |
| python-datamodel-descriptor-05 | data-model | 1 | 2 | 2 | 1.00 | |
| python-errors-elsefinally-01 | errors-exceptions | 2 | 2 | 2 | 1.00 | |
| python-errors-custom-02 | errors-exceptions | 2 | 2 | 2 | 1.00 | |
| python-errors-raisefrom-03 | errors-exceptions | 2 | 2 | 2 | 1.00 | `__cause__` explicit vs `__context__` implicit is right, so both points awarded; but it attributes the "During handling of the above exception..." message to `from err` (that is the `__context__` message; `from` prints "The above exception was the direct cause..."), and omits `from None` |
| python-errors-group-04 | errors-exceptions | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| async | 1.00 | 5 | ok | omit (strong) |
| context-managers | 0.88 | 4 | ok | omit (strong) — 7/8 |
| data-model | 1.00 | 6 | ok | omit (strong) |
| dataclasses | 1.00 | 4 | ok | omit (strong) |
| decorators | 0.90 | 5 | ok | omit (strong) — 9/10 |
| errors-exceptions | 1.00 | 5 | ok | omit (strong) |
| iterators-generators | 1.00 | 4 | ok | omit (strong) |
| types-hints | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

The only lost credit is `python-context-contextmanager-02` (weight 2, normalized 0.50),
which carries both `context-managers` and `decorators`; both tags stay above threshold.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 67 / 68 = 98.5%
```

## Derivation targets

Tags below threshold (`< 0.85`): **none**. No derived skill is warranted; no
`derived/python.nvidia__nemotron-3.5-super-vl.SKILL.md` was created.

Context: this corpus sits near its ceiling for current models (other python scorecards in
this directory range 97.1%–100%). The one weak spot worth remembering if a later corpus
revision probes it harder: the model describes `@contextlib.contextmanager` teardown as
unconditional and never says to put the `yield` inside `try/finally`.
