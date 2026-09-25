---
model_id: space-bunny-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-09-24
stack: rust
stack_corpus_rev: 1
threshold: 0.75
---

<!-- WITH-SKILL VALIDATION RUN. The derived skill
     `derived/rust.space-bunny-free.SKILL.md` (name: rust-tuning-space-bunny-free)
     was prepended to the answerer prompt as active guidance while these answers
     were produced closed-book. Graded by an independent grader to the same
     strict standard as the baseline `space-bunny-free.md` — points awarded only
     where the concept is genuinely and correctly present. This is validation,
     not derivation: no derived skill is written or modified from this run. -->

# Scorecard — space-bunny-free on rust (WITH-SKILL validation)

> Valid ONLY for `space-bunny-free` @ `alpha`. A version bump invalidates this
> scorecard — re-benchmark.
>
> **WITH-SKILL run:** derived skill `rust-tuning-space-bunny-free`
> (`../derived/rust.space-bunny-free.SKILL.md`, target tag: **async**, low-n)
> active during answering. Baseline for comparison: `space-bunny-free.md`.

Answers graded closed-book from `../answers/space-bunny-free.with-skill.md`.
Wording throughout is the model's own — no answer looked like a copy of the
reference. On the two async questions the answer tracks the skill's digest
closely (it names `Future`/`Poll`/`Waker`, `block_on`, `Poll::Pending`); that
overlap is the intended absorption signal, not contamination (see HOW-TO,
"Overlap ≠ cheating").

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| rust-ownership-01 | ownership | 3 | 2 | 2 | 1.00 | move + invalidated; double-free / sole owner |
| rust-ownership-02 | ownership | 2 | 3 | 2.5 | 0.83 | implicit vs explicit/deep present; never states Copy requires Clone and "both bindings usable" only implied (half credit on point 2); Drop conflict phrased as "two values owning the same resources" — accepted, though "only the original would be dropped after a move" is muddled |
| rust-ownership-03 | ownership, borrowing | 2 | 2 | 2 | 1.00 | |
| rust-borrowing-01 | borrowing | 3 | 2 | 2 | 1.00 | |
| rust-lifetimes-01 | lifetimes | 2 | 2 | 2 | 1.00 | |
| rust-lifetimes-02 | lifetimes | 2 | 3 | 3 | 1.00 | |
| rust-lifetimes-03 | lifetimes, borrowing | 2 | 2 | 2 | 1.00 | |
| rust-lifetimes-04 | lifetimes, traits | 1 | 2 | 2 | 1.00 | |
| rust-traits-01 | traits | 3 | 3 | 3 | 1.00 | monomorphization and vtable both named this run |
| rust-traits-02 | traits | 2 | 3 | 2 | 0.67 | same miss as baseline: no "all branches same type / avoids boxing closures-iterators" |
| rust-traits-03 | traits | 2 | 2 | 2 | 1.00 | |
| rust-error-01 | error-handling | 2 | 2 | 2 | 1.00 | |
| rust-error-02 | error-handling | 3 | 2 | 2 | 1.00 | baseline's "Ok returns immediately" slip is gone: "evaluates to the inner value, execution continues" |
| rust-error-03 | error-handling | 2 | 2 | 2 | 1.00 | |
| rust-error-04 | error-handling, traits | 2 | 3 | 3 | 1.00 | |
| rust-iterators-01 | iterators | 3 | 2 | 2 | 1.00 | |
| rust-iterators-02 | iterators, ownership | 2 | 2 | 2 | 1.00 | |
| rust-iterators-03 | iterators | 2 | 2 | 2 | 1.00 | |
| rust-iterators-04 | iterators | 2 | 2 | 2 | 1.00 | |
| rust-smartptr-01 | smart-pointers | 2 | 2 | 2 | 1.00 | "unique ownership" this run (baseline's multi-owner slip gone) |
| rust-smartptr-02 | smart-pointers, concurrency | 2 | 3 | 3 | 1.00 | |
| rust-smartptr-03 | smart-pointers, borrowing | 3 | 3 | 3 | 1.00 | |
| rust-smartptr-04 | smart-pointers, concurrency | 3 | 3 | 3 | 1.00 | |
| rust-concurrency-01 | concurrency | 3 | 3 | 3 | 1.00 | |
| rust-async-01 | async | 3 | 3 | 3 | 1.00 | **target.** now states "std has no built-in executor … nothing that polls a future to completion", names Tokio/async-std/smol/`block_on` (baseline missed this point) |
| rust-async-02 | async, concurrency | 2 | 2 | 2 | 1.00 | |
| rust-async-03 | async | 2 | 2 | 2 | 1.00 | **target.** now leads with "cooperative: a task yields only when an `.await` produces `Poll::Pending`, no preemption, blocking never reaches a yield point", then starvation, then `spawn_blocking` (baseline missed the mechanism) |
| rust-match-01 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| rust-match-02 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| rust-match-03 | pattern-matching, borrowing | 2 | 2 | 2 | 1.00 | |
| rust-match-04 | pattern-matching | 1 | 2 | 2 | 1.00 | concepts all present; combined example's guard `x > y` uses `y` which the pattern `y: n @ 1..=9` does not bind — would not compile (outside rubric) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| ownership | 0.96 | 4 | ok | omit (strong) |
| borrowing | 1.00 | 5 | ok | omit (strong) |
| lifetimes | 1.00 | 4 | ok | omit (strong) |
| traits | 0.93 | 5 | ok | omit (strong) |
| error-handling | 1.00 | 4 | ok | omit (strong) |
| iterators | 1.00 | 4 | ok | omit (strong) |
| smart-pointers | 1.00 | 4 | ok | omit (strong) |
| concurrency | 1.00 | 4 | ok | omit (strong) |
| async | 1.00 | 3 | low-n | **target — validated** (mark low-n) |
| pattern-matching | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Baseline vs with-skill comparison

| tag | baseline | with-skill | target? | verdict |
|-----|---------:|-----------:|---------|---------|
| ownership | 0.96 | 0.96 | no | unchanged (same half-point on ownership-02) |
| borrowing | 1.00 | 1.00 | no | unchanged |
| lifetimes | 1.00 | 1.00 | no | unchanged |
| traits | 0.93 | 0.93 | no | unchanged (same miss on traits-02) |
| error-handling | 0.92 | 1.00 | no | +0.08, non-target drift (error-02 factual slip absent this sample) — noise, not acted on |
| iterators | 1.00 | 1.00 | no | unchanged |
| smart-pointers | 1.00 | 1.00 | no | unchanged |
| concurrency | 1.00 | 1.00 | no | unchanged |
| **async** | **0.71** | **1.00** | **yes** | **PASS** — crosses 0.75; both previously-missed rubric points (std ships no executor; cooperative / yields only at `.await`) now present (low-n, n=3) |
| pattern-matching | 1.00 | 1.00 | no | unchanged |

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 68.0 / 69 = 98.6%
```

(baseline: 65.25 / 69 = 94.6%)

## Derivation targets

Tags below threshold (`< 0.75`): **none**. The single target tag **async**
moved 0.71 → 1.00 with `rust-tuning-space-bunny-free` active, so the skill is
validated. It remains low-n (3 questions); a corpus revision adding async
questions would raise trust. No corrective skill is (re)written from this run.
