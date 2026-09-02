---
model_id: glm-5.3-flash
model_version: "5.3"
evaluated_via: aihubmix
evaluated_on: 2026-09-01
stack: python
stack_corpus_rev: 1
threshold: 0.75
---

<!-- Filename: model_id "glm-5.3-flash" has no "/" — no flattening needed. -->

# Scorecard — glm-5.3-flash on python

> Valid ONLY for `glm-5.3-flash` @ `5.3`. A version bump invalidates this
> scorecard — re-benchmark.
>
> Graded against `questions.yaml` (corpus_rev 1). Answers were produced
> **closed-book** via `ocode run -model aihubmix/glm-5.3-flash`, fed only
> `_prompts/python.md` (id + question, no answers), per Rule 0 in
> `HOW-TO-EVALUATE.md`.
>
> **Contamination check:** a 100% sweep is the standing red flag for an
> open-book run, so this was checked explicitly. The answers are NOT a copy
> of the key: they are 2–4× longer, use independent wording throughout, and
> contain material absent from `questions.yaml` (`InitVar` /
> `object.__setattr__` in frozen `__post_init__`, `stack.callback` /
> `pop_all()` on `ExitStack`, `asyncio.to_thread`, `__suppress_context__`,
> `member_descriptor`, `@typing.runtime_checkable`, legacy `__getitem__`
> iteration, the `functools.cached_property`-needs-`__dict__` caveat). More
> tellingly, they contain **errors the key does not make**: q5 says
> `@dataclass` rejects a mutable default with `TypeError` (it is
> `ValueError`); q10 claims `TaskGroup` "unwraps automatically if exactly
> one" exception (it always raises an `ExceptionGroup`); q22's
> decorator-factory snippet has a stray duplicated `return decorator` at the
> wrong indent; q9 says `await coro` "submits it to the event loop" (await
> drives the coroutine directly). None of these flaws cost rubric points —
> the tested concepts are all present — but a model reading the key would
> have reproduced the key's correct phrasing, not invented these. Consistent
> with the sibling `mimo-v2.5` blind run (98.5%): this fundamentals corpus
> sits near the ceiling for current strong models.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| python-types-union-01 | types-hints | 2 | 2 | 2 | 1.00 | Union[int,None] not "optional arg"; PEP 604 `int \| None` (3.10) — full |
| python-types-generics-02 | types-hints | 2 | 2 | 2 | 1.00 | TypeVar same-T in/out; PEP 695 `def first[T]` / `class Box[T]`, scoped, no import — full |
| python-types-protocol-03 | types-hints | 2 | 2 | 2 | 1.00 | structural/static duck typing vs nominal must-subclass; extra `@runtime_checkable` caveat correct — full |
| python-types-self-04 | types-hints | 1 | 2 | 2 | 1.00 | `Self` (PEP 673, 3.11); `-> "Animal"` loses `Cat`; pre-3.11 bound TypeVar — full |
| python-dataclasses-basics-01 | dataclasses | 2 | 2 | 2 | 1.00 | `__init__/__repr__/__eq__`; shared-mutable rationale + per-instance `default_factory`; says dataclass raises — full. Inaccuracy: names `TypeError` (actual: `ValueError`), not rubric-bearing |
| python-dataclasses-frozen-02 | dataclasses | 2 | 2 | 2 | 1.00 | `FrozenInstanceError`, hashable when eq+frozen; `__post_init__` after `__init__` for validation/derived (+`object.__setattr__` trick) — full |
| python-dataclasses-slots-03 | dataclasses | 1 | 2 | 2 | 1.00 | slots=True (3.10) → no `__dict__`, memory/speed; no undeclared attrs, weakref/MI/inheritance caveats — full |
| python-dataclasses-vs-04 | dataclasses | 2 | 3 | 3 | 1.00 | dataclass mutable record w/ behavior; NamedTuple immutable tuple; TypedDict types a dict shape, zero runtime — full |
| python-async-await-01 | async | 3 | 2 | 2 | 1.00 | coroutine object, inert; runs only when awaited/`create_task`; "never awaited" warning — full. Wording "await submits to the loop" imprecise |
| python-async-taskgroup-02 | async | 3 | 3 | 3 | 1.00 | gather: first error propagates, siblings keep running; TaskGroup cancels rest; `ExceptionGroup` — full. Inaccuracy: "unwrapped if exactly one" is wrong (always a group) |
| python-async-blocking-03 | async | 3 | 2 | 2 | 1.00 | single thread, whole loop stalls; `asyncio.sleep`/httpx/aiohttp or `to_thread`/executor — full |
| python-async-cancel-04 | async, errors-exceptions | 2 | 2 | 2 | 1.00 | `CancelledError` at next await, cooperative; don't swallow, re-raise; not caught by `except Exception` (BaseException) — full |
| python-itergen-yield-01 | iterators-generators | 3 | 2 | 2 | 1.00 | generator object, run-to-yield/suspend with state; lazy O(1) memory vs eager list; single-pass — full |
| python-itergen-genexpr-02 | iterators-generators | 2 | 2 | 2 | 1.00 | eager list vs lazy genexpr; large/infinite, one-pass consumers (`sum`/`any`), short-circuit — full |
| python-itergen-itertools-03 | iterators-generators | 1 | 2 | 2 | 1.00 | `islice`, `groupby`, `chain`, `combinations`…; lazy/constant-memory/C-level/composable — full |
| python-itergen-protocol-04 | iterators-generators, data-model | 2 | 2 | 2 | 1.00 | `__iter__`→iterator vs `__next__`+StopIteration+`__iter__` self; single-pass vs fresh-iterator factory — full |
| python-context-with-01 | context-managers | 3 | 2 | 2 | 1.00 | `__enter__` bound to as-target, `__exit__` always runs; exc info triple, truthy return suppresses — full |
| python-context-contextmanager-02 | context-managers, decorators | 2 | 2 | 2 | 1.00 | pre-yield setup / post-yield teardown / yielded value = as-target; explicit `try/finally` around `yield` — full |
| python-context-exitstack-03 | context-managers | 1 | 2 | 2 | 1.00 | dynamic/conditional/loop-determined set; runtime-built cleanup stack unwinds all on exit (+`callback`, `pop_all`) — full. `enter_context` not named by name |
| python-context-async-04 | context-managers, async | 2 | 2 | 2 | 1.00 | awaits `__aenter__`/`__aexit__`; sync `__enter__`/`__exit__` cannot suspend — full |
| python-decorators-basics-01 | decorators | 3 | 2 | 2 | 1.00 | callable→replacement, `f = dec(f)`; `wraps` copies `__name__`/`__doc__`/…, sets `__wrapped__` — full |
| python-decorators-args-02 | decorators | 2 | 2 | 2 | 1.00 | `retry(times=3)(f)`, factory returns real decorator, 3 levels, closure captures args — full. Snippet has a stray duplicated `return decorator` |
| python-decorators-stacking-03 | decorators | 2 | 2 | 2 | 1.00 | apply bottom-up `a(b(f))`; call-time `a` outermost first — full |
| python-decorators-class-04 | decorators | 1 | 2 | 2 | 1.00 | takes class, returns/modifies it; `@dataclass`, `@total_ordering`, `@runtime_checkable` with what each injects — full |
| python-datamodel-eqhash-01 | data-model | 3 | 2 | 2 | 1.00 | `__eq__` → `__hash__ = None`, unhashable; `a==b ⇒ hash(a)==hash(b)` invariant, container breakage — full |
| python-datamodel-slots-02 | data-model | 2 | 2 | 2 | 1.00 | fixed slot descriptors instead of `__dict__`, memory/speed; `AttributeError` on new attrs, weakref/MI/subclass caveats — full |
| python-datamodel-mutable-03 | data-model | 3 | 2 | 2 | 1.00 | evaluated once at def time, shared; `None` sentinel + create inside — full |
| python-datamodel-is-04 | data-model | 2 | 2 | 2 | 1.00 | identity vs `__eq__` value; −5..256 cache / interning is CPython detail, `is` only for `None`/singletons — full |
| python-datamodel-descriptor-05 | data-model | 1 | 2 | 2 | 1.00 | `__get__`/`__set__`/`__delete__` on class attr, data vs non-data precedence; property is a descriptor, functions bind `self` via `__get__` — full |
| python-errors-elsefinally-01 | errors-exceptions | 2 | 2 | 2 | 1.00 | `else` only on no-exception, `finally` always (incl. return/break); `else` keeps `try` minimal so follow-up errors aren't caught — full |
| python-errors-custom-02 | errors-exceptions | 2 | 2 | 2 | 1.00 | subclass `Exception` (+ app base); bare except swallows KeyboardInterrupt/SystemExit and hides bugs — full |
| python-errors-raisefrom-03 | errors-exceptions | 2 | 2 | 2 | 1.00 | `from err` → `__cause__` "direct cause"; plain raise → implicit `__context__` "during handling"; `from None` suppresses — full |
| python-errors-group-04 | errors-exceptions | 2 | 2 | 2 | 1.00 | `ExceptionGroup` (3.11/PEP 654) for concurrent failures/TaskGroup; `except*` splits by type, multiple clauses fire, unmatched re-raised — full |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| types-hints | 1.00 | 4 | ok | omit (strong) |
| dataclasses | 1.00 | 4 | ok | omit (strong) |
| async | 1.00 | 5 | ok | omit (strong) |
| iterators-generators | 1.00 | 4 | ok | omit (strong) |
| context-managers | 1.00 | 4 | ok | omit (strong) |
| decorators | 1.00 | 5 | ok | omit (strong) |
| data-model | 1.00 | 6 | ok | omit (strong) |
| errors-exceptions | 1.00 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.
Dual-tagged questions (`async-cancel-04`, `itergen-protocol-04`,
`context-contextmanager-02`, `context-async-04`) count toward both tags.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 68 / 68 = 100%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none** — every tag scores 1.00.
**No derivation.** No `derived/python.glm-5.3-flash.SKILL.md` is produced;
glm-5.3-flash answers the entire Python corpus closed-book at ceiling. The
factual slips noted above (`TypeError` vs `ValueError` for dataclass mutable
defaults; `TaskGroup` never unwraps a single exception) are below rubric
granularity and did not move any tag toward threshold — recorded here only
so a future corpus revision can decide whether to test them.
