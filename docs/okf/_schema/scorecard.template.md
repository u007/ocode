---
model_id: EXACT-MODEL-ID       # e.g. claude-opus-4-8 — NEVER a family ("claude")
model_version: "X.Y"           # exact version string
provider: PROVIDER             # anthropic | openai | google | ...
evaluated_on: YYYY-MM-DD
stack: STACK                   # e.g. react
stack_corpus_rev: 1            # meta.yaml corpus_rev the answers were graded against
threshold: 0.75                # derivation threshold used
---

# Scorecard — {model_id} on {stack}

> Valid ONLY for `{model_id}` @ `{model_version}`. A version bump invalidates
> this scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| stack-topic-01 | tag | 2 | 2 | 2 | 1.00 | |
| stack-topic-02 | tag | 3 | 2 | 1 | 0.50 | missed X |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| tagA | 0.95 | 5 | ok | omit (strong) |
| tagB | 0.55 | 4 | ok | **derive** |
| tagC | 0.60 | 2 | low-n | derive (mark low-n) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = NN%
```

## Derivation targets

Tags below threshold (`< 0.75`): **tagB, tagC** → feed into
`derived/{stack}.{model_id}.SKILL.md`.
