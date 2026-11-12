---
type: Concept
title: "Server-Side Auto-Continue Loop"
description: 'Server-side auto-continue loop pattern: per-session on/off gate, bounded chain with step-limit cutoff, typesafe/chat judge dispatch over a closed next-action set, the awaiting_confirmation/awaiting_decision split, the question judge that answers structured question asks only when the transcript determines the answer, and visible end-of-turn status.'
tags: [server, auto-continue, agent-loop, architecture, typesafe, scheduled-jobs, session, question-judge]
timestamp: 2026-11-07T02:07:18Z
---
# Server-Side Auto-Continue Loop

## Overview

The server-side auto-continue loop automatically re-polls the LLM after each turn completes when the agent has not explicitly stopped, enabling unattended multi-step workflows (scheduled jobs, headless API calls, cron dispatch). The on/off gate is scoped to one chat session, not the process: each session keeps its own override, and the process-wide config value is only the default for sessions that never toggled (see "Per-Session Gate"). The loop is bounded: it chains a finite number of continuation steps before forcing a stop, preventing runaway token usage.

## Architecture

### Loop Lifecycle

1. **Initial turn** — The server dispatches the first `agent.Step` from the incoming message.
2. **Judge dispatch** — On turn completion, a lightweight "auto-continue judge" (the configured auto-continue model, e.g. `typesafe/<model>`) inspects the transcript (the selected messages, see "Transcript Selection and Original Request") and returns a typed **next action** (see "Action Set" below) with a confidence score.
3. **Continue gate** — If the judge picks an end action (`end`, `await_user`), its confidence is below the floor for the action's class (`autoContinueMinConfidenceDefault` for resume actions, `autoContinueAnswerMinConfidence` for answer actions, both in `internal/agent/autocontinue_typesafe.go`), or one of the awaiting-user vetoes applies, the loop terminates and the final result is returned. The floors are **decoupled from** `permissions.auto.min_confidence`, which governs the auto-permission judge. Different actions deserve different confidence bars: a resume is low-stakes and reversible (the chain is bounded), so it uses its own, lower floor; an answer given on the user's behalf uses the permission default.
4. **Chain extension** — If the judge picks a resume or answer action, the loop appends that action's **fixed user-role prompt**, records the action key in the chain history, increments the step counter and starts a new `agent.Step` with the accumulated transcript.
5. **Chain cap** — The loop stops after reaching `AutoContinueChainCap` (a Go constant, value 4, in `internal/agent/autocontinue_typesafe.go`), regardless of the judge's verdict. The end-of-turn reason is surfaced as a transcript notice (server) or TUI transient hint (see "End-of-Turn Surfacing" below).

### Action Set

The judge does not answer "continue or end". It picks one key from a closed set (`autoContinueActions`), and every key maps to a **fixed resume prompt** — Jev cannot write prose, so the judge chooses between pre-written instructions rather than composing one. Keys and criteria are static; per-turn state (what already fired, whether the todo plan has open items, whether the advisor is on) only filters which keys are *offered* and rides the request state.

| key | class | fires when | once per chain |
|---|---|---|---|
| `continue` | resume | cut off mid-work / truncated | no |
| `next` | resume | current item done, todo plan has open items (offered only then; the gate reads the judged agent's own plan through `tool.ReadTodoSnapshot(a.todoScope())` — project root + session id, the same address the todo tools use — never the TUI's process-global current todo session, so it works per session on the server) | no |
| `continue_todo` | resume | stopped with open todo item(s) still unstarted or in progress; continues the ready open item(s) without repeating work already present in the transcript (offered only when the plan has open items) | no |
| `validate_todo` | resume | an open todo item claims completion but shows no verification; verifies it before marking done (offered only when the plan has open items) | yes |
| `skip_todo` | resume | an open todo item is blocked or optional and the rest of the plan does not depend on it; skips it, records it in TODO.md, and continues (offered only when the plan has open items) | no |
| `retry` | resume | stopped on a transient failure (network, rate limit, flaky test) | yes |
| `fix_errors` | resume | latest output shows a failing build/tests and the reply stopped | no |
| `verify` | resume | claims done, shows no test/build run | yes |
| `review_first` | resume | large/risky diff declared done without self-review | yes |
| `write_plan` | resume | large task, no todo plan, reply is wandering | yes |
| `ask_advisor` | resume | torn between approaches, stuck, needs an answer it cannot derive, or unable to solve the current item; offered only when the advisor is enabled and installed, and at most once per chain | yes |
| `summarize` | resume | work done, no final summary | yes |
| `proceed` | answer | reply asks a yes/no go-ahead on a plan it already described; step is reversible | no |
| `skip` | answer | reply asks whether to do an optional/blocked sub-item | no |
| `reject` | answer | reply asks for a go-ahead on something off-spec or destructive | no |
| `end` | end | reply finished cleanly, or reports a blocking error | — |
| `await_user` | end | a decision only the user can make | — |

**Answer-class actions act on the user's behalf**, so they are gated three ways:

- They are admitted only with the typed reason `awaiting_confirmation`. Any other reason (including `awaiting_decision`) vetoes them: nobody asked.
- They use `autoContinueAnswerMinConfidence` (= the permission default `autoJudgeMinConfidenceDefault`, 0.80), not the 0.6 resume floor. This is the constant, not `resolveAutoJudgeMinConfidence()`: a user who raised the permission floor has said nothing about auto-answers, and a lowered permission floor must not loosen this one.
- `proceed` is vetoed when the reply's own quoted commands (fenced blocks and inline backtick spans, via `replyProposesHarmfulCommand`) are classified harmful by `IsHarmfulBashCommand` — git history rewrites, force pushes, `git stash`, destructive `rm`. `reject` on the same reply is the safe answer and stays allowed.

**Once-per-chain** actions are dropped from the offered set when their key is already in the chain history (`chain_actions_fired` in the state). The history has the same lifetime as the chain counter: the server keeps it in `runTurn` alongside `autoContinueCount`, the TUI in `m.autoContinueActions` (reset by `settleAutoContinueChain` and on new session). Each fired action counts toward `AutoContinueChainCap`.

`answer_question` (`AutoContinueAnswerQuestionAction`) is the chain-history key the host records when the question judge answers a structured `question` ask (see "Question Judge" below). It is not a row of `autoContinueActions`, so the triage judge is never offered it. It counts toward `AutoContinueChainCap` like any other fired step, and it has its own per-chain limit.

### Todo-specific actions and item prioritisation

Three resume-class actions operate on the open todo plan (`continue_todo`,
`validate_todo`, `skip_todo`). They are offered only when the plan has open
items, and the judge receives the open items as structured state
(`todo_open_items`: id, text, status), bounded to 15 items / 200 chars per
text.

The action keys stay static; the item is selected through a `todo_item` choice
question whose choices are the open item ids plus `none`. `none` is the
no-target answer: the judge must choose it for any action that is not
todo-specific, so the question never forces a pick.

The item is honoured only when it matters:

- Only `NeedsTodo` actions (`continue_todo`, `validate_todo`, `skip_todo`) read
  the item. For every other action the answer is ignored and `TodoID` stays empty.
- A valid item id (one of the judged snapshot's open items) with confidence at
  least `autoContinueMinConfidenceDefault` is recorded as `TodoID` and
  interpolated into the `{todo}` placeholder of the fixed prompt.
- A todo-specific action with no valid item (`none`, an unknown id, or a
  low-confidence pick) is **not resumed**: the decision carries the action key,
  `Resume=false`, and a detail line saying no open item was named. There is no
  placeholder-less prompt, so the agent is never told to continue or skip an
  unnamed item.

**One plan read per judge call.** `runAutoContinueJudgeTypesafe` reads the
session plan once (`tool.ReadTodoSnapshot`) and passes that snapshot to the
state (`todo_open_items`), the item question's choices and the item validation.
The judge is never validated against a plan other than the one it was shown.

The resumed prompt is sent verbatim by the hosts (server + TUI); interpolation
happens inside `runAutoContinueJudgeTypesafe` before the prompt leaves the
agent, so no host change is required.

### Key Parameters

| Parameter | Default | Description |
|---|---|---|
| `auto_continue_enabled` | `false` | Process-wide seed/default for new chat sessions — not a live global switch. Each chat session persists its own override in transcript metadata under the same key (`auto_continue_enabled`), seeded on build via `autoContinueSeed` (session override, else this default). The web sidebar toggles one session through `HandleSetAutoContinue` with `session_id`, which persists to that session's metadata and pushes a session-scoped status snapshot; a request without `session_id` keeps the legacy global-config path. |
| `auto_continue_model` | (empty) | Model used for the next-action judge decision |
| `autoContinueMinConfidenceDefault` | (constant, 0.6) | Confidence floor for resume-class actions — not a config key; decoupled from `permissions.auto.min_confidence` |
| `autoContinueAnswerMinConfidence` | (constant, 0.80) | Confidence floor for answer-class actions (`proceed`/`skip`/`reject`); equals the permission default, deliberately not the configured permission floor |
| `autoContinueSelectMinNoul` | (constant, 0.5) | Noul floor for a selection answer (a message is kept when Jev says it is at least 50% likely to bear on the request); its own constant, not the permission or verdict floor. `internal/agent/autocontinue_select_typesafe.go` |
| `autoContinueTranscriptCap` | (constant, 20) | Non-empty messages the verdict sees without a selection call; also the selection batch size. `internal/agent/autocontinue_select_typesafe.go` |
| `autoContinueSelectMaxBatches` | (constant, 8) | Selection batches one turn end may cost; the oldest unpinned messages beyond 8 × 20 are left out unscored and counted in `transcript_selection.dropped`. `internal/agent/autocontinue_select_typesafe.go` |
| `autoContinueSelectCacheCap` | (constant, 2000) | Entries in the in-memory selection score cache; overflow empties it. `internal/agent/autocontinue_select_typesafe.go` |
| `AutoContinueChainCap` | 4 (constant) | Maximum consecutive auto-fired resumes per turn — not a config key; defined in code |
| `autoContinueQuestion*` (constants) | see "Question Judge" | Caps, probability floors and noul floors of the question judge. Each is its own constant, never the permission floor. `internal/agent/autocontinue_question_typesafe.go` |

### Per-Session Gate

Auto-continue is per chat session, not per process. The gate a turn actually uses resolves in this order:

1. **Live agent** — `Agent.AutoContinueEnabled()` reads an `atomic.Bool` field owned by that agent (`autoContinueEnabled`), seeded from the session's metadata override (or the process default) at build time and flippable at runtime via `SetAutoContinueEnabled`. It never re-reads `cfg.Ocode.AutoContinueEnabled`, so a later global-config write cannot retroactively change a built session.
2. **Persisted metadata** — with no live agent, the server falls back to the session's transcript metadata key `auto_continue_enabled` (`sessionAutoContinueEnabled`, in `internal/server/autocontinue_session.go`).
3. **Process default** — a session with no override uses the process-wide `cfg.Ocode.AutoContinueEnabled` (`autoContinueFlag`).

Because the override is per session, toggling one chat never affects another: `HandleSetAutoContinue` with `session_id` (`internal/server/handler_slash_parity.go`) writes only that session's metadata and live agent, then pushes a session-scoped `status` snapshot. A request without `session_id` keeps the legacy global-config write (`config.SaveAutoContinueEnabled` + `pushStatusSnapshot`), which only seeds future sessions that never toggled.

### Typesafe Judge Path

When `auto_continue_model` is a decision-backend route (any id where `isDecisionModel` returns true — currently `typesafe/*` and Cloudflare Workers AI clef models), the judge runs via `runAutoContinueJudgeTypesafe` — a single `Decide()` call answering a typed **next action** choice (the `verdict` question, criteria = the offered action set) over the selected transcript (see "Transcript Selection and Original Request" below). This never generates text; it returns a structured decision with a confidence score. The confidence is thresholded against the floor for the action's class (see "Action Set").

The same call also answers a typed **reason** describing how the last reply ended. The closed set, in the order it is presented to the judge, is:

- `finished` — the reply completed the request; nothing is missing.
- `mid_task` — the reply stopped mid-task and work visibly remains (it says it will continue, or the task is plainly unfinished).
- `truncated` — the reply looks mechanically truncated (an unclosed code block, a cut-off sentence).
- `awaiting_confirmation` — the reply asks for a yes/no go-ahead on a plan or step it has already described in full, the step is reversible and no open design choice remains.
- `awaiting_decision` — the reply is waiting on a decision only the user can make: an open choice between approaches, an ambiguous or missing requirement, a destructive or irreversible action, or any question that is not a plain go-ahead.
- `errored` — the reply reports an error or failure and stopped.

For `finished`, `mid_task`, `truncated` and `errored` the verdict decides and the reason is advisory prose in the outcome detail. The two awaiting reasons are **decisive**:

- `awaiting_decision` vetoes every non-end action at **any** confidence; the turn ends. The veto exists because the judge sometimes reads a question-ending reply (e.g. "Which option do you want?") as unfinished mid-task work; resuming would answer the user's question on their behalf.
- `awaiting_confirmation` admits **only** the answer class (`proceed`/`skip`/`reject`). A resume-class action such as `continue` on a confirmation question is vetoed: it would be an implicit yes the judge never stated. Conversely an answer-class action with any other reason is vetoed because nobody asked.

The rubric sent to the judge (`typesafeAutoContinueInstructions`) carries this split explicitly, tells the judge never to choose `proceed` for a step that deletes data, rewrites git history, force-pushes or deploys, and lists the actions already fired in the chain.

**Since 2026-10-04**, the auto-continue judge is reached through the same `Decider` seam as the other judges. `runAutoContinueJudgeTypesafe` takes a `Decider` parameter instead of a concrete `*TypesafeClient`. The caller resolves it via `resolveDecider(slotAutoContinue)` (`internal/agent/decider.go`), which reads the `auto_continue_model` config key (unchanged; not moved or renamed). The key still accepts any decision-backend id — `isDecisionModel` is the single gate, not a hardcoded `typesafe/` prefix.

**Since 2026-10-14** the verdict is the action set above and `AutoContinueJudgeSync`/`AutoContinueJudgeAsync` take the chain history (`prior []string`) and return an `AutoContinueDecision` (`Resume`, `Action`, `Prompt`, `Detail`); the hosts send `Prompt` verbatim (todo-specific prompts are already interpolated with the prioritised item id, if any). The step-limit path bypasses the judge and fires `continue` with `AutoContinueStepLimitPrompt`.

### Transcript Selection and Original Request

The typesafe verdict does not see a fixed window of recent messages. Once a chain has an original request, the judge sees the messages that bear on it (`internal/agent/autocontinue_select_typesafe.go`, called from `runAutoContinueJudgeTypesafe`):

- **Original request.** The state carries `original_request`: the most recent genuine human message. A genuine message is user-role, not blank, not starting with `[ocode:`, and not one of the fixed resume prompts (every `autoContinueActions` prompt, a `{todo}` prompt matched by its fixed text on both sides of the placeholder with a todo id (`^t[0-9]+$`) in its place, and `AutoContinueStepLimitPrompt`). The scan runs backwards because a human turn starts a new chain; a forward scan would return the session's first request. It is capped at 4000 runes and omitted when no genuine request exists. The rubric tells the judge to choose `end` only when the whole original request is satisfied, and to pick the matching resume action while parts of it are still undone.
- **At or under 20 messages** (`autoContinueTranscriptCap`, counting only non-blank messages): every message is sent and no selection call is made.
- **Over 20:** the unpinned messages without a cached score (see "Score cache" below) are split into batches of 20. At most `autoContinueSelectMaxBatches` (8) batches run per turn end, so the OLDEST unpinned messages beyond that ceiling are left out without being scored. Each batch is one `Decide` call with one `noul` question per message, judged against `original_request` and the last reply. Batches run concurrently (at most 4 in flight, `autoContinueSelectConcurrency`), each under its own 30 s deadline. A message is kept when its noul is at least `autoContinueSelectMinNoul`. A missing or invalid answer keeps the message, the same fail-open rule as the relevance judges. Kept messages are merged back in their original order.
- **Pinned messages are always kept:** the original request and the last assistant reply, whatever their score. They are not sent for scoring.
- **Selection failure fails closed.** A batch error (transport failure or non-2xx), or a stop of the agent while batches are scoring, returns `Resume=false`, a detail naming the selection failure, and the error. Each batch is retried ONCE inside its own deadline when the call fails and neither that deadline nor the agent's stop channel is done, before the whole selection fails. The verdict is not asked on an unselected transcript, and nothing falls back to the most recent messages or to another backend.
- **Budget.** After the merge, the verdict state is measured as marshalled bytes against `decisionStateBudgetBytes`. While it is over budget, the lowest-scoring scored message is dropped (stable sort, so ties go to the earlier message; `fitTypesafeTranscript` binary-searches how many to drop). Pinned and unscored messages are never dropped. Every withheld message — a relevance veto, the batch ceiling, or a budget-fit drop — is reported to the verdict as the top-level `transcript_selection` object with `chain_messages` (the non-empty transcript length), `sent` (what the verdict got) and `dropped` (the difference); the field is absent when nothing was dropped. That is what makes the fit drop signalled rather than silent. If the state is still over budget, `prepareDecisionState` projects its bulky fields and records `_projection`, so the clipping is signalled.
- **Stop cancels selection.** Scoring batches run under the agent's stop-channel context (`stopChContext(a.StopCh())`); a Stop mid-scoring returns an error and nothing from that run is used or cached.
- **No selection slot.** There is no `auto_continue_select` slot at all: the slot constant and its no-op `slotModel` arm were removed because nothing resolved them, so `judgeSlot` has exactly ten slots. Selection uses the verdict `client` — `runAutoContinueJudgeTypesafe` → `selectTypesafeTranscript` → `scoreSelectedTranscript` — so the verdict's `auto_continue` model (`typesafe/jev-latest` by default) scores the messages, and requests are labelled with `deciderLabel(client)`, the verdict's label.
- **Score cache.** Outcomes are cached in memory on the `Agent` (`selectScores`, a `typesafeScoreCache` in `internal/agent/autocontinue_select_typesafe.go`). Each message is keyed by `typesafeHash(role, trimmed content)`, a sha256 hex, never by index, so compaction that shifts indices still hits. Only scored outcomes (keep and score) are stored. A missing or invalid answer (fail-open, unscored) is not stored, so the next call asks about it again.
- **Scope.** The cache is scoped to the pair `hash(original_request)` + `hash(last_reply)` (`scopeKey` in `scoreSelectedTranscript`), and it holds one scope at a time. A new human turn or a new last reply changes the scope, so a lookup under the new scope misses and nothing scored under the old scope is reused. The stale entries are replaced only by a successful store.
- **Writes happen only after success.** Batches are gathered first. If any batch errors, the whole selection fails, and nothing from the failed call (including successful sibling batches) is written to the cache.
- **Invalidation and bound.** A store under a new scope starts from an empty map. So does a store that would push the cache past `autoContinueSelectCacheCap` (2000 entries). On overflow the entries this call just hit are dropped too, so the next call rescores them. The TUI calls `Agent.ResetSelectScores` at four points, all of which only release memory (scope already prevents reuse): TUI startup (`newModel`), session load (`handleSessionCmd`), `/new` and `/clear` (`handleNewCmd`, beside `ClearGoal`), and `/reset-id` (`handleResetIDCmd`, beside `RekeyOpenCodeSession`). Each empties the cache. The cache lives in memory only, so a restart rescores the transcript once.
- **Not cached.** Clipped text, and the output of `fitTypesafeTranscript` and `prepareDecisionState`, which are recomputed every call. A fit drop is signalled by `transcript_selection`; a `prepareDecisionState` clip by `_projection`. Pinned messages are never scored, so they are not cached.
- **Known tradeoff.** Because the scope includes the last reply, a score is never reused against a different last reply, which keeps it correct. The cost is that every new reply rescores the unpinned transcript (see "Cost"). Reuse happens only within one unchanged last reply.
- **Cost.** Over the cap, each judge call whose last reply is new scores every unpinned message again, in batches of 20: `ceil((n - 2) / 20)` `Decide` calls for `n` non-empty messages, with up to 4 in flight. Cache hits save calls only when the same original request and last reply are judged again.
- **Known limit.** AT or under the transcript cap nothing is scored, so nothing is droppable and `prepareDecisionState` projects the state instead (`_projection`; about 168 KB before projection, about 45 KB after, pinned by `TestSharedStateBudget_WorstCaseProductionPayloads`). OVER the cap, `fitTypesafeTranscript` drops the lowest-scored messages to fit (measured 176,806 B / 20 markup messages → 92,706 B / 10 messages), and that drop is signalled by `transcript_selection`, not `_projection`. A bare confirmation ("yes"/"continue") becomes `original_request`, so the rubric line that refuses `end` while the request is undone is inert there — a known limitation, not a regression. Tracked in TODO.md.

### Chat Judge Path

When `auto_continue_model` is anything other than a decision-backend route, the judge runs via `runAutoContinueJudge` (`internal/agent/agent.go`): the transcript tail is sent to the configured chat model as one user message listing the offered action keys with their criteria, and exactly one key is expected back (first word, trailing punctuation stripped). Any error, empty response, or word that is not an offered key fails closed to **end**. A bare `YES`/`NO` is no longer an accepted answer.

The chat judge is **never offered the answer class**: it returns no typed reason, so there is nothing to gate an auto-answer with. Its prompt tells it to answer `await_user` when the reply is waiting on the user — "if it ends by asking a question, or requests clarification, confirmation, feedback, approval, or a decision, answer await_user — the user must respond first. A reply that ends in a question is not a cut-off mid-task reply." Once-per-chain filtering applies the same way, and the prompt names the actions already fired.

### Question Judge (structured `question` asks)

When the main agent pauses on a structured `question` tool call (`QuestionPrompt`, `internal/tool/misc.go`), a separate question judge may answer it on the user's behalf. It answers only when the transcript already determines the answer. Prose options in a reply ("1. … 2. …", "A) B)") are not handled: they stay `awaiting_decision` and end the turn as before.

- **Server gate.** `autoContinueAnswerQuestion` (`internal/server/agent_session.go`) runs at the top of `autoContinueShouldResume`, before the outstanding-ask gate. It needs auto-continue on or an evaluable `/goal`, a chain below `autoContinueChainCap`, no child asks, no permission ask at the tail, and the question ask as the last message. No earlier question in the same tool round may be unresolved, because only the tail can be rewritten.
- **TUI gate.** `maybeDispatchQuestionJudge` runs from the `streamDoneMsg` branch when the turn ended paused on the dialog. `questionJudgeEligible` (`internal/tui/question_prompt.go`) also requires no RC-owned prompt, no permission dialog, an empty ask queue, and a dialog the user has not touched.
- **Entry points.** `AutoContinueQuestionJudgeSync` serves the server. `AutoContinueQuestionJudgeAsync` serves the TUI and reports through `Agent.OnAutoContinueQuestionJudge`. The async path takes `autoContinueJudgeMu` with `TryLock`, shared with the triage judge. The judge uses the auto-continue decision backend (`autoContinueJudgeClientTyped`). A chat model in `auto_continue_model` makes no call, because the chat path has no typed probabilities.
- **One call, one mapping.** Each prompt gets an owner choice, `determined_by_transcript` or `user_preference`. A single-select prompt gets an answer choice over its answerable options plus `defer_to_user`. Choice keys are identifiers, `opt0`, `opt1`, … in prompt order (`questionAnswerableChoices`), never label text. The label reaches Jev in the criteria description (`<label>: <description>`, or the label alone), and `questionChoiceLabel` maps the picked key back to the label the answer carries. `defer_to_user` is reserved, so a prompt that uses it is vetoed. A multi-select prompt gets one noul per option plus a defer noul; option combinations are never enumerated. Custom "other" options are never offered. `tool.QuestionOptionIsCustom` (`internal/tool/misc.go`) is the one definition of that detection, shared with the TUI.
- **Floors.** Each is its own constant. Single-select answer probability and owner probability use `autoContinueQuestionMinProbability` and `autoContinueQuestionOwnerMinProbability` (both 0.80). Multi-select options use `autoContinueQuestionSelectNoul` (0.85) and `autoContinueQuestionRejectNoul` (0.15), and an option between those two is deferred. The defer noul uses `autoContinueQuestionDeferNoul` (0.50).
- **Caps.** `autoContinueQuestionMaxPrompts` (3) and `autoContinueQuestionMaxOptions` (6) bound the request size. `autoContinueQuestionMaxPerChain` (2) bounds the `answer_question` entries in one chain.
- **Pre-call vetoes.** No decision call is made when the ask has zero or too many prompts, a prompt has zero or too many answerable options, a label is empty, duplicated or reserved, the chain is at its per-chain limit, or the reply proposes a harmful command. The same veto applies when any header, question, label or description contains a risk word (`autoContinueQuestionRiskWords`) or quotes a command `IsHarmfulBashCommand` flags. A repeat of an earlier question in the transcript is vetoed too, even when a human answered it. Only a tool row that answers a `question` tool call counts (`questionAlreadyAsked`, `internal/agent/autocontinue_question_typesafe.go`), so another tool's JSON that carries a question field never vetoes.
- **Post-call checks.** Every consumed answer must pass `validateAnswer`, and a rejection is a deferral. The owner must be `determined_by_transcript` at its floor. A single-select answer must clear its floor and must not be `defer_to_user`. A multi-select answer is accepted only when the defer noul is below its floor, every option is decisively in or out, and at least one option is in.
- **All or nothing.** If any prompt defers, nothing is answered, so the user never sees a partial dialog.
- **Server application.** `applyQuestionAnswer` (`internal/server/handler_questions.go:93`) rewrites the tail tool result in both `messages` and `as.messages`, called from `runTurn` (`internal/server/agent_session.go:1509`). The model therefore reads the same shape a human answer produces. The host broadcasts `question_resolved`, then a `messages` frame with the rewritten rows, as `HandleAnswerQuestion` does, so the web transcript drops the question card at once and a mid-turn reload does not reopen the dialog. It then appends a `↩ auto-continue` notice, counts the step, and continues with no user-role prompt. A deferral appends no notice, and neither does an answer that fails to apply: a `json.Marshal` error or an `applyQuestionAnswer` mismatch is logged only, because a row after the open ask would move the ask off the tail.
- **Answers during the judge call.** `runTurn` holds `as.mu` across the synchronous judge call, while the dialog is already on screen. The question answer, question dismiss and permission resolve handlers find the session through `findPendingSession`, which waits up to `pendingSessionLockWait` (3 s) for the lock. A session still busy at that bound gets HTTP 503 with `Retry-After: 1` (`writeSessionBusy`), never 404 or 409: the web dialog dismisses itself on 404 and 409, but keeps itself open with an error on 503 so the user can retry. Lock rules: `docs/concepts/web-server-locking-and-liveness-rules.md`.
- **TUI application.** The matched option labels go through the normal `submitQuestionAnswers()` path, after `questionJudgeStillApplies` (`internal/tui/model.go:5890`) and label mapping in `questionJudgeSelections` (`internal/tui/question_prompt.go`). The tool result is byte-identical to a human answer. RC-owned prompts are never answered.
- **Goal interaction.** The judge resolves the ask before the goal loop steps again, so the pending-ask rule for goals holds. A deferred ask keeps the goal paused on the dialog.
- **Known limits.** Prose options are not handled. RC-bridged TUI prompts are not auto-answered. The defer reason reaches only the server log and the TUI hint, not the web dialog.
- **Tests.** `internal/agent/autocontinue_question_typesafe_test.go` covers the mapping, vetoes, floors and the request-size bound. `internal/server/agent_session_autocontinue_test.go` covers `TestRunTurnAutoContinueAnswersDeterminedQuestion`, `TestRunTurnAutoContinueDefersPreferenceQuestion` and `TestRunTurnAutoContinueQuestionJudgeRunsUnderGoal`. `internal/tui/autocontinue_test.go` covers `TestQuestionJudgeFinishedSubmitsMatchedLabels` and `TestQuestionJudgeEligibleGuards`. `internal/agent/autocontinue_question_typesafe_test.go` also covers `TestAutoContinueQuestionForeignFAQJSONDoesNotVeto`, and `internal/server/agent_session_autocontinue_test.go` covers `TestRunTurnAutoContinueAnswerBroadcastsRewrittenRow`. `internal/server/handler_questions_test.go` covers `TestFindPendingSessionReportsBusyWithinBound` and `TestHandleAnswerQuestionBusySessionIs503`. The fixture `questionDeterminedVerdictJSON` answers with the `opt0` key, not the label.

### Awaiting-User Stop Coverage

The decisive stop is pinned by mutation-verified tests (each fails against the pre-fix code):

- `internal/agent/autocontinue_typesafe_test.go` — `TestAutoContinueTypesafeAwaitingUserVetoesContinue` (a `continue` verdict with the `awaiting_decision` reason does not resume) and `TestAutoContinueGenericJudgePromptCoversAwaitingUser` (the chat-judge prompt carries the instruction).
- `internal/agent/autocontinue_actions_test.go` — the action-set gating: `proceed` resumes only with `awaiting_confirmation` and at the 0.80 floor, `continue` on a confirmation is vetoed, `awaiting_decision` vetoes every non-end key, `proceed` is vetoed on a harmful quoted command while `reject` passes, `ask_advisor`/`next` are offered only when available, the `next` gate reads the judged session's own plan file (a sibling session's open item or a corrupt file never offers it), once-per-chain keys are filtered, and the chat judge parses keys and is never offered the answer class.
- `internal/server/agent_session_autocontinue_test.go` — `TestRunTurnAutoContinueAwaitingUserVetoesContinue` (full `runTurn` chain emits no resume prompt) and `TestRunTurnAutoContinueConfirmationProceeds` (a `proceed` verdict sends the fixed proceed prompt and records the action).
- `internal/tui/autocontinue_test.go` — `TestAutoContinueJudgeFinishedFiresActionPrompt` (the TUI sends the verdict's own prompt and tracks the chain history).

Note: when `auto_continue_model` is set but `auto_continue_enabled` is `false`, the TUI `/autocontinue model <name>` command and the web `/autocontinue model <name>` command both print a warning reminding the user to run `/autocontinue on` (or flip the sidebar toggle) to actually enable the feature.

### `/goal` Precedence

A session with an evaluable `/goal` (active or paused) gives the natural-stop decision to the goal judge, so the triage judge above is not consulted for that turn. The order in `runTurn` is:

1. The step-limit hard resume, unchanged. It counts against `autoContinueCount`.
2. The goal judge (`GoalJudgeSync`), run only when the tail is not a pending permission or question ask. A `not_met` verdict appends a `[ocode:goal]` user prompt and resumes. A goal resume does **not** advance `autoContinueCount`.
3. The triage judge, reached only when no goal is evaluable.

The goal lives in the session metadata under the `goal` key. It is written after every judged turn and by the `/api/sessions/{id}/goal` endpoints, and restored in `buildAgentSession`. A human message reactivates a paused goal. Engine: `internal/agent/goal.go`. Server host: `internal/server/handler_session_goal.go`.

## End-of-Turn Surfacing

The auto-continue loop does **not** set a `auto_continue` status field on the turn result. Instead, the termination reason is surfaced through two mechanisms:

**Server (headless/web):** An assistant transcript message with empty `Content` and a `Notice` string (via `appendTranscriptMessage` in `internal/server/agent_session.go`). The notice states why the loop resumed or stopped.

**TUI:** A transient transcript hint via `agentAutoContinueDeclineDetail` (`internal/tui/model.go`), which returns a hint for each decline case:
- auto-continue is disabled but the step limit was hit.
- the chain cap is exhausted, so the user must resume manually.

A judge verdict line is rendered inline by the judge's `Detail` string, not as a status field.

## Design Constraints

- **Bounded chain only** — no unbounded loops; `AutoContinueChainCap` is enforced server-side.
- **Judge is advisory, with two decisive reasons** — a low-confidence or error result always stops the loop, and the verdict decides for every triage reason except the awaiting pair: `awaiting_decision` vetoes every non-end action at any confidence, and `awaiting_confirmation` admits only an explicit typed answer (`proceed`/`skip`/`reject`) at the permission-grade floor.
- **Every resume prompt is fixed text keyed by action** — no judge composes prose; a new action is a new row in `autoContinueActions`, never a per-turn string.
- **Naturally-ended turns only** — both judges are consulted only for a turn that ended on its own. A step-limit cutoff resumes deterministically without triage, and a pending permission sentinel blocks resume at the dispatcher (`autoContinueShouldResume` in `internal/server/agent_session.go`). The question judge is the one exception: it runs on a turn paused on a structured question ask and only answers that ask (see "Question Judge").
- **Permission-aware** — auto-continue refuses to resume past an unresolved permission ask, or a question ask the question judge has not answered. The agent can raise asks mid-chain, but the loop stops until they are resolved.
- **Question answers only from transcript-determined choices** — the question judge never picks from prose options, never uses the chat judge, and never answers without a decision backend. A deferred prompt leaves the whole ask to the user.
- **Per-session gate** — the on/off switch belongs to one chat session: each session's override lives in its transcript metadata, and the process-wide config is only the default for sessions that never toggled.
- **Surfaced in transcript** — auto-continue outcomes are rendered as a transient TUI hint or a server transcript notice, not as an agent-status field.
