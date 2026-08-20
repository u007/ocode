---
model_id: muse-spark-1.2
model_version: "1.2"
evaluated_via: opencode-go
evaluated_on: 2026-08-20
stack: vbnet
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — muse-spark-1.2 on vbnet

> Valid ONLY for `muse-spark-1.2` @ `1.2`. A version bump invalidates this
> scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| vbnet-syntax-01 | syntax-basics | 2 | 3 | 3 | 1.00 | |
| vbnet-syntax-02 | syntax-basics, conversions-arrays | 3 | 3 | 3 | 1.00 | |
| vbnet-syntax-03 | syntax-basics, oop | 2 | 2 | 2 | 1.00 | |
| vbnet-syntax-04 | syntax-basics | 2 | 2 | 2 | 1.00 | |
| vbnet-props-01 | properties | 2 | 2 | 2 | 1.00 | |
| vbnet-props-02 | properties | 2 | 2 | 2 | 1.00 | |
| vbnet-props-03 | properties | 2 | 2 | 2 | 1.00 | |
| vbnet-props-04 | properties | 1 | 2 | 2 | 1.00 | |
| vbnet-null-01 | nullability | 3 | 3 | 3 | 1.00 | |
| vbnet-null-02 | nullability | 2 | 2 | 2 | 1.00 | |
| vbnet-null-03 | nullability | 3 | 3 | 3 | 1.00 | |
| vbnet-null-04 | nullability | 2 | 2 | 2 | 1.00 | |
| vbnet-errors-01 | error-handling | 2 | 2 | 2 | 1.00 | |
| vbnet-errors-02 | error-handling | 2 | 2 | 2 | 1.00 | |
| vbnet-errors-03 | error-handling | 3 | 2 | 2 | 1.00 | |
| vbnet-errors-04 | error-handling | 2 | 2 | 2 | 1.00 | |
| vbnet-linq-01 | linq-query | 2 | 2 | 2 | 1.00 | |
| vbnet-linq-02 | linq-query | 2 | 2 | 2 | 1.00 | |
| vbnet-linq-03 | linq-query | 1 | 2 | 2 | 1.00 | |
| vbnet-linq-04 | linq-query | 2 | 2 | 2 | 1.00 | |
| vbnet-events-01 | events | 3 | 2 | 1 | 0.50 | explains WithEvents+Handles auto-wiring ("no manual wiring needed") but never states one `Handles` clause can list multiple events (e.g. `Handles a.Click, b.Click`) |
| vbnet-events-02 | events | 2 | 2 | 1 | 0.50 | covers AddHandler/RemoveHandler/AddressOf and generic "not a WithEvents source" cases, but misses the two hard-requirement cases (`Shared` events, `Structure`s) where `Handles` is structurally unusable |
| vbnet-events-03 | events | 2 | 2 | 2 | 1.00 | |
| vbnet-events-04 | events | 1 | 2 | 2 | 1.00 | |
| vbnet-oop-01 | oop | 3 | 2 | 2 | 1.00 | correctly includes the per-member `Implements IFoo.Member` binding clause |
| vbnet-oop-02 | oop | 3 | 3 | 3 | 1.00 | |
| vbnet-oop-03 | oop | 2 | 2 | 2 | 1.00 | |
| vbnet-oop-04 | oop | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-01 | conversions-arrays | 3 | 3 | 3 | 1.00 | |
| vbnet-convarr-02 | conversions-arrays | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-03 | conversions-arrays | 2 | 2 | 2 | 1.00 | |
| vbnet-convarr-04 | conversions-arrays, syntax-basics | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| syntax-basics | 1.00 | 5 | ok | omit (strong) |
| properties | 1.00 | 4 | ok | omit (strong) |
| nullability | 1.00 | 4 | ok | omit (strong) |
| error-handling | 1.00 | 4 | ok | omit (strong) |
| linq-query | 1.00 | 4 | ok | omit (strong) |
| events | 0.69 | 4 | ok | **derive** |
| oop | 1.00 | 5 | ok | omit (strong) |
| conversions-arrays | 1.00 | 5 | ok | omit (strong) |

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 96.4%
```

## Derivation targets

Tags below threshold (`< 0.75`): **events** → feed into
`derived/vbnet.muse-spark-1.2.SKILL.md`.

## Contamination check

Overall score (96.4%) is high but not a flat 100%, and the one gap (events,
0.69) is a specific, coherent weakness — the same `WithEvents`/`Handles`
edge cases (multi-event `Handles` clauses, `Shared`/`Structure` constraints)
that other models evaluated against this same corpus (mimo-v2.5, tencent/hy3)
also missed. Answers are phrased in the model's own words throughout (e.g.
own code examples, own explanations of `MyClass` vs `Me` dispatch, own
phrasing of narrowing/widening) rather than echoing the rubric's or answer
key's exact sentences — no verbatim rubric language was found. VB.NET is a
mature, extremely well-documented, syntax-stable language with three decades
of tutorials/StackOverflow/MSDN content, so a strong closed-book score here
is plausible "well-known, well-documented material" rather than corpus
leakage. Verdict: **not flagged as contamination** — genuine strong recall
with one real, specific, reproducible gap.
