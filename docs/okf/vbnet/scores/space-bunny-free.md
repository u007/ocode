---
model_id: space-bunny-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-09-24
stack: vbnet
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — space-bunny-free on vbnet

> Valid ONLY for `space-bunny-free` @ `alpha`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| vbnet-syntax-01 | syntax-basics | 2 | 3 | 3 | 1.00 | |
| vbnet-syntax-02 | syntax-basics, conversions-arrays | 3 | 3 | 3 | 1.00 | |
| vbnet-syntax-03 | syntax-basics, oop | 2 | 2 | 2 | 1.00 | |
| vbnet-syntax-04 | syntax-basics | 2 | 2 | 2 | 1.00 | name-assignment return form described only as "assign its result to the function's return value" — vague but present |
| vbnet-props-01 | properties | 2 | 2 | 2 | 1.00 | |
| vbnet-props-02 | properties | 2 | 2 | 1 | 0.50 | names the backing field `<Name>k__BackingField` (C# compiler convention); VB generates `_Name`, accessible in-class — missed |
| vbnet-props-03 | properties | 2 | 2 | 2 | 1.00 | |
| vbnet-props-04 | properties | 1 | 2 | 2 | 1.00 | |
| vbnet-null-01 | nullability | 3 | 3 | 2 | 0.67 | frames Nothing as "absence of a value / null" with value-type default as a side case; never states the unifying "Nothing = default value of the type" |
| vbnet-null-02 | nullability | 2 | 2 | 2 | 1.00 | |
| vbnet-null-03 | nullability | 3 | 3 | 2 | 0.67 | misdescribes `If(a, b)` as returning `a` "if it is True and not Nothing or DBNull" — it is pure null-coalescing (returns `a` unless Nothing); 3-arg short-circuit ternary never spelled out. IIf both-branches + side-effect/type points correct |
| vbnet-null-04 | nullability | 2 | 2 | 2 | 1.00 | |
| vbnet-errors-01 | error-handling | 2 | 2 | 2 | 1.00 | |
| vbnet-errors-02 | error-handling | 2 | 2 | 2 | 1.00 | concepts right; code sample uses `End Catch`, which is not valid VB (Catch blocks end at the next Catch/Finally/End Try) |
| vbnet-errors-03 | error-handling | 3 | 2 | 2 | 1.00 | same invalid `End Catch` in sample |
| vbnet-errors-04 | error-handling | 2 | 2 | 2 | 1.00 | |
| vbnet-linq-01 | linq-query | 2 | 2 | 2 | 1.00 | |
| vbnet-linq-02 | linq-query | 2 | 2 | 1 | 0.50 | `Group ... By key Into Alias = Group` correct, but aggregates are done via method calls in `Select` (`CustomerOrders.Count()`, `.Sum(Function ...)`); never shows VB's `Into Group, Count(), Total = Sum(o.Amount)` aggregate-in-Into form |
| vbnet-linq-03 | linq-query | 1 | 2 | 1 | 0.50 | answers the `Enumerable.Aggregate` method (`amounts.Aggregate(0, Function(sum, amount) ...)`) instead of the VB query keyword `Aggregate n In nums Into Sum(n)`; scalar-vs-sequence contrast present |
| vbnet-linq-04 | linq-query | 2 | 2 | 2 | 1.00 | |
| vbnet-events-01 | events | 3 | 2 | 2 | 1.00 | states "no explicit AddHandler is required"; does not mention one Handles clause listing multiple events |
| vbnet-events-02 | events | 2 | 2 | 1 | 0.50 | AddHandler/RemoveHandler/AddressOf correct; "when required" limited to dynamic/non-WithEvents cases — never names `Shared` events or `Structure`s, where Handles is structurally unusable |
| vbnet-events-03 | events | 2 | 2 | 2 | 1.00 | |
| vbnet-events-04 | events | 1 | 2 | 2 | 1.00 | |
| vbnet-oop-01 | oop | 3 | 2 | 1 | 0.50 | one-base/many-interfaces correct; omits VB's per-member `Implements IFoo.Member` binding clause entirely |
| vbnet-oop-02 | oop | 3 | 3 | 3 | 1.00 | |
| vbnet-oop-03 | oop | 2 | 2 | 2 | 1.00 | |
| vbnet-oop-04 | oop | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-01 | conversions-arrays | 3 | 3 | 2 | 0.67 | claims DirectCast handles "numeric narrowing" — it does no conversion at all, only inheritance/exact-type reinterpretation; CType and TryCast points correct |
| vbnet-convarr-02 | conversions-arrays | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-03 | conversions-arrays | 2 | 2 | 2 | 1.00 | rubric points met, but claims `Dim a(1 To n)` is possible — VB.NET only accepts `0 To n` (VB6-ism); plain `ReDim` discarding contents only implied |
| vbnet-convarr-04 | conversions-arrays, syntax-basics | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| syntax-basics | 1.00 | 5 | ok | omit (strong) |
| properties | 0.86 | 4 | ok | omit (strong) |
| nullability | 0.80 | 4 | ok | omit (strong) |
| error-handling | 1.00 | 4 | ok | omit (strong) |
| linq-query | 0.79 | 4 | ok | omit (above threshold, but weakest — VB-specific `Into` aggregate forms) |
| events | 0.88 | 4 | ok | omit (strong) |
| oop | 0.88 | 5 | ok | omit (strong) |
| conversions-arrays | 0.92 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.
Multi-tag questions count toward every tag they carry (syntax-03 → oop,
syntax-02 → conversions-arrays, convarr-04 → syntax-basics).

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 61 / 69 = 88%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none** → no
`derived/vbnet.space-bunny-free.SKILL.md` written.

Observed pattern worth watching if the corpus grows: several misses are C#
knowledge translated into VB keywords (`k__BackingField`, `End Catch`,
method-syntax `Aggregate`, `Dim a(1 To n)`) rather than genuine VB idiom —
exactly the failure `meta.yaml` says to grade against. None of the tags
crossed the threshold on that alone, so no corrective section is warranted
yet.
