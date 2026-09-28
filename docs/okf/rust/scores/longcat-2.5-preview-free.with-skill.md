---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: rust
stack_corpus_rev: 1
threshold: 0.75
---

<!-- WITH-SKILL VALIDATION RUN. The derived skill
     `derived/rust.longcat-2.5-preview-free.SKILL.md`
     (name: rust-tuning-longcat-2.5-preview-free) was prepended to the answerer
     prompt as active guidance while these answers were produced closed-book.
     Graded by an independent grader to the same strict standard as the
     baseline `longcat-2.5-preview-free.md`: points awarded only where the
     concept is genuinely and correctly present. This is validation, not
     derivation: no derived skill is written or modified from this run. -->

# Scorecard — longcat-2.5-preview-free on rust (WITH-SKILL validation)

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump
> invalidates this scorecard — re-benchmark.
>
> **WITH-SKILL run:** derived skill `rust-tuning-longcat-2.5-preview-free`
> (`../derived/rust.longcat-2.5-preview-free.SKILL.md`, target tag: **async**,
> low-n) active during answering. Baseline for comparison:
> `longcat-2.5-preview-free.md`.

Answers graded from `../answers/longcat-2.5-preview-free.with-skill.md`
(produced via `ocode run` from an isolated dir with the SKILL.md body prepended;
session audited: 1 user + 1 assistant turn, zero tool calls). All 31 questions
answered.

**Contamination check:** clean with respect to the answer key; non-async answers
are in the model's own wording and none reproduces reference-answer phrasing.
The three async answers, however, echo the **skill** text near-verbatim
(`rust-async-02` is essentially the skill's "values held across `.await`"
section with a sentence added on why `Rc`/`MutexGuard` are `!Send`;
`rust-async-01` and `-03` lift whole sentences from the skill). Per HOW-TO
"Overlap ≠ cheating" that is the intended absorption signal, not key leakage,
but it means this run shows the model *applies* injected guidance, not that it
reasons about async independently.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| rust-ownership-01 | ownership | 3 | 2 | 2 | 1.00 | move invalidates `a`; single ownership without GC |
| rust-ownership-02 | ownership | 2 | 3 | 2.5 | 0.83 | "original remains valid" present but still never states Copy requires Clone (half credit on point 2); Drop conflict now stated correctly — "`Drop` would run multiple times, causing double-frees" (baseline muddled this) |
| rust-ownership-03 | ownership, borrowing | 2 | 2 | 2 | 1.00 | |
| rust-borrowing-01 | borrowing | 3 | 2 | 2 | 1.00 | |
| rust-lifetimes-01 | lifetimes | 2 | 2 | 2 | 1.00 | |
| rust-lifetimes-02 | lifetimes | 2 | 3 | 3 | 1.00 | |
| rust-lifetimes-03 | lifetimes, borrowing | 2 | 2 | 2 | 1.00 | |
| rust-lifetimes-04 | lifetimes, traits | 1 | 2 | 2 | 1.00 | |
| rust-traits-01 | traits | 3 | 3 | 3 | 1.00 | monomorphization + vtable + heterogeneous collections |
| rust-traits-02 | traits | 2 | 3 | 2 | 0.67 | same miss as baseline: no "every return path must be one concrete type / lets you return closures-iterators without boxing" |
| rust-traits-03 | traits | 2 | 2 | 2 | 1.00 | |
| rust-error-01 | error-handling | 2 | 2 | 2 | 1.00 | |
| rust-error-02 | error-handling | 3 | 2 | 2 | 1.00 | |
| rust-error-03 | error-handling | 2 | 2 | 2 | 1.00 | |
| rust-error-04 | error-handling, traits | 2 | 3 | 3 | 1.00 | custom error type implementing Error (Debug+Display), `From` per source, thiserror `#[from]`; "enum" never named this run — accepted, the Error+Display machinery the point tests is present |
| rust-iterators-01 | iterators | 3 | 2 | 2 | 1.00 | |
| rust-iterators-02 | iterators, ownership | 2 | 2 | 2 | 1.00 | |
| rust-iterators-03 | iterators | 2 | 2 | 2 | 1.00 | |
| rust-iterators-04 | iterators | 2 | 2 | 2 | 1.00 | |
| rust-smartptr-01 | smart-pointers | 2 | 2 | 2 | 1.00 | |
| rust-smartptr-02 | smart-pointers, concurrency | 2 | 3 | 3 | 1.00 | |
| rust-smartptr-03 | smart-pointers, borrowing | 3 | 3 | 3 | 1.00 | |
| rust-smartptr-04 | smart-pointers, concurrency | 3 | 3 | 2.5 | 0.83 | same as baseline: calls `Rc<RefCell>` "single-threaded" but never says why (non-atomic count / `!Send`+`!Sync`); half credit on point 2 |
| rust-concurrency-01 | concurrency | 3 | 3 | 3 | 1.00 | now states structural derivation ("every type is Send+Sync unless it contains non-Send/Sync components") and `spawn` requires Send (baseline's "Send **and Sync**" slip gone) |
| rust-async-01 | async | 3 | 3 | 3 | 1.00 | **target.** "The standard library ships no executor … nothing that polls a future to completion"; names Tokio/async-std/smol/`block_on` (baseline missed this point) |
| rust-async-02 | async, concurrency | 2 | 2 | 2 | 1.00 | **target.** "every value still alive at an `.await` point is saved as a field of that state machine" → future `!Send` → `tokio::spawn` rejects (baseline half-credit on point 1) |
| rust-async-03 | async | 2 | 2 | 2 | 1.00 | **target.** "no preemption … yields only at an `.await` that returns `Poll::Pending`", then starvation, then `spawn_blocking` / `tokio::time::sleep` (baseline missed the mechanism) |
| rust-match-01 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| rust-match-02 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| rust-match-03 | pattern-matching, borrowing | 2 | 2 | 2 | 1.00 | |
| rust-match-04 | pattern-matching | 1 | 2 | 2 | 1.00 | |

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
| smart-pointers | 0.95 | 4 | ok | omit (strong) |
| concurrency | 0.95 | 4 | ok | omit (strong) |
| async | 1.00 | 3 | low-n | **target — validated** (mark low-n) |
| pattern-matching | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Baseline vs with-skill comparison

| tag | baseline | with-skill | target? | verdict |
|-----|---------:|-----------:|---------|---------|
| ownership | 0.93 | 0.96 | no | +0.03, non-target drift (ownership-02 Drop reason stated correctly this sample) — noise, not acted on |
| borrowing | 1.00 | 1.00 | no | unchanged |
| lifetimes | 1.00 | 1.00 | no | unchanged |
| traits | 0.93 | 0.93 | no | unchanged (same miss on traits-02) |
| error-handling | 1.00 | 1.00 | no | unchanged |
| iterators | 1.00 | 1.00 | no | unchanged |
| smart-pointers | 0.95 | 0.95 | no | unchanged (same half-point on smartptr-04) |
| concurrency | 0.85 | 0.95 | no | +0.10: concurrency-01 slip absent this sample (non-target drift) plus async-02 lift (skill-driven, shared tag) |
| **async** | **0.64** | **1.00** | **yes** | **PASS** — crosses 0.75; all three previously-missed mechanism points (std ships no executor; values across `.await` stored in the state machine; cooperative, yields only at `.await`) now present (low-n, n=3) |
| pattern-matching | 1.00 | 1.00 | no | unchanged |

## Target tags

| tag | baseline | with-skill | verdict |
|-----|---------:|-----------:|---------|
| async (low-n) | 0.64 | 1.00 | **PASS** |

No FAILs. Caveat: the async answers track the skill text nearly word for word,
so this validates absorption of injected guidance; with n=3 the tag stays
low-n.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 67.5 / 69 = 97.8%
```

(baseline: 64.17 / 69 = 93.0%)

## Derivation targets

Tags below threshold (`< 0.75`): **none**. The single target tag **async**
moved 0.64 → 1.00 with `rust-tuning-longcat-2.5-preview-free` active, so the
skill is validated. It remains low-n (3 questions). No corrective skill is
(re)written from this run.
