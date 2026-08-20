---
model_id: muse-spark-1.2
model_version: "1.2"
evaluated_via: opencode-go
evaluated_on: 2026-08-20
stack: rust
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — muse-spark-1.2 on rust

> Valid ONLY for `muse-spark-1.2` @ `1.2`. A version bump invalidates this
> scorecard — re-benchmark.

## Contamination check

Stack score is 100% (31/31 questions full marks). Verified NOT contamination:

- Answerer ran as an isolated `ocode2` subprocess in an empty `/tmp/kaizen-rust-answer`
  directory with `-yolo`, given only the closed-book `_prompts/rust.md` sheet
  (id + question text, no answers/rubric). Captured log
  (`/tmp/kaizen-rust-answer/_raw_output.log`) shows a single `[LLM]` call
  (`in=22370 out=5996`) and **zero tool invocations** — no `read`, `bash`,
  `grep`, `webfetch`, or `websearch` calls appear despite 43 tools being
  exposed. The model never touched the repo or the network.
- Answer wording diverges from `questions.yaml`'s reference `answer` field on
  every question (different phrasing, different example code, different
  invented compiler-error text) — not a verbatim/near-verbatim copy of the
  rubric, which is the signature of the contamination case this runbook warns
  about.
- Unlike the `conduct` corpus (un-learnable house rules), this corpus tests
  standard, extensively documented public Rust language semantics (ownership,
  borrowing, lifetimes, traits, iterators, smart pointers, Send/Sync, async,
  pattern matching) that a strong model plausibly already knows cold from
  training data. A 100% score here is a materially different signal than a
  100% on house-specific/un-learnable material.

Conclusion: legitimate closed-book result, not contamination.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| rust-ownership-01 | ownership | 3 | 2 | 2 | 1.00 | |
| rust-ownership-02 | ownership | 2 | 3 | 3 | 1.00 | |
| rust-ownership-03 | ownership, borrowing | 2 | 2 | 2 | 1.00 | |
| rust-borrowing-01 | borrowing | 3 | 2 | 2 | 1.00 | |
| rust-lifetimes-01 | lifetimes | 2 | 2 | 2 | 1.00 | |
| rust-lifetimes-02 | lifetimes | 2 | 3 | 3 | 1.00 | all 3 elision rules named |
| rust-lifetimes-03 | lifetimes, borrowing | 2 | 2 | 2 | 1.00 | |
| rust-lifetimes-04 | lifetimes, traits | 1 | 2 | 2 | 1.00 | notes default object lifetime bound for `&dyn` |
| rust-traits-01 | traits | 3 | 3 | 3 | 1.00 | |
| rust-traits-02 | traits | 2 | 3 | 3 | 1.00 | |
| rust-traits-03 | traits | 2 | 2 | 2 | 1.00 | |
| rust-error-01 | error-handling | 2 | 2 | 2 | 1.00 | |
| rust-error-02 | error-handling | 3 | 2 | 2 | 1.00 | |
| rust-error-03 | error-handling | 2 | 2 | 2 | 1.00 | |
| rust-error-04 | error-handling, traits | 2 | 3 | 3 | 1.00 | |
| rust-iterators-01 | iterators | 3 | 2 | 2 | 1.00 | |
| rust-iterators-02 | iterators, ownership | 2 | 2 | 2 | 1.00 | |
| rust-iterators-03 | iterators | 2 | 2 | 2 | 1.00 | |
| rust-iterators-04 | iterators | 2 | 2 | 2 | 1.00 | |
| rust-smartptr-01 | smart-pointers | 2 | 2 | 2 | 1.00 | |
| rust-smartptr-02 | smart-pointers, concurrency | 2 | 3 | 3 | 1.00 | |
| rust-smartptr-03 | smart-pointers, borrowing | 3 | 3 | 3 | 1.00 | |
| rust-smartptr-04 | smart-pointers, concurrency | 3 | 3 | 3 | 1.00 | |
| rust-concurrency-01 | concurrency | 3 | 3 | 3 | 1.00 | |
| rust-async-01 | async | 3 | 3 | 3 | 1.00 | names concrete runtimes (tokio/futures) |
| rust-async-02 | async, concurrency | 2 | 2 | 2 | 1.00 | |
| rust-async-03 | async | 2 | 2 | 2 | 1.00 | |
| rust-match-01 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| rust-match-02 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| rust-match-03 | pattern-matching, borrowing | 2 | 2 | 2 | 1.00 | |
| rust-match-04 | pattern-matching | 1 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| ownership | 1.00 | 4 | ok | omit (strong) |
| borrowing | 1.00 | 5 | ok | omit (strong) |
| lifetimes | 1.00 | 4 | ok | omit (strong) |
| traits | 1.00 | 5 | ok | omit (strong) |
| error-handling | 1.00 | 4 | ok | omit (strong) |
| iterators | 1.00 | 4 | ok | omit (strong) |
| smart-pointers | 1.00 | 4 | ok | omit (strong) |
| concurrency | 1.00 | 4 | ok | omit (strong) |
| async | 1.00 | 3 | low-n | omit (strong, mark low-n) |
| pattern-matching | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 69/69 = 100%
```

## Derivation targets

No tag falls below threshold (`< 0.75`). **No derived skill file was
created** — per `HOW-TO-EVALUATE.md` step 5, a derived skill is only written
for below-threshold tags, and covering already-strong tags would waste
prompt/cache budget.
