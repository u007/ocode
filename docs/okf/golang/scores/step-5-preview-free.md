---
model_id: step-5-preview-free
model_version: "5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-12
stack: golang
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — step-5-preview-free on golang

> Valid ONLY for `step-5-preview-free` @ `5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book: the answerer was `ocode run -m opencode-go/step-5-preview-free -effort med`
in an empty scratch dir with only the `_prompts/golang.md` sheet attached; the
session sqlite shows exactly 1 user + 1 assistant message and 0 tool calls.
Graded from `../answers/step-5-preview-free.md` (33/33 ids present after the two
fixes below, no duplicates, no truncation). Two anomalies in the raw output,
both already handled in the answers file:

1. The model emitted the `go-context-01` answer under the id `go-concurrency-01`
   (a typo — the body answers context cancellation, and a separate, correct
   `go-concurrency-01` answer already existed). It was renamed to
   `go-context-01`; the rename is recorded by an `# --- id fix` marker line.
2. `go-errors-04` was skipped in the main run. It was re-asked alone,
   closed-book, in a fresh session with a one-question sheet and the same
   instruction, and appended after a `# --- re-asked alone` marker line.

Answers are in the model's own wording, typically 2–3× longer than the
reference, and carry factual slips the answer key does not contain (a
`GOEXPERIMENT=noloopvar` toggle, untyped constants said to defeat type
inference, an `errors.Is(err, nil)`-style "fix" for typed nils) — no sign of
answer-key contamination. The `go-slices-01` answer leaks a visible
trial-and-error reasoning chain ("Hmm — …", "No — real canonical case: …")
into the delivered text; see its note.

Grading rule applied throughout: a point is withheld when the answer states the
concept but also makes a materially wrong claim about the same construct;
tangential wrong asides are noted only. Two such calls, both marked
"(judgment)", account for every lost point.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| go-concurrency-01 | concurrency | 2 | 2 | 2 | 1.00 |  |
| go-concurrency-02 | concurrency, goroutine-leaks | 2 | 3 | 3 | 1.00 |  |
| go-concurrency-03 | concurrency, goroutine-leaks | 3 | 3 | 3 | 1.00 | aside: says receive on closed "immediately" returns zero value; omits that buffered values drain first |
| go-concurrency-04 | concurrency, sync | 3 | 3 | 3 | 1.00 | aside: claims `GOEXPERIMENT=noloopvar` disables the 1.22 semantics (no such toggle) |
| go-sync-01 | sync | 3 | 2 | 2 | 1.00 | aside: garbled sentence ("a manageable `c` must not be copied") |
| go-sync-02 | sync, testing | 2 | 2 | 2 | 1.00 |  |
| go-sync-03 | sync | 2 | 2 | 2 | 1.00 |  |
| go-sync-04 | sync, concurrency | 2 | 2 | 2 | 1.00 |  |
| go-errors-01 | errors | 3 | 2 | 2 | 1.00 |  |
| go-errors-02 | errors | 3 | 2 | 2 | 1.00 |  |
| go-errors-03 | errors | 2 | 2 | 2 | 1.00 |  |
| go-errors-04 | errors, interfaces | 3 | 3 | 3 | 1.00 | re-asked alone (see Provenance); aside: a confused `Is(error)`/`errors.Is(err, nil)` "fix", self-hedged |
| go-interfaces-01 | interfaces | 2 | 2 | 2 | 1.00 |  |
| go-interfaces-02 | interfaces | 2 | 2 | 2 | 1.00 |  |
| go-generics-01 | generics, interfaces | 2 | 2 | 2 | 1.00 |  |
| go-generics-02 | generics | 2 | 2 | 2 | 1.00 |  |
| go-generics-03 | generics | 2 | 2 | 2 | 1.00 |  |
| go-generics-04 | generics | 1 | 2 | 1 | 0.50 | (judgment) correct that a result-only / argument-absent type param needs explicit args, but also claims an untyped-constant argument defeats inference — it infers via the constant's default type (`F(1)` → `T=int`); second point withheld |
| go-context-01 | context, goroutine-leaks | 3 | 3 | 3 | 1.00 |  |
| go-context-02 | context | 3 | 2 | 2 | 1.00 |  |
| go-context-03 | context | 2 | 2 | 2 | 1.00 |  |
| go-context-04 | context | 1 | 2 | 2 | 1.00 |  |
| go-slices-01 | slices-maps | 3 | 3 | 2 | 0.67 | (judgment) opening mechanism and final conclusion are right, and the return-value and full-slice-expression points are clean; but the worked examples are a visible trial-and-error chain with standing wrong claims (append on a len==cap slice "writes index 2"; "if len(a) < len(a)") — aliasing point withheld |
| go-slices-02 | slices-maps | 3 | 2 | 2 | 1.00 |  |
| go-slices-03 | slices-maps | 2 | 2 | 2 | 1.00 |  |
| go-slices-04 | slices-maps | 2 | 2 | 2 | 1.00 |  |
| go-defer-01 | defer-panic | 3 | 2 | 2 | 1.00 |  |
| go-defer-02 | defer-panic | 2 | 2 | 2 | 1.00 | aside: one bullet trails off into a self-question before the (correct) parenthetical |
| go-defer-03 | defer-panic, goroutine-leaks | 2 | 2 | 2 | 1.00 |  |
| go-defer-04 | defer-panic, errors | 2 | 2 | 2 | 1.00 |  |
| go-testing-01 | testing | 2 | 2 | 2 | 1.00 |  |
| go-testing-02 | testing | 2 | 2 | 2 | 1.00 |  |
| go-testing-03 | testing | 1 | 2 | 2 | 1.00 | LIFO order of Cleanup not mentioned (parenthetical in the rubric) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| concurrency | 1.00 | 5 | ok | omit (strong) |
| context | 1.00 | 4 | ok | omit (strong) |
| defer-panic | 1.00 | 4 | ok | omit (strong) |
| errors | 1.00 | 5 | ok | omit (strong) |
| generics | 0.93 | 4 | ok | omit (strong) |
| goroutine-leaks | 1.00 | 4 | ok | omit (strong) |
| interfaces | 1.00 | 4 | ok | omit (strong) |
| slices-maps | 0.90 | 4 | ok | omit (strong) |
| sync | 1.00 | 5 | ok | omit (strong) |
| testing | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 72.5 / 74 = 98.0%
```

## Derivation targets

Tags below threshold (`< 0.85`): **none**. No
`derived/golang.step-5-preview-free.SKILL.md` is created for this evaluation.
