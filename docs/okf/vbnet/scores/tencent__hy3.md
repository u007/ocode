---
model_id: tencent/hy3
model_version: "3.0"
evaluated_via: novita
evaluated_on: 2026-07-11
stack: vbnet
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — tencent/hy3 on vbnet

> Valid ONLY for `tencent/hy3` @ `3.0`. A version bump invalidates
> this scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| vbnet-syntax-01 | syntax-basics | 2 | 3 | 3 | 1.00 | Dim..As, no semicolons (notes colon separator), trailing `_` + implicit continuation |
| vbnet-syntax-02 | syntax-basics, conversions-arrays | 3 | 3 | 3 | 1.00 | Explicit/Infer/Strict all correct; narrowing + late-binding + compile-time safety |
| vbnet-syntax-03 | syntax-basics, oop | 2 | 2 | 2 | 1.00 | Module implicitly Shared/non-instantiable; Class instantiable with state |
| vbnet-syntax-04 | syntax-basics | 2 | 2 | 2 | 1.00 | Sub vs Function; return via name-assignment or Return |
| vbnet-props-01 | properties | 2 | 2 | 2 | 1.00 | auto = compiler backing field + Get/Set; full = explicit block for logic |
| vbnet-props-02 | properties | 2 | 2 | 2 | 1.00 | `_PropertyName` backing field; `= value` initializer |
| vbnet-props-03 | properties | 2 | 2 | 2 | 1.00 | ReadOnly=Get/WriteOnly=Set; ReadOnly auto allowed, no write-only auto |
| vbnet-props-04 | properties | 1 | 2 | 2 | 1.00 | obj(i) indexer; ≥1 parameter required; one default per type |
| vbnet-null-01 | nullability | 3 | 3 | 3 | 1.00 | Nothing=default; ref→null, value→type default (0/False), not null |
| vbnet-null-02 | nullability | 2 | 2 | 2 | 1.00 | Integer?/Nullable(Of T); HasValue/IsNot Nothing before .Value |
| vbnet-null-03 | nullability | 3 | 3 | 3 | 1.00 | IIf evaluates both eagerly; If short-circuits + type-safe vs Object |
| vbnet-null-04 | nullability | 2 | 2 | 2 | 1.00 | Is/IsNot ref identity; = Nothing = value comparison via overloaded operator |
| vbnet-errors-01 | error-handling | 2 | 2 | 2 | 1.00 | Try/Catch(specific first)/Finally always-runs for cleanup |
| vbnet-errors-02 | error-handling | 2 | 2 | 2 | 1.00 | When boolean filter; propagates if False; stack not disturbed |
| vbnet-errors-03 | error-handling | 3 | 2 | 2 | 1.00 | bare Throw preserves stack; Throw ex resets it |
| vbnet-errors-04 | error-handling | 2 | 2 | 2 | 1.00 | On Error/Err legacy unstructured; Try/Catch scoped/typed/preferred |
| vbnet-linq-01 | linq-query | 2 | 2 | 2 | 1.00 | From x In src Where Select shape; names keywords |
| vbnet-linq-02 | linq-query | 2 | 2 | 2 | 1.00 | Group..By..Into Group + aggregates (minor `Count()=Count()` alias glitch, concept intact) |
| vbnet-linq-03 | linq-query | 1 | 2 | 2 | 1.00 | Aggregate → scalar vs From → sequence |
| vbnet-linq-04 | linq-query | 2 | 2 | 2 | 1.00 | runs on enumeration, re-runs each iterate; ToList/ToArray/Count to force |
| vbnet-events-01 | events | 3 | 2 | 2 | 1.00 | WithEvents field + Handles auto-wire, no manual AddHandler (multi-event Handles not shown) |
| vbnet-events-02 | events | 2 | 2 | 2 | 1.00 | AddHandler/RemoveHandler runtime + AddressOf delegate; dynamic wiring cases |
| vbnet-events-03 | events | 2 | 2 | 2 | 1.00 | Event As delegate; RaiseEvent Name(args) no-op if none; also Custom Event |
| vbnet-events-04 | events | 1 | 2 | 2 | 1.00 | WithEvents/Handles static-declarative; AddHandler dynamic + unsubscribe |
| vbnet-oop-01 | oop | 3 | 2 | 1 | 0.50 | MISS: omits VB per-member `... Implements IFoo.Member` binding clause (got Inherits-one/Implements-many) |
| vbnet-oop-02 | oop | 3 | 3 | 3 | 1.00 | Overridable/Overrides/MustOverride/MustInherit/NotOverridable all correct (didn't state "not virtual by default"; wrong parenthetical on NotOverridable-as-default, core seal correct) |
| vbnet-oop-03 | oop | 2 | 2 | 2 | 1.00 | Shared = type-level, one copy, type-name access, no instance state |
| vbnet-oop-04 | oop | 2 | 2 | 2 | 1.00 | Me/MyBase; MyClass bypasses override (this-class impl) |
| vbnet-convarr-01 | conversions-arrays | 3 | 3 | 3 | 1.00 | CType any conv / DirectCast exact-type no-conv / TryCast ref-only Nothing |
| vbnet-convarr-02 | conversions-arrays | 2 | 2 | 2 | 1.00 | widening safe / narrowing may fail; Strict On → narrowing explicit |
| vbnet-convarr-03 | conversions-arrays | 2 | 2 | 2 | 1.00 | 0-based; Dim a(n)=upper bound→n+1; ReDim discards, Preserve keeps |
| vbnet-convarr-04 | conversions-arrays, syntax-basics | 2 | 2 | 2 | 1.00 | CInt/CStr/CDbl intrinsic; & always string vs + ambiguous |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| syntax-basics | 1.00 | 5 | ok | omit (strong) |
| properties | 1.00 | 4 | ok | omit (strong) |
| nullability | 1.00 | 4 | ok | omit (strong) |
| error-handling | 1.00 | 4 | ok | omit (strong) |
| linq-query | 1.00 | 4 | ok | omit (strong) |
| events | 1.00 | 4 | ok | omit (strong) |
| oop | 0.875 | 5 | ok | omit (strong) |
| conversions-arrays | 1.00 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 67.5 / 69 = 97.8%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none** — every tag ≥ 0.75 (lowest is `oop`
at 0.875). No derived skill produced.

---

Grader: independent (Opus 4.8), closed-book answers (`ocode run -dir <empty>`,
no corpus access). Grading credits genuinely-present concepts in valid VB.NET
idiom only.
