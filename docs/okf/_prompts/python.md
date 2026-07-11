# Python — Kaizen blind answer sheet (questions only)

> **CLOSED-BOOK.** Answer every question from your own knowledge alone. You MUST
> NOT open, search, or otherwise access the Kaizen corpus — `questions.yaml`,
> `questions.md`, `scores/`, `derived/`, `meta.yaml`, or any file in this repo —
> nor look the answers up online. Doing so invalidates the evaluation.
>
> Answer each question **independently** (treat every item as a fresh context —
> no memory of earlier answers). If you are unsure, say so; do not guess to look
> complete. This measures what you actually know, not what you can retrieve.
>
> **Return format** — one YAML record per question so the grader can map answers
> back by id:
>
> ```yaml
> - id: <question-id>
>   answer: |
>     <your answer>
> ```

Total questions: 33

---

### python-types-union-01

What does `Optional[int]` mean, and how do you write the same thing with the PEP 604 syntax?

### python-types-generics-02

How do you write a generic function that returns the same type it receives, and what did Python 3.12 change about declaring type parameters?

### python-types-protocol-03

What is a `typing.Protocol`, and how does it differ from inheriting an abstract base class?

### python-types-self-04

You have a method that returns `self` for chaining on a subclassable class. How do you annotate its return type accurately, and what newer tools help?

### python-dataclasses-basics-01

What does `@dataclass` generate for you, and why must a list default use `field(default_factory=list)` instead of `= []`?

### python-dataclasses-frozen-02

What does `@dataclass(frozen=True)` do, and what is `__post_init__` for?

### python-dataclasses-slots-03

What does `@dataclass(slots=True)` give you, and what is the trade-off?

### python-dataclasses-vs-04

When would you choose a `@dataclass`, a `NamedTuple`, and a `TypedDict`?

### python-async-await-01

What does calling an `async def` function return, and why does nothing happen until you `await` it or schedule it on the event loop?

### python-async-taskgroup-02

Contrast `asyncio.gather` with `asyncio.TaskGroup` for running several coroutines concurrently, especially in how they handle a failure.

### python-async-blocking-03

Why is calling `time.sleep(5)` or a synchronous `requests.get(...)` inside an async coroutine a bug, and what should you do instead?

### python-async-cancel-04

How does task cancellation work in asyncio, and why should you avoid swallowing `asyncio.CancelledError`?

### python-itergen-yield-01

What makes a function a generator, and how does its execution differ from a function that builds and returns a list?

### python-itergen-genexpr-02

What is the difference between `[x*x for x in data]` and `(x*x for x in data)`, and when does the second matter?

### python-itergen-itertools-03

Give two `itertools` tools and why they are preferable to a manual loop that builds a list.

### python-itergen-protocol-04

What methods make an object iterable vs an iterator, and why does the distinction matter (e.g. iterating the same object twice)?

### python-context-with-01

What does the `with` statement guarantee, and which dunder methods implement a context manager?

### python-context-contextmanager-02

How does `@contextlib.contextmanager` let you write a context manager as a generator, and where do setup and teardown go?

### python-context-exitstack-03

What problem does `contextlib.ExitStack` solve that a plain nested `with` does not?

### python-context-async-04

What is `async with`, which dunder methods does it use, and why can't a regular `with` do the job for async resources?

### python-decorators-basics-01

What is a decorator fundamentally, and why should the wrapper use `functools.wraps`?

### python-decorators-args-02

Why does a decorator that takes its own arguments — e.g. `@retry(times=3)` — need an extra layer of nesting compared to a plain decorator?

### python-decorators-stacking-03

Given `@a` then `@b` stacked above `def f`, in what order are the decorators applied and in what order do their wrappers run when `f` is called?

### python-decorators-class-04

What can a class decorator do, and give a real example from the standard library.

### python-datamodel-eqhash-01

If you define `__eq__` on a class, what happens to its hashability, and why must `__eq__` and `__hash__` be kept consistent?

### python-datamodel-slots-02

What does declaring `__slots__` on a class do at the data-model level, and what do you give up?

### python-datamodel-mutable-03

Why is `def add(item, target=[]):` a classic Python bug, and what's the correct pattern?

### python-datamodel-is-04

What is the difference between `is` and `==`, and why can `a is b` be surprisingly True or False for small ints and strings?

### python-datamodel-descriptor-05

What is a descriptor, and how does it explain the behavior of `property` and methods?

### python-errors-elsefinally-01

In `try/except/else/finally`, when do the `else` and `finally` blocks run, and why prefer `else` over putting the code in the `try`?

### python-errors-custom-02

How should you define a custom exception, and why catch specific exception types rather than a bare `except:`?

### python-errors-raisefrom-03

What does `raise NewError(...) from err` do, and how does it differ from a plain `raise NewError(...)` inside an `except` block?

### python-errors-group-04

What are `ExceptionGroup` and `except*`, why were they added, and how does `except*` differ from a normal `except`?
