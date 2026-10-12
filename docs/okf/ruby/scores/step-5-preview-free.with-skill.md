---
model_id: step-5-preview-free
model_version: "5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-12
stack: ruby
stack_corpus_rev: 1
threshold: 0.85
validation: with-skill
---

# Scorecard — step-5-preview-free on ruby (with-skill validation)

> Valid ONLY for `step-5-preview-free` @ `5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers: `ruby/answers/step-5-preview-free.with-skill.md`,
> produced closed-book with the current derived skill
> `derived/ruby.step-5-preview-free.SKILL.md` (`ruby-tuning-step-5-preview-free`)
> prepended to the answerer prompt as active guidance (audited zero tool calls).
> Baseline: `scores/step-5-preview-free.md`. No new derived skill is produced from
> this run.

> Target tags for this skill (baseline < 0.85): **collections-idioms**.

Graded against `ruby/questions.yaml` corpus_rev 1 with the same rule the baseline
grader applied: a `point` whose concept is genuinely present earns full credit; a
materially wrong claim about the same construct withholds that point and is marked
"(judgment)". Points are whole; fractions come only from a listed partial.

## Provenance

Closed-book answers in `answers/step-5-preview-free.with-skill.md`: the body of
`derived/ruby.step-5-preview-free.SKILL.md` was prepended to the `_prompts/ruby.md`
sheet as active guidance, no answer key attached; the session audit shows exactly
1 user + 1 assistant message and 0 tool calls (opencode-go, 2026-10-12).

## History

- Earlier with-skill scorecard: none (this is the first with-skill validation for
  step-5-preview-free on ruby).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| ruby-blocks-procvslambda-01 | blocks-procs | 3 | 2 | 2 | 1.00 |  |
| ruby-blocks-yield-02 | blocks-procs | 2 | 2 | 2 | 1.00 |  |
| ruby-blocks-ampblock-03 | blocks-procs | 2 | 2 | 2 | 1.00 |  |
| ruby-blocks-create-04 | blocks-procs | 1 | 2 | 2 | 1.00 | trailing aside "lambdas additionally satisfy `===`" — `Proc#===` exists on every Proc, not just lambdas; off-rubric |
| ruby-modules-includeextendprepend-01 | modules-mixins | 3 | 3 | 3 | 1.00 | clean this time (no garbled phrase) |
| ruby-modules-ancestors-super-02 | modules-mixins | 2 | 2 | 2 | 1.00 | lookup order list omits prepended modules (singleton → class → included → superclass); chain-walk and module-as-ancestor concepts present (minor) |
| ruby-modules-namespace-03 | modules-mixins | 1 | 2 | 2 | 1.00 | `module_function` mechanics (singleton copy + private instance copy) correct |
| ruby-modules-refinements-04 | modules-mixins | 1 | 2 | 2 | 1.00 | snippet now uses `do…end`; correctly notes `using` is not allowed inside a method body |
| ruby-objects-methodmissing-01 | objects-methods | 3 | 2 | 2 | 1.00 | `super` for unhandled names now stated |
| ruby-objects-send-02 | objects-methods | 2 | 2 | 2 | 1.00 |  |
| ruby-objects-attr-03 | objects-methods | 2 | 2 | 2 | 1.00 |  |
| ruby-objects-visibility-04 | objects-methods | 1 | 2 | 1 | 0.50 | (judgment) private point withheld, same false claim as baseline: "the sole exception: a private setter may be called as `self.x = v` since Ruby 2.7" — `self.x=` was always allowed and 2.7 lifted the restriction for every `self.` private call; public-is-default not stated. Protected point awarded |
| ruby-enumerable-include-01 | enumerable | 3 | 2 | 2 | 1.00 |  |
| ruby-enumerable-reduce-02 | enumerable | 2 | 2 | 2 | 1.00 |  |
| ruby-enumerable-lazy-03 | enumerable | 2 | 2 | 2 | 1.00 |  |
| ruby-enumerable-comparable-04 | enumerable | 2 | 2 | 2 | 1.00 | correctly adds that a nil `<=>` makes the comparison operators raise ArgumentError |
| ruby-metaprogramming-singleton-01 | metaprogramming | 2 | 2 | 2 | 1.00 |  |
| ruby-metaprogramming-ivar-02 | metaprogramming | 1 | 2 | 2 | 1.00 |  |
| ruby-metaprogramming-definemethod-vs-mm-03 | metaprogramming | 2 | 2 | 2 | 1.00 |  |
| ruby-metaprogramming-classnew-04 | metaprogramming | 1 | 2 | 2 | 1.00 |  |
| ruby-error-standarderror-01 | error-handling | 3 | 3 | 3 | 1.00 | no contradictory snippet this run; "custom errors subclass StandardError" is implied by "rescue StandardError subclasses", not stated (minor, same credit as baseline) |
| ruby-error-ensure-retry-02 | error-handling | 2 | 2 | 2 | 1.00 | baseline's wrong aside about ensure's value is gone; correctly notes ensure does not run between `retry` attempts |
| ruby-error-custom-03 | error-handling | 2 | 2 | 2 | 1.00 |  |
| ruby-error-elserescue-04 | error-handling | 1 | 2 | 2 | 1.00 |  |
| ruby-strings-symbols-01 | strings-symbols | 3 | 2 | 2 | 1.00 |  |
| ruby-strings-frozen-02 | strings-symbols | 2 | 2 | 2 | 1.00 | interpolation caveat now correct (interpolated literal is a fresh, unfrozen String); does not date it to 3.0 (minor). Baseline lost this point |
| ruby-strings-quotes-03 | strings-symbols | 1 | 2 | 2 | 1.00 |  |
| ruby-strings-percent-04 | strings-symbols | 1 | 2 | 2 | 1.00 | no garbled self-correction this run |
| ruby-collections-hashdefault-01 | collections-idioms | 2 | 2 | 2 | 1.00 | both points; shared-default-stores-nothing vs block-assigns-and-creates-key stated plainly; no nonsense trailer |
| ruby-collections-kwargs-02 | collections-idioms | 3 | 2 | 2 | 1.00 | separation point now correct and one-directional (positional Hash → keywords raises; keywords → no-keyword method still becomes a trailing Hash); `m(**hash)` / `def m(**opts)`; `ruby2_keywords` aside is accurate. Baseline withheld the separation point |
| ruby-collections-splat-03 | collections-idioms | 2 | 2 | 2 | 1.00 | params and call-site points both present; no longer claims `**opts` can't coexist with named keywords; correctly calls `first, **rest = hash` a SyntaxError. "Ruby 3.0 lets `def wrapper(...) = target(...)`" conflates `...` (2.7) with endless def (3.0) — tangential |
| ruby-collections-safenav-data-04 | collections-idioms | 2 | 2 | 2 | 1.00 | `&.` guards nil only (not false) and Data.define details (frozen, readers only, `with`, value equality) all correct; no rambling |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | delta | n | trust | target? |
|-----|---------:|-----------:|------:|--:|-------|---------|
| blocks-procs | 1.00 | 1.00 | +0.00 | 4 | ok | no |
| modules-mixins | 1.00 | 1.00 | +0.00 | 4 | ok | no |
| objects-methods | 0.94 | 0.94 | +0.00 | 4 | ok | no |
| enumerable | 1.00 | 1.00 | +0.00 | 4 | ok | no |
| metaprogramming | 1.00 | 1.00 | +0.00 | 4 | ok | no |
| error-handling | 1.00 | 1.00 | +0.00 | 4 | ok | no |
| strings-symbols | 0.86 | 1.00 | +0.14 | 4 | ok | no |
| collections-idioms | 0.72 | 1.00 | +0.28 | 4 | ok | **yes** |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: blocks-procs 8/8; modules-mixins 7/7; objects-methods 7.5/8;
enumerable 9/9; metaprogramming 6/6; error-handling 8/8; strings-symbols 7/7;
collections-idioms 9/9.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 61.5 / 62 = 99.2%
```

(Loss: visibility-04 −0.5.) Baseline 58 / 62 = 93.5%.

## Target-tag verdict

| tag | baseline | with-skill | pass (≥ 0.85)? |
|-----|---------:|-----------:|----------------|
| collections-idioms | 0.72 | 1.00 | **PASS** |

The single target tag reaches the threshold. The skill is validated for
step-5-preview-free @ 5-preview. Both baseline losses on the tag (kwargs-02
"keywords to a `*args`-only method raises"; splat-03 "`**opts` cannot coexist with
named keywords" and `first, **rest = hash` offered as destructuring) are gone, and
the answers state the corrected rules in the one-directional form the skill
prescribes. The garbled mid-paragraph self-corrections the baseline noted on
hashdefault-01, kwargs-02, percent-04 and includeextendprepend-01 did not recur.

Non-target drift (not a reason to edit the skill): strings-symbols 0.86 → 1.00
(frozen-02 interpolation caveat now correct) and the answer-level cleanups above are
single-sample resampling movement per HOW-TO-EVALUATE.md ("Only trust TARGET-tag
movement"); objects-methods stays 0.94 on the same visibility-04 false claim as the
baseline (`self.x=` setter dated to 2.7), a non-target already above threshold. If a
fresh baseline reproduces it, derive there, not here.
