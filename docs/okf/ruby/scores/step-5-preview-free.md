---
model_id: step-5-preview-free
model_version: "5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-12
stack: ruby
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — step-5-preview-free on ruby

> Valid ONLY for `step-5-preview-free` @ `5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book answers in `answers/step-5-preview-free.md`. The answerer was
`ocode run -m opencode-go/step-5-preview-free -effort med` in an empty scratch
directory with only the `_prompts/ruby.md` sheet attached; the session sqlite
shows exactly 1 user + 1 assistant message and 0 tool calls. A first attempt
failed at the provider (429) with no output and was re-run.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| ruby-blocks-procvslambda-01 | blocks-procs | 3 | 2 | 2 | 1.00 |  |
| ruby-blocks-yield-02 | blocks-procs | 2 | 2 | 2 | 1.00 |  |
| ruby-blocks-ampblock-03 | blocks-procs | 2 | 2 | 2 | 1.00 |  |
| ruby-blocks-create-04 | blocks-procs | 1 | 2 | 2 | 1.00 |  |
| ruby-modules-includeextendprepend-01 | modules-mixins | 3 | 3 | 3 | 1.00 | garbled phrase ("visible descendant superfic") before a correct `ancestors` example |
| ruby-modules-ancestors-super-02 | modules-mixins | 2 | 2 | 2 | 1.00 | lookup order stated correctly up front; later "included modules sit ahead of the class (but behind classes that prepend)" is ambiguous wording, not rubric-relevant |
| ruby-modules-namespace-03 | modules-mixins | 1 | 2 | 2 | 1.00 |  |
| ruby-modules-refinements-04 | modules-mixins | 1 | 2 | 2 | 1.00 | snippet `refine String { ... }` binds the brace block to `String`, not `refine` (needs `do…end` or parentheses); tangential |
| ruby-objects-methodmissing-01 | objects-methods | 3 | 2 | 2 | 1.00 | never mentions calling `super` for unhandled names; both asked concepts otherwise fully present |
| ruby-objects-send-02 | objects-methods | 2 | 2 | 2 | 1.00 |  |
| ruby-objects-attr-03 | objects-methods | 2 | 2 | 2 | 1.00 |  |
| ruby-objects-visibility-04 | objects-methods | 1 | 2 | 1 | 0.50 | (judgment) private point withheld: claims `self.helper` is illegal for private methods and dates the setter exception to 2.7 — inverted (2.7 allowed `self.` for every private call; `self.x=` was always allowed); public-is-default not stated. Protected point awarded |
| ruby-enumerable-include-01 | enumerable | 3 | 2 | 2 | 1.00 |  |
| ruby-enumerable-reduce-02 | enumerable | 2 | 2 | 2 | 1.00 |  |
| ruby-enumerable-lazy-03 | enumerable | 2 | 2 | 2 | 1.00 |  |
| ruby-enumerable-comparable-04 | enumerable | 2 | 2 | 2 | 1.00 |  |
| ruby-metaprogramming-singleton-01 | metaprogramming | 2 | 2 | 2 | 1.00 |  |
| ruby-metaprogramming-ivar-02 | metaprogramming | 1 | 2 | 2 | 1.00 |  |
| ruby-metaprogramming-definemethod-vs-mm-03 | metaprogramming | 2 | 2 | 2 | 1.00 |  |
| ruby-metaprogramming-classnew-04 | metaprogramming | 1 | 2 | 2 | 1.00 |  |
| ruby-error-standarderror-01 | error-handling | 3 | 3 | 3 | 1.00 | opening sentence correctly says bare `rescue => e` is StandardError; a later illustrative snippet writes `rescue => e; log e; end` as the wrapper that "eats Ctrl-C" (should be `rescue Exception => e`) — treated as a slip contradicted by the answer's own statement, not a belief |
| ruby-error-ensure-retry-02 | error-handling | 2 | 2 | 2 | 1.00 | aside "its value is discarded unless the block raises" is wrong (ensure's value is always discarded); not rubric-relevant |
| ruby-error-custom-03 | error-handling | 2 | 2 | 2 | 1.00 |  |
| ruby-error-elserescue-04 | error-handling | 1 | 2 | 2 | 1.00 |  |
| ruby-strings-symbols-01 | strings-symbols | 3 | 2 | 2 | 1.00 |  |
| ruby-strings-frozen-02 | strings-symbols | 2 | 2 | 1 | 0.50 | gets the pragma; misses the caveat: says the interpolated string is a new object each time but "frozen" / "the literal itself is frozen" — since 3.0 interpolated literals are NOT frozen under the pragma |
| ruby-strings-quotes-03 | strings-symbols | 1 | 2 | 2 | 1.00 |  |
| ruby-strings-percent-04 | strings-symbols | 1 | 2 | 2 | 1.00 | both points present; the middle of the answer is visibly garbled self-correction ("boolean-false style… sibling flags? No —") before landing on a correct statement |
| ruby-collections-hashdefault-01 | collections-idioms | 2 | 2 | 2 | 1.00 | both points present; trailing "Rule:" sentence degenerates into nonsense (`compare_by_identity irrelevant`, a `.tap` idiom) |
| ruby-collections-kwargs-02 | collections-idioms | 3 | 2 | 1 | 0.50 | (judgment) separation point withheld: asserts "passing keywords to `*args`-only methods raises" — false, 3.0 still converts keywords to a positional Hash for a method that accepts no keywords; also trails off into "you can now call m(*arr) then that hash? No —". `**hash` / `**opts` point awarded |
| ruby-collections-splat-03 | collections-idioms | 2 | 2 | 1 | 0.50 | (judgment) params point withheld: asserts "you can't combine `**opts` with explicit named keywords in the same list — one or the other" — false (`def m(a:, **rest)` is idiomatic); also offers `first, **rest = {a: 1}` as destructuring, which is a SyntaxError (that is pattern-matching syntax). Call-site spread point awarded |
| ruby-collections-safenav-data-04 | collections-idioms | 2 | 2 | 2 | 1.00 | `&.` and `Data.define` both correct; some rambling ("hash&.fetch…? — yes") |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| blocks-procs | 1.00 | 4 | ok | omit (strong) |
| modules-mixins | 1.00 | 4 | ok | omit (strong) |
| objects-methods | 0.94 | 4 | ok | omit (strong) |
| enumerable | 1.00 | 4 | ok | omit (strong) |
| metaprogramming | 1.00 | 4 | ok | omit (strong) |
| error-handling | 1.00 | 4 | ok | omit (strong) |
| strings-symbols | 0.86 | 4 | ok | omit (at threshold: 6/7 = 0.857) |
| collections-idioms | 0.72 | 4 | ok | **derive** |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 58 / 62 = 93.5%
```

Grading notes: every lost point is a specific, confidently stated false claim
about the construct being asked about, not a missing concept. The weak area is
argument passing — the 3.0 keyword/positional separation and the `*`/`**`
definition rules — where the answers were long and then drifted into
contradictory or syntactically invalid Ruby. Several otherwise-correct answers
also carried visibly garbled, self-correcting prose mid-paragraph (percent
literals, Hash default, kwargs), which reads as the model thinking out loud in
its final output rather than copying anything: no sentence matches the
reference answers verbatim, and the three off-rubric factual slips confirm no
key access.

## Derivation targets

Tags below threshold (`< 0.85`): **collections-idioms** → feed into
`derived/ruby.step-5-preview-free.SKILL.md`.
