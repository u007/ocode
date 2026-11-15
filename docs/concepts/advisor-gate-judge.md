---
type: Concept
title: Advisor gate judge (advisor_gate)
description: The advisor_gate decision judge (Jev) decides whether an advisor review runs at end of turn or before a git commit; its one answer floor applying to every choice (including review_now), the fallback when it gives no usable answer or no probability, the watermarked review ledger and its corrected limits, the human-only user_request, the once-per-Step gate, the migrated (not ignored) plan checkpoint, widened commit detection, and the stop-bound judge call.
resource: internal/agent/advisor_gate_typesafe.go
tags:
  - advisor
  - decision-judge
  - jev
  - agent-loop
  - checkpoints
  - commit
timestamp: 2026-11-15T10:08:36Z
status: active
---
# Advisor gate judge (`advisor_gate`)

Advisor reviews are no longer fired by a fixed rule alone. The `advisor_gate` decision judge (Jev, slot `slotAdvisorGate`) answers one question before a review: is a review worth running now? It is consulted only at the two moments. `review_now` at or above the answer floor runs the review — at end of turn it overrides the threshold rule, and before a commit it withholds the commit — while a `skip_*` above the floor suppresses it. Any failure (no decision backend, a judge error, an answer `validateAnswer` rejects, a missing probability for the chosen option, or any answer below the floor) leaves the caller on its own fallback rule. Added 2026-11-15.

## Where it lives

- `internal/agent/advisor_gate_typesafe.go`: the judge. `advisorGateChoiceFloor` (0.6, line 39), `advisorGateInstructions` (the rubric, line 53), `advisorGateQuestions` (line 91), `advisorGateDecide` (line 114), `advisorGateAsk` (line 129), `advisorGateState` (line 177), `advisorGateReviews` (line 223), `advisorGateCommands` (line 243), `advisorGateToolNames` (line 258), `advisorGateCapList` (line 285), `advisorGateClip` (line 295).
- `internal/agent/advisor_checkpoint.go`: `checkpointDone` (line 17), `advisorReviewEntry` (line 20), `advisorCheckpointState` (line 49), `newAdvisorCheckpointState` (line 87), `lastHumanUserContent` (line 121), `runAdvisorCheckpoint` (line 190; advice redacted at line 224), `advisorDoneCheckpoint` (line 239), `advisorCommitHold` (line 308), `bashRunsGitCommit` (line 374), `gitWordsRunCommit` (line 386), and the ledger helpers `recordAdvisorReview` (line 407) and `advisorChangedSinceLastReview` (line 436). Constants: `advisorLedgerMax` = 20 (line 32), `doneCheckpointMinToolCalls` = 5 (line 42).
- `internal/agent/agent.go`, `Agent.Step`: `userGoal := lastHumanUserContent(messages)` (line 1667), the per-Step checkpoint state and the skill flag (lines 1732-1733), the end-of-turn call (line 1932), the per-response skill check (line 1950), and the before-commit call (line 2008).
- `internal/snapshot/snapshot.go`: `LastWriteSeq` (line 718) and `ChangedSince` (line 734), the ledger's write watermark.
- Slot: `slotAdvisorGate` in `internal/agent/decider.go` (line 32), resolved from `oc.AdvisorGate.JudgeModel` (lines 119-120).
- Config: `AdvisorGate JudgeModelConfig` in `internal/config/ocodeconfig.go` (line 1088). The JSON key is `advisor_gate` with a nested `judge_model`. The default is `defaultJudgeModel` = `typesafe/jev-latest` (line 978).
- Prompt catalog key `advisor_gate` (`internal/agent/prompt_catalog.go`, line 51). A user add-on or a disable works as for other judge prompts.

## The answer floor applies to every choice

`advisorGateChoiceFloor` (0.6) is the chosen-option probability **any** answer needs before the caller may act on it — `review_now` and every `skip_*` alike.

- Below the floor, the answer is **not decided**. The caller then applies its own fallback: at end of turn the threshold rule, and before a commit the commit runs untouched. A zero-confidence `skip_*` therefore cannot suppress a review, and a zero-confidence `review_now` cannot withhold a commit.
- An answer with **no probability entry** for the option it chose is also rejected as not decided (`prob, hasProb := ans.Probabilities[ans.Choice]`). A missing probability is not a confident answer.
- The check is `!(prob >= advisorGateChoiceFloor) || math.IsInf(prob, 0)`, so NaN and infinity both fail. There is no separate skip floor any more; the old lower bar for skips is gone.

## The plan checkpoint migrated, it is not "ignored"

The plan checkpoint (a review after the first edit batch) no longer fires. A stored `"plan"` is **migrated to `"done"`**, not dropped, so a session pinned to `["plan"]` keeps a review instead of silently losing every checkpoint.

- `config.NormalizeAdvisorCheckpoints` (`internal/config/ocodeconfig.go:889`) maps `"plan"` to `"done"` case-insensitively, drops blank entries, dedupes, leaves `nil` as `nil`, and leaves an explicit empty list empty.
- It is called from `agent.AdvisorConfigFromConfig` (`internal/agent/advisor_config.go:37`), `Agent.SetAdvisorConfig` (`internal/agent/advisor_config.go:57`) and `advisorCheckpointsFromMeta` (`internal/server/advisor_session.go:215`), so the process-wide default, an in-session config change and a stored session pin all migrate.
- The `AdvisorConfig.Checkpoints` field comment (`internal/config/ocodeconfig.go:862`) states that `"done"` gates BOTH moments — the end-of-turn review and the before-commit hold — and that an empty list disables both.
- The default checkpoint list is `["done"]` (`internal/config/ocodeconfig.go:1839`). The web AdvisorForm offers only `"done"`. Regression: `TestAdvisorCheckpoint_PlanValueMigrates` (`internal/agent/advisor_checkpoint_test.go:531`).

## The two moments

### end_of_turn: `advisorDoneCheckpoint`

Runs only on a response with no tool calls. In order:

1. No judge call and no review when the checkpoint is already spent (`doneChecked`, once per Step), a permission or question ask is pending (`blockedByPendingAsk`), a loaded skill opts out (`skillSuppressed`, see [SKILL.md frontmatter](skill-frontmatter.md)), or there is nothing to review. "Nothing to review" means a snapshot store is attached, it tracks no changed file, and this Step wrote nothing (`advisorNoSessionChanges`). Without a snapshot store the gate is always consulted.
2. The judge decides. `review_now` runs the review only at or above the answer floor. A `skip_*` answer ends the check for the Step (it sets `doneChecked`), also only at or above the floor.
3. Fallback when the verdict is not decided. This covers a disabled prompt, no decision backend or API key, a judge error or empty response, a missing answer, an answer that `validateAnswer` rejects, a missing probability for the chosen option, and **any** answer below the floor. The threshold rule then applies: at least one write-class call, or `doneCheckpointMinToolCalls` (5) tool calls in total this Step. The review is also skipped when a before-commit review already ran this Step and nothing has been written since.

### before_commit: `advisorCommitHold`

Consulted before any tool in a batch is dispatched. Only a `bash` call whose command runs `git commit` counts. `bashRunsGitCommit` first runs `spaceOutOperators` (so `git commit;git push` and `git commit&&git push` are split correctly), then checks each simple command after launcher wrappers are peeled; `gitWordsRunCommit` skips global git options including the separate-value `-c`, `-C`, `--git-dir`, `--work-tree`, `--namespace`, `--super-prefix` and `--exec-path`. The judge is asked at most once per Step (`commitAsked`), even when its answer is not decided.

The commit calls are withheld **only on a decided `review_now`** — which now means at or above the answer floor. Then the review runs, and every withheld commit call gets a plain-prose tool message. That message carries the review and tells the model to fix the gaps or re-issue the commit. Every other outcome runs the batch untouched: a `skip_*`, an answer below the floor, or an unavailable judge. This is deliberate: the pre-commit check is never enforced by a fallback, so only a confident Jev answer can hold a commit. `skip_work_unfinished` is never offered at this moment.

**"At most once" is per Step, not per turn.** `doneChecked` and `commitAsked` live in the per-Step state (`advisorCheckpointState`). A turn that pauses on a permission or question ask continues in a NEW Step and may consult a checkpoint again; that is intended, because the continuation starts from new state. The model-facing messages say "per step".

## The question

One `choice` under the key `review`, with these answers:

- `review_now`: substantive unreviewed changes exist, the user did not decline, and the work is not mid-way.
- `skip_already_reviewed`: `facts.review_count` is at least 1 and `facts.unreviewed_file_count` is 0.
- `skip_user_declined`: the request is only to commit or push, or the user said not to review or validate.
- `skip_not_substantive`: this turn made no substantive change (housekeeping, a status check, an investigation, an answer, or a comment, wording or formatting edit).
- `skip_work_unfinished`: end_of_turn only. The todo plan still has open items, or the report says more steps follow.

The rubric is `advisorGateInstructions`. The per-choice criteria are built in `advisorGateQuestions`.

## The state the judge sees

- `moment`, and `subject`: `commit_command` (before_commit) or `final_report` (end_of_turn).
- `user_request`, clipped to 1500 runes. It comes from `lastHumanUserContent` (`advisor_checkpoint.go:121`), which uses `isHumanPromptMessage`: synthetic `[ocode:*]` notices, auto-continue resumes and the step-limit notice are skipped. A turn started by a background-job `[ocode:event]` notice therefore has an EMPTY `user_request`; background output can never read as the user declining a review.
- `todo_plan`, the first 30 lines.
- `session_changed_files` and `changed_since_last_review`, each capped at 100 entries with a "more not shown" marker.
- `reviews`: the last 5 ledger entries, oldest first, each with moment, files and verdict (verdict clipped to 600 runes).
- `turn`: `tool_calls_by_name`, `files_written` and `bash_commands` (the last 30, each clipped to 200 runes). `tool_calls_by_name` is capped to the 20 most-called names, each name clipped to 40 runes, with a `…(N more not shown)` key — tool names come from the model, so this map is bounded here rather than by the shared decision budget.
- `facts`: `review_count`, `unreviewed_file_count` and `files_written_this_turn`.

## Review ledger

The ledger lives in memory on the agent and keeps at most 20 entries. Each entry stores the session's changed files at review time, a clipped verdict, and `Seq` — the snapshot store's write watermark at review time (`LastWriteSeq`, `snapshot.go:718`).

`changed_since_last_review` (`advisorChangedSinceLastReview`, `advisor_checkpoint.go:436`) unions two sources:

- `snapshot.Store.ChangedSince(seq)` (`snapshot.go:734`): every tracked write newer than the last review's watermark, whatever tool made it. A post-review `apply_patch` (which carries only `patchText`), a formatter, or a bash edit therefore counts as unreviewed.
- The session's changed files that the last review's `Files` list did not cover. This catches a backup the store never sequenced.

Limits, corrected:

- **Sub-agent writes are seen.** Sub-agents share the same snapshot store, so their writes carry a sequence too; the old "only main-agent writes are tracked" limit no longer applies.
- **A backup the store never sequenced cannot be dated**, so it is always reported as changed (`Backup(path, "")` from a non-agent caller).
- The ledger is lost on restart. The gate then sees "nothing reviewed yet".
- With no review yet, `changed_since_last_review` is every changed file.

## Why the facts block exists

Live finding, 2026-11-15, against the real `jev-latest`. With only the bare lists and a looser rubric, 2 of 12 scenarios were wrong:

- "fix X and commit it" was answered `skip_user_declined` (p=0.94). The word "commit" read as a plain-commit request.
- "reviewed, fixes made, commit about to run" was answered `skip_already_reviewed` (p=0.59).
- A question-only turn scored 0.58-0.60, right at the 0.6 floor.

After the explicit counts were added, plus rubric lines defining "declined" (a commit-only request, or being told not to review) and "already reviewed" (`review_count` at least 1 AND `unreviewed_file_count` 0), all 12 scenarios were correct on two consecutive runs, with p between 0.89 and 1.00. Each request costs roughly 1.2k-2.1k input tokens. A re-run made after the answer-floor change — `go test ./internal/agent -run TestAdvisorGateLiveScenarios -v` with the configured key — returned 12/12 correct, every chosen-option probability between 0.89 and 1.00 and every verdict `acted=true`, so `advisorGateChoiceFloor` (0.6) changed no decision.

## Failure handling

- The judge call is **stop-bound**: `advisorGateAsk` uses `DecideCtx` with `stopChContext(a.StopCh())` (`advisor_gate_typesafe.go:139`), so Stop aborts the judge round trip rather than waiting it out.
- `advisorGateDecide` recovers a panic and returns not decided. It never reaches the agent loop.
- Every decision writes one `ADVISOR` debug line: moment, choice, probability, decided, review and detail.
- Usage is booked with `deciderLabel`, so the ledger does not mix backends.
- Advisor advice is passed through `redactText` before either caller persists it (`advisor_checkpoint.go:224`), in addition to the guardrail's redact → vet → truncate ordering.
- `typesafeCapContent` now delegates to `advisorGateClip`, so every judge-state truncation shares one marker.
- Never falls back to another backend. An unconfigured judge leaves the caller on the threshold rule (before_commit: the commit runs).

## Web UI

The AdvisorForm checkbox is labelled "Review at end of turn and before a commit (\"done\")". The chat reducer's status line is `Advisor <kind> checkpoint — reviewing…`, and the upstream kinds are `done` and `commit` only.

## Verification

- `go test ./internal/agent/ -run TestAdvisorGateLiveScenarios -v` runs the 12 live scenarios (`internal/agent/advisor_gate_live_test.go`, line 37). It is gated on `TYPESAFE_API_KEY`.
- Stubbed unit tests are in `internal/agent/advisor_gate_typesafe_test.go`. They cover end-of-turn skip and review, the undecided fallback, the below-floor review_now fallback, the no-session-changes case not asking the judge, the panic case, the commit hold, `bashRunsGitCommit`, the ledger (including `ChangedSince`), and the turn summary.
- Not verified: a full live agent turn end to end. The loop is covered only by stubbed tests, and the live judge only by synthetic states.

## Related

- [Advisor checkpoints must wait for a pending permission/question ask](../gotchas/advisor-checkpoints-must-wait-for-a-pending-ask.md)
- [Pending ask suppresses advisor, auto-continue, and goal-resume](pending-ask-suppresses-advisor-auto-continue-goal.md)
- [SKILL.md Frontmatter Fields](skill-frontmatter.md): the `skip_advisor` opt-out
- [Advisor Claude Code CLI backend](advisor-claude-code-backend.md): what the advisor model is and how it is selected
