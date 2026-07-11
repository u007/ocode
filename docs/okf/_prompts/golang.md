# Golang — Kaizen blind answer sheet (questions only)

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

### go-concurrency-01

What is the difference between an unbuffered and a buffered channel, and when does a send block on each?

### go-concurrency-02

What does `select` do, how does a `default` case change its behavior, and what happens if a `select` has no ready case and no default?

### go-concurrency-03

Explain the rules around closing a channel: who should close it, what happens on send/receive after close, and why closing is used.

### go-concurrency-04

A loop launches a goroutine per iteration that reads the loop variable. Historically all goroutines saw the same final value. What caused this, and what changed in Go 1.22?

### go-sync-01

What is a data race in Go, and what is the minimal correct way to protect a counter incremented by many goroutines?

### go-sync-02

What does the `-race` flag do, and what are its limitations?

### go-sync-03

When would you use `sync/atomic` instead of a `sync.Mutex`, and what is the trade-off?

### go-sync-04

How do you use `sync.WaitGroup` to wait for a set of goroutines, and what is the common misuse that causes a race or a panic?

### go-errors-01

What does the `%w` verb in `fmt.Errorf` do, and how does it differ from `%v` or `%s`?

### go-errors-02

What is the difference between `errors.Is` and `errors.As`, and when do you reach for each?

### go-errors-03

What is a sentinel error, how is it declared, and what should callers compare against it with?

### go-errors-04

A function returns `*MyError` typed as `error`. Even when it returns a nil pointer, `err != nil` is true at the call site. Why?

### go-interfaces-01

How does a type satisfy an interface in Go, and what does "accept interfaces, return structs" mean?

### go-interfaces-02

What is the empty interface (`any`), and what is the danger of a type assertion like `x.(int)` versus the two-result form?

### go-generics-01

When should you use generics (type parameters) versus a plain interface parameter?

### go-generics-02

Write the shape of a generic function and explain what a constraint is.

### go-generics-03

What does the `comparable` constraint allow, and what does the `~` token mean in a constraint like `~int`?

### go-generics-04

What is type-argument inference in generics, and when must you specify the type arguments explicitly?

### go-context-01

How does `context` cancellation stop a running goroutine, and whose responsibility is it to actually stop?

### go-context-02

What is the difference between `context.WithTimeout`/`WithDeadline` and `WithCancel`, and why must you always call the returned `cancel`?

### go-context-03

What is `context.WithValue` for, and what should it NOT be used for?

### go-context-04

State the conventions for passing `context.Context` around a codebase.

### go-slices-01

Why can appending to a slice silently overwrite data seen through another slice, and why is `append`'s return value load-bearing?

### go-slices-02

What happens when you read from and write to a nil map, and how does that differ from a nil slice?

### go-slices-03

What does `copy(dst, src)` actually copy, and why does slicing (`s[1:3]`) not give you an independent slice?

### go-slices-04

What is the iteration order of a Go map, and why can't you take the address of a map element (`&m[k]`)?

### go-defer-01

When are the arguments to a deferred call evaluated, and in what order do multiple deferred calls run?

### go-defer-02

How does `recover` work, and what are its constraints on where it can be called and which goroutine it covers?

### go-defer-03

Why is `defer file.Close()` inside a long-running loop a problem, and what do you do instead?

### go-defer-04

How can a deferred function change a function's return value, and what is this commonly used for?

### go-testing-01

What is a table-driven test in Go, and why is it the idiomatic pattern?

### go-testing-02

What does `t.Parallel()` do inside a subtest, and what loop-variable pitfall did it historically create?

### go-testing-03

What do `t.Cleanup` and `t.Helper` do?
