---
model_id: glm-5.3-flash
model_version: "5.3"
evaluated_via: aihubmix
evaluated_on: 2026-09-01
stack: ruby
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — glm-5.3-flash on ruby

> Valid ONLY for `glm-5.3-flash` @ `5.3`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| ruby-blocks-procvslambda-01 | blocks-procs | 3 | 2 | 2 | 1.00 | |
| ruby-blocks-yield-02 | blocks-procs | 2 | 2 | 2 | 1.00 | |
| ruby-blocks-ampblock-03 | blocks-procs | 2 | 2 | 2 | 1.00 | |
| ruby-blocks-create-04 | blocks-procs | 1 | 2 | 2 | 1.00 | |
| ruby-modules-includeextendprepend-01 | modules-mixins | 3 | 3 | 3 | 1.00 | |
| ruby-modules-ancestors-super-02 | modules-mixins | 2 | 2 | 2 | 1.00 | |
| ruby-modules-namespace-03 | modules-mixins | 1 | 2 | 2 | 1.00 | also names `extend self` |
| ruby-modules-refinements-04 | modules-mixins | 1 | 2 | 2 | 1.00 | |
| ruby-objects-methodmissing-01 | objects-methods | 3 | 2 | 1 | 0.50 | explains method_missing + respond_to_missing? well, but the `super` fallthrough is only attached to `respond_to_missing?` — never says method_missing itself must `super` for unhandled names (same gap as sibling models) |
| ruby-objects-send-02 | objects-methods | 2 | 2 | 2 | 1.00 | |
| ruby-objects-attr-03 | objects-methods | 2 | 2 | 2 | 1.00 | |
| ruby-objects-visibility-04 | objects-methods | 1 | 2 | 2 | 1.00 | correctly notes the 2.7 `self.foo` allowance |
| ruby-enumerable-include-01 | enumerable | 3 | 2 | 2 | 1.00 | |
| ruby-enumerable-reduce-02 | enumerable | 2 | 2 | 2 | 1.00 | |
| ruby-enumerable-lazy-03 | enumerable | 2 | 2 | 2 | 1.00 | |
| ruby-enumerable-comparable-04 | enumerable | 2 | 2 | 2 | 1.00 | |
| ruby-metaprogramming-singleton-01 | metaprogramming | 2 | 2 | 2 | 1.00 | |
| ruby-metaprogramming-ivar-02 | metaprogramming | 1 | 2 | 2 | 1.00 | |
| ruby-metaprogramming-definemethod-vs-mm-03 | metaprogramming | 2 | 2 | 2 | 1.00 | |
| ruby-metaprogramming-classnew-04 | metaprogramming | 1 | 2 | 2 | 1.00 | |
| ruby-error-standarderror-01 | error-handling | 3 | 3 | 2 | 0.67 | hierarchy + what `Exception` swallows (SignalException/SystemExit/NoMemoryError/ScriptError) are solid; never states the prescription — use `rescue StandardError` and make custom exceptions subclass `StandardError` (only offers "re-raise what you don't handle") |
| ruby-error-ensure-retry-02 | error-handling | 2 | 2 | 2 | 1.00 | bonus: `return` in ensure overrides result |
| ruby-error-custom-03 | error-handling | 2 | 2 | 2 | 1.00 | |
| ruby-error-elserescue-04 | error-handling | 1 | 2 | 2 | 1.00 | |
| ruby-strings-symbols-01 | strings-symbols | 3 | 2 | 2 | 1.00 | |
| ruby-strings-frozen-02 | strings-symbols | 2 | 2 | 2 | 1.00 | interpolation caveat present; does not date it to 3.0 but concept is there |
| ruby-strings-quotes-03 | strings-symbols | 1 | 2 | 2 | 1.00 | |
| ruby-strings-percent-04 | strings-symbols | 1 | 2 | 2 | 1.00 | |
| ruby-collections-hashdefault-01 | collections-idioms | 2 | 2 | 2 | 1.00 | |
| ruby-collections-kwargs-02 | collections-idioms | 3 | 2 | 2 | 1.00 | |
| ruby-collections-splat-03 | collections-idioms | 2 | 2 | 2 | 1.00 | |
| ruby-collections-safenav-data-04 | collections-idioms | 2 | 2 | 2 | 1.00 | one garbled sentence about `false` contexts (noise, not wrong); Data.define details correct incl. `with`, deconstruct |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| blocks-procs | 1.00 | 4 | ok | omit (strong) |
| modules-mixins | 1.00 | 4 | ok | omit (strong) |
| objects-methods | 0.81 | 4 | ok | omit (above threshold) |
| enumerable | 1.00 | 4 | ok | omit (strong) |
| metaprogramming | 1.00 | 4 | ok | omit (strong) |
| error-handling | 0.88 | 4 | ok | omit (above threshold) |
| strings-symbols | 1.00 | 4 | ok | omit (strong) |
| collections-idioms | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 59.5/62 = 96%
```

## Derivation targets

No tag scored below threshold (`< 0.75`). Every tag cleared 0.75, so **no
derived skill is written** for `glm-5.3-flash` on `ruby` — see
`rubric-guide.md` ("Tags with subscore ≥ 0.75 are omitted"). Only two points
were dropped across the corpus, both the same conceptual gap the other ruby
models show (scorecard notes only, not skill content): `objects-methods`
(no `super` fallthrough inside `method_missing`, 0.81) and `error-handling`
(no explicit "rescue StandardError / custom exceptions subclass StandardError"
prescription, 0.88).
