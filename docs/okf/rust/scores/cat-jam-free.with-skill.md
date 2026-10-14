---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: rust
stack_corpus_rev: 1
threshold: 0.85
validation: with-skill
---

# Scorecard — cat-jam-free on rust (with-skill validation)

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers: `rust/answers/cat-jam-free.with-skill.md`,
> produced closed-book with the current derived skill
> `derived/rust.cat-jam-free.SKILL.md` (`rust-tuning-cat-jam-free`)
> prepended to the answerer prompt as active guidance (audited 1 user + 1 assistant
> message, zero tool calls). Baseline: `scores/cat-jam-free.md`. No new
> derived skill is produced from this run.

> Target tags for this skill (baseline < 0.85): **ownership** (0.76), **async** (0.57, low-n).

Graded against `rust/questions.yaml` corpus_rev 1. Grading rule applied (same
strictness as the baseline): a `point` whose concept is genuinely present earns
full credit (minor missing sub-items are noted); a `point` accompanied by a
materially wrong claim about the same construct is withheld and marked
"(judgment)". A compound point only half present is withheld. Points are whole;
fractions come only from a listed partial.

## History

- Earlier with-skill scorecard: none (this is the first with-skill validation for
  cat-jam-free on rust).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| rust-ownership-01 | ownership | 3 | 2 | 2 | 1.00 | move + `a` invalidated; "exactly one owner responsible for freeing that buffer on drop, which prevents a double-free" — the baseline miss, now present |
| rust-ownership-02 | ownership | 2 | 3 | 3 | 1.00 | "`Copy: Clone`, so every `Copy` type must implement `Clone`" plus "no move, source stays usable" — compound point 2 now complete; Drop double-destructor stated |
| rust-ownership-03 | ownership, borrowing | 2 | 2 | 2 | 1.00 | borrow (`&[T]`/`&mut [T]`), return it back, or clone |
| rust-borrowing-01 | borrowing | 3 | 2 | 2 | 1.00 | |
| rust-lifetimes-01 | lifetimes | 2 | 2 | 2 | 1.00 | |
| rust-lifetimes-02 | lifetimes | 2 | 3 | 3 | 1.00 | |
| rust-lifetimes-03 | lifetimes, borrowing | 2 | 2 | 2 | 1.00 | |
| rust-lifetimes-04 | lifetimes, traits | 1 | 2 | 2 | 1.00 | |
| rust-traits-01 | traits | 3 | 3 | 3 | 1.00 | "fat pointer" not named (same as baseline); vtable + heterogeneous carries point 2 |
| rust-traits-02 | traits | 2 | 3 | 2 | 0.67 | caller-chooses / callee-chooses-one-concrete-type present (points 1, 2). Point 3 withheld: neither "all return paths must be the same type" (which earned the point in the baseline) nor "return closures/iterators without boxing" is stated this run |
| rust-traits-03 | traits | 2 | 2 | 2 | 1.00 | |
| rust-error-01 | error-handling | 2 | 2 | 2 | 1.00 | |
| rust-error-02 | error-handling | 3 | 2 | 2 | 1.00 | `Err(Into::into(e))` via `From<E>` |
| rust-error-03 | error-handling | 2 | 2 | 2 | 1.00 | |
| rust-error-04 | error-handling, traits | 2 | 3 | 3 | 1.00 | |
| rust-iterators-01 | iterators | 3 | 2 | 2 | 1.00 | |
| rust-iterators-02 | iterators, ownership | 2 | 2 | 2 | 1.00 | |
| rust-iterators-03 | iterators | 2 | 2 | 2 | 1.00 | |
| rust-iterators-04 | iterators | 2 | 2 | 2 | 1.00 | |
| rust-smartptr-01 | smart-pointers | 2 | 2 | 2 | 1.00 | "uniquely owns a heap-allocated `T`" = single-owner heap allocation; fixed-size handle not spelled out (minor, same as baseline) |
| rust-smartptr-02 | smart-pointers, concurrency | 2 | 3 | 3 | 1.00 | |
| rust-smartptr-03 | smart-pointers, borrowing | 3 | 3 | 3 | 1.00 | |
| rust-smartptr-04 | smart-pointers, concurrency | 3 | 3 | 3 | 1.00 | "standard single-threaded idiom" carries point 2; the non-atomic/`!Sync` reason is not restated here (minor) |
| rust-concurrency-01 | concurrency | 3 | 3 | 3 | 1.00 | bounds on `thread::spawn` → compile errors, not data races; "auto-trait" not named (minor) |
| rust-async-01 | async | 3 | 3 | 3 | 1.00 | "`std` provides no executor" — the baseline miss, now present |
| rust-async-02 | async, concurrency | 2 | 2 | 2 | 1.00 | "values alive across `.await` become fields of the future's state machine, so such a value makes the whole future `!Send`" — the baseline half-miss, now present; `tokio::spawn` requires `Send` |
| rust-async-03 | async | 2 | 2 | 2 | 1.00 | "runtimes are cooperative: a task yields only at an `.await` that returns `Pending`" — the baseline miss, now present; `spawn_blocking` fix |
| rust-match-01 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| rust-match-02 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| rust-match-03 | pattern-matching, borrowing | 2 | 2 | 2 | 1.00 | |
| rust-match-04 | pattern-matching | 1 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | delta | n | trust | target? |
|-----|---------:|-----------:|------:|--:|-------|---------|
| ownership | 0.76 | 1.00 | +0.24 | 4 | ok | **yes** |
| borrowing | 1.00 | 1.00 | 0.00 | 5 | ok | no |
| lifetimes | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| traits | 1.00 | 0.93 | −0.07 | 5 | ok | no |
| error-handling | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| iterators | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| smart-pointers | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| concurrency | 0.90 | 1.00 | +0.10 | 4 | ok | no |
| async | 0.57 | 1.00 | +0.43 | 3 | low-n | **yes** |
| pattern-matching | 1.00 | 1.00 | 0.00 | 4 | ok | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: ownership (3 + 2 + 2 + 2) / 9 = 9/9; borrowing 11/11; lifetimes 7/7;
traits (1 + 3 + 2×0.667 + 2 + 2) / 10 = 9.33/10 = 0.933; error-handling 9/9;
iterators 9/9; smart-pointers 10/10; concurrency 10/10; async 7/7;
pattern-matching 7/7.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 68.33 / 69 = 99.0%
```

(Loss: traits-02 −0.67.) Baseline 63.83 / 69 = 92.5%.

## Target-tag verdict

| tag | baseline | with-skill | pass (≥ 0.85)? |
|-----|---------:|-----------:|----------------|
| ownership | 0.76 | 1.00 | **PASS** |
| async | 0.57 (low-n) | 1.00 (low-n) | **PASS** |

**Verdict: PASS.** Both target tags reach the threshold. Every baseline miss the
skill cites is now stated explicitly: single owner / no double-free
(ownership-01), `Copy` requires `Clone` (ownership-02), std ships no executor
(async-01), held-across-`.await` values captured in the state machine making the
future `!Send` (async-02), and cooperative scheduling yielding only at `.await`
(async-03). The skill is validated for cat-jam-free @ alpha. async stays `low-n`
(3 questions), so its subscore carries the trust caveat the rubric guide assigns
to n < 4.

Non-target drift (not a reason to edit the skill): traits 1.00 → 0.93, because
this run's traits-02 dropped the "all return paths must be the same concrete type"
clause the baseline had; concurrency 0.90 → 1.00, because async-02 (which also
carries the concurrency tag) absorbed the state-machine mechanism. Single-sample
movement on non-target tags is noted only.

## Provenance

Closed-book (Rule 0). The answerer was `ocode run -m opencode-go/cat-jam-free -effort med`
with the derived skill body (front matter stripped) prepended to `_prompts/rust.md`
as active guidance, in an isolated HOME with every tool denied and a cwd holding
only the sheet; the session audit shows 1 user + 1 assistant message and 0 tool
calls, so no repo file, rubric or web source was consulted. All 31 ids are present
and none is truncated. The ownership and async answers now contain the
reference-answer concepts the baseline flagged as absent, often in the skill's own
phrasing (e.g. "exactly one owner responsible for freeing", "yields only at an
`.await` that returns `Pending`"); per HOW-TO-EVALUATE.md ("Overlap ≠ cheating")
that is the intended absorption signal from the skill, not contamination. Answers
on non-target tags remain the model's own terse wording.
