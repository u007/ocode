```yaml
- id: python-types-union-01
  answer: |
    `Optional[int]` is shorthand for `Union[int, None]` — it means a value is either an `int` or `None`. With PEP 604 (Python 3.10+) you write `int | None`.

- id: python-types-generics-02
  answer: |
    You write a generic function like `def first[T](lst: list[T]) -> T: return lst[0]`. Before 3.12 you used `TypeVar`: `T = TypeVar('T'); def first(lst: list[T]) -> T: ...`. Python 3.12 introduced the inline `def first[T](...)` syntax so you no longer need a separate `TypeVar` line.

- id: python-types-protocol-03
  answer: |
    A `typing.Protocol` defines a structural (duck) type — any object that has the right attributes/methods satisfies it without explicit inheritance. An abstract base class (ABC) requires explicit subclassing. Protocols are checked statically by type checkers; ABCs enforce at runtime via `__instancecheck__` and raise `TypeError` if abstract methods aren't implemented.

- id: python-types-self-04
  answer: |
    Annotate the return type as `Self` from `typing.Self` (Python 3.11+) or `typing_extensions.Self`. Before that, you'd use a `TypeVar` bound to the class: `T = TypeVar('T', bound='MyClass')` and return `T`. `Self` automatically resolves to the actual subclass.

- id: python-dataclasses-basics-01
  answer: |
    `@dataclass` auto-generates `__init__`, `__repr__`, and `__eq__` based on declared fields. A default must use `field(default_factory=list)` because mutable defaults share a single reference across all instances — a module-level list literal as a default would be the same object for every instance, so mutations in one instance affect all others.

- id: python-dataclasses-frozen-02
  answer: |
    `frozen=True` makes instances immutable — attempting to set any attribute raises `FrozenInstanceError`. It also makes instances hashable. `__post_init__` is a hook called at the end of `__init__` (and `__init_subclass__` for `__replace__`), useful for derived fields, validation, or conversions that depend on multiple fields being set.

- id: python-dataclasses-slots-03
  answer: |
    `slots=True` (Python 3.10+) generates a `__slots__` tuple from the fields, reducing per-instance memory by eliminating the `__dict__` and enabling faster attribute access. The trade-off is you cannot add arbitrary attributes not declared in the class, which can break code that monkey-patches or dynamically adds attributes.

- id: python-dataclasses-vs-04
  answer: |
    Use a `@dataclass` for mutable value objects with behavior (methods, defaults, post-init). Use a `NamedTuple` for immutable, lightweight tuples that also act as records. Use a `TypedDict` when you need a dict-like structure with typed keys — typically for JSON/API payloads that stay as plain dicts.

- id: python-async-await-01
  answer: |
    Calling an `async def` function returns a coroutine object — it doesn't execute any of the function body. The coroutine is a lazy awaitable; nothing runs until you `await` it (inside another async context) or schedule it on the event loop (e.g. `asyncio.run(coro())` or `loop.create_task(coro())`).

- id: python-async-taskgroup-02
  answer: |
    `asyncio.gather` runs coroutines concurrently and returns results in input order; if one fails it raises the exception but other tasks keep running (unless `return_exceptions=False` and you don't shield). `asyncio.TaskGroup` (Python 3.11+) uses an async context manager and automatically cancels all remaining tasks when any task raises, then raises an `ExceptionGroup` containing all errors — giving structured concurrency with automatic cleanup.

- id: python-async-blocking-03
  answer: |
    Calling `time.sleep(5)` or `requests.get(...)` inside an async coroutine blocks the entire event loop thread, stalling every other concurrent task. Use `await asyncio.sleep(5)` and an async HTTP client like `httpx` or `aiohttp` instead, which yield control back to the loop while waiting.

- id: python-async-cancel-04
  answer: |
    A task is cancelled when `Task.cancel()` is called, which schedules a `CancelledError` to be thrown into the coroutine at the next `await` point. You should not swallow `CancelledError` because doing so prevents the task from actually stopping, breaks structured concurrency (e.g. `TaskGroup` relies on propagation), and can leave resources uncleaned. Let it propagate or handle cleanup in `finally`.

- id: python-itergen-yield-01
  answer: |
    A function containing a `yield` expression is a generator function — calling it returns a generator object. Each `yield` produces a value and suspends the function's frame, resuming on the next `next()` call. This is fundamentally different from building a list and returning it: a generator is lazy, producing one value at a time with minimal memory, whereas a list-based function eagerly computes and stores all values before returning.

- id: python-itergen-genexpr-02
  answer: |
    `[x*x for x in data]` is a list comprehension that builds an entire list in memory. `(x*x for x in data)` is a generator expression that yields items lazily, one at a time. The generator expression matters when the result is large (or infinite), when you only need to iterate once, or when passing to a function that accepts any iterator (e.g. `sum(...)`, `tuple(...)`).

- id: python-itergen-itertools-03
  answer: |
    `itertools.chain.from_iterable` flattens nested iterables lazily without building intermediate lists. `itertools.islice` selects a slice from an iterator without converting to a list first. Both are preferable because they operate lazily, use C-optimized loops internally, and avoid the memory overhead of an intermediate list.

- id: python-itergen-protocol-04
  answer: |
    An **iterable** implements `__iter__` which returns an iterator. An **iterator** implements both `__iter__` (returning `self`) and `__next__` (returning the next value). The distinction matters because an iterable can be iterated multiple times (each call to `__iter__` creates a new iterator), while an iterator is exhausted after one pass — calling `list(it)` a second time yields `[]`.

- id: python-context-with-01
  answer: |
    The `with` statement guarantees the context manager's `__exit__` method runs when the block is exited — whether normally or via an exception — enabling reliable cleanup. A synchronous context manager implements `__enter__` (setup, return value bound to `as`) and `__exit__` (cleanup, receives exception info if any).

- id: python-context-contextmanager-02
  answer: |
    `@contextlib.contextmanager` wraps a generator function into a context manager. Everything before the `yield` is setup (`__enter__`), the yielded value is the `as` value, and everything after the `yield` is teardown (`__exit__`). The generator must yield exactly once; code after `yield` runs even if an exception occurs in the body (similar to `finally`).

- id: python-context-exitstack-03
  answer: |
    `ExitStack` solves the problem of dynamically managing a variable number of context managers — especially when you don't know at write-time how many you need or which ones. It lets you register context managers in a loop (`.enter_context(cm)`) and guarantees all registered `__exit__` methods run when the `ExitStack` closes, something a fixed nesting of `with` statements can't do.

- id: python-context-async-04
  answer: |
    `async with` is for context managers that perform asynchronous setup/teardown. It uses `__aenter__` (which is `async` and can be awaited) and `__aexit__` (also async). A regular `with` can't do the job because it calls `__enter__`/`__exit__` synchronously — if resource acquisition or release requires `await` (e.g. an async database connection), a synchronous call won't yield control back to the event loop.

- id: python-decorators-basics-01
  answer: |
    A decorator is a callable that takes a callable and returns a replacement callable — syntactic sugar for `func = decorator(func)`. The wrapper should use `functools.wraps` to copy `__name__`, `__doc__`, `__module__`, etc., from the original function, preserving introspection and avoiding confusing debugging output.

- id: python-decorators-args-02
  answer: |
    When a decorator takes arguments like `@retry(times=3)`, the expression `retry(times=3)` must return the actual decorator. So `retry` is a factory function that returns the wrapper/decorator, which in turn wraps the target function. Without the extra nesting, `retry(times=3)` would be called with the target function as its first argument, not with `times`.

- id: python-decorators-stacking-03
  answer: |
    Decorators are applied bottom-up: `@a` then `@b` above `def f` means Python first evaluates `b(f)`, then `a(b(f))`, so `a` is the outermost wrapper. When `f()` is called, `a`'s wrapper runs first (outermost to innermost), which then calls `b`'s wrapper, which finally calls the original `f`.

- id: python-decorators-class-04
  answer: |
    A class decorator takes a class and returns a (usually modified or replaced) class — it can wrap the class, add attributes, register it, or replace it entirely. A real standard-library example is `@functools.total_ordering`, which fills in rich comparison methods (`__lt__`, `__le__`, etc.) based on whichever single comparison you define.

- id: python-datamodel-eqhash-01
  answer: |
    If you define `__eq__` without `__hash__`, the class becomes unhashable (Python sets `__hash__ = None`). `__eq__` and `__hash__` must be consistent: if `a == b`, then `hash(a) == hash(b)` must hold, otherwise objects that compare equal end up in different hash buckets and break sets/dicts. If you override one, override the other.

- id: python-datamodel-slots-02
  answer: |
    `__slots__` tells Python to store instance attributes in a fixed-size array indexed by slot position rather than a per-instance `__dict__`. This reduces memory usage and slightly speeds attribute access. You give up the ability to add arbitrary attributes at runtime and (without extra work) multiple inheritance with classes that also use `__slots__`.

- id: python-datamodel-mutable-03
  answer: |
    `def add(item, target=[]):` creates the list `[]` once at function definition time; every call that uses the default shares and mutates the same list object — a classic "mutable default argument" bug. The correct pattern is `def add(item, target=None): if target is None: target = []`.

- id: python-datamodel-is-04
  answer: |
    `is` tests identity (same object in memory), `==` tests equality (same value). For small integers (typically -5 to 256) and short strings, Python caches singletons, so `a is b` can be `True` even when they were created separately. This is an implementation detail (CPython) — you should never rely on it. Use `==` for value comparison.

- id: python-datamodel-descriptor-05
  answer: |
    A descriptor is an object with `__get__`, `__set__`, or `__delete__` that is an attribute of a class (not an instance). When attribute access occurs on an instance, Python's data model checks the class's `__dict__` (and MRO) for descriptors and invokes them. This explains `property` (which uses `fget`/`fset`/`fdel` descriptors) and bound methods (the `function` descriptor binds `self` via `__get__`).

- id: python-errors-elsefinally-01
  answer: |
    The `else` block runs only if the `try` body completes without raising an exception — it runs before `finally`. `finally` always runs regardless of whether an exception occurred, was caught, or was re-raised. Prefer `else` because it narrows the scope of code protected by `try`, making it clear which operation is the one that might raise and reducing the chance of accidentally catching unrelated exceptions.

- id: python-errors-custom-02
  answer: |
    Define a custom exception by inheriting from `Exception` (or a more specific built-in): `class MyError(Exception): pass`. Catch specific exception types because a bare `except:` catches everything including `SystemExit`, `KeyboardInterrupt`, and `GeneratorExit`, masking real bugs and making debugging much harder.

- id: python-errors-raisefrom-03
  answer: |
    `raise NewError(...) from err` sets `__cause__` on the new exception, explicitly chaining it and showing the full traceback including the original error ("The above exception was the direct cause of the following exception"). A plain `raise NewError(...)` inside an `except` block sets `__context__` implicitly, but `from err` makes the causal relationship explicit and `from None` suppresses the chaining.

- id: python-errors-group-04
  answer: |
    `ExceptionGroup` bundles multiple exceptions into one object, representing concurrent or independent errors. `except*` (Python 3.11+) was added to handle them — it matches each sub-exception in the group independently and can handle a subset, letting unhandled parts propagate. Unlike a normal `except` which catches a single exception and its subclass tree, `except*` decomposes the group and runs matching handlers for each sub-exception, potentially invoking the handler multiple times.
```
