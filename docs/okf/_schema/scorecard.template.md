---
model_id: EXACT-MODEL-ID       # canonical model identity, PROVIDER-STRIPPED.
                               # = ocode's resolved `model` after it removes a
                               # recognized provider prefix (see client.go split).
                               # `novita/tencent/hy3` → `tencent/hy3`.
                               # `anthropic/claude-opus-4-8` → `claude-opus-4-8`.
                               # NEVER a family ("claude"); NEVER include provider.
model_version: "X.Y"           # exact version string
evaluated_via: PROVIDER        # host used to RUN the eval — informational, NOT
                               # part of the key. novita | openrouter | anthropic
                               # | together | ... An eval done via one host
                               # applies to the same model on any host. Exception:
                               # if a host serves a materially different
                               # quantization (fp8 vs fp16 vs int4), treat that as
                               # a distinct eval and note it here.
evaluated_on: YYYY-MM-DD
stack: STACK                   # e.g. react
stack_corpus_rev: 1            # meta.yaml corpus_rev the answers were graded against
threshold: 0.75                # derivation threshold used
---

<!-- Filename: model_id with "/" flattened to "__" so it is one valid path
     segment. `tencent/hy3` → tencent__hy3.md ; `claude-opus-4-8` unchanged. -->


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
`derived/{stack}.{model_id-flattened}.SKILL.md` (`/` → `__` in the filename).
