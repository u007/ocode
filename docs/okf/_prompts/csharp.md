# Csharp — Kaizen blind answer sheet (questions only)

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

Total questions: 32

---

### csharp-null-01

What do nullable reference types give you, and what is the runtime effect of the `?` annotation, the `!` operator, and `#nullable enable`?

### csharp-null-02

What is the difference between value and reference types, and what surprising thing happens when you mutate a struct held in a readonly field or a collection?

### csharp-null-03

Distinguish `record` (record class), `record struct`, and a plain `class`, especially around equality and mutability.

### csharp-null-04

What do `init`-only properties and `required` members each do, and how do they combine?

### csharp-pattern-01

How does a switch expression differ from a switch statement, and what happens if its arms are not exhaustive?

### csharp-pattern-02

What are property patterns and positional patterns, and how do they bind values?

### csharp-pattern-03

Explain relational patterns, logical patterns, and list patterns.

### csharp-pattern-04

What does the `is` declaration pattern (`obj is Customer c`) do that a plain type check plus a cast does not, and where is the bound variable in scope?

### csharp-linq-01

What is deferred versus immediate execution in LINQ, and which operators trigger each?

### csharp-linq-02

What is the difference between LINQ over `IEnumerable<T>` and over `IQueryable<T>`, and what is the danger of mixing them?

### csharp-linq-03

Why is enumerating the same LINQ query variable more than once a pitfall, and how do you avoid it?

### csharp-linq-04

What is the difference between `First` and `FirstOrDefault` (and `Single`), and what value-type gotcha does `FirstOrDefault` have?

### csharp-async-01

When would you return `ValueTask`/`ValueTask<T>` instead of `Task<T>`, and what rules constrain how a `ValueTask` may be consumed?

### csharp-async-02

What does the compiler do with `async`/`await`, and how does blocking on an async call (`.Result`/`.Wait()`) cause a deadlock?

### csharp-async-03

What does `ConfigureAwait(false)` do, and why is it recommended in library code?

### csharp-async-04

How does `CancellationToken` cancellation work, and what are `IAsyncEnumerable<T>` and `await foreach` for?

### csharp-generics-01

What does a `where` constraint do on a type parameter, and what are the main kinds?

### csharp-generics-02

Explain generic variance: what do `out` and `in` mean, and where do they apply?

### csharp-generics-03

How does type inference work for a generic method, and when must you specify the type arguments explicitly?

### csharp-generics-04

What does `default(T)` produce, and why does generic code need it?

### csharp-delegate-01

What are `Func`, `Action`, and `Predicate`, and what is a delegate?

### csharp-delegate-02

What does the `event` keyword add over a plain public delegate field?

### csharp-delegate-03

A loop creates a lambda each iteration that reads the loop variable. When do all the lambdas end up seeing the same final value, and when are they safe?

### csharp-delegate-04

What is a multicast delegate, and what happens to return values and exceptions when you invoke one?

### csharp-dispose-01

What does `IDisposable` do, and how does a `using` statement differ from a `using` declaration?

### csharp-dispose-02

What are `IAsyncDisposable` and `await using` for, and when do you use them over `IDisposable`?

### csharp-dispose-03

What is the difference between a finalizer and `Dispose`, and when does a type actually need a finalizer?

### csharp-dispose-04

Describe the full dispose pattern for a class that owns both managed and unmanaged resources.

### csharp-span-01

What are collection expressions, and what does the spread element `..` do?

### csharp-span-02

What are `Span<T>`, `ReadOnlySpan<T>`, and `Memory<T>`, and why can't you store a `Span<T>` in a field or use it across an `await`?

### csharp-span-03

What does `stackalloc` do, and what must you be careful about when using it?

### csharp-span-04

What is the difference between an array (`T[]`) and `List<T>`, and what are `params` collections?
