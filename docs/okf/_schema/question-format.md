# Question Format

`<stack>/questions.yaml` is the **source of truth** for a stack's corpus. It is a
YAML list of question records. `questions.md` is a hand-synced human render of
the same data — never grade from the `.md`.

## Record schema

```yaml
- id: react-keys-01          # REQUIRED. Stable, unique within the stack.
                             # Convention: <stack>-<topic>-<nn>.
  difficulty: medium         # REQUIRED. easy | medium | hard.
  weight: 2                  # REQUIRED. 1–3. Importance in the stack score.
                             #   1 = nice-to-know, 2 = core, 3 = critical/foot-gun.
  tags: [reconciliation]     # REQUIRED. ≥1 topic tag. Drive per-tag subscores,
                             # which is how weakness is localized for derivation.
  question: >                # REQUIRED. What you actually ask the model.
    Why does React need `key` on list items, and why is the array index a bad key?
  answer: >                  # REQUIRED. The grading reference. Complete enough to
                             # grade against, KISS enough to scan. NOT a tutorial.
    Keys give each element a stable identity across renders so React matches
    old/new children instead of re-creating them. Index keys break on
    reorder/insert: state and DOM attach to the wrong item.
  rubric:                    # REQUIRED. ≥1 entry. See below.
    - { point: "stable identity across renders", score: 1 }
    - { point: "index breaks on reorder/insert", score: 1 }
    - { partial: "says only 'performance'", score: 0.5 }
```

## Rubric entries

Each entry is one of:

- **`point`** — a concept the answer MUST contain for full credit. `score` is the
  points awarded when present. The sum of all `point` scores = full marks for the
  question.
- **`partial`** — a common half-right answer. `score` is the (smaller) credit
  awarded when the answer only reaches this level and misses the `point`s. Used
  so a grader can award partial credit consistently instead of eyeballing it.

A grader awards, per question: the sum of matched `point` scores, OR — if no
full `point` is matched but a `partial` is — the `partial` score. Cap at full
marks. See `rubric-guide.md` for the aggregation math.

## Field conventions

- `id` never changes once published (scorecards reference it). To retire a
  question, remove it and bump `stack_corpus_rev` in `meta.yaml`.
- `tags` should be reused across questions in a stack — that is what makes a tag
  a meaningful subscore axis. Keep a stack's tag vocabulary small and in
  `meta.yaml` under `tags:`.
- `weight` is about *importance to the stack*, `difficulty` is about *how hard to
  answer*. They are independent: an easy question can be critical (weight 3,
  difficulty easy) and a hard question can be niche (weight 1, difficulty hard).
