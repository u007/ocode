- id: python-types-union-01
  answer: |
    Optional[int] means the value can be either an int or None — equivalent to Union[int, None]. With PEP 604 syntax (Python 3.10+), you write it as: int | None.

- id: python-types-generics-02
  answer: |
    Use a TypeVar: `from typing import TypeVar; T = TypeVar('T'); def f(x: T) -> T: return x`. Python 3.12 (PEP 695) introduced cleaner syntax: `def f[T](x: T) -> T: return x`, removing the need for an explicit TypeVar declaration.

- id: python-types-protocol-03
  answer: |
    typing.Protocol defines a structural (duck-typed) interface: any class that implements the required methods/attributes implicitly satisfies the protocol without inheriting from it. This differs from an ABC, which uses nominal typing — a class must explicitly inherit (or be registered) to be considered a subclass. Protocols are checked only by type checkers, not at runtime (unless @runtime_checkable is used).

- id: python-types-self-04
  answer: |
    Use typing.Self (Python 3.11+): `def copy(self) -> Self: ...`. Before Self, you'd use a TypeVar bound to the class: `T = TypeVar('T', bound='MyClass'); def copy(self: T) -> T: ...`. Self is preferred because it's cleaner and handles subclassing correctly without boilerplate.

- id: python-dataclasses-basics-01
  answer: |
    @dataclass auto-generates __init__, __repr__, __eq__, and (by default) __hash__. A list default must use field(default_factory=list) because default arguments are evaluated once at class-definition time — using `= []` would share the same list across all instances, causing mutations to leak between them. default_factory calls list() for each new instance.

- id: python-dataclasses-frozen-02
  answer: |
    frozen=True makes instances immutable: attempting to assign to any attribute raises FrozenInstanceError. __post_init__ is a hook called at the end of __init__ (after all fields are set), useful for validation, computed fields, or initializing attributes that depend on other fields.

- id: python-dataclasses-slots-03
  answer: |
    slots=True adds __slots__ to the class, which eliminates per-instance __dict__ overhead — reducing memory usage and speeding up attribute access. The trade-offs: you cannot add new attributes dynamically, you cannot use weak references (unless __weakref__ is included in slots), and multiple inheritance with other slotted classes can be problematic.

- id: python-dataclasses-vs-04
  answer: |
    @dataclass: mutable data container with methods, default values, and rich dunder generation — the default choice for most data-holding classes. NamedTuple: immutable record that is also a tuple (indexable, unpackable) — good for simple structured data. TypedDict: a dict with a fixed set of typed keys — ideal for JSON-like data or API payloads where you need dict semantics with type checking.

- id: python-async-await-01
  answer: |
    Calling an async def function returns a coroutine object — nothing inside the function body executes yet. The coroutine is lazy: it only runs when awaited (which suspends the current coroutine and drives it on the event loop) or scheduled via asyncio.create_task / asyncio.gather. Without the event loop driving it, the coroutine never progresses.

- id: python-async-taskgroup-02
  answer: |
    asyncio.gather runs coroutines concurrently, but if one raises an exception, the others keep running (unless return_exceptions=True, in which case exceptions are returned as results). asyncio.TaskGroup (Python 3.11+) cancels all remaining tasks as soon as one fails, waits for them to finish, and then raises an ExceptionGroup containing all exceptions — providing structured, fail-fast concurrency with proper cleanup.

- id: python-async-blocking-03
  answer: |
    A blocking call like time.sleep(5) or requests.get(...) freezes the entire event loop thread, preventing any other coroutine from making progress — defeating the purpose of async. Instead use asyncio.sleep(5) for delays, asyncio.to_thread() to run blocking code in a thread pool, or an async-native library like aiohttp for HTTP.

- id: python-async-cancel-04
  answer: |
    Cancellation works by throwing asyncio.CancelledError into the coroutine at its current await point. The coroutine can catch it for cleanup but should re-raise it. Swallowing CancelledError (bare except or catching it without re-raising) makes the task un-cancellable — the event loop can't stop it, which can cause hangs, leaked resources, and broken shutdown.

- id: python-itergen-yield-01
  answer: |
    A function containing yield is a generator function. Calling it returns a generator object that produces values lazily — one at a time, on demand — rather than computing and storing the entire sequence in memory. Execution pauses at each yield and resumes on the next iteration, maintaining local state between yields.

- id: python-itergen-genexpr-02
  answer: |
    [x*x for x in data] is a list comprehension — it eagerly builds and returns a full list in memory. (x*x for x in data) is a generator expression — it returns a lazy iterator that yields one item at a time. The generator expression matters when data is large (memory savings), when you only need to iterate once, or when you want to chain it with other lazy operations.

- id: python-itergen-itertools-03
  answer: |
    Two examples: (1) itertools.chain — flattens multiple iterables into a single lazy sequence without building intermediate lists. (2) itertools.islice — lazily slices an iterable (e.g., islice(gen, 10, 20)) without materializing the whole thing. Both are preferable to manual loops because they're lazy (memory-efficient), implemented in C (faster), and compose well with other iterators.

- id: python-itergen-protocol-04
  answer: |
    An iterable implements __iter__() which returns an iterator. An iterator implements __iter__() (returns self) and __next__() (returns the next value or raises StopIteration). The distinction matters because iterators are exhausted after one pass — iterating twice yields nothing — while iterables can produce a fresh iterator each time, allowing repeated iteration.

- id: python-context-with-01
  answer: |
    The with statement guarantees that __exit__ is called when the block exits, whether normally or via an exception — ensuring cleanup always happens. The dunder methods are __enter__ (called on entry, returns the bound resource) and __exit__ (called on exit, receives exception info or None).

- id: python-context-contextmanager-02
  answer: |
    @contextlib.contextmanager wraps a generator function into a context manager. Code before the yield statement runs on __enter__ (setup); the yielded value is the resource bound to the as variable; code after the yield runs on __exit__ (teardown). If an exception occurs, it's thrown into the generator at the yield point.

- id: python-context-exitstack-03
  answer: |
    ExitStack solves the problem of managing a dynamic or unknown number of context managers at write time. With nested with statements, you must know the count statically. ExitStack lets you call enter_context() in a loop or conditionally, and it unwinds all entered contexts in reverse order on exit — ideal for variable-depth resource management.

- id: python-context-async-04
  answer: |
    async with is the async counterpart of with. It uses __aenter__ and __aexit__ (async dunders that are themselves coroutines). A regular with cannot do the job because __enter__ and __exit__ are synchronous — they cannot await async setup or teardown operations like acquiring a connection from an async pool.

- id: python-decorators-basics-01
  answer: |
    A decorator is fundamentally a callable that takes a function (or class) and returns a replacement — typically a wrapper function that adds behavior before/after the original. functools.wraps should be used on the wrapper to copy the original function's __name__, __doc__, __module__, and other metadata, so introspection, debugging, and documentation tools see the original function's identity.

- id: python-decorators-args-02
  answer: |
    A decorator with its own arguments needs three nested levels: the outermost function receives the decorator's arguments (e.g., times=3) and returns the actual decorator; the middle decorator receives the function being decorated; the innermost wrapper replaces the function. The extra layer exists because Python's @ syntax passes the decorated function as a single argument — you need an intermediate closure to capture the decorator arguments first.

- id: python-decorators-stacking-03
  answer: |
    Decorators are applied bottom-up: the one closest to def (b) runs first, then a wraps the result. When f is called, the wrappers execute top-down: a's wrapper runs first (its pre-code), then calls b's wrapper, which calls the original f. So application order is b then a; execution order is a's pre-code → b's pre-code → f → b's post-code → a's post-code.

- id: python-decorators-class-04
  answer: |
    A class decorator receives the class object and returns a (possibly modified or replacement) class. It can add/remove methods, wrap methods, register the class, or modify class attributes. A real stdlib example: functools.total_ordering — given one comparison method, it fills in the rest. Another: dataclasses.dataclass, which adds __init__, __repr__, __eq__, etc.

- id: python-datamodel-eqhash-01
  answer: |
    Defining __eq__ sets __hash__ to None implicitly, making instances unhashable (can't be used in sets or as dict keys). __eq__ and __hash__ must be consistent: if a == b, then hash(a) must equal hash(b). If two objects are equal but hash differently, they'll be stored in different hash buckets and set/dict lookups will fail to find them.

- id: python-datamodel-slots-02
  answer: |
    __slots__ declares a fixed set of allowed attribute names, stored in a compact tuple-like structure instead of a per-instance __dict__. This saves significant memory (no __dict__ per instance) and speeds up attribute access. What you give up: dynamic attribute assignment (can't add new attributes), weak references (unless __weakref__ is in slots), and some multiple-inheritance patterns.

- id: python-datamodel-mutable-03
  answer: |
    Default arguments are evaluated once when the def statement executes, not on each call. So the same list object is shared across every call that omits the argument — mutations persist between calls. The correct pattern is: def add(item, target=None): if target is None: target = []; ... — creating a fresh list per call.

- id: python-datamodel-is-04
  answer: |
    is checks object identity — whether two names point to the exact same object in memory. == checks value equality — whether __eq__ returns True. For small ints (-5 to 256) and interned strings, Python reuses objects, so a is b can be True even without explicit assignment. For larger ints or non-interned strings, a is b is usually False even when a == b. This makes is unreliable for value comparison.

- id: python-datamodel-descriptor-05
  answer: |
    A descriptor is an object that defines __get__, __set__, and/or __delete__ — it controls how attribute access works on a class. property is a data descriptor: __get__ calls the getter, __set__ calls the setter. Methods are non-data descriptors: __get__ binds the function to the instance (creating a bound method). This unified mechanism explains why obj.method() passes obj as self, and why property(obj) calls the getter.

- id: python-errors-elsefinally-01
  answer: |
    else runs only if the try block completed without raising an exception. finally always runs — whether an exception occurred, was caught, or not. else is preferred over putting code in the try block because code in the try is protected by the except clauses; if that code raises, it could be caught by an except meant for the original operation, masking the real error source.

- id: python-errors-custom-02
  answer: |
    Define a custom exception by subclassing Exception (or a more specific built-in like ValueError). Optionally add a docstring and custom __init__ for extra context. Catch specific exception types rather than bare except: because bare except catches everything — including SystemExit, KeyboardInterrupt, and programming errors — making debugging impossible and preventing proper program termination.

- id: python-errors-raisefrom-03
  answer: |
    raise NewError(...) from err explicitly sets NewError.__cause__ to err, creating a deliberate exception chain shown as "The above exception was the direct cause of the following exception." A plain raise inside an except block implicitly sets __context__ (shown as "During handling of the above exception, another exception occurred") — the chain is incidental rather than intentional, and the original traceback is preserved as context.

- id: python-errors-group-04
  answer: |
    ExceptionGroup (Python 3.11+) wraps multiple exceptions into a single object that can be raised together. except* catches matching exceptions from a group and can handle different exception types separately. They were added because asyncio.TaskGroup and other concurrent patterns need to surface multiple simultaneous failures. Unlike normal except (which catches the group as a whole), except* lets you catch specific types within the group and handle them differently.
