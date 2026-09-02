```yaml
- id: python-types-union-01
  answer: |
    `Optional[int]` means "an int or None" — it is shorthand for `Union[int, None]`. The name is a common trap: it does NOT mean "the field is optional/has a default"; it strictly means the value may be None.
    PEP 604 syntax (Python 3.10+): `int | None`. So `x: Optional[int]` == `x: int | None`.

- id: python-types-generics-02
  answer: |
    Classic style: declare a TypeVar and use it in both parameter and return position:
        from typing import TypeVar
        T = TypeVar("T")
        def first(x: T) -> T: ...
    That says "returns the same type it receives."
    Python 3.12 (PEP 695) added inline type-parameter declaration, removing the separate TypeVar boilerplate:
        def first[T](x: T) -> T: ...
        class Box[T]: ...
    The type parameters are scoped to the function/class automatically and no `TypeVar` import is needed.

- id: python-types-protocol-03
  answer: |
    A `typing.Protocol` class defines a structural ("static duck typing") interface: any object is considered to implement the protocol if it has the required methods/attributes with compatible signatures — no inheritance required. You just annotate with the protocol type and static checkers verify conformance implicitly.
    An ABC is nominal: a class must explicitly inherit from it (and implement its abstract methods) to be a subclass; conformance is declared, not inferred. ABCs can also provide shared default implementations and enforce instantiation checks at runtime (`TypeError` on instantiation if abstract methods remain). `@runtime_checkable` protocols can be used with `isinstance`, but that check only verifies method presence, not signatures.
    So: Protocol = implicit/structural, ABC = explicit/nominal.

- id: python-types-self-04
  answer: |
    Use `typing.Self` (PEP 673, Python 3.11+):
        def set_name(self, name: str) -> Self: ...
    This means "the type of the instance the method was called on," so chaining on a subclass `Cat(Animal)` returns `Cat`, not `Animal` (which is what `-> "Animal"` would wrongly promise).
    The older workaround was a class-scoped TypeVar bound to the class:
        SelfTV = TypeVar("SelfTV", bound="Animal")
        def set_name(self: SelfTV, name: str) -> SelfTV: ...
    `Self` replaces that boilerplate and is the modern tool (also available via `typing_extensions` on older versions).

- id: python-dataclasses-basics-01
  answer: |
    `@dataclass` inspects the class's annotated attributes and generates: `__init__` (with positional/keyword params and defaults from the fields), `__repr__`, and `__eq__` (field-by-field comparison, comparing as a tuple of fields). With `order=True` it also generates `__lt__`, `__le__`, `__gt__`, `__ge__`; `frozen=True` adds immutability; `eq=False` skips `__eq__`.
    The list default: `field: list = []` would bind ONE list object as the class-level default, shared by every instance — mutations in one instance would appear in all others (the classic mutable-default bug). The decorator actually rejects this with a TypeError for list/dict/set defaults. `field(default_factory=list)` instead calls `list()` inside `__init__` for each instance, so every object gets a fresh list.

- id: python-dataclasses-frozen-02
  answer: |
    `frozen=True` makes instances immutable: the generated `__setattr__` and `__delattr__` raise `dataclasses.FrozenInstanceError` on any attribute assignment/deletion. A frozen, eq=True dataclass also gets a generated `__hash__`, so instances are hashable and usable as dict keys / set members — unlike mutable ones (which get `__hash__ = None`).
    `__post_init__` is an optional hook the generated `__init__` calls after setting the declared fields; use it for validation or derived fields (including `InitVar` handling). With frozen=True you can't assign normally there, so you use `object.__setattr__(self, "field", value)` to set computed fields.

- id: python-dataclasses-slots-03
  answer: |
    `slots=True` (3.10+) generates a new class with `__slots__` set from the field names, so instances no longer carry a per-instance `__dict__`. Benefits: significantly lower memory per instance, slightly faster attribute access, and a typo'd attribute assignment fails loudly instead of silently creating a new attribute.
    Trade-offs: you can't add undeclared attributes to instances (no monkey-patching/attribute caches); weak references aren't supported unless `__weakref__` is included in slots; multiple inheritance is constrained (you can't inherit from two classes that both have non-empty slots); and interop with code expecting a writable `__dict__` (or patterns like `functools.cached_property`) breaks. Because the class is rebuilt, some patterns like combining with inheritance of non-slot parents lose the memory benefit.

- id: python-dataclasses-vs-04
  answer: |
    - `@dataclass`: the default choice for a structured, mutable record that also carries behavior — you want methods, validation, defaults, equality, maybe ordering or immutability.
    - `NamedTuple`: an immutable record that IS a tuple — supports indexing, unpacking, iteration, low overhead, and interop with tuple-based APIs. Good for small fixed records and data in transit where tuple compatibility matters. (Immutability and tuple semantics are the distinguishing features.)
    - `TypedDict`: purely a static typing annotation over a plain `dict` — zero runtime structure, no methods, no constructor; it exists so type checkers can validate dict shapes (e.g., JSON payloads, config dicts, kwargs bags). At runtime it's just a dict (and dicts from e.g. JSON are checked structurally).
    Rule of thumb: behavior/mutability → dataclass; immutable tuple-like record → NamedTuple; typing a plain dict → TypedDict.

- id: python-async-await-01
  answer: |
    Calling an `async def` function does NOT run it — it immediately returns a coroutine object. Coroutines are lazy: they're inert until something drives them.
    `await coro` submits it to the event loop, which runs it until it completes or hits an `await` that yields control; only then does the code actually execute. Similarly `asyncio.create_task(coro)` (or `loop.create_task`) schedules it to run concurrently. If you never await/schedule it, the body never executes (and you typically get a "coroutine was never awaited" RuntimeWarning). The event loop is the scheduler that actually runs coroutine code between suspension points.

- id: python-async-taskgroup-02
  answer: |
    `asyncio.gather(*aws)`: schedules all coroutines as tasks, returns results in argument order once all finish. With the default `return_exceptions=False`, the first raised exception propagates to the awaiter immediately, BUT the other tasks keep running in the background (gather does not cancel them), which can leak work or leave them unobserved; with `return_exceptions=True` exceptions come back as values in the result list.
    `asyncio.TaskGroup` (3.11+, structured concurrency): used as `async with asyncio.TaskGroup() as tg: tg.create_task(...)`. If any task fails, the TaskGroup cancels ALL remaining tasks in the group and waits for them to finish, then raises an `ExceptionGroup` containing all the errors (unwrapped automatically if exactly one). So TaskGroup guarantees no orphaned tasks and predictable failure semantics; gather does not cancel siblings.

- id: python-async-blocking-03
  answer: |
    The event loop runs all tasks on a single thread. A blocking call like `time.sleep(5)` or synchronous `requests.get(...)` occupies that thread for the whole duration — no other coroutine can run, so timers, heartbeats, other requests, and cancellations all stall; in a server this effectively freezes the entire process for other clients.
    Instead: use async equivalents (`await asyncio.sleep(5)`, an async HTTP client like `httpx.AsyncClient`/`aiohttp`). If only a sync version exists, offload it off the loop: `await asyncio.to_thread(func, ...)` (or `loop.run_in_executor`). Even "fast-looking" blocking calls (DNS, disk I/O, CPU-heavy work) count as bugs here.

- id: python-async-cancel-04
  answer: |
    `task.cancel()` doesn't kill the task instantly — it schedules a `asyncio.CancelledError` to be raised inside the task at its next suspension point (next `await`). The task can run cleanup in `finally`/context managers, but is expected to let the error propagate and finish as cancelled; `await task` then raises `CancelledError` in the canceller.
    You shouldn't swallow it (e.g., `except Exception` is fine since it doesn't catch CancelledError in 3.8+, but a broad `except BaseException: pass` does) because then the task refuses to die: timeouts, TaskGroup/structured-concurrency teardown, `asyncio.wait_for`, and program shutdown all rely on cancellation actually taking effect. Swallowing it leads to hung loops, leaked resources, and tasks that outlive their scope. If you must catch it for cleanup, re-raise afterward (`raise`).

- id: python-itergen-yield-01
  answer: |
    A function containing `yield` becomes a generator function: calling it executes none of the body and just returns a generator object (an iterator). The body only runs on demand — each `next()` executes until the next `yield`, returns that value, and pauses, preserving all local state; the next `next()` resumes from there. `return` in a generator ends it (raising StopIteration).
    A list-building function runs eagerly from start to finish, computes and stores every element in memory, then returns the complete list. The generator produces values one at a time, lazily, with O(1) working memory, and can even represent infinite sequences — but can only be consumed once.

- id: python-itergen-genexpr-02
  answer: |
    `[x*x for x in data]` is a list comprehension: it evaluates immediately, builds and returns a full list holding every element. `(x*x for x in data)` is a generator expression: it returns a lazy generator; nothing is computed until you iterate, and elements are produced one at a time.
    The second matters when: the sequence is large or infinite (constant memory instead of materializing everything), you'll consume it once (especially feeding into `sum()`/`any()`/`max()`/`itertools` pipelines), you want to start processing before all input is available, or short-circuiting (e.g. `any(...)`) can avoid computing later elements entirely. Use the list form when you need to iterate multiple times, index, or know the length.

- id: python-itergen-itertools-03
  answer: |
    Examples (any two):
    - `itertools.islice(iterable, stop)` — slices an iterator lazily (e.g., first N items of a huge/generator stream) without materializing or indexing an intermediate list.
    - `itertools.groupby(iterable, key)` — groups consecutive items in a single pass; a manual loop needs index bookkeeping and temporary containers.
    - `itertools.chain(*iterables)` — treats several iterables as one without concatenating lists.
    - `itertools.combinations/permutations/product` — express combinatorics declaratively instead of nested loops.
    Why preferable: they're lazy (constant memory), single-pass, C-implemented (fast), and compose — you build pipelines like `sum(x*x for x in islice(gen, 1000))` with no intermediate lists, unlike a manual loop that builds and holds a full list.

- id: python-itergen-protocol-04
  answer: |
    Iterable: an object with `__iter__()` returning an iterator (or, legacy sequence style, `__getitem__` accepting integer indices from 0). Things like lists, dicts, strings are iterables.
    Iterator: an object with `__next__()` producing the next value (raising StopIteration when exhausted) and `__iter__()` that returns itself. Generators are iterators.
    The distinction matters because iterators are consumed: each `__iter__` call on an iterator returns the same, already-exhausted self, so iterating a generator twice yields nothing the second time. A proper iterable is a factory: each `__iter__()` call returns a fresh independent iterator, which is why you can loop over a list as many times as you like. Passing an iterator where a fresh pass is expected is a classic bug.

- id: python-context-with-01
  answer: |
    The `with` statement guarantees that the context manager's cleanup (`__exit__`) runs when the block is left — whether the block finishes normally or raises an exception — making cleanup deterministic instead of relying on garbage collection.
    The dunders: `__enter__(self)` — called on entering; its return value is bound to the `as` target. `__exit__(self, exc_type, exc_value, traceback)` — called on exit; receives the exception info (None if no exception); returning a truthy value suppresses the exception, returning False/None lets it propagate.
    This is what makes `with open(...) as f`, locks, and transactions reliable even under exceptions.

- id: python-context-contextmanager-02
  answer: |
    `@contextlib.contextmanager` decorates a one-yield generator function; calling it returns a context manager. When the `with` block enters, the generator runs to the `yield` (that's the setup, and its yielded value is bound to the `as` target). When the block exits, execution resumes just after the `yield` — that's the teardown. If the body raised, the exception is thrown into the generator at the `yield` point; you can handle it or let it propagate, and teardown code should be in a `try/finally` so it runs either way.
    Pattern:
        @contextmanager
        def cm():
            resource = acquire()      # setup / __enter__
            try:
                yield resource
            finally:
                release(resource)     # teardown / __exit__

- id: python-context-exitstack-03
  answer: |
    A nested `with a, b, c:` is purely syntactic and static — you must know the exact set of context managers at authoring time. `ExitStack` manages cleanup for a dynamic set: context managers opened conditionally (if/else branches), a variable number determined at runtime (e.g., one lock or file per item in a loop), resources acquired deep in a call stack and cleaned up at a common exit point, plus ad-hoc callbacks (`stack.callback(fn)`) for non-CM cleanup.
    It also supports patterns like `stack.pop_all()` to transfer accumulated cleanups (e.g., commit/rollback semantics: pop the stack on success so cleanup doesn't run, or let it unwind on failure). In short: it turns fixed, nested `with` blocks into a programmatic, runtime-built cleanup stack.

- id: python-context-async-04
  answer: |
    `async with` is the asynchronous form of `with`, used with async context managers that implement `__aenter__` and `__aexit__` — both are coroutines (awaited by the `async with` machinery). It must be used inside a coroutine.
    A regular `with` can't do the job because acquiring/releasing many async resources — connecting a client, acquiring an `asyncio.Lock`, opening an async DB transaction — involves `await` points (possible suspension). `__enter__`/`__exit__` are plain synchronous methods and cannot suspend, so the protocol simply doesn't give them a chance to await; `__aenter__`/`__aexit__` exist precisely so setup/teardown can be awaited. The guarantee is otherwise the same: `__aexit__` runs on block exit even on exception.

- id: python-decorators-basics-01
  answer: |
    A decorator is fundamentally just a callable that takes a function (or class) and returns a replacement — usually a wrapper that adds behavior around the original. `@dec` above `def f` is sugar for `f = dec(f)`.
    `functools.wraps(orig)` should be applied to the wrapper to copy the original's metadata (`__name__`, `__doc__`, `__qualname__`, `__module__`, `__dict__`, and set `__wrapped__`). Without it, the wrapper's own name/docstring leak out: tracebacks, debuggers, `help()`, introspection, and anything that reads `__name__` (serializers, registries) see misleading values; `__wrapped__` also lets `inspect.signature` show the true signature.

- id: python-decorators-args-02
  answer: |
    Because `@expr` applies exactly ONE callable to the function. `@retry(times=3)` means `f = retry(times=3)(f)` — so `retry(times=3)` must itself return the decorator that will receive `f`. That forces three levels:
        def retry(times):            # 1. takes the decorator's arguments
            def decorator(func):     # 2. the actual decorator, takes the function
                @functools.wraps(func)
                def wrapper(*a, **k):    # 3. the wrapper, takes call args
                    ...
                return wrapper
            return decorator
        return decorator
    A plain `@dec` only needs levels 2 and 3. The outer layer is a "decorator factory" that closes over the configuration and produces the real decorator.

- id: python-decorators-stacking-03
  answer: |
    Given
        @a
        @b
        def f(): ...
    Decorators are applied bottom-up (nearest to the function first): `f = a(b(f))` — so `b` is applied first, then `a`; equivalently `a` wraps `b`'s wrapper.
    At call time execution is top-down through the nesting: `a`'s wrapper is the outermost, so its pre-call code runs first, then it calls `b`'s wrapper, whose pre-call code runs, then `f` itself, then `b`'s post-code, then `a`'s post-code. Mnemonic: application order is bottom-up; execution order is top-down (outermost wrapper first).

- id: python-decorators-class-04
  answer: |
    A class decorator is a callable that receives a class and returns a (usually modified or replaced) class. It runs once at class-definition time and can add/replace attributes and methods, wrap existing methods, register the class, or swap in a whole new implementation — anything `f = dec(f)` does when f is a class. (It can't change how instances are created the way a metaclass can — e.g., it can't alter the already-computed `__init__` call semantics of subclasses transparently — but for post-hoc modification it's simpler than a metaclass.)
    Standard-library examples: `@dataclasses.dataclass` (generates `__init__`/`__repr__`/`__eq__` etc.), `@functools.total_ordering` (fills in the missing comparison methods from `__eq__` and one ordering method), and `@typing.runtime_checkable` (adds `isinstance` support to a Protocol).

- id: python-datamodel-eqhash-01
  answer: |
    Defining `__eq__` without also defining `__hash__` sets the class's `__hash__` to None, making instances unhashable — they can't go in dicts or sets. This is deliberate: Python refuses a default identity hash for a class with value-based equality.
    Consistency requirement: the hash contract says equal objects must have equal hashes (`a == b` ⇒ `hash(a) == hash(b)`). If they diverge, hash-based containers break subtly: an object put in a set becomes unfindable after its (mutable) equality changed, duplicates can coexist, and lookups miss. That's why mutable objects generally shouldn't be hashable, and `@dataclass` only generates `__hash__` when `eq=True, frozen=True`.

- id: python-datamodel-slots-02
  answer: |
    Declaring `__slots__ = ("x", "y")` on a class replaces the per-instance `__dict__` with fixed per-class slot descriptors (members of `member_descriptor` type). Instances store their attribute values in a compact fixed-size structure instead of a hash table: less memory, faster attribute access, and attempts to assign undeclared attributes raise AttributeError.
    You give up: dynamic attribute creation (no arbitrary attributes, no attribute monkey-patching per instance); weak reference support unless you add `'__weakref__'` to slots; multiple-inheritance flexibility (a class can't inherit from two classes with non-empty slots, since each contributes instance layout); `functools.cached_property` and anything else that needs a per-instance `__dict__`; and subclasses without their own `__slots__` silently get a `__dict__` again, negating the benefit.

- id: python-datamodel-mutable-03
  answer: |
    Default parameter values are evaluated ONCE, at function definition time — not per call. So `def add(item, target=[]):` creates one list object that every call without `target` shares; `target.append(item)` mutates it, so the mutations accumulate across all calls and appear as spooky cross-call state.
    The correct pattern is a `None` sentinel, creating the container inside the call:
        def add(item, target=None):
            if target is None:
                target = []
            target.append(item)
            return target
    (Same bug applies to dicts/sets/any mutable default.)

- id: python-datamodel-is-04
  answer: |
    `is` tests identity — whether the two names reference the very same object (same memory address, effectively comparing `id()`); it's not affected by `__eq__`. `==` tests value equality — it calls the left operand's `__eq__`.
    The small int / string surprise is a CPython implementation detail: small integers in the range roughly −5..256 are cached singletons, and many strings are interned (or created at compile time from identical literals), so separately written `a = 100; b = 100` can be the same object (`a is b` True). Values outside that cache or built at runtime (`1000`, strings from input/concatenation) are usually distinct objects, so `is` is False even though `==` is True. Because this caching is version- and implementation-dependent, never use `is` for value comparison — only for true identity checks like `x is None`.

- id: python-datamodel-descriptor-05
  answer: |
    A descriptor is a class that implements `__get__(self, obj, objtype)`, and optionally `__set__`/`__delete__`, and is stored as a class attribute. Attribute access on instances is routed through it: `obj.attr` invokes the descriptor's `__get__` (with `obj` as the owning instance). Descriptors with `__set__`/`__delete__` are "data descriptors" and take priority in the lookup order (data descriptors → instance `__dict__` → non-data descriptors/class attrs); non-data descriptors (only `__get__`) are checked after the instance dict.
    This explains core Python behavior:
    - `property` is a data descriptor: `property(fget, fset)` implements `__get__`/`__set__`, so `obj.x` calls your getter, and a data descriptor without `__set__` makes the attribute read-only (assignment raises, rather than shadowing).
    - Plain functions are non-data descriptors: their `__get__` returns a bound method binding the instance — that's exactly how `self` is passed to methods, and why the same function accessed on the class vs instance differs. `classmethod`/`staticmethod` are also descriptors with different binding behavior.

- id: python-errors-elsefinally-01
  answer: |
    In `try/except/else/finally`: `else` runs only if the `try` block completed without raising any exception (it runs before `finally`). `finally` runs unconditionally — after normal completion, after a handled exception, or while an exception is propagating (even through `return`/`break`/`continue`).
    Prefer `else`: code that should run only on success but whose exceptions you do NOT want to handle belongs there, not in the `try`. Keeping the `try` block minimal means the `except` clauses only catch failures from the operation you're actually guarding; put the guarded code's follow-up work in the `try` and you risk accidentally catching (and mishandling) exceptions raised by that follow-up code.

- id: python-errors-custom-02
  answer: |
    Define a custom exception by subclassing `Exception` (not `BaseException`, which is reserved for control-flow exceptions like `KeyboardInterrupt`/`SystemExit` that shouldn't normally be caught):
        class PaymentDeclined(Exception):
            """Raised when the payment processor declines the transaction."""
    Add attributes/`__init__` args when callers need structured data; give it a docstring stating when it's raised. Libraries often define a common base (e.g., `AppError`) so users can catch broadly or narrowly.
    Catch specific types rather than a bare `except:` because bare `except:` (and even bare `except Exception:` to a lesser degree) swallows everything, including `KeyboardInterrupt`, `SystemExit`, `GeneratorExit`, and bugs like `NameError`/`AttributeError`/`TypeError` you never anticipated — masking real errors, making debugging hard, and potentially catching exceptions you can't meaningfully handle. Narrow `except SomeError as e:` scopes the handling to failures you expect and know how to recover from.

- id: python-errors-raisefrom-03
  answer: |
    `raise NewError(...) from err` — inside an `except` block — raises the new exception with `err` explicitly attached as its `__cause__` (and sets `__suppress_context__ = True`). The traceback shows the original exception first, then "The above exception was the direct cause of the following exception," making the causal chain explicit: the low-level error is preserved for debugging while callers see the higher-level domain error.
    A plain `raise NewError(...)` inside an `except` block instead relies on implicit chaining: the original exception is stored in `__context__` and shown as "During handling of the above exception, another exception occurred" — the same chain appears in output, but the relationship is implicit ("happened during"), not asserted ("was caused by"). `raise ... from None` suppresses the displayed context entirely when the original error is noise.

- id: python-errors-group-04
  answer: |
    `ExceptionGroup` (Python 3.11, PEP 654) is an exception that contains a list of other exceptions, raised all together; `BaseExceptionGroup` is the general version. It was added because concurrent code — notably `asyncio.TaskGroup` and structured concurrency — can legitimately have multiple tasks fail at once, and the old model (a single exception propagating) forced you to lose or awkwardly re-wrap all but one error.
    `except*` is the matching syntax for groups, and differs from normal `except` in key ways:
    - Each `except* SomeType` clause selects the sub-exceptions of the group matching that type; multiple `except*` clauses can ALL fire in one handling pass (normal `except`: only the first matching clause runs).
    - It matches recursively against nested groups, and unmatched exceptions are re-raised as a group to outer handlers.
    - Each `except*` handles only the matching subgroup (the group's raised error in that clause contains just those members), and `except*` cannot be mixed with regular `except` in the same `try`, nor use bare `except*`.
    Note: a bare `except ExceptionGroup` with normal `except` also works if you want to intercept the whole group.
```
