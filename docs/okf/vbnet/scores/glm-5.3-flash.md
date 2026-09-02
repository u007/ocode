---
model_id: glm-5.3-flash
model_version: "5.3"
evaluated_via: aihubmix
evaluated_on: 2026-09-01
stack: vbnet
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — glm-5.3-flash on vbnet

> Valid ONLY for `glm-5.3-flash` @ `5.3`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| vbnet-syntax-01 | syntax-basics | 2 | 3 | 3 | 1.00 | also notes `:` statement separator |
| vbnet-syntax-02 | syntax-basics, conversions-arrays | 3 | 3 | 3 | 1.00 | |
| vbnet-syntax-03 | syntax-basics, oop | 2 | 2 | 2 | 1.00 | |
| vbnet-syntax-04 | syntax-basics | 2 | 2 | 2 | 1.00 | both `Return` and name-assignment forms |
| vbnet-props-01 | properties | 2 | 2 | 2 | 1.00 | |
| vbnet-props-02 | properties | 2 | 2 | 2 | 1.00 | names `_Name` and `= value` initializer; minor inaccuracies not on rubric: says `_Name` is "not meant to be referenced" (it is accessible in-class) and dates initializers to VB 2015 (actually VB 2010) |
| vbnet-props-03 | properties | 2 | 2 | 2 | 1.00 | |
| vbnet-props-04 | properties | 1 | 2 | 2 | 1.00 | |
| vbnet-null-01 | nullability | 3 | 3 | 3 | 1.00 | |
| vbnet-null-02 | nullability | 2 | 2 | 2 | 1.00 | adds `GetValueOrDefault`/`If(n, 0)` |
| vbnet-null-03 | nullability | 3 | 3 | 3 | 1.00 | |
| vbnet-null-04 | nullability | 2 | 2 | 2 | 1.00 | |
| vbnet-errors-01 | error-handling | 2 | 2 | 2 | 1.00 | |
| vbnet-errors-02 | error-handling | 2 | 2 | 2 | 1.00 | first-pass / before-unwind stated explicitly |
| vbnet-errors-03 | error-handling | 3 | 2 | 2 | 1.00 | |
| vbnet-errors-04 | error-handling | 2 | 2 | 2 | 1.00 | scoped/nestable/typed contrast present; does not mention the can't-mix-in-one-method rule |
| vbnet-linq-01 | linq-query | 2 | 2 | 2 | 1.00 | |
| vbnet-linq-02 | linq-query | 2 | 2 | 2 | 1.00 | valid `Group By key Into Group, alias = Sum(...), Count()` |
| vbnet-linq-03 | linq-query | 1 | 2 | 2 | 1.00 | |
| vbnet-linq-04 | linq-query | 2 | 2 | 2 | 1.00 | |
| vbnet-events-01 | events | 3 | 2 | 2 | 1.00 | compiler auto-wires; multi-event `Handles` shown |
| vbnet-events-02 | events | 2 | 2 | 2 | 1.00 | names dynamic wiring + shared events; omits the `Structure` case |
| vbnet-events-03 | events | 2 | 2 | 2 | 1.00 | uses `RaiseEvent`; bonus `Custom Event` block |
| vbnet-events-04 | events | 1 | 2 | 2 | 1.00 | |
| vbnet-oop-01 | oop | 3 | 2 | 2 | 1.00 | per-member `Implements IFoo.Bar` clause present |
| vbnet-oop-02 | oop | 3 | 3 | 3 | 1.00 | non-virtual-by-default expressed as "NotOverridable is the implicit default" |
| vbnet-oop-03 | oop | 2 | 2 | 2 | 1.00 | |
| vbnet-oop-04 | oop | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-01 | conversions-arrays | 3 | 3 | 3 | 1.00 | |
| vbnet-convarr-02 | conversions-arrays | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-03 | conversions-arrays | 2 | 2 | 2 | 1.00 | upper-bound = n+1 elements stated |
| vbnet-convarr-04 | conversions-arrays, syntax-basics | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

Contamination check: a full sweep is a red flag per HOW-TO-EVALUATE.md. The
answers use different worked examples from the key throughout (orders/customers
vs people/numbers in LINQ, `Changed`/`MyEventArgs` vs `Completed`, `btn.DoubleClick`),
carry extra content absent from the key (`:` separator, `GetValueOrDefault`,
`Custom Event`, `On Error GoTo 0`) and a couple of key-contradicting slips
(props-02), so this reads as genuine closed-book knowledge of a feature-stable
language, not a copy. Sibling models score 91–98% on this corpus.

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| syntax-basics | 1.00 | 5 | ok | omit (strong) |
| properties | 1.00 | 4 | ok | omit (strong) |
| nullability | 1.00 | 4 | ok | omit (strong) |
| error-handling | 1.00 | 4 | ok | omit (strong) |
| linq-query | 1.00 | 4 | ok | omit (strong) |
| events | 1.00 | 4 | ok | omit (strong) |
| oop | 1.00 | 5 | ok | omit (strong) |
| conversions-arrays | 1.00 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 69 / 69 = 100%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none** → no
`derived/vbnet.glm-5.3-flash.SKILL.md` written.
