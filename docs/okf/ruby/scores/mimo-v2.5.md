---
model_id: mimo-v2.5
model_version: "2.5"
evaluated_via: opencode-go
evaluated_on: 2026-08-17
stack: ruby
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — mimo-v2.5 on ruby

> Valid ONLY for `mimo-v2.5` @ `2.5`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| ruby-blocks-procvslambda-01 | blocks-procs | 3 | 2 | 2 | 1.00 | |
| ruby-blocks-yield-02 | blocks-procs | 2 | 2 | 2 | 1.00 | |
| ruby-blocks-ampblock-03 | blocks-procs | 2 | 2 | 2 | 1.00 | |
| ruby-blocks-create-04 | blocks-procs | 1 | 2 | 1 | 0.50 | shows lambda creation only, no `proc {}`/`Proc.new {}` |
| ruby-modules-includeextendprepend-01 | modules-mixins | 3 | 3 | 3 | 1.00 | |
| ruby-modules-ancestors-super-02 | modules-mixins | 2 | 2 | 2 | 1.00 | states chain order as class-then-prepend (reversed) but chain/super mechanics correct |
| ruby-modules-namespace-03 | modules-mixins | 1 | 2 | 2 | 1.00 | |
| ruby-modules-refinements-04 | modules-mixins | 1 | 2 | 2 | 1.00 | |
| ruby-objects-methodmissing-01 | objects-methods | 3 | 2 | 1 | 0.50 | explains method_missing + respond_to_missing? but omits calling `super` for unhandled names |
| ruby-objects-send-02 | objects-methods | 2 | 2 | 2 | 1.00 | |
| ruby-objects-attr-03 | objects-methods | 2 | 2 | 2 | 1.00 | |
| ruby-objects-visibility-04 | objects-methods | 1 | 2 | 2 | 1.00 | |
| ruby-enumerable-include-01 | enumerable | 3 | 2 | 2 | 1.00 | |
| ruby-enumerable-reduce-02 | enumerable | 2 | 2 | 2 | 1.00 | |
| ruby-enumerable-lazy-03 | enumerable | 2 | 2 | 2 | 1.00 | |
| ruby-enumerable-comparable-04 | enumerable | 2 | 2 | 2 | 1.00 | |
| ruby-metaprogramming-singleton-01 | metaprogramming | 2 | 2 | 2 | 1.00 | |
| ruby-metaprogramming-ivar-02 | metaprogramming | 1 | 2 | 2 | 1.00 | |
| ruby-metaprogramming-definemethod-vs-mm-03 | metaprogramming | 2 | 2 | 2 | 1.00 | |
| ruby-metaprogramming-classnew-04 | metaprogramming | 1 | 2 | 2 | 1.00 | |
| ruby-error-standarderror-01 | error-handling | 3 | 3 | 2 | 0.67 | covers StandardError-vs-Exception and what leaks through (SystemExit/SignalException); never states custom exceptions should subclass StandardError |
| ruby-error-ensure-retry-02 | error-handling | 2 | 2 | 2 | 1.00 | |
| ruby-error-custom-03 | error-handling | 2 | 2 | 2 | 1.00 | |
| ruby-error-elserescue-04 | error-handling | 1 | 2 | 1 | 0.50 | explains `else` correctly but never mentions the implicit `begin` on a method/def body |
| ruby-strings-symbols-01 | strings-symbols | 3 | 2 | 2 | 1.00 | |
| ruby-strings-frozen-02 | strings-symbols | 2 | 2 | 2 | 1.00 | |
| ruby-strings-quotes-03 | strings-symbols | 1 | 2 | 2 | 1.00 | |
| ruby-strings-percent-04 | strings-symbols | 1 | 2 | 2 | 1.00 | |
| ruby-collections-hashdefault-01 | collections-idioms | 2 | 2 | 2 | 1.00 | |
| ruby-collections-kwargs-02 | collections-idioms | 3 | 2 | 1 | 0.50 | gets the pre/post-3.0 separation but never mentions collecting arbitrary keywords with `def m(**opts)` |
| ruby-collections-splat-03 | collections-idioms | 2 | 2 | 2 | 1.00 | |
| ruby-collections-safenav-data-04 | collections-idioms | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| blocks-procs | 0.94 | 4 | ok | omit (strong) |
| modules-mixins | 1.00 | 4 | ok | omit (strong) |
| objects-methods | 0.81 | 4 | ok | omit (above threshold) |
| enumerable | 1.00 | 4 | ok | omit (strong) |
| metaprogramming | 1.00 | 4 | ok | omit (strong) |
| error-handling | 0.81 | 4 | ok | omit (above threshold) |
| strings-symbols | 1.00 | 4 | ok | omit (strong) |
| collections-idioms | 0.83 | 4 | ok | omit (above threshold) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 57.0/62 = 92%
```

## Derivation targets

No tag scored below threshold (`< 0.75`). Every tag cleared 0.75, so **no
derived skill is written** for `mimo-v2.5` on `ruby` — see
`rubric-guide.md` ("Tags with subscore ≥ 0.75 are omitted"). Weakest spots
observed (all still above threshold, kept here as scorecard notes only, not
skill content): `objects-methods` (missing `super` fallthrough in
`method_missing`, 0.81), `error-handling` (missing custom-subclass-StandardError
detail and the implicit-`begin` fact, 0.81), `collections-idioms` (missing
`**opts` collection side of the keyword-argument story, 0.83).
