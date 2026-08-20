---
model_id: muse-spark-1.2
model_version: "1.2"
evaluated_via: opencode-go
evaluated_on: 2026-08-20
stack: ruby
stack_corpus_rev: 1
threshold: 0.75
---

<!-- Graded independently, CLOSED-BOOK answers (ocode2 run -model
     opencode-go/muse-spark-1.2 -dir /tmp/kaizen-ruby-answer -yolo -effort
     medium, isolated empty dir, 0 tool invocations logged). -->

# Scorecard — muse-spark-1.2 on ruby

> Valid ONLY for `muse-spark-1.2` @ `1.2`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| ruby-blocks-procvslambda-01 | blocks-procs | 3 | 2 | 2 | 1.00 | |
| ruby-blocks-yield-02 | blocks-procs | 2 | 2 | 2 | 1.00 | |
| ruby-blocks-ampblock-03 | blocks-procs | 2 | 2 | 2 | 1.00 | |
| ruby-blocks-create-04 | blocks-procs | 1 | 2 | 2 | 1.00 | shows both lambda and proc/Proc.new creation, all invoke forms |
| ruby-modules-includeextendprepend-01 | modules-mixins | 3 | 3 | 3 | 1.00 | |
| ruby-modules-ancestors-super-02 | modules-mixins | 2 | 2 | 2 | 1.00 | |
| ruby-modules-namespace-03 | modules-mixins | 1 | 2 | 2 | 1.00 | |
| ruby-modules-refinements-04 | modules-mixins | 1 | 2 | 2 | 1.00 | |
| ruby-objects-methodmissing-01 | objects-methods | 3 | 2 | 2 | 1.00 | includes the `super` fallthrough explicitly |
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
| ruby-error-standarderror-01 | error-handling | 3 | 3 | 2 | 0.67 | points 1+2 correct (bare rescue = StandardError; Exception swallows SystemExit/SignalException, breaks Ctrl-C/exit); MISSED point 3, the positive prescription — never states "rescue StandardError, and custom errors should subclass StandardError" |
| ruby-error-ensure-retry-02 | error-handling | 2 | 2 | 2 | 1.00 | |
| ruby-error-custom-03 | error-handling | 2 | 2 | 2 | 1.00 | |
| ruby-error-elserescue-04 | error-handling | 1 | 2 | 2 | 1.00 | states the implicit `begin` on a method body explicitly |
| ruby-strings-symbols-01 | strings-symbols | 3 | 2 | 2 | 1.00 | |
| ruby-strings-frozen-02 | strings-symbols | 2 | 2 | 2 | 1.00 | |
| ruby-strings-quotes-03 | strings-symbols | 1 | 2 | 2 | 1.00 | |
| ruby-strings-percent-04 | strings-symbols | 1 | 2 | 2 | 1.00 | |
| ruby-collections-hashdefault-01 | collections-idioms | 2 | 2 | 2 | 1.00 | |
| ruby-collections-kwargs-02 | collections-idioms | 3 | 2 | 2 | 1.00 | explicitly covers `def m(**opts)` collecting arbitrary keywords |
| ruby-collections-splat-03 | collections-idioms | 2 | 2 | 2 | 1.00 | |
| ruby-collections-safenav-data-04 | collections-idioms | 2 | 2 | 2 | 1.00 | |

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
error-handling at 0.875), so **no derived skill is generated** for
muse-spark-1.2 on ruby — see `rubric-guide.md` ("Tags with subscore ≥ 0.75
are omitted").

## Contamination check

Score is 98%, matching the tencent/hy3 ruby eval (also 98%, same single miss
on `ruby-error-standarderror-01` point 3). Both this eval and the mimo-v2.5
eval (92%) were run closed-book via an isolated `ocode2 run` subprocess in an
empty scratch directory, with the transcript confirming zero tool
invocations. Ruby 3.x core-language semantics (blocks/procs, mixins,
metaprogramming, error hierarchy, string/symbol semantics, keyword-argument
changes) are extremely well-documented, stable, frequently-covered-in-training
material — a near-ceiling score here is consistent with "well-documented
standard material," not leakage. There is no verbatim rubric-language
reuse: answers are phrased in the model's own words, with independently
chosen examples, code snippets, and asides (e.g. `lambda?`, `instance_exec`,
`ruby2_keywords`, `%W`/`%I` interpolated variants) that do not appear in
questions.yaml at all — a sign of genuine recall rather than copying an
answer key it never saw. No contamination flagged.
