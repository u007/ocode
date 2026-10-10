---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-05
stack: rust
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — longcat-2.5-preview-free on rust (2026-10-05 full re-run)

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump invalidates
> this scorecard — re-benchmark.
>
> Full re-run (baseline rerun 2026-10-05) of the closed-book answers in
> `answers/longcat-2.5-preview-free.rerun-2026-10-05.md`. An earlier 2026-09-28
> scorecard exists beside this one; it was not consulted.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| rust-ownership-01 | ownership | 3 | 2 | 2 | 1.00 |  |
| rust-ownership-02 | ownership | 2 | 3 | 2 | 0.67 | missed "Copy requires Clone" |
| rust-ownership-03 | ownership, borrowing | 2 | 2 | 2 | 1.00 |  |
| rust-borrowing-01 | borrowing | 3 | 2 | 2 | 1.00 |  |
| rust-lifetimes-01 | lifetimes | 2 | 2 | 2 | 1.00 |  |
| rust-lifetimes-02 | lifetimes | 2 | 3 | 3 | 1.00 |  |
| rust-lifetimes-03 | lifetimes, borrowing | 2 | 2 | 2 | 1.00 |  |
| rust-lifetimes-04 | lifetimes, traits | 1 | 2 | 2 | 1.00 |  |
| rust-traits-01 | traits | 3 | 3 | 3 | 1.00 |  |
| rust-traits-02 | traits | 2 | 3 | 2 | 0.67 | missed all-branches-same-type / avoids boxing |
| rust-traits-03 | traits | 2 | 2 | 2 | 1.00 |  |
| rust-error-01 | error-handling | 2 | 2 | 2 | 1.00 |  |
| rust-error-02 | error-handling | 3 | 2 | 2 | 1.00 | From direction stated loosely (E: From<E'>) but From::from conversion present |
| rust-error-03 | error-handling | 2 | 2 | 2 | 1.00 |  |
| rust-error-04 | error-handling, traits | 2 | 3 | 3 | 1.00 |  |
| rust-iterators-01 | iterators | 3 | 2 | 2 | 1.00 |  |
| rust-iterators-02 | iterators, ownership | 2 | 2 | 2 | 1.00 |  |
| rust-iterators-03 | iterators | 2 | 2 | 2 | 1.00 |  |
| rust-iterators-04 | iterators | 2 | 2 | 2 | 1.00 |  |
| rust-smartptr-01 | smart-pointers | 2 | 2 | 2 | 1.00 |  |
| rust-smartptr-02 | smart-pointers, concurrency | 2 | 3 | 3 | 1.00 |  |
| rust-smartptr-03 | smart-pointers, borrowing | 3 | 3 | 3 | 1.00 |  |
| rust-smartptr-04 | smart-pointers, concurrency | 3 | 3 | 3 | 1.00 |  |
| rust-concurrency-01 | concurrency | 3 | 3 | 3 | 1.00 |  |
| rust-async-01 | async | 3 | 3 | 3 | 1.00 |  |
| rust-async-02 | async, concurrency | 2 | 2 | 2 | 1.00 |  |
| rust-async-03 | async | 2 | 2 | 2 | 1.00 |  |
| rust-match-01 | pattern-matching | 2 | 2 | 2 | 1.00 |  |
| rust-match-02 | pattern-matching | 2 | 2 | 2 | 1.00 |  |
| rust-match-03 | pattern-matching, borrowing | 2 | 2 | 2 | 1.00 |  |
| rust-match-04 | pattern-matching | 1 | 2 | 2 | 1.00 |  |

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
| smart-pointers | 1.00 | 4 | ok | omit (strong) |
| concurrency | 1.00 | 4 | ok | omit (strong) |
| async | 1.00 | 3 | low-n | omit (strong, low-n) |
| pattern-matching | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 67.67 / 69 = 98.1%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none**. No derived skill is warranted.
