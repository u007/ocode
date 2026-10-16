---
model_id: nvidia/nemotron-3.5-super-vl
model_version: "3.5"
evaluated_via: openrouter
evaluated_on: 2026-10-16
stack: ruby
stack_corpus_rev: 1
threshold: 0.85
---

# Scorecard — nvidia/nemotron-3.5-super-vl on ruby

> Valid ONLY for `nvidia/nemotron-3.5-super-vl` @ `3.5`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book (Rule 0). The answerer was
`ocode run -m openrouter/nvidia/nemotron-3.5-super-vl:free -effort med` under an
isolated HOME whose config denies every tool (no tools were advertised), in a
scratch cwd containing only the `_prompts/ruby.md` sheet, one fresh session;
the session sqlite shows exactly 1 user + 1 assistant message and 0 tool calls.
All 32 ids came back once, none empty. Graded from
`../answers/nvidia__nemotron-3.5-super-vl.md` (32/32 ids present, no duplicates,
no truncation). The OpenRouter `:free` route variant is not part of the model id.

Headless runs inject the bundled skill-catalogue digest into the system prompt;
no answer cites a tool, skill, or file the question never named. No thinking-
aloud, no fabricated tool output, no duplicated answers. Every answer is two to
three terse sentences in the model's own wording; none matches the reference
text verbatim, and the two lost points plus the off-rubric slips noted below
confirm the key was not visible.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| ruby-blocks-procvslambda-01 | blocks-procs | 3 | 2 | 2 | 1.00 |  |
| ruby-blocks-yield-02 | blocks-procs | 2 | 2 | 2 | 1.00 |  |
| ruby-blocks-ampblock-03 | blocks-procs | 2 | 2 | 2 | 1.00 | names `Symbol#to_proc` for `&:sym`; does not generalize to `to_proc` on any object, not required |
| ruby-blocks-create-04 | blocks-procs | 1 | 2 | 2 | 1.00 | lambda forms and `.call`/`.()`/`[]`/`===` all correct; proc creation (`proc`/`Proc.new`) not shown, but the question asks only how to create a lambda |
| ruby-modules-includeextendprepend-01 | modules-mixins | 3 | 3 | 3 | 1.00 | uses "below/above the class" in the opposite direction to the rubric's wording, but the semantics are right: prepended module overrides the class and `super`s into it, included module is overridden |
| ruby-modules-ancestors-super-02 | modules-mixins | 2 | 2 | 2 | 1.00 | does not enumerate the prepend/self/include/superclass order, but states lookup walks the ancestor chain with included modules as links |
| ruby-modules-namespace-03 | modules-mixins | 1 | 2 | 2 | 1.00 | correctly adds that `module_function` makes the instance copy private |
| ruby-modules-refinements-04 | modules-mixins | 1 | 2 | 2 | 1.00 | "defined with `Module#refine` inside a `Module.new` block" is an odd framing (any module body works); tangential |
| ruby-objects-methodmissing-01 | objects-methods | 3 | 2 | 2 | 1.00 | the `super` clause is attached to the `respond_to_missing?` sentence rather than to `method_missing`; both concepts present |
| ruby-objects-send-02 | objects-methods | 2 | 2 | 2 | 1.00 |  |
| ruby-objects-attr-03 | objects-methods | 2 | 2 | 2 | 1.00 |  |
| ruby-objects-visibility-04 | objects-methods | 1 | 2 | 1 | 0.50 | (judgment) private point withheld: claims "any explicit receiver, even `self.method`, is forbidden" — false since Ruby 2.7 (verified on a local Ruby: `self.private_method` runs), and public-is-default not stated. Protected point awarded |
| ruby-enumerable-include-01 | enumerable | 3 | 2 | 2 | 1.00 |  |
| ruby-enumerable-reduce-02 | enumerable | 2 | 2 | 2 | 1.00 | "return-is-ignored" is only implied ("rather than reassign an accumulator every iteration"); the fixed-mutable-object contrast is explicit |
| ruby-enumerable-lazy-03 | enumerable | 2 | 2 | 2 | 1.00 |  |
| ruby-enumerable-comparable-04 | enumerable | 2 | 2 | 2 | 1.00 |  |
| ruby-metaprogramming-singleton-01 | metaprogramming | 2 | 2 | 2 | 1.00 |  |
| ruby-metaprogramming-ivar-02 | metaprogramming | 1 | 2 | 2 | 1.00 |  |
| ruby-metaprogramming-definemethod-vs-mm-03 | metaprogramming | 2 | 2 | 2 | 1.00 |  |
| ruby-metaprogramming-classnew-04 | metaprogramming | 1 | 2 | 2 | 1.00 | anonymous class + optional superclass + naming on constant assignment all present; does not mention that the block is the class body |
| ruby-error-standarderror-01 | error-handling | 3 | 3 | 3 | 1.00 | does not add that custom errors should subclass StandardError here (it does in ruby-error-custom-03) |
| ruby-error-ensure-retry-02 | error-handling | 2 | 2 | 2 | 1.00 | lists `retry` among the exits that trigger `ensure`; in fact `retry` re-enters the block and `ensure` runs once at final exit (verified locally). Off-rubric |
| ruby-error-custom-03 | error-handling | 2 | 2 | 2 | 1.00 |  |
| ruby-error-elserescue-04 | error-handling | 1 | 2 | 2 | 1.00 |  |
| ruby-strings-symbols-01 | strings-symbols | 3 | 2 | 2 | 1.00 |  |
| ruby-strings-frozen-02 | strings-symbols | 2 | 2 | 1 | 0.50 | gets the pragma; misses the caveat: says interpolated strings "allocate a new object on each evaluation" and "mutating any frozen string still raises", but never states that since 3.0 interpolated literals are NOT frozen under the pragma |
| ruby-strings-quotes-03 | strings-symbols | 1 | 2 | 2 | 1.00 |  |
| ruby-strings-percent-04 | strings-symbols | 1 | 2 | 2 | 1.00 |  |
| ruby-collections-hashdefault-01 | collections-idioms | 2 | 2 | 2 | 1.00 | also correctly notes the plain default is never stored |
| ruby-collections-kwargs-02 | collections-idioms | 3 | 2 | 2 | 1.00 | (judgment) separation point awarded: "a Hash literal is no longer silently treated as keywords" carries the before/after change. The trailing clause "and keywords are not passed as a Hash" is over-general (keywords sent to a method with no keyword params still arrive as a positional Hash) but is not the explicit "raises" claim that would withhold it |
| ruby-collections-splat-03 | collections-idioms | 2 | 2 | 2 | 1.00 | `{**a, **b}` merge is a correct extra |
| ruby-collections-safenav-data-04 | collections-idioms | 2 | 2 | 2 | 1.00 | Data.define: immutable, readers only, value equality all present; `#with` not mentioned and "keyword-initialized" omits positional construction (both forms work), not wrong |

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
| collections-idioms | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 60.5 / 62 = 97.6%
```

Grading notes: the two lost half-points are the same two slips several other
models make on this corpus — the pre-2.7 "no `self.` on private" rule, and the
frozen-string-literal interpolation caveat. Everything else is correct and
compact. Three off-rubric inaccuracies were noted but did not touch a scored
point: `ensure` on `retry`, refinements framed as needing `Module.new`, and the
over-general "keywords are not passed as a Hash". No answer drifted into
contradictory or syntactically invalid Ruby.

## Derivation targets

Tags below threshold (`< 0.85`): **none**. No derived skill is written for this
model on ruby. `strings-symbols` sits exactly at the threshold (0.857); a future
corpus revision that adds a question to that tag should re-check it.
