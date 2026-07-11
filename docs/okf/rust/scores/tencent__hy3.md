---
model_id: tencent/hy3
model_version: "3.0"
evaluated_via: novita
evaluated_on: 2026-07-11
stack: rust
stack_corpus_rev: 1
threshold: 0.75
---

<!-- Grader: independent model (Opus 4.8). Answers were produced CLOSED-BOOK
     (model never saw the rubric). Strict grading: a point is awarded only where
     the concept is genuinely and correctly present. -->

# Scorecard — tencent/hy3 on rust

> Valid ONLY for `tencent/hy3` @ `3.0`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| rust-ownership-01 | ownership | 3 | 2 | 2 | 1.00 | Move, single-owner, double-free prevention all present. |
| rust-ownership-02 | ownership | 2 | 3 | 2 | 0.67 | Copy=implicit bitwise / Clone=explicit deep ✓; Drop+Copy double-drop ✓. Missed: never states `Copy` requires `Clone` (supertrait bound) — the second rubric point's compound concept is only half present. |
| rust-ownership-03 | ownership, borrowing | 2 | 2 | 2 | 1.00 | Pass-by-value moves ✓; borrow (&/&mut) as idiomatic fix ✓. |
| rust-borrowing-01 | borrowing | 3 | 2 | 2 | 1.00 | Shared-XOR-mutable ✓; data-race/UAF/iterator-invalidation at compile time ✓. |
| rust-lifetimes-01 | lifetimes | 2 | 2 | 2 | 1.00 | Region/constraint framing ✓; compile-time-only, does not extend value life ✓. |
| rust-lifetimes-02 | lifetimes | 2 | 3 | 3 | 1.00 | All three elision rules stated correctly. |
| rust-lifetimes-03 | lifetimes, borrowing | 2 | 2 | 2 | 1.00 | &'static T vs T:'static cleanly separated; owned types qualify ✓. |
| rust-lifetimes-04 | lifetimes, traits | 1 | 2 | 2 | 1.00 | dyn defaults to +'static ✓; +'a for shorter borrow ✓. |
| rust-traits-01 | traits | 3 | 3 | 3 | 1.00 | Monomorphization/static ✓; vtable+heterogeneous ✓; tradeoff ✓. |
| rust-traits-02 | traits | 2 | 3 | 3 | 1.00 | arg=universal/caller, return=existential/callee, same-type-all-paths, avoids boxing ✓. |
| rust-traits-03 | traits | 2 | 2 | 2 | 1.00 | Orphan rule (trait-or-type local) ✓; coherence/no conflicting impls ✓. |
| rust-error-01 | error-handling | 2 | 2 | 2 | 1.00 | Option=absence ✓; Result=failure carries E ✓. |
| rust-error-02 | error-handling | 3 | 2 | 2 | 1.00 | Ok-unwrap/Err-early-return ✓; From::from conversion ✓. |
| rust-error-03 | error-handling | 2 | 2 | 2 | 1.00 | Result=recoverable ✓; panic=bugs/invariants ✓. |
| rust-error-04 | error-handling, traits | 2 | 3 | 3 | 1.00 | Error+Display ✓; From per source ✓; thiserror derives ✓. (Says "error type" not literally "enum" but variant/wrap structure is clear.) |
| rust-iterators-01 | iterators | 3 | 2 | 2 | 1.00 | Lazy adapters build pipeline ✓; consumer pulls element-at-a-time ✓. |
| rust-iterators-02 | iterators, ownership | 2 | 2 | 2 | 1.00 | iter/&T, iter_mut/&mut T, into_iter owned+consumes ✓. |
| rust-iterators-03 | iterators | 2 | 2 | 2 | 1.00 | FromIterator annotation/turbofish ✓; Result short-circuits first Err ✓. |
| rust-iterators-04 | iterators | 2 | 2 | 2 | 1.00 | Default borrow vs move-ownership ✓; outlives-scope reason ✓. |
| rust-smartptr-01 | smart-pointers | 2 | 2 | 2 | 1.00 | Single-owner heap handle ✓; recursive/trait-object/large needs ✓. |
| rust-smartptr-02 | smart-pointers, concurrency | 2 | 3 | 3 | 1.00 | Shared ref-count ✓; Rc non-atomic !Send/!Sync vs Arc atomic ✓; atomic cost → prefer Rc ✓. |
| rust-smartptr-03 | smart-pointers, borrowing | 3 | 3 | 3 | 1.00 | Interior mutability ✓; runtime borrow check ✓; panic-on-conflict cost ✓. |
| rust-smartptr-04 | smart-pointers, concurrency | 3 | 3 | 3 | 1.00 | Rc(shared)+RefCell(interior mut) combine ✓; single-threaded (non-atomic implied via thread-safe Arc contrast) ✓; Arc<Mutex>/RwLock ✓. |
| rust-concurrency-01 | concurrency | 3 | 3 | 3 | 1.00 | Send=transfer ✓; Sync=&T is Send ✓; auto-traits as bounds → races won't compile ✓. |
| rust-async-01 | async | 3 | 3 | 3 | 1.00 | Returns lazy Future ✓; must be polled/awaited ✓; std ships no executor (needs Tokio/async-std) ✓. |
| rust-async-02 | async, concurrency | 2 | 2 | 2 | 1.00 | Held-across-await infects the future ✓; non-Send → future !Send, spawn needs Send ✓. |
| rust-async-03 | async | 2 | 2 | 2 | 1.00 | Cooperative scheduling / blocking never yields ✓; spawn_blocking / async sleep ✓. |
| rust-match-01 | pattern-matching | 2 | 2 | 2 | 1.00 | Exhaustiveness or no-compile ✓; add-a-variant forces handling ✓. |
| rust-match-02 | pattern-matching | 2 | 2 | 2 | 1.00 | if let single pattern ✓; let-else bind-happy-path + diverging else ✓. |
| rust-match-03 | pattern-matching, borrowing | 2 | 2 | 2 | 1.00 | x is &T ✓; match ergonomics / default binding modes ✓. |
| rust-match-04 | pattern-matching | 1 | 2 | 2 | 1.00 | Destructuring ✓; match guards + @ bindings ✓. |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| ownership | 0.93 | 4 | ok | omit (strong) |
| borrowing | 1.00 | 5 | ok | omit (strong) |
| lifetimes | 1.00 | 4 | ok | omit (strong) |
| traits | 1.00 | 5 | ok | omit (strong) |
| error-handling | 1.00 | 4 | ok | omit (strong) |
| iterators | 1.00 | 4 | ok | omit (strong) |
| smart-pointers | 1.00 | 4 | ok | omit (strong) |
| concurrency | 1.00 | 4 | ok | omit (strong) |
| async | 1.00 | 3 | low-n | omit (strong, low-n) |
| pattern-matching | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

ownership: (3×1.00 + 2×0.67 + 2×1.00 + 2×1.00[iterators-02]) / (3+2+2+2) = 8.33/9 = 0.93.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 68.33 / 69 = 99.0%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none**. Every tag scores ≥ 0.75, so **no
derivation** — no `derived/rust.tencent__hy3.SKILL.md` is written. The single
partial miss (ownership-02, "Copy requires Clone" omitted) does not pull the
ownership subscore (0.93) below threshold.
