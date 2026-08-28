---
type: Gotcha
title: Chat Input — Queued Messages Lost on Submission Failure
description: Queued messages are lost when submission fails — sendMessage returns false but the message is already shifted from the queue
tags:
  - gotcha
  - chat
  - frontend
  - message-loss
  - queue
timestamp: 2026-08-28T07:06:13Z
---
## Chat Input — Queued Messages Lost on Submission Failure

### Problem

In `ChatInput.tsx` and `useChat.ts`, queued messages are silently lost when submission fails. `sendMessage` returns `false` on failure, but the message is already shifted from the internal queue before the failure is checked. This means the message is gone from both the queue and the UI — no error feedback, no recovery path.

### Root Cause

The queue-shift happens optimistically (before the actual send attempt), and the failure branch does not re-enqueue the message. The `sendMessage` return value is checked too late.

### Impact

- User types a message, presses send, and the message vanishes with no indication it failed.
- No retry mechanism — the user must re-type the message manually.
- Affects all chat input under load, network hiccups, or provider errors.

### Fix Direction

Two viable approaches:

1. **Shift after success:** Move the queue shift to after `sendMessage` returns `true`. On `false`, leave the message in the queue and surface the error.
2. **Re-enqueue on failure:** Keep the optimistic shift but push the message back into the queue (at the front) when `sendMessage` returns `false`, and show an error state.

Option 1 is simpler and avoids ordering issues with re-enqueueing.

### Related Files

- `web/src/components/ChatInput.tsx` — the input component
- `web/src/hooks/useChat.ts` — the hook managing send logic and queue

### Status

- Documented: 2026-08-28
- Not yet fixed