---
model_id: nvidia/nemotron-3.5-super-vl
model_version: "3.5"
evaluated_via: openrouter
evaluated_on: 2026-10-16
stack: rust
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — nvidia/nemotron-3.5-super-vl on rust

> Valid ONLY for `nvidia/nemotron-3.5-super-vl` @ `3.5`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book (Rule 0). The answerer was `ocode run -m openrouter/nvidia/nemotron-3.5-super-vl:free -effort med`
under an isolated HOME whose config denies every tool (no tools were advertised),
in a scratch cwd containing only the `_prompts/rust.md` sheet, one fresh session;
the session sqlite shows exactly 1 user + 1 assistant message and 0 tool calls.
All 31 ids came back once, none empty, none truncated. Headless runs inject the
bundled skill-catalogue digest into the system prompt; no answer cites a tool or a
skill the question never named, and there is no thinking-aloud or fabricated tool
output in the answer file. The OpenRouter `:free` route variant is not part of the
model id.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| rust-ownership-01 | ownership | 3 | 2 | 2 | 1.00 | |
| rust-ownership-02 | ownership | 2 | 3 | 2 | 0.67 | "the original stays usable" covers no-move, but never states `Copy` requires `Clone` — compound point 2 only half present, withheld (consistent with prior rust scorecards). Points 1 and 3 awarded |
| rust-ownership-03 | ownership, borrowing | 2 | 2 | 2 | 1.00 | |
| rust-borrowing-01 | borrowing | 3 | 2 | 2 | 1.00 | |
| rust-lifetimes-01 | lifetimes | 2 | 2 | 2 | 1.00 | |
| rust-lifetimes-02 | lifetimes | 2 | 3 | 3 | 1.00 | |
| rust-lifetimes-03 | lifetimes, borrowing | 2 | 2 | 2 | 1.00 | |
| rust-lifetimes-04 | lifetimes, traits | 1 | 2 | 2 | 1.00 | |
| rust-traits-01 | traits | 3 | 3 | 3 | 1.00 | also notes the object-safety requirement |
| rust-traits-02 | traits | 2 | 3 | 2 | 0.67 | caller-chooses / callee-chooses-opaque-type present (points 1, 2). Point 3 withheld: neither "every return path must be the same concrete type" nor "returns closures/iterators without boxing" is stated |
| rust-traits-03 | traits | 2 | 2 | 2 | 1.00 | |
| rust-error-01 | error-handling | 2 | 2 | 2 | 1.00 | |
| rust-error-02 | error-handling | 3 | 2 | 2 | 1.00 | |
| rust-error-03 | error-handling | 2 | 2 | 2 | 1.00 | |
| rust-error-04 | error-handling, traits | 2 | 3 | 3 | 1.00 | enum stated via the `thiserror` sentence; `Debug` correctly listed as required (supertrait of `Error`) |
| rust-iterators-01 | iterators | 3 | 2 | 2 | 1.00 | |
| rust-iterators-02 | iterators, ownership | 2 | 2 | 2 | 1.00 | |
| rust-iterators-03 | iterators | 2 | 2 | 2 | 1.00 | |
| rust-iterators-04 | iterators | 2 | 2 | 2 | 1.00 | |
| rust-smartptr-01 | smart-pointers | 2 | 2 | 2 | 1.00 | |
| rust-smartptr-02 | smart-pointers, concurrency | 2 | 3 | 3 | 1.00 | |
| rust-smartptr-03 | smart-pointers, borrowing | 3 | 3 | 3 | 1.00 | |
| rust-smartptr-04 | smart-pointers, concurrency | 3 | 3 | 2.5 | 0.83 | (judgment) says "in single-threaded code" but never why (`Rc`/`RefCell` non-atomic, `!Sync`) — restates the question's premise; half credit on point 2, consistent with the stricter prior rust scorecards |
| rust-concurrency-01 | concurrency | 3 | 3 | 3 | 1.00 | |
| rust-async-01 | async | 3 | 3 | 2 | 0.67 | missed "std ships no executor"; names "an executor/runtime like Tokio" but never states the standard library provides none |
| rust-async-02 | async, concurrency | 2 | 2 | 1.5 | 0.75 | (judgment) state-machine capture (point 1) and `!Send` future / `spawn` needs `Send` present, but adds a materially wrong claim about the same construct: "`Rc` is `!Send` and not `'static`" — `Rc<T>` is `'static` whenever `T: 'static`; the failure is `Send` only. Half credit on point 2 |
| rust-async-03 | async | 2 | 2 | 1 | 0.50 | missed cooperative scheduling / yields-only-at-`.await`; gives only the effect (executor thread stalled, other tasks not polled) and the `spawn_blocking` fix |
| rust-match-01 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| rust-match-02 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| rust-match-03 | pattern-matching, borrowing | 2 | 2 | 2 | 1.00 | |
| rust-match-04 | pattern-matching | 1 | 2 | 2 | 1.00 | concepts all present; the guard snippet `=> if x > 0` puts the guard after the arrow (real syntax is `Some(x) if x > 0 =>`) — would not compile (outside rubric, noted as in prior scorecards) |

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
| concurrency | 0.90 | 4 | ok | omit (strong) |
| async | 0.64 | 3 | low-n | **derive** (mark low-n) |
| pattern-matching | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

ownership = (3×1 + 2×0.667 + 2×1 + 2×1) / 9 = 8.33 / 9 = 0.926.
traits = (1×1 + 3×1 + 2×0.667 + 2×1 + 2×1) / 10 = 9.33 / 10 = 0.933.
smart-pointers = (2×1 + 2×1 + 3×1 + 3×0.833) / 10 = 9.5 / 10 = 0.95.
concurrency = (2×1 + 3×0.833 + 3×1 + 2×0.75) / 10 = 9.0 / 10 = 0.90.
async = (3×0.667 + 2×0.75 + 2×0.5) / 7 = 4.5 / 7 = 0.643.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 64.67 / 69 = 93.7%
```

## Derivation targets

Tags below threshold (`< 0.85`): **async** (low-n, 3 questions) → feed into
`derived/rust.nvidia__nemotron-3.5-super-vl.SKILL.md`.

## Contamination check

No verbatim overlap with the reference answers. Answers are consistently terser
than the key and use different phrasing ("use of moved value" vs the key's "value
borrowed after move"; "descriptive, not prescriptive" for lifetimes; "requires
object-safe traits" which the key never mentions). The three async misses are
exactly the reference-answer phrases a copying model would have reproduced ("Rust
ships no built-in executor", "cooperatively scheduled … only yields at `.await`"),
and the `Rc`-not-`'static` slip appears nowhere in the key, which is consistent
with a genuine closed-book run.
