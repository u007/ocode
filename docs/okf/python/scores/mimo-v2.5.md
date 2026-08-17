---
model_id: mimo-v2.5
model_version: "2.5"
evaluated_via: opencode-go
evaluated_on: 2026-08-17
stack: python
stack_corpus_rev: 1
threshold: 0.75
---

<!-- Filename: model_id "mimo-v2.5" has no "/" — no flattening needed. -->

# Scorecard — mimo-v2.5 on python

> Valid ONLY for `mimo-v2.5` @ `2.5`. A version bump invalidates this
> scorecard — re-benchmark.
>
> Graded against `questions.yaml` (corpus_rev 1). Answers were produced
> **closed-book** via `ocode run -model opencode-go/mimo-v2.5`, isolated dir,
> no repo access, fed only `_prompts/python.md`.
>
> **Contamination check:** the near-ceiling score below is not a copy of the
> answer key — the answers contain fabrications and inaccuracies absent from
> `questions.yaml`: `__post_init__ ... (and __init_subclass__ for __replace__)`
> (q6, not a real relationship), a garbled `gather` caveat
> `unless return_exceptions=False and you don't shield` (q10, backwards —
> `return_exceptions=False` is the default), `itertools.chain.from_iterable`
> where the key uses plain `chain(a, b)` (q15), `GeneratorExit` added to the
> bare-except list (q31), `@functools.total_ordering` instead of `@dataclass`
> as the class-decorator example (q24). Most tellingly, q18's answer is
> actively **wrong** in a way the key specifically warns against: it claims
> `@contextmanager` teardown after `yield` "runs even if an exception occurs
> ... (similar to finally)" with no mention of wrapping in `try/finally` — the
> exact misconception the rubric's `partial` targets. A model that had read
> the key would not make that mistake. This is a blind run of a strong model.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| python-types-union-01 | types-hints | 2 | 2 | 2 | 1.00 | Optional==Union[int,None]; int\|None PEP 604 — full |
| python-types-generics-02 | types-hints | 2 | 2 | 2 | 1.00 | TypeVar link (via example) + 3.12 `def first[T]` inline syntax — full |
| python-types-protocol-03 | types-hints | 2 | 2 | 2 | 1.00 | structural/duck vs nominal (explicit subclassing) — full |
| python-types-self-04 | types-hints | 1 | 2 | 2 | 1.00 | `Self` (3.11) + pre-3.11 bound-TypeVar workaround — full |
| python-dataclasses-basics-01 | dataclasses | 2 | 2 | 2 | 1.00 | generated dunders + shared-mutable-default rationale for factory — full |
| python-dataclasses-frozen-02 | dataclasses | 2 | 2 | 2 | 1.00 | FrozenInstanceError + hashable; `__post_init__` timing/purpose — full |
| python-dataclasses-slots-03 | dataclasses | 1 | 2 | 2 | 1.00 | slots=True (3.10), memory/speed + no-new-attr cost — full |
| python-dataclasses-vs-04 | dataclasses | 2 | 3 | 3 | 1.00 | dataclass/NamedTuple/TypedDict distinction all three correct — full |
| python-async-await-01 | async | 3 | 2 | 2 | 1.00 | coroutine object, lazy, loop-driven (run/create_task) — full |
| python-async-taskgroup-02 | async | 3 | 3 | 3 | 1.00 | gather siblings keep running; TaskGroup(3.11) cancels + ExceptionGroup — full |
| python-async-blocking-03 | async | 3 | 2 | 2 | 1.00 | blocks whole loop + async equivalents (asyncio.sleep/httpx) — full |
| python-async-cancel-04 | async, errors-exceptions | 2 | 2 | 2 | 1.00 | CancelledError at next await; don't swallow, propagate/finally — full |
| python-itergen-yield-01 | iterators-generators | 3 | 2 | 2 | 1.00 | yield→generator, suspend/resume, lazy vs eager list — full |
| python-itergen-genexpr-02 | iterators-generators | 2 | 2 | 2 | 1.00 | list-comp eager vs genexpr lazy; sum()/tuple() one-pass case — full |
| python-itergen-itertools-03 | iterators-generators | 1 | 2 | 2 | 1.00 | chain.from_iterable + islice; lazy/C-level/no intermediate list — full |
| python-itergen-protocol-04 | iterators-generators, data-model | 2 | 2 | 2 | 1.00 | iterable `__iter__` vs iterator `__next__`+self; single-pass — full |
| python-context-with-01 | context-managers | 3 | 2 | 2 | 1.00 | `__enter__`/`__exit__`, always-runs guarantee, exc info to `__exit__` — full |
| python-context-contextmanager-02 | context-managers, decorators | 2 | 2 | 1 | 0.50 | setup/teardown/yield placement correct (1pt). MISSED: claims teardown after `yield` runs on exception "similar to finally" with no `try/finally` mentioned — wrong; without it, teardown is skipped. Matches the `partial` exactly |
| python-context-exitstack-03 | context-managers | 1 | 2 | 2 | 1.00 | dynamic/unknown count + `enter_context` in a loop, all cleaned up on close — full |
| python-context-async-04 | context-managers, async | 2 | 2 | 2 | 1.00 | `__aenter__`/`__aexit__` await; sync `with` can't await — full |
| python-decorators-basics-01 | decorators | 3 | 2 | 2 | 1.00 | callable→replacement, `f=deco(f)`, functools.wraps metadata — full |
| python-decorators-args-02 | decorators | 2 | 2 | 2 | 1.00 | factory returns real decorator; args captured, mechanism explained — full |
| python-decorators-stacking-03 | decorators | 2 | 2 | 2 | 1.00 | apply bottom-up `a(b(f))`, call-time outermost-first — full |
| python-decorators-class-04 | decorators | 1 | 2 | 2 | 1.00 | class decorator + `@functools.total_ordering` real example — full |
| python-datamodel-eqhash-01 | data-model | 3 | 2 | 2 | 1.00 | `__eq__`→`__hash__=None` unhashable; equal⇒equal-hash invariant — full |
| python-datamodel-slots-02 | data-model | 2 | 2 | 2 | 1.00 | fixed layout vs `__dict__`; no new attrs, MI caveat — full |
| python-datamodel-mutable-03 | data-model | 3 | 2 | 2 | 1.00 | default eval'd once at def time/shared; None-sentinel fix — full |
| python-datamodel-is-04 | data-model | 2 | 2 | 2 | 1.00 | identity vs value; small-int/string interning is impl detail, don't rely on it — full |
| python-datamodel-descriptor-05 | data-model | 1 | 2 | 2 | 1.00 | `__get__`/`__set__`/`__delete__`; property + bound-method binding — full |
| python-errors-elsefinally-01 | errors-exceptions | 2 | 2 | 2 | 1.00 | else no-exception, finally always; else narrows try scope — full |
| python-errors-custom-02 | errors-exceptions | 2 | 2 | 2 | 1.00 | subclass Exception; bare except swallows SystemExit/KeyboardInterrupt — full |
| python-errors-raisefrom-03 | errors-exceptions | 2 | 2 | 2 | 1.00 | `from err`→`__cause__` explicit chain; implicit `__context__`; `from None` — full |
| python-errors-group-04 | errors-exceptions | 2 | 2 | 2 | 1.00 | ExceptionGroup bundles concurrent errors; except* splits group by type — full |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| types-hints | 1.00 | 4 | ok | omit (strong) |
| dataclasses | 1.00 | 4 | ok | omit (strong) |
| async | 1.00 | 4 | ok | omit (strong) |
| iterators-generators | 1.00 | 4 | ok | omit (strong) |
| context-managers | 0.875 | 4 | ok | omit (strong) |
| decorators | 0.90 | 5 | ok | omit (strong) |
| data-model | 1.00 | 6 | ok | omit (strong) |
| errors-exceptions | 1.00 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Notes: `python-context-contextmanager-02` (normalized 0.50) is dual-tagged
`context-managers` + `decorators`, pulling both down slightly. No tag falls
below the 0.75 threshold.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 67 / 68 = 98.5%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none** — every tag scores ≥ 0.875.
**No derivation.** No `derived/python.mimo-v2.5.SKILL.md` is produced;
mimo-v2.5 already answers the entire Python corpus strongly closed-book.
