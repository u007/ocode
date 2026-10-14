---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: vbnet
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — cat-jam-free on vbnet

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book (Rule 0): the answerer was `ocode run -m opencode-go/cat-jam-free -effort med`
in an empty scratch dir with only the `_prompts/vbnet.md` sheet attached; the session
sqlite shows exactly 1 user + 1 assistant message and 0 tool calls. Headless runs
inject ocode's bundled skill-catalogue digest into the system prompt. Answers file:
`vbnet/answers/cat-jam-free.md`. `opencode-go` publishes no version for this free/alpha
model, so `model_version` is recorded as `"alpha"`. The model self-identifies as
"CatJam, developed by LitterBox AI".

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| vbnet-syntax-01 | syntax-basics | 2 | 3 | 3 | 1.00 | end-of-line termination plus `:` separator; `_` and implicit continuation |
| vbnet-syntax-02 | syntax-basics, conversions-arrays | 3 | 3 | 3 | 1.00 | narrowing + late binding named; odd phrase "explicit type narrowing errors become compile-time errors" (explicit narrowing is allowed), not on the point's construct |
| vbnet-syntax-03 | syntax-basics, oop | 2 | 2 | 2 | 1.00 | calls a Module a "singleton container"; it is a non-instantiable type with implicitly Shared members, not a singleton instance. Both points present |
| vbnet-syntax-04 | syntax-basics | 2 | 2 | 2 | 1.00 | `Return` and assign-to-function-name both given |
| vbnet-props-01 | properties | 2 | 2 | 2 | 1.00 | |
| vbnet-props-02 | properties | 2 | 2 | 2 | 1.00 | names `_PropertyName` but hedges "or a generated hidden name"; `= value` initializer present |
| vbnet-props-03 | properties | 2 | 2 | 2 | 1.00 | ReadOnly auto-property with initializer; does not mention ctor assignment or that WriteOnly auto-properties are disallowed |
| vbnet-props-04 | properties | 1 | 2 | 2 | 1.00 | |
| vbnet-null-01 | nullability | 3 | 3 | 3 | 1.00 | |
| vbnet-null-02 | nullability | 2 | 2 | 2 | 1.00 | |
| vbnet-null-03 | nullability | 3 | 3 | 3 | 1.00 | short-circuit, all-args-evaluated, IIf returns Object; never names the concrete side-effect/throw hazard, but the typing half of point 3 is present |
| vbnet-null-04 | nullability | 2 | 2 | 2 | 1.00 | `= Nothing` as value equality explained only generically; no String-as-"" example |
| vbnet-errors-01 | error-handling | 2 | 2 | 2 | 1.00 | specific-first Catch ordering not mentioned |
| vbnet-errors-02 | error-handling | 2 | 2 | 1 | 0.50 | filter semantics correct; benefit given as "avoids entering an inappropriate handler" / "preserve the original exception flow more selectively". No filter-before-unwind, no avoiding catch-and-rethrow |
| vbnet-errors-03 | error-handling | 3 | 2 | 2 | 1.00 | |
| vbnet-errors-04 | error-handling | 2 | 2 | 2 | 1.00 | omits nestable and that `On Error` and `Try` cannot be mixed in one method |
| vbnet-linq-01 | linq-query | 2 | 2 | 2 | 1.00 | does not note `Select` is optional in VB |
| vbnet-linq-02 | linq-query | 2 | 2 | 2 | 1.00 | |
| vbnet-linq-03 | linq-query | 1 | 2 | 2 | 1.00 | |
| vbnet-linq-04 | linq-query | 2 | 2 | 2 | 1.00 | re-run-on-each-enumeration not stated |
| vbnet-events-01 | events | 3 | 2 | 1 | 0.50 | auto-wiring present ("the compiler generates the delegate wiring"); never states that no explicit `AddHandler` is needed, nor that one `Handles` clause can list several events |
| vbnet-events-02 | events | 2 | 2 | 2 | 1.00 | dynamic / non-WithEvents-source cases; never names `Shared` events or `Structure` events as places `Handles` cannot be used |
| vbnet-events-03 | events | 2 | 2 | 2 | 1.00 | RaiseEvent no-op with no handlers not mentioned |
| vbnet-events-04 | events | 1 | 2 | 2 | 1.00 | |
| vbnet-oop-01 | oop | 3 | 2 | 1 | 0.50 | single Inherits / many Implements correct; misses VB's per-member `Implements IFoo.Member` clause |
| vbnet-oop-02 | oop | 3 | 3 | 3 | 1.00 | all five keywords right; does not state methods are non-virtual by default |
| vbnet-oop-03 | oop | 2 | 2 | 2 | 1.00 | |
| vbnet-oop-04 | oop | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-01 | conversions-arrays | 3 | 3 | 3 | 1.00 | DirectCast described as "exact type compatibility", no conversion; does not say it throws on mismatch or that inheritance relationships qualify |
| vbnet-convarr-02 | conversions-arrays | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-03 | conversions-arrays | 2 | 2 | 2 | 1.00 | `Dim a(n)` = 0..n, n+1 elements; `ReDim Preserve` / `Array.Resize`; does not say plain `ReDim` discards |
| vbnet-convarr-04 | conversions-arrays, syntax-basics | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| syntax-basics | 1.00 | 5 | ok | omit (strong) |
| properties | 1.00 | 4 | ok | omit (strong) |
| nullability | 1.00 | 4 | ok | omit (strong) |
| error-handling | 0.89 | 4 | ok | omit (above threshold) |
| linq-query | 1.00 | 4 | ok | omit (strong) |
| events | 0.81 | 4 | ok | **derive** |
| oop | 0.88 | 5 | ok | omit (above threshold) |
| conversions-arrays | 1.00 | 5 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

- error-handling = 8 / 9 = 0.889
- events = 6.5 / 8 = 0.8125
- oop = 10.5 / 12 = 0.875

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 65 / 69 = 94.2%
```

## Derivation targets

Tags below threshold (`< 0.85`): **events** → feed into
`derived/vbnet.cat-jam-free.SKILL.md`.
Nearest above threshold: oop (0.88), error-handling (0.89).

## Anomalies

- The answers file is clean: 32 ids, each keyed `answer:`, no prose lead-in or
  trailing commentary.
- No contamination signals: answers are terse, roughly reference-length or
  shorter, use their own examples (`Foo`, `x.Key`, `x.Amount`) and miss
  rubric-specific details the key spells out (per-member `Implements`,
  multi-event `Handles`, filter-before-unwind). Nothing reads as a copy.
- Recurring pattern: no confident wrong claims on any graded construct. Every
  lost point is an omission of the VB-specific detail (the per-member
  `Implements` clause, "no explicit `AddHandler`" / multi-event `Handles`, the
  filter-runs-before-unwind benefit). The answers state the generic .NET concept
  and stop.
