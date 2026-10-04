---
type: Gotcha
title: Advisor checkpoints must wait for a pending permission/question ask
description: Advisor loop checkpoints must skip (not block or run) while a permission/question ask dialog is pending; root cause, invariants, and scope limits — including that the headless server now installs a real sub-agent/advisor permission asker (bounded, auto-denied park).
tags:
  - advisor
  - permissions
  - asks
  - agent-loop
  - gotcha
timestamp: 2026-10-03T00:25:54Z
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
3. **SKIP, never block.** The advisor runs on the loop goroutine, so blocking would park the turn on a human — the same hang class as pinning an HTTP connection for a turn. Only the second-model review is deferred; the turn is untouched. **This invariant is scoped to the CHECKPOINTS.** A permission ask the advisor itself raises (see the scope limits below) is a different, deliberate park, not a violation of it: the dialog is answerable while the parked turn holds `as.mu`, the park is bounded by a 10-minute timeout, and an unanswered ask is auto-denied rather than skipped.
4. A skip must NOT consume the checkpoint. The state is rebuilt per `Step`, so the completion review still runs on the continuation after the last dialog is answered.
5. `tool.UnansweredAsk` (in `internal/tool/ask_sentinel.go`) is the SINGLE "is a dialog open" predicate. It lives in `internal/tool`, NOT `internal/session`, because `internal/session` imports `internal/agent` — the agent package cannot import it back without a cycle. It requires the `SentinelQuestionPrompt` PREFIX, not merely a `WAITING_FOR_USER_RESPONSE` substring (which any grep hit in a tool result would satisfy). `Step`'s pause check and `internal/session/transcript_tail.go`'s `unansweredAsk` delegate to it. `internal/session/session.go`'s `isAskSentinel` / `isIncompleteToolResult` answer DIFFERENT questions and were deliberately left alone.
6. `messagesHavePendingAsk` walks BACKWARDS and stops at the first assistant row. Two non-obvious reasons:
   - Without that stop, one permission the user ignored (typing a new message instead of clicking Allow) leaves a stale sentinel and mutes the advisor for every later turn.
   - It must NOT start from "the last user message": the tail injectors (`injectTodoTail`, `injectDirMDTail`, `injectLSPDelta`) append user-role rows AFTER the ask, so such a scan would miss the very ask it is looking for.

## Deliberate scope limits (tracked in TODO.md)

- The model-invoked `advisor` TOOL is not gated — only the two loop checkpoints. It runs mid-batch where it cannot see the message list (`Agent` has no message-history getter).
- **The advisor's OWN out-of-scope tool call now raises a REAL permission dialog** — it used to get the inert `PERMISSION_ASK:` sentinel, which the advisor model cannot act on. Two things landed: `buildAgentSession` (`internal/server/agent_session.go`) installs the headless server's asker with `ag.SetSubAgentPermAsker(h.newServerSubAgentAsker(sessionID, as))`, and `internal/agent/advisor_tool.go` propagates the parent's asker into the freshly built advisor agent (`advisorAgent.OnPermissionAsk = attributePermAsker(t.mainAgent.subAgentPermAsker, advisorAgentName)`, plus `SetSubAgentPermAsker` so the advisor's own children inherit it). The ask is broadcast on the `permission` SSE frame — the same event a main-agent ask uses, so web/desktop renders the usual dialog — and it **blocks the advisor's goroutine** until answered. The park is bounded: `childPermAskTimeout` (`internal/server/child_perm_asks.go`) is 10 minutes, after which the ask auto-denies and broadcasts `permission_resolved` (parent cancel/stop auto-denies immediately; a missing or duplicate registry entry denies rather than parks). Answering never needs `as.mu`: the ask lives in the per-session `as.childAsks` registry, which has its own mutex and never takes the turn lock, so the dialog stays answerable while the parked turn holds it. Covering tests: `internal/server/child_perm_asks_test.go`.
- **Scope of that install site.** `buildAgentSession` is the only place the server installs the asker, so it applies exactly where `buildAgentSession` runs — the headless web/desktop server. The TUI installs its own asker (`internal/tui/model.go`), ACP installs its own `OnPermissionAsk` (`internal/acp/bridge.go`), and `runcli`'s is opt-in (installed only for `--yolo` / `--dangerously-skip-permissions`; with neither flag the callback stays nil and the sentinel path runs). An RC-bridged session does reach `buildAgentSession` for status/state reads, but its AGENT is owned and stepped by the TUI — and the TUI installs its own asker on the agent it owns — so the server asker is never invoked on a bridged turn.

Design record: `superpowers/specs/2026-10-02-subagent-permission-ask-design.md`.

## Validation

5 tests in `internal/agent/advisor_pending_ask_test.go`, mutation-verified (0 survivors). `go build ./...` clean; `internal/agent`, `internal/session`, `internal/tool`, `internal/server`, `internal/tui` suites green.

## Anchor note

`internal/agent/agent.go` was changed as a deliberate SAME-LINE replacement (2 insertions / 2 deletions, net zero lines) so no existing anchor into it drifts. If you cite `agent.go` line numbers, verify each by printing the real source line; same for `internal/agent/advisor_checkpoint.go`. Prefer naming symbols over line numbers here — this page deliberately cites no `file.go:NNN` anchors (adding any to `agent_session.go` or `advisor_tool.go` would drift on the next edit above them).