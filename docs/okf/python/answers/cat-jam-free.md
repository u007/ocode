# cat-jam-free — closed-book answers for python (opencode-go, 2026-10-14); answerer saw only _prompts/python.md
# Delivery: one `ocode run -m opencode-go/cat-jam-free -effort med` session, sheet attached via -f, empty scratch cwd;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: python-types-union-01
  answer: |
    `Optional[int]` means the value may be `int` or `None` (i.e. `Union[int, None]`). With PEP 604 syntax, write `int | None`.

- id: python-types-generics-02
  answer: |
    Define a `TypeVar` and use it for input and output, e.g. `T = TypeVar("T"); def f(x: T) -> T: ...`. Python 3.12 (PEP 695) lets you declare type parameters directly in the signature: `def f[T](x: T) -> T: ...`, and similarly `class Foo[T]: ...` and `type Alias[T] = ...`.

- id: python-types-protocol-03
  answer: |
    A `typing.Protocol` defines an interface by structural typing: any object with the required methods/attributes is considered compatible, without explicit inheritance. An abstract base class is nominal: compatibility normally requires inheritance or registration.

- id: python-types-self-04
  answer: |
    Annotate the return type as `Self` from `typing` (Python 3.11+) or `typing_extensions`; this preserves the actual subclass type for chaining. Older code often used a bound `TypeVar("Self", bound="Foo")`; modern type checkers such as mypy and Pyright understand `Self` directly.

- id: python-dataclasses-basics-01
  answer: |
    `@dataclass` generates boilerplate such as `__init__`, `__repr__`, and `__eq__` (plus ordering/hash-related methods depending on options). A mutable list default like `= []` would create one shared list at class-definition time, so dataclasses require `field(default_factory=list)` to construct a fresh list per instance.

- id: python-dataclasses-frozen-02
  answer: |
    `@dataclass(frozen=True)` makes instances immutable by blocking attribute assignment/deletion after construction and generally makes them hashable. `__post_init__` runs after the generated `__init__`, commonly for validation or computing derived fields.

- id: python-dataclasses-slots-03
  answer: |
    `@dataclass(slots=True)` adds `__slots__`, reducing memory use and speeding attribute access while preventing arbitrary undeclared attributes. The trade-off is less flexibility, such as complications with some multiple-inheritance or dynamic-attribute patterns.

- id: python-dataclasses-vs-04
  answer: |
    Use a dataclass for mutable or rich domain objects; use `NamedTuple` for lightweight immutable tuple-like records; use `TypedDict` to type dictionary-shaped data, especially JSON-like mappings, without a generated class.

- id: python-async-await-01
  answer: |
    Calling an `async def` function returns a coroutine object; its body does not run until it is awaited or scheduled on an event loop. This laziness enables cooperative multitasking, but an unawaited coroutine produces a runtime warning.

- id: python-async-taskgroup-02
  answer: |
    `asyncio.gather` runs awaitables concurrently and collects their results in order. `asyncio.TaskGroup` provides structured concurrency: tasks are created within a context, and if one fails, sibling tasks are cancelled and the failure is propagated, often as part of an `ExceptionGroup` when multiple fail.

- id: python-async-blocking-03
  answer: |
    Blocking synchronous code monopolizes the event loop thread, stalling all other tasks. Use async libraries instead, or move blocking work to a thread/process with `asyncio.to_thread` or an executor.

- id: python-async-cancel-04
  answer: |
    Cancellation injects `asyncio.CancelledError` at an await point. Keep cleanup in `finally` and let cancellation propagate; swallowing it with a bare or `BaseException` catch prevents the task from actually cancelling and can leak work.

- id: python-itergen-yield-01
  answer: |
    A function containing `yield` becomes a generator function; calling it returns a generator iterator. Its body executes lazily and suspends at each `yield`, whereas a list-building function computes all values eagerly and returns the complete list.

- id: python-itergen-genexpr-02
  answer: |
    `[x*x for x in data]` eagerly builds a list in memory; `(x*x for x in data)` is a lazy generator expression. The generator matters for large or infinite inputs, streaming, memory efficiency, and when only part of the result is consumed.

- id: python-itergen-itertools-03
  answer: |
    Examples: `itertools.chain` lazily concatenates iterables without building an intermediate list, and `itertools.islice` lazily slices an iterable. They avoid materializing full intermediate collections and compose efficiently.

- id: python-itergen-protocol-04
  answer: |
    An iterable implements `__iter__`, returning an iterator; an iterator implements both `__iter__` and `__next__`. The distinction matters because an iterable can usually be iterated repeatedly, while an iterator is exhausted after one pass.

- id: python-context-with-01
  answer: |
    `with` guarantees enter/setup and exit/teardown around a block, including during exceptions. It uses `__enter__` and `__exit__`; if `__exit__` returns a truthy value, it suppresses the exception.

- id: python-context-contextmanager-02
  answer: |
    `@contextlib.contextmanager` turns a generator into a context manager: code before `yield` is setup, the yielded value becomes the `as` target, and code after `yield` is teardown. The teardown runs for both normal exit and exceptions.

- id: python-context-exitstack-03
  answer: |
    `ExitStack` handles a dynamic or unknown number of context managers, entering them conditionally and unwinding in reverse. A plain nested `with` requires a fixed, statically known set.

- id: python-context-async-04
  answer: |
    `async with` manages an asynchronous context manager via coroutine methods `__aenter__` and `__aexit__`. A regular `with` cannot await those coroutines, so async resources such as async database connections need `async with`.

- id: python-decorators-basics-01
  answer: |
    A decorator is fundamentally a callable that takes an object and returns a replacement, typically a function or class. `functools.wraps` copies metadata such as `__name__`, `__doc__`, and `__wrapped__`, preserving introspection and tooling behavior.

- id: python-decorators-args-02
  answer: |
    A decorator with arguments must first accept and process those arguments, then return the actual decorator that receives the function. This extra factory layer is needed because `@retry(times=3)` is evaluated before decoration occurs.

- id: python-decorators-stacking-03
  answer: |
    Decorators are applied bottom-up: `@b` wraps the original function first, then `@a` wraps that result. When called, the outermost wrapper runs first: `a`'s wrapper, then `b`'s wrapper, then the original function.

- id: python-decorators-class-04
  answer: |
    A class decorator receives a class object and can inspect, modify, register, or replace it. A standard-library example is `@dataclasses.dataclass`, which transforms a class by generating methods.

- id: python-datamodel-eqhash-01
  answer: |
    Defining `__eq__` without `__hash__` sets `__hash__` to `None`, making instances unhashable. Equality changes the identity-based hash contract, so `__eq__` and `__hash__` must remain consistent for correct set/dict behavior.

- id: python-datamodel-slots-02
  answer: |
    `__slots__` replaces per-instance `__dict__` with fixed slot descriptors, saving memory and improving attribute access. You give up arbitrary instance attributes and must handle slot-inheritance and weak-reference requirements explicitly.

- id: python-datamodel-mutable-03
  answer: |
    A mutable default argument is evaluated once when the function is defined, so all calls share the same object and mutations leak across calls. The correct pattern is `target=None`, then create a new list inside the function when `target is None`.

- id: python-datamodel-is-04
  answer: |
    `is` tests object identity; `==` tests value equality. Small integers and some strings may be cached or interned, so `is` can unexpectedly be true, but this is an implementation detail and must not be relied upon except for singletons like `None`.

- id: python-datamodel-descriptor-05
  answer: |
    A descriptor implements `__get__`, `__set__`, and/or `__delete__` (and may use `__set_name__`). This protocol explains how `property` controls attribute access and how functions become bound methods when accessed through an instance.

- id: python-errors-elsefinally-01
  answer: |
    `else` runs only when no exception is raised; `finally` always runs, including during exceptions or returns. Preferring `else` keeps the `try` block minimal so unrelated exceptions are not accidentally caught.

- id: python-errors-custom-02
  answer: |
    Define a custom exception as a subclass of `Exception`, e.g. `class MyError(Exception): pass`. Catch specific types so handling is precise and broad `except:` does not mask `SystemExit`, `KeyboardInterrupt`, or unrelated bugs.

- id: python-errors-raisefrom-03
  answer: |
    `raise NewError(...) from err` sets `__cause__` and reports the original error as the direct cause. A plain raise inside an `except` sets `__context__` and prints "during handling of another exception"; use `from None` to suppress that context.

- id: python-errors-group-04
  answer: |
    `ExceptionGroup` wraps multiple exceptions for concurrent/structured error propagation. `except*` matches and handles a subset of exceptions in a group by type, re-raising any remaining unmatched ones, unlike a normal `except`, which matches one exception object.
