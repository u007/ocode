---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: rust
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — cat-jam-free on rust

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book (Rule 0). The answerer was `ocode run -m opencode-go/cat-jam-free -effort med`
in an empty scratch directory with only `_prompts/rust.md` attached. The session
sqlite shows exactly 1 user + 1 assistant message and 0 tool calls, so no repo
file, answer key or web source was consulted. Headless runs inject ocode's
bundled skill-catalogue digest into the system prompt. `opencode-go` publishes no
version for this free/alpha model; it self-identifies as "CatJam, developed by
LitterBox AI". All 31 ids are present and none is truncated.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| rust-ownership-01 | ownership | 3 | 2 | 1 | 0.50 | (judgment) move + `a` invalidated present; never gives the reason — single owner frees the buffer on drop / prevents double-free. "heap buffer now belongs to `b`" restates the move, not why Rust moves |
| rust-ownership-02 | ownership | 2 | 3 | 2 | 0.67 | "duplicated automatically instead of moved" covers no-move, but never states `Copy` requires `Clone` — compound point 2 withheld (consistent with prior rust scorecards) |
| rust-ownership-03 | ownership, borrowing | 2 | 2 | 2 | 1.00 | |
| rust-borrowing-01 | borrowing | 3 | 2 | 2 | 1.00 | |
| rust-lifetimes-01 | lifetimes | 2 | 2 | 2 | 1.00 | |
| rust-lifetimes-02 | lifetimes | 2 | 3 | 3 | 1.00 | |
| rust-lifetimes-03 | lifetimes, borrowing | 2 | 2 | 2 | 1.00 | |
| rust-lifetimes-04 | lifetimes, traits | 1 | 2 | 2 | 1.00 | |
| rust-traits-01 | traits | 3 | 3 | 3 | 1.00 | "fat pointer" not named; vtable + heterogeneous carries point 2 |
| rust-traits-02 | traits | 2 | 3 | 3 | 1.00 | all return paths same concrete type stated |
| rust-traits-03 | traits | 2 | 2 | 2 | 1.00 | |
| rust-error-01 | error-handling | 2 | 2 | 2 | 1.00 | |
| rust-error-02 | error-handling | 3 | 2 | 2 | 1.00 | |
| rust-error-03 | error-handling | 2 | 2 | 2 | 1.00 | |
| rust-error-04 | error-handling, traits | 2 | 3 | 3 | 1.00 | "variants wrapping each source error" = the enum |
| rust-iterators-01 | iterators | 3 | 2 | 2 | 1.00 | |
| rust-iterators-02 | iterators, ownership | 2 | 2 | 2 | 1.00 | |
| rust-iterators-03 | iterators | 2 | 2 | 2 | 1.00 | |
| rust-iterators-04 | iterators | 2 | 2 | 2 | 1.00 | |
| rust-smartptr-01 | smart-pointers | 2 | 2 | 2 | 1.00 | "allocates on the heap and owns it" = single-owner heap allocation; fixed-size handle not spelled out (minor) |
| rust-smartptr-02 | smart-pointers, concurrency | 2 | 3 | 3 | 1.00 | |
| rust-smartptr-03 | smart-pointers, borrowing | 3 | 3 | 3 | 1.00 | |
| rust-smartptr-04 | smart-pointers, concurrency | 3 | 3 | 3 | 1.00 | |
| rust-concurrency-01 | concurrency | 3 | 3 | 3 | 1.00 | bounds-enforced compile errors stated; "auto-trait" not named (minor) |
| rust-async-01 | async | 3 | 3 | 2 | 0.67 | lazy future + driven by executor/`.await` present; never states std ships no executor — Tokio/async-std/`block_on` listed as options only |
| rust-async-02 | async, concurrency | 2 | 2 | 1 | 0.50 | `spawn` requires `Send`; `Rc` `!Send`; guard held across `.await` is non-`Send` → point 2. Never says values alive across `.await` are stored in the future's state machine, nor that this makes the whole future `!Send` — point 1 withheld |
| rust-async-03 | async | 2 | 2 | 1 | 0.50 | missed cooperative scheduling / yields-only-at-`.await`; gives only the effect (executor thread occupied, other tasks not polled) and the `spawn_blocking` fix |
| rust-match-01 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| rust-match-02 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| rust-match-03 | pattern-matching, borrowing | 2 | 2 | 2 | 1.00 | |
| rust-match-04 | pattern-matching | 1 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| ownership | 0.76 | 4 | ok | **derive** |
| borrowing | 1.00 | 5 | ok | omit (strong) |
| lifetimes | 1.00 | 4 | ok | omit (strong) |
| traits | 1.00 | 5 | ok | omit (strong) |
| error-handling | 1.00 | 4 | ok | omit (strong) |
| iterators | 1.00 | 4 | ok | omit (strong) |
| smart-pointers | 1.00 | 4 | ok | omit (strong) |
| concurrency | 0.90 | 4 | ok | omit (strong) |
| async | 0.57 | 3 | low-n | **derive** (mark low-n) |
| pattern-matching | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

ownership = (3×0.5 + 2×0.667 + 2×1 + 2×1) / 9 = 6.83 / 9 = 0.759.

concurrency = (2×1 + 3×1 + 3×1 + 2×0.5) / 10 = 9 / 10 = 0.90.

async = (3×0.667 + 2×0.5 + 2×0.5) / 7 = 4 / 7 = 0.571.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 63.83 / 69 = 92.5%
```

## Derivation targets

Tags below threshold (`< 0.85`): **ownership, async** (async low-n, 3 questions)
→ feed into `derived/rust.cat-jam-free.SKILL.md`.

Pattern: answers are terse and correct on the effect but drop the *why*:
the single-owner/double-free rationale for moves, `Copy: Clone`, std's missing
executor, held-across-`.await` state-machine capture, and cooperative scheduling.

## Contamination check

No verbatim overlap with the reference answers. Answers are short and
differently phrased; the misses are exactly the reference-answer phrases a
copying model would have reproduced ("prevents double-free", "Copy requires
Clone", "std has no executor", "captured in the future's state machine",
"cooperative"), consistent with a genuine closed-book run.
