---
model_id: tencent/hy3
model_version: "3.0"
evaluated_via: novita
evaluated_on: 2026-07-11
stack: python
stack_corpus_rev: 1
threshold: 0.75
---

<!-- Filename: model_id "tencent/hy3" flattened → tencent__hy3.md -->

# Scorecard — tencent/hy3 on python

> Valid ONLY for `tencent/hy3` @ `3.0`. A version bump invalidates this
> scorecard — re-benchmark.
>
> Graded by an independent grader (Opus 4.8), strict, against
> `questions.yaml` (corpus_rev 1). Answers were produced **closed-book**
> (`ocode run -dir <empty>`, no corpus access).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| python-types-union-01 | types-hints | 2 | 2 | 2 | 1.00 | Optional==Union[int,None]; int\|None PEP 604 (3.10) both correct |
| python-types-generics-02 | types-hints | 2 | 2 | 2 | 1.00 | TypeVar link + 3.12/PEP 695 `def id[T]` + `type` statement, correct |
| python-types-protocol-03 | types-hints | 2 | 2 | 2 | 1.00 | structural vs nominal, runtime_checkable caveat — full |
| python-types-self-04 | types-hints | 1 | 2 | 2 | 1.00 | `Self` (3.11) + pre-3.11 bound TypeVar workaround — full |
| python-dataclasses-basics-01 | dataclasses | 2 | 2 | 2 | 1.00 | generated dunders + mutable-default/per-instance factory correct |
| python-dataclasses-frozen-02 | dataclasses | 2 | 2 | 1 | 0.50 | `__post_init__` correct (1pt). Point1 DOCKED: bundles "immutable ... becomes hashable" — answer states frozen "disables `__hash__` generation," which is backwards. frozen=True + default eq=True actually GENERATES `__hash__` (instance is hashable). Confidently-wrong on the tested concept (esp. since eqhash-01 gets the `__eq__`→`__hash__=None` rule right) |
| python-dataclasses-slots-03 | dataclasses | 1 | 2 | 2 | 1.00 | slots=True (3.10), memory/speed + no-new-attr cost — full |
| python-dataclasses-vs-04 | dataclasses | 2 | 3 | 3 | 1.00 | dataclass/NamedTuple/TypedDict distinction all three correct |
| python-async-await-01 | async | 3 | 2 | 2 | 1.00 | coroutine object, loop-driven, never-awaited warning — full |
| python-async-taskgroup-02 | async | 3 | 3 | 3 | 1.00 | gather siblings keep running; TaskGroup(3.11) cancels + ExceptionGroup — full |
| python-async-blocking-03 | async | 3 | 2 | 2 | 1.00 | blocks whole loop + async equivalents; did NOT mention `asyncio.to_thread`/executor for genuinely blocking/CPU work (point satisfied by async-equivalent branch) |
| python-async-cancel-04 | async, errors-exceptions | 2 | 2 | 2 | 1.00 | cooperative at await, must re-raise, BaseException (3.8) — full |
| python-itergen-yield-01 | iterators-generators | 3 | 2 | 2 | 1.00 | yield→generator, suspend/resume, lazy O(1) vs list — full |
| python-itergen-genexpr-02 | iterators-generators | 2 | 2 | 2 | 1.00 | list-comp eager vs genexpr lazy; sum() one-pass — full |
| python-itergen-itertools-03 | iterators-generators | 1 | 2 | 2 | 1.00 | chain+islice explained; lazy/C/composable rationale — full |
| python-itergen-protocol-04 | iterators-generators, data-model | 2 | 2 | 2 | 1.00 | iterable `__iter__` vs iterator `__next__`+`__iter__`→self; single-pass — full |
| python-context-with-01 | context-managers | 3 | 2 | 2 | 1.00 | `__enter__`/`__exit__`, always-runs guarantee, suppress via True — full |
| python-context-contextmanager-02 | context-managers, decorators | 2 | 2 | 1 | 0.50 | setup/teardown/yield placement correct (1pt). MISSED: falsely claims "the decorator internally wraps everything in a try/finally" so teardown runs on exception. Wrong — `@contextmanager` throws the exception INTO the generator at the yield; without a user `try/finally` the after-yield teardown is SKIPPED. This is the exact misconception the point tests |
| python-context-exitstack-03 | context-managers | 1 | 2 | 2 | 1.00 | dynamic count + reverse unwind + callback() — full |
| python-context-async-04 | context-managers, async | 2 | 2 | 2 | 1.00 | `__aenter__`/`__aexit__` await; sync with can't await — full |
| python-decorators-basics-01 | decorators | 3 | 2 | 2 | 1.00 | callable→replacement, `f=deco(f)`, functools.wraps metadata — full |
| python-decorators-args-02 | decorators | 2 | 2 | 2 | 1.00 | factory returns real decorator, 3 levels, args via nesting — full |
| python-decorators-stacking-03 | decorators | 2 | 2 | 2 | 1.00 | apply bottom-up `a(b(f))`, call-time outermost-first — full |
| python-decorators-class-04 | decorators | 1 | 2 | 2 | 1.00 | class decorator + @dataclass/@total_ordering/@enum.unique — full |
| python-datamodel-eqhash-01 | data-model | 3 | 2 | 2 | 1.00 | `__eq__`→`__hash__=None` unhashable; equal⇒equal-hash invariant — full |
| python-datamodel-slots-02 | data-model | 2 | 2 | 2 | 1.00 | fixed layout vs `__dict__`; AttributeError, MI caveats — full |
| python-datamodel-mutable-03 | data-model | 3 | 2 | 2 | 1.00 | default eval'd once at def time/shared; None-sentinel fix — full |
| python-datamodel-is-04 | data-model | 2 | 2 | 2 | 1.00 | identity vs value; small-int/string interning impl detail — full |
| python-datamodel-descriptor-05 | data-model | 1 | 2 | 2 | 1.00 | `__get__/__set__/__delete__`; property + method binding — full |
| python-errors-elsefinally-01 | errors-exceptions | 2 | 2 | 2 | 1.00 | else no-exception, finally always; else narrows try — full |
| python-errors-custom-02 | errors-exceptions | 2 | 2 | 2 | 1.00 | subclass Exception; bare except swallows KI/SystemExit — full |
| python-errors-raisefrom-03 | errors-exceptions | 2 | 2 | 2 | 1.00 | `from err`→`__cause__`; implicit `__context__`; `from None` — full |
| python-errors-group-04 | errors-exceptions | 2 | 2 | 2 | 1.00 | ExceptionGroup (3.11/PEP 654) + except* splits group — full |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| types-hints | 1.00 | 4 | ok | omit (strong) |
| dataclasses | 0.857 | 4 | ok | omit (strong) |
| async | 1.00 | 5 | ok | omit (strong) |
| iterators-generators | 1.00 | 4 | ok | omit (strong) |
| context-managers | 0.875 | 4 | ok | omit (strong) |
| decorators | 0.90 | 5 | ok | omit (strong) |
| data-model | 1.00 | 6 | ok | omit (strong) |
| errors-exceptions | 1.00 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Notes: `python-context-contextmanager-02` (normalized 0.50) is dual-tagged
`context-managers` + `decorators`, so it pulls both tags down slightly.
`python-dataclasses-frozen-02` (normalized 0.50) pulls `dataclasses` to 0.857.
No tag falls below the 0.75 threshold.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 66 / 68 = 97.1%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none** — every tag scores ≥ 0.857.
**No derivation.** No `derived/python.tencent__hy3.SKILL.md` is produced;
tencent/hy3 already answers the entire Python corpus strongly closed-book.
