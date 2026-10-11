---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-05
stack: ruby
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — longcat-2.5-preview-free on ruby (2026-10-05 full re-run)

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

Baseline rerun 2026-10-05 (closed-book answers in
`answers/longcat-2.5-preview-free.rerun-2026-10-05.md`). An earlier 2026-09-28
scorecard exists beside this one and was not consulted.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| ruby-blocks-procvslambda-01 | blocks-procs | 3 | 2 | 2 | 1.00 |  |
| ruby-blocks-yield-02 | blocks-procs | 2 | 2 | 2 | 1.00 |  |
| ruby-blocks-ampblock-03 | blocks-procs | 2 | 2 | 2 | 1.00 |  |
| ruby-blocks-create-04 | blocks-procs | 1 | 2 | 2 | 1.00 |  |
| ruby-modules-includeextendprepend-01 | modules-mixins | 3 | 3 | 3 | 1.00 |  |
| ruby-modules-ancestors-super-02 | modules-mixins | 2 | 2 | 2 | 1.00 |  |
| ruby-modules-namespace-03 | modules-mixins | 1 | 2 | 2 | 1.00 |  |
| ruby-modules-refinements-04 | modules-mixins | 1 | 2 | 2 | 1.00 |  |
| ruby-objects-methodmissing-01 | objects-methods | 3 | 2 | 2 | 1.00 |  |
| ruby-objects-send-02 | objects-methods | 2 | 2 | 2 | 1.00 |  |
| ruby-objects-attr-03 | objects-methods | 2 | 2 | 2 | 1.00 |  |
| ruby-objects-visibility-04 | objects-methods | 1 | 2 | 2 | 1.00 | both points present; side note on `self.` receiver for private is imprecise (allowed for all private calls since 2.7) |
| ruby-enumerable-include-01 | enumerable | 3 | 2 | 2 | 1.00 |  |
| ruby-enumerable-reduce-02 | enumerable | 2 | 2 | 2 | 1.00 |  |
| ruby-enumerable-lazy-03 | enumerable | 2 | 2 | 2 | 1.00 |  |
| ruby-enumerable-comparable-04 | enumerable | 2 | 2 | 2 | 1.00 |  |
| ruby-metaprogramming-singleton-01 | metaprogramming | 2 | 2 | 2 | 1.00 |  |
| ruby-metaprogramming-ivar-02 | metaprogramming | 1 | 2 | 2 | 1.00 |  |
| ruby-metaprogramming-definemethod-vs-mm-03 | metaprogramming | 2 | 2 | 2 | 1.00 |  |
| ruby-metaprogramming-classnew-04 | metaprogramming | 1 | 2 | 2 | 1.00 |  |
| ruby-error-standarderror-01 | error-handling | 3 | 3 | 3 | 1.00 |  |
| ruby-error-ensure-retry-02 | error-handling | 2 | 2 | 2 | 1.00 |  |
| ruby-error-custom-03 | error-handling | 2 | 2 | 2 | 1.00 |  |
| ruby-error-elserescue-04 | error-handling | 1 | 2 | 2 | 1.00 | both points present; side claim that `{}` blocks accept rescue is wrong (only do...end), not rubric-relevant |
| ruby-strings-symbols-01 | strings-symbols | 3 | 2 | 2 | 1.00 |  |
| ruby-strings-frozen-02 | strings-symbols | 2 | 2 | 2 | 1.00 | both points present; side claim that 3.4 freezes literals by default is wrong (3.4 only warns on mutation, "chilled" strings), not rubric-relevant |
| ruby-strings-quotes-03 | strings-symbols | 1 | 2 | 2 | 1.00 |  |
| ruby-strings-percent-04 | strings-symbols | 1 | 2 | 2 | 1.00 |  |
| ruby-collections-hashdefault-01 | collections-idioms | 2 | 2 | 2 | 1.00 |  |
| ruby-collections-kwargs-02 | collections-idioms | 3 | 2 | 2 | 1.00 |  |
| ruby-collections-splat-03 | collections-idioms | 2 | 2 | 2 | 1.00 |  |
| ruby-collections-safenav-data-04 | collections-idioms | 2 | 2 | 2 | 1.00 |  |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| blocks-procs | 1.00 | 4 | ok | omit (strong) |
| modules-mixins | 1.00 | 4 | ok | omit (strong) |
| objects-methods | 1.00 | 4 | ok | omit (strong) |
| enumerable | 1.00 | 4 | ok | omit (strong) |
| metaprogramming | 1.00 | 4 | ok | omit (strong) |
| error-handling | 1.00 | 4 | ok | omit (strong) |
| strings-symbols | 1.00 | 4 | ok | omit (strong) |
| collections-idioms | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 100%
```

Caveat: a flat 100% is a contamination red flag per README. The answers read as
genuine own-knowledge prose, but three off-rubric factual slips (see notes) suggest
no key access. Ruby language fundamentals are also widely memorized.

## Derivation targets

Tags below threshold (`< 0.75`): **none** — no derived skill warranted.
