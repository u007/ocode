---
model_id: mimo-v2.5
model_version: "2.5"
evaluated_via: opencode-go
evaluated_on: 2026-08-17
stack: vbnet
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — mimo-v2.5 on vbnet

> Valid ONLY for `mimo-v2.5` @ `2.5`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| vbnet-syntax-01 | syntax-basics | 2 | 3 | 3 | 1.00 | |
| vbnet-syntax-02 | syntax-basics, conversions-arrays | 3 | 3 | 3 | 1.00 | |
| vbnet-syntax-03 | syntax-basics, oop | 2 | 2 | 2 | 1.00 | |
| vbnet-syntax-04 | syntax-basics | 2 | 2 | 2 | 1.00 | |
| vbnet-props-01 | properties | 2 | 2 | 2 | 1.00 | |
| vbnet-props-02 | properties | 2 | 2 | 1 | 0.50 | claims backing field has "no accessible name" (C#-style); VB names it `_PropertyName` and it's accessible in-class — missed |
| vbnet-props-03 | properties | 1 | 2 | 2 | 1.00 | |
| vbnet-props-04 | properties | 1 | 2 | 2 | 1.00 | |
| vbnet-null-01 | nullability | 3 | 3 | 2 | 0.67 | splits ref=null vs value=default without stating the unifying "Nothing = default value of the type" |
| vbnet-null-02 | nullability | 2 | 2 | 2 | 1.00 | |
| vbnet-null-03 | nullability | 3 | 3 | 3 | 1.00 | |
| vbnet-null-04 | nullability | 2 | 2 | 2 | 1.00 | |
| vbnet-errors-01 | error-handling | 2 | 2 | 2 | 1.00 | |
| vbnet-errors-02 | error-handling | 2 | 2 | 2 | 1.00 | |
| vbnet-errors-03 | error-handling | 3 | 2 | 2 | 1.00 | |
| vbnet-errors-04 | error-handling | 2 | 2 | 2 | 1.00 | |
| vbnet-linq-01 | linq-query | 2 | 2 | 2 | 1.00 | |
| vbnet-linq-02 | linq-query | 2 | 2 | 2 | 1.00 | |
| vbnet-linq-03 | linq-query | 1 | 2 | 2 | 1.00 | |
| vbnet-linq-04 | linq-query | 2 | 2 | 2 | 1.00 | |
| vbnet-events-01 | events | 3 | 2 | 1 | 0.50 | explains WithEvents+Handles auto-wiring but never states "no explicit AddHandler needed" nor that one `Handles` clause can list multiple events |
| vbnet-events-02 | events | 2 | 2 | 1 | 0.50 | covers AddHandler/RemoveHandler/AddressOf but misses the two hard-requirement cases (`Shared` events, `Structure`s) where `Handles` literally cannot be used |
| vbnet-events-03 | events | 2 | 2 | 2 | 1.00 | |
| vbnet-events-04 | events | 1 | 2 | 2 | 1.00 | |
| vbnet-oop-01 | oop | 3 | 2 | 1 | 0.50 | gets Inherits(one)/Implements(many) right but omits VB's per-member `Implements IFoo.Member` binding clause entirely |
| vbnet-oop-02 | oop | 3 | 3 | 3 | 1.00 | |
| vbnet-oop-03 | oop | 2 | 2 | 2 | 1.00 | |
| vbnet-oop-04 | oop | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-01 | conversions-arrays | 3 | 3 | 3 | 1.00 | |
| vbnet-convarr-02 | conversions-arrays | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-03 | conversions-arrays | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-04 | conversions-arrays, syntax-basics | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| syntax-basics | 1.00 | 5 | ok | omit (strong) |
| properties | 0.86 | 4 | ok | omit (strong) |
| nullability | 0.90 | 4 | ok | omit (strong) |
| error-handling | 1.00 | 4 | ok | omit (strong) |
| linq-query | 1.00 | 4 | ok | omit (strong) |
| events | 0.69 | 4 | ok | **derive** |
| oop | 0.85 | 4 | ok | omit (strong) |
| conversions-arrays | 1.00 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 63 / 69 = 91%
```

## Derivation targets

Tags below threshold (`< 0.75`): **events** → feed into
`derived/vbnet.mimo-v2.5.SKILL.md`.
