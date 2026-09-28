---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: python
stack_corpus_rev: 1
threshold: 0.75
---

<!-- Filename: model_id "longcat-2.5-preview-free" has no "/" — no flattening needed. -->

# Scorecard — longcat-2.5-preview-free on python

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump
> invalidates this scorecard — re-benchmark.
>
> Graded against `questions.yaml` (corpus_rev 1). Answers were produced
> **closed-book** via `opencode-go/longcat-2.5-preview-free`, fed only
> `_prompts/python.md`, no repo access (session audited: 1 user + 1 assistant
> turn, zero tool calls).
>
> **Contamination check: clean.** No answer is a near-verbatim match to the key.
> The answers are terse and use their own examples (`def f(x: T) -> T`,
> `copy(self) -> Self`, `add(item, target=None)`, `aiohttp`, `islice(gen, 10, 20)`)
> where the key uses `first`/`Box`. They also
> carry errors a key-reader would not make: q5 claims `@dataclass` generates
> `__hash__` "by default" (default `eq=True` sets it to `None`) and says a `= []`
> default "would share the same list" (dataclass actually raises `ValueError`);
> q29 says "`property(obj)` calls the getter" (nonsense phrasing). They omit key
> material: `from None` (q32), `__exit__` returning True suppresses (q17),
> frozen → hashable (q6), the `try/finally` around `yield` (q18). Consistent with
> a blind run of a strong model.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| python-types-union-01 | types-hints | 2 | 2 | 2 | 1.00 | `Union[int, None]`, `int \| None` PEP 604 (3.10) — full (does not call out the "optional arg" misreading, not required) |
| python-types-generics-02 | types-hints | 2 | 2 | 2 | 1.00 | TypeVar identity links in/out; 3.12 PEP 695 `def f[T]` — full |
| python-types-protocol-03 | types-hints | 2 | 2 | 2 | 1.00 | structural without inheriting vs ABC nominal (inherit or register); `@runtime_checkable` — full |
| python-types-self-04 | types-hints | 1 | 2 | 2 | 1.00 | `typing.Self` (3.11); pre-3.11 bound `TypeVar` pattern, handles subclassing — full |
| python-dataclasses-basics-01 | dataclasses | 2 | 2 | 2 | 1.00 | `__init__/__repr__/__eq__`; default evaluated once → shared list, factory per instance — full. Inaccuracies (not rubric-bearing): "(by default) `__hash__`" is wrong (eq=True sets `__hash__=None`); never says dataclass raises `ValueError` on a list default |
| python-dataclasses-frozen-02 | dataclasses | 2 | 2 | 2 | 1.00 | `FrozenInstanceError` on assignment; `__post_init__` after `__init__` for validation/derived — full (omits "becomes hashable") |
| python-dataclasses-slots-03 | dataclasses | 1 | 2 | 2 | 1.00 | no per-instance `__dict__`, memory/speed; no dynamic attrs, weakref/multiple-inheritance caveats — full |
| python-dataclasses-vs-04 | dataclasses | 2 | 3 | 3 | 1.00 | dataclass general mutable record; NamedTuple immutable indexable/unpackable tuple; TypedDict typed dict keys — full |
| python-async-await-01 | async | 3 | 2 | 2 | 1.00 | returns coroutine object, body not run; runs when awaited/create_task/gather on the loop — full |
| python-async-taskgroup-02 | async | 3 | 3 | 3 | 1.00 | gather siblings keep running; TaskGroup (3.11) cancels remaining; ExceptionGroup — full |
| python-async-blocking-03 | async | 3 | 2 | 2 | 1.00 | freezes whole loop thread; `asyncio.sleep`, `to_thread`, aiohttp — full |
| python-async-cancel-04 | async, errors-exceptions | 2 | 2 | 2 | 1.00 | CancelledError thrown at current await; catch for cleanup but re-raise, swallowing makes task un-cancellable — full (does not note it is a BaseException) |
| python-itergen-yield-01 | iterators-generators | 3 | 2 | 2 | 1.00 | generator object, pauses at yield keeping local state; lazy vs storing the whole sequence — full |
| python-itergen-genexpr-02 | iterators-generators | 2 | 2 | 2 | 1.00 | eager list vs lazy iterator; large data / single pass / chaining — full |
| python-itergen-itertools-03 | iterators-generators | 1 | 2 | 2 | 1.00 | `chain`, `islice`; lazy, C-implemented, composable — full |
| python-itergen-protocol-04 | iterators-generators, data-model | 2 | 2 | 2 | 1.00 | `__iter__` vs `__iter__`(self)+`__next__`+StopIteration; iterator exhausted after one pass vs fresh iterator — full |
| python-context-with-01 | context-managers | 3 | 2 | 2 | 1.00 | `__enter__` bound to as-target, `__exit__` always runs incl. exception; exc info to `__exit__`, cleanup guarantee — full (omits suppress-by-returning-True) |
| python-context-contextmanager-02 | context-managers, decorators | 2 | 2 | 1 | 0.50 | setup before yield / teardown after / yielded value is as-target (1pt). MISSED: notes the exception is thrown into the generator at the yield but never says to wrap `yield` in `try/finally`, so after-yield teardown is skipped on error. Matches the `partial` |
| python-context-exitstack-03 | context-managers | 1 | 2 | 2 | 1.00 | dynamic/unknown count; `enter_context` in a loop, reverse-order unwind — full |
| python-context-async-04 | context-managers, async | 2 | 2 | 2 | 1.00 | awaits `__aenter__/__aexit__`; sync `__enter__/__exit__` cannot await async pool acquire — full |
| python-decorators-basics-01 | decorators | 3 | 2 | 2 | 1.00 | callable takes function, returns replacement wrapper; `wraps` copies `__name__/__doc__/__module__` — full (no `f = deco(f)` desugar) |
| python-decorators-args-02 | decorators | 2 | 2 | 2 | 1.00 | three levels, outer returns the real decorator; closure captures args because `@` passes one argument — full |
| python-decorators-stacking-03 | decorators | 2 | 2 | 2 | 1.00 | applied bottom-up (b then a); call-time a pre → b pre → f → b post → a post — full |
| python-decorators-class-04 | decorators | 1 | 2 | 2 | 1.00 | takes class, returns modified/replacement; `@total_ordering`, `@dataclass` and what they inject — full |
| python-datamodel-eqhash-01 | data-model | 3 | 2 | 2 | 1.00 | `__eq__` sets `__hash__=None` → unhashable; a==b ⇒ equal hashes, else set/dict lookups fail — full |
| python-datamodel-slots-02 | data-model | 2 | 2 | 2 | 1.00 | fixed layout vs per-instance `__dict__`, memory/speed; no new attributes, weakref caveat — full |
| python-datamodel-mutable-03 | data-model | 3 | 2 | 2 | 1.00 | evaluated once at def, shared across calls; `None` sentinel + fresh list — full |
| python-datamodel-is-04 | data-model | 2 | 2 | 2 | 1.00 | identity vs `__eq__` value; small-int/interning makes `is` unreliable for values — full (does not state the `is None`/singleton rule explicitly) |
| python-datamodel-descriptor-05 | data-model | 1 | 2 | 2 | 1.00 | `__get__/__set__/__delete__` controls attribute access; property data descriptor, functions bind via `__get__` — full (odd "`property(obj)` calls the getter" phrasing, not rubric-bearing) |
| python-errors-elsefinally-01 | errors-exceptions | 2 | 2 | 2 | 1.00 | else only on no exception, finally always; else keeps unrelated code out of the protected try — full |
| python-errors-custom-02 | errors-exceptions | 2 | 2 | 2 | 1.00 | subclass Exception / specific built-in; bare except catches SystemExit/KeyboardInterrupt, hides bugs — full |
| python-errors-raisefrom-03 | errors-exceptions | 2 | 2 | 2 | 1.00 | `from err` → `__cause__` "direct cause"; plain raise → implicit `__context__` "during handling" — full (omits `from None`) |
| python-errors-group-04 | errors-exceptions | 2 | 2 | 2 | 1.00 | ExceptionGroup (3.11) for TaskGroup concurrent failures; `except*` catches specific types within the group separately — full |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| types-hints | 1.00 | 4 | ok | omit (strong) |
| dataclasses | 1.00 | 4 | ok | omit (strong) |
| async | 1.00 | 5 | ok | omit (strong) |
| iterators-generators | 1.00 | 4 | ok | omit (strong) |
| context-managers | 0.875 | 4 | ok | omit (strong) |
| decorators | 0.90 | 5 | ok | omit (strong) |
| data-model | 1.00 | 6 | ok | omit (strong) |
| errors-exceptions | 1.00 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Notes: the only lost credit is `python-context-contextmanager-02` (weight 2,
normalized 0.50), dual-tagged `context-managers` + `decorators`: it pulls
`context-managers` to 7/8 and `decorators` to 9/10. Other dual-tagged
questions (`async-cancel-04`, `itergen-protocol-04`, `context-async-04`)
scored full.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 67 / 68 = 98.5%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none**. The lowest tag is `context-managers`
at 0.875.
**No derivation.** No `derived/python.longcat-2.5-preview-free.SKILL.md` is
produced.
