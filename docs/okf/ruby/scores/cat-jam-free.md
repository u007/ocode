---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: ruby
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — cat-jam-free on ruby

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book answers in `answers/cat-jam-free.md`. The answerer was
`ocode run -m opencode-go/cat-jam-free -effort med` in an empty scratch
directory with only the `_prompts/ruby.md` sheet attached; the session sqlite
shows exactly 1 user + 1 assistant message and 0 tool calls. Headless runs
inject ocode's bundled skill-catalogue digest into the system prompt.
`cat-jam-free` is an opencode-go free/alpha model with no published version
(`"alpha"` is a placeholder); it self-identifies as "CatJam, developed by
LitterBox AI".

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| ruby-blocks-procvslambda-01 | blocks-procs | 3 | 2 | 2 | 1.00 |  |
| ruby-blocks-yield-02 | blocks-procs | 2 | 2 | 2 | 1.00 |  |
| ruby-blocks-ampblock-03 | blocks-procs | 2 | 2 | 2 | 1.00 |  |
| ruby-blocks-create-04 | blocks-procs | 1 | 2 | 2 | 1.00 |  |
| ruby-modules-includeextendprepend-01 | modules-mixins | 3 | 3 | 3 | 1.00 |  |
| ruby-modules-ancestors-super-02 | modules-mixins | 2 | 2 | 1 | 0.50 | (judgment) lookup-order point withheld: never lays out prepend → self → include → superclass, and its one ordering claim ("included modules appear in that ancestor chain in insertion order") is backwards — the last-included module is searched first. "Next ancestor" super point awarded. Aside: "`super(...)` passes explicit arguments" conflates `...` forwarding with an explicit arg list; not rubric-relevant |
| ruby-modules-namespace-03 | modules-mixins | 1 | 2 | 2 | 1.00 | `::` reference syntax not named; namespace concept present |
| ruby-modules-refinements-04 | modules-mixins | 1 | 2 | 2 | 1.00 |  |
| ruby-objects-methodmissing-01 | objects-methods | 3 | 2 | 1 | 0.50 | (judgment) point 1 withheld: the rubric point is "handle dynamically **and super for the rest**", and the rubric's partial names the omitted super fallthrough as a half-credit failure; the answer never says to call `super` for unhandled names. respond_to_missing? point awarded |
| ruby-objects-send-02 | objects-methods | 2 | 2 | 2 | 1.00 |  |
| ruby-objects-attr-03 | objects-methods | 2 | 2 | 2 | 1.00 |  |
| ruby-objects-visibility-04 | objects-methods | 1 | 2 | 1 | 0.50 | (judgment) private point withheld: says private methods cannot be called "with an explicit receiver such as self" — stale since 2.7 (`self.foo` is allowed for any private call); public-is-default not stated. Protected point awarded (phrased as receiver type rather than caller context, close enough) |
| ruby-enumerable-include-01 | enumerable | 3 | 2 | 2 | 1.00 |  |
| ruby-enumerable-reduce-02 | enumerable | 2 | 2 | 2 | 1.00 | (judgment) never says outright that the block's return becomes the next accumulator; awarded because the contrast "returns it without manual accumulator reassignment" carries the mechanic |
| ruby-enumerable-lazy-03 | enumerable | 2 | 2 | 2 | 1.00 |  |
| ruby-enumerable-comparable-04 | enumerable | 2 | 2 | 2 | 1.00 |  |
| ruby-metaprogramming-singleton-01 | metaprogramming | 2 | 2 | 2 | 1.00 |  |
| ruby-metaprogramming-ivar-02 | metaprogramming | 1 | 2 | 2 | 1.00 |  |
| ruby-metaprogramming-definemethod-vs-mm-03 | metaprogramming | 2 | 2 | 2 | 1.00 | respond_to_missing? not named on the method_missing side; trade-off clearly drawn |
| ruby-metaprogramming-classnew-04 | metaprogramming | 1 | 2 | 2 | 1.00 |  |
| ruby-error-standarderror-01 | error-handling | 3 | 3 | 3 | 1.00 |  |
| ruby-error-ensure-retry-02 | error-handling | 2 | 2 | 2 | 1.00 |  |
| ruby-error-custom-03 | error-handling | 2 | 2 | 2 | 1.00 | (judgment) raise forms point awarded on a correct list of forms (class, class+message, instance, re-raise with message/backtrace); omits that `raise "str"` is RuntimeError and that bare `raise` re-raises the current exception |
| ruby-error-elserescue-04 | error-handling | 1 | 2 | 2 | 1.00 |  |
| ruby-strings-symbols-01 | strings-symbols | 3 | 2 | 2 | 1.00 |  |
| ruby-strings-frozen-02 | strings-symbols | 2 | 2 | 1 | 0.50 | caveat point withheld: claims "interpolated strings are also frozen" — inverted; since 3.0 interpolated literals are NOT frozen under the pragma |
| ruby-strings-quotes-03 | strings-symbols | 1 | 2 | 2 | 1.00 |  |
| ruby-strings-percent-04 | strings-symbols | 1 | 2 | 2 | 1.00 |  |
| ruby-collections-hashdefault-01 | collections-idioms | 2 | 2 | 2 | 1.00 |  |
| ruby-collections-kwargs-02 | collections-idioms | 3 | 2 | 2 | 1.00 | "or vice versa" is imprecise (keywords to a no-keyword method still become a positional Hash in 3.0), but the reference answer states the same; `def m(**opts)` not mentioned, question asks only how to pass |
| ruby-collections-splat-03 | collections-idioms | 2 | 2 | 2 | 1.00 |  |
| ruby-collections-safenav-data-04 | collections-idioms | 2 | 2 | 2 | 1.00 |  |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| blocks-procs | 1.00 | 4 | ok | omit (strong) |
| modules-mixins | 0.86 | 4 | ok | omit (at threshold: 6/7 = 0.857) |
| objects-methods | 0.75 | 4 | ok | **derive** |
| enumerable | 1.00 | 4 | ok | omit (strong) |
| metaprogramming | 1.00 | 4 | ok | omit (strong) |
| error-handling | 1.00 | 4 | ok | omit (strong) |
| strings-symbols | 0.86 | 4 | ok | omit (at threshold: 6/7 = 0.857) |
| collections-idioms | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 58 / 62 = 93.5%
```

Grading notes: answers are short and mostly accurate. Lost points are of two
kinds: an omitted half of a two-part rubric point (no `super` fallthrough in
`method_missing`; no lookup ordering for `super`), and confidently stated
claims that are stale or inverted (private methods cannot take `self.`;
interpolated strings are frozen under the pragma; included modules searched
in insertion order). No sentence matches the reference answers verbatim.

## Derivation targets

Tags below threshold (`< 0.85`): **objects-methods** → feed into
`derived/ruby.cat-jam-free.SKILL.md`.
