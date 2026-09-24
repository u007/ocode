- id: python-types-union-01
  answer: |
    `Optional[int]` means a value of type `int` or `None`.

    With PEP 604 syntax, write:

    ```python
    value: int | None
    ```
- id: python-types-generics-02
  answer: |
    Traditionally, use a `TypeVar`:

    ```python
    from typing import TypeVar

    T = TypeVar("T")

    def identity(value: T) -> T:
        return value
    ```

    Python 3.12 added PEP 695 syntax, allowing type parameters to be declared directly:

    ```python
    def identity[T](value: T) -> T:
        return value
    ```
- id: python-types-protocol-03
  answer: |
    A `typing.Protocol` defines an interface structurally: a class satisfies it if it has the required attributes and compatible method signatures, even without inheriting from the protocol.

    An abstract base class generally uses nominal typing: subclasses must explicitly inherit from it, and abstract methods prevent instantiation until implemented. Protocols can also describe existing third-party classes; ABCs are often better for shared implementation or shared class hierarchy.
- id: python-types-self-04
  answer: |
    Annotate the method as returning `Self`, meaning an instance of the runtime class or any subclass:

    ```python
    from typing import Self

    class Builder:
        def reset(self) -> Self:
            ...
            return self
    ```

    `typing.Self` is available in Python 3.11+, with `typing_extensions.Self` used on older versions. Static type checkers such as mypy and Pyright use it to verify that the returned value preserves the concrete subclass type.
- id: python-dataclasses-basics-01
  answer: |
    `@dataclass` inspects annotated fields and generates common methods, most notably `__init__`, a useful `__repr__`, and equality methods. It also supports flags controlling options such as ordering and immutability.

    A list is mutable, so a default argument such as `items=[]` would share one list among every instance, causing accidental state leakage. Modern dataclasses also reject mutable defaults in many cases. Use a factory so each instance receives a new object:

    ```python
    from dataclasses import dataclass, field

    @dataclass
    class Inventory:
        items: list[str] = field(default_factory=list)
    ```
- id: python-dataclasses-frozen-02
  answer: |
    `@dataclass(frozen=True)` makes generated instances immutable through normal attribute assignment. It also gives a frozen dataclass an automatically generated hash when equality is enabled.

    `__post_init__` is called by the generated `__init__` after fields receive their initial values. It is useful for validation, derived values, or normalization. Inside it, normalization can use `object.__setattr__`, which bypasses the frozen-instance check.
- id: python-dataclasses-slots-03
  answer: |
    `@dataclass(slots=True)` creates slots for dataclass fields, so instances generally have no per-instance `__dict__`. This reduces memory use and can improve attribute access speed.

    The trade-off is reduced flexibility: instances cannot normally receive arbitrary new attributes, slot names conflict with class variables, and weak-reference support requires an explicit `__slots__` entry such as `"__weakref__"`. Subclassing with additional undeclared attributes can also be more restrictive.
- id: python-dataclasses-vs-04
  answer: |
    - Use a `@dataclass` for a general domain or record object with generated initialization, equality, and representation.
    - Use a `NamedTuple` for a compact, immutable tuple-like value with readable fields and positional behavior, such as a fixed coordinate pair.
    - Use a `TypedDict` for a dictionary with a statically known set of keys, possibly different value types. It remains an ordinary dictionary at runtime and its key/value constraints are not enforced then.
- id: python-async-await-01
  answer: |
    Calling an `async def` function creates and returns a coroutine object. It does not execute the function body.

    The body begins only when the coroutine is awaited or otherwise scheduled on an asyncio event loop. Merely creating the coroutine does no work and also produces a “coroutine was never awaited” warning if it is discarded.
- id: python-async-taskgroup-02
  answer: |
    `asyncio.gather` runs multiple awaitables concurrently and returns their results in the order of the input awaitables. With its default behavior, the first exception is propagated immediately, but the other awaitables are not automatically cancelled and may continue running. `return_exceptions=True` instead returns both results and exceptions as values.

    `asyncio.TaskGroup` manages a structured group of child tasks. If one child fails, it cancels the remaining children, waits for their cleanup, and exits by raising an `ExceptionGroup` containing the failures. It does not directly return task results, so results must be collected separately. TaskGroup therefore provides stronger cleanup and failure handling for related concurrent operations.
- id: python-async-blocking-03
  answer: |
    An event-loop thread runs only one coroutine at a time. A synchronous call such as `time.sleep(5)` or `requests.get(...)` blocks that thread, preventing the loop from servicing other coroutines, timers, or network activity.

    Use nonblocking equivalents such as:

    ```python
    await asyncio.sleep(5)
    ```

    For HTTP work, use an async client such as `httpx.AsyncClient` or `aiohttp`. If an existing synchronous library must be used, run it in a worker thread with `await asyncio.to_thread(...)` rather than calling it directly from the coroutine.
- id: python-async-cancel-04
  answer: |
    Calling `task.cancel()` requests cancellation. The task receives a `CancelledError` at its next suspension or await point, so cancellation is cooperative rather than an immediate forced stop. Cleanup is commonly performed in `finally`.

    A coroutine should generally re-raise `CancelledError` after any necessary cleanup, rather than catching it and returning normally. Swallowing it tells asyncio that the task completed when cancellation was actually requested, which can break timeout, shutdown, and `TaskGroup` behavior. Since Python 3.8, `CancelledError` inherits from `BaseException`, not `Exception`.
- id: python-itergen-yield-01
  answer: |
    A function is a generator function if its body contains a `yield` expression. Calling it returns a generator object without executing the body; execution begins and advances when the generator is iterated or driven with `send()`.

    Generator execution is incremental and pauses at each `yield`, retaining its state. In contrast, a function using a list comprehension executes the complete computation immediately and returns a materialized list, using memory proportional to the result.
- id: python-itergen-genexpr-02
  answer: |
    `[x*x for x in data]` creates a list immediately and stores every result.

    `(x*x for x in data)` creates a lazy iterator that computes values as they are requested. The second form matters for large or potentially unbounded streams because it avoids holding all results in memory. It is also one-shot: once exhausted, it does not restart.
- id: python-itergen-itertools-03
  answer: |
    Two useful tools are:

    - `itertools.islice(iterable, start, stop, step)` selects a portion of an iterable lazily, avoiding a temporary sliced list when the input is large or unbounded.
    - `itertools.chain.from_iterable(list_of_iterables)` lazily visits items from multiple iterables, avoiding construction of a combined list.

    Both express common iteration operations clearly and usually avoid unnecessary intermediate collections while delegating iteration work to efficient library implementations.
- id: python-itergen-protocol-04
  answer: |
    An **iterable** has `__iter__`, which returns a new iterator when `iter(obj)` is called. An **iterator** has both:

    - `__iter__`, which returns itself
    - `__next__`, which produces the next item or raises `StopIteration`

    Lists and strings are iterable but not iterators; iterators such as file objects are generally one-shot. A re-iterable object can produce a fresh iterator each time, allowing repeated iteration. An iterator normally cannot restart, so iterating it a second time finds it exhausted unless the object explicitly resets itself.
- id: python-context-with-01
  answer: |
    A `with` statement calls the context manager’s `__enter__` method and guarantees that `__exit__` is called when execution leaves the statement, whether normally or because of an exception. `__exit__` can suppress an exception by returning true from a non-`async` context manager.

    The protocol methods are `__enter__` and `__exit__`. This guarantee does not apply if the interpreter or process is forcibly terminated.
- id: python-context-contextmanager-02
  answer: |
    `@contextlib.contextmanager` converts a generator function into a factory for synchronous context managers. Setup runs before the `yield`, teardown normally runs afterward, and teardown should be placed in a `finally` block:

    ```python
    from contextlib import contextmanager

    @contextmanager
    def resource():
        handle = acquire()
        try:
            yield handle
        finally:
            release(handle)
    ```

    The value yielded is available through `as` in the `with` statement. Exceptions from the `with` body are thrown into the generator at its yield point.
- id: python-context-exitstack-03
  answer: |
    `contextlib.ExitStack` manages a dynamic or conditional number of context managers, which is awkward to express with statically nested `with` statements:

    ```python
    with contextlib.ExitStack() as stack:
        for item in items:
            stack.enter_context(process(item))
    ```

    Entered contexts are exited in reverse order even if later setup fails. A plain nested `with` is clearer for a fixed, small, statically known nesting structure; `ExitStack` is preferable for optional resources, callbacks, or loop-based setup.
- id: python-context-async-04
  answer: |
    `async with` manages an asynchronous context manager. It calls and awaits `__aenter__`, and later calls and awaits `__aexit__` during normal exit or exception unwinding.

    A regular `with` uses the synchronous `__enter__` and `__exit__` protocol and does not await asynchronous setup or cleanup. Therefore it cannot directly manage resources whose acquisition or release is asynchronous.
- id: python-decorators-basics-01
  answer: |
    A decorator is a callable that receives a function, class, or other object and usually returns a replacement, often a wrapper function that adds behavior.

    A wrapper should use `functools.wraps` to preserve metadata such as the original name, docstring, annotations, and module. It also sets `__wrapped__`, improving introspection and compatibility with tools that follow wrapped functions.
- id: python-decorators-args-02
  answer: |
    A plain decorator directly receives the function and returns a replacement:

    ```python
    function = decorator(function)
    ```

    With options, the decorator expression is first evaluated to create a configured decorator:

    ```python
    function = retry(times=3)(function)
    ```

    Thus `@retry(times=3)` requires `retry(times=3)` to return the actual function-wrapping decorator, adding the extra layer.
- id: python-decorators-stacking-03
  answer: |
    Given:

    ```python
    @a
    @b
    def f():
        ...
    ```

    decorators are applied from the bottom upward, producing `f = a(b(f))`.

    When the resulting function is called, the outer `a` wrapper runs first, then the `b` wrapper, then the original `f`. Statements after each wrapper call unwind in the opposite order: first `b` finishes, then `a`.
- id: python-decorators-class-04
  answer: |
    A class decorator is a callable that receives a class and may return that same class or a replacement class after adding behavior, methods, validation, or metadata.

    A standard-library example is:

    ```python
    from dataclasses import dataclass
    from functools import total_ordering

    @total_ordering
    @dataclass
    class Version:
        major: int
        minor: int
    ```

    `total_ordering` supplies additional rich comparison methods from the ones already defined, allowing instances to be ordered.
- id: python-datamodel-eqhash-01
  answer: |
    Defining `__eq__` normally causes Python to set the class’s `__hash__` to `None`, making instances unhashable, unless the class explicitly defines `__hash__` as well.

    If two objects compare equal and remain immutable, they should have the same hash so they can work correctly as dictionary keys or set members. Mutable objects with value-based equality are usually unsafe to hash because their equality and hash value can change after insertion.
- id: python-datamodel-slots-02
  answer: |
    Declaring `__slots__` causes Python to store declared instance attributes in fixed descriptor slots rather than in a normal per-instance dictionary. This can reduce memory use and make attribute access faster.

    If no base class already supplies an instance dictionary, instances lose the ability to accept arbitrary undeclared attributes. Slot names conflict with class attributes of the same name, and weak-reference support must be explicitly included in `__slots__`. Subclasses must also declare their additional slots if they should remain dictionary-free.
- id: python-datamodel-mutable-03
  answer: |
    Default parameter values are evaluated once when the function is defined, not on every call:

    ```python
    def add(item, target=[]):
        target.append(item)
        return target
    ```

    All calls therefore share the same list, causing results to accumulate unexpectedly. Use `None` as the default and create a fresh value inside the function:

    ```python
    def add(item, target=None):
        if target is None:
            target = []
        target.append(item)
        return target
    ```
- id: python-datamodel-is-04
  answer: |
    `is` asks whether two references point to the same object. `==` asks whether two values are equal according to the relevant equality operation, and classes can customize it through `__eq__`.

    CPython commonly interns small integers and some strings, so identity can unexpectedly be true for separate references to the same immutable value. Conversely, independently constructed but equal strings may be different objects, so identity can be false. These interning details are implementation choices, not language guarantees; use `==` for value comparisons and `is` only for identity or against known singleton values such as `None`.
- id: python-datamodel-descriptor-05
  answer: |
    A descriptor is an object assigned to a class that customizes instance or class attribute access through methods such as `__get__`, `__set__`, and `__delete__`.

    `property` is a data descriptor: it implements these methods so a getter can compute a value while optional setters and deleters validate or control mutation. Functions are non-data descriptors because they implement `__get__` but not `__set__`; accessing a function through an instance binds the instance to its first parameter, producing a bound method. An instance-dictionary attribute can shadow such a function, but not a data descriptor such as `property`.
- id: python-errors-elsefinally-01
  answer: |
    The `else` block runs only when the `try` suite completes without an exception. If an exception is raised, execution uses a matching `except` handler, or propagates if none matches.

    The `finally` block runs while leaving the construct, including after normal completion, after an exception, and during return or break unwinding, unless the process is forcibly terminated.

    Prefer `else` for the success path because it keeps only the operations expected to fail inside `try`. Exceptions raised by the success-path code in `else` will not be accidentally caught by the preceding handlers.
- id: python-errors-custom-02
  answer: |
    Define a custom exception by subclassing `Exception` or an appropriate existing application-specific exception:

    ```python
    class PaymentError(Exception):
        pass
    ```

    Prefer catching specific exception types because broad handlers can conceal programming errors and unrelated failures. A bare `except:` catches even `BaseException` subclasses such as `KeyboardInterrupt`, `SystemExit`, and `asyncio.CancelledError`. Catch `Exception` only when program-level exceptions should also be handled, and handle expected failures narrowly where possible.
- id: python-errors-raisefrom-03
  answer: |
    Inside an `except` block, a plain `raise NewError(...)` automatically records the currently handled exception as `__context__`, producing a traceback that says the new error occurred during handling of the earlier one.

    ```python
    raise NewError(...) from err
    ```

    makes `err` the explicit direct cause through `__cause__`, producing a “direct cause” traceback. `raise NewError(...) from None` suppresses display of the contextual chain. Outside an active exception handler, the plain raise normally has no context.
- id: python-errors-group-04
  answer: |
    Added in Python 3.11, `ExceptionGroup` packages multiple exceptions into one raisable object, with `BaseExceptionGroup` supporting groups that may contain base exceptions such as cancellation errors. They are useful when independent failures need to be reported or cleaned up together, as `asyncio.TaskGroup` does.

    `except* ValueError` matches all `ValueError` instances in a group. Handled matches are passed to its body, while unmatched exceptions are preserved and re-raised, usually regrouped automatically. A normal `except ValueError` does not unpack the group and cannot select individual exception types. The syntax also cannot be mixed with ordinary `except` clauses in the same `try` statement.
