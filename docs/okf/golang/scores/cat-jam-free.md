---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: golang
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — cat-jam-free on golang

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book: the answerer was `ocode run -m opencode-go/cat-jam-free -effort med`
in an empty scratch cwd with only the `_prompts/golang.md` sheet attached; the
session sqlite shows exactly 1 user + 1 assistant message and 0 tool calls.
Headless runs inject ocode's bundled skill-catalogue digest into the system
prompt. `opencode-go` publishes no version for this free/alpha model, so
`model_version` is recorded as `"alpha"`; the model self-identifies as "CatJam,
developed by LitterBox AI".

Graded from `../answers/cat-jam-free.md`: 33/33 ids present, no duplicates, no
truncation, no id fixes or re-asks needed. Answers are terse (1–3 sentences,
usually shorter than the reference), in the model's own wording, with no sign
of answer-key contamination and no factual errors found. Every lost point is an
**omission** of one rubric concept, not a wrong claim: the model answers the
headline question correctly and drops the follow-on detail (the pre-1.22
workaround, the copy/clone escape hatch, child-context propagation, the
(type,value) nil rule).

Grading rule applied throughout: a point is awarded when its concept is
genuinely present in the model's words; parenthetical examples in a rubric
point are not required. A point is withheld when a distinct required concept is
absent. Calls marked "(judgment)" are the borderline ones.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| go-concurrency-01 | concurrency | 2 | 2 | 2 | 1.00 |  |
| go-concurrency-02 | concurrency, goroutine-leaks | 2 | 3 | 3 | 1.00 | (judgment) "blocks until one case becomes ready" carries the blocking point; never says it can block forever / leak |
| go-concurrency-03 | concurrency, goroutine-leaks | 3 | 3 | 3 | 1.00 |  |
| go-concurrency-04 | concurrency, sync | 3 | 3 | 2 | 0.67 | shared-variable bug and 1.22 per-iteration fix correct; no pre-1.22 workaround (`v := v` / pass as arg) — point 3 withheld |
| go-sync-01 | sync | 3 | 2 | 2 | 1.00 | undefined-behaviour aspect not mentioned (parenthetical) |
| go-sync-02 | sync, testing | 2 | 2 | 2 | 1.00 |  |
| go-sync-03 | sync | 2 | 2 | 2 | 1.00 |  |
| go-sync-04 | sync, concurrency | 2 | 2 | 2 | 1.00 |  |
| go-errors-01 | errors | 3 | 2 | 2 | 1.00 |  |
| go-errors-02 | errors | 3 | 2 | 2 | 1.00 | pointer target for `As` not mentioned |
| go-errors-03 | errors | 2 | 2 | 2 | 1.00 |  |
| go-errors-04 | errors, interfaces | 3 | 3 | 2 | 0.67 | (judgment) typed-nil-has-a-dynamic-type and "return untyped nil" are present; never states the rule that an interface is nil only when BOTH type and value are nil — point 1 withheld |
| go-interfaces-01 | interfaces | 2 | 2 | 2 | 1.00 |  |
| go-interfaces-02 | interfaces | 2 | 2 | 2 | 1.00 |  |
| go-generics-01 | generics, interfaces | 2 | 2 | 2 | 1.00 | (judgment) "preserving type safety and avoiding interface boxing" read as the avoid-`any`+assert half of point 2; the same-in/same-out type relationship is not spelled out |
| go-generics-02 | generics | 2 | 2 | 2 | 1.00 |  |
| go-generics-03 | generics | 2 | 2 | 2 | 1.00 |  |
| go-generics-04 | generics | 1 | 2 | 2 | 1.00 |  |
| go-context-01 | context, goroutine-leaks | 3 | 3 | 2 | 0.67 | (judgment) cooperative-cancellation and no-force-kill points present; cancellation propagating to child contexts is absent and the answer never says Done() is *closed* — point 1 withheld. "Ignoring it leaks" also unstated (point 3 awarded on the no-force-kill half) |
| go-context-02 | context | 3 | 2 | 2 | 1.00 |  |
| go-context-03 | context | 2 | 2 | 2 | 1.00 | unexported custom key type not mentioned |
| go-context-04 | context | 1 | 2 | 2 | 1.00 |  |
| go-slices-01 | slices-maps | 3 | 3 | 2 | 0.67 | aliasing and must-use-return-value correct; no `copy` / `slices.Clone` / full-slice expression to get an independent slice — point 3 withheld |
| go-slices-02 | slices-maps | 3 | 2 | 2 | 1.00 |  |
| go-slices-03 | slices-maps | 2 | 2 | 2 | 1.00 | says `s[1:3]` is not independent but gives no clone/copy remedy |
| go-slices-04 | slices-maps | 2 | 2 | 2 | 1.00 | "sort keys for stable order" not mentioned |
| go-defer-01 | defer-panic | 3 | 2 | 2 | 1.00 |  |
| go-defer-02 | defer-panic | 2 | 2 | 2 | 1.00 |  |
| go-defer-03 | defer-panic, goroutine-leaks | 2 | 2 | 2 | 1.00 |  |
| go-defer-04 | defer-panic, errors | 2 | 2 | 2 | 1.00 |  |
| go-testing-01 | testing | 2 | 2 | 2 | 1.00 |  |
| go-testing-02 | testing | 2 | 2 | 2 | 1.00 | (judgment) shared-loop-variable capture pitfall present ("historically"); `tc := tc` fix and the 1.22 version are not named |
| go-testing-03 | testing | 1 | 2 | 2 | 1.00 | LIFO order of Cleanup not mentioned (parenthetical) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| concurrency | 0.92 | 5 | ok | omit (strong) |
| context | 0.89 | 4 | ok | omit (strong) |
| defer-panic | 1.00 | 4 | ok | omit (strong) |
| errors | 0.92 | 5 | ok | omit (strong) |
| generics | 1.00 | 4 | ok | omit (strong) |
| goroutine-leaks | 0.90 | 4 | ok | omit (strong) |
| interfaces | 0.89 | 4 | ok | omit (strong) |
| slices-maps | 0.90 | 4 | ok | omit (strong) |
| sync | 0.92 | 5 | ok | omit (strong) |
| testing | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 70 / 74 = 94.6%
```

## Derivation targets

Tags below threshold (`< 0.85`): **none**. No
`derived/golang.cat-jam-free.SKILL.md` is created for this evaluation.
