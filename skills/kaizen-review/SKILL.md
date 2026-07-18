---
name: kaizen-review
description: >
  Audit the current session's tool-call failures AND context/efficiency/accuracy patterns,
  judge whether they reveal significant model-behavior weaknesses, and if so emit a
  paste-ready prompt to extend the Kaizen OKF corpus (~/www/ocode/docs/okf) for the
  session's model. Use when the user invokes /kaizen-review or asks to "review tool
  failures", "evaluate this session's failures", "kaizen this session", or wants failure
  patterns turned into conduct/stack benchmark questions.
---

Audit THIS session's tool-call failures and its context-usage/efficiency/accuracy patterns, and decide whether either justifies extending the Kaizen OKF corpus (in ~/www/ocode) for the model powering this session. You are the judge; do not modify any files.

## Step 1 — Collect failures from this conversation

Review the conversation so far. Collect every tool call that failed because of MODEL behavior:
- tool results with errors, wrong-parameter retries, edits that didn't apply
- hallucinated file paths, APIs, flags, or commands
- commands that exited non-zero due to a model mistake
- permission denials the model then retried verbatim

Exclude pure environment noise (network flake, missing service) unless the model handled it badly (silent fallback, no log, wrong recovery).

For each failure record: tool, what the model did wrong, the error text (short), and whether it recovered correctly.

## Step 1b — Collect context, efficiency, and accuracy patterns

Separately review the session for how well context was used, regardless of whether any tool call errored:

- **Context waste**: whole-file reads where an excerpt/grep would have sufficed, re-reading a file already read earlier in the session, redundant searches covering ground already covered, dumping large tool output into context instead of delegating to a subagent, ignoring available memory/index/search tools that would have answered the question cheaper.
- **Context speed**: places a targeted lookup (grep, single Read with offset/limit, ToolSearch) would have reached the answer faster than broad exploration; missed opportunities to parallelize independent reads/searches; sequential tool calls that had no data dependency on each other.
- **Accuracy from context gaps**: wrong assumption, wrong answer, or wrong file path traceable to skipped documentation, an unread relevant file, stale memory treated as current fact without verification, or premature action before enough context was gathered.
- **Accuracy from context misuse**: correct information was available in context (already read, already in memory) but the model contradicted or ignored it.

For each pattern record: what the model did, what the cheaper/more accurate alternative was, and the concrete cost (tokens/turns wasted, or the wrong output produced).

## Step 1c — Identify tooling gaps

For each failure and each context/efficiency/accuracy pattern from Steps 1 and 1b, ask: would this have been prevented or made cheaper by a **tool** change rather than (or in addition to) a model-behavior fix? Consider:

- **Missing tool**: no existing tool covers the need (e.g. a targeted search/lookup that had to be faked with multiple generic calls).
- **Tool description/schema is misleading or underspecified**: the model used a tool wrong because its description, parameter names, or examples didn't make the correct usage obvious.
- **Tool is too coarse or too narrow**: forces reading more than needed (no offset/limit/filter option) or requires many round trips that a batched/combined variant would cut to one.
- **Missing parallel/batch capability**: independent lookups had no way to run concurrently.
- **Missing shortcut into existing context**: the answer was already available via memory/search/index tooling but no tool surfaced it cheaply.

For each candidate note: which existing tool (built-in under `tools/`/`internal/tool`, an MCP tool, or a skill) is implicated or missing, whether the fix is NEW tool, UPDATE existing tool (description/schema/behavior), or NEW skill, and a one-line rationale tying it back to the concrete evidence from Step 1/1b. Do not write or modify any tool/skill files — this is a recommendation only.

## Step 2 — Judge significance

Group failures AND context/efficiency/accuracy patterns into behavioral patterns. Map each pattern to a Kaizen conduct tag: validation, fail-fast, error-handling, hallucination, testing, simplicity, surgical-changes, lifecycle, verification, safety, code-review, debugging, context-efficiency, context-accuracy. If a pattern is language/framework-specific rather than behavioral, note the stack (golang, react, python, ...) instead. Use `context-efficiency` for waste/speed patterns and `context-accuracy` for gap/misuse patterns above; introduce a new tag only if none of these fit.

A pattern is SIGNIFICANT only if:
1. it recurred or wasted meaningful effort, AND
2. it reflects model behavior (would happen again on another task), AND
3. it is plausibly testable as a Q&A benchmark question.

Output the full failure list as a table (pattern, tag, occurrences, verdict), and a separate table for context/efficiency/accuracy patterns using the same columns.

If any Step 1c tooling gaps were found, output them as a third table regardless of whether the corpus-side tables are significant: (gap, implicated tool/skill, fix type [NEW tool | UPDATE tool | NEW skill], rationale).

If NOTHING is significant across the first two tables AND no tooling gaps were found: say "Not significant — no corpus change recommended." and STOP.

If only tooling gaps were found (no significant corpus patterns), skip Step 3 and go straight to Step 4.

## Step 3 — Emit the ocode prompt

If significant, output a fenced code block containing a complete, self-contained prompt the user will paste into an agent session in ~/www/ocode. Fill in the concrete details — do not leave placeholders except the ones listed. The prompt must contain:

1. **Context header**: the model id of THIS session (state it exactly; if unknown, instruct the user to fill it in), date, and the embedded evidence — each significant pattern (both tool-failure and context/efficiency/accuracy) with 1-2 verbatim examples (tool/action, what happened, cost or error). The ocode session cannot see this conversation, so all evidence must be inlined.
2. **Target corpus** per pattern: behavioral → docs/okf/conduct/questions.yaml; stack-specific → docs/okf/<stack>/questions.yaml. Instruct it to first grep the target questions.yaml for existing coverage and drop any pattern already covered.
3. **Question drafting**: draft one question per uncovered pattern in the format of docs/okf/_schema/question-format.md matching the existing questions.yaml style (id, difficulty, weight, tags, question, answer, rubric). Present drafts and WAIT for user approval before writing anything.
4. **On approval, the Kaizen loop**:
   - append approved questions to questions.yaml; regenerate prompt sheets via docs/okf/_tools/gen-prompt-sheets.py
   - baseline: subagent answers ONLY the new questions closed-book — it may see only docs/okf/_prompts/<stack>.md, NEVER questions.yaml/questions.md (answer key; 100% = contamination red flag); grade against rubrics, record under docs/okf/<stack>/answers and scores using existing naming (<model>.md)
   - write/extend the model's derived improvement skill at docs/okf/<stack>/derived/<stack>.<model>.SKILL.md targeting the weak behaviors; sync via docs/okf/_tools/sync-derived-skills.py
   - re-evaluate: fresh subagent, SAME questions, with the derived skill injected (with-skill run, mirroring scores/<model>.with-skill.md); report baseline vs with-skill delta
5. **Read AGENTS.md and docs/okf/_schema first; follow house conventions.**

## Step 4 — Recommend tool/skill changes

If Step 1c found any tooling gaps, present them to the user as a numbered list (independent of, and in addition to, the Step 3 OKF prompt if one was emitted): for each, state the fix type (NEW tool / UPDATE tool / NEW skill), the implicated file or tool name, the concrete evidence it's based on, and a one-line proposed change. Do not implement any of them — wait for the user to pick which (if any) to act on before touching `tools/`, `internal/tool`, or any skill file.
