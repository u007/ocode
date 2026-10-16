---
model_id: nvidia/nemotron-3.5-super-vl
model_version: "3.5"
evaluated_via: openrouter
evaluated_on: 2026-10-16
stack: vbnet
stack_corpus_rev: 1
threshold: 0.85
validation: with-skill
---

# Scorecard — nvidia/nemotron-3.5-super-vl on vbnet (with-skill validation)

> Valid ONLY for `nvidia/nemotron-3.5-super-vl` @ `3.5`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers:
> `vbnet/answers/nvidia__nemotron-3.5-super-vl.with-skill.md`, produced closed-book
> with the current derived skill `derived/vbnet.nvidia__nemotron-3.5-super-vl.SKILL.md`
> (`vbnet-tuning-nvidia-nemotron-3.5-super-vl`, front matter stripped) prepended to
> the answerer prompt as active guidance (session audited: 1 user + 1 assistant
> message, 0 tool calls). Baseline: `scores/nvidia__nemotron-3.5-super-vl.md`.
> No new derived skill is produced from this run.

> Target tags for this skill (baseline < 0.85): **oop**.

Graded against `vbnet/questions.yaml` corpus_rev 1 with the same rule as the
baseline: a `point` whose concept is genuinely present earns full credit; a
materially wrong claim about the construct a point tests withholds that point
(marked "(judgment)"); other wrong side claims are listed in the notes column only.
Points are whole; fractions come only from a listed partial.

## Provenance

Closed-book (Rule 0). The answerer was
`ocode run -m openrouter/nvidia/nemotron-3.5-super-vl:free -effort med` under an
isolated HOME whose config denies every tool, in a scratch cwd containing only the
`_prompts/vbnet.md` sheet with the derived skill body prepended; no answer key was
attached. The session shows exactly 1 user + 1 assistant message and 0 tool calls.
All 32 ids came back once, none empty; no answer cites a tool or skill.

## History

- Earlier with-skill scorecard: none (this is the first with-skill validation for
  nvidia/nemotron-3.5-super-vl on vbnet).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| vbnet-syntax-01 | syntax-basics | 2 | 3 | 3 | 1.00 | |
| vbnet-syntax-02 | syntax-basics, conversions-arrays | 3 | 3 | 3 | 1.00 | |
| vbnet-syntax-03 | syntax-basics, oop | 2 | 2 | 2 | 1.00 | same as baseline: says members are accessed "through the module name"; unqualified access is the norm |
| vbnet-syntax-04 | syntax-basics | 2 | 2 | 2 | 1.00 | baseline's "followed by `Exit Function`" softened to "and then exit" |
| vbnet-props-01 | properties | 2 | 2 | 2 | 1.00 | |
| vbnet-props-02 | properties | 2 | 2 | 1 | 0.50 | initializer `= "default"` correct (point 2). Point 1 withheld (judgment): claims the backing field "is also named `Name` — fields and properties live in separate namespaces"; it is `_Name`. The baseline answer had `_Name` right |
| vbnet-props-03 | properties | 2 | 2 | 2 | 1.00 | same as baseline: `WriteOnly Property Password As String` shown as a bare one-liner (a WriteOnly auto-property is a compile error); ReadOnly auto-property point right |
| vbnet-props-04 | properties | 1 | 2 | 2 | 1.00 | |
| vbnet-null-01 | nullability | 3 | 3 | 3 | 1.00 | |
| vbnet-null-02 | nullability | 2 | 2 | 2 | 1.00 | |
| vbnet-null-03 | nullability | 3 | 3 | 3 | 1.00 | |
| vbnet-null-04 | nullability | 2 | 2 | 2 | 1.00 | value-equality / overloaded-operator explanation; String-as-"" quirk not named (same as baseline) |
| vbnet-errors-01 | error-handling | 2 | 2 | 2 | 1.00 | |
| vbnet-errors-02 | error-handling | 2 | 2 | 2 | 1.00 | |
| vbnet-errors-03 | error-handling | 3 | 2 | 2 | 1.00 | |
| vbnet-errors-04 | error-handling | 2 | 2 | 2 | 1.00 | same as baseline: omits that `On Error` and `Try` cannot be mixed in one method |
| vbnet-linq-01 | linq-query | 2 | 2 | 2 | 1.00 | |
| vbnet-linq-02 | linq-query | 2 | 2 | 2 | 1.00 | `Group By c.City Into cityGroup = Group, Count(c)`: form and aliasing right; `Count(c)` passes the element where the aggregate takes an optional Boolean (`Count()` or `Count(c.Active)`), a compile error under Strict On. Baseline's `Aggregate`-inside-`Into` claim is gone |
| vbnet-linq-03 | linq-query | 1 | 2 | 2 | 1.00 | |
| vbnet-linq-04 | linq-query | 2 | 2 | 2 | 1.00 | |
| vbnet-events-01 | events | 3 | 2 | 2 | 1.00 | same as baseline: declarative compile-time wiring present; never says one `Handles` may list several events |
| vbnet-events-02 | events | 2 | 2 | 2 | 1.00 | same as baseline: `Shared` / `Structure` events not named |
| vbnet-events-03 | events | 2 | 2 | 2 | 1.00 | answers with the rare `Custom Event ... AddHandler/RemoveHandler/RaiseEvent` accessor form (valid VB) and never shows the ordinary one-line `Public Event X As EventHandler`; `RaiseEvent MyEvent(Me, e)` correct |
| vbnet-events-04 | events | 1 | 2 | 2 | 1.00 | |
| vbnet-oop-01 | oop | 3 | 2 | 2 | 1.00 | per-member `Implements IFoo.Member` clause now stated; baseline's `MustInherit`-exemption claim is gone |
| vbnet-oop-02 | oop | 3 | 3 | 3 | 1.00 | `NotOverridable` now "may only appear with `Overrides` and seals that override" (baseline withheld this point) |
| vbnet-oop-03 | oop | 2 | 2 | 2 | 1.00 | now also states there is no `Me` in a Shared member |
| vbnet-oop-04 | oop | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-01 | conversions-arrays | 3 | 3 | 3 | 1.00 | TryCast returns-Nothing concept present; the reference-types-only restriction is not stated (baseline said "reference/nullable", also imprecise) |
| vbnet-convarr-02 | conversions-arrays | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-03 | conversions-arrays | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-04 | conversions-arrays, syntax-basics | 2 | 2 | 2 | 1.00 | baseline's "legacy" label on `CInt`/`CStr`/`CDbl` is gone; now "intrinsic conversion functions" |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | delta | n | trust | target? |
|-----|---------:|-----------:|------:|--:|-------|---------|
| syntax-basics | 1.00 | 1.00 | 0.00 | 5 | ok | no |
| properties | 1.00 | 0.86 | −0.14 | 4 | ok | no |
| nullability | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| error-handling | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| linq-query | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| events | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| oop | 0.79 | 1.00 | +0.21 | 5 | ok | **yes** |
| conversions-arrays | 1.00 | 1.00 | 0.00 | 5 | ok | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: syntax-basics 11/11; properties 6/7 (baseline 7/7); nullability 10/10;
error-handling 9/9; linq-query 7/7; events 8/8; oop 12/12 (baseline 9.5/12);
conversions-arrays 12/12.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 68 / 69 = 98.6%
```

(Loss: props-02 −1.0.) Baseline 66.5 / 69 = 96.4%.

## Target-tag verdict

| tag | baseline | with-skill | pass (≥ 0.85)? |
|-----|---------:|-----------:|----------------|
| oop | 0.79 | 1.00 | **PASS** |

Every target tag reaches 0.85. The skill is validated for
nvidia/nemotron-3.5-super-vl @ 3.5. Both baseline failures the skill cites were
corrected: the per-member `Implements IFoo.Member` clause is now stated and the
`MustInherit` exemption claim is gone (oop-01); `NotOverridable` is now described as
legal only with `Overrides`, sealing that override (oop-02).

Non-target drift (not acted on): properties 1.00 → 0.86 on a single new wrong claim
in props-02 (backing field named `Name`, not `_Name`) that the baseline answer did not
contain; the tag stays at the threshold. This is single-sample resampling movement per
HOW-TO-EVALUATE.md; if the `_Name` error recurs in a fresh baseline, derive a
properties section there. Several baseline side claims also disappeared on resampling
(`Aggregate` inside `Into`, `CInt` as "legacy", `Exit Function`), none of which the
skill mentions.
