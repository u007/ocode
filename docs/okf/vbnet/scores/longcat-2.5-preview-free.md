---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: vbnet
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — longcat-2.5-preview-free on vbnet

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump
> invalidates this scorecard — re-benchmark.

Contamination check: answers are in the model's own wording; no near-verbatim
overlap with `questions.yaml` reference answers (distinctive reference phrases
such as "rewires", "no-op", "zero date", "diagnostic trail", "project-wide"
absent). Session audit: 1 user + 1 assistant turn, zero tool calls. Verdict:
**clean**.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| vbnet-syntax-01 | syntax-basics | 2 | 3 | 3 | 1.00 | implicit continuation (VB 10+) not mentioned |
| vbnet-syntax-02 | syntax-basics, conversions-arrays | 3 | 3 | 3 | 1.00 | |
| vbnet-syntax-03 | syntax-basics, oop | 2 | 2 | 2 | 1.00 | |
| vbnet-syntax-04 | syntax-basics | 2 | 2 | 2 | 1.00 | |
| vbnet-props-01 | properties | 2 | 2 | 2 | 1.00 | |
| vbnet-props-02 | properties | 2 | 2 | 2 | 1.00 | names `_PropertyName` but hedges "or a name that is not directly accessible" — `_Name` is accessible in-class |
| vbnet-props-03 | properties | 2 | 2 | 2 | 1.00 | |
| vbnet-props-04 | properties | 1 | 2 | 2 | 1.00 | |
| vbnet-null-01 | nullability | 3 | 3 | 2 | 0.67 | reference→null and value-type→default correct; never states the unifying "Nothing = default value of any type" |
| vbnet-null-02 | nullability | 2 | 2 | 2 | 1.00 | |
| vbnet-null-03 | nullability | 3 | 3 | 3 | 1.00 | 2-arg If described as null-coalescing; 3-arg ternary only implied via "only one branch evaluates" |
| vbnet-null-04 | nullability | 2 | 2 | 2 | 1.00 | `= Nothing` framed as overloaded value comparison; misses String `"" = Nothing` / won't-compile cases |
| vbnet-errors-01 | error-handling | 2 | 2 | 2 | 1.00 | |
| vbnet-errors-02 | error-handling | 2 | 2 | 2 | 1.00 | |
| vbnet-errors-03 | error-handling | 3 | 2 | 2 | 1.00 | odd aside that `Throw ex` is for wrapping — core stack-trace reasoning correct |
| vbnet-errors-04 | error-handling | 2 | 2 | 2 | 1.00 | doesn't note the two models can't mix in one method |
| vbnet-linq-01 | linq-query | 2 | 2 | 2 | 1.00 | |
| vbnet-linq-02 | linq-query | 2 | 2 | 1 | 0.50 | `Group By ... Into Group` correct, but aggregate done as `Group.Count()` in `Select`; never shows `Into Group, Count(), Avg = Average(...)` |
| vbnet-linq-03 | linq-query | 1 | 2 | 0.5 | 0.25 | describes `Aggregate` as the `Enumerable.Aggregate` seed+accumulator fold, not `Aggregate n In nums Into Sum(n)`; no scalar-vs-sequence contrast → partial |
| vbnet-linq-04 | linq-query | 2 | 2 | 2 | 1.00 | |
| vbnet-events-01 | events | 3 | 2 | 1 | 0.50 | auto-wiring via Handles correct; omits "no explicit AddHandler" and multiple events per Handles clause; wrongly says WithEvents may be a local |
| vbnet-events-02 | events | 2 | 2 | 1 | 0.50 | AddHandler/RemoveHandler/AddressOf correct; when-required limited to dynamic/non-WithEvents — no `Shared` events or `Structure`s |
| vbnet-events-03 | events | 2 | 2 | 2 | 1.00 | |
| vbnet-events-04 | events | 1 | 2 | 2 | 1.00 | |
| vbnet-oop-01 | oop | 3 | 2 | 1 | 0.50 | one-base/many-interfaces correct; omits per-member `Implements IFoo.Member` clause |
| vbnet-oop-02 | oop | 3 | 3 | 2 | 0.67 | claims NotOverridable is "default for Overrides members" — wrong, an Overrides member stays overridable unless marked NotOverridable |
| vbnet-oop-03 | oop | 2 | 2 | 2 | 1.00 | |
| vbnet-oop-04 | oop | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-01 | conversions-arrays | 3 | 3 | 3 | 1.00 | |
| vbnet-convarr-02 | conversions-arrays | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-03 | conversions-arrays | 2 | 2 | 2 | 1.00 | plain `ReDim` discarding contents only implied |
| vbnet-convarr-04 | conversions-arrays, syntax-basics | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| syntax-basics | 1.00 | 5 | ok | omit (strong) |
| properties | 1.00 | 4 | ok | omit (strong) |
| nullability | 0.90 | 4 | ok | omit (strong) |
| error-handling | 1.00 | 4 | ok | omit (strong) |
| linq-query | 0.75 | 4 | ok | omit (exactly at threshold — VB `Into`/`Aggregate` query forms weak) |
| events | 0.69 | 4 | ok | **derive** |
| oop | 0.79 | 5 | ok | omit (above threshold) |
| conversions-arrays | 1.00 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.
Multi-tag questions count toward every tag they carry (syntax-03 → oop,
syntax-02 → conversions-arrays, convarr-04 → syntax-basics).

linq-query = 5.25 / 7 = 0.750; events = 5.5 / 8 = 0.6875; oop = 9.5 / 12 = 0.79.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 61.25 / 69 = 89%
```

## Derivation targets

Tags below threshold (`< 0.75`): **events** →
`derived/vbnet.longcat-2.5-preview-free.SKILL.md`.

linq-query sits exactly on 0.75 (not below) and is not derived; revisit if a
re-run drops it.
