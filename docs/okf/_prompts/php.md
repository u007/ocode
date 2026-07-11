# Php — Kaizen blind answer sheet (questions only)

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

Total questions: 34

---

### php-types-01

What does `declare(strict_types=1)` do, where must it go, and how does it change argument handling compared to the default coercive mode?

### php-types-02

What is the difference between `==` and `===`, and give a case where type juggling with `==` produces a surprising result.

### php-types-03

Explain union, intersection, and nullable type declarations, and how `?T` relates to `T|null`.

### php-types-04

Contrast the `never`, `void`, and `mixed` type declarations. When is each correct?

### php-types-05

What are typed class constants, which PHP version added them, and what rule applies when a subclass overrides a typed constant?

### php-enums-01

What is the difference between a pure enum and a backed enum in PHP 8.1, and when do you need a backed one?

### php-enums-02

Describe `cases()`, `from()`, and `tryFrom()` on enums, including how `from()` and `tryFrom()` differ on an invalid value.

### php-enums-03

Can an enum have methods, constants, and implement interfaces? What are the key restrictions versus a normal class?

### php-enums-04

Two variables hold `Suit::Hearts`. Are they the same instance, and how does that affect comparison and use in `match`?

### php-oop-01

What is constructor property promotion (PHP 8.0), and what does it replace?

### php-oop-02

Explain `readonly` properties (8.1) and `readonly` classes (8.2): when can a readonly property be written, and what does marking the whole class add?

### php-oop-03

Distinguish `self`, `static`, and `$this` in a method, and explain what late static binding solves.

### php-oop-04

What are asymmetric visibility and property hooks, which PHP version introduced them, and what do they let you avoid?

### php-oop-05

What does the `#[\Override]` attribute (PHP 8.3) do, and what class of bug does it catch?

### php-closures-01

What is the difference between a closure (`function () use (...) {}`) and an arrow function (`fn () => ...`) with respect to variable capture?

### php-closures-02

What is the first-class callable syntax `strlen(...)` (PHP 8.1), and what does it produce?

### php-closures-03

What do `Closure::bind` / `bindTo` do, and how do they affect `$this` and scope inside a closure?

### php-closures-04

In `use ($x)` versus `use (&$x)`, what is the difference, and what value does the closure see if `$x` changes after the closure is defined?

### php-error-01

Describe the PHP throwable hierarchy. What is the difference between `Error` and `Exception`, and how do you catch both?

### php-error-02

Explain `try`/`catch`/`finally` semantics: when does `finally` run, and what happens if both the `try`/`catch` and the `finally` block have a `return`?

### php-error-03

How do you define a custom exception and chain a lower-level exception to it, and why chain?

### php-error-04

What is the difference between `set_error_handler` and `try`/`catch`, and how do you make PHP warnings (e.g. from `fopen`) catchable as exceptions?

### php-arrays-01

In PHP there is a single `array` type. What is the difference between a "list" and an "associative array", and how is a PHP array actually structured?

### php-arrays-02

How does the spread operator work for array unpacking, including string keys (PHP 8.1), and how does keyed destructuring with `[...]`/`list()` work?

### php-arrays-03

PHP arrays are value types with copy-on-write. What does assigning or passing an array actually do, and how does that differ from `&` references?

### php-arrays-04

How does PHP coerce array keys? What key do `"1"`, `1.9`, `true`, and `null` end up as?

### php-null-01

What is the difference between `??` (null coalescing) and `?:` (the short ternary / "elvis")?

### php-null-02

What does the nullsafe operator `?->` (PHP 8.0) do, and what does "short-circuiting" mean for a chain like `$a?->b()->c`?

### php-null-03

What does the `??=` operator do, and how does it differ from `$x = $x ?? $y`?

### php-null-04

Distinguish `isset($x)`, `empty($x)`, and `$x === null`. Give a value where `isset` and `empty` disagree.

### php-match-01

How does `match` differ from `switch`? Cover comparison type, fallthrough, expression-vs-statement, and unmatched values.

### php-match-02

Because `match` uses strict comparison, what surprises can it produce that a `switch` would not, e.g. matching the value `0`?

### php-match-03

Given `match` is usually preferred, when is `switch` still the better fit?

### php-match-04

How do you use `match(true)` to replace an if/elseif chain of conditions, and why does it work?
