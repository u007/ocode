# Python Benchmark — Human Render

> **Generated from `questions.yaml` (corpus_rev 1). Do not grade from this file
> — `questions.yaml` is the source of truth.** Hand-synced; if they disagree,
> the YAML wins.

Legend: **W** = weight (1–3), **D** = difficulty. Rubric shows scored points
(`•`) and partial-credit levels (`~`). Version-gated features are noted inline.

---

### python-types-union-01 · types-hints · W2 · easy
**Q:** What does `Optional[int]` mean, and how do you write it with PEP 604 syntax?
**A:** `Optional[int]` == `Union[int, None]` (int or None) — not "optional argument". Since 3.10 (PEP 604) write unions with `|`: `int | None`, `int | str`; no `typing` import needed.
• Optional == Union[int, None], not "optional arg" • PEP 604 (3.10) `int | None` with `|` ~ reads Optional as "argument can be omitted"

### python-types-generics-02 · types-hints · W2 · medium
**Q:** Generic function returning the type it receives, and what 3.12 changed about type params?
**A:** Declare a `TypeVar` and reuse it: `def first(x: list[T]) -> T`. 3.12 (PEP 695) adds inline syntax: `def first[T](...)` / `class Box[T]`, scoping `T` without a separate `TypeVar`.
• TypeVar links input and output type • 3.12/PEP 695 inline `def first[T]` / `class Box[T]` ~ uses TypeVar but unaware of 3.12 `[T]`

### python-types-protocol-03 · types-hints · W2 · medium
**Q:** What is a `typing.Protocol`, and how does it differ from inheriting an ABC?
**A:** Protocol = structural typing: any class with the right methods matches, no subclassing/import. ABC = nominal: must explicitly subclass. Protocols type objects you can't make subclass; checked by the type checker (or `@runtime_checkable`).
• Protocol structural, matches by shape • ABC nominal, must subclass ~ "like an interface" without structural-vs-nominal

### python-types-self-04 · types-hints · W1 · medium
**Q:** Annotate a method returning `self` on a subclassable class — and newer tools?
**A:** Return `Self` (typing, 3.11 / PEP 673) so subclasses get the right type; `-> BaseClass` would widen. Pre-3.11 used a bound `TypeVar`. Related 3.12: `type Alias = ...` statement (PEP 695).
• return `Self` (3.11) so subclasses get right type • `-> BaseClass` loses subclass type / pre-3.11 bound TypeVar ~ "return the class name" without subclass widening

### python-dataclasses-basics-01 · dataclasses · W2 · easy
**Q:** What does `@dataclass` generate, and why use `field(default_factory=list)` not `= []`?
**A:** Generates `__init__`/`__repr__`/`__eq__` from annotated fields. A bare `= []` would be one shared mutable list (dataclasses reject it, raising `ValueError`); `default_factory=list` builds a fresh list per instance.
• generates `__init__`/`__repr__`/`__eq__` • default_factory per-instance; shared/mutable default is wrong ~ names methods but not why default_factory

### python-dataclasses-frozen-02 · dataclasses · W2 · medium
**Q:** What does `frozen=True` do, and what is `__post_init__` for?
**A:** `frozen=True`: immutable (assignment raises `FrozenInstanceError`), gains `__hash__` (normal dataclass is unhashable); set derived fields via `object.__setattr__`. `__post_init__` runs after generated `__init__` — derived fields / validation.
• frozen=True immutable + hashable • `__post_init__` runs after `__init__` for derived/validation ~ only one of the two

### python-dataclasses-slots-03 · dataclasses · W1 · medium
**Q:** What does `@dataclass(slots=True)` give you, and the trade-off?
**A:** `slots=True` (3.10) generates `__slots__`: less memory, faster attribute access, no per-instance `__dict__`. Cost: can't add undeclared attributes; `__slots__`/inheritance caveats.
• slots=True (3.10) → `__slots__`: less memory, faster, no `__dict__` • cost: no undeclared attributes ~ "saves memory" without the no-new-attributes cost

### python-dataclasses-vs-04 · dataclasses · W2 · medium
**Q:** When choose a `@dataclass`, a `NamedTuple`, a `TypedDict`?
**A:** dataclass = mutable/frozen object with methods (general record). NamedTuple = immutable tuple (indexable/unpackable/hashable). TypedDict types a plain dict's shape, not an instance. Object→dataclass, tuple→NamedTuple, dict→TypedDict.
• dataclass = object with methods • NamedTuple = immutable tuple • TypedDict types a dict's shape ~ only two of three

### python-async-await-01 · async · W3 · medium
**Q:** What does calling an `async def` return, and why does nothing run until awaited/scheduled?
**A:** Returns a coroutine object; body doesn't run. It progresses only when driven by the loop (`await`, `asyncio.run`, `create_task`); `await` suspends and yields control so other tasks run. Un-awaited → "never awaited" warning.
• async def call returns a coroutine; body doesn't run • runs only via loop (await/run/create_task); await yields ~ "await runs it" without the coroutine/loop model

### python-async-taskgroup-02 · async · W3 · hard
**Q:** Contrast `asyncio.gather` with `asyncio.TaskGroup`, especially on failure.
**A:** `gather`: one failure propagates but siblings keep running (not cancelled) — can leak work. `TaskGroup` (3.11) `async with`: on failure cancels remaining tasks and raises an `ExceptionGroup` bundling all errors. TaskGroup is the 3.11+ default.
• gather: siblings keep running on failure • TaskGroup (3.11) cancels remaining on failure • bundles errors in ExceptionGroup ~ "both run concurrently" without failure contrast

### python-async-blocking-03 · async · W3 · hard
**Q:** Why is `time.sleep(5)`/sync `requests.get` inside a coroutine a bug, and the fix?
**A:** Blocking calls don't yield, so they freeze the whole event loop (every task) for the duration. Fix: async equivalent (`await asyncio.sleep`, async HTTP client) or push blocking/CPU work off the loop with `await asyncio.to_thread(...)`/executor.
• blocking stalls the whole loop (all tasks) • fix: async equivalent or `asyncio.to_thread`/executor ~ "it's slow" without whole-loop blocking

### python-async-cancel-04 · async, errors-exceptions · W2 · hard
**Q:** How does task cancellation work, and why not swallow `CancelledError`?
**A:** `task.cancel()` raises `CancelledError` at the next `await` (cooperative). It's a `BaseException` (3.8+), so `except Exception` misses it. If you catch it for cleanup you must re-raise; swallowing breaks cancellation and can hang shutdown/TaskGroup. Use `try/finally`.
• cancel() raises CancelledError at next await; cooperative • don't swallow — re-raise (BaseException) ~ "you can cancel tasks" without must-re-raise

### python-itergen-yield-01 · iterators-generators · W3 · easy
**Q:** What makes a function a generator, and how does execution differ from returning a list?
**A:** Any function with `yield` is a generator; calling it returns a generator object, runs no body. Each `next()` runs to the next `yield`, produces a value, suspends with state frozen. Lazy, one-at-a-time, O(1) memory (can be infinite) vs a list that materializes everything.
• yield → generator; next() runs to yield then suspends (state frozen) • lazy/constant memory vs list materializes all ~ "yields multiple values" without the suspend mechanism

### python-itergen-genexpr-02 · iterators-generators · W2 · easy
**Q:** `[x*x for x in data]` vs `(x*x for x in data)` — and when does the second matter?
**A:** List comp materializes the whole list; genexpr is lazy, one item at a time. Genexpr wins for large/infinite data or feeding a one-pass consumer (`sum(x*x for x in data)`). Genexpr is single-use; a list re-iterates.
• list comp materializes all; genexpr lazy • genexpr for large/infinite or one-pass consumer (sum) ~ notes () vs [] but not eager-vs-lazy

### python-itergen-itertools-03 · iterators-generators · W1 · medium
**Q:** Two `itertools` tools and why they beat a manual list-building loop.
**A:** e.g. `chain(a, b)` iterates several iterables as one; `islice(it, 10)` takes first N lazily (works on infinite); also `count`/`cycle`/`groupby`/`batched` (3.12). Lazy C-level, constant memory, composable, handle unbounded streams; a list loop materializes everything.
• names ≥2 real itertools functions • lazy/constant-memory/composable vs materializing ~ names tools without the rationale

### python-itergen-protocol-04 · iterators-generators, data-model · W2 · medium
**Q:** Iterable vs iterator methods, and why it matters (iterating twice)?
**A:** Iterable defines `__iter__` (returns a fresh iterator); iterator defines `__next__` (+ `StopIteration`) and `__iter__` returning self. Iterator is single-pass/stateful — once exhausted, stays exhausted. A container yields a new iterator each `for`, so it re-iterates; a raw generator can't.
• iterable `__iter__` returns iterator; iterator `__next__`+`__iter__`→self • iterator single-pass; iterable re-iterates ~ conflates the two / only `__iter__`

### python-context-with-01 · context-managers · W3 · easy
**Q:** What does `with` guarantee, and which dunders implement a context manager?
**A:** `with cm as x:` calls `__enter__()` (return bound to `x`) on entry, guarantees `__exit__(exc_type, exc, tb)` on exit (normal, return, or raise). Correct way to manage resources — cleanup can't be skipped. `__exit__` gets exception info; return True suppresses it.
• `__enter__` on entry; `__exit__` always runs even on exception • guarantees cleanup / `__exit__` can suppress via True ~ "with closes files" without the dunders/guarantee

### python-context-contextmanager-02 · context-managers, decorators · W2 · medium
**Q:** How does `@contextlib.contextmanager` turn a generator into a CM, and where do setup/teardown go?
**A:** Decorate a single-`yield` generator: before `yield` = setup (`__enter__`), the yielded value is the `as` target, after `yield` = teardown (`__exit__`). Put the `yield` in `try/finally` so teardown runs even when the body raises (the exception re-raises at the yield).
• before yield=setup, after yield=teardown; yielded value is as-target • wrap yield in try/finally for exceptions ~ knows the gen→CM but misses try/finally

### python-context-exitstack-03 · context-managers · W1 · medium
**Q:** What does `contextlib.ExitStack` solve that nested `with` doesn't?
**A:** Manages a dynamic/variable number of CMs not known at compile time (e.g. a list of files). `stack.enter_context(cm)` in a loop; all unwound in reverse on exit. Nested `with` needs a fixed set. Also `stack.callback(...)` for arbitrary cleanup.
• dynamic/variable number of CMs (not static) • all entered CMs cleaned up in reverse / enter_context in a loop ~ "combines CMs" without dynamic-count

### python-context-async-04 · context-managers, async · W2 · medium
**Q:** What is `async with`, which dunders, and why can't plain `with` do async resources?
**A:** `async with cm as x:` awaits `__aenter__` on entry and `__aexit__` on exit — for coroutine-based setup/teardown (async DB, aiohttp session, async lock). Plain `with` uses sync `__enter__`/`__exit__` which can't await. Only valid inside `async def`.
• async with awaits `__aenter__`/`__aexit__` • plain with is sync, can't await ~ "with for async" without the dunders/why

### python-decorators-basics-01 · decorators · W3 · medium
**Q:** What is a decorator fundamentally, and why use `functools.wraps`?
**A:** A callable taking a function/class and returning a replacement; `@deco def f` == `f = deco(f)`. Usual shape returns an inner `wrapper` closure. The wrapper replaces the original, so `__name__`/`__doc__`/signature become the wrapper's; `@functools.wraps(func)` copies that metadata back, keeping introspection accurate.
• decorator takes a function, returns replacement; @deco == f = deco(f) • functools.wraps copies name/doc/metadata ~ explains decorator but not @wraps

### python-decorators-args-02 · decorators · W2 · hard
**Q:** Why does `@retry(times=3)` need an extra nesting layer vs a plain decorator?
**A:** Three levels: `retry(times=3)` is a factory that takes the args and returns the actual decorator, which takes `func` and returns `wrapper`. `@retry(times=3)` == `f = retry(3)(f)`. Args are captured in the closure the wrapper reads.
• outer call is a factory returning the real decorator (3 levels) • @retry(3) == retry(3)(f); args in closure ~ "add another function" without factory model

### python-decorators-stacking-03 · decorators · W2 · medium
**Q:** With `@a` then `@b` above `def f`, what order do they apply, and run at call time?
**A:** Bottom-up application: `@a @b def f` == `f = a(b(f))`, so `b` (nearest) wraps first, then `a`. At call time the outermost runs first: `a`'s wrapper before `b`'s, then the original `f`. Apply nearest-first, execute outermost-first — opposite orders.
• applied bottom-up: f = a(b(f)) (nearest wraps first) • at call time a runs first, then b, then f ~ one order without apply-vs-call distinction

### python-decorators-class-04 · decorators · W1 · medium
**Q:** What can a class decorator do — a stdlib example?
**A:** Takes the class object and returns/modifies it: `@deco class C` == `C = deco(C)`. Can add/replace methods, register, or wrap. Canonical stdlib: `@dataclass` (injects `__init__`/`__repr__`/`__eq__` from fields); also `functools.total_ordering` (fills missing comparisons).
• class decorator takes/returns the class (@deco class C == C = deco(C)) • stdlib example (@dataclass / @total_ordering) and what it injects ~ "decorates a class" with no example

### python-datamodel-eqhash-01 · data-model · W3 · hard
**Q:** If you define `__eq__`, what happens to hashability, and why keep `__eq__`/`__hash__` consistent?
**A:** Defining `__eq__` sets `__hash__` to `None` → instances become unhashable (no sets/dict keys) until you also define `__hash__`. Invariant: equal objects must hash equal (`a==b` ⇒ `hash(a)==hash(b)`), else sets/dicts lose/dupe entries. Base hash on the same fields; only hash effectively-immutable fields.
• defining `__eq__` sets `__hash__`=None → unhashable • equal objects must hash equal; base both on same fields ~ "define both" without `__hash__`=None or the invariant

### python-datamodel-slots-02 · data-model · W2 · medium
**Q:** What does declaring `__slots__` do at the data-model level, and what do you give up?
**A:** `__slots__ = ("x","y")` stores those attrs in a fixed per-instance layout (class descriptors) instead of a per-instance `__dict__`: less memory, faster access. Cost: can't assign attributes outside the list (`AttributeError`), no `__dict__` unless added, `__weakref__`/multiple-inheritance caveats.
• `__slots__` = fixed layout, no per-instance `__dict__` → less memory/faster • can't add attrs outside the list (AttributeError) ~ "saves memory" without mechanism/cost

### python-datamodel-mutable-03 · data-model · W3 · medium
**Q:** Why is `def add(item, target=[])` a classic bug, and the correct pattern?
**A:** The `[]` default is evaluated once at `def` time, not per call; every call omitting `target` shares that one list, so items accumulate (same for `{}`). Fix with the `None` sentinel: `target = [] if target is None else target` — a fresh list per call.
• default evaluated once at def time and shared across calls • fix: None sentinel, create mutable inside the function ~ notices the bug but no fix

### python-datamodel-is-04 · data-model · W2 · easy
**Q:** Difference between `is` and `==`, and why is `a is b` surprising for small ints/strings?
**A:** `==` compares value (`__eq__`); `is` compares identity (same object / `id()`). Use `is` only for singletons like `None`. CPython caches small ints (~-5..256) and interns some strings, so `a is b` may be True for equal small values as an impl detail — don't rely on it; larger/computed values give False.
• == value (`__eq__`), is identity (same object/id) • use is only for None; small-int/string interning is impl detail ~ value vs identity but claims interchangeable

### python-datamodel-descriptor-05 · data-model · W1 · hard
**Q:** What is a descriptor, and how does it explain `property` and methods?
**A:** A class-attribute object defining `__get__` (and optionally `__set__`/`__delete__`); attribute access routes through them. `property` is a descriptor — `obj.x` calls its `__get__`. Functions are descriptors too: `__get__` binds a function into a bound method (injecting `self`). Data descriptors (with `__set__`) beat the instance `__dict__`.
• descriptor = class attr with `__get__`/`__set__`/`__delete__` intercepting access • property is one; functions use `__get__` to bind methods (self) ~ "like property" without the mechanism

### python-errors-elsefinally-01 · errors-exceptions · W2 · medium
**Q:** When do `else`/`finally` run in `try/except/else/finally`, and why prefer `else`?
**A:** `else` runs only if the `try` raised nothing (before `finally`). `finally` always runs (normal, handled, unhandled, even `return`/`break`) — for guaranteed cleanup. `else` keeps success-only code outside the `try`, so its errors aren't accidentally caught by the same `except`.
• else only when try raised nothing; finally always runs (incl. return/exception) • else narrows try so unrelated errors aren't caught ~ describes finally but not else semantics

### python-errors-custom-02 · errors-exceptions · W2 · easy
**Q:** How define a custom exception, and why catch specific types not a bare `except:`?
**A:** Subclass `Exception` (not `BaseException`): `class ConfigError(Exception)`, often an app base plus subclasses. Bare `except:`/`except BaseException` also swallows `KeyboardInterrupt`/`SystemExit`, hides bugs, and makes failures invisible. Catch the narrowest exception you can handle.
• subclass Exception (app base + specific subclasses) • bare except swallows KeyboardInterrupt/SystemExit and hides bugs; catch narrow ~ how to define but no reason to avoid bare except

### python-errors-raisefrom-03 · errors-exceptions · W2 · medium
**Q:** What does `raise NewError(...) from err` do vs a plain `raise NewError(...)` in `except`?
**A:** `from err` sets `__cause__` → explicit "direct cause" chain; deliberately translate a low-level error to a domain one, preserving the original. A plain `raise` in `except` still implicitly chains via `__context__` ("during handling ... another occurred"); `from` makes the link explicit. `from None` suppresses the chain.
• from err sets `__cause__` → explicit direct-cause chain, preserves original • plain raise implicitly chains via `__context__`; from None suppresses ~ "keeps original error" without cause-vs-context

### python-errors-group-04 · errors-exceptions · W2 · hard
**Q:** What are `ExceptionGroup` and `except*`, why added, and how does `except*` differ from `except`?
**A:** Added in 3.11 (PEP 654) for multiple simultaneous exceptions — motivated by `asyncio.TaskGroup` where several tasks fail. `ExceptionGroup` bundles them. `except* ValueError` extracts matching exceptions from the group, letting non-matching ones flow to other `except*` clauses (several can each fire). A normal `except` catches the whole group as one object.
• ExceptionGroup (3.11/PEP 654) bundles simultaneous exceptions (TaskGroup) • except* splits a group by type; multiple except* can each fire ~ "catch multiple errors" without the group/split or 3.11
