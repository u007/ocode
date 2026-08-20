---
model_id: muse-spark-1.2
model_version: "1.2"
evaluated_via: opencode-go
evaluated_on: 2026-08-20
stack: ror
stack_corpus_rev: 1
threshold: 0.75
---

<!-- Filename: model_id with "/" flattened to "__" so it is one valid path
     segment. muse-spark-1.2 has no "/" so the filename is unchanged. -->

# Scorecard — muse-spark-1.2 on ror

> Valid ONLY for `muse-spark-1.2` @ `1.2`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| ror-activerecord-01 | activerecord | 3 | 2 | 2 | 1.00 | |
| ror-activerecord-02 | activerecord | 2 | 3 | 3 | 1.00 | |
| ror-activerecord-03 | activerecord | 2 | 3 | 3 | 1.00 | |
| ror-activerecord-04 | activerecord | 3 | 2 | 2 | 1.00 | |
| ror-activerecord-05 | activerecord | 1 | 2 | 2 | 1.00 | |
| ror-querying-01 | querying | 3 | 3 | 3 | 1.00 | |
| ror-querying-02 | querying | 3 | 2 | 2 | 1.00 | |
| ror-querying-03 | querying | 2 | 2 | 2 | 1.00 | |
| ror-querying-04 | querying | 2 | 2 | 2 | 1.00 | |
| ror-callbacks-transactions-01 | callbacks-transactions | 3 | 2 | 2 | 1.00 | |
| ror-callbacks-transactions-02 | callbacks-transactions | 2 | 2 | 2 | 1.00 | |
| ror-callbacks-transactions-03 | callbacks-transactions | 3 | 2 | 2 | 1.00 | |
| ror-callbacks-transactions-04 | callbacks-transactions | 1 | 2 | 2 | 1.00 | |
| ror-migrations-schema-01 | migrations-schema | 2 | 2 | 2 | 1.00 | |
| ror-migrations-schema-02 | migrations-schema | 2 | 2 | 1 | 0.50 | missed "loaded to build a fresh/test DB" purpose of the dump; got the DSL-limitation reason for structure.sql |
| ror-migrations-schema-03 | migrations-schema | 3 | 2 | 2 | 1.00 | |
| ror-migrations-schema-04 | migrations-schema | 2 | 2 | 2 | 1.00 | |
| ror-controllers-routing-01 | controllers-routing | 3 | 2 | 2 | 1.00 | |
| ror-controllers-routing-02 | controllers-routing | 3 | 2 | 2 | 1.00 | |
| ror-controllers-routing-03 | controllers-routing | 2 | 2 | 2 | 1.00 | |
| ror-controllers-routing-04 | controllers-routing | 2 | 2 | 2 | 1.00 | |
| ror-controllers-routing-05 | controllers-routing | 1 | 2 | 2 | 1.00 | |
| ror-views-helpers-01 | views-helpers | 2 | 2 | 2 | 1.00 | |
| ror-views-helpers-02 | views-helpers | 2 | 2 | 1 | 0.50 | got the form_for/form_tag unification, but claimed current Rails defaults to a remote/Turbo form — rubric expects local-by-default since 6.1; matches the documented partial-credit failure mode exactly |
| ror-views-helpers-03 | views-helpers | 3 | 2 | 2 | 1.00 | |
| ror-views-helpers-04 | views-helpers | 2 | 2 | 2 | 1.00 | |
| ror-concerns-services-01 | concerns-services | 2 | 2 | 2 | 1.00 | |
| ror-concerns-services-02 | concerns-services | 2 | 2 | 2 | 1.00 | |
| ror-concerns-services-03 | concerns-services | 2 | 2 | 2 | 1.00 | |
| ror-concerns-services-04 | concerns-services | 1 | 2 | 2 | 1.00 | |
| ror-caching-jobs-01 | caching-jobs | 2 | 2 | 2 | 1.00 | |
| ror-caching-jobs-02 | caching-jobs | 2 | 2 | 2 | 1.00 | |
| ror-caching-jobs-03 | caching-jobs | 2 | 2 | 2 | 1.00 | |
| ror-caching-jobs-04 | caching-jobs | 1 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| activerecord | 1.00 | 5 | ok | omit (strong) |
| querying | 1.00 | 4 | ok | omit (strong) |
| callbacks-transactions | 1.00 | 4 | ok | omit (strong) |
| migrations-schema | 0.89 | 4 | ok | omit (strong) |
| controllers-routing | 1.00 | 5 | ok | omit (strong) |
| views-helpers | 0.89 | 4 | ok | omit (strong) |
| concerns-services | 1.00 | 4 | ok | omit (strong) |
| caching-jobs | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 71 / 73 = 97.3%
```

## Derivation targets

No tag fell below threshold (`< 0.75`) — the lowest subscore is
`migrations-schema` / `views-helpers` at 0.89. No corrective section is
warranted; no `derived/ror.muse-spark-1.2.SKILL.md` was written.

## Contamination check

`97.3%` is high enough to warrant scrutiny per the eval protocol. Assessed as
**plausible, not leakage**, for these reasons:

- This same model scored 92–100% on all 14 other stacks graded earlier in
  this run (golang 98.6%, rust 100%, react 100%, python 100%, ruby 98%,
  csharp 97.3%, nestjs 97.6%, nextjs ~97%, php 96.2%, vbnet 96.4%, dotnet 95%,
  elixir 92.5%, tanstack 92.6%, **conduct 83.8%**). Conduct — this repo's own
  un-learnable house-rules corpus — scoring *lowest* of all 15 is the strongest
  single discriminator against leakage: if the answerer subprocess had somehow
  gotten filesystem access to the answer key (the exact failure mode Rule 0
  warns about), that same access would have leaked `conduct` too, and it would
  not be the outlier on the low side. Instead the pattern is a near-uniform
  ceiling on mainstream, extremely well-documented framework material
  (`belongs_to` presence validation, N+1/`includes` vs `preload` vs
  `eager_load`, `after_commit` vs `after_save`, Strong Parameters, Russian-doll
  caching, job idempotency) and a visibly lower score on the one corpus that
  can't be memorized from training data — the expected signature of a strong
  general model, not a leaked key.
- The answers were produced by an isolated `ocode2 run` subprocess in an empty
  scratch directory (`/tmp/kaizen-ror-answer`), given only the
  `docs/okf/_prompts/ror.md` closed-book sheet as its prompt. The transcript
  log (`[TOOLS] exposing 44 tools: ...`) shows the tool list was made
  available to the agent loop but `grep -nE '^\[TOOL' output.log` matches zero
  invocation lines — no tool was actually called, only a single `[LLM] →/←`
  completion round (`input=23077` tokens is the 44 exposed tool-schema
  definitions plus the prompt sheet, not injected repo content; `output=7012`
  is the full 34-answer reply). No filesystem, grep, or web access occurred,
  so the subprocess could not have read `questions.yaml` or any answer key.
  (An unrelated `[LOCALMODEL] auto-start: local/qwen3-4b-instruct-4bit ready`
  line appears in the log — this is the harness's local embedder
  auto-starting in the empty scratch dir; benign, no corpus content to index.)
- The two misses (`ror-migrations-schema-02`, `ror-views-helpers-02`) are
  genuine, distinguishable mistakes, not hedged non-answers — including one
  outright factual error (claiming `form_with` defaults to a remote/Turbo
  form in current Rails, when Rails 6.1+ actually defaults to a local form).
  A model parroting a memorized answer key would not introduce a specific,
  plausible-but-wrong claim like that.

No contamination flag raised for this stack.
