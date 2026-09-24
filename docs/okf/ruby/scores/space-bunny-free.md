---
model_id: space-bunny-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-09-24
stack: ruby
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — space-bunny-free on ruby

> Valid ONLY for `space-bunny-free` @ `alpha`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| ruby-blocks-procvslambda-01 | blocks-procs | 3 | 2 | 2 | 1.00 | |
| ruby-blocks-yield-02 | blocks-procs | 2 | 2 | 2 | 1.00 | |
| ruby-blocks-ampblock-03 | blocks-procs | 2 | 2 | 2 | 1.00 | |
| ruby-blocks-create-04 | blocks-procs | 1 | 2 | 2 | 1.00 | |
| ruby-modules-includeextendprepend-01 | modules-mixins | 3 | 3 | 3 | 1.00 | prepend placement/precedence correct, but states the `super` direction backwards ("super in the class method can reach M" — it is M's method that supers into the class) |
| ruby-modules-ancestors-super-02 | modules-mixins | 2 | 2 | 2 | 1.00 | |
| ruby-modules-namespace-03 | modules-mixins | 1 | 2 | 2 | 1.00 | never mentions `::` or that modules can't be instantiated; namespace + `def self.x`/`module_function` present |
| ruby-modules-refinements-04 | modules-mixins | 1 | 2 | 2 | 1.00 | |
| ruby-objects-methodmissing-01 | objects-methods | 3 | 2 | 2 | 1.00 | includes the `super` fallthrough |
| ruby-objects-send-02 | objects-methods | 2 | 2 | 2 | 1.00 | |
| ruby-objects-attr-03 | objects-methods | 2 | 2 | 2 | 1.00 | |
| ruby-objects-visibility-04 | objects-methods | 1 | 2 | 2 | 1.00 | omits "public is the default" (minor) |
| ruby-enumerable-include-01 | enumerable | 3 | 2 | 2 | 1.00 | |
| ruby-enumerable-reduce-02 | enumerable | 2 | 2 | 2 | 1.00 | "block return is ignored" only implied via "avoids reassigning the accumulator" |
| ruby-enumerable-lazy-03 | enumerable | 2 | 2 | 2 | 1.00 | never contrasts with eager chains building intermediate arrays; on-demand + infinite-sequence points present |
| ruby-enumerable-comparable-04 | enumerable | 2 | 2 | 2 | 1.00 | |
| ruby-metaprogramming-singleton-01 | metaprogramming | 2 | 2 | 2 | 1.00 | |
| ruby-metaprogramming-ivar-02 | metaprogramming | 1 | 2 | 2 | 1.00 | |
| ruby-metaprogramming-definemethod-vs-mm-03 | metaprogramming | 2 | 2 | 2 | 1.00 | oddly claims define_method "has some dispatch overhead"; trade-off itself correct |
| ruby-metaprogramming-classnew-04 | metaprogramming | 1 | 2 | 2 | 1.00 | |
| ruby-error-standarderror-01 | error-handling | 3 | 3 | 2 | 0.67 | hierarchy + what `Exception` swallows (SystemExit/Interrupt) correct; never says "so rescue StandardError" or that custom exceptions should subclass StandardError — ends with "rescue specific exceptions or re-raise" |
| ruby-error-ensure-retry-02 | error-handling | 2 | 2 | 2 | 1.00 | infinite-loop risk of `retry` only implied ("until ... a limit is reached") |
| ruby-error-custom-03 | error-handling | 2 | 2 | 2 | 1.00 | |
| ruby-error-elserescue-04 | error-handling | 1 | 2 | 2 | 1.00 | |
| ruby-strings-symbols-01 | strings-symbols | 3 | 2 | 2 | 1.00 | |
| ruby-strings-frozen-02 | strings-symbols | 2 | 2 | 2 | 1.00 | no "since 3.0" version gate, concept present |
| ruby-strings-quotes-03 | strings-symbols | 1 | 2 | 2 | 1.00 | |
| ruby-strings-percent-04 | strings-symbols | 1 | 2 | 2 | 1.00 | |
| ruby-collections-hashdefault-01 | collections-idioms | 2 | 2 | 1.5 | 0.75 | half credit on point 1: explains `Hash.new(0)` for counters and no-insert, but never states the shared-mutable-default trap (why `Hash.new([])` is wrong) — the "why it matters" half of the question |
| ruby-collections-kwargs-02 | collections-idioms | 3 | 2 | 2 | 1.00 | |
| ruby-collections-splat-03 | collections-idioms | 2 | 2 | 2 | 1.00 | |
| ruby-collections-safenav-data-04 | collections-idioms | 2 | 2 | 2 | 1.00 | no `#with`; frozen/readers/3.2 present |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| blocks-procs | 1.00 | 4 | ok | omit (strong) |
| modules-mixins | 1.00 | 4 | ok | omit (strong) |
| objects-methods | 1.00 | 4 | ok | omit (strong) |
| enumerable | 1.00 | 4 | ok | omit (strong) |
| metaprogramming | 1.00 | 4 | ok | omit (strong) |
| error-handling | 0.88 | 4 | ok | omit (above threshold) |
| strings-symbols | 1.00 | 4 | ok | omit (strong) |
| collections-idioms | 0.94 | 4 | ok | omit (above threshold) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 60.5/62 = 98%
```

## Derivation targets

No tag scored below threshold (`< 0.75`). Every tag cleared 0.75, so **no
derived skill is written** for `space-bunny-free` on `ruby` — see
`rubric-guide.md` ("Tags with subscore ≥ 0.75 are omitted"). Weakest spots
observed (all still above threshold, kept here as scorecard notes only, not
skill content): `error-handling` (never lands the "rescue StandardError /
custom errors subclass StandardError" conclusion, 0.88), `collections-idioms`
(missing the shared-mutable-default trap behind `Hash.new(obj)`, 0.94). One
factual slip worth noting: the `super` direction under `prepend` is stated
backwards in `ruby-modules-includeextendprepend-01`.

**Contamination check:** no answer reads as a copy of the reference. Wording,
structure, and examples differ throughout (e.g. its own `Person`/`<=>` sample,
lists `SyntaxError` where the key lists `ScriptError`, adds `&.` short-circuits
only on nil not false). The near-ceiling score is consistent with a strong
model on an easy-to-medium language corpus, not with answer-key exposure.
