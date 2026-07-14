---
name: kaizen-review
description: >
  Audit the current session's tool-call failures, judge whether they reveal significant
  model-behavior weaknesses, and if so emit a paste-ready prompt to extend the Kaizen OKF
  corpus (~/www/ocode/docs/okf) for the session's model. Use when the user invokes
  /kaizen-review or asks to "review tool failures", "evaluate this session's failures",
  "kaizen this session", or wants failure patterns turned into conduct/stack benchmark questions.
---

Audit THIS session's tool-call failures and decide whether they justify extending the Kaizen OKF corpus (in ~/www/ocode) for the model powering this session. You are the judge; do not modify any files.

## Step 1 — Collect failures from this conversation

Review the conversation so far. Collect every tool call that failed because of MODEL behavior:
- tool results with errors, wrong-parameter retries, edits that didn't apply
- hallucinated file paths, APIs, flags, or commands
- commands that exited non-zero due to a model mistake
- permission denials the model then retried verbatim

Exclude pure environment noise (network flake, missing service) unless the model handled it badly (silent fallback, no log, wrong recovery).

For each failure record: tool, what the model did wrong, the error text (short), and whether it recovered correctly.

## Step 2 — Judge significance

Group failures into behavioral patterns. Map each pattern to a Kaizen conduct tag: validation, fail-fast, error-handling, hallucination, testing, simplicity, surgical-changes, lifecycle, verification, safety, code-review, debugging. If a pattern is language/framework-specific rather than behavioral, note the stack (golang, react, python, ...) instead.

A pattern is SIGNIFICANT only if:
1. it recurred or wasted meaningful effort, AND
2. it reflects model behavior (would happen again on another task), AND
3. it is plausibly testable as a Q&A benchmark question.

Output the full failure list as a table (pattern, tag, occurrences, verdict).

If NOTHING is significant: say "Not significant — no corpus change recommended." and STOP.

## Step 3 — Emit the ocode prompt

If significant, output a fenced code block containing a complete, self-contained prompt the user will paste into an agent session in ~/www/ocode. Fill in the concrete details — do not leave placeholders except the ones listed. The prompt must contain:

1. **Context header**: the model id of THIS session (state it exactly; if unknown, instruct the user to fill it in), date, and the embedded failure evidence — each significant pattern with 1-2 verbatim example failures (tool, wrong action, error). The ocode session cannot see this conversation, so all evidence must be inlined.
2. **Target corpus** per pattern: behavioral → docs/okf/conduct/questions.yaml; stack-specific → docs/okf/<stack>/questions.yaml. Instruct it to first grep the target questions.yaml for existing coverage and drop any pattern already covered.
3. **Question drafting**: draft one question per uncovered pattern in the format of docs/okf/_schema/question-format.md matching the existing questions.yaml style (id, difficulty, weight, tags, question, answer, rubric). Present drafts and WAIT for user approval before writing anything.
4. **On approval, the Kaizen loop**:
   - append approved questions to questions.yaml; regenerate prompt sheets via docs/okf/_tools/gen-prompt-sheets.py
   - baseline: subagent answers ONLY the new questions closed-book — it may see only docs/okf/_prompts/<stack>.md, NEVER questions.yaml/questions.md (answer key; 100% = contamination red flag); grade against rubrics, record under docs/okf/<stack>/answers and scores using existing naming (<model>.md)
   - write/extend the model's derived improvement skill at docs/okf/<stack>/derived/<stack>.<model>.SKILL.md targeting the weak behaviors; sync via docs/okf/_tools/sync-derived-skills.py
   - re-evaluate: fresh subagent, SAME questions, with the derived skill injected (with-skill run, mirroring scores/<model>.with-skill.md); report baseline vs with-skill delta
5. **Read AGENTS.md and docs/okf/_schema first; follow house conventions.**
