---
model_id: nvidia/nemotron-3.5-super-vl
model_version: "3.5"
evaluated_via: openrouter
evaluated_on: 2026-10-16
stack: golang
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — nvidia/nemotron-3.5-super-vl on golang

> Valid ONLY for `nvidia/nemotron-3.5-super-vl` @ `3.5`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book (Rule 0). The answerer was
`ocode run -m openrouter/nvidia/nemotron-3.5-super-vl:free -effort med` under an
isolated HOME whose config denies every tool (no tools were advertised), in a
scratch cwd containing only the `_prompts/golang.md` sheet, one fresh session;
the session sqlite shows exactly 1 user + 1 assistant message and 0 tool calls.
All 33 ids came back once, none empty. Graded from
`../answers/nvidia__nemotron-3.5-super-vl.md` (33/33 ids present, no duplicates,
no truncation). The OpenRouter `:free` route variant is not part of the model id.

Headless runs inject the bundled skill-catalogue digest into the system prompt;
no answer cites a tool, skill, or file the question never named. No thinking-
aloud, no fabricated tool output, no duplicated answers. Answers are in the
model's own wording and about the same length as the reference (one to three
sentences each) — terse enough that several rubric parentheticals go unstated.
One garbled clause in `go-sync-02` ("it only races that are exercised"). No
sign of answer-key contamination: the answers carry their own small
inaccuracies the key does not (see `go-concurrency-03`, `go-testing-02`).

Grading rule applied throughout: a point is withheld when the answer states the
concept but also makes a materially wrong claim about the same construct;
tangential wrong asides are noted only. Two calls are marked "(judgment)".

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| go-concurrency-01 | concurrency | 2 | 2 | 2 | 1.00 | |
| go-concurrency-02 | concurrency, goroutine-leaks | 2 | 3 | 3 | 1.00 | no-default blocking stated via "empty select{} blocks forever"; the word "leak" is not used |
| go-concurrency-03 | concurrency, goroutine-leaks | 3 | 3 | 3 | 1.00 | aside: says receive on closed returns the zero value "immediately"; omits that buffered values drain first |
| go-concurrency-04 | concurrency, sync | 3 | 3 | 2 | 0.67 | shared-variable cause and 1.22 per-iteration fix present; missed the pre-1.22 workaround (`v := v` shadow or pass as argument) |
| go-sync-01 | sync | 3 | 2 | 2 | 1.00 | |
| go-sync-02 | sync, testing | 2 | 2 | 2 | 1.00 | garbled clause ("it only races that are exercised"); meaning recoverable |
| go-sync-03 | sync | 2 | 2 | 2 | 1.00 | aside: "no contention" for atomics overstates it |
| go-sync-04 | sync, concurrency | 2 | 2 | 2 | 1.00 | |
| go-errors-01 | errors | 3 | 2 | 2 | 1.00 | |
| go-errors-02 | errors | 3 | 2 | 2 | 1.00 | |
| go-errors-03 | errors | 2 | 2 | 2 | 1.00 | |
| go-errors-04 | errors, interfaces | 3 | 3 | 3 | 1.00 | "nil only if both slots nil" stated through its contrapositive (type slot set → non-nil) |
| go-interfaces-01 | interfaces | 2 | 2 | 2 | 1.00 | |
| go-interfaces-02 | interfaces | 2 | 2 | 2 | 1.00 | |
| go-generics-01 | generics, interfaces | 2 | 2 | 2 | 1.00 | |
| go-generics-02 | generics | 2 | 2 | 2 | 1.00 | frames a constraint as a method set only; type-set (`~T` / union) constraints not mentioned |
| go-generics-03 | generics | 2 | 2 | 2 | 1.00 | |
| go-generics-04 | generics | 1 | 2 | 2 | 1.00 | |
| go-context-01 | context, goroutine-leaks | 3 | 3 | 2 | 0.67 | (judgment) says Done() is closed, cooperative observe-and-return, and "does not forcibly kill"; cancellation propagating to derived child contexts is absent — point 1 withheld, consistent with prior golang scorecards. "Ignoring it leaks" unstated (point 3 awarded on the no-force-kill half) |
| go-context-02 | context | 3 | 2 | 2 | 1.00 | |
| go-context-03 | context | 2 | 2 | 2 | 1.00 | custom unexported key type not mentioned (parenthetical) |
| go-context-04 | context | 1 | 2 | 2 | 1.00 | |
| go-slices-01 | slices-maps | 3 | 3 | 2 | 0.67 | shared-backing-array aliasing and must-use-return-value present; no detach idiom (`copy` / `slices.Clone` / full-slice expression) — point 3 withheld |
| go-slices-02 | slices-maps | 3 | 2 | 2 | 1.00 | "must make first" not said explicitly; the panic is |
| go-slices-03 | slices-maps | 2 | 2 | 2 | 1.00 | |
| go-slices-04 | slices-maps | 2 | 2 | 2 | 1.00 | sort-keys-for-stable-order not mentioned (parenthetical) |
| go-defer-01 | defer-panic | 3 | 2 | 2 | 1.00 | |
| go-defer-02 | defer-panic | 2 | 2 | 2 | 1.00 | |
| go-defer-03 | defer-panic, goroutine-leaks | 2 | 2 | 2 | 1.00 | |
| go-defer-04 | defer-panic, errors | 2 | 2 | 2 | 1.00 | |
| go-testing-01 | testing | 2 | 2 | 2 | 1.00 | |
| go-testing-02 | testing | 2 | 2 | 1 | 0.50 | (judgment) says a parallel subtest pauses "until parallel peers complete" — wrong (it pauses until the parent's serial body returns, then runs alongside its peers) and self-contradictory, so point 1 withheld. Shared-loop-variable capture and the 1.22 fix are present (point 2 awarded); the `tc := tc` shadow is not named |
| go-testing-03 | testing | 1 | 2 | 2 | 1.00 | LIFO order of Cleanup not mentioned (parenthetical in the rubric) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| concurrency | 0.92 | 5 | ok | omit (strong) |
| context | 0.89 | 4 | ok | omit (strong) |
| defer-panic | 1.00 | 4 | ok | omit (strong) |
| errors | 1.00 | 5 | ok | omit (strong) |
| generics | 1.00 | 4 | ok | omit (strong) |
| goroutine-leaks | 0.90 | 4 | ok | omit (strong) |
| interfaces | 1.00 | 4 | ok | omit (strong) |
| slices-maps | 0.90 | 4 | ok | omit (strong) |
| sync | 0.92 | 5 | ok | omit (strong) |
| testing | 0.86 | 4 | ok | omit (strong, at the edge: 6/7) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 70 / 74 = 94.6%
```

## Derivation targets

Tags below threshold (`< 0.85`): **none**. No
`derived/golang.nvidia__nemotron-3.5-super-vl.SKILL.md` is created for this
evaluation. Nearest to the line is `testing` at 0.86, which rests on one
judgment call (`go-testing-02`); reversing it would put the tag at 1.00, so
the no-derive outcome does not depend on it.
