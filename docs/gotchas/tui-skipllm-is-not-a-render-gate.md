---
type: Gotcha
title: 'TUI: skipLLM is not a render gate — fake-agent and cron replies vanish on fresh sessions'
description: 'Gotcha: renderTranscript in internal/tui/model.go used skipLLM as a "should this render?" predicate, hiding assistant output (fake-agent replies, cron deliveries, LLM errors) on fresh sessions where every message was transient or a user echo. Fix: gate on transient + isCommandHistoryMessage only; skipLLM must never drive rendering.'
tags:
  - gotcha
  - tui
  - skipLLM
  - render
  - fake-agent
  - cron
  - transient
  - model.go
  - commands.go
timestamp: 2026-09-22T05:28:24Z
---
# TUI: skipLLM is not a render gate — fake-agent and cron replies vanish on fresh sessions

**Type:** Gotcha  
**Description:** TUI `/fake-agent` produced no visible feedback on a fresh session; cron deliveries were also hidden. Root cause: `renderTranscript` used `skipLLM` (a prompt-inclusion flag) as a "should this render?" predicate, blanking the viewport when every message was transient or a user echo.  
**Resource:** `internal/tui/model.go` (`renderTranscript`), `internal/tui/commands.go` (`runFakeAgentCmd`), `model.go` (cron deliveries, LLM transport errors, slash-echo sites)  
**Tags:** gotcha, tui, skipLLM, render, fake-agent, cron, transient, model.go, commands.go

---

## The Problem

On a brand-new TUI session, `/fake-agent` switched the model and persisted the harness change, but the confirmation reply never appeared on screen. Cron deliveries had the same silent failure. LLM transport errors were visible only by accident.

Root cause in `renderTranscript` (`internal/tui/model.go`, ~line 17355):

```go
hasRealContent := false
for _, msg := range m.messages {
    if !msg.transient && !msg.skipLLM { hasRealContent = true; break }
}
if !hasRealContent { ...m.viewport.SetContent("") or pipboy/LCARS art...; return }
```

`skipLLM` means **"exclude from the LLM prompt"**, NOT "do not render". It is set on assistant-side user-facing output:

| Site | File:Line |
|------|-----------|
| `runFakeAgentCmd` replies | `internal/tui/commands.go:1103,1108,1112,1116,1119` |
| Cron deliveries | `model.go:4767` |
| LLM transport errors | `model.go:5143` |
| User slash-echo (×2) | `model.go:8651,8774` |

On a fresh session every message is either transient ("Started new session.") or the user's own `/x` echo, so the gate evaluated `hasRealContent = false`, blanked the viewport, and returned — hiding the assistant reply (and any other `skipLLM` assistant output). LLM errors escaped only because a preceding non-`skipLLM` user message happened to exist.

## The Fix

Exclude only chrome (transient notices and the user's own slash echo) and count assistant output even when `skipLLM`:

```go
for _, msg := range m.messages {
    if msg.transient { continue }
    if msg.role == roleUser && isCommandHistoryMessage(msg) { continue }
    hasRealContent = true; break
}
```

## Why This Is Subtle

`skipLLM` lives in a namespace that tempts render-layer code to reuse it. It is a **prompt-inclusion** flag (the agent loop uses it to decide what to send to the LLM). The render layer's job is to show everything the user should see, which is a different question. Conflating the two means any new message category that is user-facing but must stay out of the prompt (by design) will silently disappear on sessions where the only other messages are transient or user echoes.

## Rule

> **In the TUI render layer, never use `skipLLM` as a "should this render" predicate.** It only governs prompt inclusion. Rendering chrome = `transient` OR (roleUser AND `isCommandHistoryMessage`). Any new message category that is user-facing but must stay out of the prompt (`skipLLM`) will render correctly only because the gate ignores `skipLLM`.

## Tests

- `internal/tui/command_test.go` — `TestFakeAgentFeedbackVisibleOnFreshSession` (fails at the old predicate)
- `internal/tui/command_test.go` — `TestFreshSessionStillRendersEmptyState` (ensures genuine empty sessions still show chrome)
- `internal/tui/command_test.go` — `TestCronDeliveryVisibleOnFreshSession` (fails at the old predicate)

All three are mutation-verified (reverting the predicate fails #1 and #3). Full `internal/tui` suite passes; `go build ./...` clean; `gofmt` clean.

## Detection

- `/fake-agent` or `/<command>` appears to do nothing on a brand-new session (the harness switched, but no confirmation renders).
- Cron deliveries are invisible on fresh sessions.
- LLM transport errors are inconsistently visible — they appear only if some earlier non-skipLLM message exists in the session.
