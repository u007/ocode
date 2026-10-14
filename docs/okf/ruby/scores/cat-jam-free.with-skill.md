---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: ruby
stack_corpus_rev: 1
threshold: 0.85
validation: with-skill
---

# Scorecard — cat-jam-free on ruby (with-skill validation)

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers: `ruby/answers/cat-jam-free.with-skill.md`,
> produced closed-book with the current derived skill
> `derived/ruby.cat-jam-free.SKILL.md` (`ruby-tuning-cat-jam-free`)
> prepended to the answerer prompt as active guidance (audited zero tool calls).
> Baseline: `scores/cat-jam-free.md`. No new derived skill is produced from
> this run.

> Target tags for this skill (baseline < 0.85): **objects-methods**.

Graded against `ruby/questions.yaml` corpus_rev 1 with the same rule the baseline
grader applied: a `point` whose concept is genuinely present earns full credit; a
materially wrong claim about the same construct withholds that point and is marked
"(judgment)". Points are whole; fractions come only from a listed partial.

## Provenance

Closed-book answers in `answers/cat-jam-free.with-skill.md`. The answerer was
`ocode run -m opencode-go/cat-jam-free -effort med` with the body of
`derived/ruby.cat-jam-free.SKILL.md` (front matter stripped) prepended to the
`_prompts/ruby.md` sheet, no answer key attached; isolated HOME with every tool
denied, cwd holding only the sheet. The session audit shows exactly 1 user + 1
assistant message and 0 tool calls (opencode-go, 2026-10-14).

## History

- Earlier with-skill scorecard: none (this is the first with-skill validation for
  cat-jam-free on ruby).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| ruby-blocks-procvslambda-01 | blocks-procs | 3 | 2 | 2 | 1.00 |  |
| ruby-blocks-yield-02 | blocks-procs | 2 | 2 | 2 | 1.00 |  |
| ruby-blocks-ampblock-03 | blocks-procs | 2 | 2 | 2 | 1.00 |  |
| ruby-blocks-create-04 | blocks-procs | 1 | 2 | 2 | 1.00 |  |
| ruby-modules-includeextendprepend-01 | modules-mixins | 3 | 3 | 3 | 1.00 | (judgment) include point awarded: says include "places the module in the ancestor chain" without saying it sits above the class; class-overrides-module is carried only by the contrast with prepend ("before the class ... so it can override or wrap") |
| ruby-modules-ancestors-super-02 | modules-mixins | 2 | 2 | 2 | 1.00 | lookup order now correct (class → included modules → superclasses); prepended modules omitted (minor). Baseline's inverted "insertion order" claim gone. Baseline withheld the lookup-order point |
| ruby-modules-namespace-03 | modules-mixins | 1 | 2 | 2 | 1.00 | `::` not named (same as baseline); "often after a private method definition" is an odd aside, off-rubric |
| ruby-modules-refinements-04 | modules-mixins | 1 | 2 | 2 | 1.00 |  |
| ruby-objects-methodmissing-01 | objects-methods | 3 | 2 | 2 | 1.00 | `super` fallthrough for unrecognized names now stated, plus respond_to_missing?. Baseline withheld point 1 |
| ruby-objects-send-02 | objects-methods | 2 | 2 | 2 | 1.00 |  |
| ruby-objects-attr-03 | objects-methods | 2 | 2 | 2 | 1.00 |  |
| ruby-objects-visibility-04 | objects-methods | 1 | 2 | 2 | 1.00 | public-is-default stated; private: explicit receiver other than `self` forbidden, `self.foo` allowed for any private method since 2.7 (correct). Baseline's stale "not even self" claim gone. Protected correct |
| ruby-enumerable-include-01 | enumerable | 3 | 2 | 2 | 1.00 |  |
| ruby-enumerable-reduce-02 | enumerable | 2 | 2 | 2 | 1.00 | (judgment) as baseline: block-return-becomes-accumulator carried by "does not require returning the accumulator each iteration" |
| ruby-enumerable-lazy-03 | enumerable | 2 | 2 | 2 | 1.00 |  |
| ruby-enumerable-comparable-04 | enumerable | 2 | 2 | 2 | 1.00 |  |
| ruby-metaprogramming-singleton-01 | metaprogramming | 2 | 2 | 2 | 1.00 |  |
| ruby-metaprogramming-ivar-02 | metaprogramming | 1 | 2 | 2 | 1.00 |  |
| ruby-metaprogramming-definemethod-vs-mm-03 | metaprogramming | 2 | 2 | 2 | 1.00 | respond_to_missing? now named on the method_missing side |
| ruby-metaprogramming-classnew-04 | metaprogramming | 1 | 2 | 2 | 1.00 |  |
| ruby-error-standarderror-01 | error-handling | 3 | 3 | 3 | 1.00 | (judgment) third point awarded on implication: states bare rescue catches StandardError and that rescuing Exception is usually wrong, but never says outright "rescue StandardError instead" (the baseline said it) |
| ruby-error-ensure-retry-02 | error-handling | 2 | 2 | 2 | 1.00 |  |
| ruby-error-custom-03 | error-handling | 2 | 2 | 2 | 1.00 | raise forms now complete (`raise "str"` → RuntimeError, bare raise re-raises) |
| ruby-error-elserescue-04 | error-handling | 1 | 2 | 1 | 0.50 | implicit-begin point withheld: never says a method body can take rescue/ensure/else without `begin` (the baseline said it). else point awarded |
| ruby-strings-symbols-01 | strings-symbols | 3 | 2 | 2 | 1.00 |  |
| ruby-strings-frozen-02 | strings-symbols | 2 | 2 | 1 | 0.50 | (judgment) caveat point withheld, same error as baseline: "interpolated string literals are also frozen" — inverted; since 3.0 interpolated literals are NOT frozen under the pragma |
| ruby-strings-quotes-03 | strings-symbols | 1 | 2 | 2 | 1.00 |  |
| ruby-strings-percent-04 | strings-symbols | 1 | 2 | 2 | 1.00 |  |
| ruby-collections-hashdefault-01 | collections-idioms | 2 | 2 | 2 | 1.00 |  |
| ruby-collections-kwargs-02 | collections-idioms | 3 | 2 | 2 | 1.00 | separation stated one-directionally; `m(**hash)` present, `def m(**opts)` not mentioned (same as baseline) |
| ruby-collections-splat-03 | collections-idioms | 2 | 2 | 2 | 1.00 |  |
| ruby-collections-safenav-data-04 | collections-idioms | 2 | 2 | 2 | 1.00 | Data.define "immutable value class" (3.2); readers-only/`with` not named (same credit as baseline) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | delta | n | trust | target? |
|-----|---------:|-----------:|------:|--:|-------|---------|
| blocks-procs | 1.00 | 1.00 | +0.00 | 4 | ok | no |
| modules-mixins | 0.86 | 1.00 | +0.14 | 4 | ok | no |
| objects-methods | 0.75 | 1.00 | +0.25 | 4 | ok | **yes** |
| enumerable | 1.00 | 1.00 | +0.00 | 4 | ok | no |
| metaprogramming | 1.00 | 1.00 | +0.00 | 4 | ok | no |
| error-handling | 1.00 | 0.94 | -0.06 | 4 | ok | no |
| strings-symbols | 0.86 | 0.86 | +0.00 | 4 | ok | no |
| collections-idioms | 1.00 | 1.00 | +0.00 | 4 | ok | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: blocks-procs 8/8; modules-mixins 7/7; objects-methods 8/8;
enumerable 9/9; metaprogramming 6/6; error-handling 7.5/8; strings-symbols 6/7;
collections-idioms 9/9.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 60.5 / 62 = 97.6%
```

(Losses: frozen-02 −1.0, elserescue-04 −0.5.) Baseline 58 / 62 = 93.5%.

## Target-tag verdict

| tag | baseline | with-skill | pass (≥ 0.85)? |
|-----|---------:|-----------:|----------------|
| objects-methods | 0.75 | 1.00 | **PASS** |

**PASS.** The single target tag reaches the threshold. The skill is validated for
cat-jam-free @ alpha. Both baseline losses on the tag are gone: methodmissing-01
now states the `super` fallthrough for unhandled names, and visibility-04 now says
public is the default and that `self.foo` is allowed for any private method since
2.7, in the wording the skill prescribes.

Non-target drift (not a reason to edit the skill): modules-mixins 0.86 → 1.00
(ancestors-super-02 lost its inverted insertion-order claim) and error-handling
1.00 → 0.94 (elserescue-04 dropped the implicit-begin point the baseline had) are
single-sample resampling movement per HOW-TO-EVALUATE.md ("Only trust TARGET-tag
movement"). strings-symbols stays 0.86 on the same inverted frozen-02 claim
("interpolated string literals are also frozen") as the baseline — reproduced in
two samples but still at threshold and non-target; if a fresh baseline drops it
below 0.85, derive there, not here.
