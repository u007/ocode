---
type: Gotcha
title: Advisor checkpoints must wait for a pending permission/question ask
description: Advisor loop checkpoints must skip (not block or run) while a permission/question ask dialog is pending; root cause, invariants, and scope limits.
tags:
  - advisor
  - permissions
  - asks
  - agent-loop
  - gotcha
timestamp: 2026-10-02T02:08:26Z
status: active
---

# Advisor checkpoints must wait for a pending permission/question ask

## The bug

With the advisor module enabled, a turn that paused on a permission request could still start an ADVISOR call while the dialog was open. The advisor is a second model running **synchronously on the agent loop** (minutes on the Claude Code CLI backend), so the turn went silent behind a modal the user had not answered.

## Root cause

- A single tool-call round can pause on **MORE THAN ONE** unresolved ask: parallel dispatch runs every call before the pause check. The desktop/web app renders one dialog per sentinel.
- `Handler.HandleResolvePermission` and `Handler.HandleAnswerQuestion` each resolve exactly ONE ask by `ToolID`, then call `as.agent.Step(working)` unconditionally. Answering one dialog therefore started a fresh turn while the other was still pending.
- The `plan` checkpoint was the one that slipped through: its only guard was the `pauseAfterResults` early return in `Step`, which sees only asks raised by the batch THIS iteration executed. A continuation `Step` appends its own results after the pending row, so the ask is no longer in the trailing tool run.

## Invariants worth keeping

1. `advisorCheckpointState.pendingAsk` is seeded once per `Step` from `messagesHavePendingAsk`; both checkpoints stand down while it is set via `blockedByPendingAsk`, which emits an `ADVISOR` debug line saying why.
2. The flag is computed ONCE BEFORE the loop and that is provably sufficient: every tool row `Step` appends goes through the `results` slice, and a sentinel row in `results` sets `pauseAfterResults` and returns — so an ask can never appear mid-`Step`. Only a continuation `Step` can START with an ask row.
3. **SKIP, never block.** The advisor runs on the loop goroutine, so blocking would park the turn on a human — the same hang class as pinning an HTTP connection for a turn. Only the second-model review is deferred; the turn is untouched.
4. A skip must NOT consume the checkpoint. The state is rebuilt per `Step`, so the completion review still runs on the continuation after the last dialog is answered.
5. `tool.UnansweredAsk` (in `internal/tool/ask_sentinel.go`) is the SINGLE "is a dialog open" predicate. It lives in `internal/tool`, NOT `internal/session`, because `internal/session` imports `internal/agent` — the agent package cannot import it back without a cycle. It requires the `SentinelQuestionPrompt` PREFIX, not merely a `WAITING_FOR_USER_RESPONSE` substring (which any grep hit in a tool result would satisfy). `Step`'s pause check and `internal/session/transcript_tail.go`'s `unansweredAsk` delegate to it. `internal/session/session.go`'s `isAskSentinel` / `isIncompleteToolResult` answer DIFFERENT questions and were deliberately left alone.
6. `messagesHavePendingAsk` walks BACKWARDS and stops at the first assistant row. Two non-obvious reasons:
   - Without that stop, one permission the user ignored (typing a new message instead of clicking Allow) leaves a stale sentinel and mutes the advisor for every later turn.
   - It must NOT start from "the last user message": the tail injectors (`injectTodoTail`, `injectDirMDTail`, `injectLSPDelta`) append user-role rows AFTER the ask, so such a scan would miss the very ask it is looking for.

## Deliberate scope limits (tracked in TODO.md)

- The model-invoked `advisor` TOOL is not gated — only the two loop checkpoints. It runs mid-batch where it cannot see the message list (`Agent` has no message-history getter).
- The server never installs `subAgentPermAsker` (only the TUI does), so the advisor's OWN out-of-scope tool call gets the inert sentinel instead of a dialog.

## Validation

5 tests in `internal/agent/advisor_pending_ask_test.go`, mutation-verified (0 survivors). `go build ./...` clean; `internal/agent`, `internal/session`, `internal/tool`, `internal/server`, `internal/tui` suites green.

## Anchor note

`internal/agent/agent.go` was changed as a deliberate SAME-LINE replacement (2 insertions / 2 deletions, net zero lines) so no existing anchor into it drifts. If you cite `agent.go` line numbers, verify each by printing the real source line; same for `internal/agent/advisor_checkpoint.go`. Prefer naming symbols over line numbers here.
