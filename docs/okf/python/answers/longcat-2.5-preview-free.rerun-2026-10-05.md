- id: python-types-union-01
  answer: |
    Optional[int] means the value is either an int or None. With PEP 604 syntax you write it as `int | None`.

- id: python-types-generics-02
  answer: |
    Use a TypeVar: `T = TypeVar('T')` then `def f(x: T) -> T: ...`. Python 3.12 (PEP 695) introduced the `def f[T](x: T) -> T` syntax, allowing type parameters to be declared directly on the function without a separate TypeVar assignment.

- id: python-types-protocol-03
  answer: |
    A typing.Protocol defines structural subtyping — an object satisfies the protocol if it has the required methods/attributes, regardless of inheritance. Unlike an ABC, you don't need to explicitly inherit from the protocol; any matching class is considered a subtype automatically.

- id: python-types-self-04
  answer: |
    Use `typing.Self` (Python 3.11+) as the return annotation: `def copy(self) -> Self: ...`. Before Self, you'd use a TypeVar bound to the class (`T = TypeVar('T', bound='MyClass')`). Self is the modern, cleaner tool.

- id: python-dataclasses-basics-01
  answer: |
    @dataclass auto-generates __init__, __repr__, __eq__, and (by default) __hash__. A list default must use field(default_factory=list) because default values are evaluated once at class definition time — using `= []` would share the same list across all instances, causing bugs.

- id: python-dataclasses-frozen-02
  answer: |
    frozen=True makes instances immutable — you cannot assign to fields after creation, and the class gets a proper __hash__. __post_init__ is called after the generated __init__ and is used for validation, computed fields, or any logic that needs the fully-initialized instance.

- id: python-dataclasses-slots-03
  answer: |
    slots=True adds __slots__ to the class, which reduces memory overhead (no per-instance __dict__) and speeds up attribute access. The trade-off is you can no longer add arbitrary new attributes to instances, and it can complicate multiple inheritance with non-slotted classes.

- id: python-dataclasses-vs-04
  answer: |
    Use @dataclass for mutable data containers with generated methods. Use NamedTuple when you want an immutable, tuple-like object with named fields. Use TypedDict when you need a dict with a fixed set of typed keys (e.g., for JSON-like data).

- id: python-async-await-01
  answer: |
    Calling an async def function returns a coroutine object — no code inside the function body runs yet. Nothing happens until you await the coroutine or schedule it on the event loop (e.g., with asyncio.create_task or asyncio.gather), because coroutines are lazy.

- id: python-async-taskgroup-02
  answer: |
    asyncio.gather runs coroutines concurrently but if one fails, the others keep running (unless you pass return_exceptions=True). asyncio.TaskGroup (3.11+) runs them concurrently but if any child fails, it cancels all remaining children and waits for them, then raises an ExceptionGroup — giving structured, deterministic failure handling.

- id: python-async-blocking-03
  answer: |
    time.sleep(5) or requests.get(...) blocks the entire event loop thread, preventing other coroutines from making progress — defeating the purpose of async. Use `await asyncio.sleep(5)` or an async HTTP library like aiohttp instead.

- id: python-async-cancel-04
  answer: |
    When a task is cancelled, asyncio raises CancelledError into the coroutine at the current await point. You should not swallow it (bare except or catching it without re-raising) because that prevents the task from actually stopping and breaks the cancellation contract — cleanup code in finally blocks may not run correctly.

- id: python-itergen-yield-01
  answer: |
    A function becomes a generator when it contains `yield`. Unlike a function that builds and returns a list (computing everything eagerly and holding it all in memory), a generator produces items one at a time lazily, pausing between yields and resuming where it left off.

- id: python-itergen-genexpr-02
  answer: |
    `[x*x for x in data]` is a list comprehension — it eagerly builds the full list in memory. `(x*x for x in data)` is a generator expression — it yields items lazily. The second matters when data is large or infinite, or when you only need to iterate once and want to avoid the memory cost.

- id: python-itergen-itertools-03
  answer: |
    Two examples: itertools.chain (iterates over multiple iterables lazily as one sequence, no intermediate list) and itertools.islice (lazy slicing of an iterator without materializing the whole thing). They are preferable because they work lazily on any iterable, use O(1) memory, and can handle infinite sequences.

- id: python-itergen-protocol-04
  answer: |
    An iterable has __iter__() returning an iterator. An iterator has __iter__() (returning self) and __next__() returning the next item or raising StopIteration. The distinction matters because iterators are exhausted after one pass — iterating twice yields nothing the second time, while an iterable can be iterated multiple times.

- id: python-context-with-01
  answer: |
    The with statement guarantees that cleanup code runs when the block exits, even if an exception occurs. The dunder methods are __enter__ (called on entry, returns the bound resource) and __exit__ (called on exit, receives exception info and can suppress it).

- id: python-context-contextmanager-02
    answer: |
    @contextlib.contextmanager decorates a generator function. Code before the `yield` is setup (runs on __enter__), the yielded value is the resource, and code after the yield is teardown (runs on __exit__). This lets you write a context manager without a class.

- id: python-context-exitstack-03
  answer: |
    ExitStack solves the problem of managing a dynamic or unknown number of context managers. With plain nested with statements, you must know the count at write time. ExitStack lets you enter context managers programmatically (via enter_callback or enter_context) and guarantees they are all exited in reverse order when the block ends.

- id: python-context-async-04
  answer: |
    `async with` is the async equivalent of `with`. It uses __aenter__ and __aexit__ (which are async methods). A regular `with` cannot do the job because entering/exiting an async resource requires awaiting, which only works inside an async context.

- id: python-decorators-basics-01
  answer: |
    A decorator is fundamentally a callable that takes a function and returns a (usually different) function. The wrapper should use functools.wraps to preserve the original function's metadata (__name__, __doc__, etc.), which would otherwise be lost and break introspection, debugging, and documentation tools.

- id: python-decorators-args-02
  answer: |
    A decorator with its own arguments needs an extra layer because the decorator factory is called with those arguments first, and must return the actual decorator that takes the function. Structure: `def retry(times): def decorator(func): ... return decorator`. The outer layer receives the config, the inner receives the function.

- id: python-decorators-stacking-03
  answer: |
    Decorators are applied bottom-up (the one closest to def runs first). When the decorated function is called, the wrappers run top-down — the outermost decorator's wrapper executes first, then calls the next one down. For @a @b def f: f = a(b(f)), so a's wrapper runs first on call.

- id: python-decorators-class-04
  answer: |
    A class decorator receives the class object and can modify, wrap, or replace it. A real stdlib example is @dataclass, which inspects class attributes and adds generated methods like __init__ and __eq__. Another is @functools.total_ordering, which fills in missing comparison methods.

- id: python-datamodel-eqhash-01
  answer: |
    Defining __eq__ sets __hash__ to None, making instances unhashable (can't use as dict keys or in sets). __eq__ and __hash__ must be consistent because equal objects must have equal hashes — if two objects compare equal but hash differently, they'll be stored in different hash table slots and lookups will fail.

- id: python-datamodel-slots-02
  answer: |
    __slots__ declares the allowed attributes explicitly, preventing the creation of a per-instance __dict__. This saves memory and speeds up attribute access. What you give up is the ability to add arbitrary new attributes to instances, and it can complicate multiple inheritance (all bases must have compatible slots).

- id: python-datamodel-mutable-03
  answer: |
    Default arguments are evaluated once when the function is defined, not on each call. So `def add(item, target=[])` shares the same list across all calls — items accumulate. The correct pattern is `def add(item, target=None): if target is None: target = []`.

- id: python-datamodel-is-04
  answer: |
    `is` checks object identity (same memory address), while `==` checks value equality (via __eq__). For small ints and strings, Python interns/caches them, so `a is b` may be True for equal values — but this is an implementation detail you should never rely on. Use `==` for value comparison.

- id: python-datamodel-descriptor-05
  answer: |
    A descriptor is an object that defines __get__, __set__, or __delete__. It explains property (a descriptor that wraps getter/setter methods) and methods (functions are descriptors — __get__ binds them to instances, creating bound methods). Descriptors are the mechanism behind attribute access customization.

- id: python-errors-elsefinally-01
  answer: |
    The else block runs only if no exception was raised in the try block. The finally block always runs, whether or not an exception occurred. Prefer else over putting code in the try because it keeps the try block minimal — only the code that can actually raise the expected exception — preventing accidental exception masking.

- id: python-errors-custom-02
  answer: |
    Define a custom exception by subclassing Exception (or a more specific built-in). Catch specific exception types rather than a bare `except:` because bare except catches everything including SystemExit and KeyboardInterrupt, making programs hard to interrupt and masking unexpected bugs.

- id: python-errors-raisefrom-03
  answer: |
    `raise NewError(...) from err` explicitly sets the __cause__ attribute, showing that NewError was a direct consequence of err. A plain `raise NewError(...)` inside an except block implicitly sets __context__ (the exception being handled) but not __cause__ — the traceback shows "During handling of the above exception, another exception occurred" instead of "The above exception was the direct cause".

- id: python-errors-group-04
  answer: |
    ExceptionGroup (Python 3.11+) bundles multiple unrelated exceptions into one object. except* is the syntax to handle them — it catches matching exceptions from the group and lets you handle each one separately. Unlike normal except which catches the whole group as a single exception, except* decomposes it and can handle different exception types differently.
