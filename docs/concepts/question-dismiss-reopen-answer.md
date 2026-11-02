---
type: Concept
title: Question dismiss, reopen and answer
description: The three states of a question tool result, who may move between them, the trailing-round gate on server and client, and why reopening a dismissed prompt is client-local.
resource: internal/server/handler_questions.go
tags:
  - chat
  - asks
  - questions
  - server
  - web
timestamp: 2026-11-02T04:00:00Z
status: active
---
A `question` prompt has three states, and the server stores each one in the same tool-result row. A user can dismiss a pending prompt, and since 2026-11-02 can reopen the dismissal from its transcript card and answer it. Reopening is a client-only step. The server only needs to accept the answer.

## The three states

- **Pending.** The tool result is the `QUESTION_PROMPT:` sentinel. `isQuestionAsk` recognises it.
- **Dismissed.** The tool result is the plain prose `tool.QuestionDismissedResult`. `isQuestionDismissed` recognises it. Plain prose survives a session load, so a dismissal persists across restarts.
- **Answered.** The tool result is the answers JSON, written by `applyQuestionAnswer`. Answered is terminal.

## Who may transition

| From | To | Entry point | Gate |
| --- | --- | --- | --- |
| pending | dismissed | `HandleDismissQuestion` (`POST /api/questions/cancel`) | sentinel in the trailing tool round |
| pending | answered | `HandleAnswerQuestion` (`POST /api/questions`) | pending or dismissed in the trailing tool round |
| dismissed | answered | `HandleAnswerQuestion` | same as above |

A second dismissal and an answer to an already answered row both return 404, because the row no longer matches the lookup predicate. A 409 is returned only when the row changes between lookup and apply. The pulse assistant's write tools reuse both handlers, so it can dismiss a pending prompt and answer a dismissed one.

## Gates on both sides

- **Server.** `findPendingSession` scans the trailing tool round, found with `trailingToolRunStart`, and returns the session with `as.mu` held. There is no explicit `turnActive` check on the answer path. A running turn holds `as.mu`, so the answer waits for the turn and then re-checks the tail under the lock. A user message or any later row ends the tool round, which ends the window in which a dismissal can be answered.
- **Client.** `dismissedQuestionFor` in `ChatPanel` attaches a reopen only when the dismissal sits at or after the index from `trailingToolRunStart` in `chatStore`. The `Answer question` button is hidden while `turnActive` is set.

## Why reopening is client-local

The server has no reopen transition. A reopened prompt is a `QuestionRequest` with `reopened: true`, built from the assistant tool call's arguments by `questionsFromToolCallArgs`, and it lives only in the browser's `pendingQuestion`. Four consequences follow:

- `hideQuestion` and `cancelQuestion` in `useChat` close a reopened prompt by dispatching `QUESTION_RESOLVED` without an API call. The transcript card then shows the reopen button again.
- `AttentionSoundBridge` ignores reopened prompts.
- `SET_MESSAGES` and `MERGE_SNAPSHOT` keep a reopened `pendingQuestion`, because the transcript cannot re-derive it.
- Submitting uses the unchanged `submitQuestionAnswers` and `POST /api/questions`. The `QUESTION_ANSWERED` echo rewrites a dismissed result as well.

## Restart and load

When `HandleAnswerQuestion` receives a `session_id`, it calls `getOrCreateAgentSession` before the lookup. That loads the session from disk through `session.LoadForDir`, so an answer works after a server restart. A load failure returns 404. Pending sentinels are a different case, covered by [pending-ask recovery](../gotchas/pending-ask-recovery-live-session-state.md).

A dismissed tail reads as stopped, not interrupted, as described in [interrupted turn notice](interrupted-turn-notice.md). Answering it runs a normal continuation.

## Known gap

In `/rc` bridge mode the server forwards the answer to the TUI. The TUI matches a resolution only against `m.rcPendingQuestion` (the `rcResolveMsg` branch in `internal/tui/model.go`), which is nil after a dismissal, so it drops the answer. This is tracked in `TODO.md`.

## Tests

- `TestApplyQuestionAnswerAcceptsDismissedPrompt`
- `TestHandleAnswerQuestionAfterDismissResolvesAndContinues`
- `TestHandleAnswerQuestionAfterDismissSupersededIs404`
- `TestHandleAnswerQuestionAfterDismissLoadsSessionFromDisk`

The last test asserts only that the session is loaded.
