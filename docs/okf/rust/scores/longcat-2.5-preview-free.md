---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: rust
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — longcat-2.5-preview-free on rust

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump
> invalidates this scorecard — re-benchmark.

Answers graded closed-book from `../answers/longcat-2.5-preview-free.md`
(produced via `ocode run` from an isolated dir; session audited: 1 user + 1
assistant turn, zero tool calls). All 31 questions answered.

**Contamination check:** clean. Wording is the model's own throughout; no answer
reproduces reference-answer phrasing. The closest overlaps (the three elision
rules in `rust-lifetimes-02`, "`Arc<Mutex<T>>` (or `Arc<RwLock<T>>`)" in
`rust-smartptr-04`) are standard Rust Book / community phrasing, not key leakage.
Misses line up with the same gaps other models show (no "std has no executor",
no cooperative-yield mechanism), which is the opposite of a copied key.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| rust-ownership-01 | ownership | 3 | 2 | 2 | 1.00 | |
| rust-ownership-02 | ownership | 2 | 3 | 2 | 0.67 | "original remains valid" covers no-move, but never states Copy requires Clone (half credit); Drop+Copy reason muddled — "duplicated after Drop has run" / "contradictory ownership guarantees" instead of "each implicit copy would run the destructor on the same resource" (half credit) |
| rust-ownership-03 | ownership, borrowing | 2 | 2 | 2 | 1.00 | |
| rust-borrowing-01 | borrowing | 3 | 2 | 2 | 1.00 | |
| rust-lifetimes-01 | lifetimes | 2 | 2 | 2 | 1.00 | |
| rust-lifetimes-02 | lifetimes | 2 | 3 | 3 | 1.00 | |
| rust-lifetimes-03 | lifetimes, borrowing | 2 | 2 | 2 | 1.00 | |
| rust-lifetimes-04 | lifetimes, traits | 1 | 2 | 2 | 1.00 | |
| rust-traits-01 | traits | 3 | 3 | 3 | 1.00 | |
| rust-traits-02 | traits | 2 | 3 | 2 | 0.67 | missed "every return path must be the same concrete type / lets you return closures-iterators without boxing" |
| rust-traits-03 | traits | 2 | 2 | 2 | 1.00 | |
| rust-error-01 | error-handling | 2 | 2 | 2 | 1.00 | |
| rust-error-02 | error-handling | 3 | 2 | 2 | 1.00 | |
| rust-error-03 | error-handling | 2 | 2 | 2 | 1.00 | |
| rust-error-04 | error-handling, traits | 2 | 3 | 3 | 1.00 | enum implied by "each variant"; Error/Display, From, thiserror all present |
| rust-iterators-01 | iterators | 3 | 2 | 2 | 1.00 | |
| rust-iterators-02 | iterators, ownership | 2 | 2 | 2 | 1.00 | |
| rust-iterators-03 | iterators | 2 | 2 | 2 | 1.00 | |
| rust-iterators-04 | iterators | 2 | 2 | 2 | 1.00 | |
| rust-smartptr-01 | smart-pointers | 2 | 2 | 2 | 1.00 | |
| rust-smartptr-02 | smart-pointers, concurrency | 2 | 3 | 3 | 1.00 | |
| rust-smartptr-03 | smart-pointers, borrowing | 3 | 3 | 3 | 1.00 | |
| rust-smartptr-04 | smart-pointers, concurrency | 3 | 3 | 2.5 | 0.83 | labels Rc<RefCell> "single-threaded" but never says why (non-atomic count / `!Send`+`!Sync`) — half credit on point 2 |
| rust-concurrency-01 | concurrency | 3 | 3 | 2.5 | 0.83 | Send/Sync definitions correct; enforcement point half credit: no auto-trait/structural derivation, and slip "forces all captured types to be Send **and Sync**" (spawn requires Send, not Sync) |
| rust-async-01 | async | 3 | 3 | 2 | 0.67 | missed "std ships no executor"; says "you need an executor (like Tokio)" without stating the standard library lacks one |
| rust-async-02 | async, concurrency | 2 | 2 | 1.5 | 0.75 | Send requirement and !Send future correct; never states that values alive across `.await` are stored in the future's generated state machine (half credit on point 1) |
| rust-async-03 | async | 2 | 2 | 1 | 0.50 | missed cooperative scheduling / yields-only-at-`.await`; gives only the effect (blocks the runtime thread) and the `spawn_blocking` / `tokio::time::sleep` fix |
| rust-match-01 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| rust-match-02 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| rust-match-03 | pattern-matching, borrowing | 2 | 2 | 2 | 1.00 | |
| rust-match-04 | pattern-matching | 1 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| ownership | 0.93 | 4 | ok | omit (strong) |
| borrowing | 1.00 | 5 | ok | omit (strong) |
| lifetimes | 1.00 | 4 | ok | omit (strong) |
| traits | 0.93 | 5 | ok | omit (strong) |
| error-handling | 1.00 | 4 | ok | omit (strong) |
| iterators | 1.00 | 4 | ok | omit (strong) |
| smart-pointers | 0.95 | 4 | ok | omit (strong) |
| concurrency | 0.85 | 4 | ok | omit (strong) |
| async | 0.64 | 3 | low-n | **derive** (mark low-n) |
| pattern-matching | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 64.17 / 69 = 93.0%
```

## Derivation targets

Tags below threshold (`< 0.75`): **async** (low-n, 3 questions) → feed into
`derived/rust.longcat-2.5-preview-free.SKILL.md`. All three async answers give
the effect but skip the mechanism: a runtime is needed (but not "std has no
executor"), a `!Send` value across `.await` breaks `spawn` (but not "it is
stored in the future's state machine"), blocking stalls the thread (but not
"scheduling is cooperative; tasks yield only at `.await`").
