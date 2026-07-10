# Rubric & Scoring Guide

How to turn a model's answers into a score, per-tag subscores, and ultimately a
derived skill.

## Per-question score

For one question:

1. Read the model's answer.
2. Award every `point` whose concept the answer genuinely contains (not just
   name-drops). Sum those `score` values → `raw`.
3. If `raw == 0` but the answer reaches a `partial` level, award that `partial`
   `score` instead.
4. `full = Σ(point.score)` for the question.
5. `normalized = min(raw, full) / full` → a 0–1 number.

Grading is about *concepts present*, not wording. If the model explains
"stable identity across renders" in its own words, award the point.

## Stack score

```
stack_score = Σ(normalized_q × weight_q) / Σ(weight_q)      # 0–1
```

Report as a percentage in the scorecard.

## Per-tag subscore (the important output)

For each tag `t`, restrict the same formula to questions carrying `t`:

```
subscore(t) = Σ(normalized_q × weight_q  for q in questions with tag t)
              / Σ(weight_q                for q in questions with tag t)
```

The per-tag subscore table is what drives derivation. A low subscore means the
model is weak *in that area of the stack* — that area, and only that area, earns
a section in the derived skill.

**Trust bar:** a subscore is only meaningful with **≥ 4 questions** on the tag.
Flag tags below that in the scorecard as `(low-n)`.

## Derivation threshold

- Start with **0.75**. Tags with `subscore < 0.75` get a corrective section in
  the derived skill.
- Tags with `subscore ≥ 0.75` are **omitted** — the model already knows it, so
  restating it wastes prompt tokens and prefix-cache budget.
- Adjust the threshold per stack if the derived skill comes out too large/small,
  and record the chosen threshold in the derived skill's front matter.

## Worked example (abridged)

Say React has these subscores for `claude-opus-4-8`:

| tag            | subscore | n | action                    |
|----------------|----------|---|---------------------------|
| hooks          | 0.95     | 5 | omit (strong)             |
| reconciliation | 0.90     | 4 | omit (strong)             |
| rsc            | 0.55     | 4 | **section in skill**      |
| effects        | 0.70     | 4 | **section in skill**      |
| suspense       | 0.60     | 2 | section, mark `(low-n)`   |

→ the derived skill covers only rsc, effects, suspense.
