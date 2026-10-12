---
model_id: step-5-preview-free
model_version: "5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-12
stack: rust
stack_corpus_rev: 1
threshold: 0.85
validation: with-skill
---

# Scorecard — step-5-preview-free on rust (with-skill validation)

> Valid ONLY for `step-5-preview-free` @ `5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers: `rust/answers/step-5-preview-free.with-skill.md`,
> produced closed-book with the current derived skill
> `derived/rust.step-5-preview-free.SKILL.md` (`rust-tuning-step-5-preview-free`)
> prepended to the answerer prompt as active guidance (audited 1 user + 1 assistant
> message, zero tool calls). Baseline: `scores/step-5-preview-free.md`. No new
> derived skill is produced from this run.

> Target tags for this skill (baseline < 0.85): **async** (0.79, low-n).

Graded against `rust/questions.yaml` corpus_rev 1. Grading rule applied (same
strictness as the baseline): a `point` whose concept is genuinely present earns
full credit (minor missing sub-items are noted); a `point` accompanied by a
materially wrong claim about the same construct is withheld and marked
"(judgment)". A compound point only half present is withheld, as the baseline
grader did for ownership-02. Points are whole; fractions come only from a listed
partial.

## History

- Earlier with-skill scorecard: none (this is the first with-skill validation for
  step-5-preview-free on rust).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| rust-ownership-01 | ownership | 3 | 2 | 2 | 1.00 | move + `a` uninitialized; both would run `Drop` on the same buffer |
| rust-ownership-02 | ownership | 2 | 3 | 2 | 0.67 | "original stays usable" covers no-move, but never states `Copy` requires `Clone` — compound point 2 only half present, withheld (same as baseline). Points 1 and 3 awarded |
| rust-ownership-03 | ownership, borrowing | 2 | 2 | 2 | 1.00 | borrow (`&[T]`/`&mut`), clone, or take-and-return |
| rust-borrowing-01 | borrowing | 3 | 2 | 2 | 1.00 | |
| rust-lifetimes-01 | lifetimes | 2 | 2 | 2 | 1.00 | |
| rust-lifetimes-02 | lifetimes | 2 | 3 | 3 | 1.00 | |
| rust-lifetimes-03 | lifetimes, borrowing | 2 | 2 | 2 | 1.00 | |
| rust-lifetimes-04 | lifetimes, traits | 1 | 2 | 2 | 1.00 | also notes the `&'a dyn Trait` elision to `'a` |
| rust-traits-01 | traits | 3 | 3 | 3 | 1.00 | |
| rust-traits-02 | traits | 2 | 3 | 3 | 1.00 | point 3 via "return a closure or iterator type whose name is unnameable while keeping zero-cost calls" (avoids boxing); the all-branches-same-type half not stated (same as baseline) |
| rust-traits-03 | traits | 2 | 2 | 2 | 1.00 | |
| rust-error-01 | error-handling | 2 | 2 | 2 | 1.00 | |
| rust-error-02 | error-handling | 3 | 2 | 2 | 1.00 | minor aside: the sentence "propagates `?`'s `?Sized`-agnostic move" is nonsense filler; both rubric concepts correct |
| rust-error-03 | error-handling | 2 | 2 | 2 | 1.00 | |
| rust-error-04 | error-handling, traits | 2 | 3 | 3 | 1.00 | |
| rust-iterators-01 | iterators | 3 | 2 | 2 | 1.00 | |
| rust-iterators-02 | iterators, ownership | 2 | 2 | 2 | 1.00 | |
| rust-iterators-03 | iterators | 2 | 2 | 2 | 1.00 | |
| rust-iterators-04 | iterators | 2 | 2 | 2 | 1.00 | |
| rust-smartptr-01 | smart-pointers | 2 | 2 | 2 | 1.00 | "moving the box moves only a pointer" = fixed-size handle; recursive type + `dyn`/unsized |
| rust-smartptr-02 | smart-pointers, concurrency | 2 | 3 | 3 | 1.00 | minor aside: "`Arc` imposes `T: Send + Sync`" is loose — `Arc<T>` holds any `T`, the bound only governs whether the `Arc` itself is `Send`/`Sync` |
| rust-smartptr-03 | smart-pointers, borrowing | 3 | 3 | 3 | 1.00 | no poisoning slip this run |
| rust-smartptr-04 | smart-pointers, concurrency | 3 | 3 | 3 | 1.00 | single-threaded framing explicit; the non-atomic reason lives in smartptr-02 rather than here (minor) |
| rust-concurrency-01 | concurrency | 3 | 3 | 3 | 1.00 | (judgment) the baseline withheld Send for listing `Cell`/`RefCell` as `!Send`. This run groups "`Rc`, `Cell`, `RefCell`, raw pointers opt out as `!Send`/`!Sync`" and then explicitly ties `Cell`/`RefCell` to the missing `Sync` ("Mutexes/atomics are `Sync` precisely because they supply the synchronisation `Cell`/`RefCell` lack"). No wrong claim about Send → awarded |
| rust-async-01 | async | 3 | 3 | 3 | 1.00 | "`std` defines `Future`, `Poll`, `Context`, `Waker` but ships **no executor**" — the baseline miss, now present |
| rust-async-02 | async, concurrency | 2 | 2 | 2 | 1.00 | "every local still alive at an `.await` point is saved as a field of the generated future" — the baseline half-miss, now present; `!Send` + `tokio::spawn` needs `Send + 'static` |
| rust-async-03 | async | 2 | 2 | 2 | 1.00 | "cooperative"/"yields only at `.await`" not spelled out, but "does not yield to the runtime, so the executor thread cannot poll any other task" plus `yield_now` chunking carries the concept (minor) |
| rust-match-01 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| rust-match-02 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| rust-match-03 | pattern-matching, borrowing | 2 | 2 | 2 | 1.00 | cites RFC 2005 correctly |
| rust-match-04 | pattern-matching | 1 | 2 | 2 | 1.00 | guard/exhaustiveness muddle from the baseline absent this run |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | delta | n | trust | target? |
|-----|---------:|-----------:|------:|--:|-------|---------|
| ownership | 0.93 | 0.93 | 0.00 | 4 | ok | no |
| borrowing | 1.00 | 1.00 | 0.00 | 5 | ok | no |
| lifetimes | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| traits | 1.00 | 1.00 | 0.00 | 5 | ok | no |
| error-handling | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| iterators | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| smart-pointers | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| concurrency | 0.85 | 1.00 | +0.15 | 4 | ok | no |
| async | 0.79 | 1.00 | +0.21 | 3 | low-n | **yes** |
| pattern-matching | 1.00 | 1.00 | 0.00 | 4 | ok | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: ownership (3 + 2×0.667 + 2 + 2) / 9 = 8.33/9; borrowing 11/11;
lifetimes 7/7; traits 10/10; error-handling 9/9; iterators 9/9; smart-pointers 10/10;
concurrency 10/10; async 7/7; pattern-matching 7/7.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 68.33 / 69 = 99.0%
```

(Loss: ownership-02 −0.67.) Baseline 65.83 / 69 = 95.4%.

## Target-tag verdict

| tag | baseline | with-skill | pass (≥ 0.85)? |
|-----|---------:|-----------:|----------------|
| async | 0.79 (low-n) | 1.00 (low-n) | **PASS** |

The only target tag reaches the threshold. Both baseline misses the skill cites
(std ships no executor; values alive across `.await` live in the future's
generated state machine) are stated explicitly in the with-skill answers to
async-01 and async-02. The skill is validated for step-5-preview-free @ 5-preview.
The tag stays `low-n` (3 questions), so the subscore carries the trust caveat the
rubric guide assigns to n < 4.

Non-target drift (not a reason to edit the skill): concurrency 0.85 → 1.00, because
this run did not repeat the `Cell`/`RefCell`-are-`!Send` slip on concurrency-01
and async-02 (which also carries the concurrency tag) absorbed the state-machine
mechanism; ownership-02 repeats the baseline's missing "`Copy` requires `Clone`"
unchanged. Single-sample movement on non-target tags is noted only.

## Provenance

Closed-book (Rule 0). The answerer was `ocode run -m opencode-go/step-5-preview-free`
with the derived skill body prepended to `_prompts/rust.md` as active guidance and
no answer key; the session audit shows 1 user + 1 assistant message and 0 tool
calls, so no repo file, rubric or web source was consulted. All 31 ids are present
and none is truncated. The async answers now contain the two reference-answer
concepts the baseline flagged as absent; per HOW-TO-EVALUATE.md ("Overlap ≠
cheating") that is the intended absorption signal from the skill, not
contamination — wording elsewhere remains the model's own (e.g. "borrow of moved
value", the `?Sized` aside on error-02, `LocalSet`/`spawn_local` on async-02).
