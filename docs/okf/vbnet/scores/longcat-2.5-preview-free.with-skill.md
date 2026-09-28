---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: vbnet
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — longcat-2.5-preview-free on vbnet (with-skill validation)

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump
> invalidates this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** These answers were produced closed-book via
> `ocode run` from an isolated directory, with the derived tuning skill
> `derived/vbnet.longcat-2.5-preview-free.SKILL.md`
> (`vbnet-tuning-longcat-2.5-preview-free`) prepended to the question sheet as
> active guidance. The answerer never saw `questions.yaml` or the rubric.
> Session audit: 1 user + 1 assistant turn, zero tool calls. Grading is held
> to the same strict standard as the baseline `scores/longcat-2.5-preview-free.md`.
> No derived skill is written or modified from this run.
>
> Target tags for this skill: **events** (the only baseline tag below 0.75).

Contamination check: answers are in the model's own wording; distinctive
reference phrases ("rewires", "no-op", "zero date", "diagnostic trail",
"project-wide") are absent. Non-target answers still miss the same points as
the baseline (linq-02 aggregates in `Select` not `Into`, linq-03 `Aggregate` as
a lambda fold, oop-01 no per-member `Implements`), which fits a blind run. The
events answers repeat the skill's own wording ("Shared events, which cannot be
handled through WithEvents/Handles", "a Structure cannot declare a WithEvents
field"). That is the skill being absorbed, not access to the answer key.
Verdict: **clean**.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| vbnet-syntax-01 | syntax-basics | 2 | 3 | 3 | 1.00 | implicit continuation (VB 10+) still not mentioned |
| vbnet-syntax-02 | syntax-basics, conversions-arrays | 3 | 3 | 3 | 1.00 | |
| vbnet-syntax-03 | syntax-basics, oop | 2 | 2 | 2 | 1.00 | |
| vbnet-syntax-04 | syntax-basics | 2 | 2 | 2 | 1.00 | `Return` form only; name-assignment form not mentioned (the point accepts either) |
| vbnet-props-01 | properties | 2 | 2 | 2 | 1.00 | |
| vbnet-props-02 | properties | 2 | 2 | 1 | 0.50 | default via `= value` correct; says the backing field is "not directly accessible by a predictable name", which is wrong (`_Name` is the fixed convention). Baseline had `_PropertyName` |
| vbnet-props-03 | properties | 2 | 2 | 2 | 1.00 | ReadOnly auto-property point present; also wrongly says an auto-property can be `WriteOnly` (not allowed) |
| vbnet-props-04 | properties | 1 | 2 | 2 | 1.00 | |
| vbnet-null-01 | nullability | 3 | 3 | 3 | 1.00 | now states "Nothing represents the default value of a type" (baseline missed this) |
| vbnet-null-02 | nullability | 2 | 2 | 2 | 1.00 | |
| vbnet-null-03 | nullability | 3 | 3 | 3 | 1.00 | 3-arg ternary only implied via "only evaluates the needed branch" (same as baseline) |
| vbnet-null-04 | nullability | 2 | 2 | 2 | 1.00 | `= Nothing` framed as overloaded equality; String `"" = Nothing` case missed (same as baseline) |
| vbnet-errors-01 | error-handling | 2 | 2 | 2 | 1.00 | |
| vbnet-errors-02 | error-handling | 2 | 2 | 1 | 0.50 | filter + fall-through correct; benefit given as "cleaner" / "not caught and then ignored". No filter-before-unwind and no avoiding catch-and-rethrow (baseline had the stack-preservation angle) |
| vbnet-errors-03 | error-handling | 3 | 2 | 2 | 1.00 | |
| vbnet-errors-04 | error-handling | 2 | 2 | 2 | 1.00 | doesn't note the two models can't mix in one method |
| vbnet-linq-01 | linq-query | 2 | 2 | 2 | 1.00 | |
| vbnet-linq-02 | linq-query | 2 | 2 | 1 | 0.50 | `Group By ... Into Group` correct; aggregate still done as `Group.Count()` in `Select`, not in the `Into` clause (same as baseline) |
| vbnet-linq-03 | linq-query | 1 | 2 | 0.5 | 0.25 | `Aggregate` still described as a lambda seed/accumulator fold (`Aggregate Function(acc, item) ...`), not `Aggregate x In xs Into Sum(x)`; no scalar-vs-sequence contrast → partial (same as baseline) |
| vbnet-linq-04 | linq-query | 2 | 2 | 2 | 1.00 | |
| vbnet-events-01 | events | 3 | 2 | 2 | 1.00 | auto-wiring + "no explicit AddHandler call is needed" + comma-separated multi-event `Handles`, all present. The wrong "WithEvents may be a local" claim is gone (baseline 0.50) |
| vbnet-events-02 | events | 2 | 2 | 2 | 1.00 | AddHandler/RemoveHandler/AddressOf + when-required: `Shared` events and `Structure` events, plus dynamic wiring (baseline 0.50) |
| vbnet-events-03 | events | 2 | 2 | 2 | 1.00 | |
| vbnet-events-04 | events | 1 | 2 | 2 | 1.00 | static vs dynamic, now also lists Shared/Structure cases |
| vbnet-oop-01 | oop | 3 | 2 | 1 | 0.50 | one-base/many-interfaces correct; per-member `Implements IFoo.Member` clause still missing (same as baseline) |
| vbnet-oop-02 | oop | 3 | 3 | 3 | 1.00 | the wrong baseline claim (NotOverridable is "default for Overrides members") is gone; NotOverridable = sealed is correct |
| vbnet-oop-03 | oop | 2 | 2 | 2 | 1.00 | no-instance-state detail not stated; type-name access present |
| vbnet-oop-04 | oop | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-01 | conversions-arrays | 3 | 3 | 3 | 1.00 | |
| vbnet-convarr-02 | conversions-arrays | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-03 | conversions-arrays | 2 | 2 | 2 | 1.00 | plain `ReDim` discarding contents not stated |
| vbnet-convarr-04 | conversions-arrays, syntax-basics | 2 | 2 | 2 | 1.00 | calls `CInt`/`CStr` "legacy" functions (they are current intrinsic operators); conversion role present |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | n | trust | target? | action |
|-----|---------:|-----------:|--:|-------|---------|--------|
| syntax-basics | 1.00 | 1.00 | 5 | ok | no | omit (strong) |
| properties | 1.00 | 0.86 | 4 | ok | no | omit (drift, props-02) |
| nullability | 0.90 | 1.00 | 4 | ok | no | omit (strong) |
| error-handling | 1.00 | 0.89 | 4 | ok | no | omit (drift, errors-02) |
| linq-query | 0.75 | 0.75 | 4 | ok | no | omit (exactly at threshold, unchanged) |
| events | 0.69 | 1.00 | 4 | ok | **yes** | **PASS** |
| oop | 0.79 | 0.88 | 5 | ok | no | omit (strong) |
| conversions-arrays | 1.00 | 1.00 | 5 | ok | no | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.
Multi-tag questions count toward every tag they carry (syntax-03 → oop,
syntax-02 → conversions-arrays, convarr-04 → syntax-basics), same as the
baseline.

Per-tag arithmetic: syntax-basics 11/11; properties 6/7; nullability 10/10;
error-handling 8/9; linq-query 5.25/7; events 8/8; oop 10.5/12;
conversions-arrays 12/12.

## Target tags

| tag | baseline | with-skill | verdict |
|-----|---------:|-----------:|---------|
| events | 0.69 | 1.00 | **PASS** (≥ 0.75, saturated) |

Validation verdict: **SUCCESS**. The one target tag went from 0.69 to 1.00, and
every events rubric point the baseline missed is now present:

- events-01: says outright that no explicit `AddHandler` is needed, gives
  multiple events in one `Handles` clause, and drops the wrong
  "WithEvents on a local" claim.
- events-02: names `Shared` events and `Structure` events as the cases where
  `Handles` cannot be used, not only "dynamic wiring".

There are no FAILs.

Non-target drift, from a single sample and not acted on:
- properties −0.14: props-02 now denies the `_Name` backing-field convention.
- error-handling −0.11: errors-02 lost the before-unwind / no-rethrow benefit.
- nullability +0.10: null-01 gained "Nothing = default value of the type".
- oop +0.09: oop-02 dropped the wrong NotOverridable-default claim.

linq-query is still exactly 0.75, with the same two misses (`Into`-clause
aggregates and `Aggregate ... Into`). It is not a target and not below
threshold. A future baseline should watch it.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 63.75 / 69 = 92.4%
```

(Baseline: 61.25 / 69 = 88.8%, reported as 89%.)

## Derivation targets

None. This is a validation run, and no derived skill is written from it. All
tags are ≥ 0.75.
