# python knowledge benchmark — answers
# Model: novita-ai/tencent/hy3   (provider-stripped id: tencent/hy3)
# CLOSED-BOOK via ocode run -dir <empty> (no corpus access). corpus_rev 1.

- id: python-types-union-01
  answer: |
    `Optional[int]` is a type hint meaning the value is either an `int` or `None`. It is exactly equivalent to `Union[int, None]` — i.e. `Optional[X]` is just shorthand for `Union[X, None]`.

    With PEP 604 (Python 3.10+) you write the same thing using the new `|` union operator: `int | None`.

- id: python-types-generics-02
  answer: |
    You declare a type variable with `typing.TypeVar` and use it as both the parameter and return type, e.g.:
        from typing import TypeVar
        T = TypeVar("T")
        def identity(x: T) -> T: return x
    This tells the type checker the returned value has the same type as the argument.

    Python 3.12 (PEP 695) added inline type-parameter syntax, so you can write the type parameter directly on the function:
        def identity[T](x: T) -> T: return x
    It also introduced the `type` statement for defining type aliases with parameters, e.g. `type Point[T] = tuple[T, T]`.

- id: python-types-protocol-03
  answer: |
    `typing.Protocol` lets you define a *structural* (duck-typed) interface: a type is considered compatible with the Protocol if it has the required methods/attributes, regardless of whether it inherits from it. Type checkers enforce this statically; you typically do not subclass the Protocol. (Marking it `runtime_checkable()` adds a best-effort `isinstance` check, but it still cannot check methods at runtime.)

    An abstract base class (ABC) enforces *nominal* subtyping: a class must explicitly inherit from the ABC and implement its `@abstractmethod`s, and the check is enforced at runtime (instantiation fails until all abstract methods are implemented). Protocols require no inheritance relationship and are checked by the type checker, not at runtime.

- id: python-types-self-04
  answer: |
    For a method that returns `self` on a subclassable class, the accurate annotation is `typing.Self` (added in Python 3.11):
        from typing import Self
        class Builder:
            def set_x(self, x: int) -> Self:
                self.x = x
                return self
    This lets the type checker infer that a subclass returns an instance of that subclass, preserving chaining types.

    Before `Self` existed, the workaround was a `TypeVar` bound to the class:
        T = TypeVar("T", bound="Builder")
        def set_x(self: T, x: int) -> T: ...
    `Self` is the cleaner, built-in tool for this.

- id: python-dataclasses-basics-01
  answer: |
    `@dataclass` is a class decorator that auto-generates several dunder methods for you: `__init__`, `__repr__`, `__eq__`, and (depending on the flags) comparison methods (`__lt__` etc.) and `__hash__`. It reads the class's annotated attributes to build these.

    You must use `field(default_factory=list)` for a mutable collection default instead of `= []` because default argument values in Python are evaluated once at class-definition time. A literal `= []` would be a single list object shared by every instance, so mutating one instance's list would affect all of them — the classic mutable-default-argument bug. `default_factory=list` calls a fresh `list()` for each new instance.

- id: python-dataclasses-frozen-02
  answer: |
    `@dataclass(frozen=True)` generates an immutable dataclass: instances cannot have their attributes reassigned after creation — any attempt raises `FrozenInstanceError`. (It also implies `eq=True` and disables `__hash__` generation unless `unsafe_hash`/eq settings allow a hash.)

    `__post_init__` is an optional method that the generated `__init__` calls automatically after all fields have been initialized. It is the place to do validation, compute/derive additional fields, or coerce inputs (since you can't easily add logic into the generated `__init__` itself).

- id: python-dataclasses-slots-03
  answer: |
    `@dataclass(slots=True)` (Python 3.10+) makes the generated class define `__slots__`, so instances no longer carry a per-instance `__dict__`. This reduces memory footprint (important when you create many instances) and slightly speeds up attribute access, and prevents accidentally adding unknown attributes.

    The trade-off: you lose the flexibility of dynamic attribute assignment, you can no longer do `vars()`/`__dict__`, pickling and some introspection patterns differ, and multiple inheritance becomes more restrictive (only one slot-bearing class in the MRO may add a `__dict__`, and base classes must cooperate).

- id: python-dataclasses-vs-04
  answer: |
    - `@dataclass`: choose when you want a normal, usually mutable, object-like container with defaults, methods, validation (`__post_init__`), and possibly immutability via `frozen=True`. Good general "record with behavior."
    - `NamedTuple` (or `@dataclass(frozen=True, eq=True)` plus tuple behavior): choose when you want an immutable value/record that also behaves like a tuple (indexable, iterable, unpackable) and is hashable by default. Great for lightweight, self-documenting records.
    - `TypedDict`: choose when you are describing the *shape of an existing dict* (e.g. JSON payloads, API responses, `**kwargs`) for static type checkers. It adds no runtime object — values are still plain dicts — so there's no memory/behavior overhead, just type documentation for dicts with known keys.

- id: python-async-await-01
  answer: |
    Calling an `async def` function does *not* run its body. It immediately returns a coroutine object. The body only executes when the coroutine is driven to completion — by `await`-ing it (which suspends the current coroutine and hands control to the event loop) or by scheduling it as a Task on the loop (e.g. `asyncio.create_task`). If you just call it and never await/schedule it, nothing happens and you'll typically get a "coroutine was never awaited" warning.

- id: python-async-taskgroup-02
  answer: |
    Both run several coroutines concurrently.

    `asyncio.gather(*coros)` runs them together and collects results into a list. By default, if one coroutine raises, `gather` propagates that exception to the caller but does *not* cancel the still-running siblings — they keep running in the background. (You can pass `return_exceptions=True` to gather all results/errors instead.)

    `asyncio.TaskGroup` (Python 3.11, structured concurrency) cancels *all* remaining tasks as soon as any one of them fails, and raises an `ExceptionGroup` containing every error. This gives clean, predictable cleanup and prevents leaked background tasks, which `gather` does not guarantee.

- id: python-async-blocking-03
  answer: |
    `time.sleep(5)` and a synchronous `requests.get(...)` are *blocking* calls: they occupy the single event-loop thread for their whole duration without ever yielding to the loop. While blocked, no other coroutine in that loop can run, so you've destroyed the concurrency you were trying to achieve (effectively making everything serial and unresponsive).

    Instead use the async equivalents: `await asyncio.sleep(5)` for delays, and an async HTTP client such as `aiohttp` or `httpx.AsyncClient` with `await client.get(...)`, so the coroutine suspends and the loop can run other tasks meanwhile.

- id: python-async-cancel-04
  answer: |
    Cancellation is cooperative. Calling `task.cancel()` schedules a `CancelledError` to be raised at the next `await` point inside the coroutine. The coroutine can catch it to clean up resources and should normally re-raise (or just let it propagate) so the task actually terminates. At the task level a `CancelledError` is propagated to the awaiter.

    You should not swallow `CancelledError` (e.g. `except CancelledError: pass` without re-raising) because doing so prevents the task from being cancelled — this breaks timeouts, `TaskGroup` cleanup, and orderly shutdown. That's precisely why, since Python 3.8, `CancelledError` derives from `BaseException` rather than `Exception`, so ordinary `except Exception` handlers don't accidentally catch and swallow it.

- id: python-itergen-yield-01
  answer: |
    A function becomes a generator simply by containing one or more `yield` statements (or `yield from`). Calling it does not execute the body; instead it returns a generator iterator object.

    Execution is lazy and stateful: the body runs only when something iterates it, and it executes up to the next `yield`, returns that value, and pauses, preserving all local state. On the next iteration it resumes right after the `yield`. A list-building function, by contrast, computes every element up front, stores the whole list in memory, and returns it once. Generators produce items one at a time and use far less memory for large/infinite sequences.

- id: python-itergen-genexpr-02
  answer: |
    `[x*x for x in data]` is a list comprehension: it is evaluated immediately and builds and returns a full list held in memory.

    `(x*x for x in data)` is a generator expression: it produces a lazy generator that computes values one at a time, on demand, and holds nothing in memory.

    The second matters when `data` is large or unbounded (memory savings — you never materialize the whole sequence) and when you only need to consume the values once through a pipeline, e.g. `sum(x*x for x in data)` or passing it straight into a function — avoiding an unnecessary intermediate list.

- id: python-itergen-itertools-03
  answer: |
    Two useful `itertools` tools:

    - `itertools.chain(*iterables)`: lazily concatenates multiple iterables into one stream without building an intermediate list. Preferable to a manual loop that appends everything into a list because it's memory-efficient and implemented in C.
    - `itertools.islice(iterable, start, stop, step)`: lazily slices an iterator (which has no indexing) without materializing it into a list.

    Other examples: `count`, `cycle`, `product`, `permutations`, `combinations`, `groupby`, `repeat`. They're preferable to manual list-building loops because they are lazy (constant memory), fast (C implementations), and express intent clearly.

- id: python-itergen-protocol-04
  answer: |
    An object is *iterable* if it implements `__iter__()` (which returns an iterator) or, as a fallback, `__getitem__()` with integer indices starting at 0. An *iterator* implements both `__iter__()` (which returns `self`) and `__next__()` (which returns the next item or raises `StopIteration` when exhausted).

    The distinction matters because iterators are single-pass: once consumed they're exhausted, so iterating the *same iterator object* a second time yields nothing. Iterables, however, can be iterated multiple times because each call to `__iter__()` returns a *fresh* iterator (e.g. a list always gives a new iterator). Confusing the two is a common source of "my loop only runs once" bugs.

- id: python-context-with-01
  answer: |
    The `with` statement guarantees that the context manager's cleanup code runs when control leaves the block — whether normally, via `return`, or via an exception — so resources are reliably released.

    A context manager implements two dunders: `__enter__(self)`, called on entering the block (its return value is what the `as` target is bound to), and `__exit__(self, exc_type, exc_val, exc_tb)`, called on leaving. If `__exit__` returns a truthy value, it suppresses the exception that occurred in the block.

- id: python-context-contextmanager-02
  answer: |
    `@contextlib.contextmanager` turns a generator function into a context manager. The decorator wraps the generator so that:

    - The code *before* the `yield` is the setup, executed when entering the block (and its `__enter__` returns the yielded value, which `as` binds).
    - The `yield` passes out the value to bind.
    - The code *after* the `yield` is the teardown, executed on exit (including if an exception was raised inside the block), because the decorator internally wraps everything in a `try/finally`.

    So setup goes before `yield`, teardown goes after `yield`.

- id: python-context-exitstack-03
  answer: |
    `contextlib.ExitStack` lets you enter a *dynamic* or *runtime-determined* number of context managers and guarantees all of them are exited (in reverse order) correctly. A plain nested `with` requires a fixed, statically-known set of `with` blocks written in the source, which can't handle "enter N managers I only know at runtime," conditionally-entered contexts, or lazy/loop-based acquisition.

    `ExitStack` also supports `enter_context()`, `push()` for arbitrary cleanup callbacks (`callback()`), and deferring cleanup — useful for opening a variable list of files, or for functions that need to acquire several resources in a loop.

- id: python-context-async-04
  answer: |
    `async with` is the asynchronous context-manager protocol: it `await`s the resource setup and teardown, which may themselves need to do I/O. It uses the dunders `__aenter__(self)` and `__aexit__(self, exc_type, exc_val, exc_tb)`, both of which are coroutine methods.

    A regular `with` calls `__enter__`/`__exit__`, which are synchronous and cannot `await`. So for async resources (async DB connections, async file/network handles) you need `async with` — a synchronous `with` couldn't perform the awaitable setup/teardown and would either block the event loop or be unable to acquire the resource correctly.

- id: python-decorators-basics-01
  answer: |
    Fundamentally a decorator is a callable that takes another callable (function or class) and returns a replacement callable — typically a wrapper that adds behavior before/after calling the original, without changing its callers. The syntax `@decorator` is just sugar for `f = decorator(f)`.

    The wrapper should use `functools.wraps(func)` because it copies the original function's metadata (`__name__`, `__doc__`, `__module__`, `__qualname__`, `__dict__`) onto the wrapper and updates the wrapper's `__wrapped__`. Without it, the decorated function masquerades as the wrapper, breaking introspection, logging, debugging (`tracebacks`/`pdb`), and some libraries that rely on these attributes.

- id: python-decorators-args-02
  answer: |
    With `@retry(times=3)`, `retry(times=3)` is *called first* and must return a decorator (a function that then takes the decorated function). So you need three nested levels: an outermost function that accepts the decorator's own arguments and returns the middle "real decorator" function, which takes the `func` and returns the innermost wrapper.

    A plain `@decorator` needs only two levels (the decorator takes `func` directly and returns the wrapper), because there are no extra decorator-time arguments to capture.

- id: python-decorators-stacking-03
  answer: |
    Application (decoration) is bottom-up: `@b` is applied to `f` first, then `@a` wraps the result, so effectively `f = a(b(f))`. `a` becomes the outermost wrapper.

    At call time, the wrappers run outside-in: `a`'s wrapper runs first, then `b`'s, then the original `f`, then control unwinds back through `b` and finally `a`. So the topmost decorator (outermost) executes first on the way in and last on the way out.

- id: python-decorators-class-04
  answer: |
    A class decorator takes a class object and returns a class (often the same one, modified in place, or a new/derived class). It can register the class in a registry, add or wrap methods/attributes, inject behavior, or apply metaprogramming — all without the user writing a metaclass.

    Real stdlib examples: `dataclasses.dataclass` (a class decorator that generates `__init__`/`__repr__`/`__eq__` etc. on the class), `functools.total_ordering` (adds the missing comparison methods), and `enum.unique` (validates that enum members have unique values).

- id: python-datamodel-eqhash-01
  answer: |
    Defining `__eq__` in a class automatically sets `__hash__` to `None`, making instances unhashable (you cannot use them in sets or as dict keys) unless you also explicitly define `__hash__`.

    `__eq__` and `__hash__` must stay consistent: objects that are equal under `__eq__` must produce equal `__hash__` values (equal objects ⇒ equal hashes). If you define a custom `__eq__` but keep or write a `__hash__` that doesn't respect that, equal objects land in different hash buckets and dict/set membership breaks. The default `__hash__` is based on identity, which would contradict a value-based `__eq__`, which is exactly why Python disables `__hash__` for you until you opt in.

- id: python-datamodel-slots-02
  answer: |
    Declaring `__slots__` on a class tells Python not to create a per-instance `__dict__` (and `__weakref__` unless listed); instead it allocates space for exactly the named attributes as fixed slots/descriptors. This uses less memory per instance and gives slightly faster attribute access, and forbids adding attributes not listed in `__slots__`.

    What you give up: no dynamic attribute assignment (can't add new attributes at runtime), no instance `__dict__` (so `vars()` and ad-hoc attributes fail), pickling/introspection differences, and stricter multiple-inheritance rules (only one slot-bearing class in the MRO may define a `__dict__`, and base classes must cooperate or you get a `TypeError`).

- id: python-datamodel-mutable-03
  answer: |
    `def add(item, target=[])` is a bug because default argument values are evaluated exactly once, when the `def` statement runs — not each call. So `target` is a single shared list object across all calls. Mutating it (e.g. `target.append(item)`) persists between calls, so later calls see earlier callers' data.

    Correct pattern: use `None` as the sentinel and create a fresh list inside the function:
        def add(item, target=None):
            if target is None:
                target = []
            target.append(item)
            return target

- id: python-datamodel-is-04
  answer: |
    `is` tests *identity* — whether two names refer to the exact same object in memory (`id(a) == id(b)`). `==` tests *value equality* — whether the objects are equivalent, via `__eq__`.

    For small ints and strings, `a is b` can be surprisingly `True` because CPython *interns/caches* them (small integers, typically -5..256, and some string literals) so the same object is reused. It can be `False` for larger ints/strings that aren't cached, even if their values are equal. This is an implementation detail — you should compare values with `==` and only use `is` for singletons like `None` (i.e. `x is None`).

- id: python-datamodel-descriptor-05
  answer: |
    A descriptor is an object that implements at least one of `__get__`, `__set__`, or `__delete__`, and is stored as a class attribute. When you access that attribute on an instance, Python's attribute-lookup machinery calls the descriptor's `__get__` (and `__set__`/`__delete__` on assignment/deletion) instead of using a plain instance value — this is how Python customizes attribute access.

    This explains `property`: `property(fget, ...)` is a data descriptor whose `__get__`/`__set__` invoke your getter/setter functions, so reading/writing the attribute runs your code. It also explains methods: functions are non-data descriptors whose `__get__` returns a *bound method* (binding the instance as the first `self` argument). Class methods and static methods are likewise implemented via descriptors.

- id: python-errors-elsefinally-01
  answer: |
    In `try/except/else/finally`:
    - `else` runs *only if* the `try` block completed with no exception (and is skipped if an exception was caught or raised).
    - `finally` *always* runs, regardless of how the block exits — normal completion, an exception, a `return`, or a `break`.

    Prefer putting success-only code in `else` rather than appending it to the end of `try`, because code inside `try` can accidentally be caught by your `except`, and code in `else` cannot raise an exception that would be misattributed to the `try` block's protected operation. It keeps the `try` minimal (just the risky operation) and the `except` focused on that operation's failures.

- id: python-errors-custom-02
  answer: |
    Define a custom exception by subclassing `Exception` (usually via a project-specific base exception, which itself subclasses `Exception`), optionally adding useful attributes/arguments:
        class MyError(Exception):
            def __init__(self, msg, code):
                super().__init__(msg)
                self.code = code
    Subclass `Exception`, not `BaseException`, so it's part of the normal catchable hierarchy.

    Catch specific exception types rather than a bare `except:` because a bare `except:` (which is `except BaseException:`) also catches `KeyboardInterrupt`, `SystemExit`, and `GeneratorExit`, preventing Ctrl-C/clean shutdowns and silently masking unrelated bugs. Specific catches handle only what you intend and let real errors propagate.

- id: python-errors-raisefrom-03
  answer: |
    `raise NewError(...) from err` raises `NewError` and explicitly sets its `__cause__` to the original `err`, establishing an explicit exception chain. The traceback then shows both exceptions with a clear "The above exception was the direct cause of the following exception" message, preserving the original error as the root cause.

    A plain `raise NewError(...)` inside an `except` block instead sets the implicit `__context__` (the exception being handled), which the interpreter *may* also display but treats as incidental context rather than an explicit cause. Using `from err` is the deliberate, recommended way to wrap/converter one error into another while keeping the underlying cause visible; you can also write `raise NewError(...) from None` to suppress chaining entirely.

- id: python-errors-group-04
  answer: |
    `ExceptionGroup` (Python 3.11, with `BaseExceptionGroup`) bundles multiple unrelated exceptions into a single exception object so they can be raised together. `except*` is a new handling clause that matches a handler against *each* exception inside a group of a given type, running the handler once per matching exception and collecting any unmatched exceptions into a new group that propagates.

    They were added to support structured concurrency and other scenarios (e.g. `asyncio.TaskGroup`) where several independent operations can fail simultaneously and you need to report/handle all failures rather than just one.

    `except*` differs from a normal `except` in that a single `except*` clause can handle several matching exceptions extracted from a group (possibly executing multiple times), whereas `except` matches at most one exception at the top level and would just catch the group as a whole.
