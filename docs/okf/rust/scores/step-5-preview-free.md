---
model_id: step-5-preview-free
model_version: "5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-12
stack: rust
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — step-5-preview-free on rust

> Valid ONLY for `step-5-preview-free` @ `5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book (Rule 0). The answerer was `ocode run -m opencode-go/step-5-preview-free -effort med`
in an empty scratch directory with only `_prompts/rust.md` attached. The session
sqlite shows exactly 1 user + 1 assistant message and 0 tool calls, so no repo
file, answer key or web source was consulted. Two earlier attempts produced only a
one-line preamble ("I'll answer all 31 questions...") and stopped; only the
successful run (`answers/step-5-preview-free.md`) is graded here. All 31 ids are
present and none is truncated.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| rust-ownership-01 | ownership | 3 | 2 | 2 | 1.00 | |
| rust-ownership-02 | ownership | 2 | 3 | 2 | 0.67 | "source stays valid" covers no-move, but never states `Copy` requires `Clone` — compound point 2 only half present, withheld (consistent with prior rust scorecards) |
| rust-ownership-03 | ownership, borrowing | 2 | 2 | 2 | 1.00 | |
| rust-borrowing-01 | borrowing | 3 | 2 | 2 | 1.00 | |
| rust-lifetimes-01 | lifetimes | 2 | 2 | 2 | 1.00 | |
| rust-lifetimes-02 | lifetimes | 2 | 3 | 3 | 1.00 | |
| rust-lifetimes-03 | lifetimes, borrowing | 2 | 2 | 2 | 1.00 | |
| rust-lifetimes-04 | lifetimes, traits | 1 | 2 | 2 | 1.00 | also correctly notes `&'a dyn Trait` defaults its object lifetime to `'a` |
| rust-traits-01 | traits | 3 | 3 | 3 | 1.00 | |
| rust-traits-02 | traits | 2 | 3 | 3 | 1.00 | point 3 via "unnameable closures/iterator adapters … allocation-free" (avoids boxing); the all-branches-same-type half not stated |
| rust-traits-03 | traits | 2 | 2 | 2 | 1.00 | |
| rust-error-01 | error-handling | 2 | 2 | 2 | 1.00 | |
| rust-error-02 | error-handling | 3 | 2 | 2 | 1.00 | names the `Try`/`FromResidual` desugaring correctly |
| rust-error-03 | error-handling | 2 | 2 | 2 | 1.00 | |
| rust-error-04 | error-handling, traits | 2 | 3 | 3 | 1.00 | |
| rust-iterators-01 | iterators | 3 | 2 | 2 | 1.00 | |
| rust-iterators-02 | iterators, ownership | 2 | 2 | 2 | 1.00 | |
| rust-iterators-03 | iterators | 2 | 2 | 2 | 1.00 | |
| rust-iterators-04 | iterators | 2 | 2 | 2 | 1.00 | |
| rust-smartptr-01 | smart-pointers | 2 | 2 | 2 | 1.00 | minor aside: "one machine word regardless of `T`" is false for `Box<dyn Trait>`/`Box<[T]>` (fat pointer); fixed-size-handle concept still present |
| rust-smartptr-02 | smart-pointers, concurrency | 2 | 3 | 3 | 1.00 | minor aside: `Arc` refcount uses `fetch_add`/`fetch_sub`, not CAS |
| rust-smartptr-03 | smart-pointers, borrowing | 3 | 3 | 3 | 1.00 | minor aside: "leaked guard can poison the state" — `RefCell` has no poisoning; a leaked guard just keeps the cell borrowed |
| rust-smartptr-04 | smart-pointers, concurrency | 3 | 3 | 3 | 1.00 | |
| rust-concurrency-01 | concurrency | 3 | 3 | 2 | 0.67 | (judgment) Send definition correct, but lists `Cell`/`RefCell` as `!Send` — they are `Send` when `T: Send` (only `!Sync`). Materially wrong claim about the same construct → Send point withheld. Sync and auto-trait/bounds points awarded |
| rust-async-01 | async | 3 | 3 | 2 | 0.67 | missed "std ships no executor"; names `block_on`/`tokio::spawn` and "without a runtime polling it" but never states the standard library provides none |
| rust-async-02 | async, concurrency | 2 | 2 | 1.5 | 0.75 | `Send` requirement and `!Send` future correct; never states that values alive across `.await` are stored in the future's generated state machine (half credit on point 1) |
| rust-async-03 | async | 2 | 2 | 2 | 1.00 | |
| rust-match-01 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| rust-match-02 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| rust-match-03 | pattern-matching, borrowing | 2 | 2 | 2 | 1.00 | cites RFC 2005 correctly |
| rust-match-04 | pattern-matching | 1 | 2 | 2 | 1.00 | minor aside: guard/exhaustiveness sentence is muddled (guarded arms never count toward exhaustiveness), conclusion "`_` still needed" is right |

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
| concurrency | 0.85 | 4 | ok | omit (exactly at threshold, not below) |
| async | 0.79 | 3 | low-n | **derive** (mark low-n) |
| pattern-matching | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

concurrency = (2×1 + 3×1 + 3×0.667 + 2×0.75) / 10 = 8.5 / 10 = 0.85 — on the
line, not under it; the two misses are the `Cell`/`RefCell` `!Send` slip and the
async-02 state-machine mechanism, which the async section already covers.

async = (3×0.667 + 2×0.75 + 2×1) / 7 = 5.5 / 7 = 0.786.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 65.83 / 69 = 95.4%
```

## Derivation targets

Tags below threshold (`< 0.85`): **async** (low-n, 3 questions) → feed into
`derived/rust.step-5-preview-free.SKILL.md`.

## Contamination check

No verbatim overlap with the reference answers. The answers are consistently
longer, differently structured and use different phrasing (e.g. "borrow of
moved value" vs the key's "value borrowed after move"; a `Try`/`FromResidual`
desugaring the key never mentions). The two async misses are exactly the two
reference-answer phrases a copying model would have reproduced ("std has no
executor", "part of its generated state machine"), which is consistent with a
genuine closed-book run.
