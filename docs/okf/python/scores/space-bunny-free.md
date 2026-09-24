---
model_id: space-bunny-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-09-24
stack: python
stack_corpus_rev: 1
threshold: 0.75
---

<!-- Filename: model_id "space-bunny-free" has no "/" — no flattening needed. -->

# Scorecard — space-bunny-free on python

> Valid ONLY for `space-bunny-free` @ `alpha`. A version bump invalidates this
> scorecard — re-benchmark.
>
> Graded against `questions.yaml` (corpus_rev 1). Answers were produced
> **closed-book** via `opencode-go/space-bunny-free`, fed only
> `_prompts/python.md`, no repo access.
>
> **Contamination check:** the near-ceiling score is not a copy of the answer
> key. The answers use their own examples throughout (`Builder.reset`,
> `Inventory`, `PaymentError`, `Version` + `@total_ordering` where the key uses
> `first`/`Box`, `ConfigError`/`AppError`, `@dataclass`), and they carry material
> the key does not: `__wrapped__` on `functools.wraps` (q21), `send()` driving a
> generator (q13), `BaseExceptionGroup` and the "cannot mix `except*` with plain
> `except`" rule (q33), `__weakref__` in `__slots__` (q7, q26),
> `typing_extensions.Self` (q4), "TaskGroup does not directly return results"
> (q10). They also make omissions and one odd claim a key-reader would not:
> q1 never says Optional is *not* "optional argument"; q4 never mentions the
> bound-`TypeVar` workaround; q21 never gives the `f = deco(f)` desugar; and q17
> asserts `__exit__` suppresses "from a non-`async` context manager" — an
> inaccurate distinction (`__aexit__` suppresses the same way) absent from the
> key. This is a blind run of a strong model.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| python-types-union-01 | types-hints | 2 | 2 | 2 | 1.00 | int-or-None + `int \| None` PEP 604 — full (does not call out the "optional arg" misreading, not required) |
| python-types-generics-02 | types-hints | 2 | 2 | 2 | 1.00 | TypeVar identity example links in/out; 3.12 PEP 695 `def identity[T]` — full |
| python-types-protocol-03 | types-hints | 2 | 2 | 2 | 1.00 | structural without inheriting vs ABC nominal must-inherit — full |
| python-types-self-04 | types-hints | 1 | 2 | 1 | 0.50 | `Self` (3.11) correct (1pt). MISSED: never contrasts with a hardcoded `-> Builder` widening to the base, and gives `typing_extensions.Self` (a backport) rather than the pre-3.11 bound-`TypeVar` pattern |
| python-dataclasses-basics-01 | dataclasses | 2 | 2 | 2 | 1.00 | `__init__`/`__repr__`/eq generated; shared-list leakage, dataclass rejects mutable default, factory per instance — full |
| python-dataclasses-frozen-02 | dataclasses | 2 | 2 | 2 | 1.00 | immutable + auto `__hash__`; `__post_init__` after `__init__` for validation/derived, `object.__setattr__` — full |
| python-dataclasses-slots-03 | dataclasses | 1 | 2 | 2 | 1.00 | no `__dict__`, memory/speed; no arbitrary attrs, weakref/class-var caveats — full (no 3.10 version note, not required) |
| python-dataclasses-vs-04 | dataclasses | 2 | 3 | 3 | 1.00 | all three distinguished; TypedDict stays a plain dict at runtime — full |
| python-async-await-01 | async | 3 | 2 | 2 | 1.00 | coroutine object, body not run; runs when awaited/scheduled on loop; never-awaited warning — full (does not state that `await` yields control) |
| python-async-taskgroup-02 | async | 3 | 3 | 3 | 1.00 | gather siblings not cancelled; TaskGroup cancels remaining; ExceptionGroup — full |
| python-async-blocking-03 | async | 3 | 2 | 2 | 1.00 | blocks the loop thread for all coroutines/timers; `asyncio.sleep`, httpx/aiohttp, `to_thread` — full |
| python-async-cancel-04 | async, errors-exceptions | 2 | 2 | 2 | 1.00 | CancelledError at next await, cooperative; re-raise after cleanup, BaseException since 3.8 — full |
| python-itergen-yield-01 | iterators-generators | 3 | 2 | 2 | 1.00 | yield → generator object, pauses at each yield retaining state; list materializes with memory ∝ result — full |
| python-itergen-genexpr-02 | iterators-generators | 2 | 2 | 2 | 1.00 | eager list vs lazy iterator; large/unbounded streams; one-shot — full |
| python-itergen-itertools-03 | iterators-generators | 1 | 2 | 2 | 1.00 | `islice` + `chain.from_iterable`, lazy, no intermediate collections — full |
| python-itergen-protocol-04 | iterators-generators, data-model | 2 | 2 | 2 | 1.00 | `__iter__` fresh iterator vs `__next__`+`__iter__` self, StopIteration; one-shot vs re-iterable — full |
| python-context-with-01 | context-managers | 3 | 2 | 2 | 1.00 | `__enter__`/`__exit__`, always runs on exception, suppress by returning true — full (odd "non-async" qualifier, does not affect the points) |
| python-context-contextmanager-02 | context-managers, decorators | 2 | 2 | 2 | 1.00 | setup before yield, teardown after, `as` target; explicit `try/finally` with exception thrown in at yield — full |
| python-context-exitstack-03 | context-managers | 1 | 2 | 2 | 1.00 | dynamic/conditional count; `enter_context` in loop, reverse-order exit — full |
| python-context-async-04 | context-managers, async | 2 | 2 | 2 | 1.00 | awaits `__aenter__`/`__aexit__`; sync protocol cannot await — full |
| python-decorators-basics-01 | decorators | 3 | 2 | 2 | 1.00 | callable takes function, returns replacement wrapper; `wraps` copies name/doc/annotations, sets `__wrapped__` — full (no `f = deco(f)` desugar) |
| python-decorators-args-02 | decorators | 2 | 2 | 2 | 1.00 | `retry(times=3)` evaluated first to return the real decorator; `retry(times=3)(function)` — full |
| python-decorators-stacking-03 | decorators | 2 | 2 | 2 | 1.00 | bottom-up `a(b(f))`; call-time a → b → f, unwinds reverse — full |
| python-decorators-class-04 | decorators | 1 | 2 | 2 | 1.00 | takes class, returns same/replacement; `@total_ordering` fills in comparison methods — full |
| python-datamodel-eqhash-01 | data-model | 3 | 2 | 2 | 1.00 | `__hash__` → None, unhashable; equal ⇒ same hash, mutable value-equal objects unsafe to hash — full |
| python-datamodel-slots-02 | data-model | 2 | 2 | 2 | 1.00 | fixed descriptor slots vs per-instance dict, memory/speed; no undeclared attrs, weakref/subclass caveats — full |
| python-datamodel-mutable-03 | data-model | 3 | 2 | 2 | 1.00 | default evaluated once at def, shared; None sentinel with fresh list — full |
| python-datamodel-is-04 | data-model | 2 | 2 | 2 | 1.00 | identity vs `__eq__` value; interning is implementation choice, `is` only for None/singletons — full |
| python-datamodel-descriptor-05 | data-model | 1 | 2 | 2 | 1.00 | `__get__`/`__set__`/`__delete__`; property is data descriptor, functions bind via `__get__`; data-descriptor precedence — full |
| python-errors-elsefinally-01 | errors-exceptions | 2 | 2 | 2 | 1.00 | else only on no exception; finally always incl. return/break; else narrows the try — full |
| python-errors-custom-02 | errors-exceptions | 2 | 2 | 2 | 1.00 | subclass Exception; bare except catches KeyboardInterrupt/SystemExit/CancelledError, catch narrow — full |
| python-errors-raisefrom-03 | errors-exceptions | 2 | 2 | 2 | 1.00 | implicit `__context__` vs explicit `__cause__` "direct cause"; `from None` suppresses — full |
| python-errors-group-04 | errors-exceptions | 2 | 2 | 2 | 1.00 | 3.11 ExceptionGroup for TaskGroup; `except*` matches by type, unmatched regrouped and re-raised; plain except cannot unpack — full |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| types-hints | 0.929 | 4 | ok | omit (strong) |
| dataclasses | 1.00 | 4 | ok | omit (strong) |
| async | 1.00 | 5 | ok | omit (strong) |
| iterators-generators | 1.00 | 4 | ok | omit (strong) |
| context-managers | 1.00 | 4 | ok | omit (strong) |
| decorators | 1.00 | 5 | ok | omit (strong) |
| data-model | 1.00 | 6 | ok | omit (strong) |
| errors-exceptions | 1.00 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Notes: the only lost credit is `python-types-self-04` (weight 1, normalized
0.50), which pulls `types-hints` to 6.5/7. Dual-tagged questions
(`async-cancel-04`, `itergen-protocol-04`, `context-contextmanager-02`,
`context-async-04`) all scored full, so no tag is pulled down by an
off-tag miss.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 67.5 / 68 = 99.3%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none** — every tag scores ≥ 0.929.
**No derivation.** No `derived/python.space-bunny-free.SKILL.md` is produced;
space-bunny-free answers the entire Python corpus strongly closed-book.
