---
model_id: tencent/hy3
model_version: "3.0"
evaluated_via: novita
evaluated_on: 2026-07-11
stack: ruby
stack_corpus_rev: 1
threshold: 0.75
---

<!-- Graded independently (grader: Claude Opus 4.8), CLOSED-BOOK answers.
     Filename flattens model_id "tencent/hy3" → tencent__hy3.md. -->

# Scorecard — tencent/hy3 on ruby

> Valid ONLY for `tencent/hy3` @ `3.0`. A version bump invalidates this
> scorecard — re-benchmark.
>
> Grading: independent grader (Claude Opus 4.8), answers produced closed-book.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| ruby-blocks-procvslambda-01 | blocks-procs | 3 | 2 | 2 | 1.00 | return + arity both correct |
| ruby-blocks-yield-02 | blocks-procs | 2 | 2 | 2 | 1.00 | yield + block_given?/LocalJumpError correct |
| ruby-blocks-ampblock-03 | blocks-procs | 2 | 2 | 2 | 1.00 | &block capture + Symbol#to_proc correct |
| ruby-blocks-create-04 | blocks-procs | 1 | 2 | 2 | 1.00 | creation + .call/.()/[] forms correct |
| ruby-modules-includeextendprepend-01 | modules-mixins | 3 | 3 | 3 | 1.00 | include/prepend/extend positions + override all correct |
| ruby-modules-ancestors-super-02 | modules-mixins | 2 | 2 | 2 | 1.00 | ancestor-chain walk + super→next ancestor (module) correct |
| ruby-modules-namespace-03 | modules-mixins | 1 | 2 | 2 | 1.00 | namespace/:: + module_function/self. correct |
| ruby-modules-refinements-04 | modules-mixins | 1 | 2 | 2 | 1.00 | global monkey-patch vs refine/using lexical scope correct |
| ruby-objects-methodmissing-01 | objects-methods | 3 | 2 | 2 | 1.00 | method_missing + respond_to_missing? correct (omits the `super` fallthrough note, but that is a sub-clause) |
| ruby-objects-send-02 | objects-methods | 2 | 2 | 2 | 1.00 | dynamic dispatch + send bypasses/public_send enforces visibility correct |
| ruby-objects-attr-03 | objects-methods | 2 | 2 | 2 | 1.00 | attr_* trio + define_method correct; BUT fabricates "second boolean arg" for attr_* (no such feature) — did not cost a point |
| ruby-objects-visibility-04 | objects-methods | 1 | 2 | 2 | 1.00 | private=no explicit receiver, protected=same-class receiver correct (says "not even self.foo", the traditional framing) |
| ruby-enumerable-include-01 | enumerable | 3 | 2 | 2 | 1.00 | define each + include Enumerable → full API correct |
| ruby-enumerable-reduce-02 | enumerable | 2 | 2 | 2 | 1.00 | reduce fold + each_with_object (memo returned/return ignored) correct |
| ruby-enumerable-lazy-03 | enumerable | 2 | 2 | 2 | 1.00 | eager intermediates vs on-demand + infinite/early-term correct |
| ruby-enumerable-comparable-04 | enumerable | 2 | 2 | 2 | 1.00 | <=> + include Comparable correct; minor: says clamp added 2.7 (actually 2.4) — did not cost a point |
| ruby-metaprogramming-singleton-01 | metaprogramming | 2 | 2 | 2 | 1.00 | eigenclass + class << self → class methods correct |
| ruby-metaprogramming-ivar-02 | metaprogramming | 1 | 2 | 2 | 1.00 | by-name @ivar get/set + bypasses accessors/encapsulation correct |
| ruby-metaprogramming-definemethod-vs-mm-03 | metaprogramming | 2 | 2 | 2 | 1.00 | define_method (upfront/introspectable) vs method_missing (lazy/invisible) trade-off correct |
| ruby-metaprogramming-classnew-04 | metaprogramming | 1 | 2 | 2 | 1.00 | anonymous class + names-on-constant-assignment correct |
| ruby-error-standarderror-01 | error-handling | 3 | 3 | 2 | 0.67 | points 1+2 correct (bare rescue=StandardError; Exception swallows SystemExit/SignalException, breaks Ctrl-C/exit). MISSED point 3: never states the positive prescription — use `rescue StandardError` and subclass custom errors from StandardError |
| ruby-error-ensure-retry-02 | error-handling | 2 | 2 | 2 | 1.00 | ensure always runs + retry re-runs begin/needs bound correct |
| ruby-error-custom-03 | error-handling | 2 | 2 | 2 | 1.00 | subclass StandardError + all raise forms (bare/string=RuntimeError/Class,msg/obj) correct |
| ruby-error-elserescue-04 | error-handling | 1 | 2 | 2 | 1.00 | implicit begin in method body + else=success-path correct |
| ruby-strings-symbols-01 | strings-symbols | 3 | 2 | 2 | 1.00 | mutable String vs interned/immutable Symbol correct (GC-since-2.2 note accurate) |
| ruby-strings-frozen-02 | strings-symbols | 2 | 2 | 2 | 1.00 | magic comment freezes literals + interpolated-not-frozen (3.0) correct |
| ruby-strings-quotes-03 | strings-symbols | 1 | 2 | 2 | 1.00 | double=interp/escapes, single=literal correct |
| ruby-strings-percent-04 | strings-symbols | 1 | 2 | 2 | 1.00 | %w=strings, %i=symbols correct |
| ruby-collections-hashdefault-01 | collections-idioms | 2 | 2 | 2 | 1.00 | shared default value vs per-key block (h[k]=[] stores) correct |
| ruby-collections-kwargs-02 | collections-idioms | 3 | 2 | 2 | 1.00 | 2.x auto-convert vs 3.0 separation + **hash/**opts correct |
| ruby-collections-splat-03 | collections-idioms | 2 | 2 | 2 | 1.00 | gather-in-params vs spread-at-call for */** correct |
| ruby-collections-safenav-data-04 | collections-idioms | 2 | 2 | 2 | 1.00 | &. nil-guard + Data.define (3.2) immutable value object correct; BUT wrongly adds `&.` short-circuits on false too (it guards nil only) and omits `#with` — did not cost a point |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| blocks-procs | 1.00 | 4 | ok | omit (strong) |
| modules-mixins | 1.00 | 4 | ok | omit (strong) |
| objects-methods | 1.00 | 4 | ok | omit (strong) |
| enumerable | 1.00 | 4 | ok | omit (strong) |
| metaprogramming | 1.00 | 4 | ok | omit (strong) |
| error-handling | 0.875 | 4 | ok | omit (strong) |
| strings-symbols | 1.00 | 4 | ok | omit (strong) |
| collections-idioms | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.
error-handling: (0.67×3 + 1×2 + 1×2 + 1×1)/8 = 7.0/8 = 0.875.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 61.0/62 = 98%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none**. Every tag ≥ 0.75 (lowest is
error-handling at 0.875), so **no derived skill is generated** for tencent/hy3
on ruby. The model's Ruby knowledge is strong across the corpus; the only
partial miss (standarderror-01, point 3) is isolated and does not pull its tag
below threshold.
