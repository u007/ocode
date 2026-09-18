---
type: Gotcha
title: 'Auto-continue turn transcript rebase: capture base length before the loop'
description: 'Gotcha: turn transcript persistence silently fails if turnBaseLen is not captured before the auto-continue loop, because resume prompts grow the messages length and cause hard-diverge on reconcile.'
tags:
  - gotcha
  - auto-continue
  - transcript
  - reconcile
  - rebase
  - persistence
  - server
timestamp: 2026-09-17T14:25:49Z
---
# Auto-continue turn transcript rebase: capture base length before the loop

## The Problem

In a bounded auto-continue loop (e.g. `runTurn` in `agent_session.go`), each iteration appends a resume prompt and the model's continuation response to the in-memory transcript (`as.messages`). When the turn ends, `persistTurnTranscript` must reconcile this turn's output against the persisted session transcript on disk — it needs to know which rows belong to *this turn* versus rows another writer appended concurrently.

The bug: if `len(messages)` is captured **after** the auto-continue loop has already appended resume prompts, the "base length" is wrong. The reconcile logic interprets the grown slice as the turn's starting point, which mismatches the actual on-disk prefix. The result is **silent hard divergence** — the reconcile drops mid-turn notices and resume prompts from memory, and every subsequent save conflicts forever.

## The Fix

Capture `turnBaseLen := len(messages)` **before** the auto-continue loop begins. Pass this pre-loop base length to `persistTurnTranscript` so the reconcile correctly identifies the turn's suffix (everything after the user message) versus the base (everything through the user message).

```go
// turnBaseLen is the turn's base transcript (everything through this
// turn's user message) — captured BEFORE the auto-continue loop below can
// append resume prompts to `messages`. persistTurnTranscript must compare
// against this base: passing the grown len(messages) made the stored
// prefix mismatch and the turn-end reconcile hard-diverge, silently
// dropping the mid-turn notices and resume prompts from memory.
turnBaseLen := len(messages)

// ... auto-continue loop appends resume prompts and continuation responses ...

h.persistTurnTranscript(sessionID, as, turnBaseLen, "turn-end")
```

## Why It's Subtle

The auto-continue loop is bounded (e.g. `AutoContinueChainCap = 4`), but even a single iteration changes `len(messages)`. The resume prompt is a user-role message the model uses to continue its work; if the reconcile drops it, the model loses context on the next reload. The symptom is not an error — it's a silent data loss where mid-turn notices and resume prompts vanish from the transcript.

This pattern generalises to **any bounded loop that mutates the message slice between a captured base length and a final persist call**: the base must be captured before the first mutation.

## Detection

- Session transcripts lose mid-turn notices or resume prompts after auto-continue.
- `persistTurnTranscript` logs "diverged from a concurrent writer; re-synced to disk" with a dropped-suffix count > 0.
- The on-disk transcript is missing rows that the in-memory transcript had before the save.

## Files

| Area | Path |
|------|------|
| turnBaseLen capture | `internal/server/agent_session.go:615-621` |
| Auto-continue loop | `internal/server/agent_session.go:734-779` |
| persistTurnTranscript | `internal/server/agent_session.go:925-951` |
| reconcileTurnSave | `internal/server/agent_session.go:955-960` |

## The Rule

> **In any bounded loop that appends to a message slice, capture the base length before the first iteration.** The persist/reconcile call after the loop must reference the pre-loop base, not the post-loop length. This prevents silent hard divergence where mid-turn rows are dropped from the transcript.
