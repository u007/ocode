# Vbnet — Kaizen blind answer sheet (questions only)

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

### vbnet-syntax-01

How do you declare a variable in VB, how are statements terminated, and how do you continue one statement across multiple lines?

### vbnet-syntax-02

What do `Option Strict`, `Option Explicit`, and `Option Infer` control, and why does `Option Strict On` matter?

### vbnet-syntax-03

What is the difference between a `Module` and a `Class` in VB?

### vbnet-syntax-04

What is the difference between a `Sub` and a `Function` in VB, and how does a `Function` return a value?

### vbnet-props-01

What is an auto-implemented property, and how does it differ from a full property with a backing field?

### vbnet-props-02

What is the backing field of an auto-implemented property named, and how do you initialize an auto-property with a default value?

### vbnet-props-03

How do you declare a read-only or write-only property in VB, and can an auto-implemented property be `ReadOnly`?

### vbnet-props-04

What is a default property in VB, and what constraint does it have?

### vbnet-null-01

In VB, what does `Nothing` mean, and how does its meaning differ for a reference type versus a value type?

### vbnet-null-02

How do you declare a nullable value type in VB, and how do you check it and read its value safely?

### vbnet-null-03

Contrast the `If(a, b)` operator with the `IIf(a, b, c)` function. Why is `If` preferred for null-coalescing and conditional selection?

### vbnet-null-04

Why do you test a reference for null with `Is Nothing` / `IsNot Nothing` rather than `= Nothing`?

### vbnet-errors-01

Describe the structure of a `Try/Catch/Finally` block in VB and when the `Finally` block runs.

### vbnet-errors-02

What does a `Catch ... When` filter do, and why use it instead of catching then re-checking inside the block?

### vbnet-errors-03

Inside a `Catch ex As Exception`, what is the difference between `Throw` and `Throw ex` when you want to rethrow, and why does it matter?

### vbnet-errors-04

What is the legacy `On Error Goto` / `Err` error handling, and why is structured `Try/Catch` preferred in modern VB?

### vbnet-linq-01

Write the shape of a basic VB LINQ query expression that filters and projects, and name the keywords involved.

### vbnet-linq-02

How do you group and aggregate in VB query syntax? Show the `Group By ... Into` form.

### vbnet-linq-03

What is the `Aggregate` query keyword in VB, and how does it differ from `From`?

### vbnet-linq-04

What is deferred (lazy) execution in LINQ, and how do you force a query to execute immediately in VB?

### vbnet-events-01

Explain the `WithEvents` + `Handles` mechanism for handling events in VB.

### vbnet-events-02

What do `AddHandler` and `RemoveHandler` do, what role does `AddressOf` play, and when must you use them instead of `Handles`?

### vbnet-events-03

How do you declare a custom event on a class and raise it, and how do handlers receive arguments?

### vbnet-events-04

When would you choose `WithEvents`/`Handles` over `AddHandler`/`RemoveHandler`, and vice versa?

### vbnet-oop-01

What is the difference between `Inherits` and `Implements` in VB, and how many of each can a class have?

### vbnet-oop-02

Explain `Overridable`, `Overrides`, `MustOverride`, `MustInherit`, and `NotOverridable` in VB's inheritance model.

### vbnet-oop-03

What does `Shared` mean on a member, and how does it differ from an instance member? How is a shared member accessed?

### vbnet-oop-04

Distinguish `Me`, `MyBase`, and `MyClass` in VB. When does `MyClass` differ from `Me`?

### vbnet-convarr-01

Compare `CType`, `DirectCast`, and `TryCast` — what each does and when to use it.

### vbnet-convarr-02

What is the difference between a widening and a narrowing conversion, and how does `Option Strict On` treat each?

### vbnet-convarr-03

In VB, are arrays 0-based, and how do you resize one while keeping its contents? What does the array bound in `Dim a(n)` mean?

### vbnet-convarr-04

What are the `CInt`/`CStr`/`CDbl` conversion functions, and why use `&` (not `+`) for string concatenation in VB?
