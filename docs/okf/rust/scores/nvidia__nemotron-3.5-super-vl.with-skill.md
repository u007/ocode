---
model_id: nvidia/nemotron-3.5-super-vl
model_version: "3.5"
evaluated_via: openrouter
evaluated_on: 2026-10-16
stack: rust
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — nvidia/nemotron-3.5-super-vl on rust (with-skill validation)

> Valid ONLY for `nvidia/nemotron-3.5-super-vl` @ `3.5`. A version bump invalidates
> this scorecard — re-benchmark.

This is the **with-skill validation** of
`../derived/rust.nvidia__nemotron-3.5-super-vl.SKILL.md` (HOW-TO-EVALUATE.md
"Validate the derived skill"). Baseline: `nvidia__nemotron-3.5-super-vl.md`.
The only target tag is **async**. Non-target movement is resampling noise and is
not acted on.

## Provenance

Closed-book (Rule 0). Same setup as the baseline: `ocode run -m
openrouter/nvidia/nemotron-3.5-super-vl:free -effort med` under an isolated HOME
whose config denies every tool, scratch cwd holding only the `_prompts/rust.md`
sheet, one fresh session with 1 user + 1 assistant message and 0 tool calls. The
derived skill body (front matter stripped) was prepended to the sheet as active
guidance; the answer key was never shown. All 31 ids came back once, none empty or
truncated. No answer cites a tool or skill; no thinking-aloud or fabricated tool
output. Answers are roughly twice the baseline length across every tag, not only
the target one.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| rust-ownership-01 | ownership | 3 | 2 | 2 | 1.00 | |
| rust-ownership-02 | ownership | 2 | 3 | 3 | 1.00 | "`Copy` implies `Clone`" plus "the original remains usable" — compound point 2 now complete (baseline withheld it); non-target drift |
| rust-ownership-03 | ownership, borrowing | 2 | 2 | 2 | 1.00 | |
| rust-borrowing-01 | borrowing | 3 | 2 | 2 | 1.00 | |
| rust-lifetimes-01 | lifetimes | 2 | 2 | 2 | 1.00 | |
| rust-lifetimes-02 | lifetimes | 2 | 3 | 3 | 1.00 | |
| rust-lifetimes-03 | lifetimes, borrowing | 2 | 2 | 2 | 1.00 | |
| rust-lifetimes-04 | lifetimes, traits | 1 | 2 | 2 | 1.00 | minor aside: "default to `'static` when no borrowed references appear inside them" — the default comes from the position (`Box<dyn T>`), not the contents; conclusion and `+ 'a` rule right |
| rust-traits-01 | traits | 3 | 3 | 3 | 1.00 | |
| rust-traits-02 | traits | 2 | 3 | 2 | 0.67 | same miss as baseline: no "every return path must be one concrete type / returns closures-iterators without boxing" |
| rust-traits-03 | traits | 2 | 2 | 2 | 1.00 | |
| rust-error-01 | error-handling | 2 | 2 | 2 | 1.00 | |
| rust-error-02 | error-handling | 3 | 2 | 2 | 1.00 | |
| rust-error-03 | error-handling | 2 | 2 | 2 | 1.00 | minor aside: "aborts the thread" — default is unwind; outside rubric |
| rust-error-04 | error-handling, traits | 2 | 3 | 3 | 1.00 | |
| rust-iterators-01 | iterators | 3 | 2 | 2 | 1.00 | |
| rust-iterators-02 | iterators, ownership | 2 | 2 | 2 | 1.00 | |
| rust-iterators-03 | iterators | 2 | 2 | 2 | 1.00 | |
| rust-iterators-04 | iterators | 2 | 2 | 2 | 1.00 | |
| rust-smartptr-01 | smart-pointers | 2 | 2 | 2 | 1.00 | |
| rust-smartptr-02 | smart-pointers, concurrency | 2 | 3 | 3 | 1.00 | |
| rust-smartptr-03 | smart-pointers, borrowing | 3 | 3 | 3 | 1.00 | |
| rust-smartptr-04 | smart-pointers, concurrency | 3 | 3 | 2.5 | 0.83 | same as baseline: "in single-threaded code" without why (non-atomic / `!Sync`); half credit on point 2 |
| rust-concurrency-01 | concurrency | 3 | 3 | 3 | 1.00 | |
| rust-async-01 | async | 3 | 3 | 3 | 1.00 | **target.** "`std` provides the `Future`/`Poll`/`Context`/`Waker` machinery but ships no executor, so a third-party runtime is mandatory" — the baseline miss, now present |
| rust-async-02 | async, concurrency | 2 | 2 | 2 | 1.00 | **target.** state-machine capture → whole future `!Send` → `tokio::spawn` needs `Send`; the baseline's wrong "`Rc` is not `'static`" claim is gone, replaced by the correct "the bound that fails is `Send`, not `'static`" |
| rust-async-03 | async | 2 | 2 | 2 | 1.00 | **target.** leads with "cooperatively scheduled: a task yields only when an `.await` returns `Poll::Pending`", then starvation, then async equivalents / `spawn_blocking` (baseline gave only the effect) |
| rust-match-01 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| rust-match-02 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| rust-match-03 | pattern-matching, borrowing | 2 | 2 | 2 | 1.00 | |
| rust-match-04 | pattern-matching | 1 | 2 | 2 | 1.00 | guard syntax now correct (`Some(x) if x > 0 =>`); the baseline's misplaced guard is gone |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | n | trust | target? |
|-----|---------:|-----------:|--:|-------|---------|
| ownership | 0.93 | 1.00 | 4 | ok | no (drift, ignored) |
| borrowing | 1.00 | 1.00 | 5 | ok | no |
| lifetimes | 1.00 | 1.00 | 4 | ok | no |
| traits | 0.93 | 0.93 | 5 | ok | no |
| error-handling | 1.00 | 1.00 | 4 | ok | no |
| iterators | 1.00 | 1.00 | 4 | ok | no |
| smart-pointers | 0.95 | 0.95 | 4 | ok | no |
| concurrency | 0.90 | 0.95 | 4 | ok | no (drift, ignored) |
| **async** | **0.64** | **1.00** | 3 | low-n | **yes — reached threshold** |
| pattern-matching | 1.00 | 1.00 | 4 | ok | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

async = (3×1 + 2×1 + 2×1) / 7 = 7 / 7 = 1.00 (baseline 4.5 / 7 = 0.643).
concurrency = (2×1 + 3×0.833 + 3×1 + 2×1) / 10 = 9.5 / 10 = 0.95; the gain is
async-02's point 2, which the async section covers.
traits = (1×1 + 3×1 + 2×0.667 + 2×1 + 2×1) / 10 = 0.933.
smart-pointers = (2 + 2 + 3 + 2.5) / 10 = 0.95.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 67.83 / 69 = 98.3%
```

(baseline 64.67 / 69 = 93.7%)

## Validation verdict

**PASS.** The single target tag, async, moved 0.64 → 1.00 and is at the 0.85
threshold with every rubric point present across all three questions. No derived
skill was written or modified by this validation.

Absorption notes: all three digest directives were applied in the model's own
sentences rather than echoed — the "`std` ships no executor" and "yields only when
an `.await` returns `Poll::Pending`" phrasings are the skill's, as expected for an
in-context directive; the `Send`-not-`'static` correction landed exactly on the
construct the baseline got wrong. Non-target tags stayed within noise (ownership
+0.07, concurrency +0.05, others unchanged); traits-02 and smartptr-04 repeat the
baseline misses, consistent with the skill saying nothing about those tags.
