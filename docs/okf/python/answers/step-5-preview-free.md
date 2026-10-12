# step-5-preview-free — closed-book answers for python (opencode-go, 2026-10-12); answerer saw only _prompts/python.md
- id: python-types-union-01
  answer: |
    `Optional[int]` means "an int or None" — it is exactly an alias for `Union[int, None]`, and is the conventional spelling for a value that may be missing/absent (arguments, return values, dataclass fields). With PEP 604 (Python 3.10+) you write the same thing with the new union operator: `int`. PEP 604 also made `isinstance(1, int | None)` and `type_aliases = int | None` work at runtime, so the annotation is no longer just for type checkers. Old style: `Optional[int]`, `Union[int, str]`; new style: `int | None`, `int | str`. Both mean the same thing; `Optional[X]` is NOT a default value and does not make the argument optional.

- id: python-types-generics-02
  answer: |
    The classic way (pre-3.12) is a `TypeVar` bound to no specific type, used for both the parameter and the return:

    ```python
    from typing import TypeVar, Sequence
    T = TypeVar("T")

    def first(items: Sequence[T]) -> T:
        return items[0]
    ```

    The checker infers `T` from the argument, so calling `first([1, 2])` is checked as `int` and `first(["a"])` as `str` — the return type tracks whatever you passed in. This pattern (called identity/pass-through generics) is how `most type-preserving` functions are annotated.

    Python 3.12 (PEP 695) added first-class type parameter declarations, so you no longer need a module-level `TypeVar`. You write the type parameters directly in square brackets after the name:

    ```python
    def first[T](items: Sequence[T]) -> T: ...
    class Stack[T]: ...
    type Vector[T] = list[T]
    ```

    Each function/class/alias declares its own parameters in its own scope (no leaked, reusable TypeVars), annotations are evaluated lazily so forward references work without `from __future__ import annotations`, and the parameters are visible at runtime via `__type_params__`. For 3.12+ code, use PEP 695 syntax.

- id: python-types-protocol-03
  answer: |
    A `typing.Protocol` is a structural (duck-typed) interface: you declare the methods/attributes an object must have, and any class with a matching shape satisfies it — no inheritance, no registration, the class need never know the protocol exists. `isinstance`/`issubclass` checks only work if the protocol is decorated with `@runtime_checkable`, and even then they only check that the attribute names exist, not their actual signatures.

    An abstract base class is nominal: a class must explicitly inherit (or `register()`) with the ABC, it can provide shared implementations, and `abstractmethod` is enforced at instantiation time. ABCs give runtime enforcement and code sharing; protocols give static compatibility for "I just need something that quacks", which is what makes stubs, plugin systems, and mocks work without coupling.

- id: python-types-self-04
  answer: |
    Use `typing.Self` (Python 3.11+; from `typing_extensions` on older versions):

    ```python
    class Builder:
        def set_x(self, x: int) -> Self:
            self.x = x
            return self
    ```

    `Self` means "the type of the instance this was called on", so subclass chaining type-checks correctly instead of narrowing the result to `Builder`.

    Before 3.11 the standard workaround was a bound `TypeVar` bound to the class and used as the annotation for `self` as well as the return:

    ```python
    BuilderT = TypeVar("BuilderT", bound="Builder")

    def set_x(self: BuilderT, x: int) -> BuilderT: ...
    ```

    (Annotating `self` with the TypeVar is the load-bearing part — `-> "Builder"` would lose the subclass type.) `typing_extensions.Self` backports the modern spelling. Also note `Self` must not be used on methods returning a different object type (e.g. `copy`, `__enter__` returning a context-manager proxy) — annotate those normally.

- id: python-dataclasses-basics-01
  answer: |
    `@dataclass` inspects the class's annotated fields and generates `__init__` (with per-field defaults), `__repr__`, `__eq__` (comparing field values), and `__ne__`; with `order=True` it also adds `__lt__`, `__le__`, `__gt__`, `__ge__`; with `eq=True, frozen=True` it adds `__hash__`; with `frozen=True` it blocks attribute writes. `field()` still lets you customize init args, `default_factory`, `repr=False`, `compare=False`, `init=False`. You write only the fields (and methods), removing a pile of boilerplate.

    A list default must be `field(default_factory=list)` because `@dataclass` turns `x: list = []` into a default *argument* in the generated `__init__` — i.e. `def __init__(self, x=[])`. Python evaluates default arguments once, at function-definition time, and reuses that same object on every call. So all instances would share one list and mutations would leak between instances and accumulate across calls. In fact dataclasses refuse this outright: a bare mutable default raises `ValueError` at class creation, forcing `field(default_factory=list)`, which is called per instance and produces a fresh list each time.

- id: python-dataclasses-frozen-02
  answer: |
    `@dataclass(frozen=True)` makes instances immutable: any attribute assignment or deletion raises `FrozenInstanceError` (a subclass of `AttributeError`), so instances can't be mutated after construction and — because `eq` is on by default — they get a real `__hash__` derived from the fields, making them usable as dict keys / set members and defaulting to hashable, unlike mutable dataclasses which get `__hash__ = None`.

    `__post_init__` is an optional hook you define on the class; the generated `__init__` calls it automatically at the very end, after all fields are assigned. It's the place for initialization that depends on more than one field or can't be expressed as a default: validation, coercion/normalization, computing derived values. On a *frozen* dataclass you can't assign to fields inside it directly (it would raise `FrozenInstanceError`), so you call `object.__setattr__(self, "name", value)` for the rare case where you must derive a field. Also note frozen is shallow: a field holding a list is still mutable.

- id: python-dataclasses-slots-03
  answer: |
    `@dataclass(slots=True)` (3.10+) has the class generated with a `__slots__` tuple of its field names, so instances carry no `__dict__`: attribute storage becomes fixed slots instead of a dict. Benefit is memory (no per-instance dict) and faster attribute access. Note the class object is *rebuilt* (a new class is created) — that matters for pickle/copy wiring, though 3.11 added proper copy/deepcopy/pickle support, and `weakref_slot=True` (3.11+) adds a `__weakref__` slot.

    Trade-offs: you lose the ability to add attributes not declared as fields (AttributeError), and with them introspection (`vars()`), monkey-patching during tests, and things that stash state on the instance — notably `functools.cached_property` (needs an instance `__dict__`). Slots also only pay off if every class in the MRO uses them. The rule is to reach for `slots=True` because of a measured memory problem in a many-instance class, not as a default.

- id: python-dataclasses-vs-04
  answer: |
    Choose by the semantics you need, not the syntax:

    - `@dataclass` when you want a real mutable object with named fields, value equality, methods, and inheritance — a domain entity that changes over its lifetime. It is the default workhorse; the cost is that instances are unhashable by default and heavier than a tuple.
    - `NamedTuple` when the record is immutable and should behave like a tuple — unpacking, indexing, iteration, fixed length, hashable, cheap to create and compare. Good as a return value or a config record; `_replace` for "with-changes". Downside: it's a tuple, so anything duck-typing sequences accepts it.
    - `TypedDict` when the value really *is* a dict you'll pass around or serialize (e.g. a JSON API payload) and you only want static checking of its string keys, with `total=False`/`NotRequired` for optional ones. It's purely a typing construct — it has no runtime behavior, no instance-level enforcement, and its `__eq__` is just dict's.

- id: python-async-await-01
  answer: |
    Calling an `async def` function runs no code at all — it immediately builds and returns a **coroutine object**; locals are unbound and the body hasn't started. Nothing happens until something *drives* it: either another coroutine `await`s it (the await drives it with `send()`, resuming the awaiting coroutine when the inner one completes) or the event loop schedules it (`create_task`, wrapping it in a `Task`, or `run`), after which the loop runs it whenever it's free. A coroutine is an awaitable that can suspend and be resumed; it sits idle until the loop or an `await` advances it. If it is never awaited, Python emits `RuntimeWarning: coroutine 'fn' was never awaited` — that object being dropped is lost work, so you should always await it or wrap it in `create_task`.

- id: python-async-taskgroup-02
  answer: |
    `asyncio.gather(*aws)` bundles coroutines into one awaitable that resolves when all finish, returning results in input order. Its failure mode is weak: with `return_exceptions=False` the first exception propagates immediately to the awaiter *while the remaining tasks keep running in the background*, un-awaited and un-cancelled — so you must manually cancel them or you leak dangling tasks and "Task exception was never retrieved" warnings; results/errors can also arrive out of completion order, forcing you to zip them back by index.

    `asyncio.TaskGroup` (3.11+, `async with asyncio.TaskGroup() as tg: tg.create_task(...)`) is structured concurrency:

    ```python
    async with asyncio.TaskGroup() as tg:
        t1 = tg.create_task(fetch("a"))
        t2 = tg.create_task(fetch("b"))
    ```

    - Only when the `async with` block exits is every task guaranteed finished (no wait, no manual joins).
    - On the first failure it *cancels all remaining siblings* automatically (via `task.cancel()`, so cleanup handlers run), then raises a single `ExceptionGroup`/`BaseExceptionGroup` containing all errors — so no task is left orphaned, and each failure is raised rather than dropped.
    - It propagates an outer cancellation of the awaiting task into the children. It also can't be misused without awaiting (the block awaits), and its group errors are caught with `except*`.

- id: python-async-blocking-03
  answer: |
    The event loop is single-threaded and cooperative: while a coroutine runs synchronous blocking code, the thread is stuck and **the loop can't run anything else** — no other task advances, no timer callbacks fire, and the blocking call can't even be cancelled from outside until it returns. It's a bug because the program appears to hang (a 5-second `time.sleep` freezes the whole server) and the loop loses all signal/abort ability.

    Do instead:
    - `await asyncio.sleep(5)` for delays — the loop is free and it's cancellable.
    - `await session.get(url)` with an async HTTP client (`aiohttp`, `httpx.AsyncClient`), or any other non-blocking async API.
    - When you absolutely must call blocking/CPU code (sync libs, `requests`, image work), push it off the thread with `await asyncio.to_thread(requests.get, url)` (3.9+) or `await loop.run_in_executor(None, fn, args)`. If you have loops like `while ...: requests.get(...)` without `await`, rewrite them as `async for`.

- id: python-async-cancel-04
  answer: |
    Cancellation is cooperative: `task.cancel()` makes the loop throw a `CancelledError` into the coroutine **at its current suspension point** (the await where it's parked), so you can catch it, do cleanup, and re-raise — the task ends up cancelled. Since 3.11 `asyncio.timeout()` uses `task.uncancel()` bookkeeping to cancel cleanly.

    Don't swallow it because `CancelledError` is the channel the loop uses to tell your task it must stop. If you `except CancelledError: pass` and return normally, the task never actually dies: the canceller (a `wait_for` timeout, a `TaskGroup` cancelling siblings, `asyncio.wait_for`, a global shutdown) believes it took effect, but the coroutine is still running and keeping its resources/state alive, or it surfaces later as a confusing half-finished result or leaked task. Catch it only to clean up (close files, undo state) and then **re-raise** — bare `raise` in the handler. Rule of thumb: `finally` for unconditional cleanup, `except CancelledError` + `raise` when you need to observe cancellation but not block it.

- id: python-itergen-yield-01
  answer: |
    A function containing `yield` becomes a generator function; calling it returns a generator object without running a single statement. Execution is lazy and suspend-resume: each `next()` runs the body until the next `yield`, returns that value, and freezes the entire local state (variables, instruction pointer) so the next call picks up exactly there. Two consequences:

    - `StopIteration` is raised once the function returns (which is how `for` loops end).
    - You can do one-shot "push" work halfway: `yield from` delegates to a sub-iterable.

    Contrast with `return [expr for x in items]`: the list function runs the whole loop immediately, allocates every result up front, and must finish before the caller sees anything. The generator is O(1) per item, produces values the consumer can consume partially and stop early (`next()`, `sum()`, `any()`, an early `break`), and can represent infinite streams — but it's single-use: once exhausted, it's exhausted, and you can't peek twice.

- id: python-itergen-genexpr-02
  answer: |
    `[x*x for x in data]` is a list comprehension: eager, builds the whole list in memory, reusable indefinitely, indexable, and works everywhere a list is needed.

    `(x*x for x in data)` is a generator expression: lazy, computes each square on demand, holding constant memory instead of N results. It matters when the input is large or infinite (you can't materialize `range(10**12)` or an open file's lines into a list), when you'll consume only a prefix (`next(islice(...))`, early-exiting with `any()`/`all()`, breaking out of a `for` after k items — the rest is never computed), or when chaining transformations to avoid one intermediate list per stage. Price: one pass only, no `len()`/indexing, and with parentheses required in calls such as `sum(x*x for x in data)`.

- id: python-itergen-itertools-03
  answer: |
    Two especially useful ones:

    1. `itertools.islice(it, 0, 10)` — a lazy slice of any iterable, including infinite ones: takes the first 10 items without exhausting or materializing the rest.
    2. `itertools.chain(a, b, c)` (and `chain.from_iterable(iterables)`) — flattens several iterables (or an iterable of iterables) into one lazy stream.

    Both are preferable to a hand-rolled loop because `itertools` is implemented in C, is fully lazy (constant memory, no intermediate list), expresses intent declaratively, and is hard to get subtly wrong (no off-by-one, no leftover partial state). Their composability means "slice a chain of chunks of a tee'd stream" is one linear pipeline, and all of it can stop early without wasted computation.

- id: python-itergen-protocol-04
  answer: |
    An object is *iterable* if calling `iter()` on it returns an iterator — in practice that means it has `__iter__` (or falls back to `__getitem__` with integer indices, the old sequence protocol). Lists, tuples, strings, dicts are iterable.

    An object is an *iterator* if it implements `__next__` (returning the next value, raising `StopIteration` when done) and `__iter__` returning itself. Generators, file objects, `filter`, `map`, `enumerate` outputs are iterators.

    The distinction matters because you can't consume an iterator twice: its state advances with each `next()`, so a second `for` loop — or a second `iter()` — over a generator or file yields nothing, silently executing zero iterations, while the same `for` loop over a list or tuple works every time because `iter()` hands back a fresh iterator. In practice this is the bug where you pass a generator into a function that iterates it (e.g. a "peek then process" helper) and the caller later sees an empty container. If you expect re-iteration or random access, force a materialization (`list(...)`) or use `itertools.tee`.

- id: python-context-with-01
  answer: |
    The `with` statement guarantees deterministic setup and teardown: the object's `__enter__()` is called at the start and its result is bound to the `as` variable, then `__exit__(exc_type, exc_val, exc_tb)` is **always** called when the block ends — whether it ends by falling off the end, by an explicit `return`/`break`/`continue`, or by an exception propagating out of the block. If the cleanup `__exit__` returns a truthy value, the exception is suppressed; returning falsy lets it continue. So `with open(f) as fh:` guarantees the file is closed even on an error or an early return, instead of depending on a `try/finally` you wrote by hand. It's the protocol that makes locks/transactions/sessions safe.

- id: python-context-contextmanager-02
  answer: |
    `@contextmanager` turns a generator function into a factory for a context manager without writing a class. All setup code runs before the single `yield`, the yielded value is what `as` binds, and all teardown goes *after* the yield:

    ```python
    @contextlib.contextmanager
    def my_resource():
        r = acquire()               # setup
        try:
            yield r                 # value of the `as` clause
        finally:
            r.release()             # teardown, always runs
    ```

    The decorator wraps the generator so that `__enter__` runs the generator up to the `yield` (or you can `return` in setup to signal... rather, you can `return`/`yield` nothing to bind `None`), and `__exit__` resumes it *after* the yield on normal exit, or throws the block's exception *into* the generator at that yield point — so a `try/finally` around the yield (or an `except` if you want to handle the error) is the standard pattern, mirroring the semantics you get from the dunder form.

- id: python-context-exitstack-03
  answer: |
    `contextlib.ExitStack` solves context managers whose number you don't know until runtime — e.g. opening a variable list of files in a `for` loop, or entering different resources depending on a condition. A plain nested `with` needs the set of resources fixed at "compile time". With `ExitStack` you enter them dynamically and still get exactly-once, reverse-order cleanup for everything entered so far:

    ```python
    with contextlib.ExitStack() as stack:
        for path in user_selected:
            f = stack.enter_context(open(path))   # registered immediately
            ...
        stack.callback(some_cleanup_fn)           # also supports callbacks
    ```

    If any entry (or the body) raises, the stack unwinds *all* resources already entered — correct, order-preserving cleanup — whereas the manual `with` version would have to track what it had opened and leak the rest. It's also how `contextlib.closing`-style helpers and library code expose "acquire N resources safely".

- id: python-context-async-04
  answer: |
    `async with` is the async version of `with`. The resource class must implement `async def __aenter__(self)` and `async def __aexit__(self, *exc)` — awaitable dunders instead of `__enter__`/`__exit__` — so the setup and teardown can await I/O (opening a TCP connection, joining a server, draining a queue). Usage:

    ```python
    async with aiohttp.ClientSession() as session:
        async with session.get(url) as resp: ...
    ```

    A regular `with` can't do this because its dunders are called synchronously — running `__enter__` would block the event loop for the duration of the I/O, and no code inside a normal coroutine can `await` a `__exit__` after a `return`/raise. The async variant exists precisely so the loop stays free while setup and teardown are in flight; `@asynccontextmanager` is the generator-counterpart of `@contextmanager`.

- id: python-decorators-basics-01
  answer: |
    A decorator is, fundamentally, a callable that takes a function (or class) as argument and returns a new object — a *function transformer* — combined with the `@` syntax so that

    ```python
    @log_calls
    def f(): ...
    ```

    means `f = log_calls(f)`, applied when the function is defined. The usual implementation is a closure: an outer function taking the function, an inner `wrapper(*args, **kwargs)` that does "before" work, calls the original, does "after" work around `return`, then returns `wrapper`.

    The wrapper should be decorated with `functools.wraps(func)` because otherwise the wrapper *replaces* the original's metadata: `f.__name__` becomes `"wrapper"`, `__doc__`, `__module__`, `__annotations__` and the signature are lost. That breaks debugging/logging, breaks tools that introspect code (help, docs, `inspect.signature`, doctests, sphinx), and breaks stack-decorating logic. `wraps` copies that metadata and sets `__wrapped__`, keeping the decorated function appearing like itself.

- id: python-decorators-args-02
  answer: |
    A plain decorator is one call: decoration time passes the function straight to the decorator, `f = deco(f)`.

    A parameterized decorator — `@retry(times=3)` — actually performs **two** calls at decoration time: first `retry(times=3)`, whose job is only to capture the configuration and return a *decorator*; then that returned decorator receives the function. So the code needs three levels of nesting:

    ```python
    def retry(times=3):
        def decorator(fn):                 # 1st call returns this
            @functools.wraps(fn)
            def wrapper(*a, **k):          # 2nd: receives the function
                for _ in range(times):
                    try:
                        return fn(*a, **k)
                    except SomeError:
                        continue
                ...
            return wrapper
        return decorator                    # decorated with `@retry(times=3)`
    ```

    Without the outer factory, `@retry(times=3)` would try to call the returned decorator as if it were the decorated function and error at import time — the extra layer exists because the `@` syntax means "call this expression with the function", and `retry(times=3)` must yield a callable that accepts a function, not the function itself.

- id: python-decorators-stacking-03
  answer: |
    Given

    ```python
    @a
    @b
    def f(): ...
    ```

    it is exactly `f = a(b(f))`.

    - Application order: **bottom-up** — `b` is applied first (inner/nearest to the function), then `a` wraps the result.
    - Call order: **top-down** — calling `f` runs `a`'s wrapper first, and when it calls the function it received, that is `b`'s wrapper, which calls the original.

    So the outermost decorator is the outermost shell at call time; the innermost decorator is closest to the real function. Any exception propagates out through both wrappers in the reverse order they were entered.

- id: python-decorators-class-04
  answer: |
    A class decorator is just a callable applied to the finished class object: `C = deco(C)` at class-creation time. It can modify the class (add/patch or replace attributes, add methods, inject mixins, register it in a registry — e.g. a plugin/subclass registry or an ORM mapping) or even return something entirely different (a wrapper function, a proxy, or a new subclass), so it can wrap every instance method in behavior (timing, locking, validation) without touching each one. Since it replaces the name binding, this is also how "apply a class-level concern" like registration is done.

    Standard-library examples: `@dataclasses.dataclass` (receives the raw class, reads the field type annotations, and generates `__init__`, `__repr__`, `__eq__` for it); `@functools.total_ordering` (given `__eq__` plus one of `__lt__`/`__le__`/..., it fills in the remaining comparison methods); `@abc.ABCMeta` can be applied via `metaclass=`... a better direct example is `@dataclass`.

- id: python-datamodel-eqhash-01
  answer: |
    Defining `__eq__` (or a dataclass with `eq=True, frozen=False`) makes Python set `__hash__ = None` for your class: instances become **unhashable**. You don't get the default identity-based `__hash__` any more, because the equal-but-distinct instances of a value class would otherwise hash differently while comparing equal.

    Keeping them consistent matters because they're a contract, not independent methods: if `a == b` then `hash(a) == hash(b)` must hold, or hash-based containers break. Inserting equal objects into a set would both survive as duplicates, and a dict lookup for an equal key would look in the wrong bucket and fail — you'd get two "equal" objects co-existing in a set and "missing" keys in a dict (silently wrong, hard to debug).

    So if you define value equality you must either (a) also define `__hash__` derived from exactly the attributes `__eq__` compares (and only make it hashable if those attributes are immutable, so an object's hash can't change while it's in a set), or (b) leave it unhashable when it's mutable — use `obj = frozen(...)`... rather: accept `__hash__ = None` (or `@dataclass(frozen=True)`), which is why mutable dataclasses are unhashable by default and frozen ones gain `__hash__`.

- id: python-datamodel-slots-02
  answer: |
    `__slots__ = ("x", "y")` declares the class's complete attribute set up front, so instances get a pre-allocated block of slots instead of a `__dict__` backing store — attribute storage becomes simpler, faster (offset lookup instead of hash-table), and dramatically smaller in memory (no per-instance dict, which for a many-instance class is the win).

    You give up:
    - Dynamic attributes: assigning `obj.new_attr = 1` raises `AttributeError`, so no monkey-patching instances and no decorator tricks that stash state per-object (e.g. `functools.cached_property` still needs to write to the instance — it can't, unless you include a `__dict__` in `__slots__` or the cached attribute is also a slot).
    - `obj.__dict__` introspection, `vars(obj)`, `weakref` support unless you add `'__weakref__'` to slots, and compatibility with anything that fills instance attributes dynamically.
    - Partial benefit in inheritance: subclasses without slots reintroduce a `__dict__`, so the memory saving only holds along a fully-slotted hierarchy.

- id: python-datamodel-mutable-03
  answer: |
    Because the default argument is evaluated **once**, when `def` is executed (function-definition time), and is then stored in the function object's `__defaults__` and shared across all calls. So `target=[]` binds the same list object on every call, and any mutation persists into the next call:

    ```python
    def add(item, target=[]):
        target.append(item)
        return target

    add(1)   # [1]
    add(2)   # [1, 2]  — surprise!
    ```

    This is "surprising" because the behavior implies a hidden, persistent state shared across calls, and it makes a pure-looking function order-dependent.

    Correct pattern: default to `None` (a sentinel) and create the fresh object inside the function body:

    ```python
    def add(item, target=None):
        if target is None:
            target = []
        target.append(item)
        return target
    ```

    Or simply require an explicit argument. The same applies to dicts/sets — any mutable default.

- id: python-datamodel-is-04
  answer: |
    `==` is value equality: it invokes `__eq__`, and for built-ins it compares contents. `is` is object **identity**: whether the two names refer to the very same object in memory. Both return a bool; they can disagree.

    `a is b` is "surprising" for small ints and strings because the interpreter caches/interns them: CPython pre-allocates integers from **-5 to 256** as singletons, so `256 + 0 is 256` may be True (and `257 is 257` False — above the cache range, though within one code object constant folding can also merge references). Similarly, identifiers/literal-like short strings ("abc", "foo") are usually **interned**, so `"abc" is "abc"` is True; longer or dynamically-built strings aren't, so `"".join(["a","b","c"]) is "abc"` can be False. These are optimization behaviors, not language guarantees — an implementation may change them. So only `==` is reliable for value comparison, and `is` is correct only for singletons (`is None`, `is True`) or deliberate identity checks (`is NotImplemented`, pool objects).

- id: python-datamodel-descriptor-05
  answer: |
    Descriptors are objects that define any of `__get__`, `__set__`, `__delete__` and are stored as **class attributes**; accessing `obj.x` may be intercepted by that attribute, controlling how the value is computed, stored, and validated. A **non-data descriptor** (only `__get__`, e.g. plain functions) can be shadowed by an instance attribute, whereas a **data descriptor** (defines `__set__` too, e.g. `property`, function types… e.g. `int`, a property) takes precedence over the instance `__dict__` and enforces the logic on writes.

    `property` is a descriptor: reading `self.x` calls the getter; assigning `self.x = v` calls the setter instead of writing to the instance dict — that's how the whole "computed/validated attribute" illusion works, plus instance-vs-class state sharing.

    Methods work this way too: `obj.method` first looks up the function found on the class and, since it's a descriptor, `__get__` returns a **bound method** — a wrapper that, when called, passes `obj` as the first positional (`self`). This one mechanism explains why `self` is automatic, why `classmethod`/`staticmethod` need their own descriptor wrappers, and how ORM/lazy-loaded fields (`ManagedAttribute` in Django, SQLAlchemy attributes) work.

- id: python-errors-elsefinally-01
  answer: |
    - `try`: the risky attempt.
    - `except <types>`: runs only if a matching exception was raised.
    - `else`: runs **only if the try block completed without raising** — a success path.
    - `finally`: runs **always** — after success, after an `except` handled it, after an exception that nobody caught, and even when the block is left by `return`/`break`/`continue`. `else` cannot be combined with `except` absent... (they can coexist), and `finally` overwrites the return value if it returns its own. A `finally` that catches infinitely... key uses: cleanup, releasing locks, closing files, restoring state.

    Prefer `else` for the success path because code in `try` is covered by the `except` clauses: anything that raises there may be caught and misinterpreted. Putting dependent logic in `else` keeps the guarded window minimal (only what you want to catch) and guarantees that an exception raised by the follow-up work itself is **not** swallowed or misreported by your error handler. Example: after opening a file, parse the lines in `else`, so a parse bug isn't masked as "file-open failed".

- id: python-errors-custom-02
  answer: |
    Define a custom exception by subclassing `Exception` (or a relevant builtin, e.g. `ValueError`) and optionally building structured data:

    ```python
    class ConfigError(Exception):
        """Raised when a config file is invalid or missing keys."""
        def __init__(self, message, *, path=None):
            super().__init__(message, path)
            self.path = path
    ```

    Subclass `Exception` (never bare `Exception` problem... subclass means it participates in `except Exception` if callers want it, and still catchable selectively). Add a `__str__` when you need special formatting. Use a module-level base class if you have a hierarchy (so callers can catch your library's whole family).

    Catch specific exception types rather than a bare `except:` (which is `except BaseException:`) because a bare except catches `SystemExit`, `KeyboardInterrupt`, `GeneratorExit`... (exception and `BaseException`s), swallows genuine bugs (a `NameError`/`TypeError` raised in the handler path gets conflated with the actual failure), and makes code uninterruptible and hard to debug — it catches everything, expected and not. Catching narrowly documents what you can actually handle; everything else propagates. If you need a broad catch, use `except Exception` at an outer boundary and re-raise or log.

- id: python-errors-raisefrom-03
  answer: |
    `raise NewError(...) from err` sets `NewError.__cause__ = err` **explicitly**, making the traceback show "The above exception was the direct cause of the following exception" and linking the two — it declares "I'm replacing this exception with that one and keeping the original as context."

    A plain `raise NewError(...)` inside an `except` block does **implicit** chaining: Python sets `NewError.__context__ = err` automatically, so the traceback still shows "During handling of the above exception, another exception occurred" (the original is not lost). The difference is the displayed relationship and intent: `from` makes it a deliberate cause, and you can `raise ... from None` to *suppress* context when the original error is internal detail. A bare `raise` re-raises the currently-handled exception instead, preserving the traceback.

- id: python-errors-group-04
  answer: |
    `ExceptionGroup` (with its `BaseException` counterpart, 3.11+, PEP 654) is a single exception object that carries multiple exceptions in `.exceptions`, possibly arranged as a tree, so one failure can represent many independent failures at once rather than picking one "winner".

    They were added mainly for concurrent code — `asyncio.TaskGroup` cancelling siblings when one task fails — where several real errors can occur at once and collapsing to "the first exception" loses information that callers need (all failures must be reported, e.g. multiple retries/timeouts, or in a fan-out of requests). They let code both raise and handle rich, nested error bundles.

    `except*` (e.g. `except* ValueError as eg:`) is the matching syntax for them, instead of a bare `except`. It "slices" the group: it looks through the group (and any nested groups) for exceptions of that type; the matching ones are removed and bound to `eg.exceptions`, the rest are *re-raised* — so multiple `except*` clauses for different types in one `try` can each catch their part, and only leftovers at the end propagate as a fresh `ExceptionGroup`. A normal `except` clause, by contrast, matches only the group object itself (e.g. `except Exception` catching the whole bundle, or `except ValueError` matching the group as a group, not splitting it) — you would then have to unwrap `eg.exceptions` yourself, and one unwrapped leftover from an earlier `except*` cannot propagate into a later plain `except` clause. Also `except*` handles intercepted groups through nested matching, and plain `except` cannot mix with `except*` in the same `try`.
