---
model_id: muse-spark-1.2
model_version: "1.2"
evaluated_via: opencode-go
evaluated_on: 2026-08-20
stack: csharp
stack_corpus_rev: 1
threshold: 0.75
---

<!-- Filename: model_id with "/" flattened to "__" so it is one valid path
     segment. muse-spark-1.2 has no "/" so the filename is unchanged. -->

# Scorecard — muse-spark-1.2 on csharp

> Valid ONLY for `muse-spark-1.2` @ `1.2`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| csharp-null-01 | types-nullability | 3 | 3 | 3 | 1.00 | |
| csharp-null-02 | types-nullability | 3 | 3 | 2 | 0.67 | missed value-type-default (0/false) vs reference-type-default (null) point |
| csharp-null-03 | types-nullability | 3 | 3 | 3 | 1.00 | |
| csharp-null-04 | types-nullability | 2 | 2 | 2 | 1.00 | |
| csharp-pattern-01 | pattern-matching | 2 | 2 | 1 | 0.50 | mischaracterized CS8509 as a hard compile error and never mentioned the runtime `SwitchExpressionException` |
| csharp-pattern-02 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| csharp-pattern-03 | pattern-matching | 2 | 2 | 2 | 1.00 | |
| csharp-pattern-04 | pattern-matching | 2 | 2 | 2 | 1.00 | scope explanation meanders and briefly floats an incorrect claim before self-correcting, but lands on the right rule (definitely-assigned only on the matching path, incl. `is not` inverting it) |
| csharp-linq-01 | linq | 3 | 3 | 3 | 1.00 | |
| csharp-linq-02 | linq | 3 | 3 | 3 | 1.00 | |
| csharp-linq-03 | linq | 2 | 2 | 2 | 1.00 | |
| csharp-linq-04 | linq | 2 | 2 | 2 | 1.00 | |
| csharp-async-01 | async | 3 | 3 | 3 | 1.00 | |
| csharp-async-02 | async | 3 | 3 | 3 | 1.00 | |
| csharp-async-03 | async | 2 | 2 | 2 | 1.00 | |
| csharp-async-04 | async | 2 | 2 | 2 | 1.00 | |
| csharp-generics-01 | generics | 2 | 2 | 2 | 1.00 | |
| csharp-generics-02 | generics | 2 | 3 | 3 | 1.00 | contravariance explanation was hedged with a self-posed question but landed correctly |
| csharp-generics-03 | generics | 2 | 2 | 2 | 1.00 | |
| csharp-generics-04 | generics | 1 | 2 | 2 | 1.00 | |
| csharp-delegate-01 | delegates-events, linq | 2 | 2 | 2 | 1.00 | |
| csharp-delegate-02 | delegates-events | 2 | 2 | 2 | 1.00 | |
| csharp-delegate-03 | delegates-events | 3 | 3 | 3 | 1.00 | |
| csharp-delegate-04 | delegates-events | 2 | 2 | 2 | 1.00 | |
| csharp-dispose-01 | disposal | 3 | 2 | 2 | 1.00 | |
| csharp-dispose-02 | disposal, async | 2 | 2 | 2 | 1.00 | |
| csharp-dispose-03 | disposal | 2 | 2 | 2 | 1.00 | |
| csharp-dispose-04 | disposal | 2 | 2 | 2 | 1.00 | |
| csharp-span-01 | collections-spans | 2 | 2 | 2 | 1.00 | |
| csharp-span-02 | collections-spans | 3 | 3 | 3 | 1.00 | |
| csharp-span-03 | collections-spans | 2 | 2 | 2 | 1.00 | |
| csharp-span-04 | collections-spans | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| types-nullability | 0.91 | 4 | ok | omit (strong) |
| pattern-matching | 0.88 | 4 | ok | omit (strong) |
| linq | 1.00 | 5 | ok | omit (strong) |
| async | 1.00 | 5 | ok | omit (strong) |
| generics | 1.00 | 4 | ok | omit (strong) |
| delegates-events | 1.00 | 4 | ok | omit (strong) |
| disposal | 1.00 | 4 | ok | omit (strong) |
| collections-spans | 1.00 | 4 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 71.0 / 73 = 97.3%
```

## Derivation targets

Tags below threshold (`< 0.75`): **none**. Every tag scored ≥ 0.75 — no
derived skill required for `muse-spark-1.2` on csharp.

## Contamination check

Score is high (97.3%) but shows the fingerprints of genuine closed-book
recall rather than corpus leakage:

- The model made a real, graded-down factual error on `csharp-pattern-01`
  (called `CS8509` a compile error and omitted `SwitchExpressionException`
  entirely) — a leaked answer sheet would not introduce a wrong claim not
  present in the answer key.
- `csharp-pattern-04` shows visible in-context reasoning/self-correction
  ("Outside the if, c remains in scope... Actually after if, it falls out of
  scope unless...") rather than confidently reciting the reference wording —
  characteristic of the model working the answer out live, not copying it.
- `csharp-null-02` missed a full rubric point (value-type vs reference-type
  `default`) rather than covering all three cleanly, which a copied answer
  would not do.
- Throughout, phrasing, examples, and code snippets diverge substantially
  from the `questions.yaml` reference answers (different example types,
  different ordering of points, own code samples in dispose-04/span-04) —
  no near-verbatim rubric language was observed.
- The transcript log confirms zero tool invocations (44 tools exposed, 0
  called) and a single LLM turn, consistent with a closed-book run with no
  filesystem/web access to leak from.

C# 8–13 nullable/pattern/LINQ/async/dispose/span mechanics are also some of
the most heavily documented, frequently-explained topics in the entire .NET
ecosystem (official docs, countless blog posts, "gotcha" articles) — a
strong model scoring high here from training data alone is plausible. Verdict:
**no contamination flag** — treat as genuine strong closed-book knowledge of
well-documented standard material, with one real, non-trivial gap
(switch-expression exhaustiveness diagnostics) that keeps the score below a
perfect 100%.
