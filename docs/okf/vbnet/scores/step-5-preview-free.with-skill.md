---
model_id: step-5-preview-free
model_version: "5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-12
stack: vbnet
stack_corpus_rev: 1
threshold: 0.85
validation: with-skill
---

# Scorecard — step-5-preview-free on vbnet (with-skill validation)

> Valid ONLY for `step-5-preview-free` @ `5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers: `vbnet/answers/step-5-preview-free.with-skill.md`,
> produced closed-book with the current derived skill
> `derived/vbnet.step-5-preview-free.SKILL.md` (`vbnet-tuning-step-5-preview-free`)
> prepended to the answerer prompt as active guidance (session audited: 1 user +
> 1 assistant message, 0 tool calls). Baseline: `scores/step-5-preview-free.md`.
> No new derived skill is produced from this run.

> Target tags for this skill (baseline < 0.85): **linq-query**.

Graded against `vbnet/questions.yaml` corpus_rev 1 with the same rule as the
baseline: a `point` whose concept is genuinely present earns full credit; a
materially wrong claim about the construct a point tests withholds that point
(marked "(judgment)"); other wrong side claims are listed in the notes column only.
Points are whole; fractions come only from a listed partial.

## Provenance

Closed-book (Rule 0): the answerer was `ocode run -m opencode-go/step-5-preview-free`
in an empty scratch dir with the derived skill body prepended to the `_prompts/vbnet.md`
sheet; no answer key was attached. The session shows exactly 1 user + 1 assistant
message and 0 tool calls. Answers file: `vbnet/answers/step-5-preview-free.with-skill.md`.

## History

- Earlier with-skill scorecard: none (this is the first with-skill validation for
  step-5-preview-free on vbnet).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| vbnet-syntax-01 | syntax-basics | 2 | 3 | 3 | 1.00 | also names the `:` statement separator |
| vbnet-syntax-02 | syntax-basics, conversions-arrays | 3 | 3 | 3 | 1.00 | |
| vbnet-syntax-03 | syntax-basics, oop | 2 | 2 | 2 | 1.00 | same as baseline: says members are reached as `ModuleName.Member`; unqualified access is the norm |
| vbnet-syntax-04 | syntax-basics | 2 | 2 | 2 | 1.00 | both return forms present; closes with "a caller does not put parentheses around a Sub call's empty argument list", contradicting its own `DoWork()` example (parens are the norm) |
| vbnet-props-01 | properties | 2 | 2 | 2 | 1.00 | |
| vbnet-props-02 | properties | 2 | 2 | 2 | 1.00 | `_Name` and `= value` present; says the field is reachable "only via reflection" (it is usable inside the class — baseline carried the same note and awarded) and that the initializer "must be evaluable at compile time", while its own `Guid.NewGuid()` example is not |
| vbnet-props-03 | properties | 2 | 2 | 2 | 1.00 | ReadOnly auto-property with initializer correct; the baseline's wrong WriteOnly-auto-property claim is gone |
| vbnet-props-04 | properties | 1 | 2 | 2 | 1.00 | indexer use and ≥1 parameter present; offers `<DefaultProperty("Item")>` as an alternative way to declare one (that is a designer attribute, not a VB default property) |
| vbnet-null-01 | nullability | 3 | 3 | 3 | 1.00 | |
| vbnet-null-02 | nullability | 2 | 2 | 2 | 1.00 | |
| vbnet-null-03 | nullability | 3 | 3 | 3 | 1.00 | |
| vbnet-null-04 | nullability | 2 | 2 | 2 | 1.00 | same as baseline: value-equality / may-not-compile explanation, String-as-"" quirk not named |
| vbnet-errors-01 | error-handling | 2 | 2 | 2 | 1.00 | |
| vbnet-errors-02 | error-handling | 2 | 2 | 2 | 1.00 | filter semantics and avoid-rethrow present; side claim "Throw loses the original stack trace" is wrong for bare `Throw` and contradicts its own errors-03 answer |
| vbnet-errors-03 | error-handling | 3 | 2 | 2 | 1.00 | |
| vbnet-errors-04 | error-handling | 2 | 2 | 2 | 1.00 | same as baseline: omits that `On Error` and `Try` cannot be mixed in one method |
| vbnet-linq-01 | linq-query | 2 | 2 | 2 | 1.00 | odd phrase "JScript-case-insensitive"; shape and keywords clean |
| vbnet-linq-02 | linq-query | 2 | 2 | 2 | 1.00 | `Group By p.City Into Group`, aggregates aliased in the same `Into`; states `Into` is mandatory (baseline claimed it could be skipped). Wording tracks the skill closely |
| vbnet-linq-03 | linq-query | 1 | 2 | 2 | 1.00 | `Aggregate n In nums Where n > 0 Into Sum(n)` valid; nested `From c In customers Aggregate o In c.Orders Into ...` valid; scalar-vs-sequence contrast present; explicitly distinguishes it from `Enumerable.Aggregate` (baseline equated them) |
| vbnet-linq-04 | linq-query | 2 | 2 | 2 | 1.00 | `For n = 0 To q.Count - 1` uses `Count` as a property on an IEnumerable (minor) |
| vbnet-events-01 | events | 3 | 2 | 2 | 1.00 | |
| vbnet-events-02 | events | 2 | 2 | 2 | 1.00 | non-WithEvents sources and `Shared` events named; `Structure` events not (baseline awarded the same coverage) |
| vbnet-events-03 | events | 2 | 2 | 2 | 1.00 | no-op-when-unsubscribed not stated (minor) |
| vbnet-events-04 | events | 1 | 2 | 2 | 1.00 | |
| vbnet-oop-01 | oop | 3 | 2 | 2 | 1.00 | per-member `Implements IDisposable.Dispose` shown |
| vbnet-oop-02 | oop | 3 | 3 | 3 | 1.00 | `NotOverridable` now correctly a sealed `Overrides`, with `NotInheritable` named for sealing a class (baseline withheld this point) |
| vbnet-oop-03 | oop | 2 | 2 | 2 | 1.00 | |
| vbnet-oop-04 | oop | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-01 | conversions-arrays | 3 | 3 | 2 | 0.67 | CType and TryCast points clean. DirectCast point withheld (judgment): states DirectCast "cannot be used for value-type conversions or unboxing (use CType for those)" — `DirectCast(obj, Integer)` unboxes fine when the boxed runtime type is Integer; the rubric's own answer lists unboxing as DirectCast's exact-type case. The baseline answer did not make this claim |
| vbnet-convarr-02 | conversions-arrays | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-03 | conversions-arrays | 2 | 2 | 2 | 1.00 | baseline's dangling `Dim a(-1)` fragment is gone; one redundant sentence restates the last-dimension rule |
| vbnet-convarr-04 | conversions-arrays, syntax-basics | 2 | 2 | 2 | 1.00 | baseline's `Option Compare` parsing claim is gone; banker's rounding example correct |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | delta | n | trust | target? |
|-----|---------:|-----------:|------:|--:|-------|---------|
| syntax-basics | 1.00 | 1.00 | 0.00 | 5 | ok | no |
| properties | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| nullability | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| error-handling | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| linq-query | 0.79 | 1.00 | +0.21 | 4 | ok | **yes** |
| events | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| oop | 0.92 | 1.00 | +0.08 | 5 | ok | no |
| conversions-arrays | 1.00 | 0.92 | −0.08 | 5 | ok | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: syntax-basics 11/11; properties 7/7; nullability 10/10;
error-handling 9/9; linq-query 7/7 (baseline 5.5/7); events 8/8; oop 12/12
(baseline 11/12); conversions-arrays 11/12 (baseline 12/12).

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 68 / 69 = 98.6%
```

(Loss: convarr-01 −1.0.) Baseline 66.5 / 69 = 96.4%.

## Target-tag verdict

| tag | baseline | with-skill | pass (≥ 0.85)? |
|-----|---------:|-----------:|----------------|
| linq-query | 0.79 | 1.00 | **PASS** |

Every target tag reaches 0.85. The skill is validated for step-5-preview-free @ 5-preview.
Both baseline failures the skill cites were corrected: `Into` is now stated as mandatory
in `Group By` (linq-02), and `Aggregate` is written with `Into`, not `In`, without the
redeclared range variable and without being equated to `Enumerable.Aggregate` (linq-03).

Non-target drift (not acted on): conversions-arrays 1.00 → 0.92 on a single new wrong
claim in convarr-01 (DirectCast cannot unbox) that the baseline answer did not contain;
oop 0.92 → 1.00 on a corrected `NotOverridable` description the skill never mentions.
Both are single-sample resampling movement per HOW-TO-EVALUATE.md; if the DirectCast
claim recurs in a fresh baseline, derive a conversions-arrays section there.
