---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: vbnet
stack_corpus_rev: 1
threshold: 0.85
validation: with-skill
---

# Scorecard — cat-jam-free on vbnet (with-skill validation)

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers: `vbnet/answers/cat-jam-free.with-skill.md`,
> produced closed-book with the current derived skill
> `derived/vbnet.cat-jam-free.SKILL.md` (`vbnet-tuning-cat-jam-free`) prepended to the
> answerer prompt as active guidance. Baseline: `scores/cat-jam-free.md`.
> No new derived skill is produced from this run.

> Target tags for this skill (baseline < 0.85): **events**.

Graded against `vbnet/questions.yaml` corpus_rev 1 with the same rule as the
baseline: a `point` whose concept is genuinely present earns full credit; a
materially wrong claim about the construct a point tests withholds that point
(marked "(judgment)"); other wrong side claims are listed in the notes column only.
Points are whole; fractions come only from a listed partial.

## Provenance

Closed-book (Rule 0): the answerer was `ocode run -m opencode-go/cat-jam-free -effort med`
with the derived SKILL.md body (front matter stripped) prepended to the `_prompts/vbnet.md`
sheet, in an isolated HOME with every tool denied and a cwd holding only the sheet; no
answer key was attached. The session sqlite shows exactly 1 user + 1 assistant message
and 0 tool calls. The raw output had an unclosed opening YAML fence and a leaked
`</rfl:parameter>` tool-envelope line at the end; the harvester dropped both. The
harvested file holds all 32 ids, each keyed `answer:`.

## History

- Earlier with-skill scorecard: none (first with-skill validation for cat-jam-free on vbnet).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| vbnet-syntax-01 | syntax-basics | 2 | 3 | 3 | 1.00 | end-of-line termination + `:` separator; `_` and implicit continuation |
| vbnet-syntax-02 | syntax-basics, conversions-arrays | 3 | 3 | 3 | 1.00 | widening-only implicit conversions, no late binding, compile-time catch; the baseline's odd "explicit narrowing errors" phrase is gone |
| vbnet-syntax-03 | syntax-basics, oop | 2 | 2 | 2 | 1.00 | members effectively `Shared`, not instantiable; baseline's "singleton container" wording gone |
| vbnet-syntax-04 | syntax-basics | 2 | 2 | 2 | 1.00 | |
| vbnet-props-01 | properties | 2 | 2 | 2 | 1.00 | |
| vbnet-props-02 | properties | 2 | 2 | 2 | 1.00 | `_PropertyName` and `= 0` initializer |
| vbnet-props-03 | properties | 2 | 2 | 2 | 1.00 | now names ctor assignment and that write-only auto-properties need a full property |
| vbnet-props-04 | properties | 1 | 2 | 2 | 1.00 | |
| vbnet-null-01 | nullability | 3 | 3 | 3 | 1.00 | |
| vbnet-null-02 | nullability | 2 | 2 | 2 | 1.00 | |
| vbnet-null-03 | nullability | 3 | 3 | 3 | 1.00 | now names side effects explicitly |
| vbnet-null-04 | nullability | 2 | 2 | 2 | 1.00 | same as baseline: equality-semantics explanation, String-as-"" quirk not named |
| vbnet-errors-01 | error-handling | 2 | 2 | 2 | 1.00 | specific-to-general ordering now stated |
| vbnet-errors-02 | error-handling | 2 | 2 | 2 | 1.00 | "avoids entering a catch block only to inspect and rethrow" earns the avoid-rethrow point the baseline missed; filter-before-unwind still not named |
| vbnet-errors-03 | error-handling | 3 | 2 | 2 | 1.00 | |
| vbnet-errors-04 | error-handling | 2 | 2 | 2 | 1.00 | same as baseline: omits nestable and no-mixing in one method; calls `Err` "global" (it is per-procedure/thread state, minor) |
| vbnet-linq-01 | linq-query | 2 | 2 | 2 | 1.00 | |
| vbnet-linq-02 | linq-query | 2 | 2 | 2 | 1.00 | `Group By x.Category Into Group, Count()`, aliased `Total = Sum(x.Amount)` |
| vbnet-linq-03 | linq-query | 1 | 2 | 2 | 1.00 | scalar-vs-sequence contrast present |
| vbnet-linq-04 | linq-query | 2 | 2 | 2 | 1.00 | re-evaluates on each enumeration now stated |
| vbnet-events-01 | events | 3 | 2 | 2 | 1.00 | "no explicit `AddHandler` call" and "one `Handles` clause can bind one method to several events" both present (baseline withheld this point). Wording tracks the skill nearly verbatim; no code example of the multi-event clause |
| vbnet-events-02 | events | 2 | 2 | 2 | 1.00 | `AddressOf`; Shared events, structure events, non-WithEvents sources named (baseline lacked Shared/Structure) |
| vbnet-events-03 | events | 2 | 2 | 2 | 1.00 | no-op-when-unsubscribed not stated (same as baseline) |
| vbnet-events-04 | events | 1 | 2 | 2 | 1.00 | |
| vbnet-oop-01 | oop | 3 | 2 | 1 | 0.50 | same as baseline: single `Inherits` / many `Implements` correct; per-member `Implements IFoo.Member` clause missing |
| vbnet-oop-02 | oop | 3 | 3 | 3 | 1.00 | non-virtual-by-default still not stated |
| vbnet-oop-03 | oop | 2 | 2 | 2 | 1.00 | |
| vbnet-oop-04 | oop | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-01 | conversions-arrays | 3 | 3 | 2 | 0.67 | CType and DirectCast points clean (DirectCast now: inheritance/interface or exact unbox, throws if incompatible). TryCast point withheld (judgment): "attempts a reference/nullable cast" — VB `TryCast` accepts reference types (and reference-constrained type parameters) only; `TryCast(x, Integer?)` is a compile error. That is the exact scope the point tests. The baseline said "for reference types" and was awarded |
| vbnet-convarr-02 | conversions-arrays | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-03 | conversions-arrays | 2 | 2 | 2 | 1.00 | plain `ReDim` discarding still not stated (same as baseline) |
| vbnet-convarr-04 | conversions-arrays, syntax-basics | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | delta | n | trust | target? |
|-----|---------:|-----------:|------:|--:|-------|---------|
| syntax-basics | 1.00 | 1.00 | 0.00 | 5 | ok | no |
| properties | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| nullability | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| error-handling | 0.89 | 1.00 | +0.11 | 4 | ok | no |
| linq-query | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| events | 0.81 | 1.00 | +0.19 | 4 | ok | **yes** |
| oop | 0.88 | 0.88 | 0.00 | 5 | ok | no |
| conversions-arrays | 1.00 | 0.92 | −0.08 | 5 | ok | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: syntax-basics 11/11; properties 7/7; nullability 10/10;
error-handling 9/9 (baseline 8/9); linq-query 7/7; events 8/8 (baseline 6.5/8);
oop 10.5/12 (baseline 10.5/12); conversions-arrays 11/12 (baseline 12/12).

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 66.5 / 69 = 96.4%
```

(Losses: oop-01 −1.5, convarr-01 −1.0.) Baseline 65 / 69 = 94.2%.

## Target-tag verdict

| tag | baseline | with-skill | pass (≥ 0.85)? |
|-----|---------:|-----------:|----------------|
| events | 0.81 | 1.00 | **PASS** |

**PASS.** Every target tag reaches 0.85. The skill is validated for cat-jam-free @ alpha.
Both baseline gaps the skill cites were absorbed: events-01 now states that no explicit
`AddHandler` is needed and that one `Handles` clause can list several events; events-02
now names `Shared` events and structure events as places `Handles` cannot be used. The
events answers track the skill's wording closely (absorption, as intended — not
contamination: the answer key was never attached).

Non-target drift (not acted on): error-handling 0.89 → 1.00 (errors-02 now names avoiding
catch-and-rethrow); conversions-arrays 1.00 → 0.92 on a single new wrong claim in
convarr-01 (TryCast works on nullable types) that the baseline answer did not contain;
oop unchanged at 0.88 (per-member `Implements` clause still missing). All are
single-sample resampling movement per HOW-TO-EVALUATE.md; if the oop-01 omission or the
TryCast claim recurs in a fresh baseline, derive a section there.

## Anomalies

- Raw output had an unclosed opening YAML fence and a trailing leaked
  `</rfl:parameter>` tool-envelope line; both dropped by the harvester. No answer text
  was lost: 32 ids present, none truncated.
- No contamination signals: non-target answers stay terse and miss the same
  rubric-specific details as the baseline (per-member `Implements`, filter-before-unwind,
  String-as-"" for `= Nothing`).
