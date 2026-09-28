---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: ruby
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — longcat-2.5-preview-free on ruby

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump
> invalidates this scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| ruby-blocks-procvslambda-01 | blocks-procs | 3 | 2 | 2 | 1.00 | |
| ruby-blocks-yield-02 | blocks-procs | 2 | 2 | 2 | 1.00 | |
| ruby-blocks-ampblock-03 | blocks-procs | 2 | 2 | 2 | 1.00 | |
| ruby-blocks-create-04 | blocks-procs | 1 | 2 | 1.5 | 0.75 | half credit on point 1: lists `->`/`lambda` and `proc`/`Proc.new` correctly, but also claims `Proc.new { }` creates a lambda ("in Ruby < 1.9, or with lambda keyword") — wrong, `Proc.new` never yields a lambda. Invocation `.call`/`.()`/`.yield` present; no `[]` form |
| ruby-modules-includeextendprepend-01 | modules-mixins | 3 | 3 | 3 | 1.00 | prepend's ability to `super` into the class only implied ("wrapping existing methods") |
| ruby-modules-ancestors-super-02 | modules-mixins | 2 | 2 | 2 | 1.00 | |
| ruby-modules-namespace-03 | modules-mixins | 1 | 2 | 2 | 1.00 | no explicit `::` / can't-instantiate; nested-module example + `module_function` / `def self.x` present |
| ruby-modules-refinements-04 | modules-mixins | 1 | 2 | 2 | 1.00 | |
| ruby-objects-methodmissing-01 | objects-methods | 3 | 2 | 2 | 1.00 | includes the `super` fallthrough |
| ruby-objects-send-02 | objects-methods | 2 | 2 | 2 | 1.00 | |
| ruby-objects-attr-03 | objects-methods | 2 | 2 | 2 | 1.00 | |
| ruby-objects-visibility-04 | objects-methods | 1 | 2 | 2 | 1.00 | frames `self.` as allowed only for setters "in 2.7+" (2.7 actually allows any `self.` receiver on private); omits "public is default" |
| ruby-enumerable-include-01 | enumerable | 3 | 2 | 2 | 1.00 | |
| ruby-enumerable-reduce-02 | enumerable | 2 | 2 | 2 | 1.00 | |
| ruby-enumerable-lazy-03 | enumerable | 2 | 2 | 2 | 1.00 | |
| ruby-enumerable-comparable-04 | enumerable | 2 | 2 | 2 | 1.00 | |
| ruby-metaprogramming-singleton-01 | metaprogramming | 2 | 2 | 2 | 1.00 | |
| ruby-metaprogramming-ivar-02 | metaprogramming | 1 | 2 | 2 | 1.00 | |
| ruby-metaprogramming-definemethod-vs-mm-03 | metaprogramming | 2 | 2 | 2 | 1.00 | doesn't name `respond_to_missing?`, but "doesn't appear in `methods`" covers the introspection cost |
| ruby-metaprogramming-classnew-04 | metaprogramming | 1 | 2 | 2 | 1.00 | |
| ruby-error-standarderror-01 | error-handling | 3 | 3 | 3 | 1.00 | lands "StandardError is what you rescue"; doesn't add "custom errors subclass StandardError" here (does in -03) |
| ruby-error-ensure-retry-02 | error-handling | 2 | 2 | 2 | 1.00 | |
| ruby-error-custom-03 | error-handling | 2 | 2 | 2 | 1.00 | |
| ruby-error-elserescue-04 | error-handling | 1 | 2 | 1 | 0.50 | `else` = success-path-only correct; never addresses the implicit `begin` in a `def`/method body (the first half of the question) — answers only in `begin/rescue/else/ensure` terms |
| ruby-strings-symbols-01 | strings-symbols | 3 | 2 | 2 | 1.00 | |
| ruby-strings-frozen-02 | strings-symbols | 2 | 2 | 2 | 1.00 | no "since 3.0" version gate; concept present |
| ruby-strings-quotes-03 | strings-symbols | 1 | 2 | 2 | 1.00 | |
| ruby-strings-percent-04 | strings-symbols | 1 | 2 | 2 | 1.00 | |
| ruby-collections-hashdefault-01 | collections-idioms | 2 | 2 | 1.5 | 0.75 | half credit on point 1: explains no-insert behaviour, but never states the shared-mutable-default trap (why `Hash.new([])` is wrong) |
| ruby-collections-kwargs-02 | collections-idioms | 3 | 2 | 2 | 1.00 | `def m(**opts)` collection form not restated here (covered in -03) |
| ruby-collections-splat-03 | collections-idioms | 2 | 2 | 2 | 1.00 | closing sentence ("double-splat also captures keywords ... when the method doesn't accept `**kwargs`") is muddled/incorrect; core gather/spread points present |
| ruby-collections-safenav-data-04 | collections-idioms | 2 | 2 | 2 | 1.00 | no `#with`; frozen/readers/3.2 present; unsupported "better performance than Struct" claim |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| blocks-procs | 0.97 | 4 | ok | omit (strong) |
| modules-mixins | 1.00 | 4 | ok | omit (strong) |
| objects-methods | 1.00 | 4 | ok | omit (strong) |
| enumerable | 1.00 | 4 | ok | omit (strong) |
| metaprogramming | 1.00 | 4 | ok | omit (strong) |
| error-handling | 0.94 | 4 | ok | omit (above threshold) |
| strings-symbols | 1.00 | 4 | ok | omit (strong) |
| collections-idioms | 0.94 | 4 | ok | omit (above threshold) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 60.75/62 = 98%
```

## Derivation targets

No tag scored below threshold (`< 0.75`), so **no derived skill is written** for
`longcat-2.5-preview-free` on `ruby` — see `rubric-guide.md` ("Tags with
subscore ≥ 0.75 are omitted"). Weakest spots observed (all above threshold,
scorecard notes only, not skill content): `error-handling` (skipped the
implicit-`begin`-in-`def` half of the else/rescue question, 0.94),
`collections-idioms` (missing the shared-mutable-default trap behind
`Hash.new(obj)`, 0.94), `blocks-procs` (false claim that `Proc.new` can create a
lambda, 0.97).

**Contamination check:** no answer reads as a copy of the reference. Wording
and structure differ throughout; examples are its own (`Person`/`age` for
Comparable and protected, `first(10)` vs the key's `first(5)` on the common
`Float::INFINITY` lazy idiom), and it contains errors the key would have
prevented (`Proc.new` as a lambda constructor, the private-`self.` "setters in
2.7+" framing, the muddled `**` closing sentence). Near-ceiling score is
consistent with a strong model on an easy-to-medium language corpus, not
answer-key exposure. Verdict: clean.
