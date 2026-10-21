---
type: Concept
title: Pending ask suppresses advisor, auto-continue, and goal-resume
description: A permission or question ask (local dialog, queued multi-ask round, parked sub-agent ask, or RC/Telegram remote approval) now suppresses the advisor checkpoints, the auto-continue resume judge, and the /goal resume judge. The suppression relies on an explicit host-declared flag (Agent.SetPendingAsk) OR'd with the transcript scan, and a server-side sessionHasOutstandingAsk() that checks both the transcript tail and the child-ask registry.
tags: [permissions, asks, agent-loop, advisor, auto-continue, goal, server, tui, gotcha]
timestamp: 2026-10-20T23:10:22Z
---

# Pending ask suppresses advisor, auto-continue, and goal-resume

## What changed

A behavioral fix prevents the advisor plan/done checkpoints, the auto-continue resume judge (`autoContinueShouldResume`), and the goal-resume judge (`goalShouldResume`) from firing while any permission or question ask is open — whether it is shown in the TUI dialog slot, queued behind a multi-ask round, parked as a sub-agent ask, or pending as a remote (RC/Telegram) approval.

Once the ask is answered, all three resume normally; there is no permanent disablement.

## Why it failed before

The TUI's `buildAgentMessagesSnapshot` strips `PERMISSION_ASK` / `QUESTION_PROMPT` sentinel rows from the transcript before calling `Agent.Step`. That strip is deliberate — a raw sentinel in the LLM input is a malformed tool-call/tool-result pairing — but it also hid the ask from the agent's own "is a dialog open" scan (`messagesHavePendingAsk`). A continuation `Step` could therefore start a second model (advisor, minutes-long Claude Code backend) or trigger an auto-continue/goal resume underneath a dialog the user had not answered.

The gap was wider for queued asks and sub-agent asks: a queued ask (not yet shown) or a parked sub-agent ask does not appear in the trailing tool run, so `pauseAfterResults` did not see it at all.

## The fix — explicit host-declared flag

- `Agent.SetPendingAsk(bool)` and `Agent.PendingAsk()` declare the ask explicitly from the host.
- The TUI calls the setter at every ask-state change (`showPermDialog` clear/enqueue/promote, queued permission ask enqueue/promote (`permAskQueue`), queued question prompt enqueue/promote (`questionAskQueue`), parked sub-agent ask (`pendingSubAgentResp`), and RC/Telegram remote-approval ask (`rcPendingPerm`, `rcPendingQuestion`)) and before every `Step` (`askAgent`, `sendCustomCommandPrompt`).
- The agent's checkpoint gate (`newAdvisorCheckpointState`) ORs the transcript scan with the live host flag; `blockedByPendingAsk` checks the live flag at checkpoint time (not only at `Step` start), so a dialog raised mid-turn also stands the checkpoint down.
- The flag does NOT consume the checkpoint — a skipped checkpoint stays armed for the next continuation `Step`.

## The fix — widened blocking predicates

- `queueDrainBlocked()` was widened from a slot-only check to `anyAskPending()` (slot + queued asks + sub-agent ask + RC/Telegram). This blocks queued-input drain and background-job resume too.
- The server introduces `Handler.sessionHasOutstandingAsk()`, which checks both the transcript tail (`tailIsPermissionAsk` / `tailIsQuestionAsk`, the main agent's own ask) and the child-ask registry (`as.childAsks.list()`, a parked sub-agent ask). It is used by `autoContinueShouldResume` and `goalShouldResume`, so neither resumes behind the user's back.

## Related mechanism

The advisor tool (`ask_advisor` in `internal/agent/autocontinue_typesafe.go`) is one of the auto-continue actions; this gate closes the path where an auto-continue verdict would start the advisor under an open dialog.
