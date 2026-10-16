# nvidia/nemotron-3.5-super-vl — closed-book answers for python (openrouter, 2026-10-16); answerer saw only _prompts/python.md
- id: python-types-union-01
  answer: |
    `Optional[int]` means a value that is either an `int` or `None`; it is shorthand for `Union[int, None]`. With PEP 604 syntax (Python 3.10+), write the same thing as `int | None`.
- id: python-types-generics-02
  answer: |
    Use a `TypeVar`: declare `T = TypeVar("T")` and annotate the function as `def first(items: list[T]) -> T: ...`, so the return type matches the type of what is received. Python 3.12 (PEP 695) added inline type parameters: `def first[T](items: list[T]) -> T: ...`, letting you declare `T` directly in square brackets after the function name instead of creating a `TypeVar` separately; the same syntax works on classes.
- id: python-types-protocol-03
  answer: |
    A `typing.Protocol` defines a structural interface: a type is a subtype if it has the required members and signatures, regardless of whether it explicitly inherits from the Protocol (static duck typing, checked by type checkers). An ABC uses nominal subtyping — a class must explicitly subclass it (or register), and the ABC can enforce abstract methods at runtime. Protocols require no inheritance and can match existing classes after the fact.
- id: python-types-self-04
  answer: |
    Annotate the return type as `Self` (PEP 673; `typing.Self` since Python 3.11, `typing_extensions.Self` before), e.g. `def with_name(self, name: str) -> Self: ...`. Type checkers then infer the actual subclass type when the method is called on a subclass, so chaining preserves the subclass type. The older approach is a `TypeVar` bound to the class (`T = TypeVar("T", bound="MyClass")`) with `-> T` and a forward-quoted class name.
- id: python-dataclasses-basics-01
  answer: |
    `@dataclass` generates `__init__` (from field annotations), `__repr__`, and `__eq__` (plus ordering methods with `order=True`, and `__hash__` behavior controlled by `frozen`/`unsafe_hash`). A default of `= []` is evaluated once at class-definition time and shared by every instance, so mutations leak between instances; `field(default_factory=list)` calls `list()` fresh for each instance, giving each object its own independent list.
- id: python-dataclasses-frozen-02
  answer: |
    `frozen=True` makes instances immutable: assigning to fields raises `FrozenInstanceError`, and the class gets `__hash__` by default so instances are hashable. `__post_init__` is a hook called at the end of the generated `__init__` to run post-initialization logic such as validation or deriving fields from others. With `frozen=True`, setting fields inside it requires `object.__setattr__(self, "field", value)`.
- id: python-dataclasses-slots-03
  answer: |
    `slots=True` makes the generated class declare `__slots__` instead of a per-instance `__dict__`, reducing memory use and speeding attribute access. Trade-off: instances cannot gain attributes at runtime, fields must be known up front, multiple inheritance is restricted (only one parent with non-empty slots unless others also use slots, or a `__dict__` appears), and some dynamic patterns break.
- id: python-dataclasses-vs-04
  answer: |
    Choose `@dataclass` for general mutable (or frozen) data objects with generated `__init__`/`__repr__`/`__eq__`, methods, validation, and defaults. Choose `NamedTuple` for immutable, tuple-like records where positional access, tuple unpacking, and subclassing `tuple` matter. Choose `TypedDict` for dict-like objects with string keys and a fixed value-type schema, e.g. JSON payloads accessed by key.
- id: python-async-await-01
  answer: |
    Calling an `async def` function returns a coroutine object without executing the body. Nothing happens until you `await` it from another async context or schedule it on the event loop (e.g. `asyncio.create_task(coro)`), which drives the coroutine forward step by step at its await points.
- id: python-async-taskgroup-02
  answer: |
    `asyncio.gather` runs awaitables concurrently and returns results in order; if one fails, it propagates that exception but does not cancel the others — they keep running detached unless you keep references and cancel them yourself. `asyncio.TaskGroup` (Python 3.11+, structured concurrency) cancels all sibling tasks when one fails, waits for every task to finish, then raises an `ExceptionGroup` containing all failures, so no task silently leaks.
- id: python-async-blocking-03
  answer: |
    `time.sleep(5)` or a synchronous `requests.get(...)` block the single thread running the event loop, so no other task can make progress during the call and concurrency freezes. Instead use async equivalents (`asyncio.sleep`, `aiohttp`/`httpx`), or offload blocking work to a thread pool with `asyncio.to_thread(...)` / `loop.run_in_executor` and await the result.
- id: python-async-cancel-04
  answer: |
    Cancellation is cooperative: `task.cancel()` sets a flag, and `asyncio.CancelledError` is raised into the task at its next `await` (it can also surface in `except`/`finally` blocks). The task should clean up and let the error propagate rather than swallow it. Don't catch and discard `CancelledError` — in Python 3.8+ it is a `BaseException`, not an `Exception` — because swallowing it prevents shutdown and makes the task look like it completed successfully.
- id: python-itergen-yield-01
  answer: |
    A function containing `yield` is a generator function: calling it returns a generator iterator without running the body. Each `next()` resumes after the last `yield`, produces one value, and suspends again, preserving local state between yields. It streams items lazily one at a time instead of building and returning an entire list in memory at once.
- id: python-itergen-genexpr-02
  answer: |
    `[x*x for x in data]` is a list comprehension: it builds the full list immediately (eager, in memory). `(x*x for x in data)` is a generator expression: it returns a lazy iterator that computes each value on demand. The second matters for large or infinite inputs, memory efficiency, and chaining operations without materializing intermediate lists.
- id: python-itergen-itertools-03
  answer: |
    `itertools.chain(*iterables)` flattens several iterables into one sequence without building an intermediate list; `itertools.islice(iterable, n)` slices an iterator lazily without indexing or converting to a list (others: `groupby`, `product`, `tee`). They are preferable because they are lazy (constant memory for the stream), composable, and replace manual index/bookkeeping loops.
- id: python-itergen-protocol-04
  answer: |
    Iterable: implements `__iter__`, returning a fresh iterator each time (e.g. `list`, `str`). Iterator: implements `__next__` to return the next item, raising `StopIteration` at the end, and `__iter__` returning itself. The distinction matters because iterators are stateful and exhaustible — iterating the same iterator twice yields nothing the second time — while an iterable can be looped repeatedly since each pass gets a new iterator.
- id: python-context-with-01
  answer: |
    `with` guarantees that cleanup runs when the block exits, whether normally, via `return`, or via an exception. It is implemented by `__enter__` (setup, called on entry; its return value is bound to the `as` target) and `__exit__(exc_type, exc, tb)` (teardown, called on exit; returning a true value suppresses the exception).
- id: python-context-contextmanager-02
  answer: |
    `@contextlib.contextmanager` turns a generator into a context manager: code before `yield` is setup, the yielded object is the resource bound to `as`, and code after `yield` is teardown, which runs when the block exits. An exception raised in the block is re-raised at the `yield` point, so the generator can catch it or run cleanup.
- id: python-context-exitstack-03
  answer: |
    `contextlib.ExitStack` manages a dynamic number of context managers and plain callbacks — you can enter contexts conditionally or in a loop when the count is only known at runtime, and register callbacks for arbitrary cleanup. A plain nested `with` requires a fixed, statically known set of contexts at write time and cannot easily add cleanup later in the block.
- id: python-context-async-04
  answer: |
    `async with` is the async counterpart for resources whose setup/teardown must await. It uses `__aenter__` and `__aexit__` (awaitable dunder methods) instead of `__enter__`/`__exit__`. A regular `with` cannot `await`, so it cannot drive async setup or teardown, e.g. closing an `aiohttp` session or an async DB connection.
- id: python-decorators-basics-01
  answer: |
    A decorator is a callable that takes a function and returns a new (usually wrapping) function — `f = decorator(f)` under the syntactic sugar. The wrapper should use `functools.wraps` to copy `__name__`, `__doc__`, `__qualname__`, `__module__`, `__annotations__` and set `__wrapped__`, preserving introspection, `help()`, and debuggability of the original function.
- id: python-decorators-args-02
  answer: |
    `@retry(times=3)` means `retry(3)` is invoked first and must return the actual decorator, which is then applied to the function. The extra nesting is required because a decorator that takes arguments must accept those arguments and return a decorator callable; a plain decorator receives the function directly, so only one level is needed.
- id: python-decorators-stacking-03
  answer: |
    Stacked `@a` over `@b` means `f = a(b(f))`: decorators apply bottom-up (`b` first, then `a`). At call time the wrappers run top-down: `a`'s wrapper runs first (outermost), then `b`'s, then the original `f` — so `b`'s wrapper is innermost, closest to the real function.
- id: python-decorators-class-04
  answer: |
    A class decorator receives the class object and can modify it (add or change methods, set attributes), wrap or replace it, or register it somewhere; it runs at class definition time. Stdlib example: `functools.total_ordering`, which fills in missing ordering methods from `__eq__` plus one defined comparison. `@dataclasses.dataclass` is another.
- id: python-datamodel-eqhash-01
  answer: |
    Defining `__eq__` sets `__hash__` to `None`, making instances unhashable, unless you also define `__hash__`. This enforces the rule that equal objects must have equal hashes — otherwise `dict`/`set` lookups break, because an object inserted under one hash could not be found by an equal object with a different hash. Keep them consistent: `a == b` implies `hash(a) == hash(b)`.
- id: python-datamodel-slots-02
  answer: |
    `__slots__` declares a fixed set of instance attributes stored in a slot structure instead of a per-instance `__dict__`, saving memory and preventing accidental creation of misspelled attributes. You give up dynamic attributes (no `__dict__` unless added to slots), `__weakref__` support unless declared, some multiple-inheritance flexibility, and the ability to add attributes at runtime.
- id: python-datamodel-mutable-03
  answer: |
    Default arguments are evaluated once at function-definition time, so `target=[]` creates one list shared by every call — appends persist and surprise later callers. Correct pattern: `def add(item, target=None): if target is None: target = []`, so a fresh list is created per call when none is supplied.
- id: python-datamodel-is-04
  answer: |
    `is` checks object identity (same `id`, same object in memory); `==` checks value equality via `__eq__`. `a is b` can be true for equal small ints (typically -5..256) or short strings only because CPython caches/interns them — an implementation detail, not a guarantee — so it can be surprisingly True or False. Use `==` for value comparison and `is` only for singletons like `None`.
- id: python-datamodel-descriptor-05
  answer: |
    A descriptor is an object defining `__get__` (and optionally `__set__`/`__delete__`) used as a class attribute; attribute access on instances invokes the descriptor protocol. `property` is a descriptor (wrapping fget/fset/fdel), and plain functions are descriptors too — `__get__` returns a bound method, which is how `self` is passed automatically.
- id: python-errors-elsefinally-01
  answer: |
    `else` runs only when the `try` block completes with no exception; `finally` runs always — after `try`, `except`, or `else`, on `return`, and even when an exception propagates. Prefer `else` for code that should run only on success because code placed in `try` would have its own exceptions caught by the following `except`, masking bugs.
- id: python-errors-custom-02
  answer: |
    Define a custom exception by subclassing `Exception` (commonly a module-level base exception, then more specific subclasses), calling `super().__init__(message)`. Catch specific exception types rather than bare `except:` because a bare clause catches `KeyboardInterrupt`, `SystemExit`, `GeneratorExit`, and genuine programming errors, making programs un-interruptible and hiding real bugs.
- id: python-errors-raisefrom-03
  answer: |
    `raise NewError(...) from err` explicitly chains the new exception to `err` via `__cause__`, so tracebacks show both the original and the new exception ("During handling of the above exception..."). A plain `raise NewError(...)` inside `except` only sets `__context__` implicitly (implicit chaining); a bare `raise` re-raises the original exception unchanged.
- id: python-errors-group-04
  answer: |
    `ExceptionGroup` (PEP 654, Python 3.11) wraps multiple exceptions into one, and `except*` matches and handles subgroups by type — several `except*` clauses can each catch the matching subset in a single statement. They were added for concurrent/parallel failures (e.g. `asyncio.TaskGroup`) where several errors can occur at once. A normal `except` treats the group as one value and catches it wholesale or not at all; `except*` can selectively handle individual exceptions inside it.
