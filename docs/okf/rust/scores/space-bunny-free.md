---
model_id: space-bunny-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-09-24
stack: rust
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — space-bunny-free on rust

> Valid ONLY for `space-bunny-free` @ `alpha`. A version bump invalidates this
> scorecard — re-benchmark.

Answers graded closed-book from `../answers/space-bunny-free.md`.
`rust-ownership-03` was skipped on the first run and re-asked closed-book as a
single question; it is the last record in that file. Wording throughout is the
model's own — no answer looked like a copy of the reference.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| rust-ownership-01 | ownership | 3 | 2 | 2 | 1.00 | |
| rust-ownership-02 | ownership | 2 | 3 | 2.5 | 0.83 | "original and copies remain independent" covers both-bindings-usable; never states Copy requires Clone (half credit on that point) |
| rust-ownership-03 | ownership, borrowing | 2 | 2 | 2 | 1.00 | |
| rust-borrowing-01 | borrowing | 3 | 2 | 2 | 1.00 | |
| rust-lifetimes-01 | lifetimes | 2 | 2 | 2 | 1.00 | |
| rust-lifetimes-02 | lifetimes | 2 | 3 | 3 | 1.00 | |
| rust-lifetimes-03 | lifetimes, borrowing | 2 | 2 | 2 | 1.00 | |
| rust-lifetimes-04 | lifetimes, traits | 1 | 2 | 2 | 1.00 | |
| rust-traits-01 | traits | 3 | 3 | 3 | 1.00 | monomorphization described ("compiled separately for each concrete T") without naming it; vtable named |
| rust-traits-02 | traits | 2 | 3 | 2 | 0.67 | missed "all branches must be the same type / avoids boxing closures-iterators"; only says it is "not the same as Box<dyn Trait>" |
| rust-traits-03 | traits | 2 | 2 | 2 | 1.00 | |
| rust-error-01 | error-handling | 2 | 2 | 2 | 1.00 | |
| rust-error-02 | error-handling | 3 | 2 | 1.5 | 0.75 | factual slip: says on `Ok(value)` "the enclosing function returns `value` immediately" — Ok continues, it does not return (half credit on point 1); From conversion correct |
| rust-error-03 | error-handling | 2 | 2 | 2 | 1.00 | |
| rust-error-04 | error-handling, traits | 2 | 3 | 3 | 1.00 | |
| rust-iterators-01 | iterators | 3 | 2 | 2 | 1.00 | |
| rust-iterators-02 | iterators, ownership | 2 | 2 | 2 | 1.00 | |
| rust-iterators-03 | iterators | 2 | 2 | 2 | 1.00 | |
| rust-iterators-04 | iterators | 2 | 2 | 2 | 1.00 | |
| rust-smartptr-01 | smart-pointers | 2 | 2 | 2 | 1.00 | minor: "freed when the last owning Box is dropped" implies multiple owners; concepts still present |
| rust-smartptr-02 | smart-pointers, concurrency | 2 | 3 | 3 | 1.00 | |
| rust-smartptr-03 | smart-pointers, borrowing | 3 | 3 | 3 | 1.00 | |
| rust-smartptr-04 | smart-pointers, concurrency | 3 | 3 | 3 | 1.00 | |
| rust-concurrency-01 | concurrency | 3 | 3 | 3 | 1.00 | minor: "equivalently, &mut T is Send when T is Send" is true but is not the Sync equivalence |
| rust-async-01 | async | 3 | 3 | 2 | 0.67 | missed "std ships no executor"; names "an async runtime" without saying the standard library lacks one |
| rust-async-02 | async, concurrency | 2 | 2 | 2 | 1.00 | |
| rust-async-03 | async | 2 | 2 | 1 | 0.50 | missed cooperative-scheduling / yields-only-at-.await mechanism; gives only the effect (worker cannot poll other tasks) and the spawn_blocking fix |
| rust-match-01 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| rust-match-02 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| rust-match-03 | pattern-matching, borrowing | 2 | 2 | 2 | 1.00 | |
| rust-match-04 | pattern-matching | 1 | 2 | 2 | 1.00 | concepts all present; the code example mixes tuple and struct patterns on one scrutinee and would not compile |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| ownership | 0.96 | 4 | ok | omit (strong) |
| borrowing | 1.00 | 5 | ok | omit (strong) |
| lifetimes | 1.00 | 4 | ok | omit (strong) |
| traits | 0.93 | 5 | ok | omit (strong) |
| error-handling | 0.92 | 4 | ok | omit (strong) |
| iterators | 1.00 | 4 | ok | omit (strong) |
| smart-pointers | 1.00 | 4 | ok | omit (strong) |
| concurrency | 1.00 | 4 | ok | omit (strong) |
| async | 0.71 | 3 | low-n | **derive** (mark low-n) |
| pattern-matching | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 65.25 / 69 = 94.6%
```

## Derivation targets

Tags below threshold (`< 0.75`): **async** (low-n, 3 questions) → feed into
`derived/rust.space-bunny-free.SKILL.md`. The gap is the same shape on both
missed questions: the model describes async effects (a runtime is needed; a
blocking call stalls the worker) without stating the mechanism (std ships no
executor; scheduling is cooperative and yields only at `.await`).
