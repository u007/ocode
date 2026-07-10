# How to Run an Evaluation

This is a runbook **and** a ready-to-paste AI prompt. It takes one **stack** +
one **exact model** and produces two artifacts: a scorecard and a derived skill.

## What you need

- A stack folder under `docs/okf/<stack>/` with a `questions.yaml` (react,
  golang, rust, tanstack, nextjs today).
- The **exact** model id + version you're evaluating (e.g. `claude-opus-4-8` @
  `4.8`). Never a family name like "claude".

## The two ways to run it

### A. Manual (you drive)

1. Open `docs/okf/<stack>/questions.yaml`. For each record, paste `question`
   into the target model in a **fresh chat** (no shared context between
   questions — you're testing the model, not its memory of your last answer).
2. Grade its reply against that record's `rubric` using
   `_schema/rubric-guide.md`: award each `point` whose concept is genuinely
   present; if none but a `partial` matches, award the partial.
3. Fill `_schema/scorecard.template.md` → save as
   `docs/okf/<stack>/scores/<exact-model-id>.md`.
4. Read the per-tag subscores. For every tag `< threshold` (0.75), write a
   corrective section in `docs/okf/<stack>/derived/<stack>.<exact-model-id>.SKILL.md`.
   Cover **only** weak tags — say nothing about tags the model aced.

### B. Prompt an AI to do it (paste the block below)

Give the AI this repo and the block below. It reads the corpus, answers as the
target model (or you feed it the target model's answers), grades, and writes both
files. **Best practice:** have the AI *grade*, but get the answers from the
actual target model — an AI grading its own answers is generous.

---

## Paste-this prompt

```
You are running a Kaizen evaluation in this repo. Read docs/okf/README.md and
docs/okf/_schema/{question-format,rubric-guide,scorecard.template,stack-detection}.md
first so you follow the exact format.

INPUTS (fill these in):
- STACK: <react | golang | rust | tanstack | nextjs>
- MODEL_ID: <exact model id, e.g. claude-opus-4-8>   # NEVER a family name
- MODEL_VERSION: "<x.y>"
- PROVIDER: <anthropic | openai | google | ...>
- ANSWERS_SOURCE: <"grade the answers I paste below" | "you answer as this model">
  # Prefer pasting real answers from the target model. An AI grading its own
  # answers inflates the score.

STEPS:
1. Load docs/okf/<STACK>/questions.yaml (the source of truth) and its meta.yaml.
   Note corpus_rev.
2. For each question, obtain the model's answer (from ANSWERS_SOURCE) and grade
   it against that record's rubric per rubric-guide.md:
   - award every `point` whose concept is genuinely present (own wording is fine),
   - else award a matching `partial`,
   - normalized = min(awarded, full) / full.
3. Compute per-tag subscores = Σ(normalized×weight)/Σ(weight) over each tag's
   questions. Mark any tag with <4 questions as (low-n).
4. Write docs/okf/<STACK>/scores/<MODEL_ID>.md from scorecard.template.md:
   fill front matter (model_id, model_version, provider, evaluated_on, stack,
   stack_corpus_rev = the corpus_rev from step 1, threshold: 0.75), the
   per-question table, per-tag subscore table, stack score, and derivation targets.
5. For every tag with subscore < 0.75, write ONE corrective section in
   docs/okf/<STACK>/derived/<STACK>.<MODEL_ID>.SKILL.md. Rules:
   - Cover ONLY below-threshold tags. Say NOTHING about tags at/above threshold
     (the model already knows them — restating wastes prompt/cache budget).
   - Front matter: name, description, when_to_use (gate on: active model id ==
     <MODEL_ID> AND repo uses <STACK>), plus tuned_for, tuned_version, stack,
     source_scorecard, threshold, revalidate_when: model_version changes.
   - Each section = concrete corrective guidance for that tag's real failure,
     drawn from what the model actually got wrong in the grading.
6. Do NOT overwrite the illustrative react/scores/claude-opus-4-8.md placeholder
   unless STACK=react and MODEL_ID=claude-opus-4-8 and you are replacing it with
   a REAL evaluation — in which case remove the "ILLUSTRATIVE" banner.
7. Validate: scorecard front matter is complete, every below-threshold tag has a
   skill section and no above-threshold tag does. Report the stack score, the
   per-tag table, and the tags you derived.

Do not commit unless asked.
```

---

## After the eval

A real scorecard + derived skill for one (stack × exact model) is the thing that
unblocks the enforcement hook (`TODO.md` → "Wire the enforcement hook"). Until at
least one real pair exists, the runtime injection stays unbuilt on purpose.

**Version discipline:** re-run the whole eval when the model's version changes —
a scorecard is valid only for the exact `model_id` + `model_version` it names.
