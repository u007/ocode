# python knowledge benchmark — answers
# Model: opencode-go/muse-spark-1.2   (provider-stripped id: muse-spark-1.2)
# CLOSED-BOOK via ocode run -dir <empty> (no corpus access). corpus_rev 1.

```yaml
- id: python-types-union-01
  answer: |
    `Optional[int]` is an alias for `Union[int, None]` — meaning the value may be either an `int` or `None`. It does NOT mean "optional parameter"; it is just a type that includes None.
    PEP 604 (Python 3.10+) introduced the `X | Y` syntax for unions, so the equivalent is `int | None` (or `int | None = None` for a parameter default). From 3.10+, `Optional[int]` == `int | None`.

- id: python-types-generics-02
  answer: |
    Use a TypeVar to capture the input type:
    ```python
    from typing import TypeVar
    T = TypeVar('T')
    def identity(x: T) -> T: return x
    ```
    This tells the checker that return type is the same as the argument type for any call.

    Python 3.12 (PEP 695) added new generic syntax that removes the need to declare TypeVar explicitly:
    ```python
    def identity[T](x: T) -> T: return x
    class Box[T]:
        def __init__(self, v: T): self.v = v
    type Vec[T] = list[T]
    ```
    The type parameter `T` is declared inline in `[]` after the function/class/type alias name, is scoped to that definition, and can have bounds/constraints: `def f[T: str](x: T)` or `def f[T: (int, str)]`.

- id: python-types-protocol-03
  answer: |
    `typing.Protocol` defines structural subtyping (static duck typing). You declare the methods/attributes a type must have; any class that implements that structure is considered compatible by a type checker, regardless of inheritance.

    Difference from Abstract Base Class (ABC):
    - ABC is nominal: you must explicitly inherit from `ABC`/`MyABC` and implement abstract methods; `isinstance` works via inheritance or explicit `register`.
    - Protocol is structural: no inheritance required. A class outside your hierarchy that happens to have `def draw(self): ...` matches `class Drawable(Protocol): def draw(self): ...` at type-check time.
    At runtime Protocols are erased unless decorated with `@runtime_checkable`, in which case `isinstance(obj, MyProtocol)` does a shallow attribute check.

- id: python-types-self-04
  answer: |
    Annotating as `-> MyClass` is inaccurate for subclasses — calling `SubClass().chain()` would be typed as `MyClass`, losing the subclass type.

    Correct patterns:
    1. Old (still valid): `Self`-bound TypeVar:
       ```python
       from typing import TypeVar
       Self = TypeVar('Self', bound='MyClass')
       class MyClass:
           def chain(self: Self) -> Self: return self
       ```
    2. New (PEP 673, Python 3.11+): `typing.Self` (or `typing_extensions.Self`):
       ```python
       from typing import Self
       class MyClass:
           def chain(self) -> Self: return self
           @classmethod
           def create(cls) -> Self: return cls()
       ```
    `Self` always means "the type of `self`/`cls` as seen by the caller", so `Sub().chain()` is correctly typed as `Sub`.

- id: python-dataclasses-basics-01
  answer: |
    `@dataclass` auto-generates `__init__`, `__repr__`, `__eq__` (and optionally `__hash__`, `__lt__` etc) from annotated fields, plus other boilerplate. You just write:
    ```python
    @dataclass
    class Point: x: int; y: int
    ```

    You must use `field(default_factory=list)` instead of `= []` because default values are evaluated once at class definition time. With `= []` every instance would share the SAME list object (mutable default bug). `default_factory=list` calls `list()` anew for each instance, giving each its own empty list. Same for `dict`, `set`, or any mutable default.

- id: python-dataclasses-frozen-02
  answer: |
    `@dataclass(frozen=True)` makes instances immutable (like a tuple): fields become read-only after `__init__` — assignment raises `FrozenInstanceError` — and the class gets a `__hash__` (if not otherwise unhashable) so it can be used in sets/dicts, assuming fields are hashable.

    `__post_init__` is a hook called at the end of the generated `__init__` after fields are initialized. Use it for validation, derived attributes, or initialization that needs logic beyond simple assignment. In a frozen dataclass you must use `object.__setattr__(self, 'field', value)` inside `__post_init__` to set derived fields.

- id: python-dataclasses-slots-03
  answer: |
    `@dataclass(slots=True)` (Python 3.10+) generates `__slots__` for the dataclass, so instances don't have a per-instance `__dict__` but store fields in a compact fixed slot array.

    Benefits: lower memory per instance (significant for many instances), faster attribute access, prevents accidental attribute creation via typos (`obj.typo = 1` raises AttributeError).

    Trade-off: no `__dict__` (unless you explicitly include `'__dict__'` in slots), no dynamic attributes, weaker compatibility with some libraries that expect `__dict__`, multiple inheritance with slots can be tricky, and pickling/weakref needs `weakref` slot if needed. Also `slots=True` is mutually exclusive with some patterns that set arbitrary attributes.

- id: python-dataclasses-vs-04
  answer: |
    - `@dataclass`: general mutable (or frozen) record with named fields, defaults, validation, methods. Choose for most domain objects/entities where you want a readable, mutable container with behavior. Can be frozen/slots if needed.
    - `NamedTuple` (`typing.NamedTuple` or `collections.namedtuple`): immutable, tuple subclass. Choose when you need tuple semantics — unpacking, immutability, hashability, positional + named access, lightweight value object that is also a tuple (e.g., `Point = NamedTuple('Point', [('x',int),('y',int)])`). Use when you want `isinstance(x, tuple)` and small memory footprint.
    - `TypedDict`: describes a `dict` with specific string keys and value types. No actual class is created — it's just a type hint for plain dicts, typically for JSON/object literals. Choose when data comes from/is going to JSON APIs, configs, or you need `dict` at runtime but want static checking of keys: `class Movie(TypedDict): title: str; year: int`.

- id: python-async-await-01
  answer: |
    Calling an `async def` function does NOT execute its body; it immediately returns a `coroutine` object. Nothing inside runs until the coroutine is awaited (`await coro`) or scheduled on an event loop (`asyncio.create_task(coro)`, `asyncio.run(coro)`).

    This is lazy execution: the coroutine is a state machine suspended at the start. `await` drives it forward, yielding control to the event loop at each `await` point until it completes and produces a result. Without awaiting/scheduling, the coroutine is never stepped and is eventually garbage-collected with a "coroutine was never awaited" warning.

- id: python-async-taskgroup-02
  answer: |
    Both run coroutines concurrently on the same event loop.

    `asyncio.gather(*coros, return_exceptions=False)` schedules all, returns when all complete (or first failure propagates). If one fails, others continue to run in the background; you must handle cancellation manually. With `return_exceptions=True` failures are returned as values, not raised.

    `asyncio.TaskGroup` (Python 3.11, PEP 584): structured concurrency via `async with TaskGroup() as tg: tg.create_task(...)`. If any task raises, all other tasks in the group are automatically cancelled, and on exit all remaining tasks are awaited. Failures are combined into an `ExceptionGroup` (PEP 654) if multiple tasks fail. It ensures no orphaned tasks and enforces the "parent waits for children" invariant, so preferred for modern code.

- id: python-async-blocking-03
  answer: |
    An asyncio event loop is single-threaded cooperative: only one coroutine runs at a time, and it must `await` to yield control. `time.sleep(5)` or `requests.get(...)` are blocking synchronous calls that hold the thread for seconds without yielding, freezing the entire loop — no other task, callback, or I/O can progress.

    Instead use non-blocking equivalents that yield:
    - `await asyncio.sleep(5)` instead of `time.sleep(5)`
    - `await aiohttp`/`httpx.AsyncClient` etc instead of `requests`; or run blocking work in a thread: `await asyncio.to_thread(requests.get, url)` or `await loop.run_in_executor(None, blocking_fn)`, or use `await asyncio.to_thread` for CPU-bound work.

- id: python-async-cancel-04
  answer: |
    Cancellation in asyncio is cooperative via `task.cancel()`, which injects a `CancelledError` (subclass of `BaseException` since 3.8, not `Exception`) at the next `await` inside the target task. The task should let it propagate so the cancellation completes.

    You should not swallow `CancelledError` with a bare `except Exception:` or `except CancelledError: pass` because:
    1. It prevents the task from actually cancelling — the task appears to succeed, breaking `TaskGroup`/`gather` cancellation and shutdown logic.
    2. Since it's a `BaseException`, a generic `except Exception` won't catch it anyway (if correctly inheriting), but `except BaseException` will.

    Correct pattern: if you must catch it for cleanup, re-raise:
    ```python
    try: await work()
    except asyncio.CancelledError:
        await cleanup()
        raise
    ```
    or use `try/finally` for cleanup without catching.

- id: python-itergen-yield-01
  answer: |
    A function containing `yield` (or `yield from`) becomes a generator function. Calling it does NOT run the body; it returns a generator iterator.

    Execution differs:
    - Regular function: runs eagerly start-to-finish, builds whole list in memory, returns it.
    - Generator: runs lazily, suspended at each `yield`. Each `next(gen)` resumes after the last `yield`, emits one value, then suspends again, maintaining local state. This yields O(1) memory and allows infinite sequences, pipelining, and early termination without building the full collection. Generator is also single-pass — once exhausted, it's done.

- id: python-itergen-genexpr-02
  answer: |
    `[x*x for x in data]` is a list comprehension: builds a full `list` in memory immediately.
    `(x*x for x in data)` is a generator expression: returns a lazy generator iterator that computes items on demand.

    The second matters when: data is large (avoids O(n) memory), you only need one-pass iteration, you want to pipeline without intermediate lists (e.g., `sum(x*x for x in data)`), or the source is infinite. It's also the only option for infinite or very large streams. Use list comprehension when you need indexing, len(), multiple passes, or to mutate the result.

- id: python-itergen-itertools-03
  answer: |
    Examples:
    - `itertools.chain(*iterables)` — flattens iterables lazily without building a new list; more efficient and readable than manual nested loops appending to a list.
    - `itertools.islice(iterable, start, stop, step)` — slices any iterator lazily without materializing; manual loop would need counters and breaks.
    Other good examples: `itertools.groupby`, `itertools.product`, `itertools.accumulate`, `itertools.zip_longest`.

    They are preferable because: (1) implemented in C, faster; (2) lazy — O(1) memory, work with infinite iterators; (3) more declarative/correct, avoiding hand-rolled bookkeeping and off-by-one errors.

- id: python-itergen-protocol-04
  answer: |
    - Iterable: has `__iter__` returning an iterator (e.g., list, str, range). Used by `for x in obj:` which calls `iter(obj)` to get an iterator.
    - Iterator: has both `__next__` (returns next item or raises StopIteration) AND `__iter__` returning `self`.

    Distinction matters because iterables can be iterated many times (each `iter()` gives a fresh iterator with independent state), while an iterator is stateful and single-pass, exhausted after one traversal. Re-using the same iterator twice yields nothing the second time:
    ```python
    it = iter([1,2]); list(it) # [1,2]; list(it) # []
    lst = [1,2]; list(lst) # [1,2] twice works
    ```
    Functions that need multiple passes should take an Iterable (or re-create iterator), not a bare iterator.

- id: python-context-with-01
  answer: |
    `with` guarantees setup and teardown regardless of whether the block exits normally or via exception/return, like a `try/finally`. It ensures resource acquisition and release (close files, release locks, rollback transactions).

    A context manager implements:
    - `__enter__(self)` — called on entry, its return value is bound to `as var`
    - `__exit__(self, exc_type, exc_val, exc_tb)` — called on exit; if it returns True it suppresses the exception. `bool` return controls suppression.

- id: python-context-contextmanager-02
  answer: |
    `@contextlib.contextmanager` lets you define a context manager as a generator function instead of a class:
    ```python
    from contextlib import contextmanager
    @contextmanager
    def my_ctx():
        setup()          # code before yield = __enter__
        try:
            yield value  # value bound to `as var`; block runs here
        finally:
            teardown()   # code after yield = __exit__ cleanup, always runs
    ```
    Everything before `yield` is setup, the `yield` value is the `__enter__` result, and everything after `yield` (usually in `finally`) is teardown executed on exit, even if an exception occurred. Exceptions raised inside the `with` block are re-thrown at the `yield` point, so you can `try/except` around `yield`.

- id: python-context-exitstack-03
  answer: |
    `contextlib.ExitStack` manages a dynamic, variable number of context managers when you don't know at write-time how many or which ones you need.

    Plain nested `with` requires statically written nesting: `with A() as a: with B() as b: ...`. ExitStack lets you enter contexts in a loop/conditionally and guarantees LIFO cleanup even if setup fails part-way:
    ```python
    with ExitStack() as stack:
        for fname in files:
            stack.enter_context(open(fname))
        # all files closed on exit in reverse order
    ```
    It also allows `stack.callback(fn)` to register arbitrary cleanups. Without it you'd need messy manual try/finally or dynamically generated nested withs.

- id: python-context-async-04
  answer: |
    `async with` is for asynchronous context managers where enter/exit need to await (e.g., async locks, db connections, aiohttp sessions).

    It uses:
    - `__aenter__(self)` (async) — awaited on entry
    - `__aexit__(self, exc_type, exc_val, exc_tb)` (async) — awaited on exit

    A regular `with` (`__enter__`/`__exit__`) cannot `await` inside, so it would block the event loop if setup/teardown needs I/O (e.g., `await db.acquire()`). `async with` yields control while awaiting those operations, keeping the loop responsive.

- id: python-decorators-basics-01
  answer: |
    A decorator is fundamentally a callable that takes a function (or class) and returns a (usually wrapped) callable to replace it. Syntax `@dec` above `def f` is sugar for `f = dec(f)`.

    The wrapper should use `functools.wraps(f)` to copy `__name__`, `__qualname__`, `__doc__`, `__module__`, `__wrapped__`, and `__dict__` from the original to the wrapper. Without it, introspection/debugging shows the wrapper's name (`wrapper` instead of `f`), docstrings and signatures are lost, and tools like `help()`, `inspect.signature`, and stacked decorators break.

- id: python-decorators-args-02
  answer: |
    A plain decorator has signature `decorator(func) -> func`. A parameterized decorator like `@retry(times=3)` must be called first with its arguments to RETURN the actual decorator.

    So you need three levels:
    ```python
    def retry(times=3):
        def decorator(func):          # real decorator
            @wraps(func)
            def wrapper(*a, **kw):    # replacement
                ...
            return wrapper
        return decorator
    ```
    `retry(times=3)` executes at decoration time, returns `decorator`, then Python does `f = decorator(f)`. Without the outer layer there is nowhere to capture `times`.

- id: python-decorators-stacking-03
  answer: |
    Given:
    ```python
    @a
    @b
    def f(): pass
    ```
    Decoration (application) is bottom-up: `f = a(b(f))` — `b` is applied first, then `a` wraps the result of `b`.

    Execution (call-time) is top-down/outside-in: calling `f()` first enters `a`'s wrapper, which calls `b`'s wrapper, which calls the original `f`. So order is `a` → `b` → `f` → `b` post → `a` post.

- id: python-decorators-class-04
  answer: |
    A class decorator takes a class object and returns a (possibly modified or replaced) class. It can add/mutate attributes, wrap methods, register the class, enforce invariants, or return a different class entirely.

    Real standard-library example: `@dataclass` (from `dataclasses`):
    ```python
    from dataclasses import dataclass
    @dataclass
    class Point: x: int; y: int
    ```
    Other examples: `@enum.unique`, `@functools.total_ordering` (generates missing comparison methods from `__eq__`+`__lt__`), `@typing.runtime_checkable`.

- id: python-datamodel-eqhash-01
  answer: |
    If you define `__eq__` without defining `__hash__`, Python sets `__hash__ = None`, making instances unhashable (`TypeError` on `hash(obj)` or use in set/dict). This is intentional to preserve the invariant: objects that compare equal must have equal hashes.

    You must keep `__eq__` and `__hash__` consistent: `a == b` ⇒ `hash(a) == hash(b)`. If you define custom equality, you must either:
    - Define a compatible `__hash__` based on same fields, or
    - Explicitly set `__hash__ = None` if the type should remain unhashable (mutable objects), or
    - For dataclasses use `unsafe_hash=True`/`frozen=True` to auto-generate a consistent hash.

- id: python-datamodel-slots-02
  answer: |
    Declaring `__slots__ = ('x','y')` tells Python to allocate fixed slot descriptors for those attributes instead of a per-instance `__dict__`. Instances store values in a compact array; attribute access goes via descriptor, not dict lookup.

    Benefits: much lower memory per instance, faster access, prevents arbitrary attribute assignment.

    What you give up: no `__dict__` by default (no dynamic attributes, `vars(obj)` fails), no `__weakref__` unless included, complications with multiple inheritance (all bases must have compatible slots), and you must declare every instance attribute upfront. Pickling still works but some meta-programming expecting `__dict__` breaks.

- id: python-datamodel-mutable-03
  answer: |
    `def add(item, target=[]):` is a bug because default argument values are evaluated ONCE at function definition time, not per call. The single list object is shared across all calls, so `add(1)` then `add(2)` accumulates to `[1,2]` unexpectedly.

    Correct pattern: use `None` sentinel and create new list inside:
    ```python
    def add(item, target=None):
        if target is None:
            target = []
        target.append(item)
        return target
    ```
    Same applies to `dict`, `set`, or any mutable default.

- id: python-datamodel-is-04
  answer: |
    `is` tests identity — same object in memory (`id(a) == id(b)`). `==` tests equality via `__eq__` — value equivalence (can be overridden).

    `a is b` can be surprisingly True for small ints and strings due to interning/caching: CPython caches small ints (-5..256) and interns some strings, so `a=256; b=256; a is b` may be True, but `a=257; b=257; a is b` is typically False (implementation detail, not guaranteed). Never use `is` for value comparison except for singletons `None`, `True`, `False` (`if x is None`).

- id: python-datamodel-descriptor-05
  answer: |
    A descriptor is any object that implements `__get__`, `__set__`, or `__delete__` (data descriptor has `__set__`, non-data has only `__get__`). When such an object is a class attribute, attribute access `obj.attr` invokes the descriptor protocol instead of returning the object itself.

    - `property` is a data descriptor: `@property` creates a descriptor whose `__get__` calls the getter, `__set__` calls the setter, allowing `obj.x` to run a function.
    - Methods are non-data descriptors: functions have `__get__` that binds `self` and returns a `method` object. That's why `obj.method` automatically passes `obj` as first argument — `function.__get__(obj, cls)` creates a bound method. `staticmethod`/`classmethod` are also descriptors with different `__get__` logic.

- id: python-errors-elsefinally-01
  answer: |
    ```python
    try: risky()
    except SomeError: handle()
    else: no_error_code()
    finally: always()
    ```
    - `else` runs ONLY if the `try` block completed WITHOUT raising (and without `return`/`break` bypass). It does not run if an exception was raised or if `try` didn't finish.
    - `finally` runs ALWAYS — whether there was an exception, return, break, or not — used for cleanup. It even runs if `except`/`else` raises.

    Prefer `else` over putting code in `try` to avoid accidentally catching exceptions from the success-path code that you didn't intend to handle. Keeping `try` minimal makes it clear which operation the `except` guards.

- id: python-errors-custom-02
  answer: |
    Define by subclassing `Exception` (or a more specific builtin like `ValueError`):
    ```python
    class MyError(ValueError):
        pass
    # or with context:
    class AppError(Exception):
        def __init__(self, msg, code): super().__init__(msg); self.code = code
    ```

    Catch specific types rather than bare `except:` because bare `except:` catches `BaseException` including `KeyboardInterrupt`, `SystemExit`, `CancelledError`, masking bugs and preventing clean shutdown. Specific catches (`except ValueError:`) handle only expected failures, let unexpected bugs propagate visibly, and allow different handling per error kind.

- id: python-errors-raisefrom-03
  answer: |
    `raise NewError(...) from err` explicitly chains exceptions, setting `__cause__ = err`. Python prints both: `The above exception was the direct cause of the following exception:` with full traceback of the original `err`.

    Plain `raise NewError(...)` inside `except` still implicitly chains via `__context__` (shows "During handling of the above exception, another exception occurred:"), but it's implicit and can be confusing. `raise ... from err` makes the causation explicit and intentional. You can also use `raise NewError(...) from None` to suppress the original context/cause and show only the new exception.

- id: python-errors-group-04
  answer: |
    `ExceptionGroup` (Python 3.11, PEP 654) lets one `raise` carry multiple unrelated exceptions together: `raise ExceptionGroup("msg", [err1, err2])`. Needed because concurrent tasks (TaskGroup) or multiple validation errors can fail simultaneously — a single `raise` couldn't represent that before.

    `except*` is the matching handler that filters an ExceptionGroup:
    ```python
    try: ...
    except* ValueError as eg: handle values
    except* TypeError as eg: handle types
    ```
    Difference from normal `except`: `except` matches the whole raised exception; if it's an ExceptionGroup it must match the group as a whole. `except*` extracts matching sub-exceptions from the group, handles them, and leaves non-matching ones to propagate (possibly as a remaining ExceptionGroup). Multiple `except*` clauses can each handle a subset of the same group, which `except` cannot do.

```
