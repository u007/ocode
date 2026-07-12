---
model_id: tencent/hy3
model_version: "3.0"
evaluated_via: novita
evaluated_on: 2026-07-11
stack: golang
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — tencent/hy3 on golang

> Valid ONLY for `tencent/hy3` @ `3.0`. A version bump invalidates this
> scorecard — re-benchmark.
>
> Graded independently (Claude Opus 4.8, 1M) and closed-book: answers were
> produced with no corpus access via `ocode run -dir <empty>`.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| go-concurrency-01 | concurrency | 2 | 2 | 2 | 1.00 | |
| go-concurrency-02 | concurrency, goroutine-leaks | 2 | 3 | 3 | 1.00 | |
| go-concurrency-03 | concurrency, goroutine-leaks | 3 | 3 | 3 | 1.00 | |
| go-concurrency-04 | concurrency, sync | 3 | 3 | 3 | 1.00 | notes the pre-1.22 arg/local-copy workaround |
| go-sync-01 | sync | 3 | 2 | 2 | 1.00 | |
| go-sync-02 | sync, testing | 2 | 2 | 2 | 1.00 | dynamic + overhead caveats both given |
| go-sync-03 | sync | 2 | 2 | 2 | 1.00 | |
| go-sync-04 | sync, concurrency | 2 | 2 | 2 | 1.00 | Add-before-launch + Add-in-goroutine race both covered |
| go-errors-01 | errors | 3 | 2 | 2 | 1.00 | |
| go-errors-02 | errors | 3 | 2 | 2 | 1.00 | Is=value, As=type extraction — clean |
| go-errors-03 | errors | 2 | 2 | 2 | 1.00 | recommends errors.Is; correctly notes == only works unwrapped |
| go-errors-04 | errors, interfaces | 3 | 3 | 3 | 1.00 | (type,value) pair + typed-nil + fix all present |
| go-interfaces-01 | interfaces | 2 | 2 | 2 | 1.00 | |
| go-interfaces-02 | interfaces | 2 | 2 | 2 | 1.00 | |
| go-generics-01 | generics, interfaces | 2 | 2 | 2 | 1.00 | |
| go-generics-02 | generics | 2 | 2 | 2 | 1.00 | |
| go-generics-03 | generics | 2 | 2 | 2 | 1.00 | comparable + ~underlying both correct |
| go-generics-04 | generics | 1 | 2 | 2 | 1.00 | |
| go-context-01 | context, goroutine-leaks | 3 | 3 | 3 | 1.00 | closes Done + cooperative + can't force-kill; child-propagation only implied (thin but full credit) |
| go-context-02 | context | 3 | 2 | 2 | 1.00 | |
| go-context-03 | context | 2 | 2 | 2 | 1.00 | custom unexported key type noted |
| go-context-04 | context | 1 | 2 | 2 | 1.00 | |
| go-slices-01 | slices-maps | 3 | 3 | 2 | 0.67 | missed: copy / slices.Clone (or full-slice expr) to obtain an independent slice |
| go-slices-02 | slices-maps | 3 | 2 | 2 | 1.00 | |
| go-slices-03 | slices-maps | 2 | 2 | 2 | 1.00 | |
| go-slices-04 | slices-maps | 2 | 2 | 2 | 1.00 | |
| go-defer-01 | defer-panic | 3 | 2 | 2 | 1.00 | |
| go-defer-02 | defer-panic | 2 | 2 | 2 | 1.00 | |
| go-defer-03 | defer-panic, goroutine-leaks | 2 | 2 | 2 | 1.00 | |
| go-defer-04 | defer-panic, errors | 2 | 2 | 2 | 1.00 | named-return requirement stated |
| go-testing-01 | testing | 2 | 2 | 2 | 1.00 | |
| go-testing-02 | testing | 2 | 2 | 2 | 1.00 | |
| go-testing-03 | testing | 1 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| concurrency | 1.00 | 5 | ok | omit (strong) |
| sync | 1.00 | 5 | ok | omit (strong) |
| errors | 1.00 | 5 | ok | omit (strong) |
| interfaces | 1.00 | 4 | ok | omit (strong) |
| generics | 1.00 | 4 | ok | omit (strong) |
| context | 1.00 | 4 | ok | omit (strong) |
| slices-maps | 0.90 | 4 | ok | omit (≥ 0.75) |
| defer-panic | 1.00 | 4 | ok | omit (strong) |
| goroutine-leaks | 1.00 | 4 | ok | omit (strong) |
| testing | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.
No tag has n < 4, so there are no `(low-n)` flags.

slices-maps: (0.67×3 + 1×3 + 1×2 + 1×2) / (3+3+2+2) = 9.0 / 10 = 0.90.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 73.0 / 74 = 98.6%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none** — every tag scores ≥ 0.75
(lowest is slices-maps at 0.90). No `derived/golang.tencent__hy3.SKILL.md`
is generated (no derivation).

## Demo derivation attempt — 2026-07-12 (reverted)

A narrow demonstration skill (`golang-tuning-tencent-hy3`) was hand-derived from
the single partial miss `go-slices-01` (0.67 — omitted the `slices.Clone`/`copy`
independent-copy point) to show the stack-gated Kaizen flow live. It was
**reverted** as a null result: re-run closed-book, hy3 scored the SAME question
**1.00 both with and without** the skill — it produced the independent-copy fix
(`make`+`copy`, `append([]int(nil), …)`) unaided. `go-slices-01`'s 0.67 was a
single-sample dip, not a reliable failure, so there was no gap for a skill to
close. Conclusion stands: hy3 needs no golang derivation. Stack-gating mechanics
remain unit-proven (`internal/skill.TestKaizenStackGating`); there is simply no
golang question hy3 fails to demonstrate a live lift on.
